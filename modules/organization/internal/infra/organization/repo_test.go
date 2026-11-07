package organization

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/lynxland/appkit/contrib/bunx/testutil"
	"github.com/lynxland/appkit/crud"
	"github.com/lynxland/lynx-start/modules/organization/domain/organization"
	"github.com/lynxland/lynx-start/modules/organization/internal/infra/organization/migrations"
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
		membersPlan := crud.PagePlan{Mode: crud.ModeKeyset, Limit: 2, Order: []crud.OrderClause{{Expr: "m.joined_at", Desc: true, RawField: "joinedAt"}, {Expr: "m.user_id", RawField: "userId"}}}
		members, total, err := repo.ListMembers(ctx, "o1", membersPlan)
		if err != nil || total != 4 || len(members) != 2 {
			t.Fatalf("page1 = %d/%d err=%v, want 2 of 4", len(members), total, err)
		}
		page2Plan := membersPlan
		page2Plan.CursorValues = []string{members[len(members)-1].JoinedAt.UTC().Format(time.RFC3339Nano), members[len(members)-1].UserID}
		page2, total, err := repo.ListMembers(ctx, "o1", page2Plan)
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
		orgsPlan := crud.PagePlan{Mode: crud.ModeKeyset, Limit: 10, Order: []crud.OrderClause{{Expr: "o.created_at", Desc: true, RawField: "createdAt"}, {Expr: "o.id", RawField: "id"}}}
		list, total, err := repo.ListByUser(ctx, "u1", orgsPlan)
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
		if list, total, _ := repo.ListByUser(ctx, "stranger", orgsPlan); total != 0 || len(list) != 0 {
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

// TestB2B 覆盖 v0.7.0：角色分级 / 邀请状态机 / 所有权转让 / 软删过滤。
func TestB2B(t *testing.T) {
	testutil.SkipUnlessPg(t)
	db := testutil.NewDB(t, testutil.Chain{Module: "organization", FS: migrations.FS})
	repo := NewRepository(db)
	ctx := context.Background()
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	// 种组织 + owner + 两成员。
	o, err := organization.NewOrganization("org-b2b", "Acme B2B", "acme-b2b", base)
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.Create(ctx, o); err != nil {
		t.Fatal(err)
	}
	owner, _ := organization.NewMemberWithRole(o.ID, "u-owner", organization.MemberRoleOwner, base)
	if _, err := repo.AddMember(ctx, owner); err != nil {
		t.Fatal(err)
	}
	mem1, _ := organization.NewMemberWithRole(o.ID, "u-mem1", organization.MemberRoleAdmin, base)
	if _, err := repo.AddMember(ctx, mem1); err != nil {
		t.Fatal(err)
	}

	t.Run("member role read and update", func(t *testing.T) {
		role, err := repo.GetMemberRole(ctx, o.ID, "u-owner")
		if err != nil || role != organization.MemberRoleOwner {
			t.Fatalf("owner role = %q err=%v", role, err)
		}
		if err := repo.UpdateMemberRole(ctx, o.ID, "u-mem1", organization.MemberRoleMember, base); err != nil {
			t.Fatal(err)
		}
		if role, _ := repo.GetMemberRole(ctx, o.ID, "u-mem1"); role != organization.MemberRoleMember {
			t.Fatalf("updated role = %q", role)
		}
		// owner 角色不可直改。
		if err := repo.UpdateMemberRole(ctx, o.ID, "u-owner", organization.MemberRoleMember, base); err != organization.ErrInvalidRole {
			t.Fatalf("owner demote err = %v, want ErrInvalidRole", err)
		}
	})

	t.Run("invitation lifecycle", func(t *testing.T) {
		inv, err := organization.NewInvitation("inv-1", o.ID, "New@Example.com", organization.MemberRoleAdmin, "tok-1", "u-owner", base)
		if err != nil {
			t.Fatal(err)
		}
		if inv.Email != "new@example.com" {
			t.Fatalf("email normalized = %q", inv.Email)
		}
		if err := repo.InsertInvitation(ctx, inv); err != nil {
			t.Fatal(err)
		}
		// 重复邀请（pending 唯一）改写已有行——令牌刷新。
		inv2, _ := organization.NewInvitation("inv-2", o.ID, "new@example.com", organization.MemberRoleMember, "tok-2", "u-owner", base.Add(time.Minute))
		if err := repo.InsertInvitation(ctx, inv2); err != nil {
			t.Fatal(err)
		}
		got, err := repo.GetInvitationByToken(ctx, "tok-2")
		if err != nil || got.Role != organization.MemberRoleMember {
			t.Fatalf("rewritten invite = %+v err=%v", got, err)
		}
		if _, err := repo.GetInvitationByToken(ctx, "tok-1"); err != organization.ErrInviteNotFound {
			t.Fatalf("stale token err = %v, want ErrInviteNotFound", err)
		}
		// 邮箱不匹配拒绝。
		if err := got.AcceptIfEligible("other@example.com", "u-x", base); err != organization.ErrInviteEmailMismatch {
			t.Fatalf("mismatch err = %v", err)
		}
		// 过期拒绝。
		if err := got.AcceptIfEligible("new@example.com", "u-new", got.ExpiresAt.Add(time.Second)); err != organization.ErrInviteExpired {
			t.Fatalf("expired err = %v", err)
		}
		// 正常接受。
		if err := got.AcceptIfEligible("new@example.com", "u-new", base); err != nil {
			t.Fatal(err)
		}
		if err := repo.UpdateInvitationStatus(ctx, got); err != nil {
			t.Fatal(err)
		}
		back, _ := repo.GetInvitationByToken(ctx, "tok-2")
		if back.Status != organization.InviteStatusAccepted || back.AcceptedBy != "u-new" {
			t.Fatalf("accepted = %+v", back)
		}
		// 二次接受拒绝（非 pending）。
		if err := back.AcceptIfEligible("new@example.com", "u-new", base); err != organization.ErrInviteNotPending {
			t.Fatalf("re-accept err = %v", err)
		}
	})

	t.Run("transfer ownership", func(t *testing.T) {
		if err := repo.TransferOwnership(ctx, o.ID, "u-mem1", base); err != nil {
			t.Fatal(err)
		}
		if role, _ := repo.GetMemberRole(ctx, o.ID, "u-mem1"); role != organization.MemberRoleOwner {
			t.Fatalf("new owner role = %q", role)
		}
		if role, _ := repo.GetMemberRole(ctx, o.ID, "u-owner"); role != organization.MemberRoleAdmin {
			t.Fatalf("old owner role = %q", role)
		}
		// 非成员转让失败。
		if err := repo.TransferOwnership(ctx, o.ID, "ghost", base); err != organization.ErrNotMember {
			t.Fatalf("ghost transfer err = %v", err)
		}
	})

	t.Run("soft delete filters visibility", func(t *testing.T) {
		if err := repo.SoftDelete(ctx, o.ID, base); err != nil {
			t.Fatal(err)
		}
		if _, err := repo.Get(ctx, o.ID); err != organization.ErrOrganizationNotFound {
			t.Fatalf("deleted get err = %v", err)
		}
		if orgs, total, _ := repo.ListByUser(ctx, "u-owner", orgsPlan(t)); total != 0 || len(orgs) != 0 {
			t.Fatalf("deleted list = %d/%d, want empty", len(orgs), total)
		}
	})
}

func orgsPlan(t *testing.T) crud.PagePlan {
	t.Helper()
	return crud.PagePlan{
		Mode:  crud.ModeKeyset,
		Limit: 10,
		Order: []crud.OrderClause{{Expr: "o.created_at", Desc: true, RawField: "createdAt"}, {Expr: "o.id", RawField: "id"}},
	}
}
