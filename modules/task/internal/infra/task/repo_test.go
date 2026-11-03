package task

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/lynxland/appkit/contrib/bunx/testutil"
	"github.com/lynxland/lynx-start/modules/task/domain/task"
	"github.com/lynxland/lynx-start/modules/task/internal/infra/task/migrations"
)

func TestRepo(t *testing.T) {
	testutil.SkipUnlessPg(t)
	// 本链只含 task_tasks；organization_members 属 organization 迁移链，
	// organizationReader 的读路径按应用侧同库集成验证（对照 organization
	// 的 userReader 同款取舍）。task_tasks 建两行不同组织数据即可验证
	// 租户隔离（不依赖 organization 链）。
	db := testutil.NewDB(t, testutil.Chain{Module: "task", FS: migrations.FS})
	repo := NewRepository(db)
	ctx := context.Background()

	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	mk := func(id, org, creator, title string) *task.Task {
		x, err := task.NewTask(id, org, creator, title, "", t0)
		if err != nil {
			t.Fatal(err)
		}
		return x
	}

	if err := repo.Create(ctx, mk("t1", "o1", "alice", "写周报")); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := repo.Create(ctx, mk("t2", "o1", "bob", "发布清单")); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := repo.Create(ctx, mk("t3", "o2", "carol", "他组织任务")); err != nil {
		t.Fatalf("create: %v", err)
	}

	t.Run("get scoped to organization", func(t *testing.T) {
		got, err := repo.Get(ctx, "o1", "t1")
		if err != nil || got.Title != "写周报" || got.CreatorID != "alice" {
			t.Fatalf("get = %+v err=%v", got, err)
		}
		// 租户隔离：同 ID 换组织取不到（ADR-0008 端口级防漏）。
		if _, err := repo.Get(ctx, "o2", "t1"); !errors.Is(err, task.ErrTaskNotFound) {
			t.Fatalf("cross-org get: err = %v, want ErrTaskNotFound", err)
		}
		if _, err := repo.Get(ctx, "o1", "ghost"); !errors.Is(err, task.ErrTaskNotFound) {
			t.Fatalf("ghost get: err = %v, want ErrTaskNotFound", err)
		}
	})

	t.Run("list scoped to organization", func(t *testing.T) {
		tasks, total, err := repo.List(ctx, "o1", task.Query{PageSize: 10})
		if err != nil || total != 2 || len(tasks) != 2 {
			t.Fatalf("list o1 = %d/%d err=%v", len(tasks), total, err)
		}
		// created_at DESC：同为 t0 时按 id DESC。
		if tasks[0].ID != "t2" {
			t.Errorf("order: first = %s, want t2", tasks[0].ID)
		}
		if tasks2, total2, _ := repo.List(ctx, "o2", task.Query{PageSize: 10}); total2 != 1 || len(tasks2) != 1 {
			t.Errorf("list o2 = %d/%d, want 1/1", len(tasks2), total2)
		}
	})

	t.Run("update whitelist and complete", func(t *testing.T) {
		got, _ := repo.Get(ctx, "o1", "t1")
		done := t0.Add(time.Hour)
		got.Complete(done)
		if err := repo.Update(ctx, got); err != nil {
			t.Fatalf("update: %v", err)
		}
		reread, _ := repo.Get(ctx, "o1", "t1")
		if reread.CompletedAt == nil || !reread.CompletedAt.Equal(done) {
			t.Errorf("completed_at = %v", reread.CompletedAt)
		}
		if !reread.UpdatedAt.Equal(done) {
			t.Errorf("updated_at = %v, want %v", reread.UpdatedAt, done)
		}
	})

	t.Run("delete scoped to organization", func(t *testing.T) {
		removed, err := repo.Delete(ctx, "o2", "t1") // 跨组织删不动
		if err != nil || removed {
			t.Fatalf("cross-org delete = %v/%v, want false", removed, err)
		}
		removed, err = repo.Delete(ctx, "o1", "t1")
		if err != nil || !removed {
			t.Fatalf("delete = %v/%v, want true", removed, err)
		}
		if removed, _ := repo.Delete(ctx, "o1", "t1"); removed { // 幂等
			t.Error("second delete should be no-op")
		}
	})
}
