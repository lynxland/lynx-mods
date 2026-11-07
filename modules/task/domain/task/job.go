// Package task 是后台任务与调度模块的领域层：Job 聚合（持久化任务队列
// 的最小事实）、RecurringRule 聚合（cron 周期规则）与仓储/handler 端口。
//
// 定位（ADR-0009 自足模块，v0.7.0 重写）：task = 应用级的后台任务与
// 调度基础设施——task queue（即时/延迟/定时入队、唯一键防重、退避重试、
// 死信）+ cron（持久化周期规则）。零模块依赖：job 属于应用全局，不带
// 组织/租户维度。worker 的 handler 注册属应用组合根（HandlerRegistry
// 端口），模块贡献其余一切（存储/认领/退避/调度/管理面）。
//
// 契约：不 import infra/app/api。
package task

import (
	"fmt"
	"time"

	"github.com/lynxland/appkit/errcode"
)

// space 是本模块的错误码区段（契约⑤）。
var space = errcode.MustRegister("task")

// 哨兵错误：api 层转 grpc/status 时携带 ErrorInfo（Domain=task）。
var (
	ErrJobNotFound   = space.New("JOB_NOT_FOUND", "job not found")
	ErrRuleNotFound  = space.New("RULE_NOT_FOUND", "recurring rule not found")
	ErrInvalidCron   = space.New("INVALID_CRON", "invalid cron expression")
	ErrInvalidJob    = space.New("INVALID_JOB", "invalid job input")
	ErrNotPending    = space.New("NOT_PENDING", "job is not pending")
	ErrNotRetryable  = space.New("NOT_RETRYABLE", "job is not dead or cancelled")
	ErrDuplicateType = space.New("DUPLICATE_TYPE", "job type conflicts with another rule")
)

// 发布的事件目录（契约②）：app 层发布与 Module().PublishedEvents 共用
// 同一常量，module.json 的 publishedEvents 与此同步。dead 事件供
// notification 等订阅方做死信告警（产品闭环）。
const (
	EventJobEnqueued  = "task.job.enqueued"
	EventJobCompleted = "task.job.completed"
	EventJobFailed    = "task.job.failed"
	EventJobDead      = "task.job.dead"
)

// Status 是任务状态机：PENDING → RUNNING → DONE /（失败退避）PENDING
// → … → DEAD；PENDING → CANCELLED（人工取消）。
type Status string

const (
	StatusPending   Status = "pending"
	StatusRunning   Status = "running"
	StatusDone      Status = "done"
	StatusDead      Status = "dead"
	StatusCancelled Status = "cancelled"
)

// Job 是一个持久化任务（不变量：id/type 非空、status 合法、
// max_attempts>=1、attempt<=max_attempts）。
type Job struct {
	ID          string
	Type        string
	Payload     string // JSON 文本（业务自定义）
	Status      Status
	Priority    int32     // 小值先执行（默认 0）
	RunAt       time.Time // 可执行时间（延迟/定时/退避重试的统一表达）
	Attempt     int32
	MaxAttempts int32
	UniqueKey   string // pending/running 存续期防重（可空）
	LastError   string
	CreatedAt   time.Time
	FinishedAt  *time.Time
}

// NewJob 构造新任务（PENDING）。
func NewJob(id, typ, payload string, runAt time.Time, priority, maxAttempts int32, uniqueKey string) (*Job, error) {
	if id == "" || typ == "" {
		return nil, fmt.Errorf("task: empty id/type")
	}
	if maxAttempts < 1 {
		maxAttempts = 5
	}
	return &Job{
		ID: id, Type: typ, Payload: payload, Status: StatusPending,
		Priority: priority, RunAt: runAt, MaxAttempts: maxAttempts,
		UniqueKey: uniqueKey, CreatedAt: runAt,
	}, nil
}

// ValidateName 校验类型/规则名（小写字母数字与点/中划线/下划线，2-64 字符）。
func ValidateName(s string) bool {
	if len(s) < 2 || len(s) > 64 {
		return false
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '.', r == '-', r == '_':
		default:
			return false
		}
	}
	return true
}

// NextRetryAt 计算失败后的下次可执行时间（退避：attempt² × base，上限
// cap；attempt 是已失败次数）。简单平方退避在多租户噪底下的可预期性
// 优于抖动指数，且 attempt 数小（默认 5）不会触达量级问题。
func NextRetryAt(attempt int32, base, cap time.Duration, now time.Time) time.Time {
	d := base * time.Duration(attempt) * time.Duration(attempt)
	if d > cap {
		d = cap
	}
	if d < 0 { // 溢出防御
		d = cap
	}
	return now.Add(d)
}
