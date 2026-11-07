package audit

import (
	"context"
	"log/slog"
	"time"

	"github.com/lynxland/appkit/periodic"
	"github.com/lynxland/lynx"
	domainaudit "github.com/lynxland/lynx-start/modules/audit/domain/audit"
)

// WriterOptions 是异步写入器的运行参数（v0.7.0 起配置化；零值取默认——
// 与 v0.6.0 硬编码一致）。
type WriterOptions struct {
	Buffer     int           // 投递缓冲条数（默认 1024）
	FlushEvery time.Duration // flush 间隔（默认 500ms）
	BatchSize  int           // 每批落库条数（默认 64）
}

func (o *WriterOptions) applyDefaults() {
	if o.Buffer <= 0 {
		o.Buffer = 1024
	}
	if o.FlushEvery <= 0 {
		o.FlushEvery = 500 * time.Millisecond
	}
	if o.BatchSize <= 0 {
		o.BatchSize = 64
	}
}

// Writer 是拦截器路径的异步审计写入器：RPC 线程只投递（非阻塞），批量
// flush 由后台 Service 承担。取舍（文档已注明）：审计不阻塞业务主路径
// ——channel 满即丢并计数（dropped 日志可见）；进程崩溃丢尾部（≤
// flush 间隔）；flush 失败同批重试一次，再失败记死信日志丢弃（v0.7.0
// 前为直接丢——重试显著降低瞬时库抖动下的丢失面）。
// API 显式 Record 路径不走这里（同步、与业务同事务）。
type Writer struct {
	ch      chan *domainaudit.Record
	repo    domainaudit.Repository
	logger  *slog.Logger
	opts    WriterOptions
	dropped int64
	dead    int64
}

// NewWriter 构造写入器。
func NewWriter(repo domainaudit.Repository, logger *slog.Logger, opts WriterOptions) *Writer {
	opts.applyDefaults()
	if logger == nil {
		logger = slog.Default()
	}
	return &Writer{
		ch:     make(chan *domainaudit.Record, opts.Buffer),
		repo:   repo,
		logger: logger,
		opts:   opts,
	}
}

// Dropped 返回累计丢弃数（Offer 满溢；运维探针用）。
func (w *Writer) Dropped() int64 { return w.dropped }

// Dead 返回累计死信数（flush 两试皆败；运维探针用）。
func (w *Writer) Dead() int64 { return w.dead }

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
	ticker := time.NewTicker(w.opts.FlushEvery)
	defer ticker.Stop()
	batch := make([]*domainaudit.Record, 0, w.opts.BatchSize)
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
	err := w.repo.AppendBatch(ctx, batch)
	if err != nil {
		time.Sleep(50 * time.Millisecond) // 同批重试一次（瞬时库抖动吸收）
		err = w.repo.AppendBatch(ctx, batch)
	}
	if err != nil {
		w.dead += int64(len(batch))
		w.logger.Error("audit writer: flush failed twice, batch dead-lettered",
			"size", len(batch), "dead_total", w.dead, "error", err)
	}
}

// NewRetention 构造保留期清理任务（periodic 电池驱动；Assembly.Services
// 贡献——每 6h 扫一轮，删 retention_days 天前的记录；0 = 禁用返回 nil）。
// 范本：appkit-demo 金丝雀的 auditSweep（应用级作业下沉为模块能力）。
func NewRetention(repo domainaudit.Repository, retentionDays int, logger *slog.Logger) *periodic.Periodic {
	if retentionDays <= 0 {
		return nil
	}
	return periodic.New("audit-retention", 6*time.Hour, time.Minute, func(ctx context.Context) error {
		cutoff := time.Now().UTC().AddDate(0, 0, -retentionDays)
		n, err := repo.DeleteBefore(ctx, cutoff)
		if err != nil {
			return err
		}
		if n > 0 {
			logger.Info("audit retention: purged aged records", "cutoff", cutoff, "purged", n)
		}
		return nil
	}, logger)
}
