package organization

import (
	"fmt"
	"strings"
	"time"
)

// 邀请状态机与角色常量（v0.7.0 B2B 基本盘）。
const (
	MemberRoleOwner  = "owner"
	MemberRoleAdmin  = "admin"
	MemberRoleMember = "member"

	InviteStatusPending  = "pending"
	InviteStatusAccepted = "accepted"
	InviteStatusRevoked  = "revoked"
	InviteStatusExpired  = "expired"
)

// 邀请域哨兵错误。
var (
	ErrInviteNotFound      = space.New("INVITE_NOT_FOUND", "invitation not found")
	ErrInviteNotPending    = space.New("INVITE_NOT_PENDING", "invitation is not pending")
	ErrInviteExpired       = space.New("INVITE_EXPIRED", "invitation has expired")
	ErrInviteEmailMismatch = space.New("INVITE_EMAIL_MISMATCH", "invitation email does not match the accepting account")
	ErrInvalidRole         = space.New("INVALID_ROLE", "role must be admin or member")
	ErrNotAdmin            = space.New("NOT_ADMIN", "actor is not an owner or admin of this organization")
	ErrAlreadyMember       = space.New("ALREADY_MEMBER", "user is already a member")
)

// normalizeEmail 与 account identity 域同语义（lower+trim）——domain 间
// 不共享实现（契约②纪律），语义一致由测试钉住。
func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

// ValidMemberRoles 是 UpdateMemberRole/邀请可授予的角色（owner 只经
// TransferOwnership 变更——结构标记不可直改）。
func IsValidGrantRole(role string) bool {
	return role == MemberRoleAdmin || role == MemberRoleMember
}

// Invitation 是一条组织邀请：pending → accepted / revoked / expired。
type Invitation struct {
	ID             string
	OrganizationID string
	Email          string
	Role           string
	Token          string
	Status         string
	CreatedBy      string
	ExpiresAt      time.Time
	AcceptedAt     *time.Time
	AcceptedBy     string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// NewInvitation 构造 pending 邀请（7 天过期；token 由 app 注入——
// 不可猜测性来自 idgen）。
func NewInvitation(id, organizationID, email, role, token, createdBy string, now time.Time) (*Invitation, error) {
	if id == "" || organizationID == "" || email == "" || token == "" {
		return nil, fmt.Errorf("organization: empty invitation field")
	}
	if !IsValidGrantRole(role) {
		return nil, ErrInvalidRole
	}
	return &Invitation{
		ID: id, OrganizationID: organizationID, Email: normalizeEmail(email),
		Role: role, Token: token, Status: InviteStatusPending, CreatedBy: createdBy,
		ExpiresAt: now.Add(7 * 24 * time.Hour), CreatedAt: now, UpdatedAt: now,
	}, nil
}

// AcceptIfEligible 校验并流转（pending + 未过期 + 邮箱匹配）→ accepted。
func (i *Invitation) AcceptIfEligible(accountEmail string, userID string, now time.Time) error {
	if i.Status != InviteStatusPending {
		return ErrInviteNotPending
	}
	if now.After(i.ExpiresAt) {
		return ErrInviteExpired
	}
	if i.Email != normalizeEmail(accountEmail) {
		return ErrInviteEmailMismatch
	}
	i.Status = InviteStatusAccepted
	i.AcceptedAt = &now
	i.AcceptedBy = userID
	i.UpdatedAt = now
	return nil
}

// RevokeIfPending 撤销（幂等：非 pending 报错交调用方裁决）。
func (i *Invitation) RevokeIfPending(now time.Time) error {
	if i.Status != InviteStatusPending {
		return ErrInviteNotPending
	}
	i.Status = InviteStatusRevoked
	i.UpdatedAt = now
	return nil
}
