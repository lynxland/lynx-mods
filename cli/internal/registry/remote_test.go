package registry

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// mkGitRepo 造一个带 tag 的最小 registry 仓库并返回其 file:// URL。
func mkGitRepo(t *testing.T) string {
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
	write("modules/demo/item.json", `{"name":"demo","version":"0.1.0"}`)
	write("scaffold/go.mod.tpl", "x")
	run("git", "init", "-q", "-b", "main")
	run("git", "add", "-A")
	run("git", "commit", "-qm", "init")
	run("git", "tag", "modules/demo/v0.1.0")
	return "file://" + dir
}

func TestIsRemote(t *testing.T) {
	for raw, want := range map[string]bool{
		"https://github.com/lynxland/appkit": true,
		"git@github.com:lynxland/appkit.git": true,
		"ssh://git@host/repo":                true,
		"/local/path":                        false,
		"../relative":                        false,
		"":                                   false,
	} {
		if got := IsRemote(raw); got != want {
			t.Errorf("IsRemote(%q) = %v, want %v", raw, got, want)
		}
	}
}

func TestResolveLocalPassthrough(t *testing.T) {
	got, err := Resolve("/some/local/path")
	if err != nil || got != "/some/local/path" {
		t.Fatalf("Resolve local = %q, %v", got, err)
	}
}

func TestResolveRemoteCloneAndRefresh(t *testing.T) {
	url := mkGitRepo(t)
	// 缓存根隔离：UserCacheDir 在测试进程内固定，用子目录名哈希天然隔离
	//（URL 含 t.TempDir 唯一路径，不会撞）。
	dir1, err := Resolve(url)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if dir1 == url || dir1 == strings.TrimPrefix(url, "file://") {
		t.Fatalf("remote must resolve to cache dir, got %q", dir1)
	}
	if _, err := os.Stat(filepath.Join(dir1, "registry", "registry.json")); err != nil {
		t.Fatalf("clone missing registry.json: %v", err)
	}
	// 二次 resolve 走增量刷新路径（不报错即过）。
	dir2, err := Resolve(url)
	if err != nil {
		t.Fatalf("re-resolve: %v", err)
	}
	if dir1 != dir2 {
		t.Fatalf("cache dir must be stable: %q vs %q", dir1, dir2)
	}
	// tag 随 clone 进缓存：版本解析可用。
	idx, err := Load(dir1)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := idx.ItemAt(dir1, "demo", "0.1.0"); err != nil {
		t.Fatalf("ItemAt over cached remote clone: %v", err)
	}
}
