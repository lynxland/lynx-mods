package identity

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/lynxland/appkit/clock"
	"github.com/lynxland/appkit/contrib/bunx/testutil"
	"github.com/lynxland/appkit/jwt"
	"github.com/lynxland/lynx-start/modules/identity/domain/identity"
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
	return New(repo, testutil.InlineRunner{}, pub, signer, verifier, fake), repo, pub, fake
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
	if got := pub.Names(); len(got) != 1 || got[0] != "identity.user.created" {
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
	if got := pub.Names(); len(got) != 2 || got[1] != "identity.user.password-changed" {
		t.Errorf("events = %v, want identity.user.password-changed as second", got)
	}
}
