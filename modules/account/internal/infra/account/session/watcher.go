package session

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/lynxland/appkit/events"
	"github.com/lynxland/lynx"
	domainidentity "github.com/lynxland/lynx-start/modules/account/domain/account/identity"
	appsession "github.com/lynxland/lynx-start/modules/account/internal/app/account/session"
	"github.com/lynxland/lynx/eventbus"
)

// PasswordChangedWatcher 订阅 identity 域的事件目录（topic=module.account；
// ADR-0009 合并后为模块内联动，直接引用 identity 域常量），在
// account.user.password-changed 时吊销该用户全部会话——「改密即全端
// 下线」。
type PasswordChangedWatcher struct {
	bus    eventbus.Bus
	uc     *appsession.UseCase
	logger *slog.Logger
}

// NewPasswordChangedWatcher 构造订阅器（Assemble 调用）。
func NewPasswordChangedWatcher(bus eventbus.Bus, uc *appsession.UseCase, logger *slog.Logger) *PasswordChangedWatcher {
	return &PasswordChangedWatcher{bus: bus, uc: uc, logger: logger}
}

var _ lynx.Service = (*PasswordChangedWatcher)(nil)

// Name 实现 lynx.Service。
func (w *PasswordChangedWatcher) Name() string { return "account-session-password-changed-watcher" }

// Init 实现 lynx.Service（无初始化动作）。
func (w *PasswordChangedWatcher) Init(lynx.AppContext) error { return nil }

// Start 订阅并保持存活直至框架关停。
func (w *PasswordChangedWatcher) Start(ctx context.Context) error {
	err := w.bus.Subscribe(ctx, events.TopicFor(domainidentity.EventUserPasswordChanged), w.handle,
		eventbus.WithHandlerName("account-session-password-changed"))
	if err != nil {
		return err
	}
	return lynx.WaitForShutdown(ctx)
}

// Stop 实现 lynx.Service（内存总线无需显式退订，随进程生命周期）。
func (w *PasswordChangedWatcher) Stop(context.Context) error { return nil }

// handle 解析事件信封并联动吊销。信封不合法/载荷缺 id 记日志吞掉
// （发布方违约不该让消费方无限重投）；吊销失败返回错误交总线重试。
func (w *PasswordChangedWatcher) handle(ctx context.Context, e *eventbus.RawEvent) error {
	var env events.Envelope
	if err := json.Unmarshal(e.Payload, &env); err != nil {
		w.logger.Warn("account session watcher: malformed envelope, drop", "topic", e.Topic, "error", err)
		return nil
	}
	if env.Name != domainidentity.EventUserPasswordChanged && env.Name != domainidentity.EventUserDisabled {
		return nil
	}
	var payload struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(env.Payload, &payload); err != nil || payload.ID == "" {
		w.logger.Warn("account session watcher: payload missing user id, drop", "event", env.Name)
		return nil
	}
	// 改密与停用（v0.7.0）同款联动：吊销该用户全部会话。
	if err := w.uc.OnPasswordChanged(ctx, payload.ID); err != nil {
		w.logger.Error("account session watcher: revoke on password change failed, will retry", "user_id", payload.ID, "error", err)
		return err
	}
	return nil
}
