# {{.AppName}}

基于 [lynx-mods](https://github.com/lynxland/lynx-mods) 模块基建的 Go 应用：
lynx 运行时 + grpcapi 双面（gRPC + HTTP gateway）+ bun/Postgres + goose 迁移。
English version below.

## 已装模块

见 `lynx-mods.json`（安装清单）与 `modules/modules.go`（装配登记）。

## 快速开始

```bash
mise install && mise trust  # 工具链（go / buf）
mise run generate           # proto → genproto/（必须先于 go mod tidy）
go mod tidy
mise run db:up              # 本地 Postgres（可选：cp .env.example .env 覆盖端口/DSN）
mise run db:migrate         # 各模块迁移链（独立版本表）
mise run dev                # gRPC :9060 / HTTP :8080
```

服务启动时会打印 HTTP 路由表（方法 / 路径 / 认证要求），无需翻 proto 找路径；
`GET /openapi.json` 同步提供 OpenAPI 3.0 文档（policy 安全语义已内含，可直接接 Swagger UI）。

## 添加模块

```bash
lynx-mods list             # 查看模块目录
lynx-mods add <module>
```

## 写自己的模块

已装模块是源码，可直接改；要新增业务域，照 `demo` 模块的四层结构手写并登记到
`modules/modules.go`。完整步骤见
[module-authoring 指南](https://github.com/lynxland/lynx-mods/blob/main/docs/module-authoring.md)
（[English](https://github.com/lynxland/lynx-mods/blob/main/docs/module-authoring.en.md)）。

## 配置

`configs/config.yaml` + 环境变量覆盖（前缀 `{{.EnvPrefix}}_`）：
如 `data.database.source` → `{{.EnvPrefix}}_DATA_DATABASE_SOURCE`。

## 迁移

```bash
mise run db:migrate                              # 全部模块 up（DSN 缺省同 config.yaml）
go run ./cmd/migrate -dsn <dsn> -module demo     # 单模块
go run ./cmd/migrate -dsn <dsn> -down 1 -module demo
```

---

# {{.AppName}} (English)

A Go application built on the [lynx-mods](https://github.com/lynxland/lynx-mods)
module infrastructure: lynx runtime + grpcapi dual surface (gRPC + HTTP
gateway) + bun/Postgres + goose migrations.

## Installed modules

See `lynx-mods.json` (install manifest) and `modules/modules.go`
(assembly registry).

## Quick start

```bash
mise install && mise trust  # toolchain (go / buf)
mise run generate           # proto → genproto/ (must run before go mod tidy)
go mod tidy
mise run db:up              # local Postgres (optional: cp .env.example .env to override port/DSN)
mise run db:migrate         # module migration chains (isolated version tables)
mise run dev                # gRPC :9060 / HTTP :8080
```

On startup the server prints an HTTP route table (method / path / auth
requirement) — no need to dig through protos for paths; `GET /openapi.json`
serves an OpenAPI 3.0 document (policy security semantics included — plug it
straight into Swagger UI).

## Adding modules

```bash
lynx-mods list             # browse the module catalog
lynx-mods add <module>
```

## Writing your own module

Installed modules are source code — edit them freely. To add a new business
domain, follow the `demo` module's four-layer structure and register it in
`modules/modules.go`. Full walkthrough:
[module authoring guide](https://github.com/lynxland/lynx-mods/blob/main/docs/module-authoring.en.md)
([中文](https://github.com/lynxland/lynx-mods/blob/main/docs/module-authoring.md)).

## Configuration

`configs/config.yaml` + environment variable overrides (prefix
`{{.EnvPrefix}}_`): e.g. `data.database.source` →
`{{.EnvPrefix}}_DATA_DATABASE_SOURCE`.

## Migrations

```bash
mise run db:migrate                              # all modules up (DSN defaults to config.yaml)
go run ./cmd/migrate -dsn <dsn> -module demo     # single module
go run ./cmd/migrate -dsn <dsn> -down 1 -module demo
```
