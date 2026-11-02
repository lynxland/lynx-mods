package audit

import (
	"context"

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

	// List 按 occurred_at 降序分页；params 来自 crud.ParseListParams，
	// 返回 (记录, 总数, 错误)，下一页 token 由用例层 FinalizeOffsetPage 收尾。
	List(ctx context.Context, f Filter, params crud.ListParams) ([]*Record, int, error)
}
