package task

import (
	"context"
	"testing"
	"time"

	"github.com/lynxland/lynx-start/modules/task/domain/task"

	"github.com/lynxland/appkit/contrib/bunx/testutil"
	"github.com/lynxland/appkit/crud"
)

// fakeRepo 见 runner 同名 fake 的最小面（app 层用例只消费 Insert/List 等）。
type fakeRepo struct {
	inserted []*task.Job
	list     []*task.Job
	total    int
}

func (f *fakeRepo) Insert(_ context.Context, j *task.Job) (*task.Job, bool, error) {
	f.inserted = append(f.inserted, j)
	return j, true, nil
}
func (f *fakeRepo) Get(context.Context, string) (*task.Job, error) { return nil, task.ErrJobNotFound }
func (f *fakeRepo) Claim(context.Context, task.ClaimFilter) ([]*task.Job, error) {
	return nil, nil
}
func (f *fakeRepo) Complete(context.Context, string, time.Time) error { return nil }
func (f *fakeRepo) Fail(context.Context, string, string, time.Time, bool, time.Time) error {
	return nil
}
func (f *fakeRepo) Retry(context.Context, string, time.Time) (*task.Job, error) {
	return nil, task.ErrNotRetryable
}
func (f *fakeRepo) Cancel(context.Context, string, time.Time) (*task.Job, error) {
	return nil, task.ErrNotPending
}
func (f *fakeRepo) List(_ context.Context, _ task.JobFilter, _ crud.PagePlan) ([]*task.Job, int, error) {
	return f.list, f.total, nil
}
func (f *fakeRepo) Stats(context.Context) (task.JobStats, error)          { return task.JobStats{}, nil }
func (f *fakeRepo) UpsertRule(context.Context, *task.RecurringRule) error { return nil }
func (f *fakeRepo) GetRule(context.Context, string) (*task.RecurringRule, error) {
	return nil, task.ErrRuleNotFound
}
func (f *fakeRepo) DeleteRule(context.Context, string) error { return nil }
func (f *fakeRepo) ListRules(context.Context, crud.PagePlan) ([]*task.RecurringRule, int, error) {
	return nil, 0, nil
}
func (f *fakeRepo) ClaimDueRules(context.Context, time.Time, int) ([]*task.RecurringRule, error) {
	return nil, nil
}

func TestEnqueueValidation(t *testing.T) {
	uc := New(&fakeRepo{}, testutil.NewCapturePublisher())
	ctx := context.Background()

	if _, _, err := uc.Enqueue(ctx, EnqueueInput{Type: "x"}); err != task.ErrInvalidJob {
		t.Errorf("short type err = %v, want ErrInvalidJob", err)
	}
	if _, _, err := uc.Enqueue(ctx, EnqueueInput{Type: "ok.type", Payload: "{oops"}); err != task.ErrInvalidJob {
		t.Errorf("bad json err = %v", err)
	}
	at := time.Now()
	d := time.Minute
	if _, _, err := uc.Enqueue(ctx, EnqueueInput{Type: "ok.type", RunAt: &at, Delay: &d}); err != task.ErrInvalidJob {
		t.Errorf("run_at+delay err = %v, want ErrInvalidJob", err)
	}
	if _, _, err := uc.Enqueue(ctx, EnqueueInput{Type: "ok.type", Delay: &d}); err != nil {
		t.Errorf("delay enqueue: %v", err)
	}
}
