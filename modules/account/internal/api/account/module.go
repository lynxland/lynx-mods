// Package account 是账号与访问模块（ADR-0009：identity+session+access
// 合并为一个产品能力单元——单独安装即得完整的注册/登录/会话/RBAC 能力）
// 的装配层：三个域的 API 服务注册、认证器/增强器与事件订阅在此聚合为
// 单一模块装配面。域内各自保持 domain/app/infra 分层，跨域经 domain 端口
// （合并前的跨模块通道②随之内化为域内依赖）。
package account

import (
	"context"

	domainaccess "github.com/lynxland/lynx-start/modules/account/domain/account/access"
	domainidentity "github.com/lynxland/lynx-start/modules/account/domain/account/identity"
	domainsession "github.com/lynxland/lynx-start/modules/account/domain/account/session"
	accessv1 "github.com/lynxland/lynx-start/modules/account/genproto/access/v1"
	identityv1 "github.com/lynxland/lynx-start/modules/account/genproto/identity/v1"
	sessionv1 "github.com/lynxland/lynx-start/modules/account/genproto/session/v1"
	apiaccess "github.com/lynxland/lynx-start/modules/account/internal/api/account/access"
	apiidentity "github.com/lynxland/lynx-start/modules/account/internal/api/account/identity"
	apisession "github.com/lynxland/lynx-start/modules/account/internal/api/account/session"
	appaccess "github.com/lynxland/lynx-start/modules/account/internal/app/account/access"
	appidentity "github.com/lynxland/lynx-start/modules/account/internal/app/account/identity"
	appsession "github.com/lynxland/lynx-start/modules/account/internal/app/account/session"
	infraaccess "github.com/lynxland/lynx-start/modules/account/internal/infra/account/access"
	infraindentity "github.com/lynxland/lynx-start/modules/account/internal/infra/account/identity"
	"github.com/lynxland/lynx-start/modules/account/internal/infra/account/migrations"
	infrasession "github.com/lynxland/lynx-start/modules/account/internal/infra/account/session"

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

// Module 返回静态装配描述（策略描述符来自本地 buf 生成物）。事件目录
// 统一 account.* 前缀（目录所有权：compose 校验事件名首段=模块名）。
// 配置保持三个段（identity/session/access）——已装应用的 config.yaml
// 段名不变，升级零配置迁移。
func Module() appkit.Module {
	return appkit.Module{
		Name:        "account",
		Description: "账号与访问：注册/登录（Argon2id + HS256 JWT）/可吊销会话（刷新令牌轮换）/RBAC（配置角色目录 + DB 分配），贡献 Bearer 认证器与 claims 增强器",
		Policies: []protoreflect.FileDescriptor{
			identityv1.File_identity_v1_identity_proto,
			sessionv1.File_session_v1_session_proto,
			accessv1.File_access_v1_access_proto,
		},
		Migrations: migrations.FS,
		PublishedEvents: []string{
			domainidentity.EventUserCreated,
			domainidentity.EventUserPasswordChanged,
			domainsession.EventSessionCreated,
			domainsession.EventSessionRefreshed,
			domainsession.EventSessionRevoked,
			domainaccess.EventAssignmentGranted,
			domainaccess.EventAssignmentRevoked,
		},
		ConsumedEvents: []string{
			// 模块内联动（合并前的跨模块订阅随 ADR-0009 内化）：
			// identity 改密 → session 全局吊销该用户会话。
			domainidentity.EventUserPasswordChanged,
		},
		Config: appkit.ConfigSpec{
			Defaults: map[string]any{
				"identity": map[string]any{
					"secret":    appidentity.DefaultDevSecret,
					"token_ttl": "30m",
					"issuer":    "lynx-start",
				},
				"session": map[string]any{
					"secret":      appsession.DefaultDevSecret,
					"access_ttl":  "15m",
					"refresh_ttl": "720h",
					"issuer":      "lynx-start",
				},
				"access": map[string]any{
					"roles":            map[string]any{"admin": []any{"account.assignment.write", "audit.log.read"}},
					"bootstrap_admins": []any{},
				},
			},
		},
	}
}

// Assemble 构造动态装配产物：依次装配三个域（配置段 → 域仓储/用例 →
// 处理器），identity 域仓储作为用户事实源被 session/access 复用（合并前
// 各自的只读适配器随之退役），最后聚合 Registrations（三个 service）、
// Authenticators（identity 与 session 各贡献一个 Bearer 认证器，链式
// fallthrough：签发密钥不同即签名不匹配，自然交给下一个）、Enricher
// （roles/permissions claims 注入）与改密联动订阅器。源码归应用所有：
// 按需修改。
func Assemble(deps appkit.Deps) (appkit.Assembly, error) {
	db := bunx.MustDB(deps)

	// —— identity 域 ——
	var idcfg appidentity.Config
	if err := configkit.LoadSection(deps.Config, "identity", idcfg.ApplyDefaults, &idcfg); err != nil {
		return appkit.Assembly{}, err
	}
	if err := idcfg.Validate(); err != nil {
		return appkit.Assembly{}, err
	}
	if idcfg.Secret == appidentity.DefaultDevSecret && deps.Logger != nil {
		deps.Logger.Warn("account: 正在使用 identity 开发默认密钥（identity.secret），生产环境必须更换")
	}
	// purpose 域隔离：访问令牌的派生用途固定，其他用途的令牌不可通过验证。
	identitySigner := jwt.NewSigner(idcfg.Secret, appidentity.PurposeAccessToken, idcfg.Issuer, idcfg.TokenTTL, clock.System{})
	identityVerifier := jwt.NewVerifier(idcfg.Secret, appidentity.PurposeAccessToken, idcfg.Issuer)
	identityRepo := infraindentity.NewRepository(db)
	identityUC := appidentity.New(identityRepo, deps.Tx, deps.Events, identitySigner, identityVerifier, clock.System{})

	// —— session 域 ——
	var sescfg appsession.Config
	if err := configkit.LoadSection(deps.Config, "session", sescfg.ApplyDefaults, &sescfg); err != nil {
		return appkit.Assembly{}, err
	}
	if err := sescfg.Validate(); err != nil {
		return appkit.Assembly{}, err
	}
	if sescfg.Secret == appsession.DefaultDevSecret && deps.Logger != nil {
		deps.Logger.Warn("account: 正在使用 session 开发默认密钥（session.secret），生产环境必须更换")
	}
	if deps.Bus == nil {
		return appkit.Assembly{}, errBusRequired
	}
	tokens := appsession.Tokens{
		Access:        jwt.NewSigner(sescfg.Secret, appsession.PurposeAccessToken, sescfg.Issuer, sescfg.AccessTTL, clock.System{}),
		Refresh:       jwt.NewSigner(sescfg.Secret, appsession.PurposeRefreshToken, sescfg.Issuer, sescfg.RefreshTTL, clock.System{}),
		AccessVerify:  jwt.NewVerifier(sescfg.Secret, appsession.PurposeAccessToken, sescfg.Issuer),
		RefreshVerify: jwt.NewVerifier(sescfg.Secret, appsession.PurposeRefreshToken, sescfg.Issuer),
		RefreshTTL:    sescfg.RefreshTTL,
	}
	sessionUC := appsession.New(
		infrasession.NewRepository(db),
		identityRepo, // 用户事实源（Refresh 边界 fail-closed 回读）
		deps.Tx, deps.Events, tokens, clock.System{},
	)

	// —— access 域 ——
	var accfg appaccess.Config
	if err := configkit.LoadSection(deps.Config, "access", accfg.ApplyDefaults, &accfg); err != nil {
		return appkit.Assembly{}, err
	}
	if err := accfg.Validate(); err != nil {
		return appkit.Assembly{}, err
	}
	accessUC := appaccess.New(
		infraaccess.NewRepository(db),
		identityRepo, // Grant 目标校验与 bootstrap 邮箱解析
		deps.Tx, deps.Events, accfg.Roles, nil,
	)
	// bootstrap 管理员收敛（幂等；未注册用户告警跳过，注册后重启生效）。
	if len(accfg.BootstrapAdmins) > 0 {
		onSkip := func(email string) {}
		if deps.Logger != nil {
			onSkip = func(email string) {
				deps.Logger.Warn("account: bootstrap 管理员未注册，跳过（注册后重启生效）", "email", email)
			}
		}
		if err := accessUC.BootstrapAdmins(context.Background(), accfg.BootstrapAdmins, onSkip); err != nil {
			return appkit.Assembly{}, err
		}
	}

	return appkit.Assembly{
		Registrations: []server.Registration{
			identityRegistration(apiidentity.NewService(identityUC)),
			sessionRegistration(apisession.NewService(sessionUC)),
			accessRegistration(apiaccess.NewService(accessUC)),
		},
		Authenticators: []authn.Authenticator{
			apiidentity.NewBearerAuthenticator(identityUC, identityVerifier, clock.System{}),
			apisession.NewBearerAuthenticator(sessionUC, tokens.AccessVerify, clock.System{}),
		},
		Enrichers: []authn.Enricher{apiaccess.NewPrincipalEnricher(accessUC)},
		Services: []lynx.Service{
			// 改密联动：订阅 account.user.password-changed，全局吊销该用户会话。
			infrasession.NewPasswordChangedWatcher(deps.Bus, sessionUC, deps.Logger),
		},
	}, nil
}

// errBusRequired 是事件总线缺失的固定错误（Assemble 期 fail-fast）。
var errBusRequired = errbus{}

type errbus struct{}

func (errbus) Error() string {
	return "account: 需要事件总线（appkit.Deps.Bus）——请更新 scaffold bootstrap.NewDeps"
}

// identityRegistration 手写注册（Name/Descriptor/RegisterGRPC/RegisterHTTP），
// 等价于 protoc-gen-grpcapi 生成物，少一个生成依赖。
func identityRegistration(svc *apiidentity.Service) server.Registration {
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

// sessionRegistration 手写注册（同 identityRegistration 说明）。
func sessionRegistration(svc *apisession.Service) server.Registration {
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

// accessRegistration 手写注册（同 identityRegistration 说明）。
func accessRegistration(svc *apiaccess.Service) server.Registration {
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
