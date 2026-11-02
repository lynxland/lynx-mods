package notification

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lynxland/appkit/contrib/bunx/testutil"
)

// TestDomainPurity 契约②护栏：domain 层不得 import 本应用的 app/infra/api 层
// （跨模块仅允许他模块的 domain 包）。
func TestDomainPurity(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	files, err := filepath.Glob(filepath.Join(wd, "*.go"))
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, f, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("parse %s: %v", f, err)
		}
		for _, imp := range file.Imports {
			path := strings.Trim(imp.Path.Value, `"`)
			for _, forbidden := range []string{"/internal/app/", "/internal/infra/", "/internal/api/"} {
				if strings.Contains(path, forbidden) {
					t.Errorf("%s 违反分层契约：domain 不得 import %s", filepath.Base(f), path)
				}
			}
		}
	}
}

// TestDepsAtAPILayer ADR-0005 护栏：appkit 根包（Deps/Assembly 所在）只允许
// api 装配层 import——domain/app/infra 一律经子包端口（uow/events/crud/…）消费。
func TestDepsAtAPILayer(t *testing.T) {
	testutil.AssertDepsAtAPILayer(t, ".", "../../internal/app", "../../internal/infra")
}
