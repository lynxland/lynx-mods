package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/lynxland/lynx-start/cli/internal/manifest"
	"github.com/lynxland/lynx-start/cli/internal/registry"
	"github.com/lynxland/lynx-start/cli/internal/render"
)

// goDirective 是生成应用 go.mod 的 go 指令——与 lynx-start go.mod 一致，
// 保证应用 tidy 时不需要更高工具链。
const goDirective = "1.26.8"

// 依赖地板：scaffold 生成代码直接使用的最低版本（chain.DefaultChain 与
// grpcapi openapi 免费挂载）。升级 appkit / grpcapi 破坏 scaffold 模板时
// 同步抬这里——应用 tidy 走 MVS 取 max，地板保证新 init 的应用直接可编译。
const (
	appkitFloor  = "v0.8.0" // appkit 根 module（契约 + compose/chain + storeerr）
	bunxFloor    = "v0.5.0" // appkit/contrib/bunx（scaffold 装配 + MapError）
	grpcapiFloor = "v0.5.0"
)

// cmdInit: lynx-start init <name> —— 从 scaffold 生成应用骨架。
func cmdInit(args []string) error {
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	module := fs.String("module", "", "Go module path (default example.com/<name>)")
	registryDir := fs.String("registry", os.Getenv("LYNXSTART_REGISTRY"), "lynx-start repo directory (containing scaffold/ and registry/)")
	force := fs.Bool("force", false, "continue even if the target directory is non-empty")
	preset := fs.String("preset", "", "install a preset module combo (see lynx-start list)")
	args = reorderArgs(map[string]bool{"module": true, "registry": true, "preset": true}, args)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("usage: lynx-start init <name> [--module <path>] [--registry <dir>]")
	}
	appName := fs.Arg(0)
	if *registryDir == "" {
		*registryDir = os.Getenv("LYNXSTART_REGISTRY")
	}
	if *registryDir == "" {
		return fmt.Errorf("--registry is required (or set LYNXSTART_REGISTRY): point it at the lynx-start repo directory")
	}

	target := appName
	if _, err := os.Stat(target); err == nil && !*force {
		if entries, _ := os.ReadDir(target); len(entries) > 0 {
			return fmt.Errorf("directory %s is not empty (use --force to continue)", target)
		}
	}
	modulePath := *module
	if modulePath == "" {
		modulePath = "example.com/" + appName
	}
	resolved, err := registry.Resolve(*registryDir)
	if err != nil {
		return err
	}
	// manifest 记原始形态（URL 或本地路径）：跨机器可复现，每次操作再经
	// registry.Resolve 解析（URL → 缓存 clone 增量刷新）。
	scaffoldDir := filepath.Join(resolved, "scaffold")
	data := render.NewData(appName, modulePath)
	res, err := render.Tree(scaffoldDir, target, data,
		render.Options{Overwrite: *force, Template: true, Report: printf})
	if err != nil {
		return err
	}

	// go.mod 直接生成（骨架模板里不放：放进去会被本仓 go.work 吸进工作区）。
	// 应用装模块后只需 buf generate + go mod tidy，不再有 go mod init 一步。
	goMod := filepath.Join(target, "go.mod")
	if _, statErr := os.Stat(goMod); statErr == nil {
		fmt.Println("skip   go.mod (already exists)")
	} else {
		content := fmt.Sprintf("module %s\n\ngo %s\n\nrequire (\n\tgithub.com/lynxland/grpcapi %s\n\tgithub.com/lynxland/appkit %s\n\tgithub.com/lynxland/appkit/contrib/bunx %s\n)\n", modulePath, goDirective, grpcapiFloor, appkitFloor, bunxFloor)
		if err := os.WriteFile(goMod, []byte(content), 0o644); err != nil {
			return err
		}
		fmt.Println("write  go.mod")
	}

	m := &manifest.Manifest{
		Schema:     1,
		Name:       appName,
		ModulePath: modulePath,
		Registry:   *registryDir,
	}
	if err := m.Save(target); err != nil {
		return err
	}

	// 预置组合（I-17）：一条命令装齐一组模块（组合内顺序即安装顺序，
	// 依赖由安装器校验）。清单已保存，直接复用 cmdAdd。
	if *preset != "" {
		presets, err := registry.LoadPresets(resolved)
		if err != nil {
			return err
		}
		ps, ok := presets.Find(*preset)
		if !ok {
			return fmt.Errorf("preset %q not found in registry (see lynx-start list)", *preset)
		}
		fmt.Printf("\ninstalling preset %s: %s\n", ps.Name, strings.Join(ps.Modules, " "))
		for _, mod := range ps.Modules {
			if err := cmdAdd([]string{mod, "--registry", *registryDir, "--dir", appName}); err != nil {
				return fmt.Errorf("preset %s: %w", ps.Name, err)
			}
		}
	}

	fmt.Printf("\n✓ app skeleton ready: %s (%d files, %d skipped)\n", target, len(res.Created), len(res.Skipped))
	fmt.Printf(`Next steps:
  cd %s
  git init && git add -A && git commit -m "chore: lynx-start init"
  lynx-start add <module> --registry %s   # pick from lynx-start list; add prints the build steps
`, target, *registryDir)
	return nil
}

// cmdList: lynx-start list —— 列出 registry 模块。
func cmdList(args []string) error {
	fs := flag.NewFlagSet("list", flag.ContinueOnError)
	registryDir := fs.String("registry", os.Getenv("LYNXSTART_REGISTRY"), "lynx-start 仓库目录")
	if err := fs.Parse(args); err != nil {
		return err
	}
	idx, regDir, err := registryLoad(*registryDir)
	if err != nil {
		return err
	}
	*registryDir = regDir
	fmt.Printf("%s registry (%d modules):\n", idx.Name, len(idx.Items))
	for _, it := range idx.Items {
		manifest, err := idx.Manifest(*registryDir, it.Name)
		if err != nil {
			return err
		}
		deps := ""
		if len(manifest.ModuleDependencies) > 0 {
			var parts []string
			for _, dep := range manifest.ModuleDependencies {
				if dep.Min != "" {
					parts = append(parts, dep.Name+">="+dep.Min)
				} else {
					parts = append(parts, dep.Name)
				}
			}
			deps = "  deps: " + strings.Join(parts, ", ")
		}
		fmt.Printf("  %-16s %-8s %s%s\n", manifest.Name, manifest.Version, manifest.Description, deps)
	}
	if presets, err := registry.LoadPresets(regDir); err == nil && len(presets.Presets) > 0 {
		fmt.Println("presets:")
		for _, ps := range presets.Presets {
			fmt.Printf("  %-16s %s\n", "--preset "+ps.Name, strings.Join(ps.Modules, " "))
		}
	}
	return nil
}

func printf(format string, args ...any) { fmt.Printf(format+"\n", args...) }
