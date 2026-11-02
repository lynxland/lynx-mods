// Package audit 是审计模块的基础设施层：bun 仓储（append-only）、迁移与
// 拦截器路径的异步写入器。
package audit

import (
	"context"
	"encoding/json"
	"time"

	domainaudit "github.com/lynx-go/lynx-mods/modules/audit/domain/audit"
	"github.com/lynxland/appkit/contrib/bunx/buntx"
	"github.com/lynxland/appkit/crud"
	"github.com/uptrace/bun"
)

// recordModel 是 PO（持久化对象）：bun 标签在此，领域聚合不染 ORM。
type recordModel struct {
	bun.BaseModel `bun:"table:audit_records,alias:r"`

	ID           string          `bun:"id,pk"`
	ActorID      string          `bun:"actor_id,notnull"`
	ActorEmail   string          `bun:"actor_email,notnull"`
	Action       string          `bun:"action,notnull"`
	ResourceType string          `bun:"resource_type,notnull"`
	ResourceID   string          `bun:"resource_id,notnull"`
	Payload      json.RawMessage `bun:"payload,type:jsonb"`
	Source       string          `bun:"source,notnull"`
	OccurredAt   time.Time       `bun:"occurred_at,notnull"`
}

func toModel(rec *domainaudit.Record) *recordModel {
	return &recordModel{
		ID:           rec.ID,
		ActorID:      rec.ActorID,
		ActorEmail:   rec.ActorEmail,
		Action:       rec.Action,
		ResourceType: rec.ResourceType,
		ResourceID:   rec.ResourceID,
		Payload:      rec.Payload,
		Source:       rec.Source,
		OccurredAt:   rec.OccurredAt,
	}
}

func fromModel(m *recordModel) *domainaudit.Record {
	return &domainaudit.Record{
		ID:           m.ID,
		ActorID:      m.ActorID,
		ActorEmail:   m.ActorEmail,
		Action:       m.Action,
		ResourceType: m.ResourceType,
		ResourceID:   m.ResourceID,
		Payload:      m.Payload,
		Source:       m.Source,
		OccurredAt:   m.OccurredAt,
	}
}

// Repo 实现 domainaudit.Repository。
type Repo struct {
	db *bun.DB
}

// NewRepository 构造仓储（Assemble 调用）。
func NewRepository(db *bun.DB) *Repo { return &Repo{db: db} }

var _ domainaudit.Repository = (*Repo)(nil)

// Append 插入单条（加入调用方事务语境）。
func (r *Repo) Append(ctx context.Context, rec *domainaudit.Record) error {
	conn := buntx.Conn(ctx, r.db)
	_, err := conn.NewInsert().Model(toModel(rec)).Exec(ctx)
	return err
}

// AppendBatch 批量插入（一次往返；空批直接成功）。
func (r *Repo) AppendBatch(ctx context.Context, recs []*domainaudit.Record) error {
	if len(recs) == 0 {
		return nil
	}
	conn := buntx.Conn(ctx, r.db)
	models := make([]recordModel, 0, len(recs))
	for _, rec := range recs {
		models = append(models, *toModel(rec))
	}
	_, err := conn.NewInsert().Model(&models).Exec(ctx)
	return err
}

// List 按 occurred_at 降序 + 结构化过滤 + offset 分页，返回 (记录, 总数)。
func (r *Repo) List(ctx context.Context, f domainaudit.Filter, params crud.ListParams) ([]*domainaudit.Record, int, error) {
	conn := buntx.Conn(ctx, r.db)
	apply := func(q *bun.SelectQuery) *bun.SelectQuery {
		if f.ActorID != "" {
			q = q.Where("r.actor_id = ?", f.ActorID)
		}
		if f.Action != "" {
			q = q.Where("r.action = ?", f.Action)
		}
		if f.ResourceType != "" {
			q = q.Where("r.resource_type = ?", f.ResourceType)
		}
		if f.ResourceID != "" {
			q = q.Where("r.resource_id = ?", f.ResourceID)
		}
		return q.Order("r.occurred_at DESC", "r.id DESC")
	}
	var rows []recordModel
	err := apply(conn.NewSelect().Model(&rows)).
		Limit(int64(params.PageSize)).Offset(int64(params.Offset)).
		Scan(ctx)
	if err != nil {
		return nil, 0, err
	}
	total, err := apply(conn.NewSelect().Model((*recordModel)(nil))).Count(ctx)
	if err != nil {
		return nil, 0, err
	}
	recs := make([]*domainaudit.Record, 0, len(rows))
	for i := range rows {
		recs = append(recs, fromModel(&rows[i]))
	}
	return recs, int(total), nil
}
