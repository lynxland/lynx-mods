# notification

通知模块：订阅身份/组织事件自动生成站内通知（渲染快照落库）+ 自助查询与
已读标记，依赖 [account](../account/README.md) 与
[organization](../organization/README.md)——纯消费方，自身不发布事件。

## 安装

```bash
lynx-start add account             # 先装依赖链
lynx-start add organization
lynx-start add notification
```

源码装入应用 `modules/notification/`；数据库迁移随安装纳入应用迁移管线。

## 依赖

- 模块依赖：account >=0.6.0、organization >=0.1.1（已装版本需满足）
- kernel 地板：appkit >=0.8.0（add 时校验应用 go.mod）
- Go 依赖（add 时自动钉版）：appkit、appkit/contrib/bunx

## 配置

无配置段（安装后开箱即用）。

## 事件

不发布事件。应用装配期订阅三个上游事件生成通知（联动表见
`internal/api/module.go`）：

- `account.user.password-changed`
- `organization.member.added`
- `organization.member.removed`

---

**English**: In-app notifications — subscribes to account/organization events,
renders and persists a snapshot per notification, with self-service listing and
read-marking. Pure consumer (publishes no events). Install chain:
`lynx-start add account && lynx-start add organization && lynx-start add notification`.
