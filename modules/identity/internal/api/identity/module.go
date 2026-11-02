package identity

import (
	"context"

	"github.com/lynxland/lynx-mods/modules/identity/domain/identity"
	identityv1 "github.com/lynxland/lynx-mods/modules/identity/genproto/identity/v1"
	appidentity "github.com/lynxland/lynx-mods/modules/identity/internal/app/identity"
	infraindentity "github.com/lynxland/lynx-mods/modules/identity/internal/infra/identity"
	"github.com/lynxland/lynx-mods/modules/identity/internal/infra/identity/migrations"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"github.com/lynxland/appkit"
	"github.com/lynxland/appkit/clock"
	"github.com/lynxland/appkit/configkit"
	"github.com/lynxland/appkit/contrib/bunx"
	"github.com/lynxland/appkit/jwt"
	"github.com/lynxland/grpcapi/authn"
	"github.com/lynxland/grpcapi/server"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// Module 返回静态装配描述（策略描述符来自本地 buf 生成物）。
func Module() appkit.Module {
	return appkit.Module{
		Name:        "identity",
		Description: "身份模块：注册/登录（Argon2id + HS256 JWT）/自助改密，贡献 Bearer 认证器",
		Policies:    []protoreflect.FileDescriptor{identityv1.File_identity_v1_identity_proto},
		Migrations:  migrations.FS,
		PublishedEvents: []string{
			identity.EventUserCreated,
			identity.EventUserPasswordChanged,
		},
		Config: appkit.ConfigSpec{
			Defaults: map[string]any{
				"secret":    appidentity.DefaultDevSecret,
				"token_ttl": "30m",
				"issuer":    "lynx-mods",
			},
		},
	}
}

// Assemble 构造动态装配产物：配置段 → 令牌签发/验证 → 仓储 → 用例 →
// 处理器 + Bearer 认证器。源码归应用所有：按需修改（例如换密钥来源、
// 追加事件订阅）。
func Assemble(deps appkit.Deps) (appkit.Assembly, error) {
	var cfg appidentity.Config
	if err := configkit.LoadSection(deps.Config, "identity", cfg.ApplyDefaults, &cfg); err != nil {
		return appkit.Assembly{}, err
	}
	if err := cfg.Validate(); err != nil {
		return appkit.Assembly{}, err
	}
	if cfg.Secret == appidentity.DefaultDevSecret && deps.Logger != nil {
		deps.Logger.Warn("identity: 正在使用开发默认密钥（identity.secret），生产环境必须更换")
	}

	// purpose 域隔离：访问令牌的派生用途固定，其他用途的令牌不可通过验证。
	signer := jwt.NewSigner(cfg.Secret, appidentity.PurposeAccessToken, cfg.Issuer, cfg.TokenTTL, clock.System{})
	verifier := jwt.NewVerifier(cfg.Secret, appidentity.PurposeAccessToken, cfg.Issuer)

	repo := infraindentity.NewRepository(bunx.MustDB(deps))
	uc := appidentity.New(repo, deps.Tx, deps.Events, signer, verifier, clock.System{})
	return appkit.Assembly{
		Registrations:  []server.Registration{newRegistration(NewService(uc))},
		Authenticators: []authn.Authenticator{NewBearerAuthenticator(uc, verifier, clock.System{})},
	}, nil
}

// newRegistration 手写注册（Name/Descriptor/RegisterGRPC/RegisterHTTP），
// 等价于 protoc-gen-grpcapi 生成物，少一个生成依赖。
func newRegistration(svc *Service) server.Registration {
	return server.Registration{
		API:        "public",
		Name:       "IdentityService",
		Descriptor: identityv1.File_identity_v1_identity_proto.Services().ByName("IdentityService"),
		RegisterGRPC: func(s *grpc.Server) {
			identityv1.RegisterIdentityServiceServer(s, svc)
		},
		RegisterHTTP: func(ctx context.Context, mux *runtime.ServeMux, conn grpc.ClientConnInterface) error {
			return identityv1.RegisterIdentityServiceHandlerClient(ctx, mux, identityv1.NewIdentityServiceClient(conn))
		},
	}
}
