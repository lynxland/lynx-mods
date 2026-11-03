package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/lynxland/lynx-start/cli/internal/registry"
)

// cmdRegistry: 模块贡献者在 registry 仓库内使用的登记/体检命令。
//
//	lynx-start registry add <module-dir>   一处输入三处一致：module.json 骨架
//	                                      （缺时生成）+ registry.json 登记 + go.work use
//	lynx-start registry check              三处一致性体检（CI 可用，问题即非零退出）
func cmdRegistry(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: lynx-start registry <add|check> [--registry <repo-root>] [<module-dir>]")
	}
	sub := args[0]
	rest := args[1:]
	fs := flag.NewFlagSet("registry "+sub, flag.ContinueOnError)
	registryDir := fs.String("registry", os.Getenv("LYNXSTART_REGISTRY"), "lynx-start repo root (the directory containing registry/ and go.work)")
	rest = reorderArgs(map[string]bool{"registry": true}, rest)
	if err := fs.Parse(rest); err != nil {
		return err
	}
	if *registryDir == "" {
		if cwd, err := os.Getwd(); err == nil {
			if up, ok := findRepoRoot(cwd); ok {
				*registryDir = up
			}
		}
	}
	if *registryDir == "" {
		return fmt.Errorf("--registry is required (or run from inside the lynx-start repo, or set LYNXSTART_REGISTRY)")
	}
	switch sub {
	case "add":
		if fs.NArg() != 1 {
			return fmt.Errorf("usage: lynx-start registry add <module-dir> (e.g. modules/todo)")
		}
		return registryAdd(*registryDir, fs.Arg(0))
	case "check":
		if fs.NArg() != 0 {
			return fmt.Errorf("usage: lynx-start registry check")
		}
		return registryCheck(*registryDir)
	default:
		return fmt.Errorf("unknown registry subcommand %q (want add|check)", sub)
	}
}

// findRepoRoot 向上找包含 registry/registry.json 的目录（贡献者在仓库内
// 任意子目录执行时免传 --registry）。
func findRepoRoot(from string) (string, bool) {
	for dir := from; ; dir = filepath.Dir(dir) {
		if _, err := os.Stat(filepath.Join(dir, "registry", "registry.json")); err == nil {
			return dir, true
		}
		if dir == filepath.Dir(dir) {
			return "", false
		}
	}
}

func registryAdd(root, moduleDir string) error {
	name := filepath.Base(moduleDir)
	if !regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`).MatchString(name) {
		return fmt.Errorf("module name %q invalid: lowercase letters/digits/- only (derived from directory name)", name)
	}
	dir := moduleDir
	if !filepath.IsAbs(dir) {
		dir = filepath.Join(root, moduleDir)
	}
	if _, err := os.Stat(dir); err != nil {
		return fmt.Errorf("module directory %s not found: %w", moduleDir, err)
	}
	goModPath, err := readGoModPath(dir)
	if err != nil {
		return fmt.Errorf("%s must be a real Go module (go.mod): %w", moduleDir, err)
	}

	manifestPath := filepath.Join(dir, "module.json")
	if _, err := os.Stat(manifestPath); os.IsNotExist(err) {
		skeleton := map[string]any{
			"name":               name,
			"version":            "0.1.0",
			"description":        "TODO: 一句话用途说明（registry list 展示）",
			"modulePath":         goModPath,
			"moduleDependencies": []string{},
			"goDependencies":     []string{},
			"configFragment":     "",
			"publishedEvents":    []string{},
		}
		raw, err := json.MarshalIndent(skeleton, "", "  ")
		if err != nil {
			return err
		}
		if err := os.WriteFile(manifestPath, append(raw, '\n'), 0o644); err != nil {
			return err
		}
		fmt.Printf("write  %s/module.json (skeleton — fill description/publishedEvents)\n", moduleDir)
	} else {
		manifest, err := registry.LoadManifest(dir, name)
		if err != nil {
			return fmt.Errorf("existing module.json broken: %w", err)
		}
		if manifest.Name != name {
			return fmt.Errorf("module.json name %q != directory %q", manifest.Name, name)
		}
		if manifest.ModulePath != "" && manifest.ModulePath != goModPath {
			return fmt.Errorf("module.json modulePath %q != go.mod module %q", manifest.ModulePath, goModPath)
		}
		fmt.Printf("ok     %s/module.json (validated)\n", moduleDir)
	}

	if err := registerInIndex(root, name, filepath.ToSlash(moduleDir)); err != nil {
		return err
	}
	if err := addToGoWork(root, filepath.ToSlash(moduleDir)); err != nil {
		return err
	}
	fmt.Printf("\n✓ %s registered\nNext steps:\n  fill description & publishedEvents in module.json (must match Module())\n  mise run lint && mise run test\n  git tag modules/%s/v0.1.0 (module.json version must already say 0.1.0)\n", name, name)
	return nil
}

// readGoModPath 抽取 go.mod 的 module 行。
func readGoModPath(dir string) (string, error) {
	raw, err := os.ReadFile(filepath.Join(dir, "go.mod"))
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(raw), "\n") {
		if mod, ok := strings.CutPrefix(strings.TrimSpace(line), "module "); ok {
			return strings.TrimSpace(mod), nil
		}
	}
	return "", fmt.Errorf("go.mod has no module line")
}

// registerInIndex 把 {name, path} 追加进 registry/registry.json（保持单行
// 对象的现有排版；已存在则幂等跳过）。
func registerInIndex(root, name, path string) error {
	idxPath := filepath.Join(root, "registry", "registry.json")
	raw, err := os.ReadFile(idxPath)
	if err != nil {
		return err
	}
	if strings.Contains(string(raw), fmt.Sprintf("%q", name)) {
		fmt.Printf("ok     registry/registry.json (already lists %s)\n", name)
		return nil
	}
	entry := fmt.Sprintf("    { \"name\": %q, \"path\": %q }", name, path)
	s := strings.TrimRight(string(raw), "\n")
	if i := strings.LastIndex(s, "]"); i > 0 {
		head := strings.TrimRight(s[:i], " \t\n")
		tail := strings.TrimLeft(s[i+1:], " \t\n")
		s = head + ",\n" + entry + "\n  ]"
		if tail != "" {
			s += "\n" + tail
		}
	} else {
		return fmt.Errorf("registry/registry.json: no items array to append to")
	}
	if err := os.WriteFile(idxPath, []byte(s+"\n"), 0o644); err != nil {
		return err
	}
	fmt.Printf("write  registry/registry.json (+%s)\n", name)
	return nil
}

// addToGoWork 把 ./<path> 插进 go.work 的 use(...) 块（保持字母序；幂等）。
func addToGoWork(root, path string) error {
	workPath := filepath.Join(root, "go.work")
	raw, err := os.ReadFile(workPath)
	if err != nil {
		return err
	}
	use := "./" + path
	lines := strings.Split(string(raw), "\n")
	if strings.Contains(string(raw), use) {
		fmt.Printf("ok     go.work (already uses %s)\n", use)
		return nil
	}
	// 缩进跟随既有 use 行（真实仓库是 tab,别自作主张）。
	indent := "\t"
	for _, line := range lines {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "./") {
			indent = line[:len(line)-len(strings.TrimLeft(line, " \t"))]
			break
		}
	}
	var out []string
	inserted := false
	inUse := false
	for _, line := range lines {
		switch {
		case strings.TrimSpace(line) == "use (":
			inUse = true
			out = append(out, line)
		case inUse && strings.TrimSpace(line) == ")":
			out = append(out, indent+use)
			inserted = true
			inUse = false
			out = append(out, line)
		case inUse && strings.HasPrefix(strings.TrimSpace(line), "./"):
			if !inserted && strings.TrimSpace(line) > use {
				out = append(out, indent+use)
				inserted = true
			}
			out = append(out, line)
		default:
			out = append(out, line)
		}
	}
	if !inserted {
		return fmt.Errorf("go.work: no use(...) block found — add %s manually", use)
	}
	if err := os.WriteFile(workPath, []byte(strings.Join(out, "\n")), 0o644); err != nil {
		return err
	}
	fmt.Printf("write  go.work (+%s)\n", use)
	return nil
}

func registryCheck(root string) error {
	idx, err := registry.Load(root)
	if err != nil {
		return err
	}
	problems := 0
	fail := func(format string, args ...any) {
		problems++
		fmt.Printf("  ✗ ")
		fmt.Printf(format+"\n", args...)
	}
	ok := func(format string, args ...any) {
		fmt.Printf("  ✓ ")
		fmt.Printf(format+"\n", args...)
	}

	goWorkRaw, _ := os.ReadFile(filepath.Join(root, "go.work"))
	goWork := string(goWorkRaw)

	fmt.Printf("lynx-start registry check: %s (%d items)\n", root, len(idx.Items))
	// 先全量收集登记名再做依赖检查（依赖检查与登记顺序无关——
	// registry.json 按字母序排列时，依赖模块可能排在声明方之后）。
	registered := map[string]bool{}
	for _, it := range idx.Items {
		registered[it.Name] = true
	}
	seen := map[string]bool{}
	for _, it := range idx.Items {
		if seen[it.Name] {
			fail("registry.json duplicate item %s", it.Name)
			continue
		}
		seen[it.Name] = true
		dir := filepath.Join(root, it.Path)
		if _, err := os.Stat(dir); err != nil {
			fail("%s directory %s exists", it.Name, it.Path)
			continue
		}
		manifest, err := registry.LoadManifest(dir, it.Name)
		if err != nil {
			fail("%s module.json loads: %v", it.Name, err)
			continue
		}
		if manifest.Name != it.Name {
			fail("%s module.json name matches (%q)", it.Name, manifest.Name)
		}
		if manifest.Version == "" || !regexp.MustCompile(`^\d+\.\d+\.\d+$`).MatchString(manifest.Version) {
			fail("%s module.json version semver (got %q)", it.Name, manifest.Version)
		}
		if manifest.Description == "" || strings.HasPrefix(manifest.Description, "TODO") {
			fail("%s description filled", it.Name)
		}
		if modPath, err := readGoModPath(dir); err == nil && manifest.CanonicalModulePath() != modPath {
			fail("%s modulePath matches go.mod (%q vs %q)", it.Name, manifest.CanonicalModulePath(), modPath)
		}
		if !strings.Contains(goWork, "./"+it.Path) {
			fail("%s in go.work use(...)", it.Name)
		}
		for _, ev := range manifest.PublishedEvents {
			if !strings.HasPrefix(ev, it.Name+".") {
				fail("%s event %q prefixed by module name", it.Name, ev)
			}
		}
		for _, dep := range manifest.ModuleDependencies {
			if !registered[dep.Name] {
				fail("%s dependency %s registered", it.Name, dep.Name)
			}
		}
		if manifest.KernelConstraint != "" {
			if _, err := registry.ParseConstraint(manifest.KernelConstraint); err != nil {
				fail("%s kernelConstraint parses: %v", it.Name, err)
			}
		}
		ok("%s (v%s, %d events)", it.Name, manifest.Version, len(manifest.PublishedEvents))
	}
	// 反向：go.work 里 use 了但未登记的模块目录。
	for _, m := range regexp.MustCompile(`\./((?:modules|kernel|cli)/[A-Za-z0-9_-]+)`).FindAllStringSubmatch(goWork, -1) {
		if m[1] != "kernel" && m[1] != "cli" && strings.HasPrefix(m[1], "modules/") && !registered[strings.TrimPrefix(m[1], "modules/")] {
			fail("go.work uses ./%s but registry.json does not list it (run registry add)", m[1])
		}
	}
	if problems > 0 {
		return fmt.Errorf("registry check found %d problem(s)", problems)
	}
	fmt.Println("✓ registry consistent")
	return nil
}
