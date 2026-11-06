// Package access 是授权域（account 模块）的基础设施层：bun 仓储。
// （ADR-0009 合并后）Grant 目标校验与 bootstrap 管理员解析直接复用
// identity 域的完整仓储——不再需要跨模块只读适配器。
package access

import (
	"context"
	"time"

	"github.com/lynxland/lynx-start/modules/account/domain/account/access"

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
