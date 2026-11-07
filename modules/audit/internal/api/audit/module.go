// Package audit 是审计模块的 API 层：显式记录/查询处理器、RPC 自动审计
// 拦截器与装配入口（Module/Assemble）。
package audit

import (
	"context"

	domainaudit "github.com/lynxland/lynx-start/modules/audit/domain/audit"
	auditv1 "github.com/lynxland/lynx-start/modules/audit/genproto/audit/v1"
	appaudit "github.com/lynxland/lynx-start/modules/audit/internal/app/audit"
	infraaudit "github.com/lynxland/lynx-start/modules/audit/internal/infra/audit"
	"github.com/lynxland/lynx-start/modules/audit/internal/infra/audit/migrations"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"github.com/lynxland/appkit"
	"github.com/lynxland/appkit/configkit"
	"github.com/lynxland/appkit/contrib/bunx"
	"github.com/lynxland/grpcapi/interceptor"
	"github.com/lynxland/grpcapi/server"
	"github.com/lynxland/lynx"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// Module 返回静态装配描述。零 DependsOn：审计只依赖 appkit 契约
// （Principal 语境/事件信封），主体与权限来自任何已装认证/授权模块。
func Module() appkit.Module {
	return appkit.Module{
		Name:        "audit",
		Description: "审计模块：RPC 拦截器自动记录 + 显式记录（同事务）+ 分页查询（append-only）",
		Policies:    []protoreflect.FileDescriptor{auditv1.File_audit_v1_audit_proto},
		Migrations:  migrations.FS,
		PublishedEvents: []string{
			domainaudit.EventLogRecorded,
		},
	}
}

// Assemble 构造动态装配产物：配置 → 仓储 → 用例 → 处理器 + 异步写入器
// （拦截器路径的落库 Service；参数配置化）+ RPC 审计拦截器（Order=
// OrderAudit，appkit 预留槽位；记录 status/latency）+ 保留期清理任务
// （retention_days > 0 时贡献 periodic Service）。
func Assemble(deps appkit.Deps) (appkit.Assembly, error) {
	var cfg appaudit.Config
	if err := configkit.LoadSection(deps.Config, "audit", cfg.ApplyDefaults, &cfg); err != nil {
		return appkit.Assembly{}, err
	}
	repo := infraaudit.NewRepository(bunx.MustDB(deps))
	uc := appaudit.New(repo, deps.Tx, deps.Events, nil)
	writer := infraaudit.NewWriter(repo, deps.Logger, infraaudit.WriterOptions{
		Buffer: cfg.WriterBuffer, FlushEvery: cfg.WriterFlushEvery, BatchSize: cfg.WriterBatchSize,
	})
	services := []lynx.Service{writer}
	if retention := infraaudit.NewRetention(repo, cfg.RetentionDays, deps.Logger); retention != nil {
		services = append(services, retention)
	}
	return appkit.Assembly{
		Registrations: []server.Registration{newRegistration(NewService(uc))},
		Services:      services,
		Stages:        []interceptor.Stage{NewRPCStage(writer, deps.Logger)},
	}, nil
}

// newRegistration 手写注册（Name/Descriptor/RegisterGRPC/RegisterHTTP）。
func newRegistration(svc *Service) server.Registration {
	return server.Registration{
		API:        "public",
		Name:       "AuditService",
		Descriptor: auditv1.File_audit_v1_audit_proto.Services().ByName("AuditService"),
		RegisterGRPC: func(s *grpc.Server) {
			auditv1.RegisterAuditServiceServer(s, svc)
		},
		RegisterHTTP: func(ctx context.Context, mux *runtime.ServeMux, conn grpc.ClientConnInterface) error {
			return auditv1.RegisterAuditServiceHandlerClient(ctx, mux, auditv1.NewAuditServiceClient(conn))
		},
	}
}
