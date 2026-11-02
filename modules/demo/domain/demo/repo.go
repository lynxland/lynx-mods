package demo

import "context"

// Repository 是 Item 聚合的持久化端口（infra 实现，经 Assemble 注入）。
// 方法一律接收 ctx：适配器从 ctx 取事务连接（appkit/uow/buntx），
// 端口签名不得出现驱动类型。
type Repository interface {
	Create(ctx context.Context, item *Item) error
	Get(ctx context.Context, id string) (*Item, error)
	// Update 只写 title/updated_at 白名单列（bun 显式列护栏）。
	Update(ctx context.Context, item *Item) error
	Delete(ctx context.Context, id string) error
	// List 按 created_at DESC, id DESC 返回一页与总数。
	List(ctx context.Context, q ListQuery) (items []*Item, total int, err error)
}
