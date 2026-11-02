package notification

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/lynxland/appkit/contrib/bunx/testutil"
	"github.com/lynxland/lynx-mods/modules/notification/domain/notification"
	"github.com/lynxland/lynx-mods/modules/notification/internal/infra/notification/migrations"
)

func TestRepo(t *testing.T) {
	testutil.SkipUnlessPg(t)
	db := testutil.NewDB(t, testutil.Chain{Module: "notification", FS: migrations.FS})
	repo := NewRepository(db)
	ctx := context.Background()

	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	mk := func(id, userID, typ, title string) *notification.Notification {
		n, err := notification.NewNotification(id, userID, typ, title, "", map[string]string{"k": "v"}, t0)
		if err != nil {
			t.Fatal(err)
		}
		return n
	}

	if err := repo.Create(ctx, mk("n1", "u1", "identity.user.password-changed", "密码已更改")); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := repo.Create(ctx, mk("n2", "u1", "organization.member.added", "你已被加入组织 Acme Inc")); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := repo.Create(ctx, mk("n3", "u2", "organization.member.removed", "你已被移出组织 Acme Inc")); err != nil {
		t.Fatalf("create: %v", err)
	}

	t.Run("list by user with unread filter", func(t *testing.T) {
		items, total, err := repo.ListByUser(ctx, "u1", notification.Query{PageSize: 10})
		if err != nil || total != 2 || len(items) != 2 {
			t.Fatalf("list u1 = %d/%d err=%v", len(items), total, err)
		}
		// created_at DESC：同为 t0 时按 id DESC。
		if items[0].ID != "n2" {
			t.Errorf("order: first = %s, want n2", items[0].ID)
		}
		// payload 语义读回（jsonb 重序列化不做字节比较）。
		if items[0].Payload["k"] != "v" {
			t.Errorf("payload = %v", items[0].Payload)
		}
	})

	t.Run("count unread", func(t *testing.T) {
		n, err := repo.CountUnread(ctx, "u1")
		if err != nil || n != 2 {
			t.Fatalf("unread u1 = %d err=%v, want 2", n, err)
		}
	})

	t.Run("mark read visibility and idempotent", func(t *testing.T) {
		if _, err := repo.MarkRead(ctx, "n1", "u2", t0.Add(time.Minute)); err == nil {
			t.Fatal("other's notification should not be markable")
		}
		updated, err := repo.MarkRead(ctx, "n1", "u1", t0.Add(time.Minute))
		if err != nil || !updated {
			t.Fatalf("mark read: updated=%v err=%v", updated, err)
		}
		// 幂等：再标不报错。
		if _, err := repo.MarkRead(ctx, "n1", "u1", t0.Add(2*time.Minute)); err != nil {
			t.Fatalf("second mark: %v", err)
		}
		n, _ := repo.CountUnread(ctx, "u1")
		if n != 1 {
			t.Errorf("unread after mark = %d, want 1", n)
		}
		items, _, _ := repo.ListByUser(ctx, "u1", notification.Query{PageSize: 10, UnreadOnly: true})
		if len(items) != 1 || items[0].ID != "n2" {
			t.Errorf("unread-only list = %v", items)
		}
	})

	t.Run("ghost mark read maps not found", func(t *testing.T) {
		if _, err := repo.MarkRead(ctx, "ghost", "u1", t0); !errors.Is(err, notification.ErrNotificationNotFound) {
			t.Fatalf("ghost: err = %v, want ErrNotificationNotFound", err)
		}
	})

	t.Run("mark all read", func(t *testing.T) {
		n, err := repo.MarkAllRead(ctx, "u1", t0.Add(time.Hour))
		if err != nil || n != 1 {
			t.Fatalf("mark all = %d err=%v, want 1（n1 已读）", n, err)
		}
		if n, _ := repo.CountUnread(ctx, "u1"); n != 0 {
			t.Errorf("unread after mark all = %d, want 0", n)
		}
		if n, _ := repo.CountUnread(ctx, "u2"); n != 1 {
			t.Errorf("u2 should stay unread")
		}
	})
}
