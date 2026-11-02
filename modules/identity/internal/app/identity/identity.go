// Package identity 是身份模块的用例层：编排领域与端口
// （事务/事件/仓储/令牌签发验证）。
package identity

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/lynxland/appkit/clock"
	"github.com/lynxland/appkit/events"
	"github.com/lynxland/appkit/idgen"
	"github.com/lynxland/appkit/jwt"
	"github.com/lynxland/appkit/password"
	"github.com/lynxland/appkit/uow"
	"github.com/lynxland/lynx-mods/modules/identity/domain/identity"
)

// PurposeAccessToken 是访问令牌的密钥派生用途标签（appkit/secretkey 的
// purpose 域隔离）：其他用途派生的令牌在本模块不可通过验证。
const PurposeAccessToken = "identity-access-token"

// UseCase 是身份模块的用例集。
type UseCase struct {
	repo     identity.Repository
	tx       uow.Runner
	events   events.Publisher
	signer   *jwt.Signer
	verifier *jwt.Verifier
	now      clock.Clock
}

// New 构造用例（Assemble 调用）。c 为空取系统时钟。
func New(repo identity.Repository, tx uow.Runner, publisher events.Publisher, signer *jwt.Signer, verifier *jwt.Verifier, c clock.Clock) *UseCase {
	if c == nil {
		c = clock.System{}
	}
	return &UseCase{repo: repo, tx: tx, events: publisher, signer: signer, verifier: verifier, now: c}
}

// Register 注册并在同一工作单元内发布事件；注册即签发访问令牌（自动登录）。
func (u *UseCase) Register(ctx context.Context, email, plainPassword, displayName string) (*identity.User, *jwt.Token, error) {
	email = identity.NormalizeEmail(email)
	if err := identity.ValidateEmail(email); err != nil {
		return nil, nil, err
	}
	if err := identity.ValidatePassword(plainPassword); err != nil {
		return nil, nil, err
	}
	hash, err := password.Hash(plainPassword)
	if err != nil {
		return nil, nil, err
	}
	var user *identity.User
	err = u.tx.Run(ctx, func(ctx context.Context) error {
		nu, err := identity.NewUser(idgen.New(), email, displayName, hash, u.now.Now())
		if err != nil {
			return err
		}
		if err := u.repo.Create(ctx, nu); err != nil {
			return err
		}
		user = nu
		return u.publish(ctx, identity.EventUserCreated, map[string]string{"id": nu.ID, "email": nu.Email})
	})
	if err != nil {
		return nil, nil, err
	}
	token, err := u.signToken(user)
	if err != nil {
		return nil, nil, err
	}
	return user, token, nil
}

// Login 邮箱+口令换访问令牌。邮箱不存在与口令错误返回同一哨兵错误。
func (u *UseCase) Login(ctx context.Context, email, plainPassword string) (*identity.User, *jwt.Token, error) {
	user, err := u.repo.GetByEmail(ctx, identity.NormalizeEmail(email))
	if err != nil {
		if errors.Is(err, identity.ErrUserNotFound) {
			// 时序对齐：邮箱不存在也跑一次等价 Argon2id（哑靶来自 appkit，
			// 与其参数恒同参——参数升级经 appkit 修复直达，不会失配）。
			if dummy, derr := password.DummyHash(); derr == nil {
				_, _ = password.Verify(plainPassword, dummy)
			}
			return nil, nil, identity.ErrInvalidCredentials
		}
		return nil, nil, err
	}
	ok, err := password.Verify(plainPassword, user.PasswordHash)
	if err != nil || !ok {
		return nil, nil, identity.ErrInvalidCredentials
	}
	if !user.IsActive() {
		return nil, nil, identity.ErrUserDisabled
	}
	token, err := u.signToken(user)
	if err != nil {
		return nil, nil, err
	}
	return user, token, nil
}

// Authenticate 验证访问令牌并回读用户状态（fail-closed：令牌指向的用户
// 不存在或已停用即拒绝——停用即时生效，代价是每请求一次主键查询）。
func (u *UseCase) Authenticate(ctx context.Context, accessToken string) (*identity.User, error) {
	claims, err := u.verifier.Verify(accessToken, u.now.Now())
	if err != nil {
		switch {
		case errors.Is(err, jwt.ErrExpired):
			return nil, identity.ErrTokenExpired
		case errors.Is(err, jwt.ErrMalformed),
			errors.Is(err, jwt.ErrBadSig),
			errors.Is(err, jwt.ErrIssuer),
			errors.Is(err, jwt.ErrNotYet):
			return nil, identity.ErrTokenInvalid
		default:
			return nil, err
		}
	}
	if claims.Subject == "" {
		return nil, identity.ErrTokenInvalid
	}
	user, err := u.repo.GetByID(ctx, claims.Subject)
	if err != nil {
		if errors.Is(err, identity.ErrUserNotFound) {
			return nil, identity.ErrTokenInvalid
		}
		return nil, err
	}
	if !user.IsActive() {
		return nil, identity.ErrUserDisabled
	}
	return user, nil
}

// GetMe 按主体 ID 取当前用户。
func (u *UseCase) GetMe(ctx context.Context, userID string) (*identity.User, error) {
	return u.repo.GetByID(ctx, userID)
}

// ResolveUser 按主体 ID 取可用用户（认证器消费：不存在或停用即拒绝，
// 与 Authenticate 的 fail-closed 回读同语义，但不做令牌验证——验证归
// 认证器，便于多模块令牌共存时区分「不是我的令牌」与「我的令牌坏了」）。
func (u *UseCase) ResolveUser(ctx context.Context, userID string) (*identity.User, error) {
	user, err := u.repo.GetByID(ctx, userID)
	if err != nil {
		return nil, err
	}
	if !user.IsActive() {
		return nil, identity.ErrUserDisabled
	}
	return user, nil
}

// ChangePassword 验旧换新并发布事件。旧令牌在过期前仍有效（见领域注释）。
func (u *UseCase) ChangePassword(ctx context.Context, userID, oldPassword, newPassword string) error {
	if err := identity.ValidatePassword(newPassword); err != nil {
		return err
	}
	return u.tx.Run(ctx, func(ctx context.Context) error {
		user, err := u.repo.GetByID(ctx, userID)
		if err != nil {
			return err
		}
		ok, err := password.Verify(oldPassword, user.PasswordHash)
		if err != nil || !ok {
			return identity.ErrInvalidCredentials
		}
		hash, err := password.Hash(newPassword)
		if err != nil {
			return err
		}
		if err := user.ChangePassword(hash, u.now.Now()); err != nil {
			return err
		}
		if err := u.repo.UpdatePassword(ctx, user); err != nil {
			return err
		}
		return u.publish(ctx, identity.EventUserPasswordChanged, map[string]string{"id": user.ID})
	})
}

func (u *UseCase) signToken(user *identity.User) (*jwt.Token, error) {
	return u.signer.Sign(user.ID, map[string]string{"email": user.Email})
}

// publish 构造事件信封（载荷为最小事实集，消费方按事件目录文档解码）。
func (u *UseCase) publish(ctx context.Context, name string, payload map[string]string) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	return u.events.Publish(ctx, events.Envelope{
		ID:         idgen.New(),
		Name:       name,
		Module:     "identity",
		OccurredAt: u.now.Now(),
		Payload:    raw,
	})
}
