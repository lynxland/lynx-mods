package audit

import (
	"context"
	"time"

	"github.com/lynxland/appkit/crud"
)

// Repository 是审计存储端口。append-only：端口形状即约束（没有 Update/
// Delete 可言）；List 语义为 occurred_at 降序 + 结构化过滤 + 分页
// （token 由 appkit crud 签发校验）。
type Repository interface {
	// Append 写入记录（加入调用方事务语境）。
	Append(ctx context.Context, rec *Record) error

	// AppendBatch 批量写入（异步 writer 的 flush 通道）。
	AppendBatch(ctx context.Context, recs []*Record) error

	// DeleteBefore 删除 cutoff 之前的记录（保留期清理），返回删除数。
	DeleteBefore(ctx context.Context, cutoff time.Time) (int64, error)
	// List 按分页计划（crud v2 keyset：occurred_at DESC + id 决胜）返回
	// 一页与总数；过滤经 Filter.FilterDigest 绑定游标。
	List(ctx context.Context, f Filter, plan crud.PagePlan) ([]*Record, int, error)
}
