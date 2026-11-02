package identity

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/lynxland/lynx-mods/modules/identity/domain/identity"
	appidentity "github.com/lynxland/lynx-mods/modules/identity/internal/app/identity"

	"github.com/lynxland/appkit/clock"
	"github.com/lynxland/appkit/events"
	"github.com/lynxland/appkit/jwt"
	"github.com/lynxland/grpcapi/authn"
)

// miniRepo 是认证器测试的最小仓储桩。
type miniRepo struct {
	mu    sync.Mutex
	users map[string]*identity.User
}

func (r *miniRepo) Create(_ context.Context, u *identity.User) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.users[u.ID] = u
	return nil
}

func (r *miniRepo) GetByID(_ context.Context, id string) (*identity.User, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if u, ok := r.users[id]; ok {
		return u, nil
	}
	return nil, identity.ErrUserNotFound
}

func (r *miniRepo) GetByEmail(_ context.Context, _ string) (*identity.User, error) {
	return nil, identity.ErrUserNotFound
}

func (r *miniRepo) UpdatePassword(_ context.Context, _ *identity.User) error { return nil }

type miniTx struct{}

func (miniTx) Run(_ context.Context, fn func(ctx context.Context) error) error {
	return fn(context.Background())
}

type miniPub struct{}

func (miniPub) Publish(context.Context, events.Envelope) error { return nil }

func newAuthn(t *testing.T) (*BearerAuthenticator, *jwt.Signer, *clock.Fake) {
	t.Helper()
	fake := clock.NewFake(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	signer := jwt.NewSigner("m", appidentity.PurposeAccessToken, "iss", 15*time.Minute, fake)
	verifier := jwt.NewVerifier("m", appidentity.PurposeAccessToken, "iss")
	uc := appidentity.New(&miniRepo{users: map[string]*identity.User{
		"u1": {ID: "u1", Email: "a@example.com", DisplayName: "A", Status: identity.StatusActive},
	}}, miniTx{}, miniPub{}, signer, verifier, fake)
	return NewBearerAuthenticator(uc, verifier, fake), signer, fake
}

// TestBearerAuthenticatorChainSemantics 链式语义护栏：非本模块令牌必须
// (nil,nil) 交链（与 session 模块令牌共存的前提），本模块令牌失效才拒绝。
func TestBearerAuthenticatorChainSemantics(t *testing.T) {
	a, signer, fake := newAuthn(t)
	ctx := context.Background()

	t.Run("own token resolves principal", func(t *testing.T) {
		tok, err := signer.Sign("u1", map[string]string{"email": "a@example.com"})
		if err != nil {
			t.Fatal(err)
		}
		p, err := a.Authenticate(ctx, authn.Credentials{Bearer: []string{tok.Value}})
		if err != nil || p == nil {
			t.Fatalf("p=%v err=%v", p, err)
		}
		if p.ID != "u1" || p.Kind != "user" || p.Attrs["email"] != "a@example.com" {
			t.Errorf("principal = %+v", p)
		}
	})

	t.Run("foreign token passes chain", func(t *testing.T) {
		other := jwt.NewSigner("m", "other-purpose", "iss", time.Minute, fake)
		foreign, _ := other.Sign("u1", nil)
		p, err := a.Authenticate(ctx, authn.Credentials{Bearer: []string{foreign.Value}})
		if p != nil || err != nil {
			t.Errorf("foreign token: p=%v err=%v, want (nil,nil) 继续链", p, err)
		}
	})

	t.Run("garbage passes chain", func(t *testing.T) {
		p, err := a.Authenticate(ctx, authn.Credentials{Bearer: []string{"garbage"}})
		if p != nil || err != nil {
			t.Errorf("garbage: p=%v err=%v, want (nil,nil)", p, err)
		}
	})

	t.Run("expired own token rejects", func(t *testing.T) {
		tok, _ := signer.Sign("u1", nil)
		fake.Advance(16 * time.Minute)
		p, err := a.Authenticate(ctx, authn.Credentials{Bearer: []string{tok.Value}})
		if p != nil || err == nil {
			t.Errorf("expired: p=%v err=%v, want error", p, err)
		}
	})

	t.Run("non-bearer credentials pass chain", func(t *testing.T) {
		p, err := a.Authenticate(ctx, authn.Credentials{APIKeys: []string{"k"}})
		if p != nil || err != nil {
			t.Errorf("api key: p=%v err=%v, want (nil,nil)", p, err)
		}
	})
}
