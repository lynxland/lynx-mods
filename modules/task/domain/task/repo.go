package task

import (
	"context"
	"time"

	"github.com/lynxland/appkit/crud"
)

// Handler 处理一个任务；ctx 承载取消（优雅停机排水）。返回错误即失败
// （按退避重试直至死信）。
type Handler func(ctx context.Context, j *Job) error

// HandlerRegistry 是 worker 的 handler 解析端口——应用组合根实现
// （类型名 → Handler）。未注册的类型按失败处理（错误可见，退避至死信
// ——配置错误不该静默跳过）。
type HandlerRegistry interface {
	Lookup(jobType string) (Handler, bool)
}

// ClaimFilter 是认领参数：StaleBefore 之前的 RUNNING 视为卡死（进程
// 崩溃遗留），先回收回 PENDING 再认领（at-least-once 的崩溃恢复语义）。
type ClaimFilter struct {
	Now         time.Time
	Limit       int
	StaleBefore time.Time
}

// Repository 是 Job/RecurringRule 的持久化端口（infra 实现，经 Assemble
// 注入）。方法一律接收 ctx：适配器从 ctx 取事务连接（appkit/uow/buntx），
// 端口签名不得出现驱动类型。
type Repository interface {
	// Insert 入队；unique_key 与存续中的任务（pending/running）冲突时
	// 返回已存在的任务与 created=false（幂等，不报错）。
	Insert(ctx context.Context, j *Job) (existing *Job, created bool, err error)
	// Get 按 ID 取；缺失映射 ErrJobNotFound。
	Get(ctx context.Context, id string) (*Job, error)
	// Claim 原子认领一批到期任务（FOR UPDATE SKIP LOCKED）：置 RUNNING、
	// attempt+1，先回收 stale RUNNING 回 PENDING。并发 worker 安全。
	Claim(ctx context.Context, f ClaimFilter) ([]*Job, error)
	// Complete 置 DONE（仅 RUNNING）。
	Complete(ctx context.Context, id string, now time.Time) error
	// Fail 记录失败：dead=true 时置 DEAD（终态），否则按 retryAt 退回
	// PENDING。仅 RUNNING 有效。
	Fail(ctx context.Context, id, errMsg string, retryAt time.Time, dead bool, now time.Time) error
	// Retry 死信/已取消 → PENDING（attempt 归零，清 last_error）。
	Retry(ctx context.Context, id string, now time.Time) (*Job, error)
	// Cancel 仅 PENDING → CANCELLED。
	Cancel(ctx context.Context, id string, now time.Time) (*Job, error)
	// List 按 filter（status/type 可空）分页（keyset：created_at DESC +
	// id 决胜）返回一页与总数。
	List(ctx context.Context, f JobFilter, plan crud.PagePlan) ([]*Job, int, error)
	// Stats 各状态计数。
	Stats(ctx context.Context) (JobStats, error)

	// UpsertRule 新建或整写规则（按 ID）。
	UpsertRule(ctx context.Context, r *RecurringRule) error
	// GetRule 按 ID 取规则；缺失映射 ErrRuleNotFound。
	GetRule(ctx context.Context, id string) (*RecurringRule, error)
	// DeleteRule 删除规则；缺失映射 ErrRuleNotFound。
	DeleteRule(ctx context.Context, id string) error
	// ListRules 分页（keyset：next_run_at 升序 + id 决胜）。
	ListRules(ctx context.Context, plan crud.PagePlan) ([]*RecurringRule, int, error)
	// ClaimDueRules 认领到期规则（FOR UPDATE SKIP LOCKED，enabled 且
	// next_run_at <= now），推进 next_run_at 并记录 last_run_at——
	// 返回的规则已持久化推进（调用方只需入队，崩溃恢复不重复触发）。
	ClaimDueRules(ctx context.Context, now time.Time, limit int) ([]*RecurringRule, error)
}

// JobFilter 是任务列表过滤（零值 = 不过滤）。
type JobFilter struct {
	Status Status
	Type   string
}

// JobStats 是各状态计数（监控/告警消费）。
type JobStats struct {
	Pending   int64
	Running   int64
	Done      int64
	Dead      int64
	Cancelled int64
}
