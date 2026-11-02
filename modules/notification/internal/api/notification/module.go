package notification

import (
	"context"
	"fmt"

	"github.com/lynx-go/lynx-mods/modules/notification/domain/notification"
	notificationv1 "github.com/lynx-go/lynx-mods/modules/notification/genproto/notification/v1"
	appnotification "github.com/lynx-go/lynx-mods/modules/notification/internal/app/notification"
	infranotification "github.com/lynx-go/lynx-mods/modules/notification/internal/infra/notification"
	"github.com/lynx-go/lynx-mods/modules/notification/internal/infra/notification/migrations"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"github.com/lynx-go/grpcapi/server"
	"github.com/lynx-go/lynx"
	"github.com/lynxland/appkit"
	"github.com/lynxland/appkit/contrib/bunx"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// Module 返回静态装配描述。DependsOn: identity+organization——本模块消费
// 其事件目录（ConsumedEvents 启动期交叉校验发布者在装；安装器校验依赖
// 已装）。Go 代码不 import 两者的包（契约②字符串目录消费）。v0.1 无
// 配置段、不发布事件。
func Module() appkit.Module {
	return appkit.Module{
		Name:        "notification",
		Description: "通知模块：订阅身份/组织事件生成站内通知（渲染快照落库）+ 自助查询与已读标记，依赖 identity+organization",
		DependsOn:   []string{"identity", "organization"},
		Policies:    []protoreflect.FileDescriptor{notificationv1.File_notification_v1_notification_proto},
		Migrations:  migrations.FS,
		ConsumedEvents: []string{
			notification.IdentityUserPasswordChanged,
			notification.OrganizationMemberAdded,
			notification.OrganizationMemberRemoved,
		},
	}
}

// Assemble 构造动态装配产物：仓储 → 用例 → 处理器 + 事件订阅器。源码归
// 应用所有：按需修改。
func Assemble(deps appkit.Deps) (appkit.Assembly, error) {
	if deps.Bus == nil {
		return appkit.Assembly{}, fmt.Errorf("notification: 需要事件总线（appkit.Deps.Bus）——请更新 scaffold bootstrap.NewDeps")
	}
	uc := appnotification.New(infranotification.NewRepository(bunx.MustDB(deps)))
	return appkit.Assembly{
		Registrations: []server.Registration{newRegistration(NewService(uc))},
		Services: []lynx.Service{
			// 事件→通知联动：订阅 identity.user.password-changed 与
			// organization.member.*，落站内通知。
			infranotification.NewWatcher(deps.Bus, uc, deps.Logger),
		},
	}, nil
}

// newRegistration 手写注册（Name/Descriptor/RegisterGRPC/RegisterHTTP），
// 等价于 protoc-gen-grpcapi 生成物，少一个生成依赖。
func newRegistration(svc *Service) server.Registration {
	return server.Registration{
		API:        "public",
		Name:       "NotificationService",
		Descriptor: notificationv1.File_notification_v1_notification_proto.Services().ByName("NotificationService"),
		RegisterGRPC: func(s *grpc.Server) {
			notificationv1.RegisterNotificationServiceServer(s, svc)
		},
		RegisterHTTP: func(ctx context.Context, mux *runtime.ServeMux, conn grpc.ClientConnInterface) error {
			return notificationv1.RegisterNotificationServiceHandlerClient(ctx, mux, notificationv1.NewNotificationServiceClient(conn))
		},
	}
}
