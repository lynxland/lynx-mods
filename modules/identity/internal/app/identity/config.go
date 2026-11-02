package identity

import (
	"fmt"
	"time"

	"github.com/lynxland/appkit/secretkey"
)

// DefaultDevSecret 是安装器写入 config.yaml 的开发占位密钥。
// Assemble 检测到它时打警告；生产环境必须更换（模块 README 有说明）。
const DefaultDevSecret = "dev-only-identity-secret-change-me"

// Config 是 identity 模块的配置段（契约③：自有段 identity，无全局
// config.proto）。Assemble 经 configkit.LoadSection 解码。
type Config struct {
	// Secret 是令牌主密钥（appkit/secretkey 按 purpose 派生子密钥）。
	Secret string `mapstructure:"secret"`
	// TokenTTL 是访问令牌有效期。无吊销机制（v0.1），保持短时效。
	TokenTTL time.Duration `mapstructure:"token_ttl"`
	// Issuer 是令牌 iss 声明（跨服务互验时对齐）。
	Issuer string `mapstructure:"issuer"`
}

// ApplyDefaults 落默认值（LoadSection 的 defaults 钩子）。
func (c *Config) ApplyDefaults() {
	c.Secret = DefaultDevSecret
	c.TokenTTL = 30 * time.Minute
	c.Issuer = "lynx-mods"
}

// Validate fail-fast 校验（组合根调用）。
func (c *Config) Validate() error {
	if err := secretkey.MustValidate(c.Secret, "config identity.secret"); err != nil {
		return err
	}
	if c.TokenTTL <= 0 {
		return fmt.Errorf("config identity.token_ttl must be positive")
	}
	return nil
}
