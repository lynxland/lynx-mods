// Package organization 是组织模块的用例层：编排领域与端口（事务/事件/
// 仓储）。管理操作 owner-only（IsOwner 结构标记）；成员可见性 = 成员资格
// 校验（v1 不做组织级 RBAC，proto 注解保持 authenticated，细粒度在 app 层）。
package organization

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/lynx-go/lynx-mods/modules/identity/domain/identity"
	"github.com/lynx-go/lynx-mods/modules/organization/domain/organization"
	"github.com/lynxland/appkit/crud"
	"github.com/lynxland/appkit/events"
	"github.com/lynxland/appkit/idgen"
	"github.com/lynxland/appkit/uow"
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
	params, err := crud.ParseListParams(pageSize, pageToken, "", "")
	if err != nil {
		return nil, err
	}
	members, total, err := u.repo.ListMembers(ctx, organizationID, organization.MemberQuery{PageSize: params.PageSize, Offset: params.Offset})
	if err != nil {
		return nil, err
	}
	page, err := crud.FinalizeOffsetPage(params, total, len(members))
	if err != nil {
		return nil, err
	}
	return &ListMembersResult{Members: members, NextToken: page.NextToken, TotalSize: int32(total)}, nil
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

// ListMyOrganizations 列当前用户加入的组织（自助，分页）。
func (u *UseCase) ListMyOrganizations(ctx context.Context, userID string, pageSize int32, pageToken string) (*ListMyOrganizationsResult, error) {
	params, err := crud.ParseListParams(pageSize, pageToken, "", "")
	if err != nil {
		return nil, err
	}
	organizations, total, err := u.repo.ListByUser(ctx, userID, organization.OrganizationQuery{PageSize: params.PageSize, Offset: params.Offset})
	if err != nil {
		return nil, err
	}
	page, err := crud.FinalizeOffsetPage(params, total, len(organizations))
	if err != nil {
		return nil, err
	}
	return &ListMyOrganizationsResult{Organizations: organizations, NextToken: page.NextToken, TotalSize: int32(total)}, nil
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
