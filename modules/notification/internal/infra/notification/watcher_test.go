package notification

import (
	"context"
	"encoding/json"
	"log/slog"
	"testing"
	"time"

	"github.com/lynxland/appkit/events"
	"github.com/lynxland/lynx-mods/modules/notification/domain/notification"
	appnotification "github.com/lynxland/lynx-mods/modules/notification/internal/app/notification"
	"github.com/lynxland/lynx/eventbus"
)

// watcherRepo 是本测试的最简内存仓储（用例层需要的完整端口面）。
type watcherRepo struct {
	created []*notification.Notification
}

func (r *watcherRepo) Create(_ context.Context, n *notification.Notification) error {
	r.created = append(r.created, n)
	return nil
}

func (r *watcherRepo) ListByUser(context.Context, string, notification.Query) ([]*notification.Notification, int, error) {
	return nil, 0, nil
}

func (r *watcherRepo) CountUnread(context.Context, string) (int, error) { return 0, nil }

func (r *watcherRepo) MarkRead(context.Context, string, string, time.Time) (bool, error) {
	return false, nil
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
	uc := appnotification.New(repo)

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
	mustPublish("identity.user.password-changed", "identity", map[string]string{"id": "u1"})
	mustPublish("identity.user.created", "identity", map[string]string{"id": "u1"}) // 干扰：不产通知
	mustPublish("organization.member.added", "organization",
		map[string]string{"organization_id": "o1", "organization_name": "Acme Inc", "user_id": "u1"})
	mustPublish("organization.member.removed", "organization",
		map[string]string{"organization_id": "o1", "organization_name": "Acme Inc", "user_id": "u2"})
	mustPublish("organization.member.added", "organization",
		map[string]string{"organization_id": "o9"}) // 载荷缺字段：吞掉

	deadline := time.Now().Add(3 * time.Second)
	for len(repo.created) != 3 {
		if time.Now().After(deadline) {
			t.Fatalf("delivered %d notifications, want 3", len(repo.created))
		}
		time.Sleep(20 * time.Millisecond)
	}

	byType := map[string]string{}
	for _, n := range repo.created {
		byType[n.Type] = n.Title
	}
	if byType["identity.user.password-changed"] != "密码已更改" {
		t.Errorf("password-changed title = %q", byType["identity.user.password-changed"])
	}
	if byType["organization.member.added"] != "你已被加入组织 Acme Inc" {
		t.Errorf("member.added title = %q", byType["organization.member.added"])
	}
	if byType["organization.member.removed"] != "你已被移出组织 Acme Inc" {
		t.Errorf("member.removed title = %q", byType["organization.member.removed"])
	}
}
