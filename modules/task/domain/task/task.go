// Package task 是任务模块的领域层：Task 聚合（组织挂靠资源）、不变量
// 与仓储端口。
//
// 定位：任务 = 组织内的协作事实。第一个组织挂靠业务资源（ADR-0008 姿势
// 首秀）：organization_id 是租户键（只存 ID，契约④）；成员资格经
// organization 的 domain 端口校验（ADR-0007 通道②）；完成与删除限
// 创建者（creator_id 是资源所有权裁决依据，v1 不做组织级 RBAC——
// 角色语义归 access）。
//
// 契约：不 import infra/app/api；跨模块只允许 import 他模块 domain 包。
package task

import (
	"context"
	"fmt"
	"time"

	"github.com/lynxland/appkit/errcode"
)

// space 是本模块的错误码区段（契约⑤）。
var space = errcode.MustRegister("task")

// 哨兵错误：api 层转 grpc/status 时携带 ErrorInfo（Domain=task）。
// 成员资格哨兵复用 organization.ErrNotMember（跨模块 domain 级哨兵）。
var (
	ErrTaskNotFound = space.New("TASK_NOT_FOUND", "task not found")
	ErrEmptyTitle   = space.New("EMPTY_TITLE", "task title must not be empty")
	ErrNotCreator   = space.New("NOT_CREATOR", "only the task creator may perform this action")
)

// 事件目录（契约②）：app 层发布与 Module().PublishedEvents 共用同一常量。
const (
	EventTaskCreated   = "task.task.created"
	EventTaskCompleted = "task.task.completed"
)

// Task 是一条组织内任务（不变量：organizationID/creatorID/title 非空；
// CompletedAt 空表示未完成）。
type Task struct {
	ID             string
	OrganizationID string
	CreatorID      string
	Title          string
	Description    string
	CompletedAt    *time.Time
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// NewTask 构造新聚合（未完成）。
func NewTask(id, organizationID, creatorID, title, description string, now time.Time) (*Task, error) {
	if id == "" || organizationID == "" || creatorID == "" {
		return nil, fmt.Errorf("task: empty id/organizationID/creatorID")
	}
	if title == "" {
		return nil, ErrEmptyTitle
	}
	return &Task{
		ID: id, OrganizationID: organizationID, CreatorID: creatorID,
		Title: title, Description: description, CreatedAt: now, UpdatedAt: now,
	}, nil
}

// Complete 标记完成（幂等：已完成的再次 Complete 为 no-op——与成员
// 变更同款运维友好语义）。
func (t *Task) Complete(now time.Time) {
	if t.CompletedAt == nil {
		ts := now
		t.CompletedAt = &ts
		t.UpdatedAt = now
	}
}

// Completed 报告完成状态。
func (t *Task) Completed() bool { return t.CompletedAt != nil }

// Query 是任务列表查询（组织内分页；v1 无过滤维度）。
type Query struct {
	PageSize int
	Offset   int
}

// Repository 是 Task 聚合的持久化端口（infra 实现，经 Assemble 注入）。
// 租户语境进端口签名：Get/List/Delete 一律携带 organizationID——
// ID 泄露也跨组织取不到（ADR-0008 防漏的端口级表达）。
type Repository interface {
	// Create 插入任务。
	Create(ctx context.Context, t *Task) error
	// Get 按组织 + ID 取；缺失或跨组织映射 ErrTaskNotFound。
	Get(ctx context.Context, organizationID, id string) (*Task, error)
	// List 按 created_at DESC, id DESC 返回组织内一页与总数。
	List(ctx context.Context, organizationID string, q Query) ([]*Task, int, error)
	// Update 只写 completed_at/updated_at 白名单列（bun 显式列护栏）。
	Update(ctx context.Context, t *Task) error
	// Delete 按组织 + ID 删除，返回是否发生删除（幂等裁决依据）。
	Delete(ctx context.Context, organizationID, id string) (bool, error)
}
