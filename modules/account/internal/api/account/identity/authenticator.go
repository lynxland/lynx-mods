package identity

import (
	"context"
	"errors"

	appidentity "github.com/lynxland/lynx-start/modules/account/internal/app/account/identity"

	"github.com/lynxland/appkit/clock"
	"github.com/lynxland/appkit/jwt"
	"github.com/lynxland/grpcapi/authn"
	"github.com/lynxland/grpcapi/principal"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// BearerAuthenticator 把 Authorization: Bearer <token> 变成 Principal
// （grpcapi authn SPI），是本模块对整个应用的横切贡献——其他模块的
// 方法只要不声明 anonymous，就自动要求 principal。
//
// 链式语义（多模块令牌共存的关键）：签名不匹配/非 JWT 形态 = 不是本
// 模块签发的令牌，返回 (nil, nil) 交给链上下一个认证器（如 session
// 模块）；签名有效但过期/未生效 = 是本模块的令牌但已失效，立即拒绝。
type BearerAuthenticator struct {
	uc       *appidentity.UseCase
	verifier *jwt.Verifier
	now      clock.Clock
}

// NewBearerAuthenticator 构造认证器（Assemble 调用）。
func NewBearerAuthenticator(uc *appidentity.UseCase, verifier *jwt.Verifier, c clock.Clock) *BearerAuthenticator {
	if c == nil {
		c = clock.System{}
	}
	return &BearerAuthenticator{uc: uc, verifier: verifier, now: c}
}

var _ authn.Authenticator = (*BearerAuthenticator)(nil)

// Name 实现 authn.Authenticator。
func (a *BearerAuthenticator) Name() string { return "identity-bearer" }

// Authenticate 实现 authn.Authenticator。
func (a *BearerAuthenticator) Authenticate(ctx context.Context, c authn.Credentials) (*principal.Principal, error) {
	token := c.BearerToken()
	if token == "" {
		return nil, nil // 非 Bearer 凭证：交给链上下一个认证器
	}
	claims, err := a.verifier.Verify(token, a.now.Now())
	if err != nil {
		switch {
		case errors.Is(err, jwt.ErrBadSig), errors.Is(err, jwt.ErrMalformed):
			return nil, nil // 不是本模块签发的令牌：继续链
		case errors.Is(err, jwt.ErrExpired):
			return nil, status.Error(codes.Unauthenticated, "access token expired")
		default:
			return nil, status.Error(codes.Unauthenticated, "invalid access token")
		}
	}
	user, err := a.uc.ResolveUser(ctx, claims.Subject)
	if err != nil {
		// 用户不存在/已停用：不泄露细节，按无效令牌拒绝。
		return nil, status.Error(codes.Unauthenticated, "invalid access token")
	}
	return &principal.Principal{
		ID:    user.ID,
		Kind:  principal.KindUser,
		Attrs: map[string]string{"email": user.Email, "display_name": user.DisplayName},
	}, nil
}
