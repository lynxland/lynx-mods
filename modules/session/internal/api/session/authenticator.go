package session

import (
	"context"
	"errors"

	appsession "github.com/lynxland/lynx-mods/modules/session/internal/app/session"

	"github.com/lynxland/appkit/clock"
	"github.com/lynxland/appkit/jwt"
	"github.com/lynxland/grpcapi/authn"
	"github.com/lynxland/grpcapi/principal"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// BearerAuthenticator 把 Authorization: Bearer <token>（本模块签发的
// 访问令牌）变成携带 sid 的 Principal。与 identity 的认证器同链共存：
// 签名不匹配/非 JWT = 不是本模块令牌，(nil, nil) 交给链上下一个。
//
// 吊销即时性：令牌本身无状态，认证器回读会话状态（含用户可用性），
// Logout/改密联动一落地，下一个请求即 401。
type BearerAuthenticator struct {
	uc       *appsession.UseCase
	verifier *jwt.Verifier
	now      clock.Clock
}

// NewBearerAuthenticator 构造认证器（Assemble 调用）。
func NewBearerAuthenticator(uc *appsession.UseCase, verifier *jwt.Verifier, c clock.Clock) *BearerAuthenticator {
	if c == nil {
		c = clock.System{}
	}
	return &BearerAuthenticator{uc: uc, verifier: verifier, now: c}
}

var _ authn.Authenticator = (*BearerAuthenticator)(nil)

// Name 实现 authn.Authenticator。
func (a *BearerAuthenticator) Name() string { return "session-bearer" }

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
			return nil, nil // 不是本模块签发的令牌：继续链（identity 等）
		case errors.Is(err, jwt.ErrExpired):
			return nil, status.Error(codes.Unauthenticated, "access token expired")
		default:
			return nil, status.Error(codes.Unauthenticated, "invalid access token")
		}
	}
	sid := claims.Custom["sid"]
	if sid == "" {
		return nil, nil // 本模块密钥签发但无 sid 声明：视为非会话令牌
	}
	if _, err := a.uc.ResolveSession(ctx, sid); err != nil {
		return nil, status.Error(codes.Unauthenticated, "invalid session")
	}
	return &principal.Principal{
		ID:    claims.Subject,
		Kind:  principal.KindUser,
		Attrs: map[string]string{"email": claims.Custom["email"], "sid": sid},
	}, nil
}
