package identity

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/lynxland/appkit/contrib/bunx/testutil"
	"github.com/lynxland/lynx-start/modules/account/domain/account/identity"
	"github.com/lynxland/lynx-start/modules/account/internal/infra/account/migrations"
)

func newTestUser(t *testing.T, email string) *identity.User {
	t.Helper()
	u, err := identity.NewUser("u-"+email, email, "", "hash", time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func TestRepo(t *testing.T) {
	dsn := testutil.SkipUnlessPg(t)
	_ = dsn
	db := testutil.NewDB(t, testutil.Chain{Module: "account", FS: migrations.FS})
	repo := NewRepository(db)
	ctx := context.Background()

	u := newTestUser(t, "alice@example.com")
	if err := repo.Create(ctx, u); err != nil {
		t.Fatal(err)
	}

	t.Run("get by id/email", func(t *testing.T) {
		got, err := repo.GetByID(ctx, u.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got.Email != u.Email || got.Status != identity.StatusActive {
			t.Errorf("got = %+v", got)
		}
		byEmail, err := repo.GetByEmail(ctx, u.Email)
		if err != nil {
			t.Fatal(err)
		}
		if byEmail.ID != u.ID {
			t.Errorf("by email id = %q, want %q", byEmail.ID, u.ID)
		}
	})

	t.Run("duplicate email maps sentinel", func(t *testing.T) {
		dup := newTestUser(t, "alice@example.com")
		dup.ID = "another-id"
		if err := repo.Create(ctx, dup); !errors.Is(err, identity.ErrEmailTaken) {
			t.Errorf("err = %v, want ErrEmailTaken", err)
		}
	})

	t.Run("missing maps sentinel", func(t *testing.T) {
		if _, err := repo.GetByID(ctx, "ghost"); !errors.Is(err, identity.ErrUserNotFound) {
			t.Errorf("by id: err = %v, want ErrUserNotFound", err)
		}
		if _, err := repo.GetByEmail(ctx, "ghost@example.com"); !errors.Is(err, identity.ErrUserNotFound) {
			t.Errorf("by email: err = %v, want ErrUserNotFound", err)
		}
	})

	t.Run("update password whitelist", func(t *testing.T) {
		u.PasswordHash = "new-hash"
		u.DisplayName = "Should Not Persist"
		u.UpdatedAt = u.UpdatedAt.Add(time.Minute)
		if err := repo.UpdatePassword(ctx, u); err != nil {
			t.Fatal(err)
		}
		got, err := repo.GetByID(ctx, u.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got.PasswordHash != "new-hash" {
			t.Errorf("hash = %q, want new-hash", got.PasswordHash)
		}
		if got.DisplayName == "Should Not Persist" {
			t.Error("display_name 被越权写入：白名单列护栏失效")
		}
		if err := repo.UpdatePassword(ctx, &identity.User{ID: "ghost"}); !errors.Is(err, identity.ErrUserNotFound) {
			t.Errorf("ghost update: err = %v, want ErrUserNotFound", err)
		}
	})
}
