package task

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/lynxland/lynx-start/modules/task/domain/task"

	"github.com/lynxland/appkit/clock"
	"github.com/lynxland/lynx"
	"github.com/uptrace/bun"
)

// RunnerOptions 是 worker 的运行参数（零值取默认）。
type RunnerOptions struct {
	Concurrency    int           // 并发 worker 数（默认 2）
	Interval       time.Duration // 空转轮询间隔（默认 1s）
	BatchSize      int           // 每 worker 单次认领上限（默认 5）
	BackoffBase    time.Duration // 失败退避基数（默认 30s；attempt² × base）
	BackoffCap     time.Duration // 退避上限（默认 1h）
	StaleAfter     time.Duration // RUNNING 卡死回收阈值（默认 10m）
	ClaimTimeout   time.Duration // 单次认领超时（默认 5s）
	HandlerTimeout time.Duration // 单个 handler 执行预算（默认 5m）
}

func (o *RunnerOptions) applyDefaults() {
	if o.Concurrency <= 0 {
		o.Concurrency = 2
	}
	if o.Interval <= 0 {
		o.Interval = time.Second
	}
	if o.BatchSize <= 0 {
		o.BatchSize = 5
	}
	if o.BackoffBase <= 0 {
		o.BackoffBase = 30 * time.Second
	}
	if o.BackoffCap <= 0 {
		o.BackoffCap = time.Hour
	}
	if o.StaleAfter <= 0 {
		o.StaleAfter = 10 * time.Minute
	}
	if o.ClaimTimeout <= 0 {
		o.ClaimTimeout = 5 * time.Second
	}
	if o.HandlerTimeout <= 0 {
		o.HandlerTimeout = 5 * time.Minute
	}
}

// Runner 是任务执行器（lynx.Service）：N 个 worker 循环「认领→执行→
// 完成/失败退避」。认领与执行都是 at-least-once：进程崩溃的 RUNNING 由
// stale 回收重新入队，handler 必须幂等。
//
// Runner 不进 Assembly（worker 必须携带应用的 handler 表，Assemble 的
// Deps 无法承载）——应用组合根经模块桥接包转发构造：
//
//	svc := task.NewRunner(db, handlers, logger, task.RunnerOptions{...})
//	app.RegisterService(svc) // 或并入应用的 lynx.Runner
type Runner struct {
	repo     task.Repository
	handlers task.HandlerRegistry
	logger   *slog.Logger
	opts     RunnerOptions
	now      clock.Clock
}

// NewRunner 构造执行器（应用组合根调用；api 包转发导出）。
func NewRunner(db *bun.DB, handlers task.HandlerRegistry, logger *slog.Logger, opts RunnerOptions) *Runner {
	opts.applyDefaults()
	if logger == nil {
		logger = slog.Default()
	}
	return &Runner{repo: NewRepository(db), handlers: handlers, logger: logger, opts: opts, now: clock.System{}}
}

var _ lynx.Service = (*Runner)(nil)

// Name 实现 lynx.Service。
func (r *Runner) Name() string { return "task-runner" }

// Init 实现 lynx.Service（无初始化动作）。
func (r *Runner) Init(lynx.AppContext) error { return nil }

// Start 阻塞运行至 ctx 取消：worker 全部退出（在途任务带预算完成——
// 优雅排水；预算内未完成的由下次启动的 stale 回收兜底）。
func (r *Runner) Start(ctx context.Context) error {
	var wg sync.WaitGroup
	for i := 0; i < r.opts.Concurrency; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			r.workerLoop(ctx, n)
		}(i)
	}
	wg.Wait()
	return nil
}

// Stop 实现 lynx.Service（排水由 Start 的 ctx 取消驱动）。
func (r *Runner) Stop(context.Context) error { return nil }

func (r *Runner) workerLoop(ctx context.Context, n int) {
	for {
		if ctx.Err() != nil {
			return
		}
		jobs, err := r.claim(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			r.logger.Error("task runner: claim failed", "worker", n, "error", err)
			r.sleep(ctx)
			continue
		}
		if len(jobs) == 0 {
			r.sleep(ctx)
			continue
		}
		for _, j := range jobs {
			if ctx.Err() != nil {
				return // 排水边界：认领批内也不再启动新 handler
			}
			r.execute(ctx, j)
		}
	}
}

func (r *Runner) claim(ctx context.Context) ([]*task.Job, error) {
	cctx, cancel := context.WithTimeout(ctx, r.opts.ClaimTimeout)
	defer cancel()
	now := r.now.Now()
	return r.repo.Claim(cctx, task.ClaimFilter{
		Now: now, Limit: r.opts.BatchSize, StaleBefore: now.Add(-r.opts.StaleAfter),
	})
}

func (r *Runner) execute(ctx context.Context, j *task.Job) {
	h, ok := r.handlers.Lookup(j.Type)
	if !ok {
		// 配置错误可见化：按失败退避直至死信（不静默跳过、不堵队列）。
		r.fail(ctx, j, errors.New("task: no handler registered for type "+j.Type))
		return
	}
	hctx, cancel := context.WithTimeout(ctx, r.opts.HandlerTimeout)
	defer cancel()
	if err := h(hctx, j); err != nil {
		r.fail(ctx, j, err)
		return
	}
	if err := r.repo.Complete(ctx, j.ID, r.now.Now()); err != nil {
		r.logger.Error("task runner: complete write failed", "job", j.ID, "error", err)
	}
}

func (r *Runner) fail(ctx context.Context, j *task.Job, cause error) {
	now := r.now.Now()
	dead := j.Attempt >= j.MaxAttempts
	retryAt := task.NextRetryAt(j.Attempt, r.opts.BackoffBase, r.opts.BackoffCap, now)
	if err := r.repo.Fail(ctx, j.ID, cause.Error(), retryAt, dead, now); err != nil {
		r.logger.Error("task runner: fail write failed", "job", j.ID, "error", err)
		return
	}
	if dead {
		r.logger.Error("task runner: job moved to dead", "job", j.ID, "type", j.Type, "attempts", j.Attempt, "last_error", cause)
	} else {
		r.logger.Warn("task runner: job failed, will retry", "job", j.ID, "type", j.Type, "attempt", j.Attempt, "retry_at", retryAt, "error", cause)
	}
}

func (r *Runner) sleep(ctx context.Context) {
	select {
	case <-ctx.Done():
	case <-time.After(r.opts.Interval):
	}
}

// HandlerMap 是 HandlerRegistry 的 map 便捷实现（应用接线最常用形态）。
type HandlerMap map[string]task.Handler

// Lookup 实现 task.HandlerRegistry。
func (m HandlerMap) Lookup(jobType string) (task.Handler, bool) { h, ok := m[jobType]; return h, ok }

var _ task.HandlerRegistry = HandlerMap(nil)
