// Package session 是会话模块的基础设施层：bun 仓储、事件订阅器与迁移。
package session

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/lynxland/appkit/contrib/bunx"
	"github.com/lynxland/appkit/contrib/bunx/buntx"
	"github.com/lynxland/appkit/crud"
	"github.com/lynxland/appkit/occ"
	"github.com/lynxland/lynx-start/modules/account/domain/account/session"
	"github.com/uptrace/bun"
)

// sessionModel 是 PO（持久化对象）：bun 标签在此，领域聚合不染 ORM。
// 不设指向 identity_users 的外键——契约④：迁移链隔离，跨模块只靠
// 事件目录与 domain 端口协同。
type sessionModel struct {
	bun.BaseModel `bun:"table:session_sessions,alias:s"`

	ID              string    `bun:"id,pk"`
	UserID          string    `bun:"user_id,notnull"`
	RefreshJTI      string    `bun:"refresh_jti,notnull"`
	Status          string    `bun:"status,notnull"`
	CreatedAt       time.Time `bun:"created_at,notnull"`
	LastRefreshedAt time.Time `bun:"last_refreshed_at,notnull"`
	ExpiresAt       time.Time `bun:"expires_at,notnull"`
	RevokedAt       time.Time `bun:"revoked_at,nullzero"`
	RevokeReason    string    `bun:"revoke_reason,nullzero"`
	UserAgent       string    `bun:"user_agent,notnull"`
}

// Repo 实现 session.Repository。
type Repo struct {
	db *bun.DB
}

// NewRepository 构造仓储（Assemble 调用）。
func NewRepository(db *bun.DB) *Repo { return &Repo{db: db} }

// transitionRevoke 是会话状态机的唯一吊销迁移声明（occ.Transition，
// dogfood 首循环③）：active → revoked。起点守卫由 bunx.IfState 编译，
// 状态机合法性单一事实源——未来增加 paused 等状态只改这里。
var transitionRevoke = occ.Transition{From: []string{session.StatusActive}, To: session.StatusRevoked}

var _ session.Repository = (*Repo)(nil)

// Create 插入（加入调用方事务）。
func (r *Repo) Create(ctx context.Context, s *session.Session) error {
	m := toModel(s)
	_, err := buntx.Conn(ctx, r.db).NewInsert().Model(&m).Exec(ctx)
	return err
}

// Get 按主键取；缺失映射领域哨兵错误。
func (r *Repo) Get(ctx context.Context, id string) (*session.Session, error) {
	m := new(sessionModel)
	err := buntx.Conn(ctx, r.db).NewSelect().Model(m).
		Where("s.id = ?", id).Scan(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, session.ErrSessionNotFound
	}
	if err != nil {
		return nil, err
	}
	return toDomain(m), nil
}

// Rotate 条件更新（v0.7.0 竞态修复）：WHERE 携带**旧** refresh_jti 期望
// 值——两个并发 Refresh 同 sid 时只有一个 CAS 成功，另一个影响 0 行
// （app 层按「旧 jti 重放」处理：吊销整个会话，rotation 防重放闭环）。
// oldJTI 是本次轮换前的 jti（= 聚合 Rotate 前的值，由 app 传入）。
func (r *Repo) Rotate(ctx context.Context, s *session.Session, oldJTI string) error {
	res, err := buntx.Conn(ctx, r.db).NewUpdate().
		Model((*sessionModel)(nil)).
		Set("refresh_jti = ?", s.RefreshJTI).
		Set("last_refreshed_at = ?", s.LastRefreshedAt).
		Where("id = ? AND status = ? AND refresh_jti = ?", s.ID, session.StatusActive, oldJTI).
		Exec(ctx)
	if err != nil {
		return err
	}
	if n, err := res.RowsAffected(); err == nil && n == 0 {
		return session.ErrRotateConflict
	}
	return nil
}

// Revoke 吊销单个会话（幂等：只更新活跃行）。
func (r *Repo) Revoke(ctx context.Context, id, reason string, at time.Time) error {
	_, err := buntx.Conn(ctx, r.db).NewUpdate().
		Model((*sessionModel)(nil)).
		Set("status = ?", session.StatusRevoked).
		Set("revoked_at = ?", at).
		Set("revoke_reason = ?", reason).
		Where("id = ? AND status = ?", id, session.StatusActive).
		Exec(ctx)
	return err
}

// RevokeByUser 吊销用户全部活跃会话，返回吊销行数。
func (r *Repo) RevokeByUser(ctx context.Context, userID, reason string, at time.Time) (int, error) {
	res, err := bunx.IfState(buntx.Conn(ctx, r.db).NewUpdate().
		Model((*sessionModel)(nil)), "status", transitionRevoke).
		Set("revoked_at = ?", at).
		Set("revoke_reason = ?", reason).
		Where("user_id = ?", userID).
		Exec(ctx)
	if err != nil {
		return 0, err
	}
	n, err := res.RowsAffected()
	return int(n), err
}

func toModel(s *session.Session) sessionModel {
	return sessionModel{
		ID: s.ID, UserID: s.UserID, RefreshJTI: s.RefreshJTI, Status: s.Status,
		CreatedAt: s.CreatedAt, LastRefreshedAt: s.LastRefreshedAt, ExpiresAt: s.ExpiresAt,
		RevokedAt: s.RevokedAt, RevokeReason: s.RevokeReason, UserAgent: s.UserAgent,
	}
}

func toDomain(m *sessionModel) *session.Session {
	return &session.Session{
		ID: m.ID, UserID: m.UserID, RefreshJTI: m.RefreshJTI, Status: m.Status,
		CreatedAt: m.CreatedAt, LastRefreshedAt: m.LastRefreshedAt, ExpiresAt: m.ExpiresAt,
		RevokedAt: m.RevokedAt, RevokeReason: m.RevokeReason, UserAgent: m.UserAgent,
	}
}

// ListByUser 列用户会话（keyset：created_at DESC + id 决胜——设备管理）。
func (r *Repo) ListByUser(ctx context.Context, userID string, plan crud.PagePlan) ([]*session.Session, int, error) {
	var rows []sessionModel
	if err := bunx.ApplyPage(buntx.Conn(ctx, r.db).NewSelect().Model(&rows).
		Where("s.user_id = ?", userID), plan).Scan(ctx); err != nil {
		return nil, 0, err
	}
	count, err := buntx.Conn(ctx, r.db).NewSelect().Model((*sessionModel)(nil)).
		Where("user_id = ?", userID).Count(ctx)
	if err != nil {
		return nil, 0, err
	}
	out := make([]*session.Session, 0, len(rows))
	for i := range rows {
		out = append(out, toDomain(&rows[i]))
	}
	return out, int(count), nil
}

// RevokeOwned 吊销本人的指定会话（自助踢设备）：收敛可见性——他人
// 会话与不存在统一 ErrSessionNotFound（不泄露存在性差异），幂等。
func (r *Repo) RevokeOwned(ctx context.Context, id, userID, reason string, at time.Time) error {
	res, err := buntx.Conn(ctx, r.db).NewUpdate().
		Model((*sessionModel)(nil)).
		Set("status = ?", session.StatusRevoked).
		Set("revoked_at = ?", at).
		Set("revoke_reason = ?", reason).
		Where("id = ? AND user_id = ? AND status = ?", id, userID, session.StatusActive).
		Exec(ctx)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		if _, gerr := r.Get(ctx, id); gerr != nil || r.ownerOf(ctx, id) != userID {
			return session.ErrSessionNotFound
		}
		// 存在且是本人的——已吊销/过期，幂等成功。
	}
	return nil
}

func (r *Repo) ownerOf(ctx context.Context, id string) string {
	m := new(sessionModel)
	if err := buntx.Conn(ctx, r.db).NewSelect().Model(m).
		Column("s.user_id").Where("s.id = ?", id).Scan(ctx); err != nil {
		return ""
	}
	return m.UserID
}
