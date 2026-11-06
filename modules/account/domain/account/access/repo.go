package access

import "context"

// Repository 是角色分配的持久化端口（infra 实现，经 Assemble 注入）。
// 方法一律接收 ctx：适配器从 ctx 取事务连接（appkit/uow/buntx）。
type Repository interface {
	// Assign 幂等授予：已存在则 no-op，返回是否发生写入。
	Assign(ctx context.Context, a *Assignment) (created bool, err error)
	// Revoke 幂等回收：不存在则 no-op，返回是否发生删除。
	Revoke(ctx context.Context, userID, role string) (removed bool, err error)
	// RolesForUser 返回用户全部角色名（无序去重）。
	RolesForUser(ctx context.Context, userID string) ([]string, error)
}
