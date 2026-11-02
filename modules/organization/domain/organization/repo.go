package organization

import "context"

// Repository 是 Organization 聚合与成员事实的持久化端口（infra 实现，经
// Assemble 注入）。方法一律接收 ctx：适配器从 ctx 取事务连接
// （appkit/uow/buntx），端口签名不得出现驱动类型。
type Repository interface {
	// Create 插入组织；slug 唯一冲突映射 ErrSlugTaken。
	Create(ctx context.Context, o *Organization) error
	// Get 按 ID 取；缺失映射 ErrOrganizationNotFound。
	Get(ctx context.Context, id string) (*Organization, error)
	// Update 只写 name/updated_at 白名单列（bun 显式列护栏；slug 不可变）。
	Update(ctx context.Context, o *Organization) error

	// AddMember 幂等添加成员（已存在则 no-op），返回是否发生写入。
	AddMember(ctx context.Context, m *Member) (created bool, err error)
	// RemoveMember 幂等移除成员（不存在则 no-op），返回是否发生删除。
	RemoveMember(ctx context.Context, organizationID, userID string) (removed bool, err error)
	// Member 取单个成员事实；缺失映射 ErrNotMember。
	Member(ctx context.Context, organizationID, userID string) (*Member, error)
	// ListMembers 按 joined_at DESC, user_id DESC 返回一页与总数。
	ListMembers(ctx context.Context, organizationID string, q MemberQuery) ([]*Member, int, error)
	// ListByUser 按 joined_at DESC, organization id DESC 返回用户加入的组织一页与总数。
	ListByUser(ctx context.Context, userID string, q OrganizationQuery) ([]*Organization, int, error)
	// CountOwners 统计组织 owner 数（最后一个 owner 不可离开的裁决依据）。
	CountOwners(ctx context.Context, organizationID string) (int, error)
}
