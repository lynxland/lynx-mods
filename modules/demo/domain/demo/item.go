// Package demo 是样例模块的领域层：聚合、不变量与仓储端口。
// 契约：不 import infra/app/api；跨模块只允许 import 他模块 domain 包。
package demo

import (
	"time"

	"github.com/lynxland/appkit/errcode"
)

// space 是本模块的错误码区段（契约⑤）。
var space = errcode.MustRegister("demo")

// 哨兵错误：app 层转 grpc/status 时携带 ErrorInfo（Domain=demo）。
var (
	ErrItemNotFound = space.New("ITEM_NOT_FOUND", "demo item not found")
	ErrEmptyTitle   = space.New("EMPTY_TITLE", "title must not be empty")
)

// 事件目录（契约②）：app 层发布与 Module().PublishedEvents 共用同一常量
// （杜绝声明与实发漂移），item.json 的 publishedEvents 与此同步。
const (
	EventItemCreated = "demo.item.created"
	EventItemRenamed = "demo.item.renamed"
	EventItemDeleted = "demo.item.deleted"
)

// Item 是样例聚合（单实体；不变量：title 非空）。
type Item struct {
	ID        string
	Title     string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// NewItem 构造新聚合。
func NewItem(id, title string, now time.Time) (*Item, error) {
	if title == "" {
		return nil, ErrEmptyTitle
	}
	return &Item{ID: id, Title: title, CreatedAt: now, UpdatedAt: now}, nil
}

// Rename 改名（更新时间戳由领域维护）。
func (i *Item) Rename(title string, now time.Time) error {
	if title == "" {
		return ErrEmptyTitle
	}
	i.Title = title
	i.UpdatedAt = now
	return nil
}

// ListQuery 是列表查询参数（分页方言见 appkit/crud）。
type ListQuery struct {
	PageSize int32
	Offset   int
}
