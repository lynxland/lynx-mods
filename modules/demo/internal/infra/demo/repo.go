// Package demo 是样例模块的基础设施层：bun 仓储实现与迁移。
package demo

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/lynxland/appkit/contrib/bunx/buntx"
	"github.com/lynxland/lynx-mods/modules/demo/domain/demo"
	"github.com/uptrace/bun"
)

// itemModel 是 PO（持久化对象）：bun 标签在此，领域聚合不染 ORM。
type itemModel struct {
	bun.BaseModel `bun:"table:demo_items,alias:d"`

	ID        string    `bun:"id,pk"`
	Title     string    `bun:"title,notnull"`
	CreatedAt time.Time `bun:"created_at,notnull"`
	UpdatedAt time.Time `bun:"updated_at,notnull"`
}

// Repo 实现 demo.Repository。
type Repo struct {
	db *bun.DB
}

// NewRepository 构造仓储（Assemble 调用）。
func NewRepository(db *bun.DB) *Repo { return &Repo{db: db} }

var _ demo.Repository = (*Repo)(nil)

// Create 插入（加入调用方事务）。
func (r *Repo) Create(ctx context.Context, item *demo.Item) error {
	m := toModel(item)
	_, err := buntx.Conn(ctx, r.db).NewInsert().Model(&m).Exec(ctx)
	return err
}

// Get 按 id 取；缺失映射领域哨兵错误。
func (r *Repo) Get(ctx context.Context, id string) (*demo.Item, error) {
	m := new(itemModel)
	err := buntx.Conn(ctx, r.db).NewSelect().Model(m).
		Where("d.id = ?", id).Scan(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, demo.ErrItemNotFound
	}
	if err != nil {
		return nil, err
	}
	return toDomain(m), nil
}

// Update 只写白名单列（bun 显式 Column 护栏：禁止整模型覆盖）。
func (r *Repo) Update(ctx context.Context, item *demo.Item) error {
	res, err := buntx.Conn(ctx, r.db).NewUpdate().
		Model((*itemModel)(nil)).
		Set("title = ?", item.Title).
		Set("updated_at = ?", item.UpdatedAt).
		Where("id = ?", item.ID).
		Exec(ctx)
	if err != nil {
		return err
	}
	if n, err := res.RowsAffected(); err == nil && n == 0 {
		return demo.ErrItemNotFound
	}
	return nil
}

// Delete 删除（幂等）。
func (r *Repo) Delete(ctx context.Context, id string) error {
	_, err := buntx.Conn(ctx, r.db).NewDelete().
		Model((*itemModel)(nil)).Where("id = ?", id).Exec(ctx)
	return err
}

// List 按 created_at DESC, id DESC 取一页与总数。
func (r *Repo) List(ctx context.Context, q demo.ListQuery) ([]*demo.Item, int, error) {
	var rows []itemModel
	conn := buntx.Conn(ctx, r.db)
	err := conn.NewSelect().Model(&rows).
		Order("d.created_at DESC", "d.id DESC").
		Limit(int64(q.PageSize)).Offset(int64(q.Offset)).
		Scan(ctx)
	if err != nil {
		return nil, 0, err
	}
	total, err := conn.NewSelect().Model((*itemModel)(nil)).Count(ctx)
	if err != nil {
		return nil, 0, err
	}
	items := make([]*demo.Item, 0, len(rows))
	for i := range rows {
		items = append(items, toDomain(&rows[i]))
	}
	return items, int(total), nil
}

func toModel(item *demo.Item) itemModel {
	return itemModel{
		ID: item.ID, Title: item.Title,
		CreatedAt: item.CreatedAt, UpdatedAt: item.UpdatedAt,
	}
}

func toDomain(m *itemModel) *demo.Item {
	return &demo.Item{
		ID: m.ID, Title: m.Title,
		CreatedAt: m.CreatedAt, UpdatedAt: m.UpdatedAt,
	}
}
