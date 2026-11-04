# audit

审计模块：RPC 拦截器自动记录 + 显式记录（同事务）+ 分页查询（append-only，
零模块依赖）——审计面不依赖任何业务模块，任何组合可独立叠加。

## 安装

```bash
lynx-start add audit
```

源码装入应用 `modules/audit/`；数据库迁移随安装纳入应用迁移管线。

## 依赖

- 模块依赖：无（零模块依赖）
- Go 依赖（add 时自动钉版）：appkit、appkit/contrib/bunx

## 配置

无配置段（安装后开箱即用）。

## 发布事件

与 `Module().PublishedEvents` 一致（启动期校验）：

- `audit.log.recorded`

---

**English**: Audit trail — automatic RPC-interceptor recording plus explicit
in-transaction recording, with paginated queries over an append-only log.
Zero module dependencies. Install with `lynx-start add audit`.
Publishes `audit.log.recorded`.
