package task

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/lynxland/lynx-start/modules/task/domain/task"

	"github.com/lynxland/appkit/clock"
	"github.com/lynxland/appkit/crud"
)

// fakeRepo 是 task.Repository 的内存 fake（runner 单测用）。
type fakeRepo struct {
	mu    sync.Mutex
	jobs  map[string]*task.Job
	claim map[string]bool // 已认领
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{jobs: map[string]*task.Job{}, claim: map[string]bool{}}
}

func (f *fakeRepo) seed(j *task.Job) { f.mu.Lock(); f.jobs[j.ID] = j; f.mu.Unlock() }

func (f *fakeRepo) Insert(_ context.Context, j *task.Job) (*task.Job, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.jobs[j.ID] = j
	return j, true, nil
}
func (f *fakeRepo) Get(_ context.Context, id string) (*task.Job, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if j, ok := f.jobs[id]; ok {
		return j, nil
	}
	return nil, task.ErrJobNotFound
}
func (f *fakeRepo) Claim(_ context.Context, flt task.ClaimFilter) ([]*task.Job, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []*task.Job
	for _, j := range f.jobs {
		if j.Status == task.StatusPending && !j.RunAt.After(flt.Now) && !f.claim[j.ID] {
			j.Status, j.Attempt = task.StatusRunning, j.Attempt+1
			f.claim[j.ID] = true
			out = append(out, j)
			if len(out) >= flt.Limit {
				break
			}
		}
	}
	return out, nil
}
func (f *fakeRepo) Complete(_ context.Context, id string, _ time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.jobs[id].Status = task.StatusDone
	return nil
}
func (f *fakeRepo) Fail(_ context.Context, id, msg string, _ time.Time, dead bool, _ time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if dead {
		f.jobs[id].Status = task.StatusDead
	} else {
		f.jobs[id].Status = task.StatusPending
	}
	f.jobs[id].LastError = msg
	return nil
}
func (f *fakeRepo) Retry(_ context.Context, id string, _ time.Time) (*task.Job, error) {
	return nil, task.ErrNotRetryable
}
func (f *fakeRepo) Cancel(_ context.Context, id string, _ time.Time) (*task.Job, error) {
	return nil, task.ErrNotPending
}
func (f *fakeRepo) List(context.Context, task.JobFilter, crud.PagePlan) ([]*task.Job, int, error) {
	return nil, 0, nil
}
func (f *fakeRepo) Stats(context.Context) (task.JobStats, error)          { return task.JobStats{}, nil }
func (f *fakeRepo) UpsertRule(context.Context, *task.RecurringRule) error { return nil }
func (f *fakeRepo) GetRule(context.Context, string) (*task.RecurringRule, error) {
	return nil, task.ErrRuleNotFound
}
func (f *fakeRepo) DeleteRule(context.Context, string) error { return task.ErrRuleNotFound }
func (f *fakeRepo) ListRules(context.Context, crud.PagePlan) ([]*task.RecurringRule, int, error) {
	return nil, 0, nil
}
func (f *fakeRepo) ClaimDueRules(context.Context, time.Time, int) ([]*task.RecurringRule, error) {
	return nil, nil
}

// mapRegistry 是 HandlerRegistry 的最小实现。
type mapRegistry map[string]task.Handler

func (m mapRegistry) Lookup(t string) (task.Handler, bool) { h, ok := m[t]; return h, ok }

func TestRunnerExecutesAndFails(t *testing.T) {
	repo := newFakeRepo()
	okJob, _ := task.NewJob("j1", "ok.type", "{}", time.Now(), 0, 3, "")
	repo.seed(okJob)
	boomJob, _ := task.NewJob("j2", "boom.type", "{}", time.Now(), 0, 1, "")
	repo.seed(boomJob)
	ghostJob, _ := task.NewJob("j3", "ghost.type", "{}", time.Now(), 0, 3, "")
	repo.seed(ghostJob)

	var handled sync.Map
	r := &Runner{
		repo: repo, now: clock.System{}, handlers: mapRegistry{
			"ok.type":   func(context.Context, *task.Job) error { handled.Store("j1", true); return nil },
			"boom.type": func(context.Context, *task.Job) error { return errors.New("boom") },
		},
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		opts:   RunnerOptions{Concurrency: 1, Interval: 10 * time.Millisecond, BatchSize: 10},
	}
	r.opts.applyDefaults()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { _ = r.Start(ctx); close(done) }()

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		repo.mu.Lock()
		okDone := repo.jobs["j1"].Status == task.StatusDone
		dead := repo.jobs["j2"].Status == task.StatusDead
		repo.mu.Unlock()
		if okDone && dead {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	<-done

	repo.mu.Lock()
	defer repo.mu.Unlock()
	if repo.jobs["j1"].Status != task.StatusDone {
		t.Errorf("ok job = %s, want done", repo.jobs["j1"].Status)
	}
	if _, ran := handled.Load("j1"); !ran {
		t.Error("handler for j1 never ran")
	}
	// boom.type max_attempts=1：首次失败即死信。
	if repo.jobs["j2"].Status != task.StatusDead || repo.jobs["j2"].LastError != "boom" {
		t.Errorf("boom job = %+v, want dead/boom", repo.jobs["j2"])
	}
	// 无 handler 的类型按失败退避（attempt 已消耗，退回 pending 重试）。
	if repo.jobs["j3"].Attempt == 0 {
		t.Error("ghost job never attempted")
	}
	if repo.jobs["j3"].LastError == "" {
		t.Error("ghost job failure not recorded")
	}
}
