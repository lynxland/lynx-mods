package session

import (
	"context"
	"fmt"

	"github.com/lynxland/lynx-start/modules/session/domain/session"
	sessionv1 "github.com/lynxland/lynx-start/modules/session/genproto/session/v1"
	appsession "github.com/lynxland/lynx-start/modules/session/internal/app/session"
	infrasession "github.com/lynxland/lynx-start/modules/session/internal/infra/session"
	"github.com/lynxland/lynx-start/modules/session/internal/infra/session/migrations"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"github.com/lynxland/appkit"
	"github.com/lynxland/appkit/clock"
	"github.com/lynxland/appkit/configkit"
	"github.com/lynxland/appkit/contrib/bunx"
	"github.com/lynxland/appkit/jwt"
	"github.com/lynxland/grpcapi/authn"
	"github.com/lynxland/grpcapi/server"
	"github.com/lynxland/lynx"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// Module 返回静态装配描述。DependsOn: identity——principal 来源与
// 改密事件目录都是 identity（安装器校验依赖已装）。
func Module() appkit.Module {
	return appkit.Module{
		Name:        "session",
		Description: "会话模块：可吊销登录会话（刷新令牌轮换 + 改密全局吊销），依赖 identity",
		DependsOn:   []string{"identity"},
		Policies:    []protoreflect.FileDescriptor{sessionv1.File_session_v1_session_proto},
		Migrations:  migrations.FS,
		PublishedEvents: []string{
			session.EventSessionCreated,
			session.EventSessionRefreshed,
			session.EventSessionRevoked,
		},
		ConsumedEvents: []string{session.IdentityUserPasswordChanged},
		Config: appkit.ConfigSpec{
			Defaults: map[string]any{
				"secret":      appsession.DefaultDevSecret,
				"access_ttl":  "15m",
				"refresh_ttl": "720h",
				"issuer":      "lynx-start",
			},
		},
	}
}

// Assemble 构造动态装配产物：配置段 → 令牌四件套 → 仓储（本模块 +
// identity domain 只读适配器）→ 用例 → 处理器 + Bearer 认证器 + 改密
// 联动订阅器。源码归应用所有：按需修改。
func Assemble(deps appkit.Deps) (appkit.Assembly, error) {
	var cfg appsession.Config
	if err := configkit.LoadSection(deps.Config, "session", cfg.ApplyDefaults, &cfg); err != nil {
		return appkit.Assembly{}, err
	}
	if err := cfg.Validate(); err != nil {
		return appkit.Assembly{}, err
	}
	if cfg.Secret == appsession.DefaultDevSecret && deps.Logger != nil {
		deps.Logger.Warn("session: 正在使用开发默认密钥（session.secret），生产环境必须更换")
	}
	if deps.Bus == nil {
		return appkit.Assembly{}, fmt.Errorf("session: 需要事件总线（appkit.Deps.Bus）——请更新 scaffold bootstrap.NewDeps")
	}

	tokens := appsession.Tokens{
		Access:        jwt.NewSigner(cfg.Secret, appsession.PurposeAccessToken, cfg.Issuer, cfg.AccessTTL, clock.System{}),
		Refresh:       jwt.NewSigner(cfg.Secret, appsession.PurposeRefreshToken, cfg.Issuer, cfg.RefreshTTL, clock.System{}),
		AccessVerify:  jwt.NewVerifier(cfg.Secret, appsession.PurposeAccessToken, cfg.Issuer),
		RefreshVerify: jwt.NewVerifier(cfg.Secret, appsession.PurposeRefreshToken, cfg.Issuer),
		RefreshTTL:    cfg.RefreshTTL,
	}

	uc := appsession.New(
		infrasession.NewRepository(bunx.MustDB(deps)),
		infrasession.NewUserReader(bunx.MustDB(deps)), // DependsOn: identity domain 只读消费
		deps.Tx, deps.Events, tokens, clock.System{},
	)

	return appkit.Assembly{
		Registrations:  []server.Registration{newRegistration(NewService(uc))},
		Authenticators: []authn.Authenticator{NewBearerAuthenticator(uc, tokens.AccessVerify, clock.System{})},
		Services: []lynx.Service{
			// 改密联动：订阅 identity.user.password-changed，全局吊销该用户会话。
			infrasession.NewPasswordChangedWatcher(deps.Bus, uc, deps.Logger),
		},
	}, nil
}

// newRegistration 手写注册（Name/Descriptor/RegisterGRPC/RegisterHTTP），
// 等价于 protoc-gen-grpcapi 生成物，少一个生成依赖。
func newRegistration(svc *Service) server.Registration {
	return server.Registration{
		API:        "public",
		Name:       "SessionService",
		Descriptor: sessionv1.File_session_v1_session_proto.Services().ByName("SessionService"),
		RegisterGRPC: func(s *grpc.Server) {
			sessionv1.RegisterSessionServiceServer(s, svc)
		},
		RegisterHTTP: func(ctx context.Context, mux *runtime.ServeMux, conn grpc.ClientConnInterface) error {
			return sessionv1.RegisterSessionServiceHandlerClient(ctx, mux, sessionv1.NewSessionServiceClient(conn))
		},
	}
}
