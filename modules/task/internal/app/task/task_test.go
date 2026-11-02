package task

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/lynx-go/lynx-mods/modules/organization/domain/organization"
	"github.com/lynx-go/lynx-mods/modules/task/domain/task"
	"github.com/lynxland/appkit/contrib/bunx/testutil"
)

// fakeRepo 是内存任务仓储。
type fakeRepo struct {
	items map[string]*task.Task
}

func newFakeRepo() *fakeRepo { return &fakeRepo{items: map[string]*task.Task{}} }

func (r *fakeRepo) Create(_ context.Context, t *task.Task) error {
	r.items[t.ID] = t
	return nil
}

func (r *fakeRepo) Get(_ context.Context, organizationID, id string) (*task.Task, error) {
	if t, ok := r.items[id]; ok && t.OrganizationID == organizationID {
		return t, nil
	}
	return nil, task.ErrTaskNotFound
}

func (r *fakeRepo) List(_ context.Context, organizationID string, q task.Query) ([]*task.Task, int, error) {
	var matched []*task.Task
	for _, t := range r.items {
		if t.OrganizationID == organizationID {
			matched = append(matched, t)
		}
	}
	total := len(matched)
	end := q.Offset + q.PageSize
	if end > total {
		end = total
	}
	if q.Offset > total {
		q.Offset = total
	}
	return matched[q.Offset:end], total, nil
}

func (r *fakeRepo) Update(_ context.Context, t *task.Task) error {
	if _, ok := r.items[t.ID]; !ok {
		return task.ErrTaskNotFound
	}
	r.items[t.ID] = t
	return nil
}

func (r *fakeRepo) Delete(_ context.Context, organizationID, id string) (bool, error) {
	if t, ok := r.items[id]; ok && t.OrganizationID == organizationID {
		delete(r.items, id)
		return true, nil
	}
	return false, nil
}

var _ task.Repository = (*fakeRepo)(nil)

// fakeOrgRepo 是 organization.Repository 的测试面：只有 Member 真实工作，
// 其余方法（本用例不会触碰）panic——触碰即测试缺陷。
type fakeOrgRepo struct {
	members map[string]bool // key: organizationID + "/" + userID
}

func (f *fakeOrgRepo) Member(_ context.Context, organizationID, userID string) (*organization.Member, error) {
	if f.members[organizationID+"/"+userID] {
		return &organization.Member{OrganizationID: organizationID, UserID: userID}, nil
	}
	return nil, organization.ErrNotMember
}

func (f *fakeOrgRepo) Create(context.Context, *organization.Organization) error { panic("unused") }
func (f *fakeOrgRepo) Get(context.Context, string) (*organization.Organization, error) {
	panic("unused")
}
func (f *fakeOrgRepo) Update(context.Context, *organization.Organization) error { panic("unused") }
func (f *fakeOrgRepo) AddMember(context.Context, *organization.Member) (bool, error) {
	panic("unused")
}
func (f *fakeOrgRepo) RemoveMember(context.Context, string, string) (bool, error) {
	panic("unused")
}
func (f *fakeOrgRepo) ListMembers(context.Context, string, organization.MemberQuery) ([]*organization.Member, int, error) {
	panic("unused")
}
func (f *fakeOrgRepo) ListByUser(context.Context, string, organization.OrganizationQuery) ([]*organization.Organization, int, error) {
	panic("unused")
}
func (f *fakeOrgRepo) CountOwners(context.Context, string) (int, error) { panic("unused") }

var _ organization.Repository = (*fakeOrgRepo)(nil)

// newUC 装配被测用例（alice/bob 是 o1 成员，carol 不是）。
func newUC(t *testing.T) (*UseCase, *fakeRepo, *testutil.CapturePublisher) {
	t.Helper()
	repo := newFakeRepo()
	orgs := &fakeOrgRepo{members: map[string]bool{"o1/alice": true, "o1/bob": true}}
	pub := testutil.NewCapturePublisher()
	return New(repo, orgs, testutil.InlineRunner{}, pub), repo, pub
}

func TestCreateRequiresMembership(t *testing.T) {
	uc, _, _ := newUC(t)
	ctx := context.Background()

	if _, err := uc.Create(ctx, "carol", "o1", "写周报", ""); !errors.Is(err, organization.ErrNotMember) {
		t.Fatalf("non-member create: err = %v, want ErrNotMember", err)
	}
	// 组织不存在与非成员统一 ErrNotMember（不泄露存在性）。
	if _, err := uc.Create(ctx, "alice", "ghost", "写周报", ""); !errors.Is(err, organization.ErrNotMember) {
		t.Fatalf("ghost org create: err = %v, want ErrNotMember", err)
	}
	if _, err := uc.Create(ctx, "alice", "o1", "", ""); !errors.Is(err, task.ErrEmptyTitle) {
		t.Fatalf("empty title: err = %v, want ErrEmptyTitle", err)
	}
}

func TestCreatePublishesEvent(t *testing.T) {
	uc, _, pub := newUC(t)
	created, err := uc.Create(context.Background(), "alice", "o1", "写周报", "周五前")
	if err != nil {
		t.Fatal(err)
	}
	envs := pub.Envelopes()
	if len(envs) != 1 || envs[0].Name != task.EventTaskCreated || envs[0].Module != "task" {
		t.Fatalf("events = %+v", envs)
	}
	if envs[0].OccurredAt.IsZero() {
		t.Error("zero OccurredAt")
	}
	if created.CreatorID != "alice" || created.OrganizationID != "o1" {
		t.Errorf("creator/org = %s/%s", created.CreatorID, created.OrganizationID)
	}
}

func TestTenantIsolationInPort(t *testing.T) {
	uc, repo, _ := newUC(t)
	ctx := context.Background()
	created, _ := uc.Create(ctx, "alice", "o1", "写周报", "")
	// 同 ID 换组织取不到（租户语境进端口签名，ADR-0008）。
	if _, err := repo.Get(ctx, "o2", created.ID); !errors.Is(err, task.ErrTaskNotFound) {
		t.Fatalf("cross-org get: err = %v, want ErrTaskNotFound", err)
	}
	// o2 成员也看不到 o1 的任务（成员资格先行拒绝）。
	if _, err := uc.Get(ctx, "alice", "o2", created.ID); !errors.Is(err, organization.ErrNotMember) {
		t.Fatalf("cross-org via usecase: err = %v, want ErrNotMember", err)
	}
}

func TestCompleteCreatorOnlyAndIdempotent(t *testing.T) {
	uc, _, pub := newUC(t)
	ctx := context.Background()
	created, _ := uc.Create(ctx, "alice", "o1", "写周报", "")

	// 成员但非创建者：拒绝。
	if _, err := uc.Complete(ctx, "bob", "o1", created.ID); !errors.Is(err, task.ErrNotCreator) {
		t.Fatalf("non-creator complete: err = %v, want ErrNotCreator", err)
	}
	if _, err := uc.Complete(ctx, "alice", "o1", created.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := uc.Complete(ctx, "alice", "o1", created.ID); err != nil {
		t.Fatalf("idempotent complete: %v", err)
	}
	if n := len(pub.Envelopes()); n != 2 { // created + completed 各一，幂等不发
		t.Errorf("events = %d, want 2", n)
	}
}

func TestDeleteCreatorOnlyAndIdempotent(t *testing.T) {
	uc, _, _ := newUC(t)
	ctx := context.Background()
	created, _ := uc.Create(ctx, "alice", "o1", "写周报", "")

	if err := uc.Delete(ctx, "bob", "o1", created.ID); !errors.Is(err, task.ErrNotCreator) {
		t.Fatalf("non-creator delete: err = %v, want ErrNotCreator", err)
	}
	if err := uc.Delete(ctx, "alice", "o1", created.ID); err != nil {
		t.Fatal(err)
	}
	if err := uc.Delete(ctx, "alice", "o1", created.ID); err != nil { // 幂等 no-op
		t.Fatalf("idempotent delete: %v", err)
	}
	if _, err := uc.Get(ctx, "alice", "o1", created.ID); !errors.Is(err, task.ErrTaskNotFound) {
		t.Fatalf("deleted get: err = %v, want ErrTaskNotFound", err)
	}
}

func TestListScopedToOrganization(t *testing.T) {
	uc, repo, _ := newUC(t)
	ctx := context.Background()
	_, _ = uc.Create(ctx, "alice", "o1", "任务一", "")
	other, _ := task.NewTask("t-other", "o9", "someone", "他组织任务", "", time.Now())
	_ = repo.Create(ctx, other)

	res, err := uc.List(ctx, "alice", "o1", 0, "")
	if err != nil || len(res.Tasks) != 1 || res.TotalSize != 1 {
		t.Fatalf("list = %d/%d err=%v, want 1/1", len(res.Tasks), res.TotalSize, err)
	}
	if res.Tasks[0].Title != "任务一" {
		t.Errorf("title = %q", res.Tasks[0].Title)
	}
}
