package session

import (
	"fmt"
	"time"

	"github.com/lynxland/appkit/secretkey"
)

// DefaultDevSecret 是安装器写入 config.yaml 的开发占位密钥。
// Assemble 检测到它时打警告；生产环境必须更换。
const DefaultDevSecret = "dev-only-session-secret-change-me"

// Config 是 session 模块的配置段（契约③：自有段 session）。
type Config struct {
	// Secret 是令牌主密钥（appkit/secretkey 按 purpose 派生；与
	// identity.secret 独立——两模块令牌天然互不通用）。
	Secret string `mapstructure:"secret"`
	// AccessTTL 是访问令牌有效期（无状态，吊销生效粒度）。
	AccessTTL time.Duration `mapstructure:"access_ttl"`
	// RefreshTTL 是刷新令牌与会话的绝对时长（轮换不续期）。
	RefreshTTL time.Duration `mapstructure:"refresh_ttl"`
	// Issuer 是令牌 iss 声明。
	Issuer string `mapstructure:"issuer"`
}

// ApplyDefaults 落默认值（LoadSection 的 defaults 钩子）。
func (c *Config) ApplyDefaults() {
	c.Secret = DefaultDevSecret
	c.AccessTTL = 15 * time.Minute
	c.RefreshTTL = 30 * 24 * time.Hour
	c.Issuer = "lynx-start"
}

// Validate fail-fast 校验（组合根调用）。
func (c *Config) Validate() error {
	if err := secretkey.MustValidate(c.Secret, "config session.secret"); err != nil {
		return err
	}
	if c.AccessTTL <= 0 {
		return fmt.Errorf("config session.access_ttl must be positive")
	}
	if c.RefreshTTL <= 0 {
		return fmt.Errorf("config session.refresh_ttl must be positive")
	}
	if c.AccessTTL >= c.RefreshTTL {
		return fmt.Errorf("config session.access_ttl must be shorter than refresh_ttl")
	}
	return nil
}
