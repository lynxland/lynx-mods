// Package notification 是通知模块的基础设施层：bun 仓储、事件订阅
// watcher 与迁移。
package notification

import (
	"context"
	"encoding/json"
	"time"

	"github.com/lynxland/appkit/contrib/bunx/buntx"
	"github.com/lynxland/lynx-mods/modules/notification/domain/notification"

	"github.com/uptrace/bun"
)

// notificationModel 是通知 PO（持久化对象）：bun 标签在此，领域聚合
// 不染 ORM。append-only：无整行 Update（已读走显式白名单列）。
type notificationModel struct {
	bun.BaseModel `bun:"table:notification_notifications,alias:n"`

	ID        string          `bun:"id,pk"`
	UserID    string          `bun:"user_id,notnull"`
	Type      string          `bun:"type,notnull"`
	Title     string          `bun:"title,notnull"`
	Body      string          `bun:"body,notnull"`
	Payload   json.RawMessage `bun:"payload,type:jsonb"`
	ReadAt    *time.Time      `bun:"read_at"`
	CreatedAt time.Time       `bun:"created_at,notnull"`
}

// Repo 实现 notification.Repository。
type Repo struct {
	db *bun.DB
}

// NewRepository 构造仓储（Assemble 调用）。
func NewRepository(db *bun.DB) *Repo { return &Repo{db: db} }

var _ notification.Repository = (*Repo)(nil)

// Create 插入通知（事实追加）。
func (r *Repo) Create(ctx context.Context, n *notification.Notification) error {
	m := toModel(n)
	_, err := buntx.Conn(ctx, r.db).NewInsert().Model(&m).Exec(ctx)
	return err
}

// ListByUser 按 created_at DESC, id DESC 取一页与总数（UnreadOnly 仅未读）。
func (r *Repo) ListByUser(ctx context.Context, userID string, q notification.Query) ([]*notification.Notification, int, error) {
	var rows []notificationModel
	conn := buntx.Conn(ctx, r.db)
	listQuery := conn.NewSelect().Model(&rows).
		Where("n.user_id = ?", userID).
		Order("n.created_at DESC", "n.id DESC").
		Limit(int64(q.PageSize)).Offset(int64(q.Offset))
	countQuery := conn.NewSelect().Model((*notificationModel)(nil)).
		Where("user_id = ?", userID)
	if q.UnreadOnly {
		listQuery = listQuery.Where("n.read_at IS NULL")
		countQuery = countQuery.Where("read_at IS NULL")
	}
	if err := listQuery.Scan(ctx); err != nil {
		return nil, 0, err
	}
	total, err := countQuery.Count(ctx)
	if err != nil {
		return nil, 0, err
	}
	items := make([]*notification.Notification, 0, len(rows))
	for i := range rows {
		items = append(items, toDomain(&rows[i]))
	}
	return items, int(total), nil
}

// CountUnread 统计未读数。
func (r *Repo) CountUnread(ctx context.Context, userID string) (int, error) {
	n, err := buntx.Conn(ctx, r.db).NewSelect().Model((*notificationModel)(nil)).
		Where("user_id = ? AND read_at IS NULL", userID).Count(ctx)
	return int(n), err
}

// MarkRead 按 ID 置已读；user_id 条件收敛可见性（他人通知与不存在
// 统一 ErrNotificationNotFound），幂等。
func (r *Repo) MarkRead(ctx context.Context, id, userID string, readAt time.Time) (bool, error) {
	res, err := buntx.Conn(ctx, r.db).NewUpdate().
		Model((*notificationModel)(nil)).
		Set("read_at = ?", readAt).
		Where("id = ? AND user_id = ? AND read_at IS NULL", id, userID).
		Exec(ctx)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	if n == 0 {
		// 不存在 / 他人通知 / 本就未读——统一哨兵（幂等已读不视为错误，
		// 但调用方无法区分也无需区分：终态都是「已读且可见性合法」）。
		exists, err := buntx.Conn(ctx, r.db).NewSelect().Model((*notificationModel)(nil)).
			Where("id = ? AND user_id = ?", id, userID).Exists(ctx)
		if err != nil {
			return false, err
		}
		if !exists {
			return false, notification.ErrNotificationNotFound
		}
	}
	return true, nil
}

// MarkAllRead 把全部未读置已读，返回标记条数。
func (r *Repo) MarkAllRead(ctx context.Context, userID string, readAt time.Time) (int64, error) {
	res, err := buntx.Conn(ctx, r.db).NewUpdate().
		Model((*notificationModel)(nil)).
		Set("read_at = ?", readAt).
		Where("user_id = ? AND read_at IS NULL", userID).
		Exec(ctx)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func toModel(n *notification.Notification) notificationModel {
	var payload json.RawMessage
	if n.Payload != nil {
		// 载荷经 json.Marshal 编码（与各模块 publish 同款纪律：map 手搓
		// 字符串会在值含引号时产生非法 JSON）。
		raw, err := json.Marshal(n.Payload)
		if err == nil {
			payload = raw
		}
	}
	return notificationModel{
		ID: n.ID, UserID: n.UserID, Type: n.Type, Title: n.Title, Body: n.Body,
		Payload: payload, ReadAt: n.ReadAt, CreatedAt: n.CreatedAt,
	}
}

func toDomain(m *notificationModel) *notification.Notification {
	var payload map[string]string
	if len(m.Payload) > 0 {
		_ = json.Unmarshal(m.Payload, &payload)
	}
	return &notification.Notification{
		ID: m.ID, UserID: m.UserID, Type: m.Type, Title: m.Title, Body: m.Body,
		Payload: payload, ReadAt: m.ReadAt, CreatedAt: m.CreatedAt,
	}
}
