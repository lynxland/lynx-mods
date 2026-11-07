package session

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/lynxland/appkit/clock"
	"github.com/lynxland/appkit/contrib/bunx/testutil"
	"github.com/lynxland/appkit/crud"
	"github.com/lynxland/appkit/jwt"
	"github.com/lynxland/lynx-start/modules/account/domain/account/identity"
	"github.com/lynxland/lynx-start/modules/account/domain/account/session"
)

// fakeSessionRepo 是会话仓储端口的内存实现。
type fakeSessionRepo struct {
	mu   sync.Mutex
	rows map[string]*session.Session
}

func newFakeSessionRepo() *fakeSessionRepo {
	return &fakeSessionRepo{rows: map[string]*session.Session{}}
}

func (r *fakeSessionRepo) Create(_ context.Context, s *session.Session) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	c := *s
	r.rows[s.ID] = &c
	return nil
}

func (r *fakeSessionRepo) Get(_ context.Context, id string) (*session.Session, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if s, ok := r.rows[id]; ok {
		c := *s
		return &c, nil
	}
	return nil, session.ErrSessionNotFound
}

func (r *fakeSessionRepo) Rotate(_ context.Context, s *session.Session, _ string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	cur, ok := r.rows[s.ID]
	if !ok || cur.Status != session.StatusActive {
		return session.ErrSessionRevoked
	}
	cur.RefreshJTI = s.RefreshJTI
	cur.LastRefreshedAt = s.LastRefreshedAt
	return nil
}

func (r *fakeSessionRepo) Revoke(_ context.Context, id, reason string, at time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if s, ok := r.rows[id]; ok && s.Status == session.StatusActive {
		s.Status = session.StatusRevoked
		s.RevokedAt = at
		s.RevokeReason = reason
	}
	return nil
}

func (r *fakeSessionRepo) RevokeByUser(_ context.Context, userID, reason string, at time.Time) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := 0
	for _, s := range r.rows {
		if s.UserID == userID && s.Status == session.StatusActive {
			s.Status = session.StatusRevoked
			s.RevokedAt = at
			s.RevokeReason = reason
			n++
		}
	}
	return n, nil
}

// fakeUserRepo 是 identity 仓储端口的内存实现（DependsOn 消费面）。
type fakeUserRepo struct {
	mu    sync.Mutex
	users map[string]*identity.User
}

func (r *fakeUserRepo) Create(_ context.Context, u *identity.User) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.users[u.ID] = u
	return nil
}

func (r *fakeUserRepo) GetByID(_ context.Context, id string) (*identity.User, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if u, ok := r.users[id]; ok {
		return u, nil
	}
	return nil, identity.ErrUserNotFound
}

func (r *fakeUserRepo) GetByEmail(_ context.Context, email string) (*identity.User, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
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

const (
	testMaster  = "session-test-master"
	testIssuer  = "test-issuer"
	testAccess  = 15 * time.Minute
	testRefresh = time.Hour
)

func newTestUseCase(t *testing.T) (*UseCase, *fakeSessionRepo, *fakeUserRepo, *testutil.CapturePublisher, *clock.Fake) {
	t.Helper()
	fake := clock.NewFake(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	tokens := Tokens{
		Access:        jwt.NewSigner(testMaster, PurposeAccessToken, testIssuer, testAccess, fake),
		Refresh:       jwt.NewSigner(testMaster, PurposeRefreshToken, testIssuer, testRefresh, fake),
		AccessVerify:  jwt.NewVerifier(testMaster, PurposeAccessToken, testIssuer),
		RefreshVerify: jwt.NewVerifier(testMaster, PurposeRefreshToken, testIssuer),
		RefreshTTL:    testRefresh,
	}
	srepo := newFakeSessionRepo()
	urepo := &fakeUserRepo{users: map[string]*identity.User{}}
	pub := testutil.NewCapturePublisher()
	return New(srepo, urepo, testutil.InlineRunner{}, pub, tokens, fake), srepo, urepo, pub, fake
}

func seedUser(t *testing.T, urepo *fakeUserRepo, id, email string) {
	t.Helper()
	u, err := identity.NewUser(id, email, "", "hash", time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	_ = urepo.Create(context.Background(), u)
}

func TestCreateSession(t *testing.T) {
	uc, srepo, urepo, pub, _ := newTestUseCase(t)
	ctx := context.Background()
	seedUser(t, urepo, "u1", "alice@example.com")

	pair, err := uc.CreateSession(ctx, "u1", "alice@example.com", "ua-test")
	if err != nil {
		t.Fatal(err)
	}
	if pair.Session.UserID != "u1" || pair.Access.Value == "" || pair.Refresh.Value == "" {
		t.Fatalf("pair = %+v", pair)
	}
	if got := pub.Names(); len(got) != 1 || got[0] != "account.session.created" {
		t.Errorf("events = %v", got)
	}
	if _, err := srepo.Get(ctx, pair.Session.ID); err != nil {
		t.Error("session not persisted")
	}
}

func TestRefreshRotationAndReuse(t *testing.T) {
	uc, _, urepo, _, _ := newTestUseCase(t)
	ctx := context.Background()
	seedUser(t, urepo, "u1", "alice@example.com")

	pair, err := uc.CreateSession(ctx, "u1", "alice@example.com", "ua-test")
	if err != nil {
		t.Fatal(err)
	}
	oldRefresh := pair.Refresh.Value

	p2, err := uc.Refresh(ctx, oldRefresh)
	if err != nil {
		t.Fatal(err)
	}
	if p2.Refresh.Value == oldRefresh || p2.Access.Value == "" {
		t.Error("refresh must rotate refresh token and reissue access")
	}

	// 旧刷新令牌重放：会话被吊销（盗用信号）。
	if _, err := uc.Refresh(ctx, oldRefresh); !errors.Is(err, session.ErrRefreshRejected) {
		t.Errorf("replay: err = %v, want ErrRefreshRejected", err)
	}
	if _, err := uc.ResolveSession(ctx, pair.Session.ID); !errors.Is(err, session.ErrSessionRevoked) {
		t.Errorf("after reuse: err = %v, want ErrSessionRevoked（整个会话吊销）", err)
	}
	// 新刷新令牌也随之失效。
	if _, err := uc.Refresh(ctx, p2.Refresh.Value); !errors.Is(err, session.ErrSessionRevoked) {
		t.Errorf("post-revoke refresh: err = %v, want ErrSessionRevoked", err)
	}
}

func TestRefreshExpiry(t *testing.T) {
	uc, _, urepo, _, fake := newTestUseCase(t)
	ctx := context.Background()
	seedUser(t, urepo, "u1", "alice@example.com")
	pair, err := uc.CreateSession(ctx, "u1", "alice@example.com", "ua-test")
	if err != nil {
		t.Fatal(err)
	}
	fake.Advance(testRefresh + time.Minute)
	if _, err := uc.Refresh(ctx, pair.Refresh.Value); !errors.Is(err, session.ErrSessionExpired) {
		t.Errorf("expired refresh: err = %v, want ErrSessionExpired", err)
	}
}

func TestResolveSessionAndLogout(t *testing.T) {
	uc, _, urepo, _, _ := newTestUseCase(t)
	ctx := context.Background()
	seedUser(t, urepo, "u1", "alice@example.com")
	pair, err := uc.CreateSession(ctx, "u1", "alice@example.com", "ua-test")
	if err != nil {
		t.Fatal(err)
	}

	s, err := uc.ResolveSession(ctx, pair.Session.ID)
	if err != nil || s.ID != pair.Session.ID {
		t.Fatalf("resolve: s=%v err=%v", s, err)
	}

	if err := uc.Logout(ctx, pair.Session.ID); err != nil {
		t.Fatal(err)
	}
	if err := uc.Logout(ctx, pair.Session.ID); err != nil {
		t.Errorf("logout must be idempotent: %v", err)
	}
	if _, err := uc.ResolveSession(ctx, pair.Session.ID); !errors.Is(err, session.ErrSessionRevoked) {
		t.Errorf("after logout: err = %v, want ErrSessionRevoked", err)
	}
}

func TestLogoutAllAndPasswordChanged(t *testing.T) {
	uc, _, urepo, pub, _ := newTestUseCase(t)
	ctx := context.Background()
	seedUser(t, urepo, "u1", "alice@example.com")
	a, err := uc.CreateSession(ctx, "u1", "alice@example.com", "ua-test")
	if err != nil {
		t.Fatal(err)
	}
	b, err := uc.CreateSession(ctx, "u1", "alice@example.com", "ua-test")
	if err != nil {
		t.Fatal(err)
	}

	n, err := uc.LogoutAll(ctx, "u1")
	if err != nil || n != 2 {
		t.Fatalf("logout all: n=%d err=%v, want 2", n, err)
	}

	// 改密联动走 OnPasswordChanged（事件订阅器的入口）。
	c, err := uc.CreateSession(ctx, "u1", "alice@example.com", "ua-test")
	if err != nil {
		t.Fatal(err)
	}
	_ = a
	_ = b
	if err := uc.OnPasswordChanged(ctx, "u1"); err != nil {
		t.Fatal(err)
	}
	if _, err := uc.ResolveSession(ctx, c.Session.ID); !errors.Is(err, session.ErrSessionRevoked) {
		t.Errorf("after password change: err = %v, want ErrSessionRevoked", err)
	}
	last := pub.Names()[len(pub.Names())-1]
	if last != "account.session.revoked" {
		t.Errorf("last event = %q, want session.session.revoked", last)
	}
}
