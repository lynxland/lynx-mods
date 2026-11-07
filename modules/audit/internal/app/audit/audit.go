// Package audit 是审计模块的用例层：显式记录（API）与查询编排。
package audit

import (
	"context"
	"encoding/json"
	"time"

	"github.com/lynxland/appkit/clock"
	"github.com/lynxland/appkit/crud"
	"github.com/lynxland/appkit/events"
	"github.com/lynxland/appkit/idgen"
	"github.com/lynxland/appkit/uow"
	domainaudit "github.com/lynxland/lynx-start/modules/audit/domain/audit"
)

// UseCase 是审计模块的用例集。
type UseCase struct {
	repo   domainaudit.Repository
	tx     uow.Runner
	events events.Publisher
	now    clock.Clock
}

// New 构造用例（Assemble 调用）。c 为空取系统时钟。
func New(repo domainaudit.Repository, tx uow.Runner, publisher events.Publisher, c clock.Clock) *UseCase {
	if c == nil {
		c = clock.System{}
	}
	return &UseCase{repo: repo, tx: tx, events: publisher, now: c}
}

// Record 显式记录一条审计（API 路径）：与调用方业务同事务落库（tx.Run
// 加入外层语境），并发布 audit.log.recorded 事件（信封载荷为记录 JSON）。
// actorID 取自认证主体（API 层注入）；action 必填。
func (u *UseCase) Record(ctx context.Context, actorID, actorEmail, action, resourceType, resourceID string, payload json.RawMessage) (*domainaudit.Record, error) {
	rec, err := domainaudit.NewRecord(idgen.New(), actorID, actorEmail, action, resourceType, resourceID, payload, domainaudit.SourceAPI, u.now.Now())
	if err != nil {
		return nil, err
	}
	err = u.tx.Run(ctx, func(ctx context.Context) error {
		if err := u.repo.Append(ctx, rec); err != nil {
			return err
		}
		payload, err := json.Marshal(rec)
		if err != nil {
			return err
		}
		return u.events.Publish(ctx, events.Envelope{
			ID:         idgen.New(),
			Name:       domainaudit.EventLogRecorded,
			Module:     "audit",
			OccurredAt: u.now.Now(),
			Payload:    payload,
		})
	})
	if err != nil {
		return nil, err
	}
	return rec, nil
}

// listSpec 是审计列表的分页规约（crud v2：keyset 游标，occurred_at DESC +
// id 决胜；过滤经 Filter.FilterDigest 绑定游标——翻页中途换滤自失效）。
var listSpec = crud.Spec{
	Modes:        crud.ModeSetKeyset,
	Sortable:     map[string]string{"occurredAt": "r.occurred_at", "id": "r.id"},
	Tiebreak:     "id",
	DefaultLimit: 50,
	MaxLimit:     200,
}

// ListRecordsResult 是审计列表用例的产物。
type ListRecordsResult struct {
	Records   []*domainaudit.Record
	NextToken string
	TotalSize int32
}

// ListRecords 查询审计（occurred_at 降序，keyset 分页）。
func (u *UseCase) ListRecords(ctx context.Context, f domainaudit.Filter, pageSize int32, pageToken string) (*ListRecordsResult, error) {
	q, err := crud.Parse[domainaudit.Filter](listSpec, crud.RawParams{After: pageToken, Limit: pageSize, OrderBy: "occurredAt desc"}, f)
	if err != nil {
		return nil, err
	}
	recs, total, err := u.repo.List(ctx, f, q.PagePlan)
	if err != nil {
		return nil, err
	}
	next := ""
	if crud.HasMore(int32(len(recs)), q.Limit) && len(recs) > 0 {
		last := recs[len(recs)-1]
		if next, err = crud.Cursor(q, []string{last.OccurredAt.UTC().Format(time.RFC3339Nano), last.ID}); err != nil {
			return nil, err
		}
	}
	return &ListRecordsResult{Records: recs, NextToken: next, TotalSize: int32(total)}, nil
}

// Config 是 audit 模块的配置段（v0.7.0 起有配置——此前零配置开箱即用，
// 默认值与 v0.6.0 硬编码一致，零配置语义不变）。
type Config struct {
	// RetentionDays 是保留期（每 6h 清理 retention_days 天前记录；0=禁用）。
	RetentionDays int `mapstructure:"retention_days"`
	// WriterBuffer/WriterFlushEvery/WriterBatchSize 是异步写入器参数
	// （零值取 infra 默认）。
	WriterBuffer     int           `mapstructure:"writer_buffer"`
	WriterFlushEvery time.Duration `mapstructure:"writer_flush_every"`
	WriterBatchSize  int           `mapstructure:"writer_batch_size"`
}

// ApplyDefaults 落默认值（LoadSection 的 defaults 钩子）。
func (c *Config) ApplyDefaults() {
	c.RetentionDays = 90
}
