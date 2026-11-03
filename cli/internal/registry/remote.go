package registry

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// 远端 registry：--registry / LYNXSTART_REGISTRY 接受 git URL（https://…、
// git@…:…、ssh://…、*.git），clone 到用户缓存目录后按本地路径使用；
// 已缓存则 fetch --tags + reset --hard origin/HEAD 增量刷新。tag 版本解析
//（modules/<m>/v<x.y.z>）对缓存目录天然生效。

// IsRemote 报告 raw 是否为 git URL 形态（本地路径原样返回）。
func IsRemote(raw string) bool {
	if raw == "" || filepath.IsAbs(raw) {
		return false
	}
	return strings.HasPrefix(raw, "http://") || strings.HasPrefix(raw, "https://") ||
		strings.HasPrefix(raw, "git@") || strings.HasPrefix(raw, "ssh://") ||
		strings.HasPrefix(raw, "git://") || strings.HasPrefix(raw, "file://") ||
		strings.HasSuffix(raw, ".git")
}

// Resolve 把 registry 参数解析为本地目录：URL → 缓存 clone（存在则增量
// 刷新）；本地路径原样返回（不校验存在性，由 Load 给出可行动错误）。
func Resolve(raw string) (string, error) {
	if !IsRemote(raw) {
		return raw, nil
	}
	cacheRoot, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("resolve registry cache dir: %w", err)
	}
	sum := sha256.Sum256([]byte(raw))
	dir := filepath.Join(cacheRoot, "lynx-start", "registry", hex.EncodeToString(sum[:])[:12])
	if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
		if err := refreshClone(dir); err != nil {
			return "", err
		}
		return dir, nil
	}
	if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
		return "", err
	}
	fmt.Printf("registry: cloning %s → %s\n", raw, dir)
	if out, err := exec.Command("git", "clone", "--quiet", raw, dir).CombinedOutput(); err != nil {
		_ = os.RemoveAll(dir)
		return "", fmt.Errorf("git clone %s: %v\n%s", raw, err, out)
	}
	return dir, nil
}

// refreshClone 增量刷新缓存 clone：fetch 全部 tag 与分支，工作区硬复位到
// 远端默认分支（缓存只读使用，本地修改一律被丢弃）。
func refreshClone(dir string) error {
	if out, err := exec.Command("git", "-C", dir, "fetch", "--quiet", "--tags", "--prune", "origin").CombinedOutput(); err != nil {
		return fmt.Errorf("git fetch: %v\n%s", err, out)
	}
	if out, err := exec.Command("git", "-C", dir, "reset", "--quiet", "--hard", "origin/HEAD").CombinedOutput(); err != nil {
		// origin/HEAD 别名缺失（浅 clone/服务端未设默认分支）：退回
		// FETCH_HEAD 提示语义，仍以 fetch 结果为准。
		return fmt.Errorf("git reset origin/HEAD: %v\n%s", err, out)
	}
	return nil
}
