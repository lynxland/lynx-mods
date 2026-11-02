// Package task 是任务模块的用例层：编排领域与端口（事务/事件/仓储）。
// 每个用例第一步都是成员资格校验（经 organization 只读端口，ADR-0007
// 通道②）——非成员与组织不存在统一 ErrNotMember，不泄露存在性差异。
package task

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/lynx-go/lynx-mods/modules/organization/domain/organization"
	"github.com/lynx-go/lynx-mods/modules/task/domain/task"
	"github.com/lynxland/appkit/crud"
	"github.com/lynxland/appkit/events"
	"github.com/lynxland/appkit/idgen"
	"github.com/lynxland/appkit/uow"
)

// UseCase 是任务模块的用例集。
type UseCase struct {
	repo task.Repository
	orgs organization.Repository // DependsOn: organization 的 domain 端口（只读）
	tx   uow.Runner
	ev   events.Publisher
	now  func() time.Time
}

// New 构造用例（Assemble 调用）。
func New(repo task.Repository, orgs organization.Repository, tx uow.Runner, ev events.Publisher) *UseCase {
	return &UseCase{repo: repo, orgs: orgs, tx: tx, ev: ev, now: time.Now}
}

// Create 建任务（成员即可创建；同一工作单元内落任务并发布事件）。
func (u *UseCase) Create(ctx context.Context, actorID, organizationID, title, description string) (*task.Task, error) {
	if err := u.requireMember(ctx, organizationID, actorID); err != nil {
		return nil, err
	}
	var out *task.Task
	err := u.tx.Run(ctx, func(ctx context.Context) error {
		t, err := task.NewTask(idgen.New(), organizationID, actorID, title, description, u.now().UTC())
		if err != nil {
			return err
		}
		if err := u.repo.Create(ctx, t); err != nil {
			return err
		}
		out = t
		return u.publish(ctx, task.EventTaskCreated, map[string]string{
			"id": t.ID, "organization_id": t.OrganizationID,
			"creator_id": t.CreatorID, "title": t.Title,
		})
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// Get 取任务（成员可见）。
func (u *UseCase) Get(ctx context.Context, actorID, organizationID, taskID string) (*task.Task, error) {
	if err := u.requireMember(ctx, organizationID, actorID); err != nil {
		return nil, err
	}
	return u.repo.Get(ctx, organizationID, taskID)
}

// ListTasksResult 是任务列表用例的产物。
type ListTasksResult struct {
	Tasks     []*task.Task
	NextToken string
	TotalSize int32
}

// List 组织任务列表（成员可见，分页）。
func (u *UseCase) List(ctx context.Context, actorID, organizationID string, pageSize int32, pageToken string) (*ListTasksResult, error) {
	if err := u.requireMember(ctx, organizationID, actorID); err != nil {
		return nil, err
	}
	params, err := crud.ParseListParams(pageSize, pageToken, "", "")
	if err != nil {
		return nil, err
	}
	tasks, total, err := u.repo.List(ctx, organizationID, task.Query{PageSize: int(params.PageSize), Offset: params.Offset})
	if err != nil {
		return nil, err
	}
	page, err := crud.FinalizeOffsetPage(params, total, len(tasks))
	if err != nil {
		return nil, err
	}
	return &ListTasksResult{Tasks: tasks, NextToken: page.NextToken, TotalSize: int32(total)}, nil
}

// Complete 标记完成（限创建者；幂等——已完成为 no-op 不重复发事件）。
func (u *UseCase) Complete(ctx context.Context, actorID, organizationID, taskID string) (*task.Task, error) {
	if err := u.requireMember(ctx, organizationID, actorID); err != nil {
		return nil, err
	}
	var out *task.Task
	err := u.tx.Run(ctx, func(ctx context.Context) error {
		t, err := u.repo.Get(ctx, organizationID, taskID)
		if err != nil {
			return err
		}
		if t.CreatorID != actorID {
			return task.ErrNotCreator
		}
		if t.Completed() {
			out = t // 幂等 no-op：不重复发布事件
			return nil
		}
		t.Complete(u.now().UTC())
		if err := u.repo.Update(ctx, t); err != nil {
			return err
		}
		out = t
		return u.publish(ctx, task.EventTaskCompleted, map[string]string{
			"id": t.ID, "organization_id": t.OrganizationID,
		})
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// Delete 删除任务（限创建者；幂等——不存在为 no-op）。
func (u *UseCase) Delete(ctx context.Context, actorID, organizationID, taskID string) error {
	if err := u.requireMember(ctx, organizationID, actorID); err != nil {
		return err
	}
	return u.tx.Run(ctx, func(ctx context.Context) error {
		t, err := u.repo.Get(ctx, organizationID, taskID)
		if err != nil {
			if errors.Is(err, task.ErrTaskNotFound) {
				return nil // 幂等 no-op：删除不泄露存在性
			}
			return err
		}
		if t.CreatorID != actorID {
			return task.ErrNotCreator
		}
		_, err = u.repo.Delete(ctx, organizationID, taskID)
		return err
	})
}

// requireMember 校验 actor 是组织成员（组织不存在与非成员统一
// ErrNotMember——与 organization.Get 同款不泄露语义）。
func (u *UseCase) requireMember(ctx context.Context, organizationID, actorID string) error {
	_, err := u.orgs.Member(ctx, organizationID, actorID)
	return err
}

// publish 构造事件信封并发布（载荷 JSON 编码，各模块同款纪律）。
func (u *UseCase) publish(ctx context.Context, name string, payload map[string]string) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	return u.ev.Publish(ctx, events.Envelope{
		ID: idgen.New(), Name: name, Module: "task",
		OccurredAt: u.now().UTC(), Payload: raw,
	})
}
