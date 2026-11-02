package notification

import (
	"context"
	"encoding/json"
	"log/slog"
	"slices"

	"github.com/lynx-go/lynx"
	"github.com/lynx-go/lynx-mods/modules/notification/domain/notification"
	appnotification "github.com/lynx-go/lynx-mods/modules/notification/internal/app/notification"
	"github.com/lynx-go/lynx/eventbus"
	"github.com/lynxland/appkit/events"
)

// Watcher 订阅 identity 与 organization 的事件目录（契约②：字符串目录
// 消费，不 import 发布方代码；topic=module.<m> 前缀收敛），把成员事实
// 变化转为收件人的站内通知。单个 Service 订三个 topic，handle 按事件
// 名路由。
type Watcher struct {
	bus    eventbus.Bus
	uc     *appnotification.UseCase
	logger *slog.Logger
}

// NewWatcher 构造订阅器（Assemble 调用）。
func NewWatcher(bus eventbus.Bus, uc *appnotification.UseCase, logger *slog.Logger) *Watcher {
	return &Watcher{bus: bus, uc: uc, logger: logger}
}

var _ lynx.Service = (*Watcher)(nil)

// Name 实现 lynx.Service。
func (w *Watcher) Name() string { return "notification-watcher" }

// Init 实现 lynx.Service（无初始化动作）。
func (w *Watcher) Init(lynx.AppContext) error { return nil }

// Start 订阅事件目录涉及的 topic 并保持存活直至框架关停。topic 是
// module.<m> 前缀收敛——同模块的多个事件共享一个 topic，必须去重后
// 每话题只订一次（重复订阅会让事件投递多遍）；事件名路由在 handle。
func (w *Watcher) Start(ctx context.Context) error {
	var topics []string
	for _, name := range []string{
		notification.IdentityUserPasswordChanged,
		notification.OrganizationMemberAdded,
		notification.OrganizationMemberRemoved,
	} {
		topic := events.TopicFor(name)
		if !slices.Contains(topics, topic) {
			topics = append(topics, topic)
		}
	}
	for _, topic := range topics {
		if err := w.bus.Subscribe(ctx, topic, w.handle,
			eventbus.WithHandlerName("notification-"+topic)); err != nil {
			return err
		}
	}
	return lynx.WaitForShutdown(ctx)
}

// Stop 实现 lynx.Service（内存总线无需显式退订，随进程生命周期）。
func (w *Watcher) Stop(context.Context) error { return nil }

// handle 解析事件信封并路由到对应通知用例。信封不合法/载荷缺字段/事件
// 不在目录内一律记日志吞掉（发布方违约不该让消费方无限重投）；写库失败
// 返回错误交总线重试。
func (w *Watcher) handle(ctx context.Context, e *eventbus.RawEvent) error {
	var env events.Envelope
	if err := json.Unmarshal(e.Payload, &env); err != nil {
		w.logger.Warn("notification watcher: malformed envelope, drop", "topic", e.Topic, "error", err)
		return nil
	}
	switch env.Name {
	case notification.IdentityUserPasswordChanged:
		var payload struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(env.Payload, &payload); err != nil || payload.ID == "" {
			w.logger.Warn("notification watcher: payload missing user id, drop", "event", env.Name)
			return nil
		}
		if err := w.uc.OnPasswordChanged(ctx, payload.ID); err != nil {
			w.logger.Error("notification watcher: write failed, will retry", "event", env.Name, "error", err)
			return err
		}
		return nil
	case notification.OrganizationMemberAdded:
		return w.onMembership(ctx, env, w.uc.OnMemberAdded)
	case notification.OrganizationMemberRemoved:
		return w.onMembership(ctx, env, w.uc.OnMemberRemoved)
	default:
		// topic 是 module.<m> 前缀订阅，同模块其他事件也会到达——过滤。
		return nil
	}
}

// onMembership 解析 member.* 载荷（organization_name 自 organization
// v0.1.1 起提供）并路由。
func (w *Watcher) onMembership(ctx context.Context, env events.Envelope, deliver func(context.Context, string, string, string) error) error {
	var payload struct {
		OrganizationID   string `json:"organization_id"`
		OrganizationName string `json:"organization_name"`
		UserID           string `json:"user_id"`
	}
	if err := json.Unmarshal(env.Payload, &payload); err != nil ||
		payload.UserID == "" || payload.OrganizationID == "" || payload.OrganizationName == "" {
		w.logger.Warn("notification watcher: payload missing organization/user, drop", "event", env.Name)
		return nil
	}
	if err := deliver(ctx, payload.UserID, payload.OrganizationID, payload.OrganizationName); err != nil {
		w.logger.Error("notification watcher: write failed, will retry", "event", env.Name, "error", err)
		return err
	}
	return nil
}
