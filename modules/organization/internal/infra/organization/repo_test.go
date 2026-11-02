package organization

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/lynx-go/lynx-mods/modules/organization/domain/organization"
	"github.com/lynx-go/lynx-mods/modules/organization/internal/infra/organization/migrations"
	"github.com/lynxland/appkit/contrib/bunx/testutil"
)

func TestRepo(t *testing.T) {
	testutil.SkipUnlessPg(t)
	// 本链含 organization_organizations/organization_members；identity_users
	// 属 identity 迁移链，userReader 的读路径按应用侧同库集成验证
	//（对照 access 同款取舍）。
	db := testutil.NewDB(t, testutil.Chain{Module: "organization", FS: migrations.FS})
	repo := NewRepository(db)
	reader := NewUserReader(db)
	ctx := context.Background()

	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	mkOrganization := func(id, name, slug string) *organization.Organization {
		o, err := organization.NewOrganization(id, name, slug, t0)
		if err != nil {
			t.Fatal(err)
		}
		return o
	}
	mkMember := func(organizationID, userID string, isOwner bool) *organization.Member {
		m, err := organization.NewMember(organizationID, userID, isOwner, t0)
		if err != nil {
			t.Fatal(err)
		}
		return m
	}

	o1 := mkOrganization("o1", "Acme Inc", "acme")
	if err := repo.Create(ctx, o1); err != nil {
		t.Fatalf("create organization: %v", err)
	}

	t.Run("slug unique maps ErrSlugTaken", func(t *testing.T) {
		dup := mkOrganization("o2", "Acme Too", "acme")
		if err := repo.Create(ctx, dup); !errors.Is(err, organization.ErrSlugTaken) {
			t.Fatalf("duplicate slug: err = %v, want ErrSlugTaken", err)
		}
		other := mkOrganization("o2", "Globex", "globex")
		if err := repo.Create(ctx, other); err != nil {
			t.Fatalf("distinct slug: %v", err)
		}
	})

	t.Run("get and update whitelist", func(t *testing.T) {
		got, err := repo.Get(ctx, "o1")
		if err != nil || got.Name != "Acme Inc" || got.Slug != "acme" {
			t.Fatalf("get = %+v err=%v", got, err)
		}
		if _, err := repo.Get(ctx, "ghost"); !errors.Is(err, organization.ErrOrganizationNotFound) {
			t.Fatalf("ghost get: err = %v, want ErrOrganizationNotFound", err)
		}
		t1 := t0.Add(time.Hour)
		if err := got.Rename("Acme Corporation", t1); err != nil {
			t.Fatal(err)
		}
		if err := repo.Update(ctx, got); err != nil {
			t.Fatalf("update: %v", err)
		}
		again, _ := repo.Get(ctx, "o1")
		if again.Name != "Acme Corporation" || again.Slug != "acme" || !again.UpdatedAt.Equal(t1) {
			t.Errorf("after update = %+v", again)
		}
		if err := repo.Update(ctx, mkOrganization("ghost", "X", "x-slug")); !errors.Is(err, organization.ErrOrganizationNotFound) {
			t.Errorf("ghost update: err = %v, want ErrOrganizationNotFound", err)
		}
	})

	t.Run("member crud idempotent", func(t *testing.T) {
		created, err := repo.AddMember(ctx, mkMember("o1", "owner", true))
		if err != nil || !created {
			t.Fatalf("first add: created=%v err=%v", created, err)
		}
		if created, _ := repo.AddMember(ctx, mkMember("o1", "owner", true)); created {
			t.Error("second add: created=true, want false（幂等）")
		}
		if _, err := repo.AddMember(ctx, mkMember("o1", "alice", false)); err != nil {
			t.Fatal(err)
		}

		m, err := repo.Member(ctx, "o1", "alice")
		if err != nil || m.IsOwner {
			t.Fatalf("member = %+v err=%v, want non-owner", m, err)
		}
		if _, err := repo.Member(ctx, "o1", "ghost"); !errors.Is(err, organization.ErrNotMember) {
			t.Fatalf("ghost member: err = %v, want ErrNotMember", err)
		}

		owners, err := repo.CountOwners(ctx, "o1")
		if err != nil || owners != 1 {
			t.Fatalf("owners = %d err=%v, want 1", owners, err)
		}

		removed, err := repo.RemoveMember(ctx, "o1", "alice")
		if err != nil || !removed {
			t.Fatalf("remove: removed=%v err=%v", removed, err)
		}
		if removed, _ := repo.RemoveMember(ctx, "o1", "alice"); removed {
			t.Error("second remove: removed=true, want false")
		}
	})

	t.Run("list members page", func(t *testing.T) {
		for _, uid := range []string{"u1", "u2", "u3"} {
			if _, err := repo.AddMember(ctx, mkMember("o1", uid, false)); err != nil {
				t.Fatal(err)
			}
		}
		members, total, err := repo.ListMembers(ctx, "o1", organization.MemberQuery{PageSize: 2, Offset: 0})
		if err != nil || total != 4 || len(members) != 2 {
			t.Fatalf("page1 = %d/%d err=%v, want 2 of 4", len(members), total, err)
		}
		page2, total, err := repo.ListMembers(ctx, "o1", organization.MemberQuery{PageSize: 2, Offset: 2})
		if err != nil || total != 4 || len(page2) != 2 {
			t.Fatalf("page2 = %d/%d err=%v, want 2 of 4", len(page2), total, err)
		}
		if members[0].UserID == page2[0].UserID {
			t.Error("pages overlap")
		}
	})

	t.Run("list by user", func(t *testing.T) {
		if _, err := repo.AddMember(ctx, mkMember("o2", "u1", false)); err != nil {
			t.Fatal(err)
		}
		list, total, err := repo.ListByUser(ctx, "u1", organization.OrganizationQuery{PageSize: 10, Offset: 0})
		if err != nil || total != 2 || len(list) != 2 {
			t.Fatalf("by user = %d/%d err=%v, want 2", len(list), total, err)
		}
		saw := map[string]bool{}
		for _, o := range list {
			saw[o.ID] = true
		}
		if !saw["o1"] || !saw["o2"] {
			t.Errorf("by user ids = %v, want o1+o2", saw)
		}
		if list, total, _ := repo.ListByUser(ctx, "stranger", organization.OrganizationQuery{PageSize: 10, Offset: 0}); total != 0 || len(list) != 0 {
			t.Errorf("stranger by user = %d/%d, want empty", len(list), total)
		}
	})

	t.Run("user reader read-only", func(t *testing.T) {
		if err := reader.Create(ctx, nil); err == nil {
			t.Error("Create must be rejected (read-only adapter)")
		}
		if err := reader.UpdatePassword(ctx, nil); err == nil {
			t.Error("UpdatePassword must be rejected (read-only adapter)")
		}
		if _, err := reader.GetByEmail(ctx, "owner@example.com"); err == nil {
			t.Error("GetByEmail must be rejected (read-only adapter)")
		}
	})
}
