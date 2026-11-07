package session

import (
	"context"
	"time"

	"github.com/lynxland/appkit/crud"
)

// Repository 是 Session 聚合的持久化端口（infra 实现，经 Assemble 注入）。
// 方法一律接收 ctx：适配器从 ctx 取事务连接（appkit/uow/buntx），
// 端口签名不得出现驱动类型。
type Repository interface {
	Create(ctx context.Context, s *Session) error
	// Get 按会话 ID 取；缺失映射 ErrSessionNotFound。
	Get(ctx context.Context, id string) (*Session, error)
	// Rotate 条件更新（CAS：WHERE 携带旧 jti——并发 Refresh 竞态修复；
	// 冲突返回 ErrRotateConflict，app 层按重放处理）。
	Rotate(ctx context.Context, s *Session, oldJTI string) error
	// Revoke 吊销单个会话（幂等）。
	Revoke(ctx context.Context, id, reason string, at time.Time) error
	// RevokeByUser 吊销用户全部活跃会话（改密联动/全端下线），返回吊销数。
	RevokeByUser(ctx context.Context, userID, reason string, at time.Time) (int, error)
}

// 设备管理扩展（v0.7.0）。
type RepositoryDeviceManagement interface {
	// ListByUser 列用户会话（keyset：created_at DESC + id 决胜）。
	ListByUser(ctx context.Context, userID string, plan crud.PagePlan) ([]*Session, int, error)
	// RevokeOwned 吊销本人的指定会话（他人/不存在统一 ErrSessionNotFound，
	// 幂等）。
	RevokeOwned(ctx context.Context, id, userID, reason string, at time.Time) error
}
