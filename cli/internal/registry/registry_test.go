package registry

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// setupRepo 造一个最小 registry 仓库：registry.json + demo 模块 + module.json，
// commit 后打 tag modules/demo/v0.1.0。
func setupRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%v: %v\n%s", args, err, out)
		}
	}
	write := func(rel, content string) {
		t.Helper()
		p := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("registry/registry.json", `{"name":"t","items":[{"name":"demo","path":"modules/demo"}]}`)
	write("modules/demo/module.json", `{"name":"demo","version":"0.1.0","modulePath":"example.com/demo"}`)
	write("modules/demo/domain/demo/demo.go", "package demo\n")
	run("git", "init", "-q")
	run("git", "add", "-A")
	run("git", "commit", "-qm", "init")
	run("git", "tag", "modules/demo/v0.1.0")
	return dir
}

func TestManifestAtLatest(t *testing.T) {
	dir := setupRepo(t)
	idx, err := Load(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	manifest, cleanup, err := idx.ManifestAt(dir, "demo", "")
	if err != nil {
		t.Fatalf("manifestat: %v", err)
	}
	defer cleanup()
	if manifest.Dir != filepath.Join(dir, "modules/demo") {
		t.Fatalf("latest form must read the worktree dir, got %s", manifest.Dir)
	}
}

func TestManifestAtTag(t *testing.T) {
	dir := setupRepo(t)
	idx, err := Load(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	manifest, cleanup, err := idx.ManifestAt(dir, "demo", "0.1.0")
	if err != nil {
		t.Fatalf("manifestat: %v", err)
	}
	defer cleanup()
	if manifest.Version != "0.1.0" {
		t.Fatalf("version = %s", manifest.Version)
	}
	if strings.HasPrefix(manifest.Dir, dir) {
		t.Fatalf("tagged form must extract to a temp dir, got %s", manifest.Dir)
	}
	// 提取树里源码可读（untar 生效）。
	if _, err := os.Stat(filepath.Join(manifest.Dir, "domain/demo/demo.go")); err != nil {
		t.Fatalf("extracted source missing: %v", err)
	}
}

func TestManifestAtMissingTag(t *testing.T) {
	dir := setupRepo(t)
	idx, _ := Load(dir)
	_, _, err := idx.ManifestAt(dir, "demo", "9.9.9")
	if err == nil || !strings.Contains(err.Error(), "no git tag") {
		t.Fatalf("want actionable missing-tag error, got %v", err)
	}
}

func TestManifestAtTagManifestDrift(t *testing.T) {
	dir := setupRepo(t)
	// 改 module.json 版本后重打同号 tag：tag 内版本与请求不一致必须拒绝。
	if err := os.WriteFile(filepath.Join(dir, "modules/demo/module.json"),
		[]byte(`{"name":"demo","version":"0.2.0"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%v: %v\n%s", args, err, out)
		}
	}
	run("git", "commit", "-qam", "bump")
	run("git", "tag", "-f", "modules/demo/v0.1.0")
	idx, _ := Load(dir)
	_, _, err := idx.ManifestAt(dir, "demo", "0.1.0")
	if err == nil || !strings.Contains(err.Error(), "drift") {
		t.Fatalf("want drift error, got %v", err)
	}
}
