package notification

import (
	"context"
	"testing"
	"time"

	"github.com/lynxland/appkit/crud"
	domainnotification "github.com/lynxland/lynx-start/modules/notification/domain/notification"
)

// fakeRepo 是内存仓储（不透明实现：测试只经端口交互）。
type fakeRepo struct {
	items map[string]*domainnotification.Notification
	order []string
	prefs []domainnotification.Preference
}

func newFakeRepo() *fakeRepo { return &fakeRepo{items: map[string]*domainnotification.Notification{}} }

func (r *fakeRepo) Create(_ context.Context, n *domainnotification.Notification) error {
	if r.items == nil {
		r.items = map[string]*domainnotification.Notification{}
	}
	r.items[n.ID] = n
	r.order = append(r.order, n.ID)
	return nil
}
func (r *fakeRepo) ListByUser(_ context.Context, userID string, _ crud.PagePlan, _ bool) ([]*domainnotification.Notification, int, error) {
	return nil, 0, nil
}
func (r *fakeRepo) CountUnread(context.Context, string) (int, error) { return 0, nil }
func (r *fakeRepo) MarkRead(context.Context, string, string, time.Time) (bool, error) {
	return false, nil
}
func (r *fakeRepo) MarkAllRead(context.Context, string, time.Time) (int64, error) {
	return 0, nil
}
func (r *fakeRepo) UpsertPreference(_ context.Context, p domainnotification.Preference) error {
	for i, e := range r.prefs {
		if e.Type == p.Type && e.Channel == p.Channel {
			r.prefs[i] = p
			return nil
		}
	}
	r.prefs = append(r.prefs, p)
	return nil
}
func (r *fakeRepo) ListPreferences(context.Context, string) ([]domainnotification.Preference, error) {
	return r.prefs, nil
}

// captureChannel 是记录投递的假渠道。
type captureChannel struct {
	name      string
	fail      bool
	delivered []domainnotification.Delivery
}

func (c *captureChannel) Name() string { return c.name }

func (c *captureChannel) Deliver(_ context.Context, d domainnotification.Delivery) error {
	if c.fail {
		return errDeliverFail
	}
	c.delivered = append(c.delivered, d)
	return nil
}

type deliverError struct{}

func (deliverError) Error() string { return "capture: fail" }

var errDeliverFail = deliverError{}

func newTestUseCase(repo *fakeRepo, channels ...domainnotification.Channel) *UseCase {
	var cfg Config
	cfg.ApplyDefaults()
	return New(repo, channels, cfg, nil)
}

func TestDispatchRendersTemplatesPerChannel(t *testing.T) {
	repo := newFakeRepo()
	inapp := &captureChannel{name: domainnotification.ChannelInApp}
	email := &captureChannel{name: domainnotification.ChannelEmail}
	uc := newTestUseCase(repo, inapp, email)
	ctx := context.Background()

	if err := uc.OnMemberAdded(ctx, "u1", "o1", "Acme Inc"); err != nil {
		t.Fatal(err)
	}
	if len(inapp.delivered) != 1 || len(email.delivered) != 1 {
		t.Fatalf("delivered inapp=%d email=%d, want 1/1", len(inapp.delivered), len(email.delivered))
	}
	d := inapp.delivered[0]
	if d.Type != domainnotification.OrganizationMemberAdded || d.UserID != "u1" {
		t.Errorf("delivery = %s/%s", d.Type, d.UserID)
	}
	if want := "你已被加入组织 Acme Inc"; d.Title != want {
		t.Errorf("title = %q, want %q（模板渲染）", d.Title, want)
	}
}

func TestPreferenceFiltersChannel(t *testing.T) {
	repo := newFakeRepo()
	inapp := &captureChannel{name: domainnotification.ChannelInApp}
	email := &captureChannel{name: domainnotification.ChannelEmail}
	uc := newTestUseCase(repo, inapp, email)
	ctx := context.Background()

	// 关闭邮件渠道的该类型投递；站内默认启用。
	if err := uc.SetMyPreference(ctx, "u1", domainnotification.IdentityUserPasswordChanged, domainnotification.ChannelEmail, false); err != nil {
		t.Fatal(err)
	}
	if err := uc.OnPasswordChanged(ctx, "u1"); err != nil {
		t.Fatal(err)
	}
	if len(inapp.delivered) != 1 {
		t.Fatalf("inapp delivered %d, want 1（未配置=启用）", len(inapp.delivered))
	}
	if len(email.delivered) != 0 {
		t.Fatalf("email delivered %d, want 0（偏好关闭）", len(email.delivered))
	}
}

func TestDeliverFailureDoesNotBlockOtherChannels(t *testing.T) {
	repo := newFakeRepo()
	broken := &captureChannel{name: domainnotification.ChannelInApp, fail: true}
	email := &captureChannel{name: domainnotification.ChannelEmail}
	uc := newTestUseCase(repo, broken, email)

	if err := uc.OnPasswordChanged(context.Background(), "u1"); err != nil {
		t.Fatal(err)
	}
	if len(email.delivered) != 1 {
		t.Fatalf("email delivered %d, want 1（他渠道失败不阻断）", len(email.delivered))
	}
}

func TestSendCustomType(t *testing.T) {
	repo := newFakeRepo()
	inapp := &captureChannel{name: domainnotification.ChannelInApp}
	uc := newTestUseCase(repo, inapp)
	ctx := context.Background()

	if err := uc.Send(ctx, domainnotification.SendInput{
		UserID: "u1", Title: "标题经载荷", Body: "正文",
		Payload: map[string]string{"title": "标题经载荷", "body": "正文"},
	}); err != nil {
		t.Fatal(err)
	}
	if len(inapp.delivered) != 1 || inapp.delivered[0].Title != "标题经载荷" {
		t.Fatalf("custom send: %+v", inapp.delivered)
	}
	if inapp.delivered[0].Type != domainnotification.TypeCustom {
		t.Errorf("custom type = %s", inapp.delivered[0].Type)
	}
}
