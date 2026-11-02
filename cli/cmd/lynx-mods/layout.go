package main

// 安装布局（ADR-0006）的唯一事实源：模块源码树装入应用 modules/<name>/
// 专用区，{domain,internal/{app,api,infra}} 下的尾段模块名目录拍平。
//
//	<app>/modules/<name>/
//	├── module.go            # 桥接包（bridgeModule 生成；装配入口唯一暴露面）
//	├── domain/…             # ← 模块仓 domain/<name>/
//	└── internal/{app,api,infra}/…   # ← 模块仓 internal/{app,api,infra}/<name>/
//（proto 源与 genproto 生成物均集中应用根 proto/<name>/ 与 genproto/<name>/）
//
// proto 契约源集中应用根 proto/<name>/v1/；基线/备份是 ~/.lynx-mods 下
// 按 <appModulePath>/<module>@<commit> 寻址的可重建缓存（lock+manifest
// 才随应用提交）。本文件被 add/update/remove/doctor/generate 共用。

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// relocateModule 把模块源码相对路径映射为应用目标路径（尾段拍平）。
// 未知前缀报错（fail-closed：布局约定外的文件不应静默落入应用根）。
func relocateModule(name, rel string) (string, error) {
	if rest, ok := strings.CutPrefix(rel, "domain/"+name+"/"); ok {
		return "modules/" + name + "/domain/" + rest, nil
	}
	for _, layer := range []string{"app", "api", "infra"} {
		if rest, ok := strings.CutPrefix(rel, "internal/"+layer+"/"+name+"/"); ok {
			return "modules/" + name + "/internal/" + layer + "/" + rest, nil
		}
	}
	if rel == "domain/"+name || strings.HasPrefix(rel, "proto/") {
		return rel, nil
	}
	return "", fmt.Errorf("module %s has a file outside the layout contract (%s): only domain/<name>/**, internal/{app,api,infra}/<name>/** and proto/** are installable", name, rel)
}

// canonicalModulesPrefixes 是 registry 模块规范路径的公共前缀集合（残留扫描用）。
// 多前缀兼容（ADR-0006）：新 canonical 是 lynxland/lynx-mods/modules/（GitHub
// 机构更名 lynx-go → lynxland 后）；历史 tag 仍是 lynx-go/lynx-mods/modules/，
// 更早的（lynx-mods 时代原样搬入）是 lynx-go/lynx-start/modules/——各系都
// 必须被改写与残留扫描覆盖。首项为新 canonical。
var canonicalModulesPrefixes = []string{
	"github.com/lynxland/lynx-mods/modules/",
	"github.com/lynx-go/lynx-mods/modules/",
	"github.com/lynx-go/lynx-start/modules/",
}

// canonicalBothPrefixes 返回 canonical 在各前缀系下的形态（canonical 本身
// 优先；仅当命中已知前缀系时给出其余各系）。
func canonicalBothPrefixes(canonical string) []string {
	for _, p := range canonicalModulesPrefixes {
		rest, ok := strings.CutPrefix(canonical, p)
		if !ok {
			continue
		}
		out := []string{canonical}
		for _, q := range canonicalModulesPrefixes {
			if q != p {
				out = append(out, q+rest)
			}
		}
		return out
	}
	return []string{canonical}
}

// moduleRewriter 构造逐依赖的结构化导入改写（add/update 共用）。
// canonicals: 已装模块名 → 规范 module path（本模块 + 声明依赖）。
//
// 规则（先长后短，顺序敏感）：
//
//	C(d)/genproto/                 → <app>/genproto/            （生成物归应用根）
//	C(d)/domain/<d>                → <app>/modules/<d>/domain
//	C(d)/internal/{app,api,infra}/<d> → <app>/modules/<d>/internal/{app,api,infra}
//
// 通吃 .go 的 import、genproto 引用 与 .proto 的 go_package。genproto 是
// 唯一不走 modules/<d>/ 的特例——也是全部约定里最稳定的一条（目录名死
// 约定），buf 直接输出应用根、零后处理（ADR-0006 修订）。
func moduleRewriter(appModulePath string, canonicals map[string]string) func(rel string, content []byte) []byte {
	type rule struct{ from, to []byte }
	names := make([]string, 0, len(canonicals))
	for name := range canonicals {
		names = append(names, name)
	}
	sort.Strings(names)
	var rules []rule
	for _, name := range names {
		// 双前缀兼容（ADR-0006）：canonical 可能是新系（lynx-mods/modules/，
		// 工作区 item）也可能是旧系（lynx-start/modules/，历史 tag item）。
		// 混代组合（旧 tag 模块源码引用旧系 sibling 路径 + sibling 解析自
		// 工作区新系 item）下，单一 canonical 会漏改——对每个 canonical
		// 同时生成两系规则，改写器对前缀系别完全无感。
		for _, c := range canonicalBothPrefixes(canonicals[name]) {
			rules = append(rules,
				rule{[]byte(c + "/genproto/"), []byte(appModulePath + "/genproto/")},
				rule{[]byte(c + "/domain/" + name), []byte(appModulePath + "/modules/" + name + "/domain")},
				rule{[]byte(c + "/internal/app/" + name), []byte(appModulePath + "/modules/" + name + "/internal/app")},
				rule{[]byte(c + "/internal/api/" + name), []byte(appModulePath + "/modules/" + name + "/internal/api")},
				rule{[]byte(c + "/internal/infra/" + name), []byte(appModulePath + "/modules/" + name + "/internal/infra")},
			)
		}
	}
	return func(rel string, content []byte) []byte {
		if !strings.HasSuffix(rel, ".go") && !strings.HasSuffix(rel, ".proto") {
			return content
		}
		for _, r := range rules {
			content = bytes.ReplaceAll(content, r.from, r.to)
		}
		return content
	}
}

// canonicalResidue 在改写后的产物里扫描残留的规范模块路径（带引号的
// import / go_package 形态）。规则漏配时 fail-closed，防止静默产坏代码。
func canonicalResidue(rel string, content []byte) (string, bool) {
	if !strings.HasSuffix(rel, ".go") && !strings.HasSuffix(rel, ".proto") {
		return "", false
	}
	for _, prefix := range canonicalModulesPrefixes {
		if i := bytes.Index(content, []byte(`"`+prefix)); i >= 0 {
			end := bytes.IndexByte(content[i+1:], '"')
			if end < 0 {
				end = 40
			}
			if end > 60 {
				end = 60
			}
			return string(content[i+1 : i+1+end]), true
		}
	}
	return "", false
}

// rewriteGuardTest 修正 domain guard_test 的 AssertDepsAtAPILayer 相对路径：
// 仓库侧 guard_test 位于 domain/<m>/（../../internal/app），拍平后位于
// modules/<m>/domain/（../internal/app）——深度差一层。guard_test 是本仓
// 模板化文件，替换确定性命中；未命中（未来模板变更）会在应用内测试失败，
// e2e 立即暴露。
func rewriteGuardTest(name, rel string, content []byte) []byte {
	if rel != "domain/"+name+"/guard_test.go" {
		return content
	}
	content = bytes.ReplaceAll(content,
		[]byte(`"../../internal/app", "../../internal/infra"`),
		[]byte(`"../internal/app", "../internal/infra"`))
	return content
}

// bridgeModule 生成 modules/<name>/module.go 桥接包内容：Go internal 可见性
// 下模块内 internal 只能被 modules/<name>/ 树内 import，应用代码（modules.go
// 登记点）经此转发面拿到 Module/Assemble。显式别名消除「路径尾段 api 与
// 包名 <name> 不一致」的歧义。
func bridgeModule(appModulePath, name string) []byte {
	var b strings.Builder
	fmt.Fprintf(&b, `// lynx-mods:bridge —— 由 add 生成（remove 删除）。转发装配入口：
// modules/<name>/internal 受 Go 可见性保护，应用唯一入口是本包。
// 改模块装配请改 internal/api；本文件属注入物，不参与 update 合并。
package %s

import (
	"github.com/lynxland/appkit"

	api "%s/modules/%s/internal/api"
)

// Module 转发模块静态装配描述。
func Module() appkit.Module { return api.Module() }

// Assemble 转发模块动态装配。
func Assemble(deps appkit.Deps) (appkit.Assembly, error) { return api.Assemble(deps) }
`, name, appModulePath, name)
	return []byte(b.String())
}

// bridgeRelPath 是桥接包在应用内的相对路径（remove 与 doctor 用）。
func bridgeRelPath(name string) string { return "modules/" + name + "/module.go" }

// ---- ~/.lynx-mods 用户缓存 ----

// userCacheRoot 返回 ~/.lynx-mods（基线/备份缓存的根，与应用仓库分离，
// 不进版本库；内容可由 registry + lock 确定性重建）。
func userCacheRoot() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("locate user home for the lynx-mods cache: %w", err)
	}
	return filepath.Join(home, ".lynx-mods"), nil
}

// baselineCacheDir 返回基线缓存目录：~/.lynx-mods/baselines/<appModulePath>/<m>@<commit12>。
// appModulePath 作应用维度隔离（基线含改写后的导入路径，per-app 不同）；
// commit 短化仅用于目录名，内容寻址不变。
func baselineCacheDir(appModulePath, module, commit string) string {
	root, err := userCacheRoot()
	if err != nil {
		// UserHomeDir 在常规环境不会失败；保守回退到应用内临时态由调用方兜底。
		return filepath.Join(os.TempDir(), "lynx-mods-baseline", appModulePath, module+"@"+shortHash(commit))
	}
	return filepath.Join(root, "baselines", appModulePath, module+"@"+shortHash(commit))
}

// backupCacheDir 返回 update 冲突覆盖前副本目录（per-app per-module）。
func backupCacheDir(appModulePath, module string) string {
	root, err := userCacheRoot()
	if err != nil {
		return filepath.Join(os.TempDir(), "lynx-mods-backup", appModulePath, module)
	}
	return filepath.Join(root, "backups", appModulePath, module)
}

// removeCacheDirs 删除模块的用户缓存（remove 用；commit 取自 lock 条目，
// 无条目时按 <module>@ 前缀清扫全部版本）。
func removeCacheDirs(appModulePath, module, commit string) error {
	root, err := userCacheRoot()
	if err != nil {
		return err
	}
	if err := os.RemoveAll(backupCacheDir(appModulePath, module)); err != nil {
		return err
	}
	if commit != "" && commit != "unknown" {
		return os.RemoveAll(baselineCacheDir(appModulePath, module, commit))
	}
	// 无 commit（旧锁/无锁）：按前缀清扫该模块全部版本目录。
	baseRoot := filepath.Join(root, "baselines", appModulePath)
	entries, err := os.ReadDir(baseRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), module+"@") {
			if err := os.RemoveAll(filepath.Join(baseRoot, e.Name())); err != nil {
				return err
			}
		}
	}
	return nil
}

// legacyLayoutInstalled 检测旧合并布局残留（manifest 有 m 且应用存在
// domain/<m>/ 或 internal/api/<m>/）——升级 CLI 后的旧应用需重装。
func legacyLayoutInstalled(appDir string, mods []string) []string {
	var legacy []string
	for _, mod := range mods {
		domain := filepath.Join(appDir, "domain", mod)
		api := filepath.Join(appDir, "internal", "api", mod)
		if _, err := os.Stat(domain); err == nil {
			legacy = append(legacy, mod)
			continue
		}
		if _, err := os.Stat(api); err == nil {
			legacy = append(legacy, mod)
		}
	}
	return legacy
}
