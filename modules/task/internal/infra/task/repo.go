// Package task 是任务模块的基础设施层：bun 仓储、organization 成员
// 只读适配器与迁移。
package task

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/lynxland/appkit/contrib/bunx/buntx"
	"github.com/lynxland/lynx-mods/modules/organization/domain/organization"
	"github.com/lynxland/lynx-mods/modules/task/domain/task"

	"github.com/uptrace/bun"
)

// taskModel 是任务 PO（持久化对象）：bun 标签在此，领域聚合不染 ORM。
// organization_id 是租户键（ADR-0008）。
type taskModel struct {
	bun.BaseModel `bun:"table:task_tasks,alias:t"`

	ID             string     `bun:"id,pk"`
	OrganizationID string     `bun:"organization_id,notnull"`
	CreatorID      string     `bun:"creator_id,notnull"`
	Title          string     `bun:"title,notnull"`
	Description    string     `bun:"description,notnull"`
	CompletedAt    *time.Time `bun:"completed_at"`
	CreatedAt      time.Time  `bun:"created_at,notnull"`
	UpdatedAt      time.Time  `bun:"updated_at,notnull"`
}

// Repo 实现 task.Repository。
type Repo struct {
	db *bun.DB
}

// NewRepository 构造仓储（Assemble 调用）。
func NewRepository(db *bun.DB) *Repo { return &Repo{db: db} }

var _ task.Repository = (*Repo)(nil)

// Create 插入任务。
func (r *Repo) Create(ctx context.Context, t *task.Task) error {
	m := toModel(t)
	_, err := buntx.Conn(ctx, r.db).NewInsert().Model(&m).Exec(ctx)
	return err
}

// Get 按组织 + ID 取；缺失或跨组织一律映射 ErrTaskNotFound（租户语境
// 进查询条件——ID 泄露也跨组织取不到，ADR-0008）。
func (r *Repo) Get(ctx context.Context, organizationID, id string) (*task.Task, error) {
	m := new(taskModel)
	err := buntx.Conn(ctx, r.db).NewSelect().Model(m).
		Where("t.organization_id = ? AND t.id = ?", organizationID, id).Scan(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, task.ErrTaskNotFound
	}
	if err != nil {
		return nil, err
	}
	return toDomain(m), nil
}

// List 按 created_at DESC, id DESC 取组织内一页与总数。
func (r *Repo) List(ctx context.Context, organizationID string, q task.Query) ([]*task.Task, int, error) {
	var rows []taskModel
	conn := buntx.Conn(ctx, r.db)
	err := conn.NewSelect().Model(&rows).
		Where("t.organization_id = ?", organizationID).
		Order("t.created_at DESC", "t.id DESC").
		Limit(int64(q.PageSize)).Offset(int64(q.Offset)).
		Scan(ctx)
	if err != nil {
		return nil, 0, err
	}
	total, err := conn.NewSelect().Model((*taskModel)(nil)).
		Where("organization_id = ?", organizationID).Count(ctx)
	if err != nil {
		return nil, 0, err
	}
	tasks := make([]*task.Task, 0, len(rows))
	for i := range rows {
		tasks = append(tasks, toDomain(&rows[i]))
	}
	return tasks, int(total), nil
}

// Update 只写白名单列（bun 显式 Set 护栏：completed_at/updated_at，
// title 等创建后不可变——v1 无编辑语义）。
func (r *Repo) Update(ctx context.Context, t *task.Task) error {
	res, err := buntx.Conn(ctx, r.db).NewUpdate().
		Model((*taskModel)(nil)).
		Set("completed_at = ?", t.CompletedAt).
		Set("updated_at = ?", t.UpdatedAt).
		Where("id = ? AND organization_id = ?", t.ID, t.OrganizationID).
		Exec(ctx)
	if err != nil {
		return err
	}
	if n, err := res.RowsAffected(); err == nil && n == 0 {
		return task.ErrTaskNotFound
	}
	return nil
}

// Delete 按组织 + ID 删除，返回是否发生删除。
func (r *Repo) Delete(ctx context.Context, organizationID, id string) (bool, error) {
	res, err := buntx.Conn(ctx, r.db).NewDelete().
		Model((*taskModel)(nil)).
		Where("organization_id = ? AND id = ?", organizationID, id).Exec(ctx)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

func toModel(t *task.Task) taskModel {
	return taskModel{
		ID: t.ID, OrganizationID: t.OrganizationID, CreatorID: t.CreatorID,
		Title: t.Title, Description: t.Description,
		CompletedAt: t.CompletedAt, CreatedAt: t.CreatedAt, UpdatedAt: t.UpdatedAt,
	}
}

func toDomain(m *taskModel) *task.Task {
	return &task.Task{
		ID: m.ID, OrganizationID: m.OrganizationID, CreatorID: m.CreatorID,
		Title: m.Title, Description: m.Description,
		CompletedAt: m.CompletedAt, CreatedAt: m.CreatedAt, UpdatedAt: m.UpdatedAt,
	}
}

// organizationReader 是 organization.Repository 的**只读**实现（ADR-0007
// 通道②：DependsOn 的消费面）。任务读写前的成员资格校验用；组织事实的
// 写路径永远归 organization 模块——本适配器只 SELECT 成员行；写入方法
// 一律报错，防止所有权漂移（userReader 模式第四处）。
type organizationReader struct {
	db *bun.DB
}

// NewOrganizationReader 构造只读适配器（Assemble 调用）。
func NewOrganizationReader(db *bun.DB) *organizationReader { return &organizationReader{db: db} }

var _ organization.Repository = (*organizationReader)(nil)

type memberRow struct {
	bun.BaseModel `bun:"table:organization_members,alias:m"`

	OrganizationID string    `bun:"organization_id"`
	UserID         string    `bun:"user_id"`
	IsOwner        bool      `bun:"is_owner"`
	JoinedAt       time.Time `bun:"joined_at"`
}

func (r *organizationReader) Member(ctx context.Context, organizationID, userID string) (*organization.Member, error) {
	row := new(memberRow)
	err := r.db.NewSelect().Model(row).
		Where("m.organization_id = ? AND m.user_id = ?", organizationID, userID).Scan(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, organization.ErrNotMember
	}
	if err != nil {
		return nil, err
	}
	return &organization.Member{
		OrganizationID: row.OrganizationID, UserID: row.UserID,
		IsOwner: row.IsOwner, JoinedAt: row.JoinedAt,
	}, nil
}

func (r *organizationReader) Create(context.Context, *organization.Organization) error {
	return errors.New("task: organizationReader is read-only (Create belongs to organization)")
}

func (r *organizationReader) Get(context.Context, string) (*organization.Organization, error) {
	return nil, errors.New("task: organizationReader is read-only (Get belongs to organization)")
}

func (r *organizationReader) Update(context.Context, *organization.Organization) error {
	return errors.New("task: organizationReader is read-only (Update belongs to organization)")
}

func (r *organizationReader) AddMember(context.Context, *organization.Member) (bool, error) {
	return false, errors.New("task: organizationReader is read-only (AddMember belongs to organization)")
}

func (r *organizationReader) RemoveMember(context.Context, string, string) (bool, error) {
	return false, errors.New("task: organizationReader is read-only (RemoveMember belongs to organization)")
}

func (r *organizationReader) ListMembers(context.Context, string, organization.MemberQuery) ([]*organization.Member, int, error) {
	return nil, 0, errors.New("task: organizationReader is read-only (ListMembers belongs to organization)")
}

func (r *organizationReader) ListByUser(context.Context, string, organization.OrganizationQuery) ([]*organization.Organization, int, error) {
	return nil, 0, errors.New("task: organizationReader is read-only (ListByUser belongs to organization)")
}

func (r *organizationReader) CountOwners(context.Context, string) (int, error) {
	return 0, errors.New("task: organizationReader is read-only (CountOwners belongs to organization)")
}
