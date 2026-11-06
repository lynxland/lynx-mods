package organization

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/lynxland/appkit/contrib/bunx/testutil"
	"github.com/lynxland/appkit/crud"
	"github.com/lynxland/lynx-start/modules/account/domain/account/identity"
	"github.com/lynxland/lynx-start/modules/organization/domain/organization"
)

// fakeRepo 是组织/成员仓储端口的内存实现。
type fakeRepo struct {
	mu            sync.Mutex
	organizations map[string]*organization.Organization // organizationID → Organization
	bySlug        map[string]string                     // slug → organizationID
	members       map[[2]string]*organization.Member    // (organizationID, userID) → Member
	order         [][2]string                           // 加入序（ListMembers/ListByUser 排序对账）
	joins         map[string][]string                   // userID → organizationID 序（ListByUser 用）
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{
		organizations: map[string]*organization.Organization{}, bySlug: map[string]string{},
		members: map[[2]string]*organization.Member{}, joins: map[string][]string{},
	}
}

func (r *fakeRepo) Create(_ context.Context, o *organization.Organization) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, taken := r.bySlug[o.Slug]; taken {
		return organization.ErrSlugTaken
	}
	r.organizations[o.ID] = o
	r.bySlug[o.Slug] = o.ID
	return nil
}

func (r *fakeRepo) Get(_ context.Context, id string) (*organization.Organization, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if o, ok := r.organizations[id]; ok {
		return o, nil
	}
	return nil, organization.ErrOrganizationNotFound
}

func (r *fakeRepo) Update(_ context.Context, o *organization.Organization) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.organizations[o.ID]; !ok {
		return organization.ErrOrganizationNotFound
	}
	r.organizations[o.ID] = o
	return nil
}

func (r *fakeRepo) AddMember(_ context.Context, m *organization.Member) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := [2]string{m.OrganizationID, m.UserID}
	if _, ok := r.members[key]; ok {
		return false, nil
	}
	r.members[key] = m
	r.order = append(r.order, key)
	r.joins[m.UserID] = append(r.joins[m.UserID], m.OrganizationID)
	return true, nil
}

func (r *fakeRepo) RemoveMember(_ context.Context, organizationID, userID string) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := [2]string{organizationID, userID}
	if _, ok := r.members[key]; !ok {
		return false, nil
	}
	delete(r.members, key)
	for i, k := range r.order {
		if k == key {
			r.order = append(r.order[:i], r.order[i+1:]...)
			break
		}
	}
	return true, nil
}

func (r *fakeRepo) Member(_ context.Context, organizationID, userID string) (*organization.Member, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if m, ok := r.members[[2]string{organizationID, userID}]; ok {
		return m, nil
	}
	return nil, organization.ErrNotMember
}

func (r *fakeRepo) ListMembers(_ context.Context, organizationID string, plan crud.PagePlan) ([]*organization.Member, int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var all []*organization.Member
	for _, k := range r.order {
		if k[0] == organizationID {
			all = append(all, r.members[k])
		}
	}
	end := int(plan.Limit)
	if end > len(all) || plan.Limit == 0 {
		end = len(all)
	}
	return all[:end], len(all), nil
}

func (r *fakeRepo) ListByUser(_ context.Context, userID string, plan crud.PagePlan) ([]*organization.Organization, int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var all []*organization.Organization
	for _, organizationID := range r.joins[userID] {
		all = append(all, r.organizations[organizationID])
	}
	end := int(plan.Limit)
	if end > len(all) || plan.Limit == 0 {
		end = len(all)
	}
	return all[:end], len(all), nil
}

func (r *fakeRepo) CountOwners(_ context.Context, organizationID string) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := 0
	for k, m := range r.members {
		if k[0] == organizationID && m.IsOwner {
			n++
		}
	}
	return n, nil
}

// fakeUserRepo 是 identity 仓储端口的内存实现（DependsOn 消费面）。
type fakeUserRepo struct{ users map[string]*identity.User }

func (r *fakeUserRepo) GetByID(_ context.Context, id string) (*identity.User, error) {
	if u, ok := r.users[id]; ok {
		return u, nil
	}
	return nil, identity.ErrUserNotFound
}

func (r *fakeUserRepo) Create(_ context.Context, u *identity.User) error {
	r.users[u.ID] = u
	return nil
}

func (r *fakeUserRepo) GetByEmail(_ context.Context, email string) (*identity.User, error) {
	return nil, identity.ErrUserNotFound
}

func (r *fakeUserRepo) UpdatePassword(_ context.Context, _ *identity.User) error { return nil }

// newUC 装配被测用例（共享 fake：InlineRunner/CapturePublisher，ADR-0005）。
func newUC(t *testing.T) (*UseCase, *fakeRepo, *fakeUserRepo, *testutil.CapturePublisher) {
	t.Helper()
	repo := newFakeRepo()
	users := &fakeUserRepo{users: map[string]*identity.User{
		"owner": {ID: "owner", Email: "owner@example.com", Status: identity.StatusActive},
		"alice": {ID: "alice", Email: "alice@example.com", Status: identity.StatusActive},
	}}
	pub := testutil.NewCapturePublisher()
	return New(repo, users, testutil.InlineRunner{}, pub), repo, users, pub
}

func TestCreate(t *testing.T) {
	uc, repo, _, pub := newUC(t)
	o, err := uc.Create(context.Background(), "owner", "Acme Inc", "acme")
	if err != nil {
		t.Fatal(err)
	}
	if o.Slug != "acme" {
		t.Fatalf("slug = %q", o.Slug)
	}
	// 创建者即 owner 成员。
	m, err := repo.Member(context.Background(), o.ID, "owner")
	if err != nil || !m.IsOwner {
		t.Fatalf("creator membership = %+v err=%v, want owner", m, err)
	}
	if names := pub.Names(); len(names) != 1 || names[0] != organization.EventOrganizationCreated {
		t.Fatalf("events = %v, want [organization.organization.created]", names)
	}
	// 坏 slug 与唯一冲突。
	if _, err := uc.Create(context.Background(), "owner", "Bad", "Acme!"); !errors.Is(err, organization.ErrInvalidSlug) {
		t.Errorf("invalid slug: err = %v", err)
	}
	if _, err := uc.Create(context.Background(), "owner", "Acme Two", "acme"); !errors.Is(err, organization.ErrSlugTaken) {
		t.Errorf("duplicate slug: err = %v, want ErrSlugTaken", err)
	}
}

func TestGetVisibility(t *testing.T) {
	uc, _, _, _ := newUC(t)
	ctx := context.Background()
	o, err := uc.Create(ctx, "owner", "Acme Inc", "acme")
	if err != nil {
		t.Fatal(err)
	}
	if err := uc.AddMember(ctx, "owner", o.ID, "alice"); err != nil {
		t.Fatal(err)
	}
	if _, err := uc.Get(ctx, "alice", o.ID); err != nil {
		t.Fatalf("member get: %v", err)
	}
	if _, err := uc.Get(ctx, "stranger", o.ID); !errors.Is(err, organization.ErrNotMember) {
		t.Errorf("stranger get: err = %v, want ErrNotMember", err)
	}
	if _, err := uc.Get(ctx, "owner", "missing"); !errors.Is(err, organization.ErrOrganizationNotFound) {
		t.Errorf("missing organization: err = %v, want ErrOrganizationNotFound", err)
	}
}

func TestRenameOwnerOnly(t *testing.T) {
	uc, _, _, _ := newUC(t)
	ctx := context.Background()
	o, _ := uc.Create(ctx, "owner", "Acme Inc", "acme")
	if err := uc.AddMember(ctx, "owner", o.ID, "alice"); err != nil {
		t.Fatal(err)
	}

	if _, err := uc.Rename(ctx, "owner", o.ID, "Acme Corporation"); err != nil {
		t.Fatalf("owner rename: %v", err)
	}
	got, _ := uc.Get(ctx, "owner", o.ID)
	if got.Name != "Acme Corporation" {
		t.Errorf("name = %q", got.Name)
	}
	if _, err := uc.Rename(ctx, "alice", o.ID, "Hacked"); !errors.Is(err, organization.ErrNotOwner) {
		t.Errorf("member rename: err = %v, want ErrNotOwner", err)
	}
	if _, err := uc.Rename(ctx, "stranger", o.ID, "Hacked"); !errors.Is(err, organization.ErrNotOwner) {
		t.Errorf("stranger rename: err = %v, want ErrNotOwner", err)
	}
}

func TestAddMember(t *testing.T) {
	uc, _, _, pub := newUC(t)
	ctx := context.Background()
	o, _ := uc.Create(ctx, "owner", "Acme Inc", "acme")

	if err := uc.AddMember(ctx, "owner", o.ID, "alice"); err != nil {
		t.Fatalf("add: %v", err)
	}
	if names := pub.Names(); len(names) != 2 || names[1] != organization.EventMemberAdded {
		t.Fatalf("events = %v, want organization.member.added", names)
	}
	// 幂等：重复添加 no-op、无新事件。
	if err := uc.AddMember(ctx, "owner", o.ID, "alice"); err != nil {
		t.Fatalf("idempotent add: %v", err)
	}
	if got := len(pub.Names()); got != 2 {
		t.Errorf("events after re-add = %d, want 2", got)
	}
	// 未知用户与非 owner 主体。
	if err := uc.AddMember(ctx, "owner", o.ID, "ghost"); !errors.Is(err, organization.ErrUnknownUser) {
		t.Errorf("ghost user: err = %v, want ErrUnknownUser", err)
	}
	if err := uc.AddMember(ctx, "alice", o.ID, "owner"); !errors.Is(err, organization.ErrNotOwner) {
		t.Errorf("non-owner actor: err = %v, want ErrNotOwner", err)
	}
}

func TestRemoveMember(t *testing.T) {
	uc, _, _, pub := newUC(t)
	ctx := context.Background()
	o, _ := uc.Create(ctx, "owner", "Acme Inc", "acme")
	if err := uc.AddMember(ctx, "owner", o.ID, "alice"); err != nil {
		t.Fatal(err)
	}

	if err := uc.RemoveMember(ctx, "owner", o.ID, "alice"); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if names := pub.Names(); names[len(names)-1] != organization.EventMemberRemoved {
		t.Fatalf("events = %v, want organization.member.removed last", names)
	}
	// 幂等。
	if err := uc.RemoveMember(ctx, "owner", o.ID, "alice"); err != nil {
		t.Fatalf("idempotent remove: %v", err)
	}
	// 不可移除 owner；缺失组织。
	if err := uc.RemoveMember(ctx, "owner", o.ID, "owner"); !errors.Is(err, organization.ErrCannotRemoveOwner) {
		t.Errorf("remove owner: err = %v, want ErrCannotRemoveOwner", err)
	}
	if err := uc.RemoveMember(ctx, "alice", "missing", "owner"); !errors.Is(err, organization.ErrOrganizationNotFound) {
		t.Errorf("missing organization: err = %v", err)
	}
}

func TestLeave(t *testing.T) {
	uc, _, _, pub := newUC(t)
	ctx := context.Background()
	o, _ := uc.Create(ctx, "owner", "Acme Inc", "acme")
	if err := uc.AddMember(ctx, "owner", o.ID, "alice"); err != nil {
		t.Fatal(err)
	}

	// 最后一个 owner 不可离开。
	if err := uc.Leave(ctx, "owner", o.ID); !errors.Is(err, organization.ErrLastOwner) {
		t.Fatalf("last owner leave: err = %v, want ErrLastOwner", err)
	}
	// 普通成员离开成功并发事件。
	if err := uc.Leave(ctx, "alice", o.ID); err != nil {
		t.Fatalf("member leave: %v", err)
	}
	if names := pub.Names(); names[len(names)-1] != organization.EventMemberRemoved {
		t.Fatalf("events = %v, want organization.member.removed last", names)
	}
	// 非成员离开拒绝。
	if err := uc.Leave(ctx, "stranger", o.ID); !errors.Is(err, organization.ErrNotMember) {
		t.Errorf("stranger leave: err = %v, want ErrNotMember", err)
	}
}

func TestListMembersAndMyOrganizations(t *testing.T) {
	uc, _, _, _ := newUC(t)
	ctx := context.Background()
	o, _ := uc.Create(ctx, "owner", "Acme Inc", "acme")
	if err := uc.AddMember(ctx, "owner", o.ID, "alice"); err != nil {
		t.Fatal(err)
	}

	res, err := uc.ListMembers(ctx, "alice", o.ID, 10, "")
	if err != nil {
		t.Fatalf("list members: %v", err)
	}
	if len(res.Members) != 2 || res.TotalSize != 2 {
		t.Fatalf("members = %+v total=%d, want 2", res.Members, res.TotalSize)
	}
	owners := 0
	for _, m := range res.Members {
		if m.IsOwner {
			owners++
		}
	}
	if owners != 1 {
		t.Errorf("owners = %d, want 1", owners)
	}
	if _, err := uc.ListMembers(ctx, "stranger", o.ID, 10, ""); !errors.Is(err, organization.ErrNotMember) {
		t.Errorf("stranger list: err = %v, want ErrNotMember", err)
	}

	mine, err := uc.ListMyOrganizations(ctx, "alice", 10, "")
	if err != nil || len(mine.Organizations) != 1 || mine.TotalSize != 1 {
		t.Fatalf("my organizations = %+v err=%v", mine, err)
	}
	if mine.Organizations[0].ID != o.ID {
		t.Errorf("my organizations[0] = %s, want %s", mine.Organizations[0].ID, o.ID)
	}
	if mine, _ := uc.ListMyOrganizations(ctx, "stranger", 10, ""); mine.TotalSize != 0 {
		t.Errorf("stranger my organizations total = %d, want 0", mine.TotalSize)
	}
}

// TestEventPayloads 载荷经 json.Marshal 编码且字段齐整（事件消费方契约）。
func TestEventPayloads(t *testing.T) {
	uc, _, _, pub := newUC(t)
	ctx := context.Background()
	o, _ := uc.Create(ctx, "owner", "Acme Inc", "acme")
	if err := uc.AddMember(ctx, "owner", o.ID, "alice"); err != nil {
		t.Fatal(err)
	}

	for _, env := range pub.Envelopes() {
		if env.Module != "organization" {
			t.Errorf("event %s: module = %q", env.Name, env.Module)
		}
		if env.OccurredAt.IsZero() {
			t.Errorf("event %s: zero OccurredAt", env.Name)
		}
	}
	last := pub.Envelopes()[len(pub.Envelopes())-1]
	want := `{"organization_id":"` + o.ID + `","organization_name":"Acme Inc","user_id":"alice"}`
	if string(last.Payload) != want {
		t.Errorf("member.added payload = %s, want %s", last.Payload, want)
	}
}

// TestNowInjectable 固定时钟注入（时间可控）。
func TestNowInjectable(t *testing.T) {
	fixed := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	repo := newFakeRepo()
	users := &fakeUserRepo{users: map[string]*identity.User{"owner": {ID: "owner"}}}
	uc := New(repo, users, testutil.InlineRunner{}, testutil.NewCapturePublisher())
	uc.now = func() time.Time { return fixed }
	o, err := uc.Create(context.Background(), "owner", "Acme Inc", "acme")
	if err != nil {
		t.Fatal(err)
	}
	if !o.CreatedAt.Equal(fixed) {
		t.Errorf("CreatedAt = %v, want %v", o.CreatedAt, fixed)
	}
}
