# account

账号与访问模块（ADR-0009：identity+session+access 三模块合并为一个产品
能力单元）——**单独安装即得完整认证授权**：注册/登录（Argon2id + HS256
JWT）、可吊销会话（刷新令牌轮换 + 改密全局吊销）、RBAC（配置角色目录 +
DB 分配），并向应用贡献 Bearer 认证器与 roles/permissions claims 增强器。

模块内保持三个域的分层边界：`domain/account/{identity,session,access}`
与三个 proto 包（API 面与旧三模块完全一致——已装应用的调用代码零改动）。

## 安装

```bash
lynx-start add account           # 最新（registry 工作区形态）
lynx-start add account@0.6.0     # 锁定 tag 版本
```

源码装入应用 `modules/account/`，配置段自动追加进 `configs/config.yaml`，
环境变量占位写入 `.env.example`；数据库迁移随安装纳入应用迁移管线。

## 依赖

- 模块依赖：无（零模块依赖，自足）
- kernel 地板：appkit >=0.8.0（add 时校验应用 go.mod）
- Go 依赖（add 时自动钉版）：appkit、appkit/contrib/bunx、grpcapi、lynx

## 配置

三个段（已装旧三模块的应用段名不变，升级零配置迁移）：

```yaml
identity:
  secret: "dev-only-identity-secret-change-me"  # 令牌主密钥，生产必须更换
  token_ttl: 30m   # 令牌时效（无 session 域时无吊销机制，保持短时效）
  issuer: lynx-start
session:
  secret: "dev-only-session-secret-change-me"  # 令牌主密钥（与 identity 独立），生产必须更换
  access_ttl: 15m   # 访问令牌（无状态，吊销生效粒度的上界）
  refresh_ttl: 720h # 会话绝对时长（轮换不续期）
  issuer: lynx-start
access:
  roles:                      # 角色目录（可审计可改；精确匹配无通配符）
    admin: ["account.assignment.write", "audit.log.read"]
    editor: []
  bootstrap_admins:           # 启动期自动授予 admin 的邮箱（未注册则告警跳过，注册后重启生效）
    - "admin@example.com"
```

默认 admin 角色含 `audit.log.read`——与 audit 模块同装时开箱可查审计。

## 环境变量

| 变量 | 说明 |
| --- | --- |
| `IDENTITY_SECRET` | 身份令牌签发主密钥（secret：add 生成随机占位，生产必须更换） |
| `SESSION_SECRET` | 会话令牌主密钥（与 identity 独立） |

## 发布事件

与 `Module().PublishedEvents` 一致（启动期校验；ADR-0009 起统一 account.*
前缀——目录所有权：事件名首段=模块名）：

- `account.user.created`
- `account.user.password-changed`
- `account.session.created` / `account.session.refreshed` / `account.session.revoked`
- `account.assignment.granted` / `account.assignment.revoked`

模块内联动：改密事件触发该用户全部会话吊销（「改密即全端下线」）。

## 从旧三模块升级（v0.6.0 迁移）

```bash
lynx-start remove access && lynx-start remove session && lynx-start remove identity
lynx-start add account && lynx-start generate && go mod tidy && go run ./cmd/migrate
```

- 数据零迁移：表名/列不变（identity_users / session_sessions /
  access_role_assignments），迁移 00001 全 IF NOT EXISTS；旧 goose 版本表
  （goose_identity_db 等）残留无害，可手工清理
- config.yaml 三段照旧；configFragment 注释里的权限字符串
  `access.assignment.write` 需改为 `account.assignment.write`
- 事件订阅方（notification 等）必须同版升级（事件名改前缀）

---

**English**: Account & access module (ADR-0009 merger of identity+session+
access) — a self-sufficient product capability unit: register/login (Argon2id
+ HS256 JWT), revocable sessions with refresh-token rotation, and RBAC
(config-declared role catalog + DB assignments). Contributes Bearer
authenticators and the roles/permissions claims enricher. API surface is
byte-compatible with the former three modules (same proto packages and
routes); event names moved to the `account.*` prefix.
