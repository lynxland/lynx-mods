package access

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/lynxland/appkit/crud"
	"github.com/lynxland/appkit/events"
	"github.com/lynxland/lynx-start/modules/account/domain/account/access"
	"github.com/lynxland/lynx-start/modules/account/domain/account/identity"
)

// fakeRepo 是分配仓储端口的内存实现。
type fakeRepo struct {
	mu    sync.Mutex
	rows  map[[2]string]time.Time // (user, role) → grantedAt
	order []string                // 事件对账用：追加 "user:role"
}

func newFakeRepo() *fakeRepo { return &fakeRepo{rows: map[[2]string]time.Time{}} }

func (r *fakeRepo) Assign(_ context.Context, a *access.Assignment) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := [2]string{a.UserID, a.Role}
	if _, ok := r.rows[key]; ok {
		return false, nil
	}
	r.rows[key] = a.GrantedAt
	r.order = append(r.order, key[0]+":"+key[1])
	return true, nil
}

func (r *fakeRepo) Revoke(_ context.Context, userID, role string) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := [2]string{userID, role}
	if _, ok := r.rows[key]; !ok {
		return false, nil
	}
	delete(r.rows, key)
	return true, nil
}

func (r *fakeRepo) RolesForUser(_ context.Context, userID string) ([]string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var roles []string
	for k := range r.rows {
		if k[0] == userID {
			roles = append(roles, k[1])
		}
	}
	return roles, nil
}

// fakeUserRepo 是 identity 仓储端口的内存实现（DependsOn 消费面）。
type fakeUserRepo struct{ users map[string]*identity.User }

func (r *fakeUserRepo) Create(_ context.Context, u *identity.User) error {
	r.users[u.ID] = u
	return nil
}

func (r *fakeUserRepo) GetByID(_ context.Context, id string) (*identity.User, error) {
	if u, ok := r.users[id]; ok {
		return u, nil
	}
	return nil, identity.ErrUserNotFound
}

func (r *fakeUserRepo) GetByEmail(_ context.Context, email string) (*identity.User, error) {
	for _, u := range r.users {
		if u.Email == identity.NormalizeEmail(email) {
			return u, nil
		}
	}
	return nil, identity.ErrUserNotFound
}

func (r *fakeUserRepo) UpdateStatus(context.Context, string, string, time.Time) error { return nil }
func (r *fakeUserRepo) UpdateEmailVerified(context.Context, string, bool, time.Time) error {
	return nil
}
func (r *fakeUserRepo) List(context.Context, crud.PagePlan) ([]*identity.User, int, error) {
	return nil, 0, nil
}
func (r *fakeUserRepo) UpdatePassword(_ context.Context, _ *identity.User) error { return nil }

type fakeTx struct{}

func (fakeTx) Run(_ context.Context, fn func(ctx context.Context) error) error {
	return fn(context.Background())
}

type fakePublisher struct {
	mu   sync.Mutex
	evts []events.Envelope
}

func (p *fakePublisher) Publish(_ context.Context, ev events.Envelope) error {
	if err := ev.Validate(); err != nil {
		return err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.evts = append(p.evts, ev)
	return nil
}

func (p *fakePublisher) names() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]string, len(p.evts))
	for i, e := range p.evts {
		out[i] = e.Name
	}
	return out
}

var testRoles = map[string][]string{
	"admin":  {"account.assignment.write", "audit.read"},
	"editor": {"demo.item.write"},
}

func newTestUseCase(t *testing.T) (*UseCase, *fakeRepo, *fakePublisher) {
	t.Helper()
	repo := newFakeRepo()
	pub := &fakePublisher{}
	return New(repo, &fakeUserRepo{users: map[string]*identity.User{
		"u1": {ID: "u1", Email: "a@example.com", Status: identity.StatusActive},
		"u2": {ID: "u2", Email: "b@example.com", Status: identity.StatusActive},
	}}, fakeTx{}, pub, testRoles, func() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) }), repo, pub
}

func TestGrantRevoke(t *testing.T) {
	uc, _, pub := newTestUseCase(t)
	ctx := context.Background()

	if err := uc.Grant(ctx, "u1", "editor"); err != nil {
		t.Fatal(err)
	}
	if err := uc.Grant(ctx, "u1", "editor"); err != nil {
		t.Fatal(err) // 幂等：重复授予不报错
	}
	if err := uc.Grant(ctx, "ghost", "editor"); !errors.Is(err, access.ErrUnknownUser) {
		t.Errorf("unknown user: err = %v, want ErrUnknownUser", err)
	}
	if err := uc.Grant(ctx, "u1", "nope"); !errors.Is(err, access.ErrUnknownRole) {
		t.Errorf("unknown role: err = %v, want ErrUnknownRole", err)
	}
	if got := pub.names(); len(got) != 1 || got[0] != "account.assignment.granted" {
		t.Errorf("events = %v, want exactly one granted（幂等不重复发布）", got)
	}

	if err := uc.Revoke(ctx, "u1", "editor"); err != nil {
		t.Fatal(err)
	}
	if err := uc.Revoke(ctx, "u1", "editor"); err != nil {
		t.Fatal(err) // 幂等回收
	}
	if got := pub.names(); len(got) != 2 || got[1] != "account.assignment.revoked" {
		t.Errorf("events = %v, want granted+revoked", got)
	}
}

func TestPermissionsForUser(t *testing.T) {
	uc, _, _ := newTestUseCase(t)
	ctx := context.Background()

	perms, err := uc.PermissionsForUser(ctx, "u2")
	if err != nil || len(perms) != 0 {
		t.Fatalf("no assignment: perms=%v err=%v, want empty（含空 claims 语义）", perms, err)
	}
	if err := uc.Grant(ctx, "u2", "admin"); err != nil {
		t.Fatal(err)
	}
	if err := uc.Grant(ctx, "u2", "editor"); err != nil {
		t.Fatal(err)
	}
	perms, err = uc.PermissionsForUser(ctx, "u2")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"account.assignment.write", "audit.read", "demo.item.write"}
	if len(perms) != len(want) {
		t.Fatalf("perms = %v, want %v", perms, want)
	}
	for i := range want {
		if perms[i] != want[i] {
			t.Errorf("perms[%d] = %q, want %q（并集+排序稳定）", i, perms[i], want[i])
		}
	}
}

func TestBootstrapAdmins(t *testing.T) {
	uc := New(newFakeRepo(), &fakeUserRepo{users: map[string]*identity.User{
		"u1": {ID: "u1", Email: "a@example.com", Status: identity.StatusActive},
	}}, fakeTx{}, &fakePublisher{}, testRoles, nil)

	var skipped []string
	err := uc.BootstrapAdmins(context.Background(), []string{"A@Example.com", "ghost@example.com"}, func(email string) {
		skipped = append(skipped, email)
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(skipped) != 1 || skipped[0] != "ghost@example.com" {
		t.Errorf("skipped = %v, want ghost only", skipped)
	}
	roles, err := uc.RolesForUser(context.Background(), "u1")
	if err != nil || len(roles) != 1 || roles[0] != "admin" {
		t.Errorf("bootstrap roles = %v err=%v, want [admin]（邮箱大小写归一）", roles, err)
	}
	// 幂等：再跑一遍不报错不重复。
	if err := uc.BootstrapAdmins(context.Background(), []string{"a@example.com"}, nil); err != nil {
		t.Fatal(err)
	}
}
