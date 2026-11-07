// Package task 是后台任务与调度模块的用例层：编排领域与端口（事件/
// 仓储）。入队与失败路径发布事件（供 notification 等订阅方做完成通知/
// 死信告警）；管理面（列表/重试/取消/规则 CRUD）按 proto 注解的权限
// 裁决后直达仓储。
package task

import (
	"context"
	"encoding/json"
	"time"

	"github.com/lynxland/lynx-start/modules/task/domain/task"

	"github.com/lynxland/appkit/crud"
	"github.com/lynxland/appkit/events"
	"github.com/lynxland/appkit/idgen"
)

// Config 是 task 模块的配置段。
type Config struct {
	// SchedulerInterval 是周期调度器的轮询间隔。
	SchedulerInterval time.Duration `mapstructure:"scheduler_interval"`
}

// ApplyDefaults 落默认值（LoadSection 的 defaults 钩子）。
func (c *Config) ApplyDefaults() {
	if c.SchedulerInterval <= 0 {
		c.SchedulerInterval = 30 * time.Second
	}
}

// UseCase 是模块的用例集。
type UseCase struct {
	repo   task.Repository
	events events.Publisher
	now    func() time.Time
}

// New 构造用例（Assemble 调用）。
func New(repo task.Repository, publisher events.Publisher) *UseCase {
	return &UseCase{repo: repo, events: publisher, now: time.Now}
}

// ---- Job 面task ----

// EnqueueInput 是入队输入（RunAt 与 Delay 二选一，皆空为即时）。
type EnqueueInput struct {
	Type        string
	Payload     string
	RunAt       *time.Time
	Delay       *time.Duration
	UniqueKey   string
	Priority    int32
	MaxAttempts int32
}

// Enqueue 入队（即时/延迟/定时；唯一键在 pending/running 存续期防重——
// 冲突返回已存在任务与 created=false）。
func (u *UseCase) Enqueue(ctx context.Context, in EnqueueInput) (*task.Job, bool, error) {
	if !task.ValidateName(in.Type) {
		return nil, false, task.ErrInvalidJob
	}
	if in.Payload == "" {
		in.Payload = "{}"
	}
	if !json.Valid([]byte(in.Payload)) {
		return nil, false, task.ErrInvalidJob
	}
	now := u.now().UTC()
	runAt := now
	switch {
	case in.RunAt != nil && in.Delay != nil:
		return nil, false, task.ErrInvalidJob // 互斥
	case in.RunAt != nil:
		runAt = in.RunAt.UTC()
	case in.Delay != nil:
		runAt = now.Add(*in.Delay)
	}
	j, err := task.NewJob(idgen.New(), in.Type, in.Payload, runAt, in.Priority, in.MaxAttempts, in.UniqueKey)
	if err != nil {
		return nil, false, err
	}
	existing, created, err := u.repo.Insert(ctx, j)
	if err != nil {
		return nil, false, err
	}
	if created {
		if err := u.publish(ctx, task.EventJobEnqueued, map[string]string{"id": j.ID, "type": j.Type}); err != nil {
			return nil, false, err
		}
	}
	return existing, created, nil
}

// Get 取任务。
func (u *UseCase) Get(ctx context.Context, id string) (*task.Job, error) {
	return u.repo.Get(ctx, id)
}

// jobsSpec 是任务列表的分页规约（keyset：created_at DESC + id 决胜）。
var jobsSpec = crud.Spec{
	Modes:        crud.ModeSetKeyset,
	Sortable:     map[string]string{"createdAt": "j.created_at", "id": "j.id"},
	Tiebreak:     "id",
	DefaultLimit: 50,
	MaxLimit:     200,
}

// ListJobsResult 是任务列表用例的产物。
type ListJobsResult struct {
	Jobs      []*task.Job
	NextToken string
	TotalSize int32
}

// List 列任务（status/type 过滤可空，keyset 分页）。
func (u *UseCase) List(ctx context.Context, f task.JobFilter, pageSize int32, pageToken string) (*ListJobsResult, error) {
	q, err := crud.Parse[task.JobFilter](jobsSpec, crud.RawParams{After: pageToken, Limit: pageSize, OrderBy: "createdAt desc"}, f)
	if err != nil {
		return nil, err
	}
	jobs, total, err := u.repo.List(ctx, f, q.PagePlan)
	if err != nil {
		return nil, err
	}
	next := ""
	if crud.HasMore(int32(len(jobs)), q.Limit) && len(jobs) > 0 {
		last := jobs[len(jobs)-1]
		if next, err = crud.Cursor(q, []string{last.CreatedAt.UTC().Format(time.RFC3339Nano), last.ID}); err != nil {
			return nil, err
		}
	}
	return &ListJobsResult{Jobs: jobs, NextToken: next, TotalSize: int32(total)}, nil
}

// Retry 重试死信/已取消任务（attempt 归零）。
func (u *UseCase) Retry(ctx context.Context, id string) (*task.Job, error) {
	j, err := u.repo.Retry(ctx, id, u.now().UTC())
	if err != nil {
		return nil, err
	}
	if err := u.publish(ctx, task.EventJobEnqueued, map[string]string{"id": j.ID, "type": j.Type}); err != nil {
		return nil, err
	}
	return j, nil
}

// Cancel 取消待执行任务（仅 pending）。
func (u *UseCase) Cancel(ctx context.Context, id string) (*task.Job, error) {
	return u.repo.Cancel(ctx, id, u.now().UTC())
}

// Stats 各状态计数。
func (u *UseCase) Stats(ctx context.Context) (task.JobStats, error) {
	return u.repo.Stats(ctx)
}

// ---- RecurringRule 面 ----

// rulesSpec 是规则列表的分页规约（keyset：next_run_at 升序 + id 决胜）。
var rulesSpec = crud.Spec{
	Modes:        crud.ModeSetKeyset,
	Sortable:     map[string]string{"nextRunAt": "r.next_run_at", "id": "r.id"},
	Tiebreak:     "id",
	DefaultLimit: 50,
	MaxLimit:     200,
}

// ListRulesResult 是规则列表用例的产物。
type ListRulesResult struct {
	Rules     []*task.RecurringRule
	NextToken string
	TotalSize int32
}

// ListRules 列周期规则（keyset 分页，next_run_at 升序）。
func (u *UseCase) ListRules(ctx context.Context, pageSize int32, pageToken string) (*ListRulesResult, error) {
	q, err := crud.Parse[any](rulesSpec, crud.RawParams{After: pageToken, Limit: pageSize, OrderBy: "nextRunAt"}, nil)
	if err != nil {
		return nil, err
	}
	rules, total, err := u.repo.ListRules(ctx, q.PagePlan)
	if err != nil {
		return nil, err
	}
	next := ""
	if crud.HasMore(int32(len(rules)), q.Limit) && len(rules) > 0 {
		last := rules[len(rules)-1]
		if next, err = crud.Cursor(q, []string{last.NextRunAt.UTC().Format(time.RFC3339Nano), last.ID}); err != nil {
			return nil, err
		}
	}
	return &ListRulesResult{Rules: rules, NextToken: next, TotalSize: int32(total)}, nil
}

// UpsertRule 新建或整写周期规则（id 空=新建；cron 校验 fail-closed）。
func (u *UseCase) UpsertRule(ctx context.Context, id, typ, payload, cronExpr string, enabled bool) (*task.RecurringRule, error) {
	if !task.ValidateName(typ) {
		return nil, task.ErrInvalidJob
	}
	now := u.now().UTC()
	if id == "" {
		id = idgen.New()
		r, err := task.NewRecurringRule(id, typ, payload, cronExpr, enabled, now)
		if err != nil {
			return nil, err
		}
		return r, u.repo.UpsertRule(ctx, r)
	}
	// 更新：保留 last_run_at，重算 next_run_at。
	old, err := u.repo.GetRule(ctx, id)
	if err != nil {
		return nil, err
	}
	old.Type, old.Payload, old.CronExpr, old.Enabled = typ, payload, cronExpr, enabled
	next, err := old.NextAfter(now)
	if err != nil {
		return nil, err
	}
	old.NextRunAt, old.UpdatedAt = next, now
	return old, u.repo.UpsertRule(ctx, old)
}

// DeleteRule 删除规则。
func (u *UseCase) DeleteRule(ctx context.Context, id string) error {
	return u.repo.DeleteRule(ctx, id)
}

// publish 构造事件信封（载荷为最小事实集）。
func (u *UseCase) publish(ctx context.Context, name string, payload map[string]string) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	return u.events.Publish(ctx, events.Envelope{
		ID:         idgen.New(),
		Name:       name,
		Module:     "task",
		OccurredAt: u.now().UTC(),
		Payload:    raw,
	})
}
