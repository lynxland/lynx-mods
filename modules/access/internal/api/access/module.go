package access

import (
	"context"

	"github.com/lynxland/lynx-mods/modules/access/domain/access"
	accessv1 "github.com/lynxland/lynx-mods/modules/access/genproto/access/v1"
	appaccess "github.com/lynxland/lynx-mods/modules/access/internal/app/access"
	infraaccess "github.com/lynxland/lynx-mods/modules/access/internal/infra/access"
	"github.com/lynxland/lynx-mods/modules/access/internal/infra/access/migrations"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"github.com/lynxland/appkit"
	"github.com/lynxland/appkit/configkit"
	"github.com/lynxland/appkit/contrib/bunx"
	"github.com/lynxland/grpcapi/authn"
	"github.com/lynxland/grpcapi/server"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// Module 返回静态装配描述。DependsOn: identity——分配目标是 identity
// 用户，bootstrap 管理员经其 domain 端口解析。
func Module() appkit.Module {
	return appkit.Module{
		Name:        "access",
		Description: "授权模块：RBAC（配置角色目录 + DB 分配），认证后注入 roles/permissions claims",
		DependsOn:   []string{"identity"},
		Policies:    []protoreflect.FileDescriptor{accessv1.File_access_v1_access_proto},
		Migrations:  migrations.FS,
		PublishedEvents: []string{
			access.EventAssignmentGranted,
			access.EventAssignmentRevoked,
		},
		Config: appkit.ConfigSpec{
			Defaults: map[string]any{
				"roles":            map[string]any{"admin": []any{"access.assignment.write"}},
				"bootstrap_admins": []any{},
			},
		},
	}
}

// Assemble 构造动态装配产物：配置段（角色目录）→ 仓储（分配 + identity
// 只读适配器）→ 用例 → bootstrap 管理员收敛 → 处理器 + Principal
// 增强器。源码归应用所有：按需修改（例如给 Enricher 加缓存）。
func Assemble(deps appkit.Deps) (appkit.Assembly, error) {
	var cfg appaccess.Config
	if err := configkit.LoadSection(deps.Config, "access", cfg.ApplyDefaults, &cfg); err != nil {
		return appkit.Assembly{}, err
	}
	if err := cfg.Validate(); err != nil {
		return appkit.Assembly{}, err
	}

	uc := appaccess.New(
		infraaccess.NewRepository(bunx.MustDB(deps)),
		infraaccess.NewUserReader(bunx.MustDB(deps)), // DependsOn: identity domain 只读消费
		deps.Tx, deps.Events, cfg.Roles, nil,
	)

	// bootstrap 管理员收敛（幂等；未注册用户告警跳过，注册后重启生效）。
	if len(cfg.BootstrapAdmins) > 0 {
		onSkip := func(email string) {}
		if deps.Logger != nil {
			onSkip = func(email string) {
				deps.Logger.Warn("access: bootstrap 管理员未注册，跳过（注册后重启生效）", "email", email)
			}
		}
		if err := uc.BootstrapAdmins(context.Background(), cfg.BootstrapAdmins, onSkip); err != nil {
			return appkit.Assembly{}, err
		}
	}

	return appkit.Assembly{
		Registrations: []server.Registration{newRegistration(NewService(uc))},
		Enrichers:     []authn.Enricher{NewPrincipalEnricher(uc)},
	}, nil
}

// newRegistration 手写注册（Name/Descriptor/RegisterGRPC/RegisterHTTP），
// 等价于 protoc-gen-grpcapi 生成物，少一个生成依赖。
func newRegistration(svc *Service) server.Registration {
	return server.Registration{
		API:        "public",
		Name:       "AccessService",
		Descriptor: accessv1.File_access_v1_access_proto.Services().ByName("AccessService"),
		RegisterGRPC: func(s *grpc.Server) {
			accessv1.RegisterAccessServiceServer(s, svc)
		},
		RegisterHTTP: func(ctx context.Context, mux *runtime.ServeMux, conn grpc.ClientConnInterface) error {
			return accessv1.RegisterAccessServiceHandlerClient(ctx, mux, accessv1.NewAccessServiceClient(conn))
		},
	}
}
