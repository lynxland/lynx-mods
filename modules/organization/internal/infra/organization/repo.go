// Package organization 是组织模块的基础设施层：bun 仓储、identity 用户
// 只读适配器与迁移。
package organization

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/lynxland/lynx-start/modules/account/domain/account/identity"
	"github.com/lynxland/lynx-start/modules/organization/domain/organization"

	"github.com/lynxland/appkit/contrib/bunx"
	"github.com/lynxland/appkit/contrib/bunx/buntx"
	"github.com/lynxland/appkit/crud"
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

	ID        string     `bun:"id,pk"`
	Name      string     `bun:"name,notnull"`
	Slug      string     `bun:"slug,notnull,unique"`
	DeletedAt *time.Time `bun:"deleted_at,nullzero"`
}

// memberModel 是成员事实 PO。user_id 只存 ID 不设外键——契约④：
// 迁移链隔离，跨模块引用只存 ID。
type memberModel struct {
	bun.BaseModel `bun:"table:organization_members,alias:m"`

	OrganizationID string    `bun:"organization_id,pk"`
	UserID         string    `bun:"user_id,pk"`
	IsOwner        bool      `bun:"is_owner,notnull"` // 派生写：role == owner 时为 true
	Role           string    `bun:"role,notnull"`
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
		Where("o.id = ? AND o.deleted_at IS NULL", id).Scan(ctx)
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
func (r *Repo) ListMembers(ctx context.Context, organizationID string, plan crud.PagePlan) ([]*organization.Member, int, error) {
	var rows []memberModel
	conn := buntx.Conn(ctx, r.db)
	err := bunx.ApplyPage(conn.NewSelect().Model(&rows).
		Where("m.organization_id = ?", organizationID), plan).
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
func (r *Repo) ListByUser(ctx context.Context, userID string, plan crud.PagePlan) ([]*organization.Organization, int, error) {
	var rows []organizationModel
	conn := buntx.Conn(ctx, r.db)
	err := bunx.ApplyPage(conn.NewSelect().Model(&rows).
		Join("JOIN organization_members AS jm ON jm.organization_id = o.id").
		Where("jm.user_id = ? AND o.deleted_at IS NULL", userID), plan).
		Scan(ctx)
	if err != nil {
		return nil, 0, err
	}
	total, err := conn.NewSelect().Model((*organizationModel)(nil)).
		Join("JOIN organization_members AS jm ON jm.organization_id = o.id").
		Where("jm.user_id = ? AND o.deleted_at IS NULL", userID).Count(ctx)
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

// roleOf 取成员角色（旧数据兼容：role 空且 is_owner 时视作 owner）。
func roleOf(m *organization.Member) string {
	if m.Role != "" {
		return m.Role
	}
	if m.IsOwner {
		return organization.MemberRoleOwner
	}
	return organization.MemberRoleMember
}

func toMemberModel(m *organization.Member) memberModel {
	return memberModel{
		OrganizationID: m.OrganizationID, UserID: m.UserID, IsOwner: roleOf(m) == organization.MemberRoleOwner,
		Role: roleOf(m), JoinedAt: m.JoinedAt,
	}
}

func toMemberDomain(m *memberModel) *organization.Member {
	return &organization.Member{
		OrganizationID: m.OrganizationID, UserID: m.UserID, IsOwner: m.Role == organization.MemberRoleOwner || m.IsOwner,
		Role: m.Role, JoinedAt: m.JoinedAt,
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

func (r *userReader) UpdateStatus(context.Context, string, string, time.Time) error {
	return errors.New("organization: userReader is read-only (UpdateStatus belongs to identity)")
}

func (r *userReader) UpdateEmailVerified(context.Context, string, bool, time.Time) error {
	return errors.New("organization: userReader is read-only (UpdateEmailVerified belongs to identity)")
}

func (r *userReader) List(context.Context, crud.PagePlan) ([]*identity.User, int, error) {
	return nil, 0, errors.New("organization: userReader is read-only (List belongs to identity)")
}

// ---- B2B 扩展（v0.7.0）----

// invitationModel 是邀请的 PO。
type invitationModel struct {
	bun.BaseModel `bun:"table:organization_invitations,alias:i"`

	ID             string     `bun:"id,pk"`
	OrganizationID string     `bun:"organization_id"`
	Email          string     `bun:"email"`
	Role           string     `bun:"role"`
	Token          string     `bun:"token,unique"`
	Status         string     `bun:"status"`
	CreatedBy      string     `bun:"created_by"`
	ExpiresAt      time.Time  `bun:"expires_at"`
	AcceptedAt     *time.Time `bun:"accepted_at"`
	AcceptedBy     string     `bun:"accepted_by"`
	CreatedAt      time.Time  `bun:"created_at"`
	UpdatedAt      time.Time  `bun:"updated_at"`
}

func toInvitationDomain(m *invitationModel) *organization.Invitation {
	return &organization.Invitation{
		ID: m.ID, OrganizationID: m.OrganizationID, Email: m.Email, Role: m.Role,
		Token: m.Token, Status: m.Status, CreatedBy: m.CreatedBy,
		ExpiresAt: m.ExpiresAt, AcceptedAt: m.AcceptedAt, AcceptedBy: m.AcceptedBy,
		CreatedAt: m.CreatedAt, UpdatedAt: m.UpdatedAt,
	}
}

// UpdateMemberRole 改成员角色（owner 行不可直改——结构性保护）。
func (r *Repo) UpdateMemberRole(ctx context.Context, organizationID, userID, role string, now time.Time) error {
	res, err := buntx.Conn(ctx, r.db).NewUpdate().
		Model((*memberModel)(nil)).
		Set("role = ?", role).
		Set("is_owner = ?", role == organization.MemberRoleOwner).
		Set("joined_at = joined_at"). // 只写角色列（白名单护栏：不触碰 joined_at）
		Where("organization_id = ? AND user_id = ? AND role <> 'owner'", organizationID, userID).
		Exec(ctx)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		// 区分：非成员 vs owner 行。
		if _, gerr := r.Member(ctx, organizationID, userID); gerr != nil {
			return organization.ErrNotMember
		}
		return organization.ErrInvalidRole
	}
	return nil
}

// TransferOwnership 原子互换：新 owner 升 owner，原 owner 降 admin。
func (r *Repo) TransferOwnership(ctx context.Context, organizationID, newOwnerID string, now time.Time) error {
	return r.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		// 新 owner 升位。
		res, err := tx.NewUpdate().Model((*memberModel)(nil)).
			Set("role = 'owner'").Set("is_owner = true").
			Where("organization_id = ? AND user_id = ?", organizationID, newOwnerID).
			Exec(ctx)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return organization.ErrNotMember
		}
		// 原唯一 owner 降为 admin。
		_, err = tx.NewUpdate().Model((*memberModel)(nil)).
			Set("role = 'admin'").Set("is_owner = false").
			Where("organization_id = ? AND role = 'owner' AND user_id <> ?", organizationID, newOwnerID).
			Exec(ctx)
		return err
	})
}

// SoftDelete 软删组织（幂等）。
func (r *Repo) SoftDelete(ctx context.Context, organizationID string, now time.Time) error {
	_, err := buntx.Conn(ctx, r.db).NewUpdate().
		Model((*organizationModel)(nil)).
		Set("deleted_at = ?", now).
		Where("id = ?", organizationID).
		Where("deleted_at IS NULL").
		Exec(ctx)
	return err
}

// InsertInvitation 建邀请（pending 唯一冲突 → ErrInvitePending 语义由
// 调用方处理：唯一索引吸收，方法返回哨兵）。
func (r *Repo) InsertInvitation(ctx context.Context, inv *organization.Invitation) error {
	m := &invitationModel{
		ID: inv.ID, OrganizationID: inv.OrganizationID, Email: inv.Email,
		Role: inv.Role, Token: inv.Token, Status: inv.Status, CreatedBy: inv.CreatedBy,
		ExpiresAt: inv.ExpiresAt, CreatedAt: inv.CreatedAt, UpdatedAt: inv.UpdatedAt,
	}
	// 同组织同邮箱 pending 唯一：改写已有 pending（令牌刷新、过期重置）。
	_, err := buntx.Conn(ctx, r.db).NewInsert().Model(m).
		On("CONFLICT (organization_id, email) WHERE status = 'pending' DO UPDATE SET token = EXCLUDED.token, role = EXCLUDED.role, expires_at = EXCLUDED.expires_at, created_by = EXCLUDED.created_by, updated_at = EXCLUDED.updated_at").
		Exec(ctx)
	return err
}

// GetInvitationByToken 按令牌取。
func (r *Repo) GetInvitationByToken(ctx context.Context, token string) (*organization.Invitation, error) {
	m := new(invitationModel)
	err := buntx.Conn(ctx, r.db).NewSelect().Model(m).
		Where("i.token = ?", token).Scan(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, organization.ErrInviteNotFound
	}
	if err != nil {
		return nil, err
	}
	return toInvitationDomain(m), nil
}

// GetInvitation 按 ID 取（组织内收敛可见性）。
func (r *Repo) GetInvitation(ctx context.Context, organizationID, id string) (*organization.Invitation, error) {
	m := new(invitationModel)
	err := buntx.Conn(ctx, r.db).NewSelect().Model(m).
		Where("i.organization_id = ? AND i.id = ?", organizationID, id).Scan(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, organization.ErrInviteNotFound
	}
	if err != nil {
		return nil, err
	}
	return toInvitationDomain(m), nil
}

// ListInvitations 分页（keyset：created_at DESC + id 决胜）。
func (r *Repo) ListInvitations(ctx context.Context, organizationID string, plan crud.PagePlan) ([]*organization.Invitation, int, error) {
	var rows []invitationModel
	if err := bunx.ApplyPage(buntx.Conn(ctx, r.db).NewSelect().Model(&rows).
		Where("i.organization_id = ?", organizationID), plan).Scan(ctx); err != nil {
		return nil, 0, err
	}
	count, err := buntx.Conn(ctx, r.db).NewSelect().Model((*invitationModel)(nil)).
		Where("organization_id = ?", organizationID).Count(ctx)
	if err != nil {
		return nil, 0, err
	}
	out := make([]*organization.Invitation, 0, len(rows))
	for i := range rows {
		out = append(out, toInvitationDomain(&rows[i]))
	}
	return out, int(count), nil
}

// UpdateInvitationStatus 写邀请状态流转。
func (r *Repo) UpdateInvitationStatus(ctx context.Context, inv *organization.Invitation) error {
	_, err := buntx.Conn(ctx, r.db).NewUpdate().
		Model((*invitationModel)(nil)).
		Set("status = ?", inv.Status).
		Set("accepted_at = ?", inv.AcceptedAt).
		Set("accepted_by = ?", inv.AcceptedBy).
		Set("updated_at = ?", inv.UpdatedAt).
		Where("id = ?", inv.ID).
		Exec(ctx)
	return err
}

// GetMemberRole 取成员角色。
func (r *Repo) GetMemberRole(ctx context.Context, organizationID, userID string) (string, error) {
	m := new(memberModel)
	err := buntx.Conn(ctx, r.db).NewSelect().Model(m).
		Column("m.role", "m.is_owner").
		Where("m.organization_id = ? AND m.user_id = ?", organizationID, userID).Scan(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		return "", organization.ErrNotMember
	}
	if err != nil {
		return "", err
	}
	if m.Role == "" {
		if m.IsOwner {
			return organization.MemberRoleOwner, nil
		}
		return organization.MemberRoleMember, nil
	}
	return m.Role, nil
}
