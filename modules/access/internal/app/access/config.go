package access

import (
	"fmt"

	"github.com/lynxland/lynx-start/modules/access/domain/access"
)

// Config 是 access 模块的配置段（契约③：自有段 access）。角色目录在此
// 声明——应用所有者可审计可改；分配事实（谁拥有什么角色）落库。
type Config struct {
	// Roles 是角色目录：role → permissions（精确匹配，无通配符；
	// grpcapi 策略 permissions 声明为 MATCH_ALL）。
	Roles map[string][]string `mapstructure:"roles"`
	// BootstrapAdmins 是启动期自动授予 admin 角色的邮箱清单
	// （幂等；用户未注册则告警跳过，注册后重启收敛）。
	BootstrapAdmins []string `mapstructure:"bootstrap_admins"`
}

// ApplyDefaults 落默认值（LoadSection 的 defaults 钩子）。
func (c *Config) ApplyDefaults() {
	if c.Roles == nil {
		c.Roles = map[string][]string{
			// admin 开箱即得本模块管理权；应用按需在 config.yaml 扩充。
			"admin": {"access.assignment.write"},
		}
	}
}

// Validate fail-fast 校验（组合根调用）。
func (c *Config) Validate() error {
	for role, perms := range c.Roles {
		if !access.ValidateName(role) {
			return fmt.Errorf("config access.roles: invalid role name %q", role)
		}
		for _, p := range perms {
			if !access.ValidateName(p) {
				return fmt.Errorf("config access.roles[%s]: invalid permission %q", role, p)
			}
		}
	}
	if len(c.BootstrapAdmins) > 0 {
		if _, ok := c.Roles["admin"]; !ok {
			return fmt.Errorf("config access: bootstrap_admins 需要目录中声明 admin 角色")
		}
	}
	return nil
}
