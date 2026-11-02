// Package access 是授权模块的基础设施层：bun 仓储、identity 用户只读
// 适配器与迁移。
package access

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/lynxland/lynx-mods/modules/access/domain/access"
	"github.com/lynxland/lynx-mods/modules/identity/domain/identity"

	"github.com/lynxland/appkit/contrib/bunx/buntx"
	"github.com/uptrace/bun"
)

// assignmentModel 是 PO（持久化对象）：bun 标签在此，领域聚合不染 ORM。
// user_id 只存 ID 不设外键——契约④：迁移链隔离，跨模块引用只存 ID。
type assignmentModel struct {
	bun.BaseModel `bun:"table:access_role_assignments,alias:a"`

	UserID    string    `bun:"user_id,pk"`
	Role      string    `bun:"role,pk"`
	GrantedAt time.Time `bun:"granted_at,notnull"`
}

// Repo 实现 access.Repository。
type Repo struct {
	db *bun.DB
}

// NewRepository 构造仓储（Assemble 调用）。
func NewRepository(db *bun.DB) *Repo { return &Repo{db: db} }

var _ access.Repository = (*Repo)(nil)

// Assign 幂等授予（ON CONFLICT DO NOTHING），返回是否发生写入。
func (r *Repo) Assign(ctx context.Context, a *access.Assignment) (bool, error) {
	m := assignmentModel{UserID: a.UserID, Role: a.Role, GrantedAt: a.GrantedAt}
	res, err := buntx.Conn(ctx, r.db).NewInsert().Model(&m).
		On("CONFLICT (user_id, role) DO NOTHING").Exec(ctx)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// Revoke 幂等回收，返回是否发生删除。
func (r *Repo) Revoke(ctx context.Context, userID, role string) (bool, error) {
	res, err := buntx.Conn(ctx, r.db).NewDelete().
		Model((*assignmentModel)(nil)).
		Where("user_id = ? AND role = ?", userID, role).Exec(ctx)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// RolesForUser 返回用户全部角色名。
func (r *Repo) RolesForUser(ctx context.Context, userID string) ([]string, error) {
	var roles []string
	err := buntx.Conn(ctx, r.db).NewSelect().
		Model((*assignmentModel)(nil)).
		Column("a.role").
		Where("a.user_id = ?", userID).Scan(ctx, &roles)
	if err != nil {
		return nil, err
	}
	return roles, nil
}

// userReader 是 identity.Repository 的**只读**实现（DependsOn 的消费面）：
// Grant 目标校验与 bootstrap 管理员解析用。identity_users 的写路径永远
// 归 identity 模块——写入方法一律报错，防止所有权漂移。
type userReader struct {
	db *bun.DB
}

// NewUserReader 构造只读适配器（Assemble 调用）。
func NewUserReader(db *bun.DB) *userReader { return &userReader{db: db} }

var _ identity.Repository = (*userReader)(nil)

type userRow struct {
	bun.BaseModel `bun:"table:identity_users,alias:u"`

	ID     string `bun:"id"`
	Email  string `bun:"email"`
	Status string `bun:"status"`
}

func (r *userReader) GetByID(ctx context.Context, id string) (*identity.User, error) {
	row := new(userRow)
	err := r.db.NewSelect().Model(row).
		Column("u.id", "u.email", "u.status").
		Where("u.id = ?", id).Scan(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, identity.ErrUserNotFound
	}
	if err != nil {
		return nil, err
	}
	return &identity.User{ID: row.ID, Email: row.Email, Status: row.Status}, nil
}

func (r *userReader) GetByEmail(ctx context.Context, email string) (*identity.User, error) {
	row := new(userRow)
	err := r.db.NewSelect().Model(row).
		Column("u.id", "u.email", "u.status").
		Where("u.email = ?", identity.NormalizeEmail(email)).Scan(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, identity.ErrUserNotFound
	}
	if err != nil {
		return nil, err
	}
	return &identity.User{ID: row.ID, Email: row.Email, Status: row.Status}, nil
}

func (r *userReader) Create(context.Context, *identity.User) error {
	return errors.New("access: userReader is read-only (Create belongs to identity)")
}

func (r *userReader) UpdatePassword(context.Context, *identity.User) error {
	return errors.New("access: userReader is read-only (UpdatePassword belongs to identity)")
}
