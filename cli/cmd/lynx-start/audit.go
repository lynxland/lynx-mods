package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/lynxland/lynx-start/cli/internal/manifest"
	"github.com/lynxland/lynx-start/cli/internal/registry"
)

// cmdAudit: lynx-start audit —— 安全公告门禁。模块安装即断连，已装用户
// 只能靠 registry 维护的 advisories.json 得知安全修复；本命令比对应用
// 已装版本，命中即非零退出（doctor 只作 ⚠ 提示不过载体检语义，CI 门禁
// 用本命令）。
func cmdAudit(args []string) error {
	fs := flag.NewFlagSet("audit", flag.ContinueOnError)
	registryDir := fs.String("registry", os.Getenv("LYNXSTART_REGISTRY"), "lynx-start repo directory")
	dir := fs.String("dir", ".", "app directory")
	format := fs.String("format", "text", "output format: text | github")
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
	_ = idx
	adv, err := registry.LoadAdvisories(regDir)
	if err != nil {
		return err
	}

	github := *format == "github"
	hits := 0
	for _, mod := range m.Modules {
		for _, a := range adv.Hits(mod.Name, mod.Version) {
			hits++
			if github {
				fmt.Printf("::error title=%s/%s::%s@%s affected — upgrade to %s: %s\n", a.Severity, a.ID, a.Module, mod.Version, a.Fixed, a.Summary)
				continue
			}
			fmt.Printf("⚠ %s [%s] %s@%s affected — upgrade to %s\n    %s\n", a.ID, a.Severity, a.Module, mod.Version, a.Fixed, a.Summary)
		}
	}
	if hits == 0 {
		if github {
			fmt.Println("::notice title=lynx-start audit::no known advisories affect installed versions")
		} else {
			fmt.Println("✓ no known advisories affect installed versions")
		}
		return nil
	}
	if !github {
		fmt.Println("\nupgrade: git add -A && git commit -m wip && lynx-start update <module>")
	}
	return fmt.Errorf("audit: %d advisory hit(s)", hits)
}
