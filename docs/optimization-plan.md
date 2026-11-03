# lynx-start 优化方案

> 语境注（2026-11-03）：本文写于拆分之前——文中 lynx-start 指当时全仓项目
> （今 appkit + 本仓），`lynx-start` 命令为现 CLI 前身（曾名 lynx-mods，
> 2026-11-03 起复名 lynx-start）。历史原文按既定政策保留。

- 日期:2026-10-25
- 状态:提案(待评审排期)
- 输入:「第三方开发者视角」评审(实测)+ 竞品机制调研
- 关联:ADR-0003(三层供给)、ADR-0004(模块六契约)、[module-authoring.md](module-authoring.md)

---

## 1. 执行摘要

评审结论:**工程纪律远超同阶段项目,端到端流程实测全通;但项目目前是"作者视角"的优秀,还不是"陌生人视角"的可托付。** 第三方不选它的三条决定性理由——生态信任、文档语言、模块目录厚度——全部在代码之外。

本方案把评审发现结构化为 **18 个问题(P-xx)** 与 **22 个改进项(I-xx)**,归入四个里程碑:

| 里程碑 | 主题 | 核心交付 | 性质 |
|---|---|---|---|
| M1 ✅(2026-10-25 已交付) | 让陌生人进门 | 英文文档、go install、Non-goals、路由表日志、端口诊断、CHANGELOG | 纯文档 + 小改 |
| M2 | 修头 15 分钟断点 | OpenAPI 生成、chain.DefaultChain、update --dry-run、组合矩阵 CI | 小幅 kernel/CLI 变更 |
| M3 | 源码分发的及格线 | 安全公告通道、lockfile、env 声明、版本范围约束 | registry 协议扩展 |
| M4 | 更新机制差异化 | 两段式 update、codemod 通道、config recipe 更新 | 押注项,品类第一 |
| 持续线 | 目录与生态 | org → notification → tenancy ADR、preset、doctor CI 化 | 并行推进 |

**市场判断:2026 年"shadcn for backend"没有占位者,Go 后端格子是空的。** 胜负手不在模块数量,而在还清源码分发模式欠下的两笔机制债——供应链完整性 + 可更新性——这恰好也是企业采用的门槛。

---

## 2. 评审结论摘要

### 2.1 实测记录(评审方法)

在干净环境完整扮演新用户,全程约 15 分钟:

```
构建 CLI → init myapp → add identity / add session
→ buf generate → go mod tidy(kernel v0.2.0 网络解析)→ go build ./... 全绿
→ docker Postgres → migrate(identity/session 各 v1)
→ dev 启动 → HTTP 注册 / 登录 / Bearer 访问 /v1/identity/me 全通
```

故意踩坑验证错误路径:缺依赖装 session(提示可执行,exit 1)、装不存在的模块(报错含 `lynx-start list` 指引)、5432 被占(见 P-05)、路由凭猜测 404(见 P-02)。

### 2.2 三维结论

| 维度 | 结论 | 一句话 |
|---|---|---|
| 上手难度 | ★★★☆☆ | 流程顺畅,但概念栈陡(lynx + grpcapi + kernel 14 包 + DDD 词汇),且中文独占 |
| 使用体验 | ★★★★☆ | CLI 成熟度罕见(基线/三方合并/doctor);断点在路由发现与 update 心理门槛 |
| 表达能力 | ★★★★☆(框内) | modular monolith + Postgres CRUD + 账号域内出色;边界外(分布式/流式/多存储)明确不覆盖,但**边界未写出来** |

### 2.3 竞品参照

- **已超越 shadcn/ui 的部分:update 三方合并**(shadcn 没有 update 机制)、DAG 依赖校验、启动期事件目录交叉校验。
- **落后于成熟生态的部分**:shadcn 的 registry-item.json 规范(env_vars/transform/Schema 校验)、Nx migrate 的两段式 + codemod、Symfony Flex 的 recipe 更新与 lock。
- **路线验证**:JHipster(生成物巨大、升级痛苦)从反面、shadcn(源码归你)从正面,双重背书"薄脚手架 + 真实模块"的选型。

---

## 3. 问题清单

### A. 上手难度

| # | 问题 | 现象与证据 | 影响 |
|---|---|---|---|
| P-01 | 文档中文独占 | README 英文段是指向中文的 stub;module-authoring.md 纯中文 | 非中文开发者实际无法采用,可触达人群被切掉 ~90% |
| P-02 | API 路由不可发现 | 无 OpenAPI 生成(scaffold/buf.gen.yaml 仅 go/gateway/grpc 三插件)、无 gRPC reflection、无路由表命令;实测猜 `/v1/auth/register` 404 后只能 grep proto | "装完就能用"的卖点在最后一公里断线 |
| P-03 | 概念栈陡且无渐进式文档 | 有效使用需同时掌握 protobuf/buf + DDD 词汇(聚合/端口/哨兵错误)+ lynx + grpcapi + kernel 14 个子包;module-authoring.md 假设读者熟悉 DDD | 目标用户之外的 Go 开发者流失 |
| P-04 | 二进制分发靠手工 | README 教 `go build` 到 /usr/local/bin;`cli/` 有独立 go.mod 却未文档化 `go install` | 第一条命令就劝退 |
| P-05 | 端口冲突报错误导 | 5432 被本机已有 Postgres 占用时,用户看到 `SASL: password authentication failed`,指向认证而非端口(兜底机制存在:`.env` + `POSTGRES_PORT`,但报错不指路) | 高频本地问题,新手排障 10 分钟起步 |

### B. 使用体验

| # | 问题 | 现象与证据 | 影响 |
|---|---|---|---|
| P-06 | update 不可预览 | `add` 有 `--dry-run`(cli/add.go),`update` 没有(cli/update.go)——而 update 恰是"写盘不可回滚"的那个 | "装了半年敢不敢 update"的心理门槛 |
| P-07 | 拦截器链复刻在脚手架 | scaffold/cmd/server/main.go:72-99 复刻 recovery/requestid/clientinfo/authn/authz/validate 全链;注释自认"漏一段则认证与权限判定静默丢失"。scaffold 是生成后归用户的静态文件,**这里的 bug 永远修不到已生成的应用** | 违反自身 ADR 哲学("用户不想改、出错要能远程修的下沉 kernel");最锋利的脚枪 |
| P-08 | 模块目录薄 | 5 个模块(demo/identity/session/access/audit)全是账号域;registry 网络效应未启动 | 只省一周认证工作,对其余业务无引力 |
| P-09 | 配置片段无更新通道 | configFragment 仅在 add 时追加;模块新版本需要新配置键时,update 不碰 config.yaml,缺键靠运行时报错 | 升级后"莫名坏掉" |
| P-10 | 无变更记录 | kernel 有 semver 承诺但无 CHANGELOG;无 GitHub Releases 自动化 | 第三方评估 0.x 项目第一件事("在动吗、破坏性变更说清了吗")无法回答 |
| P-11 | 贡献路径三处手工 | 发布模块需手工改 module.json + registry.json + go.work,doctor 只能事后查错 | 模块贡献的摩擦,抑制 registry 增长 |

### C. 表达能力(多为"边界未声明"而非缺陷)

| # | 问题 | 现象与证据 | 影响 |
|---|---|---|---|
| P-12 | 边界未文档化 | Postgres/bun-only、进程内事件(无 outbox/跨进程)、HTTP 限注解+ExtraRoute——这些是设计选择,但 README 无 Non-goals 章节 | 候选人只能读源码猜;错配用户装到一半才发现 |
| P-13 | tenancy 方向未定 | 契约⑥只说"不进 kernel";事后往各模块迁移 retrofit `tenant_id` 极痛 | 越晚决定代价越大 |

### D. 信任与供应链

| # | 问题 | 现象与证据 | 影响 |
|---|---|---|---|
| P-14 | 断连模块无安全公告 | 模块安装即断连;identity 0.1.0→0.1.1 修时序哑靶,已装用户永远不会知道;kernel semver 只覆盖下沉部分 | 模型根本裂缝,企业采用一票否决项 |
| P-15 | 供应链完整性无校验 | baseline 记录形态但无哈希;无 lock;无 tag 签名验证文档 | "往应用贴源码"是供应链最高权限操作,不可审计 |
| P-16 | 依赖无版本范围 | moduleDependencies 只有名字;装 identity@0.1.0 + 需 0.1.1 事件目录的 session,安装器放行,启动期才炸 | 部分"启动期 fail-closed"应前移到安装期 |
| P-17 | CI 组合覆盖单一 | .github/workflows/ci.yml e2e 仅 demo+identity 组合 | "registry 承诺可信"缺机制化保障 |

---

## 4. 改进项总览

成本:S(≤1 天)/ M(数天)/ L(≥1 周,需单独设计)。

| # | 改进项 | 解决 | 优先级 | 成本 | 里程碑 | 学谁 |
|---|---|---|---|---|---|---|
| I-01 | 文档英文化(README + module-authoring) | P-01,P-03 | P0 | M | M1 | — |
| I-02 | `go install` 分发写进文档 | P-04 | P0 | S | M1 | — |
| I-03 | README 增加 Non-goals 章节 | P-12 | P0 | S | M1 | — |
| I-04 | 启动期路由表日志 | P-02 | P0 | S | M1 | — |
| I-05 | 端口冲突可诊断报错 | P-05 | P0 | S | M1 | — |
| I-06 | CHANGELOG + Releases 自动化 | P-10 | P0 | S | M1 | — |
| I-07 | OpenAPI 生成 | P-02 | P1 | M | M2 | shadcn 生态惯例 |
| I-08 | `chain.DefaultChain` 下沉 | P-07 | P1 | M | M2 | 自家 ADR 哲学 |
| I-09 | `update --dry-run` | P-06 | P1 | M | M2 | — |
| I-10 | 组合矩阵 CI | P-17 | P1 | M | M2 | — |
| I-11 | doctor 的 CI 形态(`--format github`) | P-17 | P1 | S | M2 | golangci-lint |
| I-12 | 贡献路径去手工化 + CONTRIBUTING | P-11 | P2 | M | M2 | — |
| I-13 | 安全公告通道 | P-14 | P2(及格线) | M | M3 | npm audit / OSV |
| I-14 | lockfile + 内容哈希 | P-15 | P2(及格线) | M | M3 | Symfony `symfony.lock` |
| I-15 | env 声明机制 | P-01 衍生 | P2 | S | M3 | shadcn `env_vars` |
| I-16 | 依赖版本范围 + kernelConstraint | P-16 | P2(及格线) | M | M3 | npm peerDeps |
| I-17 | Preset 机制 | P-08 导航 | P2 | S | M3 | Laravel Jetstack / create-t3 |
| I-18 | 目录扩容 org → notification → tenancy ADR | P-08,P-13 | 持续 | L | 持续线 | — |
| I-19 | 两段式更新(`--plan`/`--execute`) | P-06 深化 | 押注 | L | M4 | Nx migrate |
| I-20 | 破坏性变更 codemod 通道 | P-06 深化 | 押注 | L | M4 | Nx migrations |
| I-21 | configFragment recipe 更新 | P-09 | P2 | M | M4 | Symfony `recipes:update` |
| I-22 | gRPC reflection(可选) | P-02 | P2 | S | 按需 | — |

---

## 5. M1 —— 让陌生人进门(纯文档 + 小改,不动 kernel API)

> **状态:已交付(2026-10-25)。** lint / test(带 DSN 实跑)/ test:scaffold e2e 全绿;
> 路由表与注册链路经真实冒烟验证(临时库,已清理)。交付物:README 英文段 + 双语
> 快速开始/Non-goals、docs/module-authoring.en.md、scaffold README 双语、GLOSSARY
> 英文对照、scaffold 路由表打印(bootstrap.HintDB 共用)、kernel/CHANGELOG.md、
> 根 CHANGELOG.md、.github/workflows/release.yml(awk 摘取已验证)。
> 遗留:release workflow 需首个 tag 推送实测一次;既有已生成应用需手动同步
> scaffold 三处改动(main.go 路由表 / bootstrap.HintDB / migrate 提示)。

### I-01 文档英文化

- **做法**:沿用 README 现有"中文在上 / English below"双语格式补全英文段;`module-authoring.md` 出英文完整版(可 `module-authoring.en.md` 双文件);GLOSSARY.md 附英文对照。翻译策略需先定:双语并行维护(推荐,中文为源)。
- **验收**:英文读者从 README 快速开始 → module-authoring 走通 todo 模块七步,无需中文。

### I-02 `go install` 分发

- **做法**:README 快速开始首行改为 `go install github.com/lynx-go/lynx-start/cli@latest`;保留手工 build 作为备选。有余力加 goreleaser + Homebrew tap。
- **验收**:一行命令装上 CLI 并跑通 `lynx-start list`。

### I-03 README Non-goals 章节

- **做法**:README"定位"之后新增一节,明说:单进程 modular monolith;Postgres/bun-only;进程内事件(无 outbox/跨进程投递);HTTP 限于注解 + ExtraRoute;流式/上传/WebSocket 不在覆盖面。与 Roadmap 分开——Non-goals 是承诺不做,不是还没做。
- **验收**:候选人 5 分钟内可自我筛选。

### I-04 启动期路由表日志

- **做法**:`policy.Set` 已含全部方法与 exposure 信息;在 scaffold(或 I-08 落地后放 kernel)启动完成后打印路由表(method、HTTP 路径、anonymous/permissions)。人不再需要开浏览器猜路径。
- **验收**:dev 启动日志可见全部已装模块路由。

### I-05 端口冲突可诊断报错

- **做法**:`cmd/migrate`/bootstrap 包装 pgdriver 连接错误,`SASL/password authentication failed` 或 `connection refused` 时附提示:"port 5432 may be owned by another postgres — set POSTGRES_PORT in .env and re-run db:up"。进阶:`db:up` 前探测端口占用。
- **验收**:5432 被占场景下,报错第一行给出可执行动作。

### I-06 CHANGELOG + Releases 自动化

- **做法**:kernel 与模块各自 CHANGELOG(kernel 的 semver 承诺需要变更记录背书);tag push 触发 GitHub Release,release notes 从 CHANGELOG 摘取。
- **验收**:kernel vX.Y.Z 发布自动出 Release;破坏性变更有迁移说明。

---

## 6. M2 —— 修头 15 分钟断点

> **状态:已交付(2026-10-25,第二批)。** I-07(grpcapi v0.4.0 openapi 包 + scaffold 开关)、I-08(kernel v0.3.0 DefaultChain + scaffold 一行调用)、I-09(update --dry-run,实测零写盘)、I-10(CI 五组合矩阵,LYNXMODS_E2E_MODULES 参数化)、I-11(doctor --format github + 稳定退出码)全部落地;I-12(贡献路径去手工化)未做,留待下批。

### I-07 OpenAPI 生成 ✅(能力已下沉 grpcapi,2026-10-25)

- **架构决策(评审反馈)**:OpenAPI 生成本质是"从 policy 注解推导 API 文档",policy 体系(anonymous/roles/permissions/scopes)是 grpcapi 的私有知识,generic buf 插件表达不了——**能力下沉 grpcapi**,不放进本仓 scaffold。
- **已落地(grpcapi 仓,待发版)**:新增 `openapi` 包(surface IR → OpenAPI 3.0.3:最终路径含 API 组前缀、anonymous→`security:[]`、策略谓词→`x-grpcapi-requirements`、schema 递归生成 + 知名类型映射 + query/path/body 语义);`server.Config.OpenAPI` 非 nil 免费挂 `GET /openapi.json`(与 /docs/ 同等待遇)。单测 + server e2e 全绿。
- **本仓剩余动作(阻塞于 grpcapi 发版)**:scaffold main.go 的 `grpcapiserver.Config` 加 `OpenAPI: &openapi.Info{Title: cfg.AppName…}` 一行;scaffold README 提及;e2e 断言 /openapi.json。grpcapi 打 v0.4 后一并完成。

### I-08 `chain.DefaultChain` 下沉(kernel 小 API,kernel 发 minor 版)

- **做法**:kernel 提供 `DefaultChain(logger, extraStages ...) (*interceptor.Chain, error)`,把 main.go:72-99 复刻的 recovery/requestid/clientinfo/authn/authz/validate + 模块 Stage 排序逻辑移进 kernel(semver 保护、本仓 CI 测试)。scaffold 一行调用。修复从此可推送,不必等用户重新 init。
- **验收**:scaffold 的 main.go 不再含链构建细节;贡献 Stage 的模块(audit)行为不变;已生成应用 `go get kernel@new` 即获得链修复。
- **风险**:kernel API 变更,升 minor;需与 grpcapi 的默认链保持对齐(注释放注释里说明版本对应关系)。

### I-09 `update --dry-run`

- **做法**:复用合并逻辑的 dry 分支,打印计划:自动合并 N 文件 / 冲突 M 文件(逐文件列出冲突区)/ upstream 删除 K 文件 / configFragment 变化提示(依赖 I-21 前先提示不处理)。
- **验收**:`update <mod> --dry-run` 不写盘,输出可据此决定是否 commit + 真跑。

### I-10 组合矩阵 CI

- **做法**:e2e job 参数化,跑三个层级:每模块单独、每条依赖边(如 session→identity、access→identity)、全量。build/vet 层跑全组合,集成测试跑全量与依赖边。
- **验收**:CI 矩阵绿;新增模块自动进入矩阵(登记 registry.json 即入)。

### I-11 doctor CI 形态

- **做法**:`doctor --format github`(PR 内联注解)与稳定退出码(0=健康/1=漂移);文档给一段应用仓库 CI 片段。
- **验收**:示例应用 PR 里 doctor 结果以行内注释呈现。

### I-12 贡献路径去手工化

- **做法**:`lynx-start registry add <dir>`(或 Make 目标)从模块目录生成/校验 module.json、登记 registry.json、追加 go.work,一处输入三处一致;配 CONTRIBUTING.md(中文+英文)。
- **验收**:新模块登记由一条命令完成,doctor/CI 全绿。

---

## 7. M3 —— 源码分发的及格线(供应链与版本求解)

> **状态:全部交付(M3 完结,2026-10-25 第三、四批)。** I-13(advisories + audit 门禁 + doctor ⚠)、I-16(版本范围 + kernelConstraint 安装期强制)、I-15(env 声明全生命周期)、I-14(lock.json:tag/worktree 溯源 + commit + 基线 sha256,update 篡改拦截 + doctor 复核)、I-17(preset saas + init --preset)全部落地并实测。剩余 M4 押注项。

> 不做这三项,认真评估的团队会一票否决:断连模块的安全修复必须可通知(I-13)、贴源码必须可审计(I-14)、依赖必须可求解(I-16)。

### I-13 安全公告通道

- **做法**:registry 维护 `advisories.json`(模块/影响版本/严重度/描述/修复版本);`doctor` 与新增 `lynx-start audit` 比对 `.lynx-mods` 基线版本,报告"identity@0.1.0 存在已知问题 → 建议 update 至 0.1.1"。远端 registry(git URL)场景随 fetch 增量刷新带入。
- **验收**:构造 advisory 后,doctor 对旧版本应用给出明确告警与升级指引;本仓 identity 哑靶修复作为首条真实数据回填。
- **定位**:kernel 管得了的实现推修复(semver),kernel 管不了的靠公告兜底——与 ADR 哲学互补。

### I-14 lockfile + 内容哈希

- **做法**:`.lynx-mods/lynx-start.lock` 记录每模块解析版本(tag/commit)+ 基线文件 sha256;add/update 校验基线完整性;文档声明 git tag 签名与 `git verify-tag` 验证路径(私有 registry 部署指南)。
- **验收**:基线被篡改或 registry tag 被移动时,操作被拒绝并说明原因。

### I-15 env 声明机制

- **做法**:module.json 增加 `env` 字段(变量名/说明/是否密钥);add 时写 `.env.example`(密钥生成随机占位);doctor 校验生产密钥非默认值(联动 identity 的 Assemble 警告)。
- **验收**:add identity 后 .env.example 出现 IDENTITY_SECRET 条目;默认密钥在 doctor 报 WARN。

### I-16 依赖版本范围 + kernelConstraint

- **做法**:moduleDependencies 从 `[]string` 扩展为支持 `{"identity": ">=0.1.1"}`(旧格式兼容);module.json 声明 `kernelConstraint`,doctor 比对 go.mod 的 kernel 版本;add 时求解校验,冲突给出可执行建议(先 `update identity`)。
- **验收**:装 identity@0.1.0 后 add 需 0.1.1 的 session,安装期被拒并指引;而非启动期。

### I-17 Preset 机制

- **做法**:registry 增加 presets(如 `saas` = identity+session+access+audit);`init --preset saas` 一次装齐;`list` 展示 preset。进阶:init 交互式勾选(create-t3 体验)。
- **验收**:一条命令得到可跑的 SaaS 账号底座。

---

## 8. M4 —— 更新机制差异化(押注项)

> 详细设计见 [proposals/m4-update-proposal.md](proposals/m4-update-proposal.md)(2026-10-25 提案,待评审)。

> shadcn 没有 update;Symfony 只有配置级 recipes:update。做到本节,lynx-start 的更新机制是品类第一。

### I-19 两段式更新

- **做法**:`update <mod> --plan` 产出合并计划文件(`.lynx-mods/plans/<mod>.json`:文件级三向结果、冲突区、upstream 删除、configFragment 变化)——可检查、可编辑、可 commit;`update --execute <plan>` 按计划落盘。计划文件是 CI/Renovate 类自动化的接口。
- **验收**:plan→inspect→execute 全程可审计;execute 前应用必须干净(沿用现有 commit 前置要求)。

### I-20 破坏性变更 codemod 通道

- **做法**:模块版本升级可附带 Go 编写的 codemod(module.json 声明,如 `codemods: [{"from": "0.1", "to": "0.2", "path": "..."}]`);update 合并后、编译前执行,处理"改不了你的调用代码"的部分(domain 改名后业务代码引用修复)。codemod 在本仓 CI 与组合矩阵中随模块一起测试。
- **验收**:构造 domain 常量改名的模块升级,codemod 自动修复应用侧引用,`go build` 绿。
- **成本**:L;先做最小形态(仅支持基于 golang.org/x/tools 的文本级重写规则),AST 级后续。

### I-21 configFragment recipe 更新

- **做法**:基线记录安装时的 configFragment;update 检测 upstream 片段 diff,新增键以注释形式提示追加(`# lynx-start: identity@0.2.0 新增,建议合并`),绝不自动覆盖用户改过的值。
- **验收**:模块新增配置键后 update 给出明确提示;用户自定义值不被触碰。

---

## 9. 持续线 —— 目录与生态

- **I-18 目录扩容,顺序 org → notification → tenancy ADR:**
  - **org/team**:access 的 RBAC 当前只有 bootstrap 管理员可挂,org 是其自然对象;也是 SaaS 底座的下一块。
  - **notification**:email 验证码/通知是注册流程的自然延伸。
  - **tenancy**:实现可推迟,**方向不能推迟**——契约⑥"不进 kernel"之下的横切方案(claim?列级隔离?schema 级?)必须先落 ADR,避免各模块迁移定型后 retrofit `tenant_id` 的沉没成本。
- **I-22 gRPC reflection(可选)**:scaffold 选项级开关,按需。

---

## 10. 里程碑与退出标准

| 里程碑 | 周期(参考) | 退出标准 |
|---|---|---|
| M1 | 2–3 周 | 英文读者独立走通 quickstart + todo 七步;go install 一行可用;README 有 Non-goals;5432 冲突报错指路;kernel 有 CHANGELOG 与首个自动 Release |
| M2 | 3–4 周 | /openapi.json 可用;main.go 无链构建细节(kernel 发 minor);update --dry-run 可用;CI 矩阵(单模块/依赖边/全量)常绿;doctor 可入 CI |
| M3 | 4–6 周 | advisories 首条真实数据(identity 0.1.0 哑靶)在 doctor 生效;lock 校验启用;env 声明落地;版本范围约束在安装期拦截错误组合 |
| M4 | 独立排期 | 两段式 update 全流程;首个带 codemod 的模块升级;configFragment 提示合并 |
| 持续 | — | org 模块发布;tenancy ADR 落稿;preset saas 上线 |

依赖关系:I-08 是 I-04 的理想落点(可先 scaffold 落地再下沉);I-19 是 I-09 的演进(I-09 先行降低门槛,I-19 重构内部);I-13 依赖基线版本记录(I-14 的 lock 强化数据);I-20 依赖 I-19 的计划文件机制。

---

## 11. Non-goals(本方案明确不做)

- 第二存储方言、分布式事件/outbox、流式 HTTP——写进 README Non-goals,不排 roadmap;
- 抄 shadcn 的 HTTP registry 协议——git tag 方案对私有部署更友好,维持现状;
- 等待生态出现再抄作业——Go 侧没有先例,借邻近生态(Nx/Symfony/shadcn 规范)自行趟。

---

## 附录 A:实测记录(2026-10-25,干净环境)

| 步骤 | 命令 | 结果 |
|---|---|---|
| 构建 CLI | `cd cli && go build` | ✓ |
| 骨架 | `lynx-start init myapp --module github.com/acme/myapp` | ✓ 14 文件,Next steps 清晰 |
| 装模块 | `add identity` / `add session` | ✓ 17 written, 1 skipped(policy.proto 本地保护) |
| DAG 错误 | 无 identity 时 `add session` | ✓ "depends on identity: run lynx-start add identity first",exit 1 |
| 未知模块 | `add nosuch` | ✓ 报错含 `lynx-start list` 指引,exit 1 |
| 生成 | `buf dep update && buf generate` | ✓ genproto/{identity,session} |
| 依赖 | `go mod tidy` | ✓ kernel v0.2.0 网络解析 |
| 构建 | `go build ./...` | ✓ |
| 端口坑 | 5432 被占 | ✗ 报 SASL 认证错(→ I-05);`.env` + `POSTGRES_PORT=5499` 后恢复 |
| 迁移 | `go run ./cmd/migrate` | ✓ identity v1 / session v1 |
| 业务流 | register → login → Bearer me | ✓ 全通;404 猜路由一次(→ I-04/I-07) |

## 附录 B:竞品与参考

- shadcn registry 规范:<https://ui.shadcn.com/docs/registry/registry-item-json>(env_vars / per-file transform / Schema 校验)
- shadcn 注册表校验(2026-05):<https://ui.shadcn.com/docs/changelog/2026-05-registry-include>
- Nx migrate(两段式 + codemod 标杆):<https://nx.dev/docs/features/automate-updating-dependencies>
- Symfony recipes:update:<https://symfony.com/blog/fast-smart-flex-recipe-upgrades-with-recipes-update>
- Symfony Flex serverless 化(私有 recipe 友好):<https://symfony.com/blog/symfony-flex-is-going-serverless>
- "Shadcn for the backend" 试探者:Hanma(MERN)、Grit plugins(<https://gritframework.dev/docs/plugins/authoring>)、MakerKit(<https://makerkit.dev/blog/changelog/introducing-makerkit-plugin-system>)、open-mercato(<https://github.com/open-mercato/official-modules>)、gsxui(Go UI 侧)——均未成标准
