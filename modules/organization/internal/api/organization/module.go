package organization

import (
	"context"

	"github.com/lynx-go/lynx-mods/modules/organization/domain/organization"
	organizationv1 "github.com/lynx-go/lynx-mods/modules/organization/genproto/organization/v1"
	apporganization "github.com/lynx-go/lynx-mods/modules/organization/internal/app/organization"
	infraorganization "github.com/lynx-go/lynx-mods/modules/organization/internal/infra/organization"
	"github.com/lynx-go/lynx-mods/modules/organization/internal/infra/organization/migrations"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"github.com/lynx-go/grpcapi/server"
	"github.com/lynxland/appkit"
	"github.com/lynxland/appkit/contrib/bunx"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// Module 返回静态装配描述。DependsOn: identity——成员是 identity 用户
// （只存 ID），添加成员时经 identity domain 只读端口校验用户存在
// （安装器校验依赖已装）。v0.1 无配置段。
func Module() appkit.Module {
	return appkit.Module{
		Name:        "organization",
		Description: "组织模块：组织与成员事实（创建者即 owner，owner-only 管理），依赖 identity",
		DependsOn:   []string{"identity"},
		Policies:    []protoreflect.FileDescriptor{organizationv1.File_organization_v1_organization_proto},
		Migrations:  migrations.FS,
		PublishedEvents: []string{
			organization.EventOrganizationCreated,
			organization.EventMemberAdded,
			organization.EventMemberRemoved,
		},
	}
}

// Assemble 构造动态装配产物：仓储（本模块 + identity domain 只读适配器）
// → 用例 → 处理器 → 注册。源码归应用所有：按需修改。
func Assemble(deps appkit.Deps) (appkit.Assembly, error) {
	uc := apporganization.New(
		infraorganization.NewRepository(bunx.MustDB(deps)),
		infraorganization.NewUserReader(bunx.MustDB(deps)), // DependsOn: identity domain 只读消费
		deps.Tx, deps.Events,
	)
	return appkit.Assembly{
		Registrations: []server.Registration{newRegistration(NewService(uc))},
	}, nil
}

// newRegistration 手写注册（Name/Descriptor/RegisterGRPC/RegisterHTTP），
// 等价于 protoc-gen-grpcapi 生成物，少一个生成依赖。
func newRegistration(svc *Service) server.Registration {
	return server.Registration{
		API:        "public",
		Name:       "OrganizationService",
		Descriptor: organizationv1.File_organization_v1_organization_proto.Services().ByName("OrganizationService"),
		RegisterGRPC: func(s *grpc.Server) {
			organizationv1.RegisterOrganizationServiceServer(s, svc)
		},
		RegisterHTTP: func(ctx context.Context, mux *runtime.ServeMux, conn grpc.ClientConnInterface) error {
			return organizationv1.RegisterOrganizationServiceHandlerClient(ctx, mux, organizationv1.NewOrganizationServiceClient(conn))
		},
	}
}
