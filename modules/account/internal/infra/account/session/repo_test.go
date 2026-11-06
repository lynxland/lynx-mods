package session

import (
	"context"
	"testing"
	"time"

	"github.com/lynxland/appkit/contrib/bunx/testutil"
	"github.com/lynxland/lynx-start/modules/account/domain/account/session"
	"github.com/lynxland/lynx-start/modules/account/internal/infra/account/migrations"
)

var t0 = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

func TestRepo(t *testing.T) {
	testutil.SkipUnlessPg(t)
	db := testutil.NewDB(t, testutil.Chain{Module: "account", FS: migrations.FS})
	repo := NewRepository(db)
	ctx := context.Background()

	mk := func(id, jti string) *session.Session {
		s, err := session.NewSession(id, "u1", jti, t0, t0.Add(time.Hour))
		if err != nil {
			t.Fatal(err)
		}
		return s
	}

	if err := repo.Create(ctx, mk("s1", "jti-1")); err != nil {
		t.Fatal(err)
	}
	if err := repo.Create(ctx, mk("s2", "jti-2")); err != nil {
		t.Fatal(err)
	}

	t.Run("get", func(t *testing.T) {
		got, err := repo.Get(ctx, "s1")
		if err != nil {
			t.Fatal(err)
		}
		if got.RefreshJTI != "jti-1" || got.Status != session.StatusActive {
			t.Errorf("got = %+v", got)
		}
		if _, err := repo.Get(ctx, "ghost"); err != session.ErrSessionNotFound {
			t.Errorf("ghost: err = %v, want ErrSessionNotFound", err)
		}
	})

	t.Run("rotate whitelist", func(t *testing.T) {
		s, _ := repo.Get(ctx, "s1")
		if err := s.Rotate("jti-1b", t0.Add(time.Minute)); err != nil {
			t.Fatal(err)
		}
		if err := repo.Rotate(ctx, s); err != nil {
			t.Fatal(err)
		}
		got, _ := repo.Get(ctx, "s1")
		if got.RefreshJTI != "jti-1b" {
			t.Errorf("jti = %q, want jti-1b", got.RefreshJTI)
		}
	})

	t.Run("revoke by user counts active only", func(t *testing.T) {
		if err := repo.Revoke(ctx, "s1", session.RevokeLogout, t0.Add(2*time.Minute)); err != nil {
			t.Fatal(err)
		}
		n, err := repo.RevokeByUser(ctx, "u1", session.RevokePassword, t0.Add(3*time.Minute))
		if err != nil || n != 1 {
			t.Fatalf("revoke by user: n=%d err=%v, want 1（s1 已吊销不再计数）", n, err)
		}
		got, _ := repo.Get(ctx, "s2")
		if got.Status != session.StatusRevoked || got.RevokeReason != session.RevokePassword {
			t.Errorf("s2 = %+v", got)
		}
		// 幂等：再次按用户吊销计数为 0。
		if n, _ := repo.RevokeByUser(ctx, "u1", session.RevokePassword, t0.Add(4*time.Minute)); n != 0 {
			t.Errorf("second pass n=%d, want 0", n)
		}
		// 已吊销会话轮换被拒。
		s2, _ := repo.Get(ctx, "s2")
		if err := repo.Rotate(ctx, s2); err != session.ErrSessionRevoked {
			t.Errorf("rotate revoked: err = %v, want ErrSessionRevoked", err)
		}
	})
}
