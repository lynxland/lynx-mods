// Package lock 维护应用侧安装锁（.lynx-start/lock.json）：每模块的解析
// 版本、来源（tag / worktree）与 commit、基线文件的 sha256。供应链审计的
// 最小闭环——「往应用贴源码」必须可事后核验：
//
//   - 基线完整性：update 三方合并以基线为锚，基线被篡改会静默污染合并；
//     写入时记哈希，update/doctor 复核。
//   - 版本溯源：钉版安装记 tag commit；worktree 安装记当时 HEAD。tag 不应
//     移动——doctor 对比安装时的 commit 与 tag 当前指向。
package lock

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
)

// Entry 是单模块的锁条目。
type Entry struct {
	Version string `json:"version"`
	// Source 是安装来源描述："tag modules/<name>/v<version>" 或 "worktree"。
	Source string `json:"source,omitempty"`
	// Commit 是来源的 commit hash（解析失败 "unknown"）。
	Commit string            `json:"commit,omitempty"`
	Files  map[string]string `json:"files"` // 基线相对路径 → sha256
}

// Lock 是全部已装模块的锁。
type Lock struct {
	Modules map[string]Entry `json:"modules"`
}

// Path 返回锁文件路径。
func Path(appDir string) string {
	return filepath.Join(appDir, ".lynx-start", "lock.json")
}

// Load 读锁；文件不存在返回空锁（旧应用无锁，跳过校验语义）。
func Load(appDir string) (*Lock, error) {
	raw, err := os.ReadFile(Path(appDir))
	if os.IsNotExist(err) {
		return &Lock{Modules: map[string]Entry{}}, nil
	}
	if err != nil {
		return nil, err
	}
	var l Lock
	if err := json.Unmarshal(raw, &l); err != nil {
		return nil, fmt.Errorf("parse %s: %w", Path(appDir), err)
	}
	if l.Modules == nil {
		l.Modules = map[string]Entry{}
	}
	return &l, nil
}

// Save 写锁（缩进两格 + 末尾换行；键有序由 json map 天然无序——模块数
// 少，可读性接受）。
func (l *Lock) Save(appDir string) error {
	if err := os.MkdirAll(filepath.Dir(Path(appDir)), 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(l, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(Path(appDir), append(raw, '\n'), 0o644)
}

// Record 哈希 baselineDir 现状并记录条目（add/update 共用：在基线写盘
// 之后调用）。
func (l *Lock) Record(baselineDir, module, version, source, commit string) error {
	hashes, err := HashTree(baselineDir)
	if err != nil {
		return err
	}
	l.Modules[module] = Entry{Version: version, Source: source, Commit: commit, Files: hashes}
	return nil
}

// Remove 删除条目（无条目幂等）。
func (l *Lock) Remove(module string) {
	delete(l.Modules, module)
}

// Mismatch 返回与 dir 现状不符的文件清单（哈希不同 / 锁有盘无 / 盘有锁无）。
func (e Entry) Mismatch(dir string) ([]string, error) {
	current, err := HashTree(dir)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var out []string
	for rel, want := range e.Files {
		got, ok := current[rel]
		switch {
		case !ok:
			out = append(out, rel+" (missing from baseline)")
		case got != want:
			out = append(out, rel+" (hash mismatch)")
		}
		seen[rel] = true
	}
	for rel := range current {
		if !seen[rel] {
			out = append(out, rel+" (not in lock)")
		}
	}
	sort.Strings(out)
	return out, nil
}

// HashTree 哈希目录内全部文件（斜杠相对路径 → sha256 hex）。
func HashTree(root string) (map[string]string, error) {
	out := map[string]string{}
	if _, err := os.Stat(root); os.IsNotExist(err) {
		return out, nil
	}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(raw)
		out[filepath.ToSlash(rel)] = hex.EncodeToString(sum[:])
		return nil
	})
	return out, err
}

// ResolveCommit 解析安装来源的 commit：version 非空取 tag
// modules/<name>/v<version> 的指向（安装期已验证 tag 存在）；空取 registry
// 工作区 HEAD。非 git 目录返回 "unknown"（溯源降级，不阻断安装）。
func ResolveCommit(registryDir, module, version string) (source, commit string) {
	if version != "" {
		tag := fmt.Sprintf("modules/%s/v%s", module, version)
		if out, err := exec.Command("git", "-C", registryDir, "rev-parse", tag+"^{commit}").Output(); err == nil {
			return "tag " + tag, firstLine(out)
		}
		return "tag " + tag, "unknown"
	}
	if out, err := exec.Command("git", "-C", registryDir, "rev-parse", "HEAD").Output(); err == nil {
		return "worktree", firstLine(out)
	}
	return "worktree", "unknown"
}

// TagCommit 返回 tag 当前指向的 commit（tag 不存在返回 "", false）。
func TagCommit(registryDir, module, version string) (string, bool) {
	out, err := exec.Command("git", "-C", registryDir, "rev-parse", fmt.Sprintf("modules/%s/v%s^{commit}", module, version)).Output()
	if err != nil {
		return "", false
	}
	return firstLine(out), true
}

func firstLine(b []byte) string {
	s := string(b)
	for i, r := range s {
		if r == '\n' {
			return s[:i]
		}
	}
	return s
}
