# session

会话模块：可吊销登录会话（刷新令牌轮换 + 改密全局吊销），依赖
[identity](../identity/README.md)——identity 的 JWT 无吊销机制，需要「登出/
踢人/改密即失效」时用本模块叠加。

## 安装

```bash
lynx-start add identity           # 先装依赖
lynx-start add session
```

源码装入应用 `modules/session/`，配置段与环境变量占位自动写入；数据库迁移
随安装纳入应用迁移管线。

## 依赖

- 模块依赖：identity >=0.1.1（已装版本需满足）
- Go 依赖（add 时自动钉版）：appkit、appkit/contrib/bunx、grpcapi

## 配置

```yaml
session:
  secret: "dev-only-session-secret-change-me"  # 令牌主密钥（与 identity 独立），生产必须更换
  access_ttl: 15m   # 访问令牌（无状态，吊销生效粒度的上界）
  refresh_ttl: 720h # 会话绝对时长（轮换不续期）
  issuer: lynx-start
```

## 环境变量

| 变量 | 说明 |
| --- | --- |
| `SESSION_SECRET` | 会话令牌主密钥（与 identity 独立；secret：add 生成随机占位，生产必须更换） |

## 发布事件

与 `Module().PublishedEvents` 一致（启动期校验）：

- `session.session.created`
- `session.session.refreshed`
- `session.session.revoked`

---

**English**: Revocable login sessions on top of identity — refresh-token
rotation, global revocation on password change. Install with
`lynx-start add identity && lynx-start add session` (identity >=0.1.1 required).
Publishes `session.session.created` / `refreshed` / `revoked`.
