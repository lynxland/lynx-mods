// Package task 是后台任务与调度模块的基础设施层：bun 仓储（SKIP LOCKED
// 原子认领）、worker runner 与周期调度器。
package task

import (
	"context"
	"database/sql"
	"errors"

	"time"

	"github.com/lynxland/lynx-start/modules/task/domain/task"

	"github.com/lynxland/appkit/contrib/bunx"
	"github.com/lynxland/appkit/contrib/bunx/buntx"
	"github.com/lynxland/appkit/crud"
	"github.com/uptrace/bun"
)

// jobModel 是 PO（持久化对象）：bun 标签在此，领域聚合不染 ORM。
type jobModel struct {
	bun.BaseModel `bun:"table:task_jobs,alias:j"`

	ID          string     `bun:"id,pk"`
	Type        string     `bun:"type"`
	Payload     string     `bun:"payload"`
	Status      string     `bun:"status"`
	Priority    int32      `bun:"priority"`
	RunAt       time.Time  `bun:"run_at"`
	Attempt     int32      `bun:"attempt"`
	MaxAttempts int32      `bun:"max_attempts"`
	UniqueKey   *string    `bun:"unique_key"`
	LastError   *string    `bun:"last_error"`
	CreatedAt   time.Time  `bun:"created_at"`
	UpdatedAt   time.Time  `bun:"updated_at"`
	FinishedAt  *time.Time `bun:"finished_at"`
}

type ruleModel struct {
	bun.BaseModel `bun:"table:task_recurring_rules,alias:r"`

	ID        string     `bun:"id,pk"`
	Type      string     `bun:"type"`
	Payload   string     `bun:"payload"`
	CronExpr  string     `bun:"cron_expr"`
	Enabled   bool       `bun:"enabled"`
	LastRunAt *time.Time `bun:"last_run_at"`
	NextRunAt time.Time  `bun:"next_run_at"`
	CreatedAt time.Time  `bun:"created_at"`
	UpdatedAt time.Time  `bun:"updated_at"`
}

// Repo 实现 task.Repository。
type Repo struct {
	db *bun.DB
}

// NewRepository 构造仓储（Assemble 调用）。
func NewRepository(db *bun.DB) *Repo { return &Repo{db: db} }

var _ task.Repository = (*Repo)(nil)

func toJobDomain(m *jobModel) *task.Job {
	j := &task.Job{
		ID: m.ID, Type: m.Type, Payload: m.Payload, Status: task.Status(m.Status),
		Priority: m.Priority, RunAt: m.RunAt, Attempt: m.Attempt, MaxAttempts: m.MaxAttempts,
		CreatedAt: m.CreatedAt, FinishedAt: m.FinishedAt,
	}
	if m.UniqueKey != nil {
		j.UniqueKey = *m.UniqueKey
	}
	if m.LastError != nil {
		j.LastError = *m.LastError
	}
	return j
}

func toRuleDomain(m *ruleModel) *task.RecurringRule {
	return &task.RecurringRule{
		ID: m.ID, Type: m.Type, Payload: m.Payload, CronExpr: m.CronExpr,
		Enabled: m.Enabled, LastRunAt: m.LastRunAt, NextRunAt: m.NextRunAt,
		CreatedAt: m.CreatedAt, UpdatedAt: m.UpdatedAt,
	}
}

// Insert 入队；unique_key 与存续任务冲突时返回已存在任务（幂等）。
func (r *Repo) Insert(ctx context.Context, j *task.Job) (*task.Job, bool, error) {
	m := &jobModel{
		ID: j.ID, Type: j.Type, Payload: j.Payload, Status: string(task.StatusPending),
		Priority: j.Priority, RunAt: j.RunAt, Attempt: 0, MaxAttempts: j.MaxAttempts,
		CreatedAt: j.CreatedAt, UpdatedAt: j.CreatedAt,
	}
	if j.UniqueKey != "" {
		m.UniqueKey = &j.UniqueKey
	}
	_, err := buntx.Conn(ctx, r.db).NewInsert().Model(m).
		On("CONFLICT DO NOTHING").Returning("").Exec(ctx)
	if err != nil {
		return nil, false, err
	}
	if j.UniqueKey != "" {
		// 唯一键冲突（ON CONFLICT DO NOTHING 吸收）：读回存续中的同 key 任务。
		existing, gerr := r.getByUniqueKey(ctx, j.UniqueKey)
		if gerr == nil && existing != nil && existing.ID != j.ID {
			return existing, false, nil
		}
	}
	return j, true, nil
}

func (r *Repo) getByUniqueKey(ctx context.Context, key string) (*task.Job, error) {
	m := new(jobModel)
	err := buntx.Conn(ctx, r.db).NewSelect().Model(m).
		Where("j.unique_key = ? AND j.status IN ('pending','running')", key).
		Limit(1).Scan(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return toJobDomain(m), nil
}

// Get 按 ID 取；缺失映射 ErrJobNotFound。
func (r *Repo) Get(ctx context.Context, id string) (*task.Job, error) {
	m := new(jobModel)
	err := buntx.Conn(ctx, r.db).NewSelect().Model(m).
		Where("j.id = ?", id).Scan(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, task.ErrJobNotFound
	}
	if err != nil {
		return nil, err
	}
	return toJobDomain(m), nil
}

// Claim 原子认领：先回收 stale RUNNING（进程崩溃遗留——at-least-once 的
// 崩溃恢复），再 FOR UPDATE SKIP LOCKED 认领到期 pending（并发 worker
// 互不阻塞、不重复）。单条 SQL 完成状态迁移与 attempt 递增。
func (r *Repo) Claim(ctx context.Context, f task.ClaimFilter) ([]*task.Job, error) {
	if f.StaleBefore.After(time.Time{}) {
		if _, err := buntx.Conn(ctx, r.db).NewUpdate().
			Model((*jobModel)(nil)).
			Set("status = 'pending'").
			Set("updated_at = ?", f.Now).
			Where("status = 'running' AND updated_at < ?", f.StaleBefore).
			Exec(ctx); err != nil {
			return nil, err
		}
	}
	var rows []jobModel
	err := buntx.Conn(ctx, r.db).NewUpdate().
		Model(&rows).
		Set("status = 'running'").
		Set("attempt = j.attempt + 1").
		Set("updated_at = ?", f.Now).
		Where("j.id IN (SELECT j.id FROM task_jobs j WHERE j.status = 'pending' AND j.run_at <= ? ORDER BY j.run_at, j.priority, j.id LIMIT ? FOR UPDATE SKIP LOCKED)", f.Now, f.Limit).
		Returning("*").
		Scan(ctx)
	if err != nil {
		return nil, err
	}
	jobs := make([]*task.Job, 0, len(rows))
	for i := range rows {
		jobs = append(jobs, toJobDomain(&rows[i]))
	}
	return jobs, nil
}

// Complete 置 DONE（仅 RUNNING；幂等裁决交调用方）。
func (r *Repo) Complete(ctx context.Context, id string, now time.Time) error {
	_, err := buntx.Conn(ctx, r.db).NewUpdate().
		Model((*jobModel)(nil)).
		Set("status = 'done'").
		Set("updated_at = ?", now).
		Set("finished_at = ?", now).
		Set("last_error = NULL").
		Where("id = ? AND status = 'running'", id).
		Exec(ctx)
	return err
}

// Fail 记录失败并按 dead 决定终态或退避回 PENDING。
func (r *Repo) Fail(ctx context.Context, id, errMsg string, retryAt time.Time, dead bool, now time.Time) error {
	status := "pending"
	if dead {
		status = "dead"
	}
	_, err := buntx.Conn(ctx, r.db).NewUpdate().
		Model((*jobModel)(nil)).
		Set("status = ?", status).
		Set("last_error = ?", errMsg).
		Set("run_at = ?", retryAt).
		Set("updated_at = ?", now).
		Set("finished_at = ?", now). // 死信终态时间；pending 的 finished_at 由下次完成覆盖
		Where("id = ? AND status = 'running'", id).
		Exec(ctx)
	return err
}

// Retry 死信/已取消 → PENDING。
func (r *Repo) Retry(ctx context.Context, id string, now time.Time) (*task.Job, error) {
	res, err := buntx.Conn(ctx, r.db).NewUpdate().
		Model((*jobModel)(nil)).
		Set("status = 'pending'").
		Set("attempt = 0").
		Set("last_error = NULL").
		Set("finished_at = NULL").
		Set("run_at = ?", now).
		Set("updated_at = ?", now).
		Where("id = ? AND status IN ('dead','cancelled')", id).
		Exec(ctx)
	if err != nil {
		return nil, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, task.ErrNotRetryable
	}
	return r.Get(ctx, id)
}

// Cancel 仅 PENDING → CANCELLED。
func (r *Repo) Cancel(ctx context.Context, id string, now time.Time) (*task.Job, error) {
	res, err := buntx.Conn(ctx, r.db).NewUpdate().
		Model((*jobModel)(nil)).
		Set("status = 'cancelled'").
		Set("finished_at = ?", now).
		Set("updated_at = ?", now).
		Where("id = ? AND status = 'pending'", id).
		Exec(ctx)
	if err != nil {
		return nil, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, task.ErrNotPending
	}
	return r.Get(ctx, id)
}

// List 按 filter 分页（keyset：created_at DESC + id 决胜）。
func (r *Repo) List(ctx context.Context, f task.JobFilter, plan crud.PagePlan) ([]*task.Job, int, error) {
	var rows []jobModel
	apply := func(q *bun.SelectQuery) *bun.SelectQuery {
		if f.Status != "" {
			q = q.Where("j.status = ?", string(f.Status))
		}
		if f.Type != "" {
			q = q.Where("j.type = ?", f.Type)
		}
		return q
	}
	if err := bunx.ApplyPage(apply(buntx.Conn(ctx, r.db).NewSelect().Model(&rows)), plan).Scan(ctx); err != nil {
		return nil, 0, err
	}
	count, err := apply(buntx.Conn(ctx, r.db).NewSelect().Model((*jobModel)(nil))).Count(ctx)
	if err != nil {
		return nil, 0, err
	}
	jobs := make([]*task.Job, 0, len(rows))
	for i := range rows {
		jobs = append(jobs, toJobDomain(&rows[i]))
	}
	return jobs, int(count), nil
}

// Stats 各状态计数。
func (r *Repo) Stats(ctx context.Context) (task.JobStats, error) {
	var rows []struct {
		Status string `bun:"status"`
		N      int64  `bun:"n"`
	}
	err := buntx.Conn(ctx, r.db).NewSelect().
		TableExpr("task_jobs").
		Column("status").
		ColumnExpr("count(*) AS n").
		Group("status").
		Scan(ctx, &rows)
	if err != nil {
		return task.JobStats{}, err
	}
	var s task.JobStats
	for _, row := range rows {
		switch task.Status(row.Status) {
		case task.StatusPending:
			s.Pending = row.N
		case task.StatusRunning:
			s.Running = row.N
		case task.StatusDone:
			s.Done = row.N
		case task.StatusDead:
			s.Dead = row.N
		case task.StatusCancelled:
			s.Cancelled = row.N
		}
	}
	return s, nil
}

// UpsertRule 新建或整写。
func (r *Repo) UpsertRule(ctx context.Context, rule *task.RecurringRule) error {
	m := &ruleModel{
		ID: rule.ID, Type: rule.Type, Payload: rule.Payload, CronExpr: rule.CronExpr,
		Enabled: rule.Enabled, LastRunAt: rule.LastRunAt, NextRunAt: rule.NextRunAt,
		CreatedAt: rule.CreatedAt, UpdatedAt: rule.UpdatedAt,
	}
	_, err := buntx.Conn(ctx, r.db).NewInsert().Model(m).
		On("CONFLICT (id) DO UPDATE SET type = EXCLUDED.type, payload = EXCLUDED.payload, cron_expr = EXCLUDED.cron_expr, enabled = EXCLUDED.enabled, next_run_at = EXCLUDED.next_run_at, updated_at = EXCLUDED.updated_at").
		Exec(ctx)
	return err
}

// GetRule 按 ID 取；缺失映射 ErrRuleNotFound。
func (r *Repo) GetRule(ctx context.Context, id string) (*task.RecurringRule, error) {
	m := new(ruleModel)
	err := buntx.Conn(ctx, r.db).NewSelect().Model(m).
		Where("r.id = ?", id).Scan(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, task.ErrRuleNotFound
	}
	if err != nil {
		return nil, err
	}
	return toRuleDomain(m), nil
}

// DeleteRule 删除；缺失映射 ErrRuleNotFound。
func (r *Repo) DeleteRule(ctx context.Context, id string) error {
	res, err := buntx.Conn(ctx, r.db).NewDelete().
		Model((*ruleModel)(nil)).Where("id = ?", id).Exec(ctx)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return task.ErrRuleNotFound
	}
	return nil
}

// ListRules 分页（keyset：next_run_at 升序 + id 决胜）。
func (r *Repo) ListRules(ctx context.Context, plan crud.PagePlan) ([]*task.RecurringRule, int, error) {
	var rows []ruleModel
	if err := bunx.ApplyPage(buntx.Conn(ctx, r.db).NewSelect().Model(&rows), plan).Scan(ctx); err != nil {
		return nil, 0, err
	}
	count, err := buntx.Conn(ctx, r.db).NewSelect().Model((*ruleModel)(nil)).Count(ctx)
	if err != nil {
		return nil, 0, err
	}
	rules := make([]*task.RecurringRule, 0, len(rows))
	for i := range rows {
		rules = append(rules, toRuleDomain(&rows[i]))
	}
	return rules, int(count), nil
}

// ClaimDueRules 认领到期规则并原子推进（事务内 SELECT FOR UPDATE
// SKIP LOCKED + 逐条按 cron 重算 next_run_at）：返回的规则已持久化推进
// （last_run_at=now、next_run_at=cron.Next(now)——错过多次触发只补
// 一次），调用方只需入队；入队失败只丢本次触发（下轮按新 next_run_at
// 继续，不重复不堵死）。调度器多实例并发安全。
func (r *Repo) ClaimDueRules(ctx context.Context, now time.Time, limit int) ([]*task.RecurringRule, error) {
	var out []*task.RecurringRule
	err := r.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		var rows []ruleModel
		err := tx.NewSelect().Model(&rows).
			Where("enabled AND next_run_at <= ?", now).
			Order("next_run_at", "id").Limit(int64(limit)).
			For("UPDATE SKIP LOCKED").
			Scan(ctx)
		if err != nil {
			return err
		}
		for i := range rows {
			rule := toRuleDomain(&rows[i])
			next, err := rule.NextAfter(now)
			if err != nil {
				return err
			}
			last := now
			rule.LastRunAt, rule.NextRunAt, rule.UpdatedAt = &last, next, now
			if _, err := tx.NewUpdate().Model(&rows[i]).
				Set("last_run_at = ?", last).
				Set("next_run_at = ?", next).
				Set("updated_at = ?", now).
				WherePK().Exec(ctx); err != nil {
				return err
			}
			out = append(out, rule)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}
