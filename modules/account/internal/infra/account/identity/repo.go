// Package identity 是身份模块的基础设施层：bun 仓储实现与迁移。
package identity

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/lynxland/appkit/contrib/bunx"
	"github.com/lynxland/appkit/contrib/bunx/buntx"
	"github.com/lynxland/appkit/crud"
	"github.com/lynxland/appkit/storeerr"
	"github.com/lynxland/lynx-start/modules/account/domain/account/identity"
	"github.com/uptrace/bun"
)

// userModel 是 PO（持久化对象）：bun 标签在此，领域聚合不染 ORM。
type userModel struct {
	bun.BaseModel `bun:"table:identity_users,alias:u"`

	ID            string    `bun:"id,pk"`
	Email         string    `bun:"email,notnull,unique"`
	PasswordHash  string    `bun:"password_hash,notnull"`
	DisplayName   string    `bun:"display_name,notnull"`
	Status        string    `bun:"status,notnull"`
	EmailVerified bool      `bun:"email_verified,notnull"`
	CreatedAt     time.Time `bun:"created_at,notnull"`
	UpdatedAt     time.Time `bun:"updated_at,notnull"`
}

// Repo 实现 identity.Repository。
type Repo struct {
	db *bun.DB
}

// NewRepository 构造仓储（Assemble 调用）。
func NewRepository(db *bun.DB) *Repo { return &Repo{db: db} }

var _ identity.Repository = (*Repo)(nil)

// Create 插入（加入调用方事务）；email 唯一冲突映射领域哨兵错误。
func (r *Repo) Create(ctx context.Context, user *identity.User) error {
	m := toModel(user)
	_, err := buntx.Conn(ctx, r.db).NewInsert().Model(&m).Exec(ctx)
	if isUniqueViolation(err) {
		return identity.ErrEmailTaken
	}
	return err
}

// GetByID 按主键取；缺失映射领域哨兵错误。
func (r *Repo) GetByID(ctx context.Context, id string) (*identity.User, error) {
	m := new(userModel)
	err := buntx.Conn(ctx, r.db).NewSelect().Model(m).
		Where("u.id = ?", id).Scan(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, identity.ErrUserNotFound
	}
	if err != nil {
		return nil, err
	}
	return toDomain(m), nil
}

// GetByEmail 按唯一索引取；缺失映射领域哨兵错误。
func (r *Repo) GetByEmail(ctx context.Context, email string) (*identity.User, error) {
	m := new(userModel)
	err := buntx.Conn(ctx, r.db).NewSelect().Model(m).
		Where("u.email = ?", email).Scan(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, identity.ErrUserNotFound
	}
	if err != nil {
		return nil, err
	}
	return toDomain(m), nil
}

// UpdatePassword 只写白名单列（bun 显式 Set 护栏：禁止整模型覆盖）。
func (r *Repo) UpdatePassword(ctx context.Context, user *identity.User) error {
	res, err := buntx.Conn(ctx, r.db).NewUpdate().
		Model((*userModel)(nil)).
		Set("password_hash = ?", user.PasswordHash).
		Set("updated_at = ?", user.UpdatedAt).
		Where("id = ?", user.ID).
		Exec(ctx)
	if err != nil {
		return err
	}
	if n, err := res.RowsAffected(); err == nil && n == 0 {
		return identity.ErrUserNotFound
	}
	return nil
}

// isUniqueViolation 识别唯一约束冲突——经 bunx.MapError 归一为
// storeerr 哨兵（pgdriver 值类型坑由适配层收口）。
func isUniqueViolation(err error) bool {
	return storeerr.IsAlreadyExists(bunx.MapError(err))
}

func toModel(u *identity.User) userModel {
	return userModel{
		ID: u.ID, Email: u.Email, PasswordHash: u.PasswordHash,
		DisplayName: u.DisplayName, Status: u.Status, EmailVerified: u.EmailVerified,
		CreatedAt: u.CreatedAt, UpdatedAt: u.UpdatedAt,
	}
}

func toDomain(m *userModel) *identity.User {
	return &identity.User{
		ID: m.ID, Email: m.Email, PasswordHash: m.PasswordHash,
		DisplayName: m.DisplayName, Status: m.Status, EmailVerified: m.EmailVerified,
		CreatedAt: m.CreatedAt, UpdatedAt: m.UpdatedAt,
	}
}

// UpdateStatus 只写 status/updated_at（Disable/Enable）。
func (r *Repo) UpdateStatus(ctx context.Context, id, status string, now time.Time) error {
	res, err := buntx.Conn(ctx, r.db).NewUpdate().
		Model((*userModel)(nil)).
		Set("status = ?", status).
		Set("updated_at = ?", now).
		Where("id = ?", id).
		Exec(ctx)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return identity.ErrUserNotFound
	}
	return nil
}

// UpdateEmailVerified 只写 email_verified/updated_at（幂等）。
func (r *Repo) UpdateEmailVerified(ctx context.Context, id string, verified bool, now time.Time) error {
	res, err := buntx.Conn(ctx, r.db).NewUpdate().
		Model((*userModel)(nil)).
		Set("email_verified = ?", verified).
		Set("updated_at = ?", now).
		Where("id = ?", id).
		Exec(ctx)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return identity.ErrUserNotFound
	}
	return nil
}

// List 用户分页（keyset：created_at DESC + id 决胜）。
func (r *Repo) List(ctx context.Context, plan crud.PagePlan) ([]*identity.User, int, error) {
	var rows []userModel
	if err := bunx.ApplyPage(buntx.Conn(ctx, r.db).NewSelect().Model(&rows), plan).Scan(ctx); err != nil {
		return nil, 0, err
	}
	count, err := buntx.Conn(ctx, r.db).NewSelect().Model((*userModel)(nil)).Count(ctx)
	if err != nil {
		return nil, 0, err
	}
	users := make([]*identity.User, 0, len(rows))
	for i := range rows {
		users = append(users, toDomain(&rows[i]))
	}
	return users, int(count), nil
}
