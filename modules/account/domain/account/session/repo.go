package session

import (
	"context"
	"time"
)

// Repository 是 Session 聚合的持久化端口（infra 实现，经 Assemble 注入）。
// 方法一律接收 ctx：适配器从 ctx 取事务连接（appkit/uow/buntx），
// 端口签名不得出现驱动类型。
type Repository interface {
	Create(ctx context.Context, s *Session) error
	// Get 按会话 ID 取；缺失映射 ErrSessionNotFound。
	Get(ctx context.Context, id string) (*Session, error)
	// Rotate 只写 refresh_jti/last_refreshed_at 白名单列（轮换）。
	Rotate(ctx context.Context, s *Session) error
	// Revoke 吊销单个会话（幂等）。
	Revoke(ctx context.Context, id, reason string, at time.Time) error
	// RevokeByUser 吊销用户全部活跃会话（改密联动/全端下线），返回吊销数。
	RevokeByUser(ctx context.Context, userID, reason string, at time.Time) (int, error)
}
