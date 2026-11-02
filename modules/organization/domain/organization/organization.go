// Package org 是组织模块的领域层：Organization 聚合（名称/slug）、成员
// 事实、不变量与仓储端口。
//
// 定位：组织 = 结构与成员事实。IsOwner 是组织结构标记（创建者即 owner、
// owner-only 管理、最后一个 owner 不可离开），不是 RBAC——角色/权限语义
// 归 access 模块（后续可经其角色目录表达组织级角色），本域不与其重叠。
//
// 契约：不 import infra/app/api；跨模块只允许 import 他模块 domain 包。
// 本模块声明 DependsOn: identity——成员是 identity 用户（UserID 只存 ID，
// 契约④；不设跨模块外键），添加成员时经 identity 的 domain 端口校验
// 用户存在。
package organization

import (
	"fmt"
	"regexp"
	"time"

	"github.com/lynxland/appkit/errcode"
)

// space 是本模块的错误码区段（契约⑤）。
var space = errcode.MustRegister("org")

// 哨兵错误：api 层转 grpc/status 时携带 ErrorInfo（Domain=organization）。
var (
	ErrOrganizationNotFound = space.New("ORGANIZATION_NOT_FOUND", "organization not found")
	ErrSlugTaken            = space.New("SLUG_TAKEN", "organization slug is already in use")
	ErrInvalidSlug          = space.New("INVALID_SLUG", "organization slug format is invalid")
	ErrEmptyName            = space.New("EMPTY_NAME", "organization name must not be empty")
	ErrUnknownUser          = space.New("UNKNOWN_USER", "user does not exist")
	ErrNotMember            = space.New("NOT_A_MEMBER", "user is not a member of the organization")
	ErrNotOwner             = space.New("NOT_AN_OWNER", "only organization owners may perform this action")
	ErrCannotRemoveOwner    = space.New("CANNOT_REMOVE_OWNER", "organization owners cannot be removed by others; owners leave on their own")
	ErrLastOwner            = space.New("LAST_OWNER", "the last owner cannot leave the organization")
)

// 事件目录（契约②）：app 层发布与 Module().PublishedEvents 共用同一常量
// （杜绝声明与实发漂移），item.json 的 publishedEvents 与此同步。
const (
	EventOrganizationCreated = "organization.organization.created"
	EventMemberAdded         = "organization.member.added"
	EventMemberRemoved       = "organization.member.removed"
)

// slugRe 约束 slug 形态：3-64 位小写字母/数字/中划线，字母开头、
// 结尾非中划线（URL 友好的稳定标识；全局唯一）。
var slugRe = regexp.MustCompile(`^[a-z][a-z0-9-]{1,62}[a-z0-9]$`)

// ValidSlug 校验 slug 形态。
func ValidSlug(slug string) bool { return slugRe.MatchString(slug) }

// Organization 是组织聚合（不变量：name 非空、slug 形态合法）。
type Organization struct {
	ID        string
	Name      string
	Slug      string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// NewOrganization 构造新聚合。
func NewOrganization(id, name, slug string, now time.Time) (*Organization, error) {
	if name == "" {
		return nil, ErrEmptyName
	}
	if !ValidSlug(slug) {
		return nil, ErrInvalidSlug
	}
	if id == "" {
		return nil, fmt.Errorf("organization: empty id")
	}
	return &Organization{ID: id, Name: name, Slug: slug, CreatedAt: now, UpdatedAt: now}, nil
}

// Rename 改名（slug 创建后不可变——外部引用稳定性；更新时间戳由领域维护）。
func (o *Organization) Rename(name string, now time.Time) error {
	if name == "" {
		return ErrEmptyName
	}
	o.Name = name
	o.UpdatedAt = now
	return nil
}

// Member 是一条成员事实（UserID 只存 ID，契约④）。IsOwner 是结构标记
// 而非角色：管理操作授权依据（owner-only），权限语义归 access。
type Member struct {
	OrganizationID string
	UserID         string
	IsOwner        bool
	JoinedAt       time.Time
}

// NewMember 构造新成员事实。
func NewMember(organizationID, userID string, isOwner bool, now time.Time) (*Member, error) {
	if organizationID == "" || userID == "" {
		return nil, fmt.Errorf("organization: empty organizationID/userID")
	}
	return &Member{OrganizationID: organizationID, UserID: userID, IsOwner: isOwner, JoinedAt: now}, nil
}

// MemberQuery 是成员列表查询参数（分页方言见 appkit/crud）。
type MemberQuery struct {
	PageSize int32
	Offset   int
}

// OrganizationQuery 是组织列表查询参数（ListMyOrganizations 用）。
type OrganizationQuery struct {
	PageSize int32
	Offset   int
}
