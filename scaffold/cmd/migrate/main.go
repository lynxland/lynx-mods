// 迁移执行器：遍历已装模块的迁移链，每模块独立 goose 版本表（契约④）。
// 用法：
//
//	go run ./cmd/migrate -dsn <postgres-dsn>                 # 全部模块 up
//	go run ./cmd/migrate -dsn <dsn> -module demo             # 单模块 up
//	go run ./cmd/migrate -dsn <dsn> -down 1 -module demo     # demo 回退 1 个
//
// DSN 解析顺序：-dsn 旗标 > 环境变量 {{.EnvPrefix}}_DATA_DATABASE_SOURCE >
// configs/config.yaml 的同款缺省（init 生成期烘入，与配置文件同源）。
package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"os"

	"github.com/lynxland/appkit/compose"
	"github.com/lynxland/appkit/goosekit"
	"github.com/uptrace/bun/driver/pgdriver"
	"{{.ModulePath}}/internal/bootstrap"
	"{{.ModulePath}}/modules"
)

// defaultDSN 与 configs/config.yaml 的 data.database.source 缺省同款
// （模板变量 {{.DefaultDSN}}）。改了 config.yaml 的 DSN 后，用环境变量
// 或 -dsn 覆盖，或同步改这里。
const defaultDSN = "{{.DefaultDSN}}"

func main() {
	dsn := flag.String("dsn", firstNonEmpty(os.Getenv("{{.EnvPrefix}}_DATA_DATABASE_SOURCE"), defaultDSN), "Postgres DSN（环境变量 {{.EnvPrefix}}_DATA_DATABASE_SOURCE 优先，缺省 config.yaml 同款）")
	module := flag.String("module", "", "只处理指定模块（空=全部）")
	down := flag.Int("down", 0, "回退最近 N 个迁移（默认 0=执行 up）")
	flag.Parse()

	if *dsn == "" {
		fatal("-dsn 或 {{.EnvPrefix}}_DATA_DATABASE_SOURCE 必填")
	}
	db := sql.OpenDB(pgdriver.NewConnector(pgdriver.WithDSN(*dsn)))
	defer db.Close() //nolint:errcheck

	ctx := context.Background()
	installed := modules.Installed()
	for _, m := range installed {
		if m.Module.Migrations == nil {
			continue
		}
		if *module != "" && m.Module.Name != *module {
			continue
		}
		var err error
		switch {
		case *down > 0:
			err = goosekit.Down(ctx, db, m.Module.Migrations, m.Module.Name, *down)
		default:
			err = goosekit.Up(ctx, db, m.Module.Migrations, m.Module.Name)
		}
		if err != nil {
			fatal(fmt.Errorf("module %s: %w", m.Module.Name, err))
		}
		v, err := goosekit.Version(ctx, db, m.Module.Migrations, m.Module.Name)
		if err != nil {
			fatal(err)
		}
		fmt.Printf("module %-16s version %d\n", m.Module.Name, v)
	}
	_ = compose.ValidateModules(compose.Descriptors(installed))
}

func fatal(v any) {
	fmt.Fprintln(os.Stderr, "migrate:", v)
	if err, ok := v.(error); ok {
		if hint := bootstrap.HintDB(err); hint != "" {
			fmt.Fprintln(os.Stderr, hint)
		}
	}
	os.Exit(1)
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
