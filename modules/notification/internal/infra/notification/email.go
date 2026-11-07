package notification

import (
	"context"
	"fmt"
	"net/smtp"
	"time"

	"github.com/lynxland/appkit/idgen"
	"github.com/lynxland/lynx-start/modules/account/domain/account/identity"
	domainnotification "github.com/lynxland/lynx-start/modules/notification/domain/notification"
)

// SMTPConfig 是邮件渠道配置（notification.smtp 段；Host 空 = 渠道禁用，
// 降级仅站内）。
type SMTPConfig struct {
	Host     string
	Port     int
	Username string
	Password string
	From     string
}

// EmailChannel 经 SMTP 投递邮件（标准库 net/smtp + STARTTLS，零第三方
// 依赖；事务性邮件的最小实现——无附件/批量/DKIM，营销类投递明确
// Non-goal，见 README）。收件地址经 account 的 identity 域端口解析
// （ADR-0004 通道②：消费方自持只读适配器）。
type EmailChannel struct {
	cfg   SMTPConfig
	users identity.Repository // 只读：userID → email
}

// NewEmailChannel 构造邮件渠道（cfg.Host 空返回 nil——调用方按禁用处理）。
func NewEmailChannel(cfg SMTPConfig, users identity.Repository) *EmailChannel {
	if cfg.Host == "" {
		return nil
	}
	if cfg.Port == 0 {
		cfg.Port = 587
	}
	return &EmailChannel{cfg: cfg, users: users}
}

var _ domainnotification.Channel = (*EmailChannel)(nil)

// Name 实现 domain.Channel。
func (c *EmailChannel) Name() string { return domainnotification.ChannelEmail }

// Deliver 渲染并发送：主题=Title，正文=Title + 空行 + Body（纯文本）。
// 用户缺失映射为错误（调用方记死信日志，不重试）。
func (c *EmailChannel) Deliver(ctx context.Context, d domainnotification.Delivery) error {
	user, err := c.users.GetByID(ctx, d.UserID)
	if err != nil {
		return fmt.Errorf("email channel: resolve recipient: %w", err)
	}
	subject := d.Title
	body := d.Title + "\n\n" + d.Body
	addr := fmt.Sprintf("%s:%d", c.cfg.Host, c.cfg.Port)
	auth := smtp.PlainAuth("", c.cfg.Username, c.cfg.Password, c.cfg.Host)
	from, to := c.cfg.From, []string{user.Email}
	// 事务性邮件最小头集；Content-Type 显式纯文本（部分客户端默认推断）。
	msg := []byte("To: " + user.Email + "\r\n" +
		"From: " + from + "\r\n" +
		"Subject: " + subject + "\r\n" +
		"MIME-Version: 1.0\r\n" +
		"Content-Type: text/plain; charset=UTF-8\r\n" +
		"\r\n" +
		body + "\r\n")
	// STARTTLS：587 端口的标准路径；465（隐式 TLS）不覆盖（文档标注）。
	return smtp.SendMail(addr, auth, from, to, msg)
}

// InAppChannel 是站内渠道（渲染快照落库——与通知本体同一存储）。
type InAppChannel struct {
	repo domainnotification.Repository
}

// NewInAppChannel 构造站内渠道。
func NewInAppChannel(repo domainnotification.Repository) *InAppChannel {
	return &InAppChannel{repo: repo}
}

var _ domainnotification.Channel = (*InAppChannel)(nil)

// Name 实现 domain.Channel。
func (c *InAppChannel) Name() string { return domainnotification.ChannelInApp }

// Deliver 落库一条站内通知（append 事实）。
func (c *InAppChannel) Deliver(ctx context.Context, d domainnotification.Delivery) error {
	n, err := domainnotification.NewNotification(
		idgen.New(), d.UserID, d.Type, d.Title, d.Body, d.Payload, time.Now().UTC(),
	)
	if err != nil {
		return err
	}
	return c.repo.Create(ctx, n)
}
