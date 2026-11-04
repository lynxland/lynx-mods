# organization

组织模块：组织与成员事实（创建者即 owner，owner-only 管理；结构非 RBAC——
权限语义归 [access](../access/README.md)），依赖
[identity](../identity/README.md)。组织内挂靠业务资源（如
[task](../task/README.md)）以其成员事实为边界。

## 安装

```bash
lynx-start add identity           # 先装依赖
lynx-start add organization
```

源码装入应用 `modules/organization/`；数据库迁移随安装纳入应用迁移管线。

## 依赖

- 模块依赖：identity >=0.1.1（已装版本需满足）
- kernel 地板：appkit >=0.8.0（add 时校验应用 go.mod）
- Go 依赖（add 时自动钉版）：appkit、appkit/contrib/bunx、grpcapi

## 配置

无配置段（安装后开箱即用）。

## 发布事件

与 `Module().PublishedEvents` 一致（启动期校验）：

- `organization.organization.created`
- `organization.member.added`
- `organization.member.removed`

---

**English**: Organizations and membership facts — creator becomes owner,
owner-only management; structure only (permission semantics live in access).
Install with `lynx-start add identity && lynx-start add organization`
(identity >=0.1.1 required). Publishes `organization.organization.created` /
`organization.member.added` / `organization.member.removed`.
