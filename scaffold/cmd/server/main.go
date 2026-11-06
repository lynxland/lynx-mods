// {{.AppName}} 服务入口：装配已安装的 lynx-start 模块并托管生命周期。
// 本文件由 lynx-start init 生成，后续基本不再变——模块增减只改 modules/modules.go。
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/lynxland/lynx"
	"github.com/lynxland/lynx/contrib/schedule"
	"github.com/lynxland/lynx/contrib/zap"

	"github.com/lynxland/appkit"
	"github.com/lynxland/appkit/chain"
	"github.com/lynxland/appkit/compose"
	grpcapiauthn "github.com/lynxland/grpcapi/authn"
	grpcapiopenapi "github.com/lynxland/grpcapi/openapi"
	grpcapipolicy "github.com/lynxland/grpcapi/policy"
	grpcapiprotopolicy "github.com/lynxland/grpcapi/policy/protopolicy"
	grpcapiserver "github.com/lynxland/grpcapi/server"
	"google.golang.org/genproto/googleapis/api/annotations"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"

	"{{.ModulePath}}/internal/bootstrap"
	"{{.ModulePath}}/internal/config"
	"{{.ModulePath}}/modules"
)

func main() {
	runner := lynx.NewRunner(setup,
		lynx.WithName("{{.AppName}}"),
		lynx.WithShutdownTimeout(30*time.Second),
		lynx.WithBindConfigFunc(config.BindConfig),
		lynx.WithLoggerProvider(func(app lynx.AppContext) (*slog.Logger, error) {
			return zap.NewLogger(app)
		}),
	)
	runner.Run()
}

func setup(app lynx.App) error {
	logger := app.Logger()

	cfg, err := config.Load(app.Config())
	if err != nil {
		return err
	}
	deps, cleanup, err := bootstrap.NewDeps(app, cfg, logger)
	if err != nil {
		return err
	}
	app.OnPostStop(cleanup)

	installed := modules.Installed()
	agg, err := compose.AssembleAll(deps, installed)
	if err != nil {
		return err
	}

	// 策略集：全部已装模块的 proto 注解（fail-closed 启动断言由 grpcapi 承担）。
	set, err := buildPolicies(app.Context(), installed)
	if err != nil {
		return err
	}

	// 模块贡献的认证后增强器（access 的 roles/permissions claims 等）。
	authnOpts := grpcapiauthn.Options{Enrichers: agg.Enrichers}

	// 模块贡献拦截器 Stage（audit=50 等）时接管链构建——复刻 server 默认链
	// 的知识在 chain.DefaultChain（I-08：修复经 appkit semver 推送，
	// scaffold 生成物不再持有这段易错逻辑）。
	chain, err := chain.DefaultChain(logger, set, agg, authnOpts)
	if err != nil {
		return err
	}

	srv, err := grpcapiserver.New(app.Context(), grpcapiserver.Config{
		Policy:               set,
		Services:             agg.Registrations,
		ExtraRoutes:          agg.ExtraRoutes,
		Authenticators:       agg.Authenticators,
		AuthnOptions:         authnOpts,
		Chain:                chain,
		GRPCAddr:             cfg.Server.GRPCAddr,
		HTTPAddr:             cfg.Server.HTTPAddr,
		AllowNoAuthenticator: len(agg.Authenticators) == 0,
		OpenAPI:              &grpcapiopenapi.Info{Title: "{{.AppName}}", Version: "v1"},
		Logger:               logger,
	})
	if err != nil {
		return err
	}
	app.Register(bootstrap.NewAPIService(srv, logger))

	printRouteTable(installed, set)

	if len(agg.Tasks) > 0 {
		scheduler, err := schedule.NewScheduler(agg.Tasks)
		if err != nil {
			return err
		}
		app.Register(scheduler)
	}
	app.Register(agg.Services...)
	return nil
}

func buildPolicies(ctx context.Context, installed []appkit.InstalledModule) (*grpcapipolicy.Set, error) {
	files := compose.PolicyFiles(compose.Descriptors(installed))
	if len(files) == 0 {
		return grpcapipolicy.NewSet()
	}
	return grpcapipolicy.Build(ctx, grpcapipolicy.FromFiles(files...), grpcapipolicy.Options{
		Provider: grpcapiprotopolicy.New(grpcapiprotopolicy.Options{}),
		Registry: grpcapipolicy.DefaultRegistry(),
	})
}

// printRouteTable 启动期打印 HTTP 路由表（proto 的 google.api.http 注解驱动），
// 免去翻 proto 找路径：方法、路径、RPC 全名与认证要求一屏可见。手写的
// ExtraRoutes 与 gRPC 面（:9060）不在此列。
func printRouteTable(installed []appkit.InstalledModule, set *grpcapipolicy.Set) {
	type routeRow struct{ method, path, rpc, auth string }
	var rows []routeRow
	for _, fd := range compose.PolicyFiles(compose.Descriptors(installed)) {
		services := fd.Services()
		for i := 0; i < services.Len(); i++ {
			sd := services.Get(i)
			methods := sd.Methods()
			for j := 0; j < methods.Len(); j++ {
				md := methods.Get(j)
				rpcName := string(sd.FullName()) + "/" + string(md.Name())
				auth := "auth"
				if set.IsAnonymous("/" + rpcName) {
					auth = "anonymous"
				}
				for _, hr := range httpRules(md) {
					rows = append(rows, routeRow{method: hr[0], path: hr[1], rpc: rpcName, auth: auth})
				}
			}
		}
	}
	if len(rows) == 0 {
		return
	}
	sort.Slice(rows, func(a, b int) bool { return rows[a].path < rows[b].path })
	w := tabwriter.NewWriter(os.Stdout, 2, 0, 2, ' ', 0)
	fmt.Fprintln(w, "HTTP routes:")
	for _, r := range rows {
		fmt.Fprintf(w, "  %s\t%s\t%s\t%s\n", r.method, r.path, r.rpc, r.auth)
	}
	_ = w.Flush()
}

// httpRules 抽取方法的 google.api.http 注解（primary 与 additional_bindings）。
func httpRules(md protoreflect.MethodDescriptor) [][2]string {
	if md.Options() == nil {
		return nil
	}
	rule, ok := proto.GetExtension(md.Options(), annotations.E_Http).(*annotations.HttpRule)
	if !ok || rule == nil {
		return nil
	}
	add := func(r *annotations.HttpRule, out *[][2]string) {
		switch p := r.Pattern.(type) {
		case *annotations.HttpRule_Get:
			*out = append(*out, [2]string{"GET", p.Get})
		case *annotations.HttpRule_Put:
			*out = append(*out, [2]string{"PUT", p.Put})
		case *annotations.HttpRule_Post:
			*out = append(*out, [2]string{"POST", p.Post})
		case *annotations.HttpRule_Delete:
			*out = append(*out, [2]string{"DELETE", p.Delete})
		case *annotations.HttpRule_Patch:
			*out = append(*out, [2]string{"PATCH", p.Patch})
		case *annotations.HttpRule_Custom:
			*out = append(*out, [2]string{strings.ToUpper(p.Custom.GetKind()), p.Custom.GetPath()})
		}
	}
	var out [][2]string
	add(rule, &out)
	for _, ab := range rule.GetAdditionalBindings() {
		add(ab, &out)
	}
	return out
}
