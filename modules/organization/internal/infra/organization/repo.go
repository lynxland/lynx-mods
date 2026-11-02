// Package organization 是组织模块的基础设施层：bun 仓储、identity 用户
// 只读适配器与迁移。
package organization

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/lynxland/lynx-mods/modules/identity/domain/identity"
	"github.com/lynxland/lynx-mods/modules/organization/domain/organization"

	"github.com/lynxland/appkit/contrib/bunx"
	"github.com/lynxland/appkit/contrib/bunx/buntx"
	"github.com/lynxland/appkit/row"
	"github.com/lynxland/appkit/storeerr"
	"github.com/uptrace/bun"
)

// organizationModel 是组织 PO（持久化对象）：bun 标签在此，领域聚合不染 ORM。
// 审计时间列经 row.Stamps 声明（dogfood 首循环④试点：列名 snake_case
// 自动同构；domain 构造器持有时间戳——显式规约，非零不覆写）。
type organizationModel struct {
	bun.BaseModel `bun:"table:organization_organizations,alias:o"`

	row.Stamps

	ID   string `bun:"id,pk"`
	Name string `bun:"name,notnull"`
	Slug string `bun:"slug,notnull,unique"`
}

// memberModel 是成员事实 PO。user_id 只存 ID 不设外键——契约④：
// 迁移链隔离，跨模块引用只存 ID。
type memberModel struct {
	bun.BaseModel `bun:"table:organization_members,alias:m"`

	OrganizationID string    `bun:"organization_id,pk"`
	UserID         string    `bun:"user_id,pk"`
	IsOwner        bool      `bun:"is_owner,notnull"`
	JoinedAt       time.Time `bun:"joined_at,notnull"`
}

// Repo 实现 organization.Repository。
type Repo struct {
	db *bun.DB
}

// NewRepository 构造仓储（Assemble 调用）。
func NewRepository(db *bun.DB) *Repo { return &Repo{db: db} }

var _ organization.Repository = (*Repo)(nil)

// Create 插入组织；slug 唯一冲突（pgdriver 23505）映射 ErrSlugTaken。
func (r *Repo) Create(ctx context.Context, o *organization.Organization) error {
	m := toOrganizationModel(o)
	_, err := buntx.Conn(ctx, r.db).NewInsert().Model(&m).Exec(ctx)
	if err != nil {
		if storeerr.IsAlreadyExists(bunx.MapError(err)) {
			return organization.ErrSlugTaken
		}
		return err
	}
	return nil
}

// Get 按 ID 取；缺失映射领域哨兵错误。
func (r *Repo) Get(ctx context.Context, id string) (*organization.Organization, error) {
	m := new(organizationModel)
	err := buntx.Conn(ctx, r.db).NewSelect().Model(m).
		Where("o.id = ?", id).Scan(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, organization.ErrOrganizationNotFound
	}
	if err != nil {
		return nil, err
	}
	return toOrganizationDomain(m), nil
}

// Update 只写白名单列（bun 显式 Set 护栏：禁止整模型覆盖；slug 不可变）。
func (r *Repo) Update(ctx context.Context, o *organization.Organization) error {
	res, err := buntx.Conn(ctx, r.db).NewUpdate().
		Model((*organizationModel)(nil)).
		Set("name = ?", o.Name).
		Set("updated_at = ?", o.UpdatedAt).
		Where("id = ?", o.ID).
		Exec(ctx)
	if err != nil {
		return err
	}
	if n, err := res.RowsAffected(); err == nil && n == 0 {
		return organization.ErrOrganizationNotFound
	}
	return nil
}

// AddMember 幂等添加（ON CONFLICT DO NOTHING），返回是否发生写入。
func (r *Repo) AddMember(ctx context.Context, m *organization.Member) (bool, error) {
	row := toMemberModel(m)
	res, err := buntx.Conn(ctx, r.db).NewInsert().Model(&row).
		On("CONFLICT (organization_id, user_id) DO NOTHING").Exec(ctx)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// RemoveMember 幂等移除，返回是否发生删除。
func (r *Repo) RemoveMember(ctx context.Context, organizationID, userID string) (bool, error) {
	res, err := buntx.Conn(ctx, r.db).NewDelete().
		Model((*memberModel)(nil)).
		Where("organization_id = ? AND user_id = ?", organizationID, userID).Exec(ctx)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// Member 取单个成员事实；缺失映射 ErrNotMember。
func (r *Repo) Member(ctx context.Context, organizationID, userID string) (*organization.Member, error) {
	m := new(memberModel)
	err := buntx.Conn(ctx, r.db).NewSelect().Model(m).
		Where("m.organization_id = ? AND m.user_id = ?", organizationID, userID).Scan(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, organization.ErrNotMember
	}
	if err != nil {
		return nil, err
	}
	return toMemberDomain(m), nil
}

// ListMembers 按 joined_at DESC, user_id DESC 取一页与总数。
func (r *Repo) ListMembers(ctx context.Context, organizationID string, q organization.MemberQuery) ([]*organization.Member, int, error) {
	var rows []memberModel
	conn := buntx.Conn(ctx, r.db)
	err := conn.NewSelect().Model(&rows).
		Where("m.organization_id = ?", organizationID).
		Order("m.joined_at DESC", "m.user_id DESC").
		Limit(int64(q.PageSize)).Offset(int64(q.Offset)).
		Scan(ctx)
	if err != nil {
		return nil, 0, err
	}
	total, err := conn.NewSelect().Model((*memberModel)(nil)).
		Where("organization_id = ?", organizationID).Count(ctx)
	if err != nil {
		return nil, 0, err
	}
	members := make([]*organization.Member, 0, len(rows))
	for i := range rows {
		members = append(members, toMemberDomain(&rows[i]))
	}
	return members, int(total), nil
}

// ListByUser 按 joined_at DESC, organization id DESC 取用户加入的组织一页与总数。
func (r *Repo) ListByUser(ctx context.Context, userID string, q organization.OrganizationQuery) ([]*organization.Organization, int, error) {
	var rows []organizationModel
	conn := buntx.Conn(ctx, r.db)
	err := conn.NewSelect().Model(&rows).
		Join("JOIN organization_members AS jm ON jm.organization_id = o.id").
		Where("jm.user_id = ?", userID).
		Order("jm.joined_at DESC", "o.id DESC").
		Limit(int64(q.PageSize)).Offset(int64(q.Offset)).
		Scan(ctx)
	if err != nil {
		return nil, 0, err
	}
	total, err := conn.NewSelect().Model((*organizationModel)(nil)).
		Join("JOIN organization_members AS jm ON jm.organization_id = o.id").
		Where("jm.user_id = ?", userID).Count(ctx)
	if err != nil {
		return nil, 0, err
	}
	organizations := make([]*organization.Organization, 0, len(rows))
	for i := range rows {
		organizations = append(organizations, toOrganizationDomain(&rows[i]))
	}
	return organizations, int(total), nil
}

// CountOwners 统计组织 owner 数。
func (r *Repo) CountOwners(ctx context.Context, organizationID string) (int, error) {
	n, err := buntx.Conn(ctx, r.db).NewSelect().Model((*memberModel)(nil)).
		Where("organization_id = ? AND is_owner = ?", organizationID, true).Count(ctx)
	return int(n), err
}

func toOrganizationModel(o *organization.Organization) organizationModel {
	return organizationModel{
		ID: o.ID, Name: o.Name, Slug: o.Slug,
		Stamps: row.Stamps{CreatedAt: o.CreatedAt, UpdatedAt: o.UpdatedAt},
	}
}

func toOrganizationDomain(m *organizationModel) *organization.Organization {
	return &organization.Organization{
		ID: m.ID, Name: m.Name, Slug: m.Slug,
		CreatedAt: m.CreatedAt, UpdatedAt: m.UpdatedAt,
	}
}

func toMemberModel(m *organization.Member) memberModel {
	return memberModel{
		OrganizationID: m.OrganizationID, UserID: m.UserID, IsOwner: m.IsOwner, JoinedAt: m.JoinedAt,
	}
}

func toMemberDomain(m *memberModel) *organization.Member {
	return &organization.Member{
		OrganizationID: m.OrganizationID, UserID: m.UserID, IsOwner: m.IsOwner, JoinedAt: m.JoinedAt,
	}
}

// userReader 是 identity.Repository 的**只读**实现（DependsOn 的消费面）：
// AddMember 校验目标用户存在用。identity_users 的写路径永远归 identity
// 模块——本适配器只 SELECT 显式列；写入方法一律报错，防止所有权漂移。
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
	return nil, errors.New("organization: userReader is read-only (GetByEmail belongs to identity)")
}

func (r *userReader) Create(context.Context, *identity.User) error {
	return errors.New("organization: userReader is read-only (Create belongs to identity)")
}

func (r *userReader) UpdatePassword(context.Context, *identity.User) error {
	return errors.New("organization: userReader is read-only (UpdatePassword belongs to identity)")
}
