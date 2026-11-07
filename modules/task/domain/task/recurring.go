package task

import (
	"time"

	"github.com/robfig/cron/v3"
)

// RecurringRule 是一条 cron 周期规则：调度器按 next_run_at 到期入队，
// 每次触发的 job 带 unique_key = "<rule_id>:<触发时间戳>"（同一触发窗
// 重放安全——调度器崩溃恢复后不会为同一时刻重复入队）。
type RecurringRule struct {
	ID        string
	Type      string
	Payload   string
	CronExpr  string
	Enabled   bool
	LastRunAt *time.Time
	NextRunAt time.Time
	CreatedAt time.Time
	UpdatedAt time.Time
}

// cronParser 接受 5 段标准表达式与 @every 描述符（与 lynx/contrib/schedule
// 同引擎 robfig/cron v3；秒级精度由 @every 表达，标准表达式按分钟粒度）。
var cronParser = cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor)

// ParseCron 校验并解析 cron 表达式（5 段或 @every）。
func ParseCron(expr string) (cron.Schedule, error) {
	s, err := cronParser.Parse(expr)
	if err != nil {
		return nil, ErrInvalidCron
	}
	return s, nil
}

// NewRecurringRule 构造规则（算出首个 next_run_at）。
func NewRecurringRule(id, typ, payload, cronExpr string, enabled bool, now time.Time) (*RecurringRule, error) {
	s, err := ParseCron(cronExpr)
	if err != nil {
		return nil, err
	}
	return &RecurringRule{
		ID: id, Type: typ, Payload: payload, CronExpr: cronExpr,
		Enabled: enabled, NextRunAt: s.Next(now), CreatedAt: now, UpdatedAt: now,
	}, nil
}

// NextAfter 返回 t 之后的首个触发时间。
func (r *RecurringRule) NextAfter(t time.Time) (time.Time, error) {
	s, err := ParseCron(r.CronExpr)
	if err != nil {
		return time.Time{}, err
	}
	return s.Next(t), nil
}
