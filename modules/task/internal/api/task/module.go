package task

import (
	"context"
	"log/slog"

	"github.com/lynxland/lynx-start/modules/task/domain/task"
	taskv1 "github.com/lynxland/lynx-start/modules/task/genproto/task/v1"
	apptask "github.com/lynxland/lynx-start/modules/task/internal/app/task"
	infratask "github.com/lynxland/lynx-start/modules/task/internal/infra/task"
	"github.com/lynxland/lynx-start/modules/task/internal/infra/task/migrations"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"github.com/lynxland/appkit"
	"github.com/lynxland/appkit/configkit"
	"github.com/lynxland/appkit/contrib/bunx"
	"github.com/lynxland/grpcapi/server"
	"github.com/lynxland/lynx"
	"github.com/uptrace/bun"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// Module 返回静态装配描述。零模块依赖（ADR-0009 自足模块）：job 属于
// 应用全局，不带组织/租户维度。
func Module() appkit.Module {
	return appkit.Module{
		Name:        "task",
		Description: "后台任务与调度：持久化任务队列（即时/延迟/定时入队、唯一键防重、退避重试、死信）+ cron 周期规则；worker handler 经桥接包由应用接线",
		Policies:    []protoreflect.FileDescriptor{taskv1.File_task_v1_task_proto},
		Migrations:  migrations.FS,
		PublishedEvents: []string{
			task.EventJobEnqueued,
			task.EventJobCompleted,
			task.EventJobFailed,
			task.EventJobDead,
		},
		Config: appkit.ConfigSpec{
			Defaults: map[string]any{
				"scheduler_interval": "30s",
			},
		},
	}
}

// Assemble 构造动态装配产物：管理面 RPC（TaskService）+ 周期调度器
// （recurring → 入队，自治无需 handler）。**worker runner 不在此**——
// 它必须携带应用的 handler 表，经 NewRunner 由应用组合根构造（转发
// 写法见模块 README「接线 runner」）。
func Assemble(deps appkit.Deps) (appkit.Assembly, error) {
	var cfg apptask.Config
	if err := configkit.LoadSection(deps.Config, "task", cfg.ApplyDefaults, &cfg); err != nil {
		return appkit.Assembly{}, err
	}
	db := bunx.MustDB(deps)
	uc := apptask.New(infratask.NewRepository(db), deps.Events)
	return appkit.Assembly{
		Registrations: []server.Registration{newRegistration(NewService(uc))},
		Services: []lynx.Service{
			infratask.NewScheduler(infratask.NewRepository(db), cfg.SchedulerInterval, deps.Logger),
		},
	}, nil
}

// NewRunner 转发 worker 执行器的构造（应用组合根经模块桥接包调用：
// `func NewRunner(db *bun.DB, handlers domain task.HandlerRegistry, ...)`
// ——桥接包注入物形态，应用按需追加转发行；见 README）。
func NewRunner(db *bun.DB, handlers task.HandlerRegistry, logger *slog.Logger, opts infratask.RunnerOptions) *infratask.Runner {
	return infratask.NewRunner(db, handlers, nil, opts)
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
