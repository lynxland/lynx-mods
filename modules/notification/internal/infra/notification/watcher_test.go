package notification

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/lynxland/appkit/crud"
	"github.com/lynxland/appkit/events"
	"github.com/lynxland/lynx-start/modules/notification/domain/notification"
	appnotification "github.com/lynxland/lynx-start/modules/notification/internal/app/notification"
	"github.com/lynxland/lynx/eventbus"
)

// watchChannel 是记录投递的假渠道（watcher 测试断言「事件→投递」链路）。
type watchChannel struct {
	mu        sync.Mutex
	delivered []notification.Delivery
}

func (c *watchChannel) Name() string { return notification.ChannelInApp }

func (c *watchChannel) Deliver(_ context.Context, d notification.Delivery) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.delivered = append(c.delivered, d)
	return nil
}

// newWatchUseCase 构造带 capture 渠道的用例。
func newWatchUseCase(repo *watcherRepo) (*appnotification.UseCase, *watchChannel) {
	var cfg appnotification.Config
	cfg.ApplyDefaults()
	ch := &watchChannel{}
	return appnotification.New(repo, []notification.Channel{ch}, cfg, nil), ch
}

// watcherRepo 是本测试的最简内存仓储（用例层需要的完整端口面）。
type watcherRepo struct {
	created []*notification.Notification
}

func (r *watcherRepo) Create(_ context.Context, n *notification.Notification) error {
	r.created = append(r.created, n)
	return nil
}

func (r *watcherRepo) ListByUser(context.Context, string, crud.PagePlan, bool) ([]*notification.Notification, int, error) {
	return nil, 0, nil
}

func (r *watcherRepo) CountUnread(context.Context, string) (int, error) { return 0, nil }

func (r *watcherRepo) MarkRead(context.Context, string, string, time.Time) (bool, error) {
	return false, nil
}

func (r *watcherRepo) UpsertPreference(context.Context, notification.Preference) error { return nil }
func (r *watcherRepo) ListPreferences(context.Context, string) ([]notification.Preference, error) {
	return nil, nil
}

func (r *watcherRepo) MarkAllRead(context.Context, string, time.Time) (int64, error) {
	return 0, nil
}

var _ notification.Repository = (*watcherRepo)(nil)

// TestWatcherDelivers 跨模块联动集成：identity/organization 的事件（经
// 总线 topic module.<m> 投递）→ 站内通知。内存总线异步投递，轮询等待
// 生效；同 topic 干扰事件与缺字段载荷不产通知。
func TestWatcherDelivers(t *testing.T) {
	bus := eventbus.NewMemoryBus(eventbus.Options{})
	pub := events.NewBusPublisher(bus)

	repo := &watcherRepo{}
	uc, ch := newWatchUseCase(repo)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w := NewWatcher(bus, uc, slog.Default())
	go func() { _ = w.Start(ctx) }()

	// 等订阅生效。
	time.Sleep(100 * time.Millisecond)

	mustPublish := func(name, module string, payload any) {
		t.Helper()
		raw, err := json.Marshal(payload)
		if err != nil {
			t.Fatal(err)
		}
		if err := pub.Publish(ctx, events.Envelope{
			ID: "evt-" + name, Name: name, Module: module, Payload: raw,
		}); err != nil {
			t.Fatal(err)
		}
	}

	// 三个目录事件 + 一个同 topic 干扰事件 + 一个缺字段事件。
	mustPublish("account.user.password-changed", "account", map[string]string{"id": "u1"})
	mustPublish("account.user.created", "account", map[string]string{"id": "u1"}) // 干扰：不产通知
	mustPublish("organization.member.added", "organization",
		map[string]string{"organization_id": "o1", "organization_name": "Acme Inc", "user_id": "u1"})
	mustPublish("organization.member.removed", "organization",
		map[string]string{"organization_id": "o1", "organization_name": "Acme Inc", "user_id": "u2"})
	mustPublish("organization.member.added", "organization",
		map[string]string{"organization_id": "o9"}) // 载荷缺字段：吞掉

	deadline := time.Now().Add(3 * time.Second)
	ch.mu.Lock()
	for len(ch.delivered) != 3 {
		ch.mu.Unlock()
		if time.Now().After(deadline) {
			ch.mu.Lock()
			t.Fatalf("delivered %d notifications, want 3", len(ch.delivered))
		}
		time.Sleep(20 * time.Millisecond)
		ch.mu.Lock()
	}

	byType := map[string]string{}
	for _, d := range ch.delivered {
		byType[d.Type] = d.Title
	}
	ch.mu.Unlock()
	if byType["account.user.password-changed"] != "密码已更改" {
		t.Errorf("password-changed title = %q", byType["account.user.password-changed"])
	}
	if byType["organization.member.added"] != "你已被加入组织 Acme Inc" {
		t.Errorf("member.added title = %q", byType["organization.member.added"])
	}
	if byType["organization.member.removed"] != "你已被移出组织 Acme Inc" {
		t.Errorf("member.removed title = %q", byType["organization.member.removed"])
	}
}
