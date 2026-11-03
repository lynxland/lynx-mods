package main

import (
	"flag"
	"fmt"
	"go/format"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/lynxland/lynx-start/cli/internal/lock"
	"github.com/lynxland/lynx-start/cli/internal/manifest"
	"github.com/lynxland/lynx-start/cli/internal/merge"
	"github.com/lynxland/lynx-start/cli/internal/registry"
	"github.com/lynxland/lynx-start/cli/internal/render"
)

// cmdUpdate: lynx-start update <module>[@<version>] —— 把已装模块更新到
// registry 新版，与本地改动做三方合并（local / 安装基线 / upstream）。
//
// 语义：基线 ≡ 上次从 registry 同步的形态（add 时建立，update 后刷新）。
// 本地与 upstream 改动不重叠则自动合并；同区改动打 git 冲突标记，冲突文件
// 覆盖前在 .lynx-start/backup/ 留副本。upstream 删除：本地未改 → 自动删
// （删除前同款备份）；本地有改动 → 保留并提示。建议先 commit 应用再
// update——合并写盘不可自动回滚。
func cmdUpdate(args []string) error {
	fs := flag.NewFlagSet("update", flag.ContinueOnError)
	registryDir := fs.String("registry", os.Getenv("LYNXSTART_REGISTRY"), "lynx-start repo directory")
	dir := fs.String("dir", ".", "app directory")
	force := fs.Bool("force", false, "re-sync even when the version is unchanged")
	dryRun := fs.Bool("dry-run", false, "preview the merge plan without writing anything")
	args = reorderArgs(map[string]bool{"registry": true, "dir": true}, args)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("usage: lynx-start update <module>[@<version>] [--registry <dir>] [--dry-run]")
	}
	name := fs.Arg(0)
	version := ""
	if base, ver, found := strings.Cut(name, "@"); found {
		name, version = base, ver
	}
	appDir, err := filepath.Abs(*dir)
	if err != nil {
		return err
	}
	m, err := manifest.Load(appDir)
	if err != nil {
		return err
	}
	installed, ok := m.Get(name)
	if !ok {
		return fmt.Errorf("module %s is not installed (see lynx-start add)", name)
	}
	if *registryDir == "" {
		*registryDir = m.Registry
	}
	idx, regDir, err := registryLoad(*registryDir)
	if err != nil {
		return err
	}
	*registryDir = regDir
	mod, cleanup, err := idx.ManifestAt(*registryDir, name, version)
	if err != nil {
		return err
	}
	defer cleanup()

	if mod.Version == installed.Version && !*force {
		fmt.Printf("✓ %s already at %s (use --force to re-sync the baseline from the registry worktree)\n", name, mod.Version)
		return nil
	}
	// 供应链锁复核 + 基线缓存就位（ADR-0006：基线住 ~/.lynx-start，缺失时
	// 按 lock 记录的 commit 从 registry 确定性重建，换机器不依赖提交基线）。
	lk, err := lock.Load(appDir)
	if err != nil {
		return err
	}
	baseDir, err := ensureBaseline(idx, *registryDir, m, lk, name)
	if err != nil {
		return err
	}
	if entry, ok := lk.Modules[name]; ok {
		mismatch, err := entry.Mismatch(baseDir)
		if err != nil {
			return err
		}
		if len(mismatch) > 0 {
			return fmt.Errorf("baseline for %s drifted from .lynx-start/lock.json (tampered or hand-edited): %s — rebuild with `lynx-start add %s -o` (overwrites local files; commit first)", name, strings.Join(mismatch, ", "), name)
		}
	}
	base, hasBase, err := loadBaselineTree(baseDir)
	if err != nil {
		return err
	}
	if !hasBase || len(base) == 0 {
		return fmt.Errorf("no update baseline for %s: re-install with `lynx-start add %s -o` to rebuild it (your files are overwritten by upstream; commit local changes first), then update is available again", name, name)
	}

	// upstream 渲染到临时目录：与 add 同一管线（改写导入路径、gofmt、
	// 排除工程文件），保证与应用/基线处于同一形态空间。
	tmp, err := os.MkdirTemp("", "lynx-start-upstream-")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(tmp) }()
	if err := renderUpstream(idx, *registryDir, mod, m, tmp); err != nil {
		return err
	}
	other, err := readTree(tmp)
	if err != nil {
		return err
	}
	if len(other) == 0 {
		return fmt.Errorf("registry module %s rendered to an empty tree (module.json broken?)", name)
	}

	// local 树：基线与 upstream 文件并集对应的应用现状（缺 = 用户已删）。
	local := map[string][]byte{}
	for rel := range base {
		local[rel] = nil
	}
	for rel := range other {
		local[rel] = nil
	}
	for rel := range local {
		if content, err := os.ReadFile(filepath.Join(appDir, filepath.FromSlash(rel))); err == nil {
			local[rel] = content
		} else {
			delete(local, rel)
		}
	}

	res, err := merge.Tree(local, base, other)
	if err != nil {
		return err
	}

	// --dry-run：合并计划完整呈现（自动合并 / 冲突 / upstream 删除），
	// 不写任何文件——把「写盘不可回滚，先 commit」的判断依据交给用户。
	if *dryRun {
		fmt.Printf("\n(dry-run) %s %s → %s: %d auto-merged, %d unchanged, %d conflicts, %d deleted-upstream, %d removed-upstream-needs-review\n",
			name, installed.Version, mod.Version, len(res.Updates), res.Unchanged, len(res.Conflicts), len(res.Deleted), len(res.Removed))
		printPlan("auto-merge", sortedKeys(res.Updates))
		printPlan("conflict", res.Conflicts)
		printPlan("delete", res.Deleted)
		printPlan("removed-upstream", res.Removed)
		if len(res.Conflicts) > 0 {
			fmt.Println("  conflicts would get git markers (yours/baseline/theirs); nothing is written in dry-run")
		}
		fmt.Println("  (dry-run writes nothing; re-run without --dry-run to apply — commit the app first)")
		return nil
	}

	// 冲突覆盖与自动删除前留备份（用户目录缓存，与应用仓库分离）。
	backup := backupCacheDir(m.ModulePath, name)
	for _, rel := range append(append([]string{}, res.Conflicts...), res.Deleted...) {
		src := filepath.Join(appDir, filepath.FromSlash(rel))
		if content, err := os.ReadFile(src); err == nil {
			dst := filepath.Join(backup, filepath.FromSlash(rel))
			if err := os.MkdirAll(filepath.Dir(dst), 0o755); err == nil {
				_ = os.WriteFile(dst, content, 0o644)
			}
		}
	}
	rels := make([]string, 0, len(res.Updates))
	for rel := range res.Updates {
		rels = append(rels, rel)
	}
	sort.Strings(rels)
	for _, rel := range rels {
		target := filepath.Join(appDir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(target, res.Updates[rel], 0o644); err != nil {
			return fmt.Errorf("write %s: %w", rel, err)
		}
	}

	// upstream 删除且本地未改的文件落盘删除；清掉因此变空的父目录
	// （vendored policy.proto 弃用后残留副本会与 BSR dep 同路径冲突）。
	for _, rel := range res.Deleted {
		if err := os.Remove(filepath.Join(appDir, filepath.FromSlash(rel))); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("delete %s: %w", rel, err)
		}
		removeEmptyParentDirs(appDir, filepath.Join(appDir, filepath.Dir(filepath.FromSlash(rel))))
	}

	// 桥接包无条件刷新（注入物，不参与合并）。
	bridge := bridgeModule(m.ModulePath, name)
	if formatted, ferr := format.Source(bridge); ferr == nil {
		bridge = formatted
	}
	bridgePath := filepath.Join(appDir, filepath.FromSlash(bridgeRelPath(name)))
	if err := os.MkdirAll(filepath.Dir(bridgePath), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(bridgePath, bridge, 0o644); err != nil {
		return fmt.Errorf("write bridge module.go: %w", err)
	}

	// 幂等注入重放：新版模块可能带来新的装配登记/配置段；锚点丢失不阻断
	// 源码更新（此时 add 时也修不好的锚点，交给用户比对 scaffold 模板）。
	if err := injectModulesEntry(appDir, name, m.ModulePath); err != nil {
		fmt.Printf("warn: modules.go injection skipped: %v\n", err)
	}
	if err := appendEnvExample(appDir, m, mod); err != nil {
		fmt.Printf("warn: env example skipped: %v\n", err)
	}
	if err := appendConfigFragment(appDir, name, mod.ConfigFragment); err != nil {
		fmt.Printf("warn: config fragment skipped: %v\n", err)
	}

	files := make([]string, 0, len(other))
	for rel := range other {
		files = append(files, rel)
	}
	sort.Strings(files)
	source, commit := lock.ResolveCommit(*registryDir, name, version)
	newBaseDir := baselineCacheDir(m.ModulePath, name, commit)
	if err := replaceBaselineTree(newBaseDir, other); err != nil {
		return err
	}
	if err := lk.Record(newBaseDir, name, mod.Version, source, commit); err != nil {
		return err
	}
	if err := lk.Save(appDir); err != nil {
		return err
	}
	m.Set(manifest.Module{Name: name, Version: mod.Version, Files: files})
	if err := m.Save(appDir); err != nil {
		return err
	}

	fmt.Printf("\n✓ %s %s → %s (%d merged, %d unchanged", name, installed.Version, mod.Version, len(rels), res.Unchanged)
	if len(res.Deleted) > 0 {
		fmt.Printf(", %d deleted", len(res.Deleted))
	}
	if len(res.Conflicts) > 0 {
		fmt.Printf(", %d CONFLICTS", len(res.Conflicts))
	}
	fmt.Println(")")
	for _, rel := range res.Deleted {
		fmt.Printf("  deleted upstream (pre-delete copy in %s/%s)\n", backup, rel)
	}
	for _, rel := range res.Conflicts {
		fmt.Printf("  conflict %s  (markers: yours/baseline/theirs; pre-update copy in %s/%s)\n", rel, backup, rel)
	}
	for _, rel := range res.Removed {
		fmt.Printf("  removed upstream with local edits, kept: %s (delete manually if unwanted)\n", rel)
	}
	if len(res.Conflicts) > 0 {
		fmt.Println("  resolve conflicts (search for <<<<<<<), then build: go build ./...")
	} else {
		fmt.Println(`Next steps (order matters):
  go mod tidy            # appkit/module deps may have moved
  mise run generate      # if .proto files changed
  go build ./... && go test ./...`)
	}
	return nil
}

// renderUpstream 用 add 同款管线把模块源码树渲染到 dst（布局重定位到
// modules/<m>/ + 五条结构化导入改写——与应用文件/基线同处一个路径空间，
// 合并不会产生虚假冲突）。依赖前缀集取 manifest 已装模块（与安装时
// 确定性等价：改写只对源码中实际出现的前缀生效）。
func renderUpstream(idx *registry.Index, registryDir string, mod *registry.ModuleManifest, m *manifest.Manifest, dst string) error {
	canonicals := map[string]string{mod.Name: mod.CanonicalModulePath()}
	for _, installed := range m.Modules {
		if installed.Name == mod.Name {
			continue
		}
		depMod, err := idx.Manifest(registryDir, installed.Name)
		if err != nil {
			return err
		}
		canonicals[installed.Name] = depMod.CanonicalModulePath()
	}
	rewrite := moduleRewriter(m.ModulePath, canonicals)
	res, err := render.Tree(mod.Dir, dst, render.NewData(m.Name, m.ModulePath), render.Options{
		Overwrite: true,
		Skip:      skipModuleFile,
		Relocate:  func(rel string) (string, error) { return relocateModule(mod.Name, rel) },
		Rewrite: func(rel string, content []byte) []byte {
			return rewriteGuardTest(mod.Name, rel, rewrite(rel, content))
		},
	})
	if err != nil {
		return err
	}
	for _, rel := range res.Created {
		content, rerr := os.ReadFile(filepath.Join(dst, filepath.FromSlash(rel)))
		if rerr != nil {
			return rerr
		}
		if resi, bad := canonicalResidue(rel, content); bad {
			return fmt.Errorf("%s: canonical module path %q survived the rewrite (layout rule gap) — report this bug", rel, resi)
		}
	}
	return nil
}

// printPlan 逐行列出合并计划文件（dry-run 报告用）。
func printPlan(kind string, rels []string) {
	for _, rel := range rels {
		fmt.Printf("  %-15s %s\n", kind, rel)
	}
}

// removeEmptyParentDirs 从 dir 向上删除空目录直到 root（不含 root）——
// upstream 删除文件后不留空壳目录树（如 proto/grpcapi/v1/）。
func removeEmptyParentDirs(root, dir string) {
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return
	}
	dirAbs, err := filepath.Abs(dir)
	if err != nil {
		return
	}
	for strings.HasPrefix(dirAbs, rootAbs+string(filepath.Separator)) {
		entries, err := os.ReadDir(dirAbs)
		if err != nil || len(entries) > 0 {
			return
		}
		if err := os.Remove(dirAbs); err != nil {
			return
		}
		dirAbs = filepath.Dir(dirAbs)
	}
}

// sortedKeys 返回 map 键的排序切片。
func sortedKeys(m map[string][]byte) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// readTree 把目录读为内存树（斜杠路径 → 内容）。
func readTree(root string) (map[string][]byte, error) {
	tree := map[string][]byte{}
	return tree, filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
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
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		tree[filepath.ToSlash(rel)] = content
		return nil
	})
}
