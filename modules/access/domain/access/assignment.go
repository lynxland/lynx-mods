// Package access 是授权模块的领域层：角色分配聚合、命名约束与仓储端口。
//
// 契约：不 import infra/app/api；跨模块只允许 import 他模块 domain 包。
// 本模块声明 DependsOn: identity——角色分配给 identity 的用户（只存
// user ID，契约④），bootstrap 管理员解析消费 identity 的 domain 端口。
//
// 角色的定义（名字 → 权限集）在配置段声明（应用所有者可审计可改），
// 不落库；本聚合只承载「谁拥有什么角色」这一动态事实。
package access

import (
	"regexp"
	"time"

	"github.com/lynxland/appkit/errcode"
)

// space 是本模块的错误码区段（契约⑤）。
var space = errcode.MustRegister("access")

// 哨兵错误：api 层转 grpc/status 时附带 ErrorInfo（Domain=access）。
var (
	// ErrUnknownRole 授予/回收配置目录之外的角色。
	ErrUnknownRole = space.New("UNKNOWN_ROLE", "role is not declared in config")
	// ErrUnknownUser 分配目标用户不存在（identity 侧校验）。
	ErrUnknownUser = space.New("UNKNOWN_USER", "user does not exist")
)

// 事件目录（契约②）：app 层发布与 Module().PublishedEvents 共用同一常量
// （杜绝声明与实发漂移），item.json 的 publishedEvents 与此同步。
const (
	EventAssignmentGranted = "access.assignment.granted"
	EventAssignmentRevoked = "access.assignment.revoked"
)

// nameRe 角色名与权限名约束：小写字母开头，可含数字/点/中划线/下划线
// （如 "admin"、"access.assignment.write"）。
var nameRe = regexp.MustCompile(`^[a-z][a-z0-9._-]*$`)

// ValidateName 校验角色名/权限名形态。
func ValidateName(name string) bool { return nameRe.MatchString(name) }

// Assignment 是一条用户角色分配（聚合根）。幂等语义：重复授予与
// 回收不存在的分配都是合法的 no-op（运维友好，事件只在状态变化时发布）。
type Assignment struct {
	UserID    string
	Role      string
	GrantedAt time.Time
}

// NewAssignment 构造新分配。
func NewAssignment(userID, role string, now time.Time) (*Assignment, error) {
	if userID == "" || !ValidateName(role) {
		return nil, ErrUnknownRole
	}
	return &Assignment{UserID: userID, Role: role, GrantedAt: now}, nil
}
