# 部署指引（Deployment）

> 面向把应用带出 dev 的第一次上线：部署物、生产配置、TLS、备份。
> 模块能力（账号/审计等）的边界见各模块 README 的「已知限制」。

## 部署物

- **Dockerfile**：init 生成的应用根自带（多阶段：mise 钉版构建 →
  debian-slim 单二进制；`docker build -t myapp .`）。
- 二进制产物：`./cmd/server`（服务）与 `./cmd/migrate`（迁移执行器）。
- 健康探测：`GET /healthz`（进程存活）与 `GET /readyz`（**真实探活**——
  接 DB ping，滚动发布的就绪依据）；gRPC 侧 health 协议在 :9060。

## 生产配置

配置两通道（`configs/config.yaml` + `<APP>_` 前缀环境变量覆盖），
生产建议**环境变量承载全部 secret**（容器编排的 secret 注入/K8s Secret
直通）——config.yaml 进版本库可以不含任何敏感值。

必检清单（dev 能跑、上线必查）：

| 项 | 位置 | 说明 |
|---|---|---|
| `identity.secret` / `session.secret` | env `{{.EnvPrefix}}_IDENTITY_SECRET` 等 | 开发默认值启动时有 Warn；**生产必须换**（随机 32+ 字节） |
| `data.database.source` | env `{{.EnvPrefix}}_DATA_DATABASE_SOURCE` | 生产 DSN（含 `sslmode=require`） |
| `server.cors.allowed_origins` | config.yaml | 浏览器前端必配（精确 origin，不支持 `*` 携带凭据）；非浏览器客户端无需 |
| `server.access_log` | config.yaml | 流量画像（审计模块已记录全部 RPC——这是补充不是替代） |
| `notification.smtp.*` | env `{{.EnvPrefix}}_NOTIFICATION_SMTP_PASSWORD` 等 | 邮件渠道；host 空则禁用（仅站内） |

## TLS

服务自身支持 TLS（grpcapi `Config.TLS`），**推荐形态是 TLS 终止在反向
代理/网关**（cert-manager/ALB/云网关），应用内网明文。反向代理要点：

- HTTP :8080（REST/gateway）与 gRPC :9060 分别转发（gRPC 需 HTTP/2）；
- `X-Forwarded-For` 透传（访问日志/审计的客户端来源）。

## 迁移与发布

1. 发布新版本前先跑 `./migrate`（ goose 各模块独立版本表，幂等）；
2. 滚动发布以 `/readyz` 为就绪门（DB 不可达时不接流量）；
3. 优雅停机：SIGTERM 后在途请求排水（框架级 30s 预算）——audit 的异步
   写入器同窗口 flush。

## 备份与恢复

- PG 常规备份即可覆盖全部模块数据（无外部存储）：
  `pg_dump --format=custom $DSN > backup.dump`；恢复 `pg_restore`。
  迁移表（goose_*_db）随库备份——恢复后版本状态自洽。
- 演练建议：每季度一次「恢复到临时库 + migrate + 冒烟」全流程。

## 已知运营边界（诚实清单）

- 单进程 modular monolith：事件/限流计数/幂等缓存均为进程内——多实例
  水平扩缩**不在当前语义**（模块 README 的 Non-goals）；
- 纯 identity（无 session）部署下，禁用用户的已发访问令牌在 TTL 内
  残留（上界=令牌时效）；装 session 后经事件即时吊销；
- SMTP 事务邮件为标准库最小实现（无附件/批量/DKIM）——营销类邮件
  接 ESP（应用侧自建发送通道，notification 的模板与偏好仍可用）。
