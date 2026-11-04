# demo

样例模块：单实体 CRUD，验证装配管线（不承载业务语义）——新应用/新姿势的
冒烟用例与模块作者的参照实现。

## 安装

```bash
lynx-start add demo
```

源码装入应用 `modules/demo/`；数据库迁移随安装纳入应用迁移管线。

## 依赖

- 模块依赖：无（零模块依赖）
- Go 依赖（add 时自动钉版）：appkit、appkit/contrib/bunx

## 配置

无配置段（安装后开箱即用）。

## 发布事件

与 `Module().PublishedEvents` 一致（启动期校验）：

- `demo.item.created`
- `demo.item.renamed`
- `demo.item.deleted`

写自己的模块时以本模块为骨架起步，流程见
[module-authoring](../../docs/module-authoring.md)。

---

**English**: Sample module — single-entity CRUD to smoke-test the assembly
pipeline. Zero module dependencies, no business semantics. Install with
`lynx-start add demo`; also the reference implementation for module authors.
Publishes `demo.item.created` / `renamed` / `deleted`.
