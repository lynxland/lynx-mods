// Package session 是会话模块的用例层：可吊销登录会话的创建、轮换与
// 吊销。凭证核验不在此复制——CreateSession 消费已认证主体（identity
// 的认证器产出）；跨模块联动（改密全局吊销）经事件目录由 infra 的
// 总线订阅器触发；Refresh 边界经 identity domain 端口 fail-closed 回读
// 用户状态。
package session

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/lynxland/appkit/clock"
	"github.com/lynxland/appkit/crud"
	"github.com/lynxland/appkit/events"
	"github.com/lynxland/appkit/idgen"
	"github.com/lynxland/appkit/jwt"
	"github.com/lynxland/appkit/uow"
	"github.com/lynxland/lynx-start/modules/account/domain/account/identity"
	"github.com/lynxland/lynx-start/modules/account/domain/account/session"
)

// 令牌派生用途（appkit/secretkey purpose 域隔离）：访问/刷新两类令牌
// 互不通用，也与 identity 的令牌互不通用（签发密钥不同即签名不匹配，
// 认证器按链式语义交给下一个）。
const (
	PurposeAccessToken  = "session-access-token"
	PurposeRefreshToken = "session-refresh-token"
)

// Tokens 汇聚会话令牌四件套（Assemble 构造注入）。
type Tokens struct {
	Access        *jwt.Signer
	Refresh       *jwt.Signer
	AccessVerify  *jwt.Verifier
	RefreshVerify *jwt.Verifier
	// RefreshTTL 是会话绝对时长（与刷新令牌 TTL 一致；轮换不续期）。
	RefreshTTL time.Duration
}

// UseCase 是会话模块的用例集。
type UseCase struct {
	repo   session.Repository
	users  identity.Repository // DependsOn: identity 的 domain 端口（Refresh 边界回读）
	tx     uow.Runner
	events events.Publisher
	tokens Tokens
	now    clock.Clock
}

// New 构造用例（Assemble 调用）。c 为空取系统时钟。
func New(repo session.Repository, users identity.Repository, tx uow.Runner, publisher events.Publisher, tokens Tokens, c clock.Clock) *UseCase {
	if c == nil {
		c = clock.System{}
	}
	return &UseCase{repo: repo, users: users, tx: tx, events: publisher, tokens: tokens, now: c}
}

// TokenPair 是一次创建/刷新的产物。
type TokenPair struct {
	Session *session.Session
	Access  *jwt.Token
	Refresh *jwt.Token
}

// CreateSession 把已认证主体升级为可吊销会话（凭证核验归 identity，
// 本模块只消费 principal 事实）。userAgent 是客户端自报的设备元数据。
func (u *UseCase) CreateSession(ctx context.Context, userID, email, userAgent string) (*TokenPair, error) {
	now := u.now.Now()
	sid := idgen.New()
	refresh, err := u.tokens.Refresh.Sign(sid, map[string]string{"uid": userID, "email": email})
	if err != nil {
		return nil, err
	}
	s, err := session.NewSession(sid, userID, refresh.Claims.ID, userAgent, now, now.Add(u.tokens.RefreshTTL))
	if err != nil {
		return nil, err
	}
	err = u.tx.Run(ctx, func(ctx context.Context) error {
		if err := u.repo.Create(ctx, s); err != nil {
			return err
		}
		return u.publish(ctx, session.EventSessionCreated, map[string]string{"id": s.ID, "user_id": s.UserID})
	})
	if err != nil {
		return nil, err
	}
	access, err := u.tokens.Access.Sign(userID, map[string]string{"sid": s.ID, "email": email})
	if err != nil {
		return nil, err
	}
	return &TokenPair{Session: s, Access: access, Refresh: refresh}, nil
}

// Refresh 轮换刷新令牌并签发新访问令牌。旧 jti 再现视为盗用信号：
// 立即吊销整个会话（refresh token rotation 的标准防重放）。用户被删/
// 停用后拒绝刷新——会话最长存活到本次刷新。
func (u *UseCase) Refresh(ctx context.Context, refreshToken string) (*TokenPair, error) {
	claims, err := u.tokens.RefreshVerify.Verify(refreshToken, u.now.Now())
	if err != nil {
		switch {
		case errors.Is(err, jwt.ErrExpired):
			return nil, session.ErrSessionExpired
		default:
			return nil, session.ErrRefreshRejected
		}
	}
	s, err := u.repo.Get(ctx, claims.Subject)
	if err != nil {
		return nil, session.ErrRefreshRejected
	}
	if claims.ID != s.RefreshJTI {
		// 旧刷新令牌重放：吊销会话，拒绝本次与后续一切刷新。
		if err := u.revoke(ctx, s, session.RevokeRefreshReuse); err != nil {
			return nil, err
		}
		return nil, session.ErrRefreshRejected
	}
	switch {
	case s.Status == session.StatusRevoked:
		return nil, session.ErrSessionRevoked
	case u.now.Now().After(s.ExpiresAt):
		return nil, session.ErrSessionExpired
	}
	// fail-closed 回读用户状态（DependsOn: identity domain 端口）。
	userID := claims.Custom["uid"]
	user, err := u.users.GetByID(ctx, userID)
	if err != nil {
		if errors.Is(err, identity.ErrUserNotFound) {
			return nil, session.ErrRefreshRejected
		}
		return nil, err
	}
	if !user.IsActive() {
		return nil, identity.ErrUserDisabled
	}

	now := u.now.Now()
	email := claims.Custom["email"]
	refresh, err := u.tokens.Refresh.Sign(s.ID, map[string]string{"uid": userID, "email": email})
	if err != nil {
		return nil, err
	}
	oldJTI := s.RefreshJTI
	if err := s.Rotate(refresh.Claims.ID, now); err != nil {
		return nil, err
	}
	err = u.tx.Run(ctx, func(ctx context.Context) error {
		if err := u.repo.Rotate(ctx, s, oldJTI); err != nil {
			// CAS 冲突 = 并发 Refresh 已先行（旧 jti 被换）——与本方法
			// 开头的「旧 jti 重放」同款盗用信号：吊销整个会话。
			if errors.Is(err, session.ErrRotateConflict) {
				if rerr := u.revoke(ctx, s, session.RevokeRefreshReuse); rerr != nil {
					return rerr
				}
				return session.ErrRefreshRejected
			}
			return err
		}
		return u.publish(ctx, session.EventSessionRefreshed, map[string]string{"id": s.ID, "user_id": s.UserID})
	})
	if err != nil {
		return nil, err
	}
	access, err := u.tokens.Access.Sign(userID, map[string]string{"sid": s.ID, "email": email})
	if err != nil {
		return nil, err
	}
	return &TokenPair{Session: s, Access: access, Refresh: refresh}, nil
}

// Logout 吊销当前会话（幂等）。
func (u *UseCase) Logout(ctx context.Context, sessionID string) error {
	s, err := u.repo.Get(ctx, sessionID)
	if err != nil {
		if errors.Is(err, session.ErrSessionNotFound) {
			return nil
		}
		return err
	}
	if s.Status == session.StatusRevoked {
		return nil
	}
	return u.revoke(ctx, s, session.RevokeLogout)
}

// LogoutAll 吊销当前用户全部活跃会话，返回吊销数。
func (u *UseCase) LogoutAll(ctx context.Context, userID string) (int, error) {
	var n int
	err := u.tx.Run(ctx, func(ctx context.Context) error {
		var err error
		n, err = u.repo.RevokeByUser(ctx, userID, session.RevokeLogoutAll, u.now.Now())
		if err != nil {
			return err
		}
		return u.publish(ctx, session.EventSessionRevoked, map[string]string{"user_id": userID, "reason": session.RevokeLogoutAll})
	})
	return n, err
}

// ResolveSession 认证器消费：会话未吊销未过期即放行。用户状态在
// CreateSession（principal 已过 identity 认证器）与 Refresh（显式回读）
// 两个边界保证；访问令牌 TTL 是状态变化的暴露上界。
func (u *UseCase) ResolveSession(ctx context.Context, sessionID string) (*session.Session, error) {
	s, err := u.repo.Get(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	switch {
	case s.Status == session.StatusRevoked:
		return nil, session.ErrSessionRevoked
	case u.now.Now().After(s.ExpiresAt):
		return nil, session.ErrSessionExpired
	}
	return s, nil
}

// OnPasswordChanged 改密联动：吊销该用户全部会话（由事件订阅器触发）。
func (u *UseCase) OnPasswordChanged(ctx context.Context, userID string) error {
	return u.tx.Run(ctx, func(ctx context.Context) error {
		_, err := u.repo.RevokeByUser(ctx, userID, session.RevokePassword, u.now.Now())
		if err != nil {
			return err
		}
		return u.publish(ctx, session.EventSessionRevoked, map[string]string{"user_id": userID, "reason": session.RevokePassword})
	})
}

func (u *UseCase) revoke(ctx context.Context, s *session.Session, reason string) error {
	return u.tx.Run(ctx, func(ctx context.Context) error {
		if err := u.repo.Revoke(ctx, s.ID, reason, u.now.Now()); err != nil {
			return err
		}
		return u.publish(ctx, session.EventSessionRevoked, map[string]string{"id": s.ID, "user_id": s.UserID, "reason": reason})
	})
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

// ---- 设备管理（v0.7.0）----

// deviceRepository 是设备管理扩展端口（infra Repo 一并实现；与 domain 的
// RepositoryDeviceManagement 同形——app 侧持有窄视图）。
type deviceRepository interface {
	ListByUser(ctx context.Context, userID string, plan crud.PagePlan) ([]*session.Session, int, error)
	RevokeOwned(ctx context.Context, id, userID, reason string, at time.Time) error
}

// sessionsSpec 是会话列表的分页规约（keyset：created_at DESC + id 决胜）。
var sessionsSpec = crud.Spec{
	Modes:        crud.ModeSetKeyset,
	Sortable:     map[string]string{"createdAt": "s.created_at", "id": "s.id"},
	Tiebreak:     "id",
	DefaultLimit: 20,
	MaxLimit:     100,
}

// ListSessionsResult 是会话列表用例的产物。
type ListSessionsResult struct {
	Sessions  []*session.Session
	NextToken string
	TotalSize int32
}

// ListMySessions 列当前用户会话（自助——设备管理；含已吊销尾巴）。
func (u *UseCase) ListMySessions(ctx context.Context, userID string, pageSize int32, pageToken string) (*ListSessionsResult, error) {
	q, err := crud.Parse[any](sessionsSpec, crud.RawParams{After: pageToken, Limit: pageSize, OrderBy: "createdAt desc"}, nil)
	if err != nil {
		return nil, err
	}
	dr, ok := u.repo.(deviceRepository)
	if !ok {
		return nil, fmt.Errorf("session: repository lacks device management support")
	}
	items, total, err := dr.ListByUser(ctx, userID, q.PagePlan)
	if err != nil {
		return nil, err
	}
	next := ""
	if crud.HasMore(int32(len(items)), q.Limit) && len(items) > 0 {
		last := items[len(items)-1]
		if next, err = crud.Cursor(q, []string{last.CreatedAt.UTC().Format(time.RFC3339Nano), last.ID}); err != nil {
			return nil, err
		}
	}
	return &ListSessionsResult{Sessions: items, NextToken: next, TotalSize: int32(total)}, nil
}

// RevokeMySession 踢出自己的指定会话（幂等；他人会话不可见不可踢）。
func (u *UseCase) RevokeMySession(ctx context.Context, userID, sessionID string) error {
	dr, ok := u.repo.(deviceRepository)
	if !ok {
		return fmt.Errorf("session: repository lacks device management support")
	}
	return dr.RevokeOwned(ctx, sessionID, userID, session.RevokeLogout, u.now.Now())
}

// OnUserDisabled 停用联动：吊销该用户全部会话（订阅 account.user.disabled）。
func (u *UseCase) OnUserDisabled(ctx context.Context, userID string) error {
	return u.OnPasswordChanged(ctx, userID)
}
