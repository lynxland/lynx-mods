package notification

import (
	"context"
	"fmt"

	domainnotification "github.com/lynxland/lynx-start/modules/notification/domain/notification"
	notificationv1 "github.com/lynxland/lynx-start/modules/notification/genproto/notification/v1"
	appnotification "github.com/lynxland/lynx-start/modules/notification/internal/app/notification"
	infranotification "github.com/lynxland/lynx-start/modules/notification/internal/infra/notification"
	"github.com/lynxland/lynx-start/modules/notification/internal/infra/notification/migrations"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"github.com/lynxland/appkit"
	"github.com/lynxland/appkit/configkit"
	"github.com/lynxland/appkit/contrib/bunx"
	"github.com/lynxland/grpcapi/server"
	"github.com/lynxland/lynx"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// Module 返回静态装配描述。DependsOn: account+organization——本模块消费
// 其事件目录（ConsumedEvents 启动期交叉校验发布者在装；安装器校验依赖
// 已装）。Go 代码不 import 两者的包（契约②字符串目录消费）。v0.1 无
// 配置段、不发布事件。
func Module() appkit.Module {
	return appkit.Module{
		Name:        "notification",
		Description: "通知模块：多渠道通知（站内+邮件、模板文案、投递偏好）+ 事件订阅联动 + 显式发送，依赖 account+organization",
		DependsOn:   []string{"account", "organization"},
		Policies:    []protoreflect.FileDescriptor{notificationv1.File_notification_v1_notification_proto},
		Migrations:  migrations.FS,
		ConsumedEvents: []string{
			// account 的一次性令牌流（v0.7.0）：验证/重置令牌经邮件投递。
			domainnotification.AccountUserVerificationRequested,
			domainnotification.AccountUserResetRequested,
			domainnotification.IdentityUserPasswordChanged,
			domainnotification.OrganizationMemberAdded,
			domainnotification.OrganizationMemberRemoved,
		},
	}
}

// Assemble 构造动态装配产物：配置（模板+SMTP）→ 渠道（站内必在；邮件
// 视 SMTP 配置，未配置降级禁用）→ 用例 → 处理器 + 事件订阅器。源码归
// 应用所有：按需修改。
func Assemble(deps appkit.Deps) (appkit.Assembly, error) {
	if deps.Bus == nil {
		return appkit.Assembly{}, fmt.Errorf("notification: 需要事件总线（appkit.Deps.Bus）——请更新 scaffold bootstrap.NewDeps")
	}
	var cfg appnotification.Config
	if err := configkit.LoadSection(deps.Config, "notification", cfg.ApplyDefaults, &cfg); err != nil {
		return appkit.Assembly{}, err
	}
	db := bunx.MustDB(deps)
	repo := infranotification.NewRepository(db)

	// 渠道装配：站内必在；邮件渠道收件地址经 account 的 identity 域
	// 只读端口解析（通道②），SMTP 未配置（host 空）时降级禁用。
	smtpCfg := infranotification.SMTPConfig{
		Host: cfg.SMTP.Host, Port: cfg.SMTP.Port,
		Username: cfg.SMTP.Username, Password: cfg.SMTP.Password, From: cfg.SMTP.From,
	}
	channels := []domainnotification.Channel{infranotification.NewInAppChannel(repo)}
	if email := infranotification.NewEmailChannel(smtpCfg, infranotification.NewUserReader(db)); email != nil {
		channels = append(channels, email)
	} else if deps.Logger != nil {
		deps.Logger.Info("notification: smtp 未配置，邮件渠道禁用（仅站内投递）")
	}

	uc := appnotification.New(repo, channels, cfg, deps.Logger)
	return appkit.Assembly{
		Registrations: []server.Registration{newRegistration(NewService(uc))},
		Services: []lynx.Service{
			// 事件→通知联动：订阅 account.user.password-changed 与
			// organization.member.*，经投递管线（偏好过滤+多渠道）。
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
