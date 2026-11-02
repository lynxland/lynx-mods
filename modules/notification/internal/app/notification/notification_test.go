package notification

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/lynxland/lynx-mods/modules/notification/domain/notification"
)

// fakeRepo 是内存仓储（不透明实现：测试只经端口交互）。
type fakeRepo struct {
	items []*notification.Notification
}

func (r *fakeRepo) Create(_ context.Context, n *notification.Notification) error {
	r.items = append(r.items, n)
	return nil
}

func (r *fakeRepo) ListByUser(_ context.Context, userID string, q notification.Query) ([]*notification.Notification, int, error) {
	var matched []*notification.Notification
	for _, n := range r.items {
		if n.UserID != userID {
			continue
		}
		if q.UnreadOnly && !n.Unread() {
			continue
		}
		matched = append(matched, n)
	}
	total := len(matched)
	end := q.Offset + q.PageSize
	if end > total {
		end = total
	}
	if q.Offset > total {
		q.Offset = total
	}
	return matched[q.Offset:end], total, nil
}

func (r *fakeRepo) CountUnread(_ context.Context, userID string) (int, error) {
	n := 0
	for _, it := range r.items {
		if it.UserID == userID && it.Unread() {
			n++
		}
	}
	return n, nil
}

func (r *fakeRepo) MarkRead(_ context.Context, id, userID string, readAt time.Time) (bool, error) {
	for _, it := range r.items {
		if it.ID == id && it.UserID == userID {
			it.MarkRead(readAt)
			return true, nil
		}
	}
	return false, notification.ErrNotificationNotFound
}

func (r *fakeRepo) MarkAllRead(_ context.Context, userID string, readAt time.Time) (int64, error) {
	var n int64
	for _, it := range r.items {
		if it.UserID == userID && it.Unread() {
			it.MarkRead(readAt)
			n++
		}
	}
	return n, nil
}

var _ notification.Repository = (*fakeRepo)(nil)

func TestOnEventCreatesSnapshot(t *testing.T) {
	repo := &fakeRepo{}
	uc := New(repo)
	ctx := context.Background()

	if err := uc.OnPasswordChanged(ctx, "u1"); err != nil {
		t.Fatal(err)
	}
	if err := uc.OnMemberAdded(ctx, "u1", "o1", "Acme Inc"); err != nil {
		t.Fatal(err)
	}
	if err := uc.OnMemberRemoved(ctx, "u2", "o1", "Acme Inc"); err != nil {
		t.Fatal(err)
	}

	if len(repo.items) != 3 {
		t.Fatalf("created %d notifications, want 3", len(repo.items))
	}
	pw := repo.items[0]
	if pw.Type != notification.IdentityUserPasswordChanged || pw.UserID != "u1" {
		t.Errorf("password-changed: type=%s user=%s", pw.Type, pw.UserID)
	}
	if pw.Title != "密码已更改" || pw.Body == "" {
		t.Errorf("password-changed snapshot: title=%q body=%q", pw.Title, pw.Body)
	}
	added := repo.items[1]
	if added.Title != "你已被加入组织 Acme Inc" {
		t.Errorf("member.added title = %q", added.Title)
	}
	if added.Payload["organization_id"] != "o1" {
		t.Errorf("member.added payload = %v", added.Payload)
	}
	removed := repo.items[2]
	if removed.UserID != "u2" || removed.Title != "你已被移出组织 Acme Inc" {
		t.Errorf("member.removed: user=%s title=%q", removed.UserID, removed.Title)
	}
}

func TestListUnreadFilterAndCount(t *testing.T) {
	repo := &fakeRepo{}
	uc := New(repo)
	ctx := context.Background()
	_ = uc.OnMemberAdded(ctx, "u1", "o1", "Acme") // 未读
	_ = uc.OnMemberAdded(ctx, "u1", "o2", "Globex")
	_ = uc.OnPasswordChanged(ctx, "u1") // 将标记已读
	_ = uc.OnPasswordChanged(ctx, "u2") // 他人的，不计数

	res, err := uc.List(ctx, "u1", false, 0, "")
	if err != nil || len(res.Notifications) != 3 {
		t.Fatalf("list u1 = %d items err=%v", len(res.Notifications), err)
	}
	if err := uc.MarkRead(ctx, "u1", repo.items[2].ID); err != nil {
		t.Fatal(err)
	}
	res, err = uc.List(ctx, "u1", true, 0, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Notifications) != 2 || res.TotalSize != 2 {
		t.Errorf("unread list = %d/%d, want 2/2", len(res.Notifications), res.TotalSize)
	}
	if res.UnreadSize != 2 {
		t.Errorf("unread size = %d, want 2", res.UnreadSize)
	}
}

func TestMarkReadVisibilityAndIdempotent(t *testing.T) {
	repo := &fakeRepo{}
	uc := New(repo)
	ctx := context.Background()
	_ = uc.OnPasswordChanged(ctx, "u1")
	id := repo.items[0].ID

	if err := uc.MarkRead(ctx, "u2", id); !errors.Is(err, notification.ErrNotificationNotFound) {
		t.Fatalf("other's notification: err = %v, want ErrNotificationNotFound", err)
	}
	if err := uc.MarkRead(ctx, "u1", id); err != nil {
		t.Fatal(err)
	}
	if err := uc.MarkRead(ctx, "u1", id); err != nil { // 幂等
		t.Fatalf("second mark: %v", err)
	}
	if repo.items[0].Unread() {
		t.Error("still unread after MarkRead")
	}
}

func TestMarkAllRead(t *testing.T) {
	repo := &fakeRepo{}
	uc := New(repo)
	ctx := context.Background()
	_ = uc.OnPasswordChanged(ctx, "u1")
	_ = uc.OnMemberAdded(ctx, "u1", "o1", "Acme")
	_ = uc.OnPasswordChanged(ctx, "u2")

	n, err := uc.MarkAllRead(ctx, "u1")
	if err != nil || n != 2 {
		t.Fatalf("mark all = %d err=%v, want 2", n, err)
	}
	for _, it := range repo.items[:2] {
		if it.Unread() {
			t.Errorf("%s still unread", it.ID)
		}
	}
	if !repo.items[2].Unread() {
		t.Error("u2 notification should stay unread")
	}
}
