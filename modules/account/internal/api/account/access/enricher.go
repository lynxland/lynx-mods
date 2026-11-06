package access

import (
	"context"

	appaccess "github.com/lynxland/lynx-start/modules/account/internal/app/account/access"

	"github.com/lynxland/grpcapi/authn"
	"github.com/lynxland/grpcapi/principal"
)

// PrincipalEnricher 在认证成功后往 Principal 注入 roles/permissions
// claims（grpcapi authn.Enricher SPI）——proto 策略里的
// permissions/roles 声明由此生效。这是本模块对整个应用的横切贡献：
// 任何模块的方法都能声明所需权限，而不关心令牌来自 identity 还是
// session。
//
// 代价：每个带凭证的请求一次主键查询 + 内存目录展开（v0.1 无缓存，
// 热点应用可在源码里加 TTL 缓存——模块归你所有）。
type PrincipalEnricher struct {
	uc *appaccess.UseCase
}

// NewPrincipalEnricher 构造增强器（Assemble 调用）。
func NewPrincipalEnricher(uc *appaccess.UseCase) *PrincipalEnricher {
	return &PrincipalEnricher{uc: uc}
}

var _ authn.Enricher = (*PrincipalEnricher)(nil)

// Name 实现 authn.Enricher。
func (e *PrincipalEnricher) Name() string { return "access-claims" }

// Enrich 实现 authn.Enricher：无分配的用户也写入空 claims（明确「查过」
// 与「没查」的区分，授权判定恒定走 claims 路径）。
func (e *PrincipalEnricher) Enrich(ctx context.Context, p *principal.Principal) error {
	roles, err := e.uc.RolesForUser(ctx, p.ID)
	if err != nil {
		return err
	}
	perms, err := e.uc.PermissionsForUser(ctx, p.ID)
	if err != nil {
		return err
	}
	if p.Claims == nil {
		p.Claims = map[string][]string{}
	}
	p.Claims["roles"] = roles
	p.Claims["permissions"] = perms
	return nil
}
