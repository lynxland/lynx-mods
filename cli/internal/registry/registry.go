// Package registry 读取 lynx-mods 仓库的模块仓：registry/registry.json 索引 +
// 各模块目录的 item.json 清单。版本解析走仓库 git tag（modules/<name>/v<x.y.z>，
// 与 kernel/v<x.y.z> 同款子目录前缀形式）；远端 registry 后续接入。
package registry

import (
	"archive/tar"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

// Index 是 registry/registry.json。
type Index struct {
	Name  string `json:"name"`
	Items []struct {
		Name string `json:"name"`
		Path string `json:"path"`
	} `json:"items"`
}

// Item 是模块目录里的 item.json（registry item 清单）。
type Item struct {
	Name        string `json:"name"`
	Version     string `json:"version"`
	Description string `json:"description"`
	// ModulePath 是模块源码的规范 Go module path（模块在本仓内以真实代码
	// 形态维护，自带 go.mod 与 genproto）；安装时 lynx-mods add 把 .go/.proto 中
	// 的该路径改写为应用 module path。
	ModulePath string `json:"modulePath"`
	// ModuleDependencies 声明 domain 级依赖。JSON 双形态：
	//   ["identity"]                —— 无版本约束（旧形态）
	//   [{"identity": ">=0.1.1"}]   —— 最低版本约束（安装期校验已装版本）
	ModuleDependencies Dependencies `json:"moduleDependencies"`
	// KernelConstraint 声明模块需要的 appkit 最低版本（如 ">=0.3.0"）；
	// add 时校验应用 go.mod 的 appkit require，doctor 复核。
	KernelConstraint string   `json:"kernelConstraint"`
	GoDependencies   []string `json:"goDependencies"`
	ConfigFragment   string   `json:"configFragment"`
	PublishedEvents  []string `json:"publishedEvents"`
	// Env 声明模块需要的 应用环境变量（add 时写进 .env.example；
	// Secret 为 true 生成随机占位，生产必须更换）。
	Env []EnvVar `json:"env"`
	// Dir 是模块源码模板目录（加载时填充）。
	Dir string `json:"-"`
}

// EnvVar 是 item.json 的 env 声明。
type EnvVar struct {
	Key         string `json:"key"`
	Description string `json:"description"`
	Secret      bool   `json:"secret,omitempty"`
}

// Dependency 是单个 domain 级依赖声明。
type Dependency struct {
	Name string // 依赖模块名
	Min  string // 最低版本（"0.1.1"，无约束时空串）
}

// Dependencies 兼容 JSON 数组的字符串/对象混合形态。
type Dependencies []Dependency

// UnmarshalJSON 接受 ["a", {"b": ">=0.1.0"}] 混合数组。
func (d *Dependencies) UnmarshalJSON(raw []byte) error {
	var entries []json.RawMessage
	if err := json.Unmarshal(raw, &entries); err != nil {
		return err
	}
	out := make(Dependencies, 0, len(entries))
	for _, e := range entries {
		var s string
		if err := json.Unmarshal(e, &s); err == nil {
			out = append(out, Dependency{Name: s})
			continue
		}
		var obj map[string]string
		if err := json.Unmarshal(e, &obj); err != nil {
			return fmt.Errorf(`moduleDependencies entry must be a name or {name: ">=x.y.z"}: %s`, e)
		}
		for name, constraint := range obj {
			dep := Dependency{Name: name}
			if constraint != "" {
				min, err := ParseConstraint(constraint)
				if err != nil {
					return fmt.Errorf("moduleDependencies %q: %w", name, err)
				}
				dep.Min = min
			}
			out = append(out, dep)
		}
	}
	*d = out
	return nil
}

// ParseConstraint 解析约束表达式；v1 只支持 ">=x.y.z"（含等于）。
func ParseConstraint(c string) (string, error) {
	if min, ok := strings.CutPrefix(c, ">="); ok {
		if !semverRe.MatchString(min) {
			return "", fmt.Errorf("constraint %q: want >=x.y.z", c)
		}
		return min, nil
	}
	return "", fmt.Errorf("constraint %q: only >=x.y.z is supported", c)
}

var semverRe = regexp.MustCompile(`^\d+\.\d+\.\d+$`)

// CompareVersions 比较两个 x.y.z；非法输入按 0.0.0 处理。
func CompareVersions(a, b string) int {
	parse := func(v string) [3]int {
		var out [3]int
		parts := strings.SplitN(strings.TrimPrefix(v, "v"), ".", 3)
		for i := 0; i < 3 && i < len(parts); i++ {
			n := 0
			for _, r := range parts[i] {
				if r < '0' || r > '9' {
					break
				}
				n = n*10 + int(r-'0')
			}
			out[i] = n
		}
		return out
	}
	pa, pb := parse(a), parse(b)
	for i := 0; i < 3; i++ {
		if pa[i] != pb[i] {
			if pa[i] < pb[i] {
				return -1
			}
			return 1
		}
	}
	return 0
}

// Meets 报告 installed 满足依赖的最低版本要求。
func (d Dependency) Meets(installed string) bool {
	if d.Min == "" {
		return true
	}
	return CompareVersions(installed, d.Min) >= 0
}

// CanonicalModulePath 返回模块的规范路径（缺省推导）。
func (i *Item) CanonicalModulePath() string {
	if i.ModulePath != "" {
		return i.ModulePath
	}
	return "github.com/lynx-go/lynx-mods/modules/" + i.Name
}

// Load 读 registryDir/registry/registry.json。
func Load(registryDir string) (*Index, error) {
	raw, err := os.ReadFile(filepath.Join(registryDir, "registry", "registry.json"))
	if err != nil {
		return nil, fmt.Errorf("load registry: lynx-mods init/add need --registry pointing at the lynx-mods repo directory (containing registry/registry.json): %w", err)
	}
	var idx Index
	if err := json.Unmarshal(raw, &idx); err != nil {
		return nil, err
	}
	return &idx, nil
}

// Item 按名查模块：定位 item.json 与源码目录（registry 工作区最新形态）。
func (idx *Index) Item(registryDir, name string) (*Item, error) {
	path, err := idx.modulePath(name)
	if err != nil {
		return nil, err
	}
	dir := filepath.Join(registryDir, path)
	item, err := loadItem(dir, name)
	if err != nil {
		return nil, err
	}
	item.Dir = dir
	return item, nil
}

// ItemAt 按名与可选版本查模块：version 为空等价 Item（工作区最新）；
// 非空则从 git tag modules/<name>/v<version> 提取该版本源码树。
// 返回的 cleanup 释放临时目录，调用方必须 defer（工作区形态时为 nil）。
// tag 内 item.json 的 version 必须与请求一致（防 tag 与清单漂移）。
func (idx *Index) ItemAt(registryDir, name, version string) (*Item, func(), error) {
	if version == "" {
		item, err := idx.Item(registryDir, name)
		return item, func() {}, err
	}
	path, err := idx.modulePath(name)
	if err != nil {
		return nil, nil, err
	}
	tag := fmt.Sprintf("modules/%s/v%s", name, version)
	if err := exec.Command("git", "-C", registryDir, "rev-parse", "--verify", "--quiet", tag+"^{commit}").Run(); err != nil {
		return nil, nil, fmt.Errorf("module %s@%s not found: no git tag %s in the registry repo (available: git -C %s tag -l 'modules/%s/*')",
			name, version, tag, registryDir, name)
	}
	raw, err := exec.Command("git", "-C", registryDir, "archive", "--format=tar", tag, "--", path).Output()
	if err != nil {
		return nil, nil, fmt.Errorf("git archive %s: %w", tag, err)
	}
	tmp, err := os.MkdirTemp("", "lynx-mods-registry-")
	if err != nil {
		return nil, nil, err
	}
	cleanup := func() { _ = os.RemoveAll(tmp) }
	if err := untar(tmp, bytes.NewReader(raw)); err != nil {
		cleanup()
		return nil, nil, fmt.Errorf("extract %s: %w", tag, err)
	}
	dir := filepath.Join(tmp, path)
	item, err := loadItem(dir, name)
	if err != nil {
		cleanup()
		return nil, nil, err
	}
	if item.Version != version {
		cleanup()
		return nil, nil, fmt.Errorf("tag %s carries item.json version %q (tag/manifest drift): fix the tag or the manifest", tag, item.Version)
	}
	item.Dir = dir
	return item, cleanup, nil
}

// ItemAtCommit 按名与 commit 提取该历史形态源码树（基线缓存重建用：
// worktree 安装记当时 HEAD，tag 安装记 tag 指向——统一按 commit 寻址，
// 与 tag 是否存在/移动无关）。expectedVersion 与树内 item.json 不一致
// 即拒绝（registry 历史被改写，重建不再可信）。
func (idx *Index) ItemAtCommit(registryDir, name, commit, expectedVersion string) (*Item, func(), error) {
	if commit == "" || commit == "unknown" {
		return nil, nil, fmt.Errorf("module %s: install source commit is unknown, cannot rebuild the baseline (re-install with `lynx-mods add %s -o`)", name, name)
	}
	path, err := idx.modulePath(name)
	if err != nil {
		return nil, nil, err
	}
	if err := exec.Command("git", "-C", registryDir, "rev-parse", "--verify", "--quiet", commit+"^{commit}").Run(); err != nil {
		return nil, nil, fmt.Errorf("module %s: commit %s not found in the registry repo (history rewritten?): rebuild the baseline with `lynx-mods add %s -o`", name, shortHash(commit), name)
	}
	raw, err := exec.Command("git", "-C", registryDir, "archive", "--format=tar", commit, "--", path).Output()
	if err != nil {
		return nil, nil, fmt.Errorf("git archive %s: %w", shortHash(commit), err)
	}
	tmp, err := os.MkdirTemp("", "lynx-mods-registry-")
	if err != nil {
		return nil, nil, err
	}
	cleanup := func() { _ = os.RemoveAll(tmp) }
	if err := untar(tmp, bytes.NewReader(raw)); err != nil {
		cleanup()
		return nil, nil, fmt.Errorf("extract %s: %w", shortHash(commit), err)
	}
	dir := filepath.Join(tmp, path)
	item, err := loadItem(dir, name)
	if err != nil {
		cleanup()
		return nil, nil, err
	}
	if item.Version != expectedVersion {
		cleanup()
		return nil, nil, fmt.Errorf("module %s: registry commit %s carries item.json version %q but the lock pins %s — rebuild the baseline with `lynx-mods add %s -o`", name, shortHash(commit), item.Version, expectedVersion, name)
	}
	item.Dir = dir
	return item, cleanup, nil
}

func (idx *Index) modulePath(name string) (string, error) {
	for _, it := range idx.Items {
		if it.Name == name {
			return it.Path, nil
		}
	}
	return "", fmt.Errorf("module %q not found in registry (see lynx-mods list)", name)
}

func loadItem(dir, name string) (*Item, error) {
	raw, err := os.ReadFile(filepath.Join(dir, "item.json"))
	if err != nil {
		return nil, fmt.Errorf("read item.json: %w", err)
	}
	var item Item
	if err := json.Unmarshal(raw, &item); err != nil {
		return nil, err
	}
	if item.Name == "" {
		item.Name = name
	}
	return &item, nil
}

// LoadItem 导出 loadItem 供 registry add/check 命令（贡献者工具）使用。
func LoadItem(dir, name string) (*Item, error) { return loadItem(dir, name) }

// Advisory 是 registry/advisories.json 中的一条安全公告：affectedVersions
// 精确列出受影响版本（0.x 世界不做范围解析），fixed 是修复版本。
type Advisory struct {
	ID               string   `json:"id"`
	Module           string   `json:"module"`
	AffectedVersions []string `json:"affectedVersions"`
	Fixed            string   `json:"fixed"`
	Severity         string   `json:"severity"`
	Summary          string   `json:"summary"`
}

// Advisories 是公告集合。
type Advisories struct {
	Advisories []Advisory `json:"advisories"`
}

// Preset 是 registry/presets.json 中的一组预置组合（init --preset 一次装齐）。
type Preset struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Modules     []string `json:"modules"`
}

// Presets 是预置组合集合。
type Presets struct {
	Presets []Preset `json:"presets"`
}

// LoadPresets 读 registryDir/registry/presets.json；文件不存在视为空集合。
func LoadPresets(registryDir string) (*Presets, error) {
	raw, err := os.ReadFile(filepath.Join(registryDir, "registry", "presets.json"))
	if os.IsNotExist(err) {
		return &Presets{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load presets: %w", err)
	}
	var out Presets
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("parse presets.json: %w", err)
	}
	return &out, nil
}

// Find 按名查预置组合。
func (p *Presets) Find(name string) (Preset, bool) {
	for _, ps := range p.Presets {
		if ps.Name == name {
			return ps, true
		}
	}
	return Preset{}, false
}

// LoadAdvisories 读 registryDir/registry/advisories.json；文件不存在视为
// 空集合（私有 registry 可不携带公告）。
func LoadAdvisories(registryDir string) (*Advisories, error) {
	raw, err := os.ReadFile(filepath.Join(registryDir, "registry", "advisories.json"))
	if os.IsNotExist(err) {
		return &Advisories{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load advisories: %w", err)
	}
	var out Advisories
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("parse advisories.json: %w", err)
	}
	return &out, nil
}

// Hits 返回影响指定已装版本的公告。
func (a *Advisories) Hits(module, installedVersion string) []Advisory {
	var out []Advisory
	for _, adv := range a.Advisories {
		if adv.Module != module {
			continue
		}
		for _, v := range adv.AffectedVersions {
			if v == installedVersion {
				out = append(out, adv)
				break
			}
		}
	}
	return out
}

// untar 解 git archive 生成的 tar 流（仅文件与目录，拒绝异常路径）。
// shortHash 截 12 位（错误信息紧凑）。
func shortHash(h string) string {
	if len(h) > 12 {
		return h[:12]
	}
	return h
}

func untar(dst string, r io.Reader) error {
	tr := tar.NewReader(r)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		name := filepath.Clean(hdr.Name)
		if filepath.IsAbs(name) || strings.HasPrefix(name, "..") {
			return fmt.Errorf("refusing archive entry %q", hdr.Name)
		}
		target := filepath.Join(dst, name)
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			f, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, os.FileMode(hdr.Mode))
			if err != nil {
				return err
			}
			if _, err := io.Copy(f, tr); err != nil {
				_ = f.Close()
				return err
			}
			if err := f.Close(); err != nil {
				return err
			}
		case tar.TypeXGlobalHeader, tar.TypeXHeader:
			// pax 扩展头（git archive 的元数据条目），忽略。
		default:
			return fmt.Errorf("unsupported archive entry %q (type %c)", hdr.Name, hdr.Typeflag)
		}
	}
}
