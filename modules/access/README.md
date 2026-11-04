# access

授权模块：RBAC（配置角色目录 + DB 分配），认证后注入 roles/permissions
claims，依赖 [identity](../identity/README.md)——身份归 identity，权限语义
归本模块。

## 安装

```bash
lynx-start add identity           # 先装依赖
lynx-start add access
```

源码装入应用 `modules/access/`，配置段自动追加；数据库迁移随安装纳入应用
迁移管线。

## 依赖

- 模块依赖：identity（无版本约束）
- Go 依赖（add 时自动钉版）：appkit、appkit/contrib/bunx

## 配置

```yaml
access:
  roles:                      # 角色目录（可审计可改；精确匹配无通配符）
    admin: ["access.assignment.write"]
    editor: []
  bootstrap_admins:           # 启动期自动授予 admin 的邮箱（未注册则告警跳过，注册后重启生效）
    - "admin@example.com"
```

## 发布事件

与 `Module().PublishedEvents` 一致（启动期校验）：

- `access.assignment.granted`
- `access.assignment.revoked`

组织维度的成员事实用 [organization](../organization/README.md)（结构），
本模块管「谁能做什么」（权限）。

---

**English**: RBAC authorization — a config-declared role catalog plus DB-backed
assignments, injecting roles/permissions claims after authentication.
Install with `lynx-start add identity && lynx-start add access`.
Publishes `access.assignment.granted` / `access.assignment.revoked`.
