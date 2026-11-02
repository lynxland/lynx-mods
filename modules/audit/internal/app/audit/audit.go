// Package audit 是审计模块的用例层：显式记录（API）与查询编排。
package audit

import (
	"context"
	"encoding/json"
	"fmt"

	domainaudit "github.com/lynx-go/lynx-mods/modules/audit/domain/audit"
	"github.com/lynxland/appkit/clock"
	"github.com/lynxland/appkit/crud"
	"github.com/lynxland/appkit/events"
	"github.com/lynxland/appkit/idgen"
	"github.com/lynxland/appkit/uow"
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

// ListRecords 查询审计（occurred_at 降序）；过滤经 filter 摘要绑定进分页
// token（跨页一致性），收尾由 crud.FinalizeOffsetPage 完成。
func (u *UseCase) ListRecords(ctx context.Context, f domainaudit.Filter, pageSize int32, pageToken string) ([]*domainaudit.Record, crud.OffsetPage, error) {
	params, err := crud.ParseListParams(pageSize, pageToken, filterKey(f), "occurred_at desc")
	if err != nil {
		return nil, crud.OffsetPage{}, err
	}
	recs, total, err := u.repo.List(ctx, f, params)
	if err != nil {
		return nil, crud.OffsetPage{}, err
	}
	page, err := crud.FinalizeOffsetPage(params, total, len(recs))
	if err != nil {
		return nil, crud.OffsetPage{}, err
	}
	return recs, page, nil
}

// filterKey 把结构化过滤拼成确定性串（crud token 的 filter 绑定通道；
// 换页时过滤不同会直接拒绝——分页语义安全）。
func filterKey(f domainaudit.Filter) string {
	return fmt.Sprintf("actor=%s;action=%s;resource=%s/%s", f.ActorID, f.Action, f.ResourceType, f.ResourceID)
}
