package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lynxland/lynx-mods/cli/internal/manifest"
)

func TestRevertModulesEntryRoundtrip(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "modules", "modules.go")
	const tmpl = `package modules

import (
	// lynx-mods:module-import（lynx-mods add 注入锚点，勿删）
)

var Installed = []appkit.InstalledModule{
	{
		Module:   demo.Module(),
		Assemble: demo.Assemble,
	},
	// lynx-mods:module-entry（lynx-mods add 注入锚点，勿删）
}
`
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(tmpl), 0o644); err != nil {
		t.Fatal(err)
	}
	// 模拟 add：注入 audit 登记（与 injectModulesEntry 同格式）。
	src := tmpl
	entry := "{\n\t\t\tModule:   audit.Module(),\n\t\t\tAssemble: audit.Assemble,\n\t\t},\n\t\t"
	src = strings.Replace(src, entryAnchor, entry+entryAnchor, 1)
	imp := "audit \"example.com/app/modules/audit\"\n"
	src = strings.Replace(src, importAnchor, imp+importAnchor, 1)
	if err := os.WriteFile(p, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := revertModulesEntry(dir, "audit", "example.com/app"); err != nil {
		t.Fatalf("revert: %v", err)
	}
	got, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(got), "audit") {
		t.Fatalf("audit still present:\n%s", got)
	}
	if !strings.Contains(string(got), entryAnchor) || !strings.Contains(string(got), importAnchor) {
		t.Fatalf("anchors must survive removal:\n%s", got)
	}
	if !strings.Contains(string(got), "demo.Module()") {
		t.Fatalf("other module registration must survive:\n%s", got)
	}
}

func TestRemoveConfigFragment(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "configs", "config.yaml")
	const tmpl = `app:
  name: x
# lynx-mods:config-fragment（lynx-mods add 追加模块配置段的锚点，勿删）
# --- module: demo ---
demo:
  x: 1

# --- module: audit ---
audit:
  y: 2

# --- module: session ---
session:
  z: 3
`
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(tmpl), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := removeConfigFragment(dir, "audit"); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(p)
	s := string(got)
	if strings.Contains(s, "audit") || strings.Contains(s, "y: 2") {
		t.Fatalf("audit fragment must be gone:\n%s", s)
	}
	for _, want := range []string{"# --- module: demo ---", "# --- module: session ---", "z: 3"} {
		if !strings.Contains(s, want) {
			t.Fatalf("must keep %q:\n%s", want, s)
		}
	}
}

func TestRegisteredModuleNames(t *testing.T) {
	src := `var Installed = []appkit.InstalledModule{
	{
		Module:   demo.Module(),
		Assemble: demo.Assemble,
	},
	{
		Module:   identity.Module(),
		Assemble: identity.Assemble,
	},
}`
	names := registeredModuleNames(src)
	if len(names) != 2 {
		t.Fatalf("names = %v", names)
	}
	set := map[string]bool{}
	for _, n := range names {
		set[n] = true
	}
	if !set["demo"] || !set["identity"] {
		t.Fatalf("names = %v", names)
	}
}

func TestManifestSetUpsert(t *testing.T) {
	m := &manifest.Manifest{}
	m.Set(manifest.Module{Name: "a", Version: "1"})
	m.Set(manifest.Module{Name: "b", Version: "1"})
	m.Set(manifest.Module{Name: "a", Version: "2"})
	if len(m.Modules) != 2 {
		t.Fatalf("upsert must not duplicate: %+v", m.Modules)
	}
	a, _ := m.Get("a")
	if a.Version != "2" {
		t.Fatalf("upsert must replace: %+v", a)
	}
}
