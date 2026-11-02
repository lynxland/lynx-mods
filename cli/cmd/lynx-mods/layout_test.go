package main

import (
	"strings"
	"testing"
)

// TestRelocateModule 断言布局映射（ADR-0006：装入 modules/<m>/ 并拍平尾段）。
func TestRelocateModule(t *testing.T) {
	cases := map[string]string{
		"domain/identity/user.go":                  "modules/identity/domain/user.go",
		"internal/app/identity/identity.go":        "modules/identity/internal/app/identity.go",
		"internal/api/identity/module.go":          "modules/identity/internal/api/module.go",
		"internal/infra/identity/repo.go":          "modules/identity/internal/infra/repo.go",
		"internal/infra/identity/migrations/x.sql": "modules/identity/internal/infra/migrations/x.sql",
		"proto/identity/v1/identity.proto":         "proto/identity/v1/identity.proto",
	}
	for in, want := range cases {
		got, err := relocateModule("identity", in)
		if err != nil {
			t.Fatalf("relocate %s: %v", in, err)
		}
		if got != want {
			t.Errorf("relocate(%s) = %s, want %s", in, got, want)
		}
	}
	// 布局约定外的文件 fail-closed。
	for _, bad := range []string{"README.md", "cmd/x/main.go", "domain/other/file.go"} {
		if _, err := relocateModule("identity", bad); err == nil {
			t.Errorf("relocate(%s) should fail (outside layout contract)", bad)
		}
	}
}

// TestModuleRewriter 断言五条结构化改写（含 genproto 与跨模块依赖）。
func TestModuleRewriter(t *testing.T) {
	canonicals := map[string]string{
		"session":  "github.com/lynxland/lynx-mods/modules/session",
		"identity": "github.com/lynxland/lynx-mods/modules/identity",
	}
	rw := moduleRewriter("example.com/app", canonicals)
	goSrc := `package x

import (
	"github.com/lynxland/lynx-mods/modules/session/domain/session"
	appsession "github.com/lynxland/lynx-mods/modules/session/internal/app/session"
	sessionv1 "github.com/lynxland/lynx-mods/modules/session/genproto/session/v1"
	"github.com/lynxland/lynx-mods/modules/identity/domain/identity"
)

var _ = session.X
var _ = appsession.Y
var _ = sessionv1.Z
var _ = identity.User{}
`
	got := string(rw("internal/app/session/x.go", []byte(goSrc)))
	// 尾段拍平：canonical …/domain/session → app modules/session/domain；
	// genproto 是唯一不走 modules/ 的特例（归应用根——ADR-0006 修订）。
	for _, want := range []string{
		"\"example.com/app/modules/session/domain\"",
		"appsession \"example.com/app/modules/session/internal/app\"",
		"sessionv1 \"example.com/app/genproto/session/v1\"",
		"\"example.com/app/modules/identity/domain\"",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("rewrite missing %s:\n%s", want, got)
		}
	}
	for _, prefix := range canonicalModulesPrefixes {
		if strings.Contains(got, `"`+prefix) {
			t.Errorf("canonical prefix survived (%s):\n%s", prefix, got)
		}
	}

	protoSrc := `option go_package = "github.com/lynxland/lynx-mods/modules/session/genproto/session/v1;sessionv1";`
	got = string(rw("proto/session/v1/session.proto", []byte(protoSrc)))
	if !strings.Contains(got, `"example.com/app/genproto/session/v1;sessionv1"`) {
		t.Errorf("go_package rewrite wrong: %s", got)
	}
}

// TestCanonicalResidue 残留扫描只在带引号的 import/go_package 形态命中
// （注释里裸写前缀不误报）。
func TestCanonicalResidue(t *testing.T) {
	for _, prefix := range canonicalModulesPrefixes {
		if _, bad := canonicalResidue("a.go", []byte("// see "+prefix+"foo")); bad {
			t.Errorf("bare comment mention must not trip the residue check (%s)", prefix)
		}
		if _, bad := canonicalResidue("a.go", []byte(`import "`+prefix+`foo/bar"`)); !bad {
			t.Errorf("quoted import residue must be detected (%s)", prefix)
		}
		if _, bad := canonicalResidue("a.proto", []byte(`option go_package = "`+prefix+`x/y";`)); !bad {
			t.Errorf("quoted go_package residue must be detected (%s)", prefix)
		}
	}
}

// TestDualPrefixCompat 多前缀兼容（ADR-0006）：新 canonical（lynxland/lynx-mods）、
// 历史 tag（lynx-go/lynx-mods）与更早的（lynx-go/lynx-start）各系源码都要能经
// 各自的 canonicals 正确改写——.go import 与 .proto go_package 双路径同验。
func TestDualPrefixCompat(t *testing.T) {
	for _, tc := range []struct {
		name      string
		canonical string
		importPID string
	}{
		{"modern", "github.com/lynxland/lynx-mods/modules/identity", "github.com/lynxland/lynx-mods/modules/"},
		{"legacy-lynx-go", "github.com/lynx-go/lynx-mods/modules/identity", "github.com/lynx-go/lynx-mods/modules/"},
		{"legacy-lynx-start", "github.com/lynx-go/lynx-start/modules/identity", "github.com/lynx-go/lynx-start/modules/"},
	} {
		rw := moduleRewriter("example.com/app", map[string]string{"identity": tc.canonical})
		goSrc := `import "` + tc.canonical + `/domain/identity"`
		got := string(rw("internal/api/identity/x.go", []byte(goSrc)))
		if !strings.Contains(got, `"example.com/app/modules/identity/domain"`) {
			t.Errorf("%s: .go import rewrite wrong: %s", tc.name, got)
		}
		protoSrc := `option go_package = "` + tc.canonical + `/genproto/identity/v1;identityv1";`
		got = string(rw("proto/identity/v1/identity.proto", []byte(protoSrc)))
		if !strings.Contains(got, `"example.com/app/genproto/identity/v1;identityv1"`) {
			t.Errorf("%s: .proto go_package rewrite wrong: %s", tc.name, got)
		}
		if _, bad := canonicalResidue("y.proto", rw("y.proto", []byte(protoSrc))); bad {
			t.Errorf("%s: rewritten proto must be residue-free", tc.name)
		}
		// 漏配 fail-closed：改写规则没覆盖的旧系引用必须被残留扫描逮住。
		stranger := []byte(`import "` + tc.importPID + `stranger/domain/stranger"`)
		if _, bad := canonicalResidue("z.go", stranger); !bad {
			t.Errorf("%s: unrewritten canonical import must be flagged", tc.name)
		}
	}
}

// TestBridgeModule 桥接包编译形态：转发 Module/Assemble、显式别名。
func TestBridgeModule(t *testing.T) {
	got := string(bridgeModule("example.com/app", "identity"))
	for _, want := range []string{
		"package identity",
		`"github.com/lynxland/appkit"`,
		`api "example.com/app/modules/identity/internal/api"`,
		"func Module() appkit.Module { return api.Module() }",
		"func Assemble(deps appkit.Deps) (appkit.Assembly, error) { return api.Assemble(deps) }",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("bridge missing %q:\n%s", want, got)
		}
	}
}

// TestBaselineCacheDir 缓存寻址：<appModulePath>/<m>@<commit12>。
func TestBaselineCacheDir(t *testing.T) {
	got := baselineCacheDir("example.com/app", "identity", "0123456789abcdef0123456789abcdef")
	if !strings.Contains(got, "example.com/app") || !strings.HasSuffix(got, "identity@0123456789ab") {
		t.Errorf("baselineCacheDir = %s", got)
	}
}
