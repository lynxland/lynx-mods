package identity

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/lynxland/appkit/clock"
	"github.com/lynxland/appkit/contrib/bunx/testutil"
	"github.com/lynxland/appkit/crud"
	"github.com/lynxland/appkit/jwt"
	"github.com/lynxland/appkit/ratelimit"
	"github.com/lynxland/lynx-start/modules/account/domain/account/identity"
)

// fakeRepo 是仓储端口的内存实现（返回深拷贝防用例层误改仓储状态）。
type fakeRepo struct {
	mu    sync.Mutex
	users map[string]*identity.User
}

func newFakeRepo() *fakeRepo { return &fakeRepo{users: map[string]*identity.User{}} }

func (r *fakeRepo) Create(_ context.Context, user *identity.User) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, u := range r.users {
		if u.Email == user.Email {
			return identity.ErrEmailTaken
		}
	}
	r.users[user.ID] = cloneUser(user)
	return nil
}

func (r *fakeRepo) GetByID(_ context.Context, id string) (*identity.User, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	u, ok := r.users[id]
	if !ok {
		return nil, identity.ErrUserNotFound
	}
	return cloneUser(u), nil
}

func (r *fakeRepo) GetByEmail(_ context.Context, email string) (*identity.User, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, u := range r.users {
		if u.Email == identity.NormalizeEmail(email) {
			return cloneUser(u), nil
		}
	}
	return nil, identity.ErrUserNotFound
}

func (r *fakeRepo) UpdateStatus(context.Context, string, string, time.Time) error { return nil }
func (r *fakeRepo) UpdateEmailVerified(_ context.Context, id string, v bool, _ time.Time) error {
	if u, ok := r.users[id]; ok {
		u.EmailVerified = v
	}
	return nil
}
func (r *fakeRepo) List(context.Context, crud.PagePlan) ([]*identity.User, int, error) {
	return nil, 0, nil
}
func (r *fakeRepo) UpdatePassword(_ context.Context, user *identity.User) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	cur, ok := r.users[user.ID]
	if !ok {
		return identity.ErrUserNotFound
	}
	cur.PasswordHash = user.PasswordHash
	cur.UpdatedAt = user.UpdatedAt
	return nil
}

// delete/remove 模拟「令牌指向的用户已不存在」。
func (r *fakeRepo) delete(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.users, id)
}

// disable 模拟停用账号。
func (r *fakeRepo) disable(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.users[id].Status = identity.StatusDisabled
}

func cloneUser(u *identity.User) *identity.User {
	c := *u
	return &c
}

const (
	testMaster = "test-master-secret"
	testIssuer = "test-issuer"
)

func newTestUseCase(t *testing.T) (*UseCase, *fakeRepo, *testutil.CapturePublisher, *clock.Fake) {
	t.Helper()
	repo := newFakeRepo()
	pub := testutil.NewCapturePublisher()
	fake := clock.NewFake(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	signer := jwt.NewSigner(testMaster, PurposeAccessToken, testIssuer, 30*time.Minute, fake)
	verifier := jwt.NewVerifier(testMaster, PurposeAccessToken, testIssuer)
	return New(repo, testutil.InlineRunner{}, pub, signer, verifier, signer, verifier, nil, fake), repo, pub, fake
}

func TestRegister(t *testing.T) {
	uc, _, pub, fake := newTestUseCase(t)
	ctx := context.Background()

	user, token, err := uc.Register(ctx, "  Alice@Example.com ", "correct-horse-battery", "Alice")
	if err != nil {
		t.Fatal(err)
	}
	if user.Email != "alice@example.com" {
		t.Errorf("email = %q, want normalized", user.Email)
	}
	// 注册即登录：令牌立即可用。
	auth, err := uc.Authenticate(ctx, token.Value)
	if err != nil {
		t.Fatal(err)
	}
	if auth.ID != user.ID {
		t.Errorf("authenticate id = %q, want %q", auth.ID, user.ID)
	}
	claims, err := uc.verifier.Verify(token.Value, fake.Now())
	if err != nil {
		t.Fatal(err)
	}
	if claims.Custom["email"] != "alice@example.com" {
		t.Errorf("custom email claim = %q", claims.Custom["email"])
	}
	if got := pub.Names(); len(got) != 1 || got[0] != "account.user.created" {
		t.Errorf("events = %v, want [identity.user.created]", got)
	}
}

func TestRegisterValidation(t *testing.T) {
	uc, _, _, _ := newTestUseCase(t)
	ctx := context.Background()

	if _, _, err := uc.Register(ctx, "not-an-email", "correct-horse-battery", ""); !errors.Is(err, identity.ErrInvalidEmail) {
		t.Errorf("invalid email: err = %v", err)
	}
	if _, _, err := uc.Register(ctx, "a@example.com", "short", ""); !errors.Is(err, identity.ErrWeakPassword) {
		t.Errorf("weak password: err = %v", err)
	}
	if _, _, err := uc.Register(ctx, "a@example.com", "correct-horse-battery", ""); err != nil {
		t.Fatal(err)
	}
	if _, _, err := uc.Register(ctx, "A@example.com", "correct-horse-battery", ""); !errors.Is(err, identity.ErrEmailTaken) {
		t.Errorf("duplicate email (大小写归一): err = %v", err)
	}
}

func TestLogin(t *testing.T) {
	uc, repo, _, _ := newTestUseCase(t)
	ctx := context.Background()
	if _, _, err := uc.Register(ctx, "bob@example.com", "correct-horse-battery", ""); err != nil {
		t.Fatal(err)
	}

	user, token, err := uc.Login(ctx, "BOB@example.com", "correct-horse-battery")
	if err != nil {
		t.Fatal(err)
	}
	if token.Value == "" || user.ID == "" {
		t.Fatal("login: empty token or user")
	}

	if _, _, err := uc.Login(ctx, "bob@example.com", "wrong-password"); !errors.Is(err, identity.ErrInvalidCredentials) {
		t.Errorf("wrong password: err = %v", err)
	}
	if _, _, err := uc.Login(ctx, "ghost@example.com", "whatever-pass"); !errors.Is(err, identity.ErrInvalidCredentials) {
		t.Errorf("unknown email: err = %v, want same sentinel as wrong password", err)
	}

	repo.disable(user.ID)
	if _, _, err := uc.Login(ctx, "bob@example.com", "correct-horse-battery"); !errors.Is(err, identity.ErrUserDisabled) {
		t.Errorf("disabled: err = %v", err)
	}
}

func TestAuthenticate(t *testing.T) {
	uc, repo, _, fake := newTestUseCase(t)
	ctx := context.Background()
	user, token, err := uc.Register(ctx, "carol@example.com", "correct-horse-battery", "")
	if err != nil {
		t.Fatal(err)
	}

	t.Run("expired", func(t *testing.T) {
		fake.Advance(31 * time.Minute)
		if _, err := uc.Authenticate(ctx, token.Value); !errors.Is(err, identity.ErrTokenExpired) {
			t.Errorf("err = %v, want ErrTokenExpired", err)
		}
		fake.Set(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	})

	t.Run("foreign purpose", func(t *testing.T) {
		other := jwt.NewSigner(testMaster, "other-purpose", testIssuer, time.Minute, fake)
		foreign, err := other.Sign(user.ID, nil)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := uc.Authenticate(ctx, foreign.Value); !errors.Is(err, identity.ErrTokenInvalid) {
			t.Errorf("err = %v, want ErrTokenInvalid (purpose 隔离)", err)
		}
	})

	t.Run("garbage", func(t *testing.T) {
		if _, err := uc.Authenticate(ctx, "not-a-jwt"); !errors.Is(err, identity.ErrTokenInvalid) {
			t.Errorf("err = %v, want ErrTokenInvalid", err)
		}
	})

	t.Run("user deleted", func(t *testing.T) {
		repo.delete(user.ID)
		if _, err := uc.Authenticate(ctx, token.Value); !errors.Is(err, identity.ErrTokenInvalid) {
			t.Errorf("err = %v, want ErrTokenInvalid (fail-closed 回读)", err)
		}
	})
}

func TestChangePassword(t *testing.T) {
	uc, _, pub, _ := newTestUseCase(t)
	ctx := context.Background()
	user, _, err := uc.Register(ctx, "dave@example.com", "correct-horse-battery", "")
	if err != nil {
		t.Fatal(err)
	}

	if err := uc.ChangePassword(ctx, user.ID, "wrong-old-pass", "new-correct-horse"); !errors.Is(err, identity.ErrInvalidCredentials) {
		t.Errorf("wrong old: err = %v", err)
	}
	if err := uc.ChangePassword(ctx, user.ID, "correct-horse-battery", "short"); !errors.Is(err, identity.ErrWeakPassword) {
		t.Errorf("weak new: err = %v", err)
	}
	if err := uc.ChangePassword(ctx, user.ID, "correct-horse-battery", "new-correct-horse"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := uc.Login(ctx, "dave@example.com", "correct-horse-battery"); !errors.Is(err, identity.ErrInvalidCredentials) {
		t.Errorf("old password after change: err = %v, want ErrInvalidCredentials", err)
	}
	if _, _, err := uc.Login(ctx, "dave@example.com", "new-correct-horse"); err != nil {
		t.Errorf("new password login: %v", err)
	}
	if got := pub.Names(); len(got) != 2 || got[1] != "account.user.password-changed" {
		t.Errorf("events = %v, want identity.user.password-changed as second", got)
	}
}

// TestLoginRateLimit 锁定语义：阈值内失败→仍可尝试；达阈值→拒绝（与
// 密码错误同响应——锁定态不可探测）；窗口滑动（fake clock）后解锁。
func TestLoginRateLimit(t *testing.T) {
	uc, repo, _, clk := newTestUseCase(t)
	uc.loginFails = ratelimit.NewSlidingWindow(3, 10*time.Minute)
	if _, _, err := uc.Register(context.Background(), "rl@example.com", "password123", "RL"); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if _, _, err := uc.Login(context.Background(), "rl@example.com", "wrong-password"); err != identity.ErrInvalidCredentials {
			t.Fatalf("fail %d: err = %v", i, err)
		}
	}
	// 第 4 次：正确密码也被拒（锁定）。
	if _, _, err := uc.Login(context.Background(), "rl@example.com", "password123"); err != identity.ErrInvalidCredentials {
		t.Fatalf("locked login err = %v, want ErrInvalidCredentials", err)
	}
	// 窗口滑出（失败记录退出）→ 解锁。
	clk.Advance(11 * time.Minute)
	if _, _, err := uc.Login(context.Background(), "rl@example.com", "password123"); err != nil {
		t.Fatalf("unlocked login err = %v", err)
	}
	_ = repo
}

// TestPasswordResetEnumerationSafety 枚举防护：不存在邮箱与存在邮箱的
// 响应完全一致（都成功、无差异）。
func TestPasswordResetEnumerationSafety(t *testing.T) {
	uc, _, pub, _ := newTestUseCase(t)
	if err := uc.RequestPasswordReset(context.Background(), "ghost@example.com"); err != nil {
		t.Fatalf("ghost reset err = %v, want nil", err)
	}
	// ghost 不产生事件（不泄露存在性）。
	for _, e := range pub.Envelopes() {
		if e.Name == identity.EventUserResetRequested {
			t.Fatal("ghost reset published event (enumeration leak)")
		}
	}
	if _, _, err := uc.Register(context.Background(), "real@example.com", "password123", "R"); err != nil {
		t.Fatal(err)
	}
	if err := uc.RequestPasswordReset(context.Background(), "real@example.com"); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range pub.Envelopes() {
		if e.Name == identity.EventUserResetRequested {
			found = true
		}
	}
	if !found {
		t.Fatal("real reset did not publish event")
	}
}

// TestPasswordResetFlow 全流：请求→确认（令牌改密）→旧密码失效+会话
// 吊销事件发出。
func TestPasswordResetFlow(t *testing.T) {
	uc, _, pub, _ := newTestUseCase(t)
	if _, _, err := uc.Register(context.Background(), "reset@example.com", "password123", "R"); err != nil {
		t.Fatal(err)
	}
	if err := uc.RequestPasswordReset(context.Background(), "reset@example.com"); err != nil {
		t.Fatal(err)
	}
	// 从事件载荷截取令牌（邮件通道在 e2e 验证）。
	var token string
	for _, e := range pub.Envelopes() {
		var payload struct {
			Token string `json:"token"`
		}
		_ = json.Unmarshal(e.Payload, &payload)
		if payload.Token != "" {
			token = payload.Token
		}
	}
	if token == "" {
		t.Fatal("reset token not published")
	}
	if err := uc.ConfirmPasswordReset(context.Background(), token, "new-password-456"); err != nil {
		t.Fatalf("confirm reset: %v", err)
	}
	// 旧密码失效。
	if _, _, err := uc.Login(context.Background(), "reset@example.com", "password123"); err != identity.ErrInvalidCredentials {
		t.Fatalf("old password err = %v, want ErrInvalidCredentials", err)
	}
	// 新密码可登录。
	if _, _, err := uc.Login(context.Background(), "reset@example.com", "new-password-456"); err != nil {
		t.Fatalf("new password login: %v", err)
	}
	// 改密事件已发（session 联动的驱动源）。
	sawChanged := false
	for _, e := range pub.Envelopes() {
		if e.Name == identity.EventUserPasswordChanged {
			sawChanged = true
		}
	}
	if !sawChanged {
		t.Fatal("password-changed event missing after reset")
	}
}
