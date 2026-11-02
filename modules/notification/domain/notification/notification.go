// Package notification 是通知模块的领域层：Notification 聚合（渲染
// 快照）、不变量与仓储端口。
//
// 定位：通知 = 事件的事实回执。内容在发生时刻定格（title/body 快照，
// 不引用模板——发布后源文案变更不影响已发生的通知）；payload 保留
// 原始事件载荷供前端跳转。通知只可见于收件人本人（user_id 从事件
// 载荷来，只存 ID，契约④）。
//
// 契约：不 import infra/app/api；消费 identity/organization 的事件走
// 字符串目录（不 import 发布方 Go 代码）。本模块声明 DependsOn:
// identity+organization——ConsumedEvents 启动期交叉校验发布者在装。
package notification

import (
	"fmt"
	"time"

	"github.com/lynxland/appkit/errcode"
)

// space 是本模块的错误码区段（契约⑤）。
var space = errcode.MustRegister("notification")

// 哨兵错误：api 层转 grpc/status 时携带 ErrorInfo（Domain=notification）。
var (
	ErrNotificationNotFound = space.New("NOTIFICATION_NOT_FOUND", "notification not found")
)

// 消费的事件目录（契约②）：字符串常量在本模块声明（杜绝 watcher 订阅
// 与 Module().ConsumedEvents 声明漂移），不 import 发布方代码。
// organization>=0.1.1 起 member.* 载荷含 organization_name。
const (
	IdentityUserPasswordChanged = "identity.user.password-changed"
	OrganizationMemberAdded     = "organization.member.added"
	OrganizationMemberRemoved   = "organization.member.removed"
)

// Notification 是一条站内通知（不变量：userID/type/title 非空；
// ReadAt 空表示未读）。
type Notification struct {
	ID        string
	UserID    string
	Type      string
	Title     string
	Body      string
	Payload   map[string]string
	ReadAt    *time.Time
	CreatedAt time.Time
}

// NewNotification 构造新聚合（未读）。
func NewNotification(id, userID, typ, title, body string, payload map[string]string, now time.Time) (*Notification, error) {
	if id == "" || userID == "" {
		return nil, fmt.Errorf("notification: empty id/userID")
	}
	if typ == "" {
		return nil, fmt.Errorf("notification: empty type")
	}
	if title == "" {
		return nil, fmt.Errorf("notification: empty title")
	}
	return &Notification{
		ID: id, UserID: userID, Type: typ, Title: title, Body: body,
		Payload: payload, CreatedAt: now,
	}, nil
}

// MarkRead 标记已读（幂等：已读再标为 no-op）。
func (n *Notification) MarkRead(now time.Time) {
	if n.ReadAt == nil {
		t := now
		n.ReadAt = &t
	}
}

// Unread 报告未读状态。
func (n *Notification) Unread() bool { return n.ReadAt == nil }
