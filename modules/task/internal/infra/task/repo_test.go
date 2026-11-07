package task

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/lynxland/lynx-start/modules/task/domain/task"

	"github.com/lynxland/appkit/contrib/bunx/testutil"
	"github.com/lynxland/appkit/crud"
	"github.com/lynxland/appkit/idgen"
	"github.com/lynxland/lynx-start/modules/task/internal/infra/task/migrations"
)

// jobsPlan 与 app 层 jobsSpec 解析产物同形（keyset：created_at DESC +
// id 决胜 ASC）。
var jobsPlan = crud.PagePlan{
	Mode:  crud.ModeKeyset,
	Limit: 50,
	Order: []crud.OrderClause{{Expr: "j.created_at", Desc: true, RawField: "createdAt"}, {Expr: "j.id", RawField: "id"}},
}

func TestRepo(t *testing.T) {
	testutil.SkipUnlessPg(t)
	db := testutil.NewDB(t, testutil.Chain{Module: "task", FS: migrations.FS})
	repo := NewRepository(db)
	ctx := context.Background()
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	mk := func(typ string, runAt time.Time, unique string) *task.Job {
		t.Helper()
		j, err := task.NewJob(idgen.New(), typ, `{"k":1}`, runAt, 0, 3, unique)
		if err != nil {
			t.Fatal(err)
		}
		j.CreatedAt = base
		return j
	}

	t.Run("insert unique-key dedupe", func(t *testing.T) {
		a := mk("email.send", base, "dedupe-1")
		if _, created, err := repo.Insert(ctx, a); err != nil || !created {
			t.Fatalf("first insert: created=%v err=%v", created, err)
		}
		b := mk("email.send", base.Add(time.Minute), "dedupe-1")
		existing, created, err := repo.Insert(ctx, b)
		if err != nil || created || existing.ID != a.ID {
			t.Fatalf("dedupe insert: created=%v existing=%v err=%v", created, existing.ID, err)
		}
		// 完成后同 key 可重新入队（存续期唯一）。
		claimed, err := repo.Claim(ctx, task.ClaimFilter{Now: base, Limit: 10})
		if err != nil {
			t.Fatal(err)
		}
		for _, c := range claimed {
			if c.ID == a.ID {
				if err := repo.Complete(ctx, a.ID, base); err != nil {
					t.Fatal(err)
				}
			}
		}
		c := mk("email.send", base.Add(2*time.Minute), "dedupe-1")
		if _, created, err := repo.Insert(ctx, c); err != nil || !created {
			t.Fatalf("re-insert after done: created=%v err=%v", created, err)
		}
	})

	t.Run("claim atomic under concurrency", func(t *testing.T) {
		// 种 8 个任务，2 个并发认领者各取 4——并集恰好 8、交集为空。
		var seeds []*task.Job
		for i := 0; i < 8; i++ {
			j := mk("batch.job", base.Add(time.Duration(i)*time.Second), "")
			if _, _, err := repo.Insert(ctx, j); err != nil {
				t.Fatal(err)
			}
			seeds = append(seeds, j)
		}
		var mu sync.Mutex
		got := map[string]bool{}
		var wg sync.WaitGroup
		for w := 0; w < 2; w++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				jobs, err := repo.Claim(ctx, task.ClaimFilter{Now: base.Add(time.Hour), Limit: 4})
				if err != nil {
					t.Error(err)
					return
				}
				mu.Lock()
				defer mu.Unlock()
				for _, j := range jobs {
					if got[j.ID] {
						t.Errorf("job %s claimed twice", j.ID)
					}
					got[j.ID] = true
					if j.Status != task.StatusRunning || j.Attempt != 1 {
						t.Errorf("claimed job state = %s/%d, want running/1", j.Status, j.Attempt)
					}
				}
			}()
		}
		wg.Wait()
		if len(got) != 8 {
			t.Fatalf("claimed %d unique jobs, want 8", len(got))
		}
		_ = seeds
	})

	t.Run("claim respects run_at and stale recovery", func(t *testing.T) {
		future := mk("future.job", base.Add(time.Hour), "")
		if _, _, err := repo.Insert(ctx, future); err != nil {
			t.Fatal(err)
		}
		jobs, err := repo.Claim(ctx, task.ClaimFilter{Now: base, Limit: 10})
		if err != nil {
			t.Fatal(err)
		}
		for _, j := range jobs {
			if j.ID == future.ID {
				t.Error("future job claimed before run_at")
			}
		}
		// stale 回收：把 future 认领后人为卡 RUNNING，超阈值回收回 PENDING。
		staleJobs, _ := repo.Claim(ctx, task.ClaimFilter{Now: base.Add(2 * time.Hour), Limit: 10})
		var staleID string
		for _, j := range staleJobs {
			if j.ID == future.ID {
				staleID = j.ID
			}
		}
		if staleID == "" {
			t.Fatal("future job not claimed at its run_at")
		}
		recovered, err := repo.Claim(ctx, task.ClaimFilter{Now: base.Add(3 * time.Hour), Limit: 10, StaleBefore: base.Add(2 * time.Hour).Add(time.Minute)})
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, j := range recovered {
			if j.ID == staleID {
				found = true
				if j.Attempt != 2 {
					t.Errorf("recovered attempt = %d, want 2", j.Attempt)
				}
			}
		}
		if !found {
			t.Fatal("stale running job not recovered to pending")
		}
	})

	t.Run("fail backoff and dead transition", func(t *testing.T) {
		j := mk("flaky.job", base, "")
		if _, _, err := repo.Insert(ctx, j); err != nil {
			t.Fatal(err)
		}
		now := base
		for attempt := 1; attempt <= 3; attempt++ {
			claimed, err := repo.Claim(ctx, task.ClaimFilter{Now: now, Limit: 10})
			if err != nil {
				t.Fatal(err)
			}
			var cur *task.Job
			for _, c := range claimed {
				if c.ID == j.ID {
					cur = c
				}
			}
			if cur == nil {
				t.Fatalf("attempt %d: job not claimable (run_at too far?)", attempt)
			}
			retryAt := task.NextRetryAt(cur.Attempt, 30*time.Second, time.Hour, now)
			dead := cur.Attempt >= cur.MaxAttempts
			if err := repo.Fail(ctx, cur.ID, "boom", retryAt, dead, now); err != nil {
				t.Fatal(err)
			}
			after, _ := repo.Get(ctx, cur.ID)
			wantStatus := task.StatusPending
			if dead {
				wantStatus = task.StatusDead
			}
			if after.Status != wantStatus {
				t.Fatalf("attempt %d: status = %s, want %s", attempt, after.Status, wantStatus)
			}
			now = retryAt // 退避到点才可再认领
		}
		// 死信可 Retry。
		revived, err := repo.Retry(ctx, j.ID, now)
		if err != nil || revived.Status != task.StatusPending || revived.Attempt != 0 {
			t.Fatalf("retry: %+v err=%v", revived, err)
		}
		// 非 dead/cancelled 不可 Retry。
		if _, err := repo.Retry(ctx, j.ID, now); !errors.Is(err, task.ErrNotRetryable) {
			t.Fatalf("pending retry err = %v, want ErrNotRetryable", err)
		}
	})

	t.Run("cancel only pending", func(t *testing.T) {
		j := mk("cancel.job", base.Add(time.Hour), "")
		if _, _, err := repo.Insert(ctx, j); err != nil {
			t.Fatal(err)
		}
		if _, err := repo.Cancel(ctx, j.ID, base); err != nil {
			t.Fatal(err)
		}
		if _, err := repo.Cancel(ctx, j.ID, base); !errors.Is(err, task.ErrNotPending) {
			t.Fatalf("second cancel err = %v", err)
		}
	})

	t.Run("stats", func(t *testing.T) {
		if _, err := repo.Stats(ctx); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("list filter and keyset", func(t *testing.T) {
		// 自足种子：种一个直通死信的任务（认领后以 dead=true 落败）。
		victim := mk("dead.job", base, "")
		if _, _, err := repo.Insert(ctx, victim); err != nil {
			t.Fatal(err)
		}
		claimed, err := repo.Claim(ctx, task.ClaimFilter{Now: base, Limit: 10})
		if err != nil {
			t.Fatal(err)
		}
		for _, c := range claimed {
			if c.ID == victim.ID {
				if err := repo.Fail(ctx, c.ID, "fatal", base, true, base); err != nil {
					t.Fatal(err)
				}
			}
		}
		jobs, total, err := repo.List(ctx, task.JobFilter{Status: task.StatusDead}, jobsPlan)
		if err != nil || len(jobs) != 1 || total != 1 {
			t.Fatalf("dead filter: %d/%d err=%v", len(jobs), total, err)
		}
		if jobs[0].Status != task.StatusDead || jobs[0].ID != victim.ID {
			t.Fatalf("filter leaked: %+v", jobs[0])
		}
		// keyset 翻页：每页 2 条推进。
		page1 := jobsPlan
		page1.Limit = 2
		first, _, err := repo.List(ctx, task.JobFilter{}, page1)
		if err != nil || len(first) != 2 {
			t.Fatalf("page1: %d err=%v", len(first), err)
		}
		page2 := page1
		page2.CursorValues = []string{first[len(first)-1].CreatedAt.UTC().Format(time.RFC3339Nano), first[len(first)-1].ID}
		second, _, err := repo.List(ctx, task.JobFilter{}, page2)
		if err != nil {
			t.Fatal(err)
		}
		for _, b := range second {
			for _, a := range first {
				if a.ID == b.ID {
					t.Errorf("pages overlap at %s", a.ID)
				}
			}
		}
	})

	t.Run("recurring rules: upsert, due claim advances, dedupe", func(t *testing.T) {
		now := base
		rule, err := task.NewRecurringRule(idgen.New(), "digest.send", "{}", "@every 1h", true, now)
		if err != nil {
			t.Fatal(err)
		}
		if err := repo.UpsertRule(ctx, rule); err != nil {
			t.Fatal(err)
		}
		// 未到期：认领为空。
		if due, err := repo.ClaimDueRules(ctx, now.Add(30*time.Minute), 10); err != nil || len(due) != 0 {
			t.Fatalf("early claim: %v err=%v", len(due), err)
		}
		// 到期：认领并推进（next_run_at 越过 now+1h）。
		due, err := repo.ClaimDueRules(ctx, now.Add(time.Hour), 10)
		if err != nil || len(due) != 1 {
			t.Fatalf("due claim: %v err=%v", len(due), err)
		}
		if !due[0].NextRunAt.After(now.Add(time.Hour)) {
			t.Fatalf("next_run_at not advanced: %v", due[0].NextRunAt)
		}
		// 同一时刻重复 tick：已被推进，不再认领。
		if due2, _ := repo.ClaimDueRules(ctx, now.Add(time.Hour), 10); len(due2) != 0 {
			t.Fatalf("re-claim at same instant: %v", len(due2))
		}
		// 更新规则重算 next_run_at。
		old, err := repo.GetRule(ctx, rule.ID)
		if err != nil {
			t.Fatal(err)
		}
		old.Enabled = false
		if err := repo.UpsertRule(ctx, old); err != nil {
			t.Fatal(err)
		}
		fresh, _ := repo.GetRule(ctx, rule.ID)
		if fresh.Enabled {
			t.Fatal("rule update not persisted")
		}
		if err := repo.DeleteRule(ctx, rule.ID); err != nil {
			t.Fatal(err)
		}
		if err := repo.DeleteRule(ctx, rule.ID); !errors.Is(err, task.ErrRuleNotFound) {
			t.Fatalf("second delete err = %v", err)
		}
	})
}
