# lynx-mods

[![CI](https://github.com/lynxland/lynx-mods/actions/workflows/ci.yml/badge.svg)](https://github.com/lynxland/lynx-mods/actions/workflows/ci.yml)

**lynx 生态的 shadcn 式模块分发**：registry + 安装 CLI + 源码模块。为任何基于
lynx 的 Go 后端应用提供按需安装的业务域源码——安装后代码归你所有，随便改。

**shadcn-style module distribution for the lynx ecosystem**: a registry, an
installer CLI, and business modules you install as source code you own.

> 范式基座（装配契约 Module/Assembly/Deps、事务/分页/错误码等电池）在姊妹项目
> **[lynxland/appkit](https://github.com/lynxland/appkit)**（2026-11-02 前名
> lynx-start）——本仓的模块编译期实现它的契约（semver）。本仓由 lynx-start
> 拆分而来（2026-10-31，见
> [ADR-0006](docs/adr/0006-lynx-mods-split-from-lynx-start.md)）。

## 定位：三层供给（承 ADR-0003）

| 层 | 形态 | 稳定性承诺 |
|---|---|---|
| 框架层 | lynx / grpcapi / bun / goose（本来就是库） | 随上游 |
| 契约层 | **appkit**（装配协议、电池、contrib/bunx 适配） | **semver** |
| **源码模块**（`modules/`，registry items） | 一个业务域的四层实现（domain/app/api/infra + proto + 迁移 + 配置段） | 无承诺——源码即你的 |

「稳定」由契约层承担（编译期类型契约），「可扩展」由源码模块承担（改的是自己的
代码，不是插件钩子），「演化」由 registry 承担（git tag 版本 + 安装基线三方合并
更新，安全敏感实现下沉契约层经 semver 推送修复）。
shadcn/ui 是参照系：Radix 是库、组件是源码、registry 是分发协议。

## 快速开始

```bash
# 安装 CLI
go install github.com/lynxland/lynx-mods/cli/cmd/lynx-mods@latest

# registry 两种形态：本仓库目录，或 git URL（自动 clone 到 ~/.cache/lynx-mods 并增量刷新）
export LYNXMODS_REGISTRY=https://github.com/lynxland/lynx-mods.git

# 生成应用骨架 + 安装模块
lynx-mods init myapp --module github.com/acme/myapp --registry <本仓库目录>
cd myapp
lynx-mods add demo --registry <本仓库目录>          # 样例：验证装配管线
lynx-mods init myapp-saas --preset saas --registry <本仓库目录>  # 一条命令装齐 SaaS 底座
lynx-mods add identity@0.2.0 --registry <本仓库目录>  # 钉版本安装（git tag）
lynx-mods add session --registry <本仓库目录>        # 会话：可吊销令牌对（依赖 identity，安装器校验）
lynx-mods add access --registry <本仓库目录>         # 授权：RBAC + claims 注入（依赖 identity）
lynx-mods add audit --registry <本仓库目录>          # 审计：RPC 拦截器自动记录 + 显式记录 + 查询（零依赖）
lynx-mods add organization --registry <本仓库目录>   # 组织：组织与成员事实（依赖 identity）
lynx-mods add task --registry <本仓库目录>           # 任务：第一个组织挂靠业务资源（ADR-0005 姿势）

# registry 模块出新版时（三方合并：本地 / 安装基线 / upstream）
git add -A && git commit -m wip    # update 写盘不可回滚，先 commit
lynx-mods update identity --registry <本仓库目录>

# 体检与卸载
lynx-mods doctor --registry <本仓库目录>
lynx-mods audit --registry <本仓库目录>      # 安全公告门禁：命中已知公告即非零退出（CI 用）
lynx-mods remove audit --registry <本仓库目录>  # DAG 反向校验；迁移表保留（数据安全）

# 跑起来（顺序重要：genproto 未生成时 go mod tidy 必失败；mise 钉版 go/buf）
mise install && mise trust
lynx-mods generate       # buf dep update + lint + generate → genproto/
go mod tidy              # 契约库（appkit semver）与模块依赖在此拉取
mise run db:up && mise run db:migrate
mise run dev             # gRPC :9060 / HTTP :8080
```

从 lynx-start 时代的应用升级（旧 `lynx-start` CLI / `lynx-start.json` /
`.lynx-start/`）：见 appkit 仓
[UPGRADING.md](https://github.com/lynxland/appkit/blob/main/UPGRADING.md)。

## 开放性

- registry 是 **git 协议**：`--registry` 接受任意 git URL（https/git@/ssh/file），
  第三方可自建私有 registry（模块经 kernelConstraint 声明对契约版本的约束）。
- 安装器**多前缀兼容**（ADR-0006）：lynx-go 机构时代的历史 tag
  （`github.com/lynx-go/lynx-mods/modules/*`）与更早 lynx-start 时代的
  （`github.com/lynx-go/lynx-start/modules/*`）照常安装与改写。

写自己的业务域模块：见 [docs/module-authoring.md](docs/module-authoring.md)
（[English](docs/module-authoring.en.md)）。

## 非目标（Non-goals）

- 单进程 modular monolith（无跨进程事件/outbox——路线图见 appkit）。
- Postgres + bun only（契约层已 ORM 无关，模块生态默认 bun）。
- 不抄 shadcn HTTP registry 协议（git tag 对私有部署更友好）。

Apache-2.0。
