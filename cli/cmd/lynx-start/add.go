package main

import (
	"crypto/rand"
	"encoding/hex"
	"flag"
	"fmt"
	"go/format"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/lynxland/lynx-start/cli/internal/lock"
	"github.com/lynxland/lynx-start/cli/internal/manifest"
	"github.com/lynxland/lynx-start/cli/internal/registry"
	"github.com/lynxland/lynx-start/cli/internal/render"
)

// 注入锚点（与 scaffold/internal/modules/modules.go、configs/config.yaml 模板一致）。
const (
	importAnchor = "// lynx-start:module-import（lynx-start add 注入锚点，勿删）"
	entryAnchor  = "// lynx-start:module-entry（lynx-start add 注入锚点，勿删）"
	configAnchor = "# lynx-start:config-fragment（lynx-start add 追加模块配置段的锚点，勿删）"
)

// cmdAdd: lynx-start add <module> —— 安装模块源码到当前应用。
func cmdAdd(args []string) error {
	fs := flag.NewFlagSet("add", flag.ContinueOnError)
	registryDir := fs.String("registry", os.Getenv("LYNXSTART_REGISTRY"), "lynx-start repo directory")
	overwrite := fs.Bool("o", false, "overwrite existing files")
	overwriteLong := fs.Bool("overwrite", false, "overwrite existing files")
	dryRun := fs.Bool("dry-run", false, "preview without writing")
	dir := fs.String("dir", ".", "app directory")
	args = reorderArgs(map[string]bool{"registry": true, "dir": true}, args)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("usage: lynx-start add <module>[@<version>] [-o] [--dry-run] [--registry <dir>]")
	}
	// name@version：无版本取 registry 工作区最新；有版本从 git tag
	// modules/<name>/v<version> 提取（可复现安装）。
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
	// go.mod 必须先在（init 生成；手建应用需先 go mod init）——否则后面
	// go mod tidy 报「cannot find main module」，与 genproto 缺失的报错
	// 一样难以归因，这里提前给出可行动的错误。
	if _, statErr := os.Stat(filepath.Join(appDir, "go.mod")); statErr != nil {
		return fmt.Errorf("app is missing go.mod: lynx-start init generates it; for a hand-created app run go mod init %s first", m.ModulePath)
	}
	if *registryDir == "" {
		*registryDir = m.Registry // 清单里记的仓库目录兜底
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

	// 模块依赖（domain 级 DAG）必须已装，且满足声明的最低版本
	//（module.json 的 [{"identity": ">=0.1.1"}] 形态）——把启动期 fail-closed
	// 前移到安装期。
	for _, dep := range mod.ModuleDependencies {
		if !m.Has(dep.Name) {
			return fmt.Errorf("module %s depends on %s: run lynx-start add %s first", name, dep.Name, dep.Name)
		}
		if installed, ok := m.Get(dep.Name); ok && !dep.Meets(installed.Version) {
			return fmt.Errorf("module %s requires %s >=%s (installed %s): run lynx-start update %s first", name, dep.Name, dep.Min, installed.Version, dep.Name)
		}
	}
	// kernel 地板：模块声明的 kernelConstraint 对照应用 go.mod 的 require。
	if mod.KernelConstraint != "" {
		min, err := registry.ParseConstraint(mod.KernelConstraint)
		if err != nil {
			return fmt.Errorf("%s module.json kernelConstraint: %w", name, err)
		}
		have, err := appkitVersion(appDir)
		if err != nil {
			return err
		}
		if registry.CompareVersions(have, min) < 0 {
			return fmt.Errorf("module %s requires appkit >=%s (app go.mod has %s): run go get github.com/lynxland/appkit@v%s", name, min, have, min)
		}
	}
	already := m.Has(name)

	data := render.NewData(m.Name, m.ModulePath)
	overwriteAny := *overwrite || *overwriteLong

	// 1) 渲染模块源码树：模块在本仓是真实 Go module，安装时排除工程文件
	//    （go.mod/module.json/genproto/buf 配置）。布局重定位（ADR-0006）：
	//    domain/<m>/** 与 internal/{app,api,infra}/<m>/** 装入应用专用区
	//    modules/<m>/ 并拍平尾段；proto/** 集中应用根。.go/.proto 中的规范
	//    module path 按五条结构化规则改写为应用路径（本模块与全部声明依赖
	//    ——模块可能 import 依赖模块的 domain 包，依赖方必已安装）。
	canonicals := map[string]string{name: mod.CanonicalModulePath()}
	for _, dep := range mod.ModuleDependencies {
		depMod, err := idx.Manifest(*registryDir, dep.Name)
		if err != nil {
			return err
		}
		canonicals[dep.Name] = depMod.CanonicalModulePath()
	}
	rewrite := moduleRewriter(m.ModulePath, canonicals)
	res, err := render.Tree(mod.Dir, appDir, data, render.Options{
		Overwrite: overwriteAny,
		DryRun:    *dryRun,
		Skip:      skipModuleFile,
		Relocate:  func(rel string) (string, error) { return relocateModule(name, rel) },
		Rewrite: func(rel string, content []byte) []byte {
			return rewriteGuardTest(name, rel, rewrite(rel, content))
		},
		Report: printf,
	})
	if err != nil {
		return err
	}
	// fail-closed 兜底：改写规则漏配会静默产出坏 import——扫描产物中
	// 残留的规范模块路径（带引号的 import/go_package 形态）。
	if !*dryRun {
		for _, rel := range res.Created {
			content, rerr := os.ReadFile(filepath.Join(appDir, filepath.FromSlash(rel)))
			if rerr != nil {
				continue
			}
			if resi, bad := canonicalResidue(rel, content); bad {
				return fmt.Errorf("%s: canonical module path %q survived the rewrite (layout rule gap) — report this bug", rel, resi)
			}
		}
	}
	if *dryRun {
		fmt.Printf("\n(dry-run) would write %d files, skip %d\n", len(res.Created), len(res.Skipped))
		return nil
	}

	// 2) 桥接包（modules/<m>/module.go）：应用代码经它取 Module/Assemble
	//    （Go internal 可见性）。注入物，不参与 update 合并；无条件刷新。
	bridgePath := filepath.Join(appDir, filepath.FromSlash(bridgeRelPath(name)))
	if err := os.MkdirAll(filepath.Dir(bridgePath), 0o755); err != nil {
		return err
	}
	bridge := bridgeModule(m.ModulePath, name)
	if formatted, ferr := format.Source(bridge); ferr == nil {
		bridge = formatted
	}
	if err := os.WriteFile(bridgePath, bridge, 0o644); err != nil {
		return fmt.Errorf("write bridge module.go: %w", err)
	}

	// 3) modules.go 注入（幂等）。
	if !already {
		if err := injectModulesEntry(appDir, name, m.ModulePath); err != nil {
			return err
		}
		// 4) config.yaml 追加模块配置段（幂等）。
		if err := appendEnvExample(appDir, m, mod); err != nil {
			fmt.Printf("warn: env example skipped: %v\n", err)
		}
		if err := appendConfigFragment(appDir, name, mod.ConfigFragment); err != nil {
			return err
		}
	}

	// 5) 基线快照 + 版本 pin：无论首装还是重装（-o），凡本次被 upstream
	//    形态写入的文件都进基线——update 三方合并的 base 由此而来。
	//    基线缓存按 ~/.lynx-start/baselines/<appModulePath>/<m>@<commit>
	//    寻址（commit 先解析，见 ADR-0006：多版本并存、可重建、不进仓库）。
	files := append([]string{}, res.Created...)
	files = append(files, res.Skipped...)
	sort.Strings(files)
	source, commit := lock.ResolveCommit(*registryDir, mod.Name, version)
	baseDir := baselineCacheDir(m.ModulePath, mod.Name, commit)
	if err := syncBaseline(appDir, baseDir, res); err != nil {
		return fmt.Errorf("snapshot install baseline: %w", err)
	}
	// 供应链锁：版本溯源（tag/worktree + commit）与基线 sha256（update 合并
	// 锚的防篡改）。
	lk, err := lock.Load(appDir)
	if err != nil {
		return err
	}
	if err := lk.Record(baseDir, mod.Name, mod.Version, source, commit); err != nil {
		return err
	}
	if err := lk.Save(appDir); err != nil {
		return err
	}
	m.Set(manifest.Module{Name: mod.Name, Version: mod.Version, Files: files})
	if err := m.Save(appDir); err != nil {
		return err
	}

	fmt.Printf("\n✓ module %s@%s installed (%d written, %d skipped)\n", mod.Name, mod.Version, len(res.Created), len(res.Skipped))
	if len(res.Skipped) > 0 && !overwriteAny {
		fmt.Println("  note: existing files were skipped (local-modification guard); use -o to overwrite")
	}
	applied := applyGoDependencies(appDir, mod)
	fmt.Printf(`Next steps (order matters: go mod tidy fails before genproto exists):
  mise install && mise trust   # toolchain (go / buf; skip if already set up)
  mise run generate            # buf generates genproto/
  go mod tidy%s
  mise run db:up && mise run db:migrate
  mise run dev                 # gRPC :9060 / HTTP :8080
`, applied)
	return nil
}

// applyGoDependencies 安装期自动应用 module.json 的 goDependencies（go get
// 钉版）：模块源码用到新电池版本而应用地板未抬时，编译不再失败。
// 首选 exec go get；失败（离线/mise 未 trust 等）退回纯文本改写 go.mod
// 的 require 行（go.sum 由后续 go mod tidy 收口）——始终离线可用
// （ADR-0006 分发器改进，2026-11-01：storeerr/occ 两轮试点均被地板绊倒后落地）。
func applyGoDependencies(appDir string, mod *registry.ModuleManifest) string {
	hint := goGetHint(mod)
	if len(mod.GoDependencies) == 0 {
		return ""
	}
	if _, err := os.Stat(filepath.Join(appDir, "go.mod")); err != nil {
		return hint
	}
	if applyByGoGet(appDir, mod.GoDependencies) {
		fmt.Printf("  deps applied: go get %s\n", strings.Join(mod.GoDependencies, " "))
		return ""
	}
	if applyByGoModEdit(appDir, mod.GoDependencies) {
		fmt.Printf("  deps applied (go.mod edit): %s\n", strings.Join(mod.GoDependencies, " "))
		return ""
	}
	fmt.Printf("  note: could not apply dependencies, run manually:%s\n", hint)
	return hint
}

func applyByGoGet(appDir string, deps []string) bool {
	// 只升不降：go get 显式版本会降级（后装模块携带旧 goDependencies
	// 时会拖垮先装模块已抬的版本）——先过滤出需要抬升的条目。
	wanted := filterUpgrades(readGoModRequires(appDir), deps)
	if len(wanted) == 0 {
		return true
	}
	for _, dep := range wanted {
		cmd := exec.Command("go", "get", dep)
		cmd.Dir = appDir
		if _, err := cmd.CombinedOutput(); err != nil {
			return false
		}
	}
	return true
}

// readGoModRequires 读 go.mod 的 require 版本表（模块路径 → 去 v 前缀版本）。
func readGoModRequires(appDir string) map[string]string {
	out := map[string]string{}
	raw, err := os.ReadFile(filepath.Join(appDir, "go.mod"))
	if err != nil {
		return out
	}
	for _, ln := range strings.Split(string(raw), "\n") {
		f := strings.Fields(ln)
		if len(f) == 2 && strings.HasPrefix(f[0], "github.com/") && strings.HasPrefix(f[1], "v") {
			out[f[0]] = strings.TrimPrefix(f[1], "v")
		}
	}
	return out
}

// filterUpgrades 过滤出需要抬升的依赖（当前缺失或版本更旧）。
func filterUpgrades(current map[string]string, deps []string) []string {
	var wanted []string
	for _, dep := range deps {
		modPath, version, ok := strings.Cut(dep, "@")
		if !ok || !strings.HasPrefix(version, "v") {
			continue
		}
		have, exists := current[modPath]
		if !exists || registry.CompareVersions(strings.TrimPrefix(version, "v"), have) > 0 {
			wanted = append(wanted, dep)
		}
	}
	return wanted
}

// applyByGoModEdit 以纯文本方式抬升 go.mod 的 require 版本（仅升不降，
// MVS 语义）。兜底路径：不依赖 go 命令可用（mise shim 在未 trust 目录
// 会拒绝执行）。只升不插：require 里不存在的模块路径（含旧 tag 携带的
// 历史路径如 lynx-start/kernel）一律跳过——插入未知模块会毒化后续 tidy；
// 新模块路径的依赖应用交给 exec go get 路径或安装提示。
func applyByGoModEdit(appDir string, deps []string) bool {
	path := filepath.Join(appDir, "go.mod")
	raw, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	lines := strings.Split(string(raw), "\n")
	changed := false
	for _, dep := range deps {
		modPath, version, ok := strings.Cut(dep, "@")
		if !ok || !strings.HasPrefix(version, "v") {
			continue
		}
		re := regexp.MustCompile(`^(\t` + regexp.QuoteMeta(modPath) + ` )v\S+$`)
		for i, ln := range lines {
			if m := re.FindStringSubmatch(ln); m != nil {
				old := strings.Fields(ln)[1]
				if registry.CompareVersions(strings.TrimPrefix(version, "v"), strings.TrimPrefix(old, "v")) > 0 {
					lines[i] = m[1] + version
					changed = true
				}
				break
			}
		}
	}
	if !changed {
		return true // 已满足（无需改动也算成功）
	}
	return os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0o644) == nil
}

// skipModuleFile 排除模块目录里的工程/生成/元文件（add 与 update 共用）：
// go.mod/go.sum 是模块自身模块管理的产物（应用的由 go mod tidy 生成——
// 尤其 go.sum，三方合并它只会产生垃圾冲突）；genproto 与 buf 配置由
// 应用的 buf 管线生成；module.json 是 registry 元数据；README.md 是
// registry 元文档（面向浏览本仓的读者，不入应用树）。
func skipModuleFile(rel string) bool {
	return rel == "module.json" || rel == "go.mod" || rel == "go.sum" ||
		strings.HasPrefix(rel, "genproto/") ||
		rel == "buf.yaml" || rel == "buf.gen.yaml" || rel == "buf.lock" ||
		rel == "README.md"
}

func goGetHint(mod *registry.ModuleManifest) string {
	if len(mod.GoDependencies) == 0 {
		return ""
	}
	return "\n  go get " + strings.Join(mod.GoDependencies, " ")
}

// injectModulesEntry 在 modules/modules.go 的锚点处注入 import 与装配登记
// （存在即跳过，幂等；import 指向桥接包 <app>/modules/<name>）。
func injectModulesEntry(appDir, name, modulePath string) error {
	path := filepath.Join(appDir, "modules", "modules.go")
	raw, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read modules.go (generated by lynx-start init): %w", err)
	}
	src := string(raw)
	if strings.Contains(src, name+".Module()") {
		return nil // 已登记
	}
	entry := fmt.Sprintf("{\n\t\t\tModule:   %s.Module(),\n\t\t\tAssemble: %s.Assemble,\n\t\t},\n\t\t", name, name)
	if !strings.Contains(src, entryAnchor) {
		return fmt.Errorf("modules.go is missing entry anchor %q (deleted? diff against the scaffold template)", entryAnchor)
	}
	src = strings.Replace(src, entryAnchor, entry+entryAnchor, 1)

	imp := fmt.Sprintf("%s %q\n", name, modulePath+"/modules/"+name)
	if !strings.Contains(src, importAnchor) {
		return fmt.Errorf("modules.go is missing import anchor %q", importAnchor)
	}
	if !strings.Contains(src, fmt.Sprintf("%q", modulePath+"/modules/"+name)) {
		src = strings.Replace(src, importAnchor, imp+importAnchor, 1)
	}
	formatted, ferr := format.Source([]byte(src))
	if ferr != nil {
		return fmt.Errorf("gofmt modules.go after injection: %w", ferr)
	}
	return os.WriteFile(path, formatted, 0o644)
}

// appendConfigFragment 在 configs/config.yaml 锚点后追加模块配置段（幂等）。
func appendConfigFragment(appDir, name, fragment string) error {
	path := filepath.Join(appDir, "configs", "config.yaml")
	raw, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read config.yaml: %w", err)
	}
	src := string(raw)
	marker := fmt.Sprintf("# --- module: %s ---", name)
	if strings.Contains(src, marker) {
		return nil
	}
	block := "\n" + marker + "\n"
	if fragment != "" {
		block += fragment
		if !strings.HasSuffix(fragment, "\n") {
			block += "\n"
		}
	}
	if !strings.Contains(src, configAnchor) {
		return fmt.Errorf("config.yaml is missing config anchor %q", configAnchor)
	}
	src = strings.Replace(src, configAnchor, configAnchor+"\n"+strings.TrimPrefix(block, "\n"), 1)
	return os.WriteFile(path, []byte(src), 0o644)
}

// registryLoad 解析 registry 参数（git URL → 缓存 clone，本地路径原样）并
// 加载索引；返回已解析的本地目录（init/add/update/remove/doctor/list 复用，
// 后续 Manifest/ManifestAt/scaffold 路径一律用它）。
func registryLoad(dir string) (*registry.Index, string, error) {
	resolved, err := registry.Resolve(dir)
	if err != nil {
		return nil, "", err
	}
	idx, err := registry.Load(resolved)
	return idx, resolved, err
}

// appkitVersion 读应用 go.mod 里 appkit 根 module 的 require 版本（缺失
// 返回 "0.0.0"——约束校验会给出可行动报错）。兼容读取 lynx-start 时代
// 旧应用的 require 行（读取兼容，非生成兼容；appkit v0.7.0 起，ADR-0010）。
func appkitVersion(appDir string) (string, error) {
	raw, err := os.ReadFile(filepath.Join(appDir, "go.mod"))
	if err != nil {
		return "", fmt.Errorf("read app go.mod: %w", err)
	}
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		for _, prefix := range []string{"github.com/lynxland/appkit ", "github.com/lynx-go/lynx-start "} {
			if rest, ok := strings.CutPrefix(line, prefix); ok {
				v := strings.TrimSpace(strings.Split(rest, " ")[0])
				return strings.TrimPrefix(v, "v"), nil
			}
		}
	}
	return "0.0.0", nil
}

// appendEnvExample 把模块声明的 env 变量追加进 .env.example（幂等；Secret
// 变量生成随机占位——example 文件不入真值，生产由应用自行更换）。变量名
// 带应用前缀（与 config 环境覆盖约定一致：identity.secret → <P>_IDENTITY_SECRET）。
func appendEnvExample(appDir string, m *manifest.Manifest, mod *registry.ModuleManifest) error {
	if len(mod.Env) == 0 {
		return nil
	}
	prefix := strings.ToUpper(strings.ReplaceAll(m.Name, "-", "_"))
	path := filepath.Join(appDir, ".env.example")
	src := ""
	if raw, err := os.ReadFile(path); err == nil {
		src = string(raw)
	}
	var block strings.Builder
	for _, e := range mod.Env {
		name := prefix + "_" + e.Key
		if strings.Contains(src, name+"=") {
			continue
		}
		value := ""
		if e.Secret {
			buf := make([]byte, 16)
			if _, err := rand.Read(buf); err != nil {
				return fmt.Errorf("generate %s: %w", name, err)
			}
			value = hex.EncodeToString(buf)
		}
		desc := e.Description
		if e.Secret {
			desc += " (secret — rotate for production)"
		}
		fmt.Fprintf(&block, "\n# module %s: %s\n%s=%s\n", mod.Name, desc, name, value)
	}
	if block.Len() == 0 {
		return nil
	}
	if src != "" && !strings.HasSuffix(src, "\n") {
		src += "\n"
	}
	return os.WriteFile(path, []byte(src+block.String()), 0o644)
}
