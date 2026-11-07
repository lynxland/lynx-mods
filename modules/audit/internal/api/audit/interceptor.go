package audit

import (
	"context"
	"log/slog"
	"time"

	"github.com/lynxland/appkit/contexts"
	"github.com/lynxland/appkit/idgen"
	"github.com/lynxland/grpcapi/interceptor"
	domainaudit "github.com/lynxland/lynx-start/modules/audit/domain/audit"
	infraaudit "github.com/lynxland/lynx-start/modules/audit/internal/infra/audit"
	"google.golang.org/grpc"
	"google.golang.org/grpc/status"
)

// NewRPCStage 构造 RPC 审计拦截器（Order=OrderAudit=50：认证之后、鉴权
// 之前——记录的调用面与拒绝面都算审计事实）。handler 之后投递异步
// writer，不阻塞响应路径。
func NewRPCStage(writer *infraaudit.Writer, logger *slog.Logger) interceptor.Stage {
	return interceptor.Stage{
		Name:  "audit",
		Order: interceptor.OrderAudit,
		Unary: func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
			start := time.Now()
			resp, err := handler(ctx, req)
			writer.Offer(&domainaudit.Record{
				ID:            idgen.New(),
				ActorID:       contexts.SubjectID(ctx),
				Action:        info.FullMethod,
				Source:        domainaudit.SourceRPC,
				StatusCode:    status.Code(err).String(),
				LatencyMillis: time.Since(start).Milliseconds(),
				OccurredAt:    time.Now(),
			})
			return resp, err
		},
		Stream: func(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
			start := time.Now()
			err := handler(srv, ss)
			writer.Offer(&domainaudit.Record{
				ID:            idgen.New(),
				ActorID:       contexts.SubjectID(ss.Context()),
				Action:        info.FullMethod,
				Source:        domainaudit.SourceRPC,
				StatusCode:    status.Code(err).String(),
				LatencyMillis: time.Since(start).Milliseconds(),
				OccurredAt:    time.Now(),
			})
			return err
		},
	}
}
