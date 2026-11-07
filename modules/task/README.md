# task

后台任务与调度模块（v0.7.0 重写，ADR-0009 自足模块）——**单独安装即得
完整的任务系统**：持久化任务队列（即时/延迟/定时入队、唯一键防重、平方
退避重试、死信）+ cron 周期规则 + 管理面。零模块依赖：job 属于应用
全局，不带组织/租户维度。旧的「组织内任务示例」（v0.6.0 及以前）已随
重定位移除——组织挂靠业务资源的姿势见 module-authoring 文档。

## 安装

```bash
lynx-start add task            # 最新（registry 工作区形态）
lynx-start add task@0.7.0      # 锁定 tag 版本
```

## 接线 runner（必读）

模块自治部分（存储/认领/退避/调度/管理 RPC）随安装即用；**worker 必须由
应用接线**——它携带你的 handler 表。在应用内 `modules/task/module.go`
（桥接包，注入物、update 不覆盖，可自由追加）加：

```go
import (
    domaintask "example.com/myapp/modules/task/domain/task"
    infratask "example.com/myapp/modules/task/internal/infra/task"
    "github.com/uptrace/bun"
)

// NewTaskRunner 构造任务执行器（在应用 main 注册为 service）。
func NewTaskRunner(db *bun.DB, logger *slog.Logger) *infratask.Runner {
    handlers := infratask.HandlerMap{ // 或实现 domaintask.HandlerRegistry
        "email.send": func(ctx context.Context, j *domaintask.Job) error {
            return sendEmail(ctx, j.Payload)
        },
    }
    return infratask.NewRunner(db, handlers, logger, infratask.RunnerOptions{
        Concurrency: 4, Interval: time.Second,
    })
}
```

handler 必须**幂等**：认领与执行是 at-least-once（进程崩溃的 RUNNING 由
stale 回收重新入队，默认 10 分钟阈值）。

## 语义

- **入队**：`run_at`（定时）或 `delay_seconds`（延迟）二选一，皆空即时；
  `unique_key` 在 pending/running 存续期内防重（完成/死信后同 key 可再入队）。
- **认领**：`FOR UPDATE SKIP LOCKED` 原子认领（并发 worker 互不阻塞不重
  复），按 `run_at → priority → id` 排序。
- **失败退避**：attempt² × 30s（上限 1h，可由 RunnerOptions 调）；超过
  `max_attempts`（默认 5）转死信。
- **cron 规则**：5 段标准表达式或 `@every 1h`（robfig/cron v3）；调度器
  多实例安全（SKIP LOCKED + 触发唯一键双保险）；错过多次触发只补一次。
- **管理面**：权限注解 `task.job.read` / `task.job.write` /
  `task.rule.write`——与 account 同装时在角色目录授予（如 admin:
  `["task.job.read", "task.job.write", "task.rule.write"]`）。

## 配置

```yaml
task:
  scheduler_interval: 30s  # 周期规则调度轮询间隔
```

## 发布事件

与 `Module().PublishedEvents` 一致（启动期校验）：

- `task.job.enqueued` / `task.job.completed` / `task.job.failed` / `task.job.dead`

`task.job.dead` 供 notification 订阅做死信告警（产品闭环）。

## 依赖

- 模块依赖：无（零依赖自足）
- kernel 地板：appkit >=0.8.0
- Go 依赖：appkit、appkit/contrib/bunx、grpcapi、lynx、robfig/cron/v3

---

**English**: Background jobs & scheduling module (v0.7.0 rewrite, ADR-0009
self-sufficient module) — a durable task queue (immediate/delayed/scheduled
enqueue, unique-key dedupe, squared backoff retries, dead letters) plus cron
recurring rules and a management API. Zero module dependencies. Workers are
wired by the application (handler table via the bridge package); claiming is
atomic via `FOR UPDATE SKIP LOCKED`; execution is at-least-once — handlers
must be idempotent.
