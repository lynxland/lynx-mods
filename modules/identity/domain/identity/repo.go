package identity

import "context"

// Repository 是 User 聚合的持久化端口（infra 实现，经 Assemble 注入）。
// 方法一律接收 ctx：适配器从 ctx 取事务连接（appkit/uow/buntx），
// 端口签名不得出现驱动类型。
type Repository interface {
	// Create 插入；email 唯一冲突映射 ErrEmailTaken。
	Create(ctx context.Context, user *User) error
	GetByID(ctx context.Context, id string) (*User, error)
	// GetByEmail 接收已规范化的邮箱；缺失映射 ErrUserNotFound。
	GetByEmail(ctx context.Context, email string) (*User, error)
	// UpdatePassword 只写 password_hash/updated_at 白名单列（bun 显式列护栏）。
	UpdatePassword(ctx context.Context, user *User) error
}
