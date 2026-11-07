// Package notification 是通知模块的用例层：投递管线（模板渲染 → 渠道
// 偏好过滤 → 多渠道尽力投递）与自助查询/已读/偏好管理。文案在渲染时刻
// 定格为快照（模板可改，已投递的不受影响）。
package notification

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"text/template"
	"time"

	"github.com/lynxland/appkit/crud"
	"github.com/lynxland/lynx-start/modules/notification/domain/notification"
)

// Template 是一类通知的文案模板（text/template；数据=事件载荷
// map[string]string——如 {{.organization_name}}）。
type Template struct {
	Title string `mapstructure:"title"`
	Body  string `mapstructure:"body"`
}

// SMTP 是邮件渠道配置（notification.smtp 段；Host 空 = 渠道禁用）。
type SMTP struct {
	Host     string `mapstructure:"host"`
	Port     int    `mapstructure:"port"`
	Username string `mapstructure:"username"`
	Password string `mapstructure:"password"`
	From     string `mapstructure:"from"`
}

// Config 是通知模块的配置段。
type Config struct {
	Templates map[string]Template `mapstructure:"templates"`
	SMTP      SMTP                `mapstructure:"smtp"`
}

// defaultTemplates 是各通知类型的默认文案（与 v0.6.0 硬编码一致——
// 已装应用零文案漂移；应用在 config.yaml 覆盖或扩充）。
func defaultTemplates() map[string]Template {
	return map[string]Template{
		notification.IdentityUserPasswordChanged: {
			Title: "密码已更改",
			Body:  "你的密码刚刚被更改。如果不是你本人操作，请立即重置密码。",
		},
		notification.OrganizationMemberAdded: {
			Title: "你已被加入组织 {{.organization_name}}",
			Body:  "你已被组织 {{.organization_name}} 添加为成员。",
		},
		notification.OrganizationMemberRemoved: {
			Title: "你已被移出组织 {{.organization_name}}",
			Body:  "你不再是组织 {{.organization_name}} 的成员。",
		},
		notification.TypeCustom: {
			Title: "{{.title}}",
			Body:  "{{.body}}",
		},
		// account 的一次性令牌流（v0.7.0）：令牌在载荷——邮件模板应渲染
		// 成带链接的正文（应用在 config.yaml 覆盖为自家前端地址）。
		notification.AccountUserVerificationRequested: {
			Title: "验证你的邮箱",
			Body:  "请使用以下验证令牌完成邮箱验证（15 分钟内有效）：{{.token}}",
		},
		notification.AccountUserResetRequested: {
			Title: "重置你的密码",
			Body:  "请使用以下重置令牌重置密码（15 分钟内有效）：{{.token}}",
		},
	}
}

// ApplyDefaults 落默认值（LoadSection 的 defaults 钩子；用户声明的模板
// 覆盖同名默认条目，其余保留）。
func (c *Config) ApplyDefaults() {
	if c.Templates == nil {
		c.Templates = defaultTemplates()
		return
	}
	for typ, tpl := range defaultTemplates() {
		if _, ok := c.Templates[typ]; !ok {
			c.Templates[typ] = tpl
		}
	}
}

// UseCase 是通知模块的用例集。
type UseCase struct {
	repo      notification.Repository
	channels  []notification.Channel
	templates map[string]Template
	logger    *slog.Logger
	now       func() time.Time
}

// New 构造用例（Assemble 调用；channels 为已启用渠道——站内必在，
// 邮件视 SMTP 配置）。
func New(repo notification.Repository, channels []notification.Channel, cfg Config, logger *slog.Logger) *UseCase {
	if logger == nil {
		logger = slog.Default()
	}
	return &UseCase{repo: repo, channels: channels, templates: cfg.Templates, logger: logger, now: time.Now}
}

// ---- 投递管线 ----

// dispatch 是所有投递（事件订阅与显式 Send）的唯一路径：模板渲染 →
// 渠道偏好过滤 → 逐渠道尽力投递（失败记死信日志，不阻断其他渠道——
// 单进程内无重试；高可靠投递经 task 模块组合，见 README）。
func (u *UseCase) dispatch(ctx context.Context, userID, typ string, payload map[string]string) error {
	title, body, err := u.render(typ, payload)
	if err != nil {
		return err
	}
	prefs, err := u.repo.ListPreferences(ctx, userID)
	if err != nil {
		return err
	}
	enabled := map[string]bool{} // 未配置 = 启用
	for _, p := range prefs {
		if p.Type == typ {
			enabled[p.Channel] = p.Enabled
		}
	}
	for _, ch := range u.channels {
		if on, ok := enabled[ch.Name()]; ok && !on {
			continue
		}
		if err := ch.Deliver(ctx, notification.Delivery{
			UserID: userID, Type: typ, Title: title, Body: body, Payload: payload,
		}); err != nil {
			u.logger.Error("notification: channel deliver failed (dead-lettered)",
				"channel", ch.Name(), "user_id", userID, "type", typ, "error", err)
		}
	}
	return nil
}

// render 渲染类型模板（数据=载荷；模板缺失退化为 title=类型名）。
func (u *UseCase) render(typ string, payload map[string]string) (string, string, error) {
	tpl, ok := u.templates[typ]
	if !ok {
		tpl = Template{Title: typ, Body: ""}
	}
	render := func(text string) (string, error) {
		if text == "" {
			return "", nil
		}
		t, err := template.New(typ).Parse(text)
		if err != nil {
			return "", fmt.Errorf("notification: template %s: %w", typ, err)
		}
		var b strings.Builder
		if err := t.Execute(&b, payload); err != nil {
			return "", fmt.Errorf("notification: template %s: %w", typ, err)
		}
		return b.String(), nil
	}
	title, err := render(tpl.Title)
	if err != nil {
		return "", "", err
	}
	body, err := render(tpl.Body)
	if err != nil {
		return "", "", err
	}
	return title, body, nil
}

// ---- 显式发送（domain.Sender）----

// Send 显式发送（domain.Sender 通道③ 能力；Type 空则 notification.custom，
// 模板可引用载荷的 title/body）。
func (u *UseCase) Send(ctx context.Context, input notification.SendInput) error {
	if input.Type == "" {
		input.Type = notification.TypeCustom
	}
	return u.dispatch(ctx, input.UserID, input.Type, input.Payload)
}

// 编译期断言：用例实现 domain.Sender。
var _ notification.Sender = (*UseCase)(nil)

// ---- 事件订阅 ----

// OnPasswordChanged 生成改密安全提醒（收件人 = 事件载荷里的用户）。
func (u *UseCase) OnPasswordChanged(ctx context.Context, userID string) error {
	return u.dispatch(ctx, userID, notification.IdentityUserPasswordChanged, map[string]string{"id": userID})
}

// OnMemberAdded 生成「被加入组织」通知。
func (u *UseCase) OnMemberAdded(ctx context.Context, userID, organizationID, organizationName string) error {
	return u.dispatch(ctx, userID, notification.OrganizationMemberAdded,
		map[string]string{"organization_id": organizationID, "user_id": userID, "organization_name": organizationName})
}

// OnMemberRemoved 生成「被移出组织」通知。
func (u *UseCase) OnMemberRemoved(ctx context.Context, userID, organizationID, organizationName string) error {
	return u.dispatch(ctx, userID, notification.OrganizationMemberRemoved,
		map[string]string{"organization_id": organizationID, "user_id": userID, "organization_name": organizationName})
}

// ---- 偏好（自助面）----

// ListMyPreferences 返回用户已配置的偏好行（未配置类型默认启用）。
func (u *UseCase) ListMyPreferences(ctx context.Context, userID string) ([]notification.Preference, error) {
	return u.repo.ListPreferences(ctx, userID)
}

// SetMyPreference 写一条偏好（channel 校验 fail-closed）。
func (u *UseCase) SetMyPreference(ctx context.Context, userID, typ, channel string, enabled bool) error {
	if channel != notification.ChannelInApp && channel != notification.ChannelEmail {
		return fmt.Errorf("notification: unknown channel %q", channel)
	}
	return u.repo.UpsertPreference(ctx, notification.Preference{
		UserID: userID, Type: typ, Channel: channel, Enabled: enabled,
	})
}

// ---- 自助查询 ----

// ListResult 是通知列表用例的产物。
type ListResult struct {
	Notifications []*notification.Notification
	NextToken     string
	TotalSize     int32
	UnreadSize    int32
}

// listSpec 是通知列表的分页规约（crud v2：keyset 游标，created_at DESC +
// id 决胜；总数另经 COUNT 供粗参考）。
var listSpec = crud.Spec{
	Modes:        crud.ModeSetKeyset,
	Sortable:     map[string]string{"createdAt": "n.created_at", "id": "n.id"},
	Tiebreak:     "id",
	DefaultLimit: 20,
	MaxLimit:     100,
}

// List 列收件人通知（keyset 分页；unreadOnly 仅未读），附带整户未读计数。
func (u *UseCase) List(ctx context.Context, userID string, unreadOnly bool, pageSize int32, pageToken string) (*ListResult, error) {
	q, err := crud.Parse[any](listSpec, crud.RawParams{After: pageToken, Limit: pageSize, OrderBy: "createdAt desc"}, nil)
	if err != nil {
		return nil, err
	}
	items, total, err := u.repo.ListByUser(ctx, userID, q.PagePlan, unreadOnly)
	if err != nil {
		return nil, err
	}
	next := ""
	if crud.HasMore(int32(len(items)), q.Limit) && len(items) > 0 {
		last := items[len(items)-1]
		if next, err = crud.Cursor(q, []string{last.CreatedAt.UTC().Format(time.RFC3339Nano), last.ID}); err != nil {
			return nil, err
		}
	}
	unread, err := u.repo.CountUnread(ctx, userID)
	if err != nil {
		return nil, err
	}
	return &ListResult{
		Notifications: items, NextToken: next,
		TotalSize: int32(total), UnreadSize: int32(unread),
	}, nil
}

// MarkRead 标记单条已读（他人通知与不存在统一 ErrNotificationNotFound，
// 幂等）。
func (u *UseCase) MarkRead(ctx context.Context, userID, id string) error {
	_, err := u.repo.MarkRead(ctx, id, userID, u.now().UTC())
	return err
}

// MarkAllRead 全部未读置已读，返回标记条数。
func (u *UseCase) MarkAllRead(ctx context.Context, userID string) (int64, error) {
	return u.repo.MarkAllRead(ctx, userID, u.now().UTC())
}

// OnVerificationRequested 投递邮箱验证令牌（account 的事件）。
func (u *UseCase) OnVerificationRequested(ctx context.Context, userID, token string) error {
	return u.dispatch(ctx, userID, notification.AccountUserVerificationRequested,
		map[string]string{"id": userID, "token": token})
}

// OnResetRequested 投递密码重置令牌（account 的事件）。
func (u *UseCase) OnResetRequested(ctx context.Context, userID, token string) error {
	return u.dispatch(ctx, userID, notification.AccountUserResetRequested,
		map[string]string{"id": userID, "token": token})
}
