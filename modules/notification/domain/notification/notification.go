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
	"context"
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
	IdentityUserPasswordChanged = "account.user.password-changed"
	OrganizationMemberAdded     = "organization.member.added"
	OrganizationMemberRemoved   = "organization.member.removed"
)

// TypeCustom 是显式发送（Sender）在未指定 Type 时的默认通知类型。
const TypeCustom = "notification.custom"

// Sender 是模块对外导出的显式发送能力（ADR-0004 通道③：供给方 domain
// 能力接口）：应用代码（后台任务、启动期引导、业务流程）不经事件目录
// 直接给用户发系统通知。方法不染驱动类型；ctx 携带事务时落库与业务
// 同事务（仓储经 buntx.Conn 取连接）。
//
// 实现归模块 internal，构造经模块桥接包转发（Go internal 可见性下
// modules/<notification>/module.go 是应用唯一入口；转发写法见模块
// README「显式发送」一节）。
type Sender interface {
	Send(ctx context.Context, input SendInput) error
}

// SendInput 是一次显式发送的输入（Type 空则 TypeCustom）。
type SendInput struct {
	UserID  string
	Type    string
	Title   string
	Body    string
	Payload map[string]string
}

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

// ---- 渠道（v0.7.0 渠道化）----

// 渠道名常量（偏好键的第二维；自定义渠道经应用扩展）。
const (
	ChannelInApp = "inapp"
	ChannelEmail = "email"
)

// Delivery 是一次渠道投递的输入（title/body 已按模板渲染——渠道拿成品，
// 不再二次格式化；邮件渠道以 title 为主题、title+body 为纯文本正文）。
type Delivery struct {
	UserID  string
	Type    string
	Title   string
	Body    string
	Payload map[string]string
}

// Channel 是投递渠道端口（infra 实现，Assemble 注入）：站内（落库）与
// 邮件（SMTP）。Deliver 失败由调用方记死信日志——渠道间失败不互相
// 阻断（尽力而为，单进程内无重试队列；高可靠投递经 task 模块组合，
// 见 README）。
type Channel interface {
	Name() string
	Deliver(ctx context.Context, d Delivery) error
}

// ---- 偏好 ----

// Preference 是一条投递偏好（复合键 UserID+Type+Channel）。未配置 =
// 启用（乐观默认）；关闭即该类型在该渠道不再投递（退订位的最小实现，
// 事务性通知的渠道选择权归收件人）。
type Preference struct {
	UserID  string
	Type    string
	Channel string
	Enabled bool
}
