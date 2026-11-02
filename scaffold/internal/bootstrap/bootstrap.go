// Package bootstrap 构造应用基础层：数据库连接、共享依赖（appkit.Deps）与
// 托管 grpcapi Server 的 lynx.Service。模块经 Deps 消费这一切。
package bootstrap

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"strings"
	"time"

	grpcapiserver "github.com/lynxland/grpcapi/server"
	"github.com/lynxland/lynx"
	"github.com/lynxland/appkit"
	"github.com/lynxland/appkit/contrib/bunx/buntx"
	"github.com/lynxland/appkit/events"
	"{{.ModulePath}}/internal/config"

	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/pgdialect"
	"github.com/uptrace/bun/driver/pgdriver"
)

// NewDeps 打开数据库并构造模块共享依赖；返回的 cleanup 关闭连接池
// （main.go 挂 OnPostStop）。
func NewDeps(app lynx.App, cfg *config.Config, logger *slog.Logger) (appkit.Deps, func(), error) {
	sqldb := sql.OpenDB(pgdriver.NewConnector(
		pgdriver.WithDSN(cfg.Data.Database.Source),
	))
	if cfg.Data.Database.MaxOpenConns > 0 {
		sqldb.SetMaxOpenConns(cfg.Data.Database.MaxOpenConns)
	}
	if err := sqldb.Ping(); err != nil {
		_ = sqldb.Close()
		if hint := HintDB(err); hint != "" {
			return appkit.Deps{}, nil, fmt.Errorf("ping database: %w\n%s", err, hint)
		}
		return appkit.Deps{}, nil, fmt.Errorf("ping database: %w", err)
	}
	db := bun.NewDB(sqldb, pgdialect.New())

	deps := appkit.Deps{
		DB:     db,
		Tx:     buntx.New(db),
		Events: events.NewBusPublisher(app.Bus()),
		Bus:    app.Bus(),
		Config: app.Config(),
		Logger: logger,
	}
	return deps, func() { _ = db.Close() }, nil
}

// HintDB 对最常见的本地连接失败形态（端口被既有 Postgres 占用→撞上别家
// 凭据）给出排障提示；migrate 与 server 启动共用。错误形态不匹配时返回空串。
func HintDB(err error) string {
	if err == nil {
		return ""
	}
	s := err.Error()
	for _, marker := range []string{
		"SASL", "password authentication failed",
		"connection refused", "dial tcp", "failed to initialize",
	} {
		if strings.Contains(s, marker) {
			return "hint: another local Postgres may already own this port — set POSTGRES_PORT in .env and re-run `mise run db:up`, or point data.database.source at your server"
		}
	}
	return ""
}

// APIService 把 grpcapi Server 托管进 lynx 生命周期：Start 阻塞运行
// gRPC+HTTP 双面，Stop 触发优雅关停。
type APIService struct {
	srv    *grpcapiserver.Server
	logger *slog.Logger
}

// NewAPIService 构造 APIService。
func NewAPIService(srv *grpcapiserver.Server, logger *slog.Logger) *APIService {
	return &APIService{srv: srv, logger: logger}
}

// Name 实现 lynx.Service。
func (s *APIService) Name() string { return "api" }

// Init 实现 lynx.Service。
func (s *APIService) Init(_ lynx.AppContext) error { return nil }

// Start 实现 lynx.Service：Run 在 ctx 取消时返回。
func (s *APIService) Start(ctx context.Context) error {
	return s.srv.Run(ctx)
}

// Stop 实现 lynx.Service：Run 返回后的 Shutdown 幂等安全。
func (s *APIService) Stop(ctx context.Context) error {
	stopCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	if err := s.srv.Shutdown(stopCtx); err != nil {
		s.logger.Warn("api shutdown", "error", err)
	}
	return nil
}
