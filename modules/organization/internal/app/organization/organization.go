// Package organization 是组织模块的用例层：编排领域与端口（事务/事件/
// 仓储）。管理操作 owner-only（IsOwner 结构标记）；成员可见性 = 成员资格
// 校验（v1 不做组织级 RBAC，proto 注解保持 authenticated，细粒度在 app 层）。
package organization

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/lynxland/appkit/crud"
	"github.com/lynxland/appkit/events"
	"github.com/lynxland/appkit/idgen"
	"github.com/lynxland/appkit/uow"
	"github.com/lynxland/lynx-start/modules/account/domain/account/identity"
	"github.com/lynxland/lynx-start/modules/organization/domain/organization"
)

// UseCase 是组织模块的用例集。
type UseCase struct {
	repo   organization.Repository
	users  identity.Repository // DependsOn: identity 的 domain 端口（只读）
	tx     uow.Runner
	events events.Publisher
	now    func() time.Time
}

// New 构造用例（Assemble 调用）。
func New(repo organization.Repository, users identity.Repository, tx uow.Runner, publisher events.Publisher) *UseCase {
	return &UseCase{repo: repo, users: users, tx: tx, events: publisher, now: time.Now}
}

// Create 建组织，创建者即 owner（同一工作单元内落组织与 owner 成员并发布事件）。
func (u *UseCase) Create(ctx context.Context, creatorID, name, slug string) (*organization.Organization, error) {
	var out *organization.Organization
	err := u.tx.Run(ctx, func(ctx context.Context) error {
		now := u.now().UTC()
		o, err := organization.NewOrganization(idgen.New(), name, slug, now)
		if err != nil {
			return err
		}
		if err := u.repo.Create(ctx, o); err != nil {
			return err
		}
		owner, err := organization.NewMember(o.ID, creatorID, true, now)
		if err != nil {
			return err
		}
		if _, err := u.repo.AddMember(ctx, owner); err != nil {
			return err
		}
		out = o
		return u.publish(ctx, organization.EventOrganizationCreated, map[string]string{
			"id": o.ID, "name": o.Name, "slug": o.Slug, "owner_id": creatorID,
		})
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// Get 取组织（仅成员可见：非成员一律 ErrNotMember，不泄露存在性差异）。
func (u *UseCase) Get(ctx context.Context, actorID, organizationID string) (*organization.Organization, error) {
	o, err := u.repo.Get(ctx, organizationID)
	if err != nil {
		return nil, err
	}
	if _, err := u.repo.Member(ctx, organizationID, actorID); err != nil {
		if errors.Is(err, organization.ErrNotMember) {
			return nil, organization.ErrNotMember
		}
		return nil, err
	}
	return o, nil
}

// Rename 改名（owner-only；slug 创建后不可变）。
func (u *UseCase) Rename(ctx context.Context, actorID, organizationID, name string) (*organization.Organization, error) {
	var out *organization.Organization
	err := u.tx.Run(ctx, func(ctx context.Context) error {
		o, err := u.repo.Get(ctx, organizationID)
		if err != nil {
			return err
		}
		if err := u.requireOwner(ctx, organizationID, actorID); err != nil {
			return err
		}
		if err := o.Rename(name, u.now().UTC()); err != nil {
			return err
		}
		if err := u.repo.Update(ctx, o); err != nil {
			return err
		}
		out = o
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// AddMember 加成员（owner-only；目标用户须存在于 identity；幂等——
// 重复添加为 no-op，事件仅在状态变化时发布）。
func (u *UseCase) AddMember(ctx context.Context, actorID, organizationID, userID string) error {
	o, err := u.repo.Get(ctx, organizationID)
	if err != nil {
		return err
	}
	if err := u.requireOwner(ctx, organizationID, actorID); err != nil {
		return err
	}
	if _, err := u.users.GetByID(ctx, userID); err != nil {
		if errors.Is(err, identity.ErrUserNotFound) {
			return organization.ErrUnknownUser
		}
		return err
	}
	return u.tx.Run(ctx, func(ctx context.Context) error {
		m, err := organization.NewMember(organizationID, userID, false, u.now().UTC())
		if err != nil {
			return err
		}
		created, err := u.repo.AddMember(ctx, m)
		if err != nil {
			return err
		}
		if !created {
			return nil // 幂等 no-op：不重复发布事件
		}
		return u.publish(ctx, organization.EventMemberAdded, map[string]string{
			"organization_id": organizationID, "organization_name": o.Name, "user_id": userID,
		})
	})
}

// RemoveMember 移除成员（owner-only；不可移除 owner——owner 只能自行离开；
// 幂等——目标不存在为 no-op）。
func (u *UseCase) RemoveMember(ctx context.Context, actorID, organizationID, userID string) error {
	o, err := u.repo.Get(ctx, organizationID)
	if err != nil {
		return err
	}
	if err := u.requireOwner(ctx, organizationID, actorID); err != nil {
		return err
	}
	if m, err := u.repo.Member(ctx, organizationID, userID); err != nil {
		if !errors.Is(err, organization.ErrNotMember) {
			return err
		}
	} else if m.IsOwner {
		return organization.ErrCannotRemoveOwner
	}
	return u.tx.Run(ctx, func(ctx context.Context) error {
		removed, err := u.repo.RemoveMember(ctx, organizationID, userID)
		if err != nil {
			return err
		}
		if !removed {
			return nil // 幂等 no-op：不重复发布事件
		}
		return u.publish(ctx, organization.EventMemberRemoved, map[string]string{
			"organization_id": organizationID, "organization_name": o.Name, "user_id": userID,
		})
	})
}

// ListMembers 列成员（成员可见）。
func (u *UseCase) ListMembers(ctx context.Context, actorID, organizationID string, pageSize int32, pageToken string) (*ListMembersResult, error) {
	if _, err := u.repo.Get(ctx, organizationID); err != nil {
		return nil, err
	}
	if _, err := u.repo.Member(ctx, organizationID, actorID); err != nil {
		if errors.Is(err, organization.ErrNotMember) {
			return nil, organization.ErrNotMember
		}
		return nil, err
	}
	q, err := crud.Parse[any](membersSpec, crud.RawParams{After: pageToken, Limit: pageSize, OrderBy: "joinedAt desc"}, nil)
	if err != nil {
		return nil, err
	}
	members, total, err := u.repo.ListMembers(ctx, organizationID, q.PagePlan)
	if err != nil {
		return nil, err
	}
	next := ""
	if crud.HasMore(int32(len(members)), q.Limit) && len(members) > 0 {
		last := members[len(members)-1]
		if next, err = crud.Cursor(q, []string{last.JoinedAt.UTC().Format(time.RFC3339Nano), last.UserID}); err != nil {
			return nil, err
		}
	}
	return &ListMembersResult{Members: members, NextToken: next, TotalSize: int32(total)}, nil
}

// Leave 成员自行离开（最后一个 owner 不可离开——组织不可无主）。
func (u *UseCase) Leave(ctx context.Context, userID, organizationID string) error {
	o, err := u.repo.Get(ctx, organizationID)
	if err != nil {
		return err
	}
	m, err := u.repo.Member(ctx, organizationID, userID)
	if err != nil {
		return err
	}
	if m.IsOwner {
		owners, err := u.repo.CountOwners(ctx, organizationID)
		if err != nil {
			return err
		}
		if owners <= 1 {
			return organization.ErrLastOwner
		}
	}
	return u.tx.Run(ctx, func(ctx context.Context) error {
		removed, err := u.repo.RemoveMember(ctx, organizationID, userID)
		if err != nil {
			return err
		}
		if !removed {
			return nil // 幂等 no-op：不重复发布事件
		}
		return u.publish(ctx, organization.EventMemberRemoved, map[string]string{
			"organization_id": organizationID, "organization_name": o.Name, "user_id": userID,
		})
	})
}

// ListMyOrganizationsResult 是「我的组织」列表用例的产物。
type ListMyOrganizationsResult struct {
	Organizations []*organization.Organization
	NextToken     string
	TotalSize     int32
}

// membersSpec / myOrganizationsSpec 是两个列表的分页规约（crud v2：
// keyset 游标，joined_at DESC + 唯一列决胜；总数另经 COUNT 供粗参考）。
var (
	membersSpec = crud.Spec{
		Modes:        crud.ModeSetKeyset,
		Sortable:     map[string]string{"joinedAt": "m.joined_at", "userId": "m.user_id"},
		Tiebreak:     "userId",
		DefaultLimit: 20,
		MaxLimit:     100,
	}
	myOrganizationsSpec = crud.Spec{
		Modes:        crud.ModeSetKeyset,
		Sortable:     map[string]string{"createdAt": "o.created_at", "id": "o.id"},
		Tiebreak:     "id",
		DefaultLimit: 20,
		MaxLimit:     100,
	}
)

// ListMyOrganizations 列当前用户加入的组织（自助，keyset 分页）。
func (u *UseCase) ListMyOrganizations(ctx context.Context, userID string, pageSize int32, pageToken string) (*ListMyOrganizationsResult, error) {
	q, err := crud.Parse[any](myOrganizationsSpec, crud.RawParams{After: pageToken, Limit: pageSize, OrderBy: "createdAt desc"}, nil)
	if err != nil {
		return nil, err
	}
	organizations, total, err := u.repo.ListByUser(ctx, userID, q.PagePlan)
	if err != nil {
		return nil, err
	}
	next := ""
	if crud.HasMore(int32(len(organizations)), q.Limit) && len(organizations) > 0 {
		last := organizations[len(organizations)-1]
		if next, err = crud.Cursor(q, []string{last.CreatedAt.UTC().Format(time.RFC3339Nano), last.ID}); err != nil {
			return nil, err
		}
	}
	return &ListMyOrganizationsResult{Organizations: organizations, NextToken: next, TotalSize: int32(total)}, nil
}

// ListMembersResult 是成员列表用例的产物。
type ListMembersResult struct {
	Members   []*organization.Member
	NextToken string
	TotalSize int32
}

// requireOwner 校验 actor 是本组织 owner（非成员与普通成员统一拒绝为
// ErrNotOwner——管理授权不区分「你不在」与「你不够格」）。
func (u *UseCase) requireOwner(ctx context.Context, organizationID, actorID string) error {
	m, err := u.repo.Member(ctx, organizationID, actorID)
	if err != nil {
		if errors.Is(err, organization.ErrNotMember) {
			return organization.ErrNotOwner
		}
		return err
	}
	if !m.IsOwner {
		return organization.ErrNotOwner
	}
	return nil
}

// publish 构造事件信封并发布（载荷 JSON 编码——手搓字符串会在 name 含
// 引号/反斜杠时产生非法 JSON；与 demo/identity/session 的 publish 同款纪律）。
func (u *UseCase) publish(ctx context.Context, name string, payload map[string]string) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	return u.events.Publish(ctx, events.Envelope{
		ID:         idgen.New(),
		Name:       name,
		Module:     "organization",
		OccurredAt: u.now().UTC(),
		Payload:    raw,
	})
}

// ---- B2B 基本盘（v0.7.0）：邀请 / 角色分级 / 所有权 / 软删 ----

// b2bRepo 是 B2B 扩展端口（infra 一并实现）。
type b2bRepo interface {
	UpdateMemberRole(ctx context.Context, organizationID, userID, role string, now time.Time) error
	TransferOwnership(ctx context.Context, organizationID, newOwnerID string, now time.Time) error
	SoftDelete(ctx context.Context, organizationID string, now time.Time) error
	InsertInvitation(ctx context.Context, inv *organization.Invitation) error
	GetInvitationByToken(ctx context.Context, token string) (*organization.Invitation, error)
	GetInvitation(ctx context.Context, organizationID, id string) (*organization.Invitation, error)
	ListInvitations(ctx context.Context, organizationID string, plan crud.PagePlan) ([]*organization.Invitation, int, error)
	UpdateInvitationStatus(ctx context.Context, inv *organization.Invitation) error
	GetMemberRole(ctx context.Context, organizationID, userID string) (string, error)
}

// requireOwnerOrAdmin 管理操作裁决（owner 或 admin；成员角色分级）。
func (u *UseCase) requireOwnerOrAdmin(ctx context.Context, organizationID, actorID string) error {
	role, err := u.repo.(b2bRepo).GetMemberRole(ctx, organizationID, actorID)
	if err != nil {
		return err // ErrNotMember 归一（组织不存在/非成员同响应）
	}
	if role != organization.MemberRoleOwner && role != organization.MemberRoleAdmin {
		return organization.ErrNotAdmin
	}
	return nil
}

// CreateInvitation 创建邀请（owner/admin）：令牌经事件投递到被邀邮箱
// （organization.invite.created——通知渠道发信）；重复邀请改写已有
// pending（令牌刷新）。
func (u *UseCase) CreateInvitation(ctx context.Context, actorID, organizationID, email, role string) (*organization.Invitation, error) {
	o, err := u.repo.Get(ctx, organizationID)
	if err != nil {
		return nil, err
	}
	if err := u.requireOwnerOrAdmin(ctx, organizationID, actorID); err != nil {
		return nil, err
	}
	inv, err := organization.NewInvitation(idgen.New(), organizationID, email, role, idgen.New(), actorID, u.now().UTC())
	if err != nil {
		return nil, err
	}
	if err := u.repo.(b2bRepo).InsertInvitation(ctx, inv); err != nil {
		return nil, err
	}
	if err := u.publish(ctx, organization.EventInvitationCreated, map[string]string{
		"organization_id": organizationID, "organization_name": o.Name,
		"email": inv.Email, "role": inv.Role, "token": inv.Token,
	}); err != nil {
		return nil, err
	}
	return inv, nil
}

// invitationsSpec 是邀请列表的分页规约（keyset：created_at DESC + id 决胜）。
var invitationsSpec = crud.Spec{
	Modes:        crud.ModeSetKeyset,
	Sortable:     map[string]string{"createdAt": "i.created_at", "id": "i.id"},
	Tiebreak:     "id",
	DefaultLimit: 20,
	MaxLimit:     100,
}

// ListInvitationsResult 是邀请列表用例的产物。
type ListInvitationsResult struct {
	Invitations []*organization.Invitation
	NextToken   string
	TotalSize   int32
}

// ListInvitations 列组织邀请（owner/admin）。
func (u *UseCase) ListInvitations(ctx context.Context, actorID, organizationID string, pageSize int32, pageToken string) (*ListInvitationsResult, error) {
	if err := u.requireOwnerOrAdmin(ctx, organizationID, actorID); err != nil {
		return nil, err
	}
	q, err := crud.Parse[any](invitationsSpec, crud.RawParams{After: pageToken, Limit: pageSize, OrderBy: "createdAt desc"}, nil)
	if err != nil {
		return nil, err
	}
	items, total, err := u.repo.(b2bRepo).ListInvitations(ctx, organizationID, q.PagePlan)
	if err != nil {
		return nil, err
	}
	next := ""
	if crud.HasMore(int32(len(items)), q.Limit) && len(items) > 0 {
		last := items[len(items)-1]
		if next, err = crud.Cursor(q, []string{last.CreatedAt.UTC().Format(time.RFC3339Nano), last.ID}); err != nil {
			return nil, err
		}
	}
	return &ListInvitationsResult{Invitations: items, NextToken: next, TotalSize: int32(total)}, nil
}

// RevokeInvitation 撤销邀请（owner/admin；幂等裁决交领域）。
func (u *UseCase) RevokeInvitation(ctx context.Context, actorID, organizationID, invitationID string) error {
	if err := u.requireOwnerOrAdmin(ctx, organizationID, actorID); err != nil {
		return err
	}
	inv, err := u.repo.(b2bRepo).GetInvitation(ctx, organizationID, invitationID)
	if err != nil {
		return err
	}
	if err := inv.RevokeIfPending(u.now().UTC()); err != nil {
		return err
	}
	return u.repo.(b2bRepo).UpdateInvitationStatus(ctx, inv)
}

// AcceptInvitation 接受邀请（自助——凭令牌；邮箱须与账号一致；幂等裁决：
// 已是成员直接成功）。
func (u *UseCase) AcceptInvitation(ctx context.Context, accountEmail, userID, token string) error {
	inv, err := u.repo.(b2bRepo).GetInvitationByToken(ctx, token)
	if err != nil {
		return err
	}
	if err := inv.AcceptIfEligible(accountEmail, userID, u.now().UTC()); err != nil {
		return err
	}
	o, err := u.repo.Get(ctx, inv.OrganizationID)
	if err != nil {
		return err
	}
	return u.tx.Run(ctx, func(ctx context.Context) error {
		m, err := organization.NewMemberWithRole(inv.OrganizationID, userID, inv.Role, u.now().UTC())
		if err != nil {
			return err
		}
		created, err := u.repo.AddMember(ctx, m)
		if err != nil {
			return err
		}
		if err := u.repo.(b2bRepo).UpdateInvitationStatus(ctx, inv); err != nil {
			return err
		}
		if !created {
			return nil // 已是成员：邀请闭环即可
		}
		if err := u.publish(ctx, organization.EventInvitationAccepted, map[string]string{
			"organization_id": inv.OrganizationID, "user_id": userID,
		}); err != nil {
			return err
		}
		_ = o
		return nil
	})
}

// UpdateMemberRole 改成员角色（owner/admin；owner 角色不可直改）。
func (u *UseCase) UpdateMemberRole(ctx context.Context, actorID, organizationID, userID, role string) error {
	if !organization.IsValidGrantRole(role) {
		return organization.ErrInvalidRole
	}
	if err := u.requireOwnerOrAdmin(ctx, organizationID, actorID); err != nil {
		return err
	}
	return u.repo.(b2bRepo).UpdateMemberRole(ctx, organizationID, userID, role, u.now().UTC())
}

// TransferOwnership 转让所有权（owner-only；新 owner 须为现有成员）——
// 解「单 owner 组织不可换主」死锁；转让后原 owner 降为 admin。
func (u *UseCase) TransferOwnership(ctx context.Context, actorID, organizationID, newOwnerID string) error {
	if err := u.requireOwner(ctx, organizationID, actorID); err != nil {
		return err
	}
	if err := u.repo.(b2bRepo).TransferOwnership(ctx, organizationID, newOwnerID, u.now().UTC()); err != nil {
		return err
	}
	return u.publish(ctx, organization.EventOwnershipTransferred, map[string]string{
		"organization_id": organizationID, "old_owner_id": actorID, "new_owner_id": newOwnerID,
	})
}

// DeleteOrganization 软删（owner-only；成员即时失去访问——Get/List 过滤
// 已删组织，requireMember 随之失效；数据保留供审计）。
func (u *UseCase) DeleteOrganization(ctx context.Context, actorID, organizationID string) error {
	if _, err := u.repo.Get(ctx, organizationID); err != nil {
		return err
	}
	if err := u.requireOwner(ctx, organizationID, actorID); err != nil {
		return err
	}
	return u.repo.(b2bRepo).SoftDelete(ctx, organizationID, u.now().UTC())
}
