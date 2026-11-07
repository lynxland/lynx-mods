// Package identity 是身份模块的用例层：编排领域与端口
// （事务/事件/仓储/令牌签发验证）。
package identity

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/lynxland/appkit/clock"
	"github.com/lynxland/appkit/crud"
	"github.com/lynxland/appkit/events"
	"github.com/lynxland/appkit/idgen"
	"github.com/lynxland/appkit/jwt"
	"github.com/lynxland/appkit/password"
	"github.com/lynxland/appkit/ratelimit"
	"github.com/lynxland/appkit/uow"
	"github.com/lynxland/lynx-start/modules/account/domain/account/identity"
)

// 令牌派生用途标签（appkit/secretkey 的 purpose 域隔离）：访问/邮箱
// 验证/密码重置三类令牌互不通用（验证器按 purpose 匹配，跨用途令牌
// 直接拒绝）。
const (
	PurposeAccessToken   = "identity-access-token"
	PurposeEmailVerify   = "identity-email-verify"
	PurposePasswordReset = "identity-password-reset"
)

// OneShotTokenTTL 是验证/重置令牌的有效期（无状态令牌——单次性由状态
// 变化达成：验证翻转 email_verified；重置改密后旧密码失配+会话全吊销）。
// Assemble 构造一次性签发器时引用。
const OneShotTokenTTL = 15 * time.Minute

// UseCase 是身份模块的用例集。
type UseCase struct {
	repo     identity.Repository
	tx       uow.Runner
	events   events.Publisher
	signer   *jwt.Signer
	verifier *jwt.Verifier
	// oneShot 签发邮箱验证/密码重置的一次性令牌（专用 purpose 短时效）。
	oneShot  *jwt.Signer
	oneShotV *jwt.Verifier
	// loginFails 登录失败滑窗限流（nil = 关闭；进程内实现——单进程
	// modular monolith 语义内有效，多实例部署见 README 已知限制）。
	loginFails ratelimit.Limiter
	now        clock.Clock
}

// New 构造用例（Assemble 调用）。c 为空取系统时钟；loginFails 为 nil
// 关闭登录限流。
func New(repo identity.Repository, tx uow.Runner, publisher events.Publisher, signer *jwt.Signer, verifier *jwt.Verifier, oneShot *jwt.Signer, oneShotV *jwt.Verifier, loginFails ratelimit.Limiter, c clock.Clock) *UseCase {
	if c == nil {
		c = clock.System{}
	}
	return &UseCase{repo: repo, tx: tx, events: publisher, signer: signer, verifier: verifier, oneShot: oneShot, oneShotV: oneShotV, loginFails: loginFails, now: c}
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
	normalized := identity.NormalizeEmail(email)
	// 失败锁定（前置检查）：窗口内失败计数已达阈值即拒绝（与密码错误
	// 同响应——锁定态不可探测）。
	if u.loginFails != nil {
		if d := u.loginFails.Allow("login:"+normalized, u.now.Now()); !d.Allowed {
			return nil, nil, identity.ErrInvalidCredentials
		}
	}
	fail := func() (*identity.User, *jwt.Token, error) {
		if u.loginFails != nil {
			_ = u.loginFails.Allow("login:"+normalized, u.now.Now())
		}
		return nil, nil, identity.ErrInvalidCredentials
	}
	user, err := u.repo.GetByEmail(ctx, normalized)
	if err != nil {
		if errors.Is(err, identity.ErrUserNotFound) {
			// 时序对齐：邮箱不存在也跑一次等价 Argon2id（哑靶来自 appkit，
			// 与其参数恒同参——参数升级经 appkit 修复直达，不会失配）。
			if dummy, derr := password.DummyHash(); derr == nil {
				_, _ = password.Verify(plainPassword, dummy)
			}
			return fail()
		}
		return nil, nil, err
	}
	ok, err := password.Verify(plainPassword, user.PasswordHash)
	if err != nil || !ok {
		return fail()
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
		Module:     "account",
		OccurredAt: u.now.Now(),
		Payload:    raw,
	})
}

// ---- P0 流（v0.7.0）：邮箱验证 / 密码重置 / 管理面 ----

// RequestEmailVerification 向认证主体邮箱发送验证令牌（幂等——已验证
// 用户重复请求也发新令牌，验证本身幂等）。令牌经事件载荷投递
// （account.user.verification-requested——通知渠道发信；载荷含 token，
// 进程内事件总线语义下单进程可见）。
func (u *UseCase) RequestEmailVerification(ctx context.Context, userID string) error {
	user, err := u.repo.GetByID(ctx, userID)
	if err != nil {
		return err
	}
	token, err := u.oneShotToken(user, PurposeEmailVerify)
	if err != nil {
		return err
	}
	return u.publish(ctx, identity.EventUserVerificationRequested, map[string]string{
		"id": user.ID, "email": user.Email, "token": token,
	})
}

// VerifyEmail 验证邮箱令牌（翻转 email_verified；幂等——重复验证 no-op）。
func (u *UseCase) VerifyEmail(ctx context.Context, token string) (*identity.User, error) {
	user, err := u.verifyOneShot(token, PurposeEmailVerify)
	if err != nil {
		return nil, err
	}
	user.MarkEmailVerified(u.now.Now())
	if err := u.repo.UpdateEmailVerified(ctx, user.ID, user.EmailVerified, user.UpdatedAt); err != nil {
		return nil, err
	}
	return user, nil
}

// RequestPasswordReset 请求密码重置——**恒返回成功**（邮箱枚举防护，
// OWASP：存在与否的响应无差异）。令牌只向存在用户的邮箱投递（事件）。
func (u *UseCase) RequestPasswordReset(ctx context.Context, email string) error {
	user, err := u.repo.GetByEmail(ctx, identity.NormalizeEmail(email))
	if err != nil {
		if errors.Is(err, identity.ErrUserNotFound) {
			return nil // 静默——不泄露邮箱存在性
		}
		return err
	}
	token, err := u.oneShotToken(user, PurposePasswordReset)
	if err != nil {
		return err
	}
	return u.publish(ctx, identity.EventUserResetRequested, map[string]string{
		"id": user.ID, "email": user.Email, "token": token,
	})
}

// ConfirmPasswordReset 确认重置：验令牌 → 改密（不需要旧密码）→ 发布
// 改密事件（session 域联动全局吊销——令牌单次性由改密生效达成）。
func (u *UseCase) ConfirmPasswordReset(ctx context.Context, token, newPassword string) error {
	user, err := u.verifyOneShot(token, PurposePasswordReset)
	if err != nil {
		return err
	}
	return u.changePassword(ctx, user, newPassword)
}

// DisableUser 停用（admin 面）；发布 account.user.disabled（session 域
// 联动吊销全部会话）。
func (u *UseCase) DisableUser(ctx context.Context, id string) (*identity.User, error) {
	user, err := u.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if err := user.Disable(u.now.Now()); err != nil {
		return nil, err
	}
	if err := u.repo.UpdateStatus(ctx, user.ID, user.Status, user.UpdatedAt); err != nil {
		return nil, err
	}
	if err := u.publish(ctx, identity.EventUserDisabled, map[string]string{"id": user.ID}); err != nil {
		return nil, err
	}
	return user, nil
}

// EnableUser 启用（admin 面）。
func (u *UseCase) EnableUser(ctx context.Context, id string) (*identity.User, error) {
	user, err := u.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if err := user.Enable(u.now.Now()); err != nil {
		return nil, err
	}
	if err := u.repo.UpdateStatus(ctx, user.ID, user.Status, user.UpdatedAt); err != nil {
		return nil, err
	}
	if err := u.publish(ctx, identity.EventUserEnabled, map[string]string{"id": user.ID}); err != nil {
		return nil, err
	}
	return user, nil
}

// usersSpec 是用户列表的分页规约（keyset：created_at DESC + id 决胜）。
var usersSpec = crud.Spec{
	Modes:        crud.ModeSetKeyset,
	Sortable:     map[string]string{"createdAt": "u.created_at", "id": "u.id"},
	Tiebreak:     "id",
	DefaultLimit: 20,
	MaxLimit:     100,
}

// ListUsersResult 是用户列表用例的产物。
type ListUsersResult struct {
	Users     []*identity.User
	NextToken string
	TotalSize int32
}

// ListUsers 列用户（admin 面，keyset 分页）。
func (u *UseCase) ListUsers(ctx context.Context, pageSize int32, pageToken string) (*ListUsersResult, error) {
	q, err := crud.Parse[any](usersSpec, crud.RawParams{After: pageToken, Limit: pageSize, OrderBy: "createdAt desc"}, nil)
	if err != nil {
		return nil, err
	}
	users, total, err := u.repo.List(ctx, q.PagePlan)
	if err != nil {
		return nil, err
	}
	next := ""
	if crud.HasMore(int32(len(users)), q.Limit) && len(users) > 0 {
		last := users[len(users)-1]
		if next, err = crud.Cursor(q, []string{last.CreatedAt.UTC().Format(time.RFC3339Nano), last.ID}); err != nil {
			return nil, err
		}
	}
	return &ListUsersResult{Users: users, NextToken: next, TotalSize: int32(total)}, nil
}

// oneShotToken 签发一次性令牌（purpose 写进 custom claims——单签发器
// 多用途；验证时比对，跨用途令牌拒绝）。有效期由签发器构造（15 分钟）。
func (u *UseCase) oneShotToken(user *identity.User, purpose string) (string, error) {
	tok, err := u.oneShot.Sign(user.ID, map[string]string{"email": user.Email, "purpose": purpose})
	return tok.Value, err
}

// verifyOneShot 验一次性令牌并回读用户（用途不符/过期/用户不存在统一
// ErrInvalidToken——不泄露差异）。
func (u *UseCase) verifyOneShot(token, purpose string) (*identity.User, error) {
	claims, err := u.oneShotV.Verify(token, u.now.Now())
	if err != nil || claims.Custom["purpose"] != purpose {
		return nil, identity.ErrInvalidToken
	}
	user, err := u.repo.GetByID(context.Background(), claims.Subject)
	if err != nil {
		return nil, identity.ErrInvalidToken
	}
	return user, nil
}

// changePassword 是 ChangePassword/ConfirmPasswordReset 共用的改密内核
// （校验强度 → Argon2id → 白名单落库 → 改密事件）。
func (u *UseCase) changePassword(ctx context.Context, user *identity.User, newPassword string) error {
	if err := identity.ValidatePassword(newPassword); err != nil {
		return err
	}
	hash, err := password.Hash(newPassword)
	if err != nil {
		return err
	}
	user.PasswordHash = hash
	user.UpdatedAt = u.now.Now()
	return u.tx.Run(ctx, func(ctx context.Context) error {
		if err := u.repo.UpdatePassword(ctx, user); err != nil {
			return err
		}
		return u.publish(ctx, identity.EventUserPasswordChanged, map[string]string{"id": user.ID})
	})
}
