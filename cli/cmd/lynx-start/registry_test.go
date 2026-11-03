package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// newRegistryScratch 建一个最小 registry 仓库骨架（已登记 demo 模块）。
func newRegistryScratch(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	write := func(rel, content string) {
		t.Helper()
		path := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("registry/registry.json", "{\n  \"name\": \"lynx-start\",\n  \"items\": [\n    { \"name\": \"demo\", \"path\": \"modules/demo\" }\n  ]\n}\n")
	write("go.work", "go 1.26.8\n\nuse (\n  ./kernel\n  ./modules/demo\n)\n")
	write("modules/demo/go.mod", "module github.com/lynxland/lynx-start/modules/demo\n\ngo 1.26.8\n")
	write("modules/demo/module.json", "{\n  \"name\": \"demo\",\n  \"version\": \"0.1.0\",\n  \"description\": \"demo module\",\n  \"modulePath\": \"github.com/lynxland/lynx-start/modules/demo\",\n  \"moduleDependencies\": [],\n  \"goDependencies\": [],\n  \"configFragment\": \"\",\n  \"publishedEvents\": [\"demo.item.created\"]\n}\n")
	write("modules/todo/go.mod", "module github.com/lynxland/lynx-start/modules/todo\n\ngo 1.26.8\n")
	return root
}

func TestRegistryAddRegistersThreePlaces(t *testing.T) {
	root := newRegistryScratch(t)
	if err := registryAdd(root, "modules/todo"); err != nil {
		t.Fatalf("registryAdd: %v", err)
	}
	// module.json 骨架生成。
	manifestRaw, err := os.ReadFile(filepath.Join(root, "modules", "todo", "module.json"))
	if err != nil {
		t.Fatalf("module.json not written: %v", err)
	}
	if !strings.Contains(string(manifestRaw), `"modulePath": "github.com/lynxland/lynx-start/modules/todo"`) {
		t.Errorf("module.json modulePath not derived from go.mod:\n%s", manifestRaw)
	}
	// registry.json 登记 + 排版保持（逗号接在上一条目后,不产生独立逗号行）。
	idxRaw, _ := os.ReadFile(filepath.Join(root, "registry", "registry.json"))
	idx := string(idxRaw)
	if !strings.Contains(idx, `{ "name": "todo", "path": "modules/todo" }`) {
		t.Errorf("registry.json missing todo entry:\n%s", idx)
	}
	if strings.Contains(idx, "\n ,") || strings.Contains(idx, "},\n ,") {
		t.Errorf("registry.json formatting broken (stray comma line):\n%s", idx)
	}
	// go.work use 插入且保持既有缩进。
	workRaw, _ := os.ReadFile(filepath.Join(root, "go.work"))
	work := string(workRaw)
	if !strings.Contains(work, "./modules/todo") {
		t.Errorf("go.work missing ./modules/todo:\n%s", work)
	}
	if !strings.Contains(work, "  ./modules/todo\n") {
		t.Errorf("go.work indent does not match existing entries:\n%s", work)
	}

	// 幂等：重复 add 不报错、不重复登记。
	if err := registryAdd(root, "modules/todo"); err != nil {
		t.Fatalf("second registryAdd should be idempotent: %v", err)
	}
	idx2, _ := os.ReadFile(filepath.Join(root, "registry", "registry.json"))
	if strings.Count(string(idx2), `"name": "todo"`) != 1 {
		t.Errorf("registry.json has duplicate todo entries:\n%s", idx2)
	}
}

func TestRegistryCheckCatchesInconsistency(t *testing.T) {
	root := newRegistryScratch(t)
	if err := registryAdd(root, "modules/todo"); err != nil {
		t.Fatalf("registryAdd: %v", err)
	}
	// 骨架 description 是 TODO → check 必须失败。
	if err := registryCheck(root); err == nil {
		t.Fatal("registryCheck should fail while description is a TODO placeholder")
	}
	// 填掉 description → 通过。
	manifestPath := filepath.Join(root, "modules", "todo", "module.json")
	raw, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manifestPath, []byte(strings.Replace(string(raw), "TODO: 一句话用途说明（registry list 展示）", "todo module", 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := registryCheck(root); err != nil {
		t.Fatalf("registryCheck after filling description: %v", err)
	}
	// 摘掉 go.work 的 use → check 失败。
	workPath := filepath.Join(root, "go.work")
	work, _ := os.ReadFile(workPath)
	if err := os.WriteFile(workPath, []byte(strings.ReplaceAll(string(work), "\n  ./modules/todo", "")), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := registryCheck(root); err == nil {
		t.Fatal("registryCheck should fail when go.work entry is missing")
	}
}
