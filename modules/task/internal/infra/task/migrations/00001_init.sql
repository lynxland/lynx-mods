-- +goose Up
-- task 模块（v0.7.0 重写：后台任务与调度，ADR-0009 自足模块）的初始迁移。

CREATE TABLE IF NOT EXISTS task_jobs (
    id           text PRIMARY KEY,
    type         text NOT NULL,
    payload      text NOT NULL DEFAULT '{}',
    status       text NOT NULL DEFAULT 'pending',
    priority     integer NOT NULL DEFAULT 0,
    run_at       timestamptz NOT NULL DEFAULT now(),
    attempt      integer NOT NULL DEFAULT 0,
    max_attempts integer NOT NULL DEFAULT 5,
    unique_key   text,
    last_error   text,
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now(),
    finished_at  timestamptz
);
-- 到期扫描（认领的主索引）：run_at 升序 + 优先级 + id 决胜，只扫 pending。
CREATE INDEX IF NOT EXISTS idx_task_jobs_due ON task_jobs (run_at, priority, id) WHERE status = 'pending';
-- 唯一键防重：仅在存续状态（pending/running）内唯一——已完成/死信的任务
-- 不阻塞同 key 重新入队。
CREATE UNIQUE INDEX IF NOT EXISTS idx_task_jobs_unique ON task_jobs (unique_key) WHERE unique_key IS NOT NULL AND status IN ('pending', 'running');
-- 管理面列表（keyset：created_at DESC + id 决胜）。
CREATE INDEX IF NOT EXISTS idx_task_jobs_list ON task_jobs (created_at DESC, id);

CREATE TABLE IF NOT EXISTS task_recurring_rules (
    id          text PRIMARY KEY,
    type        text NOT NULL,
    payload     text NOT NULL DEFAULT '{}',
    cron_expr   text NOT NULL,
    enabled     boolean NOT NULL DEFAULT true,
    last_run_at timestamptz,
    next_run_at timestamptz NOT NULL,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_task_rules_due ON task_recurring_rules (next_run_at) WHERE enabled;

-- +goose Down
DROP TABLE IF EXISTS task_recurring_rules;
DROP TABLE IF EXISTS task_jobs;
