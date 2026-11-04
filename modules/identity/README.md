# identity

身份模块：注册/登录（Argon2id + HS256 JWT）/自助改密，并向应用贡献 Bearer
认证器——其余模块的「已认证身份」都由它供给，是多数组合的安装起点。

## 安装

```bash
lynx-start add identity           # 最新（registry 工作区形态）
lynx-start add identity@0.5.0     # 锁定 tag 版本
```

源码装入应用 `modules/identity/`，配置段自动追加进 `configs/config.yaml`，
环境变量占位写入 `.env.example`；数据库迁移随安装纳入应用迁移管线。

## 依赖

- 模块依赖：无（零模块依赖）
- kernel 地板：appkit >=0.8.0（add 时校验应用 go.mod）
- Go 依赖（add 时自动钉版）：appkit、appkit/contrib/bunx、grpcapi

## 配置

```yaml
identity:
  secret: "dev-only-identity-secret-change-me"  # 令牌主密钥，生产必须更换
  token_ttl: 30m   # 无吊销机制（v0.1），保持短时效
  issuer: lynx-start
```

## 环境变量

| 变量 | 说明 |
| --- | --- |
| `IDENTITY_SECRET` | 身份令牌签发主密钥（secret：add 生成随机占位，生产必须更换） |

## 发布事件

与 `Module().PublishedEvents` 一致（启动期校验）：

- `identity.user.created`
- `identity.user.password-changed`

需要可吊销会话时叠加 [session](../session/README.md)；需要角色授权时叠加
[access](../access/README.md)。

---

**English**: Authentication module — register/login (Argon2id + HS256 JWT),
self-service password change, and the Bearer authenticator other modules rely
on. Install with `lynx-start add identity`; zero module dependencies, requires
appkit >=0.8.0. Publishes `identity.user.created` / `identity.user.password-changed`.
