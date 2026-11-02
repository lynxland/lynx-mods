// Package access 是授权模块的用例层：角色授予/回收、claims 计算
// （Enricher 消费）与 bootstrap 管理员。角色目录来自配置（可审计），
// 分配事实落库；跨模块消费 identity 的 domain 端口（DependsOn）。
package access

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/lynxland/appkit/events"
	"github.com/lynxland/appkit/idgen"
	"github.com/lynxland/appkit/uow"
	"github.com/lynxland/lynx-mods/modules/access/domain/access"
	"github.com/lynxland/lynx-mods/modules/identity/domain/identity"
)

// UseCase 是授权模块的用例集。
type UseCase struct {
	repo   access.Repository
	users  identity.Repository // DependsOn: identity 的 domain 端口（只读）
	tx     uow.Runner
	events events.Publisher
	roles  map[string][]string // 角色目录（配置声明）
	now    func() time.Time
}

// New 构造用例（Assemble 调用）。roles 是角色目录（role → permissions）。
func New(repo access.Repository, users identity.Repository, tx uow.Runner, publisher events.Publisher, roles map[string][]string, now func() time.Time) *UseCase {
	if now == nil {
		now = time.Now
	}
	return &UseCase{repo: repo, users: users, tx: tx, events: publisher, roles: roles, now: now}
}

// Grant 授予角色（幂等）：校验角色在目录内、目标用户存在，仅状态
// 变化时发布事件。
func (u *UseCase) Grant(ctx context.Context, userID, role string) error {
	if _, ok := u.roles[role]; !ok {
		return access.ErrUnknownRole
	}
	if _, err := u.users.GetByID(ctx, userID); err != nil {
		if errors.Is(err, identity.ErrUserNotFound) {
			return access.ErrUnknownUser
		}
		return err
	}
	return u.tx.Run(ctx, func(ctx context.Context) error {
		a, err := access.NewAssignment(userID, role, u.now().UTC())
		if err != nil {
			return err
		}
		created, err := u.repo.Assign(ctx, a)
		if err != nil {
			return err
		}
		if !created {
			return nil // 幂等 no-op：不重复发布事件
		}
		return u.publish(ctx, access.EventAssignmentGranted, map[string]string{"user_id": userID, "role": role})
	})
}

// Revoke 回收角色（幂等）。
func (u *UseCase) Revoke(ctx context.Context, userID, role string) error {
	if _, ok := u.roles[role]; !ok {
		return access.ErrUnknownRole
	}
	return u.tx.Run(ctx, func(ctx context.Context) error {
		removed, err := u.repo.Revoke(ctx, userID, role)
		if err != nil {
			return err
		}
		if !removed {
			return nil
		}
		return u.publish(ctx, access.EventAssignmentRevoked, map[string]string{"user_id": userID, "role": role})
	})
}

// RolesForUser 返回用户全部角色（目录顺序稳定）。
func (u *UseCase) RolesForUser(ctx context.Context, userID string) ([]string, error) {
	roles, err := u.repo.RolesForUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	sort.Strings(roles)
	return roles, nil
}

// PermissionsForUser 计算用户权限并集（Enricher 每请求消费：一次
// 主键查询 + 内存目录展开）。未知角色（目录删除后遗留的分配）跳过。
func (u *UseCase) PermissionsForUser(ctx context.Context, userID string) ([]string, error) {
	roles, err := u.RolesForUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var perms []string
	for _, r := range roles {
		for _, p := range u.roles[r] {
			if !seen[p] {
				seen[p] = true
				perms = append(perms, p)
			}
		}
	}
	sort.Strings(perms)
	return perms, nil
}

// BootstrapAdmins 把配置里的 bootstrap 管理员邮箱解析为用户并授予
// admin 角色（幂等，Assemble 期调用一次）。用户未注册时经 onSkip
// 告警跳过——注册后重启即可收敛（README 有说明）。
func (u *UseCase) BootstrapAdmins(ctx context.Context, emails []string, onSkip func(email string)) error {
	if onSkip == nil {
		onSkip = func(string) {}
	}
	for _, email := range emails {
		user, err := u.users.GetByEmail(ctx, identity.NormalizeEmail(email))
		if err != nil {
			if errors.Is(err, identity.ErrUserNotFound) {
				onSkip(email)
				continue
			}
			return err
		}
		if err := u.Grant(ctx, user.ID, "admin"); err != nil {
			return fmt.Errorf("bootstrap admin %s: %w", email, err)
		}
	}
	return nil
}

// publish 构造事件信封（载荷为最小事实集，消费方按事件目录文档解码）。
func (u *UseCase) publish(ctx context.Context, name string, payload map[string]string) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	return u.events.Publish(ctx, events.Envelope{
		ID:         idgen.New(),
		Name:       name,
		Module:     "access",
		OccurredAt: u.now().UTC(),
		Payload:    raw,
	})
}
