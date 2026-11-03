package main

import (
	"os"
	"path/filepath"
	"testing"
)

// TestHasLocalProto 断言空应用拦截：proto/ 无 .proto（或目录不存在）时
// generate 管线应跳过 buf（policy.proto 已改经 BSR dep 供给）。
func TestHasLocalProto(t *testing.T) {
	dir := t.TempDir()
	if hasLocalProto(dir) {
		t.Fatal("empty app should have no local proto")
	}
	if err := os.MkdirAll(filepath.Join(dir, "proto", "identity", "v1"), 0o755); err != nil {
		t.Fatal(err)
	}
	if hasLocalProto(dir) {
		t.Fatal("empty proto tree should not count")
	}
	if err := os.WriteFile(filepath.Join(dir, "proto", "identity", "v1", "identity.proto"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if !hasLocalProto(dir) {
		t.Fatal("proto tree with a .proto should be detected")
	}
}
