# task

任务模块：组织内任务（创建/查询/完成/删除，完成与删除限创建者）——首个
组织挂靠业务资源，展示「组织成员边界内的业务资源」姿势，依赖
[organization](../organization/README.md)。

## 安装

```bash
lynx-start add account             # 先装依赖链（organization 依赖 account）
lynx-start add organization
lynx-start add task
```

源码装入应用 `modules/task/`；数据库迁移随安装纳入应用迁移管线。

## 依赖

- 模块依赖：organization >=0.1.0（已装版本需满足）
- kernel 地板：appkit >=0.8.0（add 时校验应用 go.mod）
- Go 依赖（add 时自动钉版）：appkit、appkit/contrib/bunx

## 配置

无配置段（安装后开箱即用）。

## 发布事件

与 `Module().PublishedEvents` 一致（启动期校验）：

- `task.task.created`
- `task.task.completed`

---

**English**: Tasks scoped to an organization — create/list/complete/delete,
with completion and deletion restricted to the creator. The first
organization-scoped business resource. Install chain:
`lynx-start add account && lynx-start add organization && lynx-start add task`
(organization >=0.1.0 required). Publishes `task.task.created` / `task.task.completed`.
