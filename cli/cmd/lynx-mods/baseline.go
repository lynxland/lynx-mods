package main

// 基线快照（update 三方合并的 base）存用户目录缓存
// ~/.lynx-mods/baselines/<appModulePath>/<module>@<commit>/，内容是
// 「最近一次从 registry 同步的最终形态」——已过导入改写与 gofmt，与应用
// 文件同处一个路径空间（modules/<m>/…），合并不会产生虚假冲突。
//
// 缓存不进应用仓库（lock.json + lynx-mods.json 才提交）：基线可由
// registry + lock 确定性重建（ensureBaseline——按 lock 记录的 commit
// archive 源码，经 add 同款改写管线复现），换机器不再依赖提交基线。
// 同一模块不同 commit 的基线并存（多版本区分）；旧 commit 不自动清理，
// 可定期手动删 ~/.lynx-mods/baselines。
//
// 不变量：基线 ≡ upstream 最近一次同步形态。冲突文件解决后 baseline 仍为
// upstream 形态（下次 update 把用户的解决结果视为本地改动自然保留）。

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"

	"github.com/lynxland/lynx-mods/cli/internal/lock"
	"github.com/lynxland/lynx-mods/cli/internal/manifest"
	"github.com/lynxland/lynx-mods/cli/internal/registry"
	"github.com/lynxland/lynx-mods/cli/internal/render"
)

// syncBaseline 在 add 后同步基线：本次被 upstream 写入的文件（Created）刷新
// 为应用现状；被跳过的文件仅在基线缺项时补记（重装不覆盖时保留旧基线，
// 不把本地漂移误当作「已同步」）。
func syncBaseline(appDir, baseDir string, res *render.Result) error {
	for _, rel := range res.Created {
		if err := copyIntoBaseline(appDir, baseDir, rel); err != nil {
			return err
		}
	}
	for _, rel := range res.Skipped {
		if _, err := os.Stat(filepath.Join(baseDir, filepath.FromSlash(rel))); err == nil {
			continue
		}
		if err := copyIntoBaseline(appDir, baseDir, rel); err != nil {
			return err
		}
	}
	return nil
}

func copyIntoBaseline(appDir, dir, rel string) error {
	src := filepath.Join(appDir, filepath.FromSlash(rel))
	content, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	target := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	return os.WriteFile(target, content, 0o644)
}

// treeHasFiles 报告目录存在且含文件。
func treeHasFiles(dir string) bool {
	found := false
	_ = filepath.WalkDir(dir, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if !d.IsDir() {
			found = true
		}
		return nil
	})
	return found
}

// ensureBaseline 确保基线缓存就位并返回其目录：已有则直用；缺失则按
// lock 条目（commit + version）从 registry 提取该历史形态，经 add 同款
// 改写管线（依赖前缀集 = manifest 已装模块，与安装时确定性等价——改写
// 只对源码中实际出现的前缀生效）重建。重建的产物只写用户缓存，不碰应用。
func ensureBaseline(idx *registry.Index, registryDir string, m *manifest.Manifest, lk *lock.Lock, name string) (string, error) {
	commit, version := "", ""
	if entry, ok := lk.Modules[name]; ok {
		commit, version = entry.Commit, entry.Version
	}
	baseDir := baselineCacheDir(m.ModulePath, name, commit)
	if treeHasFiles(baseDir) {
		return baseDir, nil
	}
	if commit == "" || commit == "unknown" {
		return "", fmt.Errorf("no baseline cache for %s and the install source is unrecorded (pre-lock install): re-install with `lynx-mods add %s -o` to rebuild it (your files are overwritten by upstream; commit local changes first)", name, name)
	}
	item, cleanup, err := idx.ItemAtCommit(registryDir, name, commit, version)
	if err != nil {
		return "", err
	}
	defer cleanup()
	tmp, err := os.MkdirTemp("", "lynx-mods-baseline-")
	if err != nil {
		return "", err
	}
	defer func() { _ = os.RemoveAll(tmp) }()
	if err := renderUpstream(idx, registryDir, item, m, tmp); err != nil {
		return "", fmt.Errorf("rebuild baseline for %s: %w", name, err)
	}
	if err := os.RemoveAll(baseDir); err != nil {
		return "", err
	}
	if err := moveTree(tmp, baseDir); err != nil {
		return "", err
	}
	fmt.Printf("  baseline cache rebuilt: %s@%s → %s\n", name, shortHash(commit), baseDir)
	return baseDir, nil
}

// moveTree 把 src 整树搬移为 dst。重建中间失败下次重来，无需原子性；
// os.Rename 跨文件系统（/tmp 缓存 → 用户目录）会 EXDEV，回退逐文件复制。
func moveTree(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	if err := os.Rename(src, dst); err == nil {
		return nil
	}
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if err := os.WriteFile(target, content, 0o644); err != nil {
			return err
		}
		return os.Remove(path)
	})
}

// loadBaselineTree 读基线为内存树（斜杠路径 → 内容）。目录缺失返回空树
// 与 ok=false（update 据此给出可行动提示）。
func loadBaselineTree(baseDir string) (map[string][]byte, bool, error) {
	tree := map[string][]byte{}
	if _, err := os.Stat(baseDir); err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, false, err
	}
	err := filepath.WalkDir(baseDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(baseDir, path)
		if err != nil {
			return err
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		tree[filepath.ToSlash(rel)] = content
		return nil
	})
	return tree, true, err
}

// replaceBaselineTree 用 upstream 形态整树替换基线并清掉 upstream 已不存在的
// 旧条目（维持「基线 ≡ upstream 最近同步形态」不变量）。
func replaceBaselineTree(baseDir string, upstream map[string][]byte) error {
	if err := os.RemoveAll(baseDir); err != nil {
		return err
	}
	rels := make([]string, 0, len(upstream))
	for rel := range upstream {
		rels = append(rels, rel)
	}
	sort.Strings(rels)
	for _, rel := range rels {
		target := filepath.Join(baseDir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(target, upstream[rel], 0o644); err != nil {
			return fmt.Errorf("write baseline %s: %w", rel, err)
		}
	}
	return nil
}
