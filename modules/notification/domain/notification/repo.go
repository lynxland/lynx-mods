package notification

import (
	"context"
	"time"

	"github.com/lynxland/appkit/crud"
)

// Repository 是 Notification 聚合的持久化端口（infra 实现，经 Assemble
// 注入）。方法一律接收 ctx：适配器从 ctx 取事务连接（appkit/uow/buntx），
// 端口签名不得出现驱动类型。
type Repository interface {
	// Create 插入通知（append 事实，无更新路径——已读走 MarkRead）。
	Create(ctx context.Context, n *Notification) error
	// ListByUser 按分页计划（crud v2 keyset：created_at DESC + id 决胜）
	// 返回收件人的一页与总数（unreadOnly 时仅未读；游标不绑定滤——
	// 翻页中途切换 unread_only 请重新起页）。
	ListByUser(ctx context.Context, userID string, plan crud.PagePlan, unreadOnly bool) ([]*Notification, int, error)
	// CountUnread 统计收件人未读数（角标）。
	CountUnread(ctx context.Context, userID string) (int, error)
	// MarkRead 按 ID 标记已读；userID 收敛可见性（只许标本人的，
	// 他人通知与不存在统一映射 ErrNotificationNotFound——不泄露存在性
	// 差异），幂等，返回是否发生写入。
	MarkRead(ctx context.Context, id, userID string, readAt time.Time) (bool, error)
	// MarkAllRead 把收件人全部未读置为已读，返回标记条数。
	MarkAllRead(ctx context.Context, userID string, readAt time.Time) (int64, error)

	// UpsertPreference 写入一条投递偏好（复合键整写）。
	UpsertPreference(ctx context.Context, p Preference) error
	// ListPreferences 返回用户的全部偏好行（未配置的类型不在其中——
	// 调用方按「未配置=启用」语义补全）。
	ListPreferences(ctx context.Context, userID string) ([]Preference, error)
}
