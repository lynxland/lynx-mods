package account

import (
	"github.com/lynxland/appkit/contrib/bunx/testutil"

	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestDomainPurity 契约②护栏（ADR-0009 合并后覆盖三个域子包）：domain
// 层不得 import 本模块的 app/infra/api 层（跨模块仅允许他模块的 domain
// 包；模块内跨域同理——域间经 domain 端口协作）。
func TestDomainPurity(t *testing.T) {
	fset := token.NewFileSet()
	err := filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		file, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		for _, imp := range file.Imports {
			p := strings.Trim(imp.Path.Value, `"`)
			for _, forbidden := range []string{"/internal/app/", "/internal/infra/", "/internal/api/"} {
				if strings.Contains(p, forbidden) {
					t.Errorf("%s 违反分层契约：domain 不得 import %s", filepath.Base(path), p)
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// TestDepsAtAPILayer ADR-0005 护栏：appkit 根包（Deps/Assembly 所在）只允许
// api 装配层 import——domain/app/infra 一律经子包端口（uow/events/crud/…）消费。
// 递归覆盖三个域子包与 app/infra 两层。
func TestDepsAtAPILayer(t *testing.T) {
	testutil.AssertDepsAtAPILayer(t, ".", "../../internal/app", "../../internal/infra")
}

// TestNoCrossDomainInternalImport 模块内域边界护栏（ADR-0009）：三域的
// internal/{app,infra} 不得互相 import（域间协作只经 domain 端口，与跨
// 模块同纪律——合并不意味着域边界融化）。
func TestNoCrossDomainInternalImport(t *testing.T) {
	domains := []string{"identity", "session", "access"}
	fset := token.NewFileSet()
	for _, dom := range domains {
		for _, layer := range []string{"app", "infra"} {
			dir := filepath.Join("..", "..", "internal", layer, "account", dom)
			err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
				if err != nil {
					return err
				}
				if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
					return nil
				}
				file, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
				if err != nil {
					t.Fatalf("parse %s: %v", path, err)
				}
				for _, imp := range file.Imports {
					p := strings.Trim(imp.Path.Value, `"`)
					for _, other := range domains {
						if other == dom {
							continue
						}
						if strings.Contains(p, "/internal/"+layer+"/account/"+other) {
							t.Errorf("%s 违反域边界：%s 域不得 import %s 域的 %s 层", path, dom, other, layer)
						}
					}
				}
				return nil
			})
			if err != nil {
				if os.IsNotExist(err) {
					continue
				}
				t.Fatal(err)
			}
		}
	}
}
