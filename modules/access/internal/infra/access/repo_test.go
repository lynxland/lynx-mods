package access

import (
	"context"
	"testing"
	"time"

	"github.com/lynx-go/lynx-mods/modules/access/domain/access"
	"github.com/lynx-go/lynx-mods/modules/access/internal/infra/access/migrations"
	"github.com/lynxland/appkit/contrib/bunx/testutil"
)

func TestRepo(t *testing.T) {
	testutil.SkipUnlessPg(t)
	db := testutil.NewDB(t, testutil.Chain{Module: "access", FS: migrations.FS})
	repo := NewRepository(db)
	reader := NewUserReader(db)
	ctx := context.Background()

	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	mk := func(user, role string) *access.Assignment {
		a, err := access.NewAssignment(user, role, t0)
		if err != nil {
			t.Fatal(err)
		}
		return a
	}

	t.Run("assign idempotent", func(t *testing.T) {
		created, err := repo.Assign(ctx, mk("u1", "admin"))
		if err != nil || !created {
			t.Fatalf("first assign: created=%v err=%v", created, err)
		}
		created, err = repo.Assign(ctx, mk("u1", "admin"))
		if err != nil || created {
			t.Fatalf("second assign: created=%v err=%v, want false（幂等）", created, err)
		}
	})

	t.Run("roles for user", func(t *testing.T) {
		if _, err := repo.Assign(ctx, mk("u1", "editor")); err != nil {
			t.Fatal(err)
		}
		roles, err := repo.RolesForUser(ctx, "u1")
		if err != nil {
			t.Fatal(err)
		}
		if len(roles) != 2 {
			t.Errorf("roles = %v, want 2", roles)
		}
		if roles, _ := repo.RolesForUser(ctx, "ghost"); len(roles) != 0 {
			t.Errorf("ghost roles = %v, want empty", roles)
		}
	})

	t.Run("revoke idempotent", func(t *testing.T) {
		removed, err := repo.Revoke(ctx, "u1", "editor")
		if err != nil || !removed {
			t.Fatalf("revoke: removed=%v err=%v", removed, err)
		}
		if removed, _ := repo.Revoke(ctx, "u1", "editor"); removed {
			t.Error("second revoke: removed=true, want false")
		}
	})

	t.Run("user reader read-only", func(t *testing.T) {
		// identity_users 属于 identity 迁移链——本链不含该表，读取
		// 按运行时集成验证（应用侧两模块同库）；此处只验证写入拒绝。
		if err := reader.Create(ctx, nil); err == nil {
			t.Error("Create must be rejected (read-only adapter)")
		}
		if err := reader.UpdatePassword(ctx, nil); err == nil {
			t.Error("UpdatePassword must be rejected (read-only adapter)")
		}
	})
}
