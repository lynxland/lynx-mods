// Package identity 是身份模块的领域层：User 聚合、不变量与仓储端口。
// 契约：不 import infra/app/api；跨模块只允许 import 他模块 domain 包。
package identity

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/lynxland/appkit/errcode"
)

// space 是本模块的错误码区段（契约⑤）。
var space = errcode.MustRegister("identity")

// 哨兵错误：api 层转 grpc/status 时附带 ErrorInfo（Domain=identity）。
var (
	ErrInvalidEmail       = space.New("INVALID_EMAIL", "email format is invalid")
	ErrEmailTaken         = space.New("EMAIL_ALREADY_EXISTS", "email is already registered")
	ErrWeakPassword       = space.New("WEAK_PASSWORD", "password must be 8-512 characters")
	ErrInvalidCredentials = space.New("INVALID_CREDENTIALS", "email or password is incorrect")
	ErrUserNotFound       = space.New("USER_NOT_FOUND", "user not found")
	ErrUserDisabled       = space.New("USER_DISABLED", "user is disabled")
	ErrTokenInvalid       = space.New("TOKEN_INVALID", "access token is invalid")
	ErrTokenExpired       = space.New("TOKEN_EXPIRED", "access token is expired")
)

// 事件目录（契约②）：app 层发布与 Module().PublishedEvents 共用同一常量
// （杜绝声明与实发漂移），item.json 的 publishedEvents 与此同步。
const (
	EventUserCreated         = "identity.user.created"
	EventUserPasswordChanged = "identity.user.password-changed"
)

// 账号状态（v0.1 只有 active 参与流转；disabled 为停用保留位）。
const (
	StatusActive   = "active"
	StatusDisabled = "disabled"
)

// 口令长度策略。长度是下限护栏；复杂度策略归应用所有者按需收紧
// （源码所有权模型：装进应用后自行强化）。
const (
	MinPasswordLength = 8
	MaxPasswordLength = 512
)

// emailRe 在规范化（小写+去空白）之后校验：local@domain.tld，禁空白。
var emailRe = regexp.MustCompile(`^[a-z0-9._%+-]+@[a-z0-9.-]+\.[a-z]{2,}$`)

// User 是身份聚合（不变量：email 规范形态、状态合法）。
// PasswordHash 是 appkit/password 的 Argon2id PHC 串；领域只见散列，
// 明文口令不越过用例层。
type User struct {
	ID           string
	Email        string
	PasswordHash string
	DisplayName  string
	Status       string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// NormalizeEmail 规范化邮箱：去首尾空白 + 小写。
func NormalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

// ValidateEmail 校验规范化后的邮箱形态。
func ValidateEmail(email string) error {
	if !emailRe.MatchString(email) {
		return ErrInvalidEmail
	}
	return nil
}

// ValidatePassword 校验口令长度策略。
func ValidatePassword(pw string) error {
	if len(pw) < MinPasswordLength || len(pw) > MaxPasswordLength {
		return ErrWeakPassword
	}
	return nil
}

// NewUser 构造新聚合（passwordHash 由用例层经 appkit/password 产生）。
// displayName 缺省取邮箱 local 部分。
func NewUser(id, email, displayName, passwordHash string, now time.Time) (*User, error) {
	email = NormalizeEmail(email)
	if err := ValidateEmail(email); err != nil {
		return nil, err
	}
	if displayName == "" {
		displayName = strings.SplitN(email, "@", 2)[0]
	}
	return &User{
		ID:           id,
		Email:        email,
		PasswordHash: passwordHash,
		DisplayName:  displayName,
		Status:       StatusActive,
		CreatedAt:    now,
		UpdatedAt:    now,
	}, nil
}

// IsActive 报告账号是否可用。
func (u *User) IsActive() bool { return u.Status == StatusActive }

// ChangePassword 换新的口令散列（时间戳由领域维护）。
// 注：v0.1 无令牌吊销——改密后已签发令牌仍有效至过期，TTL 应保持短
// （默认 30m）；session/吊销清单留给后续模块。
func (u *User) ChangePassword(hash string, now time.Time) error {
	if hash == "" {
		return fmt.Errorf("identity: empty password hash")
	}
	u.PasswordHash = hash
	u.UpdatedAt = now
	return nil
}
