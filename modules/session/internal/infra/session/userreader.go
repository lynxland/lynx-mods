package session

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/lynxland/lynx-mods/modules/identity/domain/identity"
	"github.com/uptrace/bun"
)

// userReader 是 identity.Repository 的**只读**实现（DependsOn 的消费面）：
// Refresh 边界回读用户状态用。identity_users 表的写路径永远归 identity
// 模块——本适配器只 SELECT 显式列；写入方法一律报错，防止所有权漂移。
type userReader struct {
	db *bun.DB
}

// NewUserReader 构造只读适配器（Assemble 调用）。
func NewUserReader(db *bun.DB) *userReader { return &userReader{db: db} }

var _ identity.Repository = (*userReader)(nil)

type userRow struct {
	bun.BaseModel `bun:"table:identity_users,alias:u"`

	ID     string `bun:"id"`
	Email  string `bun:"email"`
	Status string `bun:"status"`
}

func (r *userReader) GetByID(ctx context.Context, id string) (*identity.User, error) {
	row := new(userRow)
	err := r.db.NewSelect().Model(row).
		Column("u.id", "u.email", "u.status").
		Where("u.id = ?", id).Scan(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, identity.ErrUserNotFound
	}
	if err != nil {
		return nil, err
	}
	return &identity.User{ID: row.ID, Email: row.Email, Status: row.Status}, nil
}

func (r *userReader) GetByEmail(ctx context.Context, email string) (*identity.User, error) {
	return nil, fmt.Errorf("session: userReader is read-only (GetByEmail belongs to identity)")
}

func (r *userReader) Create(context.Context, *identity.User) error {
	return fmt.Errorf("session: userReader is read-only (Create belongs to identity)")
}

func (r *userReader) UpdatePassword(context.Context, *identity.User) error {
	return fmt.Errorf("session: userReader is read-only (UpdatePassword belongs to identity)")
}
