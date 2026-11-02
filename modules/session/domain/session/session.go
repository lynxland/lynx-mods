// Package session 是会话模块的领域层：Session 聚合（可吊销的登录会话）、
// 不变量与仓储端口。
//
// 契约：不 import infra/app/api；跨模块只允许 import 他模块 domain 包。
// 本模块声明 DependsOn: identity——凭证核验消费 identity 的 domain
// （Repository 端口 + User 聚合 + INVALID_CREDENTIALS/USER_DISABLED 哨兵，
// 客户端从 identity/session 两个登录端点看到一致的错误码），改密联动消费
// identity 的事件目录（identity.user.password-changed，经 Deps.Bus 订阅，
// 不 import 代码）。
package session

import (
	"fmt"
	"time"

	"github.com/lynxland/appkit/errcode"
)

// space 是本模块的错误码区段（契约⑤）。凭证类错误沿用 identity 区段
// 的哨兵（见包注释），本区段只定义会话自身错误。
var space = errcode.MustRegister("session")

// 哨兵错误：api 层转 grpc/status 时附带 ErrorInfo（Domain=session）。
var (
	ErrSessionNotFound = space.New("SESSION_NOT_FOUND", "session not found")
	ErrSessionRevoked  = space.New("SESSION_REVOKED", "session is revoked")
	ErrSessionExpired  = space.New("SESSION_EXPIRED", "session is expired")
	ErrRefreshRejected = space.New("REFRESH_REJECTED", "refresh token is rejected")
)

// 会话状态。
const (
	StatusActive  = "active"
	StatusRevoked = "revoked"
)

// 吊销原因（诊断用，随事件与仓储记录）。
const (
	RevokeLogout       = "logout"
	RevokeLogoutAll    = "logout-all"
	RevokePassword     = "password-changed"
	RevokeRefreshReuse = "refresh-reuse"
)

// 本模块发布的事件目录（契约②）：app 层发布与 Module().PublishedEvents
// 共用同一常量，item.json 的 publishedEvents 与此同步。
const (
	EventSessionCreated   = "session.session.created"
	EventSessionRefreshed = "session.session.refreshed"
	EventSessionRevoked   = "session.session.revoked"
)

// 消费的跨模块事件目录（契约②：字符串引用 identity 的目录，不 import
// 其代码；Module().ConsumedEvents 声明后由 appkit 启动期校验确有发布者）。
const IdentityUserPasswordChanged = "identity.user.password-changed"

// Session 是一次可吊销的登录会话。访问令牌短时效无状态（不查库），
// 会话状态是吊销的裁决点：Refresh 与认证器回读本聚合。
type Session struct {
	ID              string
	UserID          string
	RefreshJTI      string // 当前有效刷新令牌的 jti（轮换检测：旧 jti 再现即盗用信号）
	Status          string
	CreatedAt       time.Time
	LastRefreshedAt time.Time
	ExpiresAt       time.Time // 绝对过期（自创建起算，轮换不续期——滑动窗口留给后续）
	RevokedAt       time.Time
	RevokeReason    string
}

// NewSession 构造新会话（refreshJTI 是首个刷新令牌的 jti）。
func NewSession(id, userID, refreshJTI string, now, expiresAt time.Time) (*Session, error) {
	if id == "" || userID == "" || refreshJTI == "" {
		return nil, fmt.Errorf("session: empty id/userID/refreshJTI")
	}
	return &Session{
		ID: id, UserID: userID, RefreshJTI: refreshJTI,
		Status:          StatusActive,
		CreatedAt:       now,
		LastRefreshedAt: now,
		ExpiresAt:       expiresAt,
	}, nil
}

// IsActive 报告会话未吊销且未过期。
func (s *Session) IsActive(now time.Time) bool {
	return s.Status == StatusActive && !now.After(s.ExpiresAt)
}

// Rotate 换入新刷新令牌的 jti（每次刷新轮换）。
func (s *Session) Rotate(newJTI string, now time.Time) error {
	if newJTI == "" {
		return ErrRefreshRejected
	}
	s.RefreshJTI = newJTI
	s.LastRefreshedAt = now
	return nil
}

// Revoke 吊销（幂等：已吊销保持原吊销时刻与原因）。
func (s *Session) Revoke(reason string, now time.Time) {
	if s.Status == StatusRevoked {
		return
	}
	s.Status = StatusRevoked
	s.RevokedAt = now
	s.RevokeReason = reason
}
