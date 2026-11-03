package audit

import (
	"context"
	"log/slog"
	"time"

	"github.com/lynxland/lynx"
	domainaudit "github.com/lynxland/lynx-start/modules/audit/domain/audit"
)

// Writer 是拦截器路径的异步审计写入器：RPC 线程只投递（非阻塞），批量
// flush 由后台 Service 承担。取舍（v0.1，文档已注明）：审计不阻塞业务
// 主路径——channel 满即丢并计数；进程崩溃丢尾部（≤ flush 间隔）。
// API 显式 Record 路径不走这里（同步、与业务同事务）。
type Writer struct {
	ch      chan *domainaudit.Record
	repo    domainaudit.Repository
	logger  *slog.Logger
	dropped int64
}

// NewWriter 构造写入器（buffer 1024 条；flush 间隔 500ms 或满 64 条）。
func NewWriter(repo domainaudit.Repository, logger *slog.Logger) *Writer {
	return &Writer{
		ch:     make(chan *domainaudit.Record, 1024),
		repo:   repo,
		logger: logger,
	}
}

// Offer 非阻塞投递；满则丢弃并计数（业务路径绝不等待审计）。
func (w *Writer) Offer(rec *domainaudit.Record) {
	select {
	case w.ch <- rec:
	default:
		w.dropped++
		if w.dropped == 1 || w.dropped%100 == 0 {
			w.logger.Warn("audit writer: buffer full, dropping records", "dropped_total", w.dropped)
		}
	}
}

var _ lynx.Service = (*Writer)(nil)

// Name 实现 lynx.Service。
func (w *Writer) Name() string { return "audit-writer" }

// Init 实现 lynx.Service（无初始化动作）。
func (w *Writer) Init(lynx.AppContext) error { return nil }

// Start 持续批量落库直至框架关停。
func (w *Writer) Start(ctx context.Context) error {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	batch := make([]*domainaudit.Record, 0, 64)
	for {
		select {
		case <-ctx.Done():
			w.flush(context.Background(), batch)
			return nil
		case rec := <-w.ch:
			batch = append(batch, rec)
			if len(batch) >= 64 {
				w.flush(ctx, batch)
				batch = batch[:0]
			}
		case <-ticker.C:
			w.flush(ctx, batch)
			batch = batch[:0]
		}
	}
}

// Stop 实现 lynx.Service（关停 flush 由 Start 的 ctx.Done 分支完成）。
func (w *Writer) Stop(context.Context) error { return nil }

// flush 批量写入；失败记日志不重试（下轮 ticker 继续；已从 channel 取出
// 的这批失败即丢——审计尽力而为，不反压业务）。
func (w *Writer) flush(ctx context.Context, batch []*domainaudit.Record) {
	if len(batch) == 0 {
		return
	}
	if err := w.repo.AppendBatch(ctx, batch); err != nil {
		w.logger.Error("audit writer: flush failed, batch dropped", "size", len(batch), "error", err)
	}
}
