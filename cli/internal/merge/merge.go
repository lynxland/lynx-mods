// Package merge 提供源码模块更新的三方合并：local（应用本地）/ base（安装
// 基线，即上次从 registry 同步的形态）/ other（registry 新版）。文件级合并
// 委托 git merge-file（应用开发者必然有 git，且语义与 git 冲突标记一致）；
// 本包做树级编排（新增/删除/保留的判定）。
package merge

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
)

// File 三方合并单文件内容：返回合并结果与冲突块数。
// 冲突时结果是带 <<<<<<< yours / ======= / >>>>>>> theirs 标记的文本，
// 交由调用方落盘并提示用户解决。
func File(local, base, other []byte) (merged []byte, conflicts int, err error) {
	dir, err := os.MkdirTemp("", "lynx-start-merge-")
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = os.RemoveAll(dir) }()
	write := func(name string, content []byte) (string, error) {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, content, 0o644); err != nil {
			return "", err
		}
		return p, nil
	}
	lpath, err := write("local", local)
	if err != nil {
		return nil, 0, err
	}
	bpath, err := write("base", base)
	if err != nil {
		return nil, 0, err
	}
	opath, err := write("other", other)
	if err != nil {
		return nil, 0, err
	}
	cmd := exec.Command("git", "merge-file",
		"-p", "-L", "yours", "-L", "baseline", "-L", "theirs",
		lpath, bpath, opath)
	out, err := cmd.Output()
	if err == nil {
		return out, 0, nil
	}
	ee, ok := err.(*exec.ExitError)
	if !ok {
		return nil, 0, fmt.Errorf("git merge-file: %w", err)
	}
	// git merge-file 退出码 = 冲突块数（1..127）；>=128 是执行错误。
	if code := ee.ExitCode(); code > 0 && code < 128 {
		return out, code, nil
	}
	return nil, 0, fmt.Errorf("git merge-file exited %d: %s", ee.ExitCode(), ee.Stderr)
}

// TreeResult 是一次树级三方合并的结果。
type TreeResult struct {
	// Updates 是需要写回应用的路径 → 内容（含带冲突标记的文件）。
	Updates map[string][]byte
	// Conflicts 是存在冲突标记的路径（用户须解决后才能编译/提交）。
	Conflicts []string
	// Deleted 是 upstream 已删除且本地未改、可安全删除的路径
	// （调用方先备份后落盘删除）。
	Deleted []string
	// Removed 是 upstream 已删除但本地有改动的路径（不自动删，提示处理）。
	Removed []string
	// Unchanged 是三方一致、无需写入的文件数。
	Unchanged int
}

// Tree 对三棵内存树做三方合并。键是斜杠相对路径。
// 判定：
//   - 三方都有：git merge-file；local 与 other 改动不重叠则干净合并
//   - upstream 新增（不在 base）：直接采用；本地恰有同名文件则视为冲突
//   - upstream 删除、本地未改（local==base）：安全删除（与 git 三方语义
//     一致；上游明确删了而用户没动过，结果就该是删。首次实践：模块
//     弃 vendored policy.proto 改 BSR dep，残留副本与 dep 同路径冲突）
//   - upstream 删除、本地有改动：不可逆判断交还用户，列入 Removed 提示
//   - upstream 删除、用户也已删：无事
//   - 只在 local：用户自己的文件，不碰
func Tree(local, base, other map[string][]byte) (*TreeResult, error) {
	res := &TreeResult{Updates: map[string][]byte{}}
	paths := map[string]bool{}
	for p := range base {
		paths[p] = true
	}
	for p := range other {
		paths[p] = true
	}
	sorted := make([]string, 0, len(paths))
	for p := range paths {
		sorted = append(sorted, p)
	}
	sort.Strings(sorted)

	for _, p := range sorted {
		o, inOther := other[p]
		b, inBase := base[p]
		l, inLocal := local[p]
		switch {
		case !inOther && inBase && !inLocal:
			// upstream 与用户都删了：无事。
		case !inOther && inBase && string(l) == string(b):
			res.Deleted = append(res.Deleted, p)
		case !inOther && inBase:
			res.Removed = append(res.Removed, p)
		case !inBase && inOther && !inLocal:
			res.Updates[p] = o
		case !inBase && inOther && inLocal:
			if string(l) == string(o) {
				res.Unchanged++
				break
			}
			// 用户自建的同名文件撞上 upstream 新文件：统一走 merge-file
			// （空 base = 双方新增，天然全文件冲突语义）。
			merged, n, err := File(l, nil, o)
			if err != nil {
				return nil, fmt.Errorf("merge %s: %w", p, err)
			}
			res.Updates[p] = merged
			if n > 0 {
				res.Conflicts = append(res.Conflicts, p)
			}
		case inBase && inOther && !inLocal:
			// 用户删过的文件 upstream 还在：不复活（尊重本地删除），提示。
			res.Unchanged++
		case inBase && inOther:
			if string(l) == string(b) {
				// 本地未改：直接采用新版（跳过 merge-file，语义等价且更快）。
				if string(o) != string(b) {
					res.Updates[p] = o
				} else {
					res.Unchanged++
				}
				break
			}
			if string(o) == string(b) {
				// upstream 未改：本地改动原样保留。
				res.Unchanged++
				break
			}
			merged, n, err := File(l, b, o)
			if err != nil {
				return nil, fmt.Errorf("merge %s: %w", p, err)
			}
			res.Updates[p] = merged
			if n > 0 {
				res.Conflicts = append(res.Conflicts, p)
			}
		}
	}
	return res, nil
}
