# M4 提案:更新机制差异化

> 语境注（2026-11-03）：本文写于拆分之前——文中 lynx-start 指当时全仓项目
> （今 appkit + 本仓），`lynx-start` 命令为现 CLI 前身（曾名 lynx-mods，
> 2026-11-03 起复名 lynx-start）。历史原文按既定政策保留；提案落地时
> 命令名按届时 CLI 为准。

- 日期:2026-10-25
- 状态:提案(待评审拍板后排期)
- 范围:I-19 两段式更新、I-20 codemod 通道、I-21 configFragment recipe 更新
- 上游:[optimization-plan.md](optimization-plan.md) M4 节;竞品依据:Nx migrate(两段式标杆)、Nx migrations(随包 codemod)、Symfony recipes:update(配置 recipe)

## 0. 定位与赌注

shadcn/ui 没有 update;Symfony Flex 只有配置级 recipe 更新。M4 做成后,
lynx-start 的更新机制是「源码分发」品类第一:**文件级三方合并 + 计划可审计 +
破坏性变更可自动迁移 + 配置键可跟进**,四层闭环。

前提共识(M1-M3 已交付):基线三方合并、安装基线、供应链锁(lock.json:
版本/来源/commit/基线 sha256)、update --dry-run。M4 全部建立在锁与基线
机制之上,不另起炉灶。

## 1. I-19 两段式更新(update --plan / --execute)

### 动机

update 写盘不可回滚,依赖口头纪律(「先 commit」)。I-09 的 --dry-run 是
只读预览,不留产物;CI/Renovate 类自动化需要**可 diff、可持有、可执行的中间物**。

### 设计

**计划文件** `.lynx-mods/plans/<module>-<from>-to-<to>.json`(随应用提交可
选;结构在 v1 内稳定,之后视为接口):

```json
{
  "schema": 1,
  "module": "identity",
  "from": "0.1.0", "to": "0.1.1",
  "source": "tag modules/identity/v0.1.1", "commit": "…",
  "createdAt": "…",
  "precondition": { "baselineHashesMatch": true },
  "files": [
    { "rel": "internal/app/identity/identity.go", "action": "merge" },
    { "rel": "domain/identity/repo.go",           "action": "create" },
    { "rel": "internal/…/old.go",                 "action": "remove-upstream" },
    { "rel": "…",                                  "action": "conflict" }
  ],
  "configFragment": { "changed": true, "addedKeys": ["identity.key_rotation"] },
  "codemods": ["0.1->0.2"]
}
```

**CLI**:

- `lynx-start update <m> --plan`:计算合并计划,写计划文件,**不碰任何应用文件**;
- `lynx-start update --execute <plan-file>`:前置校验(计划属于本应用;
  基线哈希与 lock 仍符;**local 文件哈希与计划生成时一致**——计划生成后应用
  又改过即拒绝,重新 --plan)+ 按计划落盘(冲突打 markers、upstream 删除
  仍只提示)+ 刷新基线/锁/清单 + 触发 codemod 链;
- 现有单段 `update <m>` 保留:等价于 plan+execute 即时执行,不落计划文件。

**自动化接口**:计划文件是纯 JSON,CI 可对 plans/ 目录做 review;Renovate
类工具可 `--plan` 后按 `files[].action` 统计风险。

### 兼容与风险

- 计划与现状的时间差:execute 时逐文件重验 local 哈希(记入计划的
  files[].localHash),不符即拒绝——宁可重出计划,不盲执行;
- 计划文件手工编辑:v1 仅允许**删除**条目(跳过某文件的合并),不允许改
  action——改 action 等于绕过校验,拒绝执行。

## 2. I-20 codemod 通道(破坏性变更的自动迁移)

### 动机

三方合并只能搬文件,改不了**用户的调用代码**:domain 常量改名后,应用业务
代码里的 `identity.EventUserCreated` 编译不过,用户手工修——这正是升级痛苦
的主体。Nx 用随包分发的 schematic codemod 解决。

### 设计

**声明**(item.json):

```json
"codemods": [
  { "from": "0.1", "to": "0.2", "entry": "./codemods/0.1-0.2" }
]
```

codemod 源码住模块目录 `codemods/<from>-<to>/main.go`,**不随 add 落应用**
(skipModuleFile 排除),来源经 registry git 溯源。

**执行点**:`update --execute` 在文件合并落盘后、提示 `go build` 前运行。
进程模型:`go run <registry-dir>/<module>/codemods/<from>-<to>` 从 registry
源码现场构建(不做任意二进制插件——供应链面与模块源码同级);输入:
`--app <dir> --plan <plan.json>`;输出(stdout JSON):修改的文件清单。
版本跳跃(0.1→0.3)按声明链顺序执行 0.1→0.2、0.2→0.3。

**v1 能力边界**:文本/AST 级重命名(import 路径、包级标识符),基于
golang.org/x/tools;不处理泛型签名变化、跨模块引用(只改本模块符号的
引用)。codemod 必须幂等(可重跑)且必须带 fixture(before/after 目录),
fixture 测试进 CI 组合矩阵。

**失败语义**:codemod 失败不回滚文件合并(文件层合并仍有效),但在结果
输出中显式标注「codemod 0.1→0.2 失败:…,需手工迁移」,execute 退出码
非零提醒。

### 风险

- `go run` 的构建耗时与离线场景:缓存构建(与 registry clone 缓存同目录);
- codemod 本身 bug 污染用户代码:计划文件记录 codemod 改动清单,doctor
  可复核;失败时提示 `git checkout -- <files>`(前置纪律仍是先 commit)。

## 3. I-21 configFragment recipe 更新

### 动机

模块新版需要新配置键时,update 不碰 config.yaml——缺键只能运行时报错,
这是「升级后莫名坏掉」的主要来源之一(P-09)。

### 设计

- **记录**:lock 条目增加 `configFragmentHash`(安装时片段的 sha256);
- **比对**:update 时解析 upstream 片段与安装时片段(YAML 语义级),得到
  **新增键/删除键/默认值变化**三集;
- **应用**:新增键以**注释形式**追加到该模块配置段尾
  (`# lynx-start: identity@0.1.2 新增,建议合并:\n# key_rotation: 24h`),
  绝不自动改用户已有键;删除键只提示;
- **doctor**:按已装版本(tag 冻结 item)的片段键集合,校验应用配置段
  键齐全(语义比对,键集合差异即报);
- 与两段式整合:片段 diff 计入计划文件的 `configFragment` 字段,execute
  时统一应用。

### 边界

- 用户改过片段内键的**值**不触碰;只追加缺失键的注释提示;
- 片段含复杂结构(list/map)时,键集合比对按顶层键 v1。

## 4. 里程碑与工作量(参考)

| 批次 | 内容 | 量级 |
|---|---|---|
| M4-a | I-19 计划文件 + --plan/--execute + e2e(含 local 漂移拒绝路径) | 2-3 天 |
| M4-b | I-21 片段 recipe(lock 记录/语义 diff/注释追加/doctor)+ e2e | 1-2 天 |
| M4-c | I-20 codemod 声明/执行链/fixture 测试 + 首个真实 codemod(可等首个破坏性变更时顺势做) | 3-5 天 |

顺序建议 a→b→c:前两者是确定性收益;c 依赖真实破坏性变更场景验证。

## 5. 验收标准

- a:计划文件生成→人工 review→execute 全程;计划过期(local 变更)被拒;
  单段模式行为与现状完全一致(回归);
- b:模块新增配置键后 update,应用 config.yaml 出现注释提示且用户值未动;
  doctor 对缺键给出精确清单;
- c:构造 domain 常量改名的模块升级,codemod 自动修复应用侧引用,
  `go build ./...` 绿;fixture 进 CI。

## 6. Non-goals(v1 明确不做)

- 不自动修改用户已有配置键的值;
- 不做任意二进制 codemod 插件(只接受 registry git 源码现场构建);
- 不做跨模块 codemod(只改本模块符号);
- 不承诺计划文件 schema 跨大版本稳定(schema 字段有版本号,变更走 minor)。

## 7. 开放问题(评审拍板)

1. 计划文件是否默认随应用提交(建议:是,与 .lynx-mods/ 其余部分一致)?
2. codemod 的入口签名(App/Plan 之外是否需要 --dry-run 与 --stdin 协议)?
3. 片段比对用 gopkg.in/yaml.v3 语义树还是简化顶层键(v1 建议后者)?
