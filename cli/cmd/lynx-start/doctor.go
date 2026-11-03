package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/lynxland/lynx-start/cli/internal/lock"
	"github.com/lynxland/lynx-start/cli/internal/manifest"
	"github.com/lynxland/lynx-start/cli/internal/registry"
)

// cmdDoctor: lynx-start doctor —— 应用与安装清单的一致性体检（只读）：
// modules.go 双向对账、config 段存在性、基线/文件完整性、模块 DAG。
// 任何 ✗ 项退出码非零（CI/脚本可用）。
func cmdDoctor(args []string) error {
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	registryDir := fs.String("registry", os.Getenv("LYNXSTART_REGISTRY"), "lynx-start repo directory")
	dir := fs.String("dir", ".", "app directory")
	format := fs.String("format", "text", "output format: text | github (workflow command annotations for CI)")
	args = reorderArgs(map[string]bool{"registry": true, "dir": true}, args)
	if err := fs.Parse(args); err != nil {
		return err
	}
	appDir, err := filepath.Abs(*dir)
	if err != nil {
		return err
	}
	m, err := manifest.Load(appDir)
	if err != nil {
		return err
	}
	if *registryDir == "" {
		*registryDir = m.Registry
	}
	idx, regDir, err := registryLoad(*registryDir)
	if err != nil {
		return err
	}
	*registryDir = regDir
	problems := 0
	github := *format == "github"
	check := func(ok bool, format string, args ...any) {
		if ok {
			if !github {
				fmt.Printf("  ✓ ")
				fmt.Printf(format+"\n", args...)
			}
			return
		}
		problems++
		if github {
			// workflow command：PR 界面行内注解；退出码稳定（1=有问题）。
			fmt.Printf("::error title=lynx-start doctor::%s\n", fmt.Sprintf(format, args...))
			return
		}
		fmt.Printf("  ✗ ")
		fmt.Printf(format+"\n", args...)
	}

	fmt.Printf("lynx-start doctor: %s (%d modules)\n", appDir, len(m.Modules))

	// 0) 旧布局检测（ADR-0006 之前的合并布局）：升级 CLI 后的旧应用，
	//    给出重装指引而不是一连串误导性的文件缺失。
	for _, mod := range legacyLayoutInstalled(appDir, moduleNames(m)) {
		check(false, "%s is installed in the legacy layout (domain/%s or internal/api/%s present): re-install with `lynx-start add %s -o` to move it under modules/%s (commit local changes first; see docs/adr/0006)", mod, mod, mod, mod, mod)
	}

	// 1) modules.go 双向对账：登记的模块都在清单，清单的模块都已登记。
	modulesSrc := ""
	if raw, err := os.ReadFile(filepath.Join(appDir, "modules", "modules.go")); err == nil {
		modulesSrc = string(raw)
	} else {
		check(false, "read modules/modules.go: %v", err)
	}
	if modulesSrc != "" {
		for _, mod := range m.Modules {
			check(strings.Contains(modulesSrc, mod.Name+".Module()"),
				"modules.go registers %s", mod.Name)
		}
		for _, name := range registeredModuleNames(modulesSrc) {
			_, ok := m.Get(name)
			check(ok, "manifest lists %s (registered in modules.go but missing from lynx-start.json)", name)
		}
	}

	// 2) 每个已装模块：依赖已装、config 段在、Files 在应用、基线完整。
	for _, mod := range m.Modules {
		// 按已装版本解析 item（tag 冻结形态）：工作区 item 可能声明了新版才有的
		// env/依赖约束，拿它校验旧安装会误报。
		item, cleanupItem, err := idx.ItemAt(*registryDir, mod.Name, mod.Version)
		if err != nil {
			check(false, "registry knows %s@%s: %v", mod.Name, mod.Version, err)
			continue
		}
		defer cleanupItem()
		for _, dep := range item.ModuleDependencies {
			installedDep, has := m.Get(dep.Name)
			check(has, "%s dependency %s installed", mod.Name, dep.Name)
			if has && dep.Min != "" {
				check(dep.Meets(installedDep.Version), "%s dependency %s >=%s (installed %s)", mod.Name, dep.Name, dep.Min, installedDep.Version)
			}
		}
		if item.KernelConstraint != "" {
			if min, err := registry.ParseConstraint(item.KernelConstraint); err == nil {
				have, err := appkitVersion(appDir)
				check(err == nil && registry.CompareVersions(have, min) >= 0,
					"%s kernelConstraint >=%s satisfied (go.mod has %s)", mod.Name, min, have)
			} else {
				check(false, "%s kernelConstraint parses: %v", mod.Name, err)
			}
		}
		envExample := ""
		if raw, err := os.ReadFile(filepath.Join(appDir, ".env.example")); err == nil {
			envExample = string(raw)
		}
		envPrefix := strings.ToUpper(strings.ReplaceAll(m.Name, "-", "_"))
		for _, e := range item.Env {
			check(strings.Contains(envExample, envPrefix+"_"+e.Key+"="),
				"%s env %s declared in .env.example", mod.Name, e.Key)
		}
		if item.ConfigFragment != "" {
			raw, err := os.ReadFile(filepath.Join(appDir, "configs", "config.yaml"))
			has := err == nil && strings.Contains(string(raw), fmt.Sprintf("# --- module: %s ---", mod.Name))
			check(has, "%s config fragment present", mod.Name)
		}
		gone := 0
		for _, rel := range mod.Files {
			if _, err := os.Stat(filepath.Join(appDir, filepath.FromSlash(rel))); err != nil {
				gone++
			}
		}
		check(gone == 0, "%s files intact (%d of %d missing)", mod.Name, gone, len(mod.Files))
		if len(mod.Files) == 0 {
			check(false, "%s has no update baseline (add %s -o to rebuild)", mod.Name, mod.Name)
		}
		// 供应链锁（I-14）+ 基线缓存（ADR-0006：~/.lynx-start 下按
		// <m>@<commit> 寻址，缺失自动重建——重建失败才 ✗）。
		lk, lkErr := lock.Load(appDir)
		if lkErr != nil {
			check(false, "%s lock loads: %v", mod.Name, lkErr)
		} else if entry, ok := lk.Modules[mod.Name]; ok {
			baseDir, err := ensureBaseline(idx, *registryDir, m, lk, mod.Name)
			if err != nil {
				check(false, "%s baseline cache available: %v", mod.Name, err)
			} else {
				baseGone := 0
				for _, rel := range mod.Files {
					if _, err := os.Stat(filepath.Join(baseDir, filepath.FromSlash(rel))); err != nil {
						baseGone++
					}
				}
				check(baseGone == 0, "%s baseline intact (%d of %d missing)", mod.Name, baseGone, len(mod.Files))
				mismatch, err := entry.Mismatch(baseDir)
				check(err == nil && len(mismatch) == 0, "%s baseline matches lock (%s)", mod.Name, strings.Join(mismatch, ", "))
			}
			// tag 漂移检查只对「从 tag 安装」的条目——worktree 安装记的是当时
			// HEAD,与同名旧 tag 比对没有意义。
			if strings.HasPrefix(entry.Source, "tag ") && entry.Commit != "" {
				if cur, exists := lock.TagCommit(*registryDir, mod.Name, mod.Version); exists && cur != entry.Commit {
					check(false, "%s tag modules/%s/v%s unmoved (installed from %s, now %s)", mod.Name, mod.Name, mod.Version, shortHash(entry.Commit), shortHash(cur))
				}
			}
		} else {
			check(false, "%s lock entry present (update or add -o to create)", mod.Name)
		}
	}

	// 3) 安全公告：只提示不计数（体检不过载）；CI 门禁用 lynx-start audit。
	if adv, err := registry.LoadAdvisories(*registryDir); err == nil {
		for _, mod := range m.Modules {
			for _, a := range adv.Hits(mod.Name, mod.Version) {
				if github {
					fmt.Printf("::warning title=%s/%s::%s@%s affected — upgrade to %s (lynx-start audit gate)\n", a.Severity, a.ID, a.Module, mod.Version, a.Fixed)
				} else {
					fmt.Printf("  ⚠ %s [%s] %s@%s affected — upgrade to %s (see lynx-start audit)\n", a.ID, a.Severity, a.Module, mod.Version, a.Fixed)
				}
			}
		}
	}

	if problems == 0 {
		if github {
			fmt.Println("::notice title=lynx-start doctor::all checks passed")
		} else {
			fmt.Println("✓ all checks passed")
		}
		return nil
	}
	return fmt.Errorf("doctor found %d problem(s)", problems)
}

// registeredModuleNames 从 modules.go 源码提取 <name>.Module() 形态的模块名
// （手工追加登记的模块没有 manifest 条目时用于反向对账）。
func registeredModuleNames(src string) []string {
	re := regexp.MustCompile(`([A-Za-z][A-Za-z0-9_]*)\.Module\(\)`)
	var names []string
	seen := map[string]bool{}
	for _, match := range re.FindAllStringSubmatch(src, -1) {
		name := match[1]
		if seen[name] {
			continue
		}
		seen[name] = true
		names = append(names, name)
	}
	return names
}

// shortHash 缩短 commit hash 供诊断输出。
func shortHash(h string) string {
	if len(h) > 12 {
		return h[:12]
	}
	return h
}

// moduleNames 提取 manifest 的模块名列表。
func moduleNames(m *manifest.Manifest) []string {
	names := make([]string, 0, len(m.Modules))
	for _, mod := range m.Modules {
		names = append(names, mod.Name)
	}
	return names
}
