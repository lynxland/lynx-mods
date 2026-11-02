package audit

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/lynxland/appkit/contrib/bunx/testutil"
	"github.com/lynxland/appkit/crud"
	domainaudit "github.com/lynxland/lynx-mods/modules/audit/domain/audit"
	"github.com/lynxland/lynx-mods/modules/audit/internal/infra/audit/migrations"
)

func newRec(t *testing.T, id, actorID, action string, at time.Time) *domainaudit.Record {
	t.Helper()
	rec, err := domainaudit.NewRecord(id, actorID, "", action, "order", "o-"+id, json.RawMessage(`{"k":1}`), domainaudit.SourceAPI, at)
	if err != nil {
		t.Fatal(err)
	}
	return rec
}

func TestRepo(t *testing.T) {
	testutil.SkipUnlessPg(t)
	db := testutil.NewDB(t, testutil.Chain{Module: "audit", FS: migrations.FS})
	repo := NewRepository(db)
	ctx := context.Background()

	base := time.Now().UTC().Truncate(time.Millisecond)
	for i, rec := range []*domainaudit.Record{
		newRec(t, "a1", "u1", "order.created", base),
		newRec(t, "a2", "u1", "order.cancelled", base.Add(time.Second)),
		newRec(t, "a3", "u2", "order.created", base.Add(2*time.Second)),
	} {
		if err := repo.Append(ctx, rec); err != nil {
			t.Fatalf("append %d: %v", i, err)
		}
	}

	t.Run("list ordered desc", func(t *testing.T) {
		recs, total, err := repo.List(ctx, domainaudit.Filter{}, crud.ListParams{PageSize: 10})
		if err != nil {
			t.Fatal(err)
		}
		if total != 3 || len(recs) != 3 {
			t.Fatalf("total=%d len=%d", total, len(recs))
		}
		if recs[0].ID != "a3" || recs[2].ID != "a1" {
			t.Errorf("order = %s,%s,%s", recs[0].ID, recs[1].ID, recs[2].ID)
		}
		// jsonb 读回会重序列化（键后加空格），语义比较而非字节比较。
		var payload map[string]any
		if err := json.Unmarshal(recs[0].Payload, &payload); err != nil || payload["k"] != float64(1) {
			t.Errorf("payload = %s (err %v)", recs[0].Payload, err)
		}
	})

	t.Run("list filter actor/action", func(t *testing.T) {
		recs, total, err := repo.List(ctx, domainaudit.Filter{ActorID: "u1"}, crud.ListParams{PageSize: 10})
		if err != nil || total != 2 || len(recs) != 2 {
			t.Fatalf("actor filter: total=%d len=%d err=%v", total, len(recs), err)
		}
		recs, total, err = repo.List(ctx, domainaudit.Filter{ActorID: "u1", Action: "order.cancelled"}, crud.ListParams{PageSize: 10})
		if err != nil || total != 1 || recs[0].ID != "a2" {
			t.Fatalf("actor+action filter: total=%d err=%v", total, err)
		}
		recs, total, err = repo.List(ctx, domainaudit.Filter{ResourceType: "order", ResourceID: "o-a1"}, crud.ListParams{PageSize: 10})
		if err != nil || total != 1 || recs[0].ID != "a1" {
			t.Fatalf("resource filter: total=%d err=%v", total, err)
		}
	})

	t.Run("list paging", func(t *testing.T) {
		recs, total, err := repo.List(ctx, domainaudit.Filter{}, crud.ListParams{PageSize: 2})
		if err != nil || total != 3 || len(recs) != 2 || recs[0].ID != "a3" {
			t.Fatalf("page1: len=%d total=%d err=%v", len(recs), total, err)
		}
		recs, _, err = repo.List(ctx, domainaudit.Filter{}, crud.ListParams{PageSize: 2, Offset: 2})
		if err != nil || len(recs) != 1 || recs[0].ID != "a1" {
			t.Fatalf("page2: len=%d err=%v", len(recs), err)
		}
	})

	t.Run("append batch", func(t *testing.T) {
		batch := []*domainaudit.Record{
			newRec(t, "b1", "u9", "bulk.one", base.Add(time.Minute)),
			newRec(t, "b2", "u9", "bulk.two", base.Add(time.Minute)),
		}
		if err := repo.AppendBatch(ctx, batch); err != nil {
			t.Fatal(err)
		}
		if err := repo.AppendBatch(ctx, nil); err != nil {
			t.Fatal("empty batch must be a no-op")
		}
		_, total, err := repo.List(ctx, domainaudit.Filter{ActorID: "u9"}, crud.ListParams{PageSize: 10})
		if err != nil || total != 2 {
			t.Fatalf("batch visible: total=%d err=%v", total, err)
		}
	})
}
