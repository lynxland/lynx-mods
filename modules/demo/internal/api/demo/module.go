package demo

import (
	"context"

	"github.com/lynxland/lynx-mods/modules/demo/domain/demo"
	demov1 "github.com/lynxland/lynx-mods/modules/demo/genproto/demo/v1"
	appdemo "github.com/lynxland/lynx-mods/modules/demo/internal/app/demo"
	infrademo "github.com/lynxland/lynx-mods/modules/demo/internal/infra/demo"
	"github.com/lynxland/lynx-mods/modules/demo/internal/infra/demo/migrations"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"github.com/lynxland/appkit"
	"github.com/lynxland/appkit/contrib/bunx"
	"github.com/lynxland/grpcapi/server"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// Module 返回静态装配描述（策略描述符来自本地 buf 生成物）。
func Module() appkit.Module {
	return appkit.Module{
		Name:        "demo",
		Description: "样例模块：单实体 CRUD，验证装配管线（不承载业务语义）",
		Policies:    []protoreflect.FileDescriptor{demov1.File_demo_v1_demo_proto},
		Migrations:  migrations.FS,
		PublishedEvents: []string{
			demo.EventItemCreated,
			demo.EventItemRenamed,
			demo.EventItemDeleted,
		},
	}
}

// Assemble 构造动态装配产物：仓储 → 用例 → 处理器 → 注册。
// 源码归应用所有：按需修改（例如换掉 newID、追加事件订阅）。
func Assemble(deps appkit.Deps) (appkit.Assembly, error) {
	uc := appdemo.New(infrademo.NewRepository(bunx.MustDB(deps)), deps.Tx, deps.Events)
	svc := NewService(uc)
	return appkit.Assembly{
		Registrations: []server.Registration{newRegistration(svc)},
	}, nil
}

// newRegistration 手写注册（Name/Descriptor/RegisterGRPC/RegisterHTTP），
// 等价于 protoc-gen-grpcapi 生成物，少一个生成依赖。
func newRegistration(svc *Service) server.Registration {
	return server.Registration{
		API:        "public",
		Name:       "DemoService",
		Descriptor: demov1.File_demo_v1_demo_proto.Services().ByName("DemoService"),
		RegisterGRPC: func(s *grpc.Server) {
			demov1.RegisterDemoServiceServer(s, svc)
		},
		RegisterHTTP: func(ctx context.Context, mux *runtime.ServeMux, conn grpc.ClientConnInterface) error {
			return demov1.RegisterDemoServiceHandlerClient(ctx, mux, demov1.NewDemoServiceClient(conn))
		},
	}
}
