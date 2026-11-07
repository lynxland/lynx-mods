package audit

import (
	"context"
	"testing"
	"time"

	domainaudit "github.com/lynxland/lynx-start/modules/audit/domain/audit"

	"github.com/lynxland/appkit/crud"
	"github.com/lynxland/appkit/idgen"
)

// fakeRepo 记录批量写入（Writer 单测用）。
type fakeRepo struct {
	fail  bool
	batch [][]*domainaudit.Record
}

func (f *fakeRepo) AppendBatch(_ context.Context, recs []*domainaudit.Record) error {
	if f.fail {
		return errBoom
	}
	f.batch = append(f.batch, recs)
	return nil
}
func (f *fakeRepo) DeleteBefore(context.Context, time.Time) (int64, error) { return 0, nil }
func (f *fakeRepo) Append(context.Context, *domainaudit.Record) error      { return nil }
func (f *fakeRepo) List(context.Context, domainaudit.Filter, crud.PagePlan) ([]*domainaudit.Record, int, error) {
	return nil, 0, nil
}

type boomError struct{}

func (boomError) Error() string { return "boom" }

var errBoom = boomError{}

func TestWriterFlushAndRetry(t *testing.T) {
	repo := &fakeRepo{}
	w := NewWriter(repo, nil, WriterOptions{Buffer: 8, FlushEvery: 10 * time.Millisecond, BatchSize: 4})
	ctx, cancel := context.WithCancel(context.Background())
	go func() { _ = w.Start(ctx) }()
	for i := 0; i < 5; i++ {
		w.Offer(&domainaudit.Record{ID: idgen.New(), Action: "a", Source: "rpc"})
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if len(repo.batch) > 0 && len(repo.batch[0]) == 5 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	if len(repo.batch) == 0 || len(repo.batch[0]) != 5 {
		t.Fatalf("flushed batches = %v", len(repo.batch))
	}
}

func TestWriterDeadLetterAfterRetry(t *testing.T) {
	repo := &fakeRepo{fail: true}
	w := NewWriter(repo, nil, WriterOptions{Buffer: 8, FlushEvery: 10 * time.Millisecond, BatchSize: 4})
	w.Offer(&domainaudit.Record{ID: "r1", Action: "a", Source: "rpc"})
	ctx, cancel := context.WithCancel(context.Background())
	go func() { _ = w.Start(ctx) }()
	deadline := time.Now().Add(2 * time.Second)
	for w.Dead() == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	if w.Dead() != 1 {
		t.Fatalf("dead = %d, want 1", w.Dead())
	}
}
