package organization

import (
	"context"
	"time"

	"github.com/lynxland/appkit/crud"
)

// Repository 是 Organization 聚合与成员事实的持久化端口（infra 实现，经
// Assemble 注入）。方法一律接收 ctx：适配器从 ctx 取事务连接
// （appkit/uow/buntx），端口签名不得出现驱动类型。
type Repository interface {
	// Create 插入组织；slug 唯一冲突映射 ErrSlugTaken。
	Create(ctx context.Context, o *Organization) error
	// Get 按 ID 取；缺失映射 ErrOrganizationNotFound。
	Get(ctx context.Context, id string) (*Organization, error)
	// Update 只写 name/updated_at 白名单列（bun 显式列护栏；slug 不可变）。
	Update(ctx context.Context, o *Organization) error

	// AddMember 幂等添加成员（已存在则 no-op），返回是否发生写入。
	AddMember(ctx context.Context, m *Member) (created bool, err error)
	// RemoveMember 幂等移除成员（不存在则 no-op），返回是否发生删除。
	RemoveMember(ctx context.Context, organizationID, userID string) (removed bool, err error)
	// Member 取单个成员事实；缺失映射 ErrNotMember。
	Member(ctx context.Context, organizationID, userID string) (*Member, error)
	// ListMembers 按分页计划（crud v2 keyset：joined_at DESC + user_id
	// 决胜）返回组织成员一页与总数。
	ListMembers(ctx context.Context, organizationID string, plan crud.PagePlan) ([]*Member, int, error)
	// ListByUser 按分页计划（crud v2 keyset：joined_at DESC + id 决胜）
	// 返回用户加入的组织一页与总数。
	ListByUser(ctx context.Context, userID string, plan crud.PagePlan) ([]*Organization, int, error)
	// CountOwners 统计组织 owner 数（最后一个 owner 不可离开的裁决依据）。
	CountOwners(ctx context.Context, organizationID string) (int, error)
}

// B2B 扩展端口（v0.7.0）：角色分级 / 邀请 / 软删。
type RepositoryB2B interface {
	// UpdateMemberRole 改成员角色（owner 角色不可直改——TransferOwnership
	// 专用）；影响 0 行映射 ErrNotMember。
	UpdateMemberRole(ctx context.Context, organizationID, userID, role string, now time.Time) error
	// TransferOwnership 原子互换（新 owner 升 owner，原 owner 降 admin）。
	TransferOwnership(ctx context.Context, organizationID, newOwnerID string, now time.Time) error
	// SoftDelete 软删组织（deleted_at 落值；幂等）。
	SoftDelete(ctx context.Context, organizationID string, now time.Time) error
	// InsertInvitation 建邀请（pending 唯一冲突映射 ErrInvitePending——
	// 由 app 决定改写还是拒绝）。
	InsertInvitation(ctx context.Context, inv *Invitation) error
	// GetInvitationByToken 按令牌取邀请（接受面）。
	GetInvitationByToken(ctx context.Context, token string) (*Invitation, error)
	// GetInvitation 按 ID 取。
	GetInvitation(ctx context.Context, organizationID, id string) (*Invitation, error)
	// ListInvitations 分页（keyset：created_at DESC + id 决胜）。
	ListInvitations(ctx context.Context, organizationID string, plan crud.PagePlan) ([]*Invitation, int, error)
	// UpdateInvitationStatus 写邀请状态流转（accepted/revoked/expired）。
	UpdateInvitationStatus(ctx context.Context, inv *Invitation) error
	// GetMemberRole 取成员角色（裁决 owner-or-admin 用）；非成员映射
	// ErrNotMember。
	GetMemberRole(ctx context.Context, organizationID, userID string) (string, error)
}
