// Package demo 是样例模块的用例层：编排领域与端口（事务/事件/仓储）。
package demo

import (
	"context"
	"encoding/json"
	"time"

	"github.com/lynxland/appkit/crud"
	"github.com/lynxland/appkit/events"
	"github.com/lynxland/appkit/idgen"
	"github.com/lynxland/appkit/uow"
	"github.com/lynxland/lynx-start/modules/demo/domain/demo"
)

// UseCase 是样例模块的用例集。
type UseCase struct {
	repo   demo.Repository
	tx     uow.Runner
	events events.Publisher
	now    func() time.Time
}

// New 构造用例（Assemble 调用）。
func New(repo demo.Repository, tx uow.Runner, publisher events.Publisher) *UseCase {
	return &UseCase{repo: repo, tx: tx, events: publisher, now: time.Now}
}

// Create 建项并在同一工作单元内发布事件。
func (u *UseCase) Create(ctx context.Context, title string) (*demo.Item, error) {
	var out *demo.Item
	err := u.tx.Run(ctx, func(ctx context.Context) error {
		item, err := demo.NewItem(idgen.New(), title, u.now().UTC())
		if err != nil {
			return err
		}
		if err := u.repo.Create(ctx, item); err != nil {
			return err
		}
		out = item
		return u.publish(ctx, demo.EventItemCreated, map[string]string{"id": item.ID, "title": item.Title})
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// Get 取单项。
func (u *UseCase) Get(ctx context.Context, id string) (*demo.Item, error) {
	return u.repo.Get(ctx, id)
}

// Rename 改名。
func (u *UseCase) Rename(ctx context.Context, id, title string) (*demo.Item, error) {
	var out *demo.Item
	err := u.tx.Run(ctx, func(ctx context.Context) error {
		item, err := u.repo.Get(ctx, id)
		if err != nil {
			return err
		}
		if err := item.Rename(title, u.now().UTC()); err != nil {
			return err
		}
		if err := u.repo.Update(ctx, item); err != nil {
			return err
		}
		out = item
		return u.publish(ctx, demo.EventItemRenamed, map[string]string{"id": item.ID, "title": item.Title})
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// Delete 删除（幂等；不存在不报错）。
func (u *UseCase) Delete(ctx context.Context, id string) error {
	return u.tx.Run(ctx, func(ctx context.Context) error {
		if err := u.repo.Delete(ctx, id); err != nil {
			return err
		}
		return u.publish(ctx, demo.EventItemDeleted, map[string]string{"id": id})
	})
}

// ListResult 是列表用例的产物。
type ListResult struct {
	Items     []*demo.Item
	NextToken string
	TotalSize int32
}

// listSpec 是列表的分页规约（crud v2：keyset 游标，created_at DESC + id
// 决胜；总数另经 COUNT 供粗参考）。
var listSpec = crud.Spec{
	Modes:        crud.ModeSetKeyset,
	Sortable:     map[string]string{"createdAt": "d.created_at", "id": "d.id"},
	Tiebreak:     "id",
	DefaultLimit: 20,
	MaxLimit:     100,
}

// List 分页列表（keyset 游标）。
func (u *UseCase) List(ctx context.Context, pageSize int32, pageToken string) (*ListResult, error) {
	q, err := crud.Parse[any](listSpec, crud.RawParams{After: pageToken, Limit: pageSize, OrderBy: "createdAt desc"}, nil)
	if err != nil {
		return nil, err
	}
	items, total, err := u.repo.List(ctx, q.PagePlan)
	if err != nil {
		return nil, err
	}
	next := ""
	if crud.HasMore(int32(len(items)), q.Limit) && len(items) > 0 {
		last := items[len(items)-1]
		if next, err = crud.Cursor(q, []string{last.CreatedAt.UTC().Format(time.RFC3339Nano), last.ID}); err != nil {
			return nil, err
		}
	}
	return &ListResult{Items: items, NextToken: next, TotalSize: int32(total)}, nil
}

// publish 构造事件信封并发布（载荷 JSON 编码——手搓字符串会在 title 含
// 引号/反斜杠时产生非法 JSON；与 identity/session 的 publish 同款纪律）。
func (u *UseCase) publish(ctx context.Context, name string, payload map[string]string) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	return u.events.Publish(ctx, events.Envelope{
		ID:         idgen.New(),
		Name:       name,
		Module:     "demo",
		OccurredAt: u.now().UTC(),
		Payload:    raw,
	})
}
