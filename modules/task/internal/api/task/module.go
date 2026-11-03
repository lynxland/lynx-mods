package task

import (
	"context"

	"github.com/lynxland/lynx-start/modules/task/domain/task"
	taskv1 "github.com/lynxland/lynx-start/modules/task/genproto/task/v1"
	apptask "github.com/lynxland/lynx-start/modules/task/internal/app/task"
	infratask "github.com/lynxland/lynx-start/modules/task/internal/infra/task"
	"github.com/lynxland/lynx-start/modules/task/internal/infra/task/migrations"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"github.com/lynxland/appkit"
	"github.com/lynxland/appkit/contrib/bunx"
	"github.com/lynxland/grpcapi/server"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// Module 返回静态装配描述。DependsOn: organization——任务必属一个组织，
// 成员资格经其 domain 端口校验（安装器校验依赖已装）。第一个组织挂靠
// 业务资源（ADR-0008）。v0.1 无配置段、不消费事件。
func Module() appkit.Module {
	return appkit.Module{
		Name:        "task",
		Description: "任务模块：组织内任务（创建/查询/完成/删除，完成与删除限创建者），依赖 organization",
		DependsOn:   []string{"organization"},
		Policies:    []protoreflect.FileDescriptor{taskv1.File_task_v1_task_proto},
		Migrations:  migrations.FS,
		PublishedEvents: []string{
			task.EventTaskCreated,
			task.EventTaskCompleted,
		},
	}
}

// Assemble 构造动态装配产物：仓储（本模块 + organization domain 只读
// 适配器）→ 用例 → 处理器 → 注册。源码归应用所有：按需修改。
func Assemble(deps appkit.Deps) (appkit.Assembly, error) {
	uc := apptask.New(
		infratask.NewRepository(bunx.MustDB(deps)),
		infratask.NewOrganizationReader(bunx.MustDB(deps)), // DependsOn: organization domain 只读消费
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
		Name:       "TaskService",
		Descriptor: taskv1.File_task_v1_task_proto.Services().ByName("TaskService"),
		RegisterGRPC: func(s *grpc.Server) {
			taskv1.RegisterTaskServiceServer(s, svc)
		},
		RegisterHTTP: func(ctx context.Context, mux *runtime.ServeMux, conn grpc.ClientConnInterface) error {
			return taskv1.RegisterTaskServiceHandlerClient(ctx, mux, taskv1.NewTaskServiceClient(conn))
		},
	}
}
