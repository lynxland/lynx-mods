package task

import (
	"context"
	"log/slog"
	"time"

	"github.com/lynxland/lynx-start/modules/task/domain/task"

	"github.com/lynxland/appkit/idgen"
	"github.com/lynxland/appkit/periodic"
)

// NewScheduler 构造周期调度器（Assembly.Services 贡献；periodic 电池
// 驱动——预算轮次/panic 恢复）：每轮认领到期规则（原子推进）并为每条
// 入队一个 job（unique_key=<rule_id>:<触发时刻>——调度器重放安全）。
// 多实例并发安全（ClaimDueRules 的 SKIP LOCKED + unique_key 双保险）。
func NewScheduler(repo task.Repository, interval time.Duration, logger *slog.Logger) *periodic.Periodic {
	return periodic.New("task-recurring-scheduler", interval, time.Minute, func(ctx context.Context) error {
		return tickRules(ctx, repo, logger)
	}, logger)
}

// tickRules 认领一轮到期规则并入队。
func tickRules(ctx context.Context, repo task.Repository, logger *slog.Logger) error {
	now := time.Now().UTC()
	rules, err := repo.ClaimDueRules(ctx, now, 100)
	if err != nil {
		return err
	}
	for _, rule := range rules {
		triggeredAt := now
		if rule.LastRunAt != nil {
			triggeredAt = *rule.LastRunAt
		}
		j, err := task.NewJob(idgen.New(), rule.Type, rule.Payload, now, 0, 5, rule.ID+":"+triggeredAt.Format(time.RFC3339))
		if err != nil {
			logger.Error("task scheduler: build job failed", "rule", rule.ID, "error", err)
			continue
		}
		if _, created, err := repo.Insert(ctx, j); err != nil {
			logger.Error("task scheduler: enqueue failed", "rule", rule.ID, "error", err)
			continue
		} else if !created {
			logger.Warn("task scheduler: duplicate trigger skipped", "rule", rule.ID, "unique_key", j.UniqueKey)
		}
	}
	return nil
}
