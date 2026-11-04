# Changelog — lynx-start

本仓由 lynx-start（旧基础库，2026-11-02 更名 appkit）拆分而来（2026-10-31，
ADR-0006）：模块分发（cli/modules/registry/scaffold）整体迁入；2026-11-03
本仓更名回 lynx-start（ADR-0007）。以下历史条目承接 lynx-start 时代的根
CHANGELOG（全仓视角）；kernel 谱系的 changelog 留在 lynx-start。条目按发布
时间倒序。

## 未发版（Unreleased）

- **模块新增 README**：8 个内置模块各加根级 `README.md`（registry 元文档，
  中文主体 + English 摘要；用途/安装/依赖/配置/环境变量/事件，以
  module.json 为事实源）。CLI `skipModuleFile` 同步把 `README.md` 列入
  跳过——`relocateModule` 对布局契约外的文件 fail-closed，不跳过则所有
  工作区形态 `add`/`update` 直接报错；跳过后 README 不入应用树、不进基线，
  update 合并不受影响。
- **e2e 负路径场景弃用历史 tag**：module.json 更名后旧 tag（item.json 形态）
  不可安装，`test:scaffold` 的依赖负路径/版本约束/update 三场景改由 fixture
  registry 伪造「module.json 形态低版本 tag」（临时 clone + 版本号改写 +
  legacy 标记注入 + revert 工作区），场景语义完整保留；旧系前缀改写覆盖
  由单元测试 TestDualPrefixCompat 承担，e2e 不再重复。

## 全模块 v0.5.0 + cli v0.6.0（2026-11-04，registry 清单更名 item.json → module.json）

- **registry 清单工件更名**：`modules/<name>/item.json` →
  `modules/<name>/module.json`——「item」是 registry 实现视角的条目话术，
  对读者不表意；新名与 registry.json（仓库级索引）构成「索引列模块、
  module.json 描述单模块」的对称命名。决策与备选否决见 ADR-0008。
- **CLI 全量随更名，不留兼容**（ADR-0007 政策）：类型 `registry.Item` →
  `registry.ModuleManifest`，方法 `Item`/`ItemAt`/`ItemAtCommit` →
  `Manifest`/`ManifestAt`/`ManifestAtCommit`，`LoadItem` → `LoadManifest`；
  tag/manifest 漂移等错误信息、渲染 skip 过滤、`registry add` 骨架生成
  与 `registry check` 体检项同步。`Index.Items`（registry.json 索引条目）
  语义不同，不动。
- **发版工具链**：mise release 任务改读 module.json（模块版本唯一事实源
  不变，仅文件名随更）；cli 版本钉 0.6.0。
- **全模块重发 v0.5.0**：清单随 tag 树分发，旧 tag 树内只有 item.json，
  改名后不可安装——8 模块 version 0.4.0 → 0.5.0。存量应用升级：取新
  cli 后 `lynx-start add <module> -o` 重建基线，doctor 对未迁移应用给出
  可行动报错。
- 文档同步：CONTRIBUTING（中英）、module-authoring（中英）、
  optimization-plan、模块 domain 注释；历史 ADR/提案/CHANGELOG 条目按
  既定政策保留原文。optimization-plan 中 shadcn 的 `registry-item.json`
  为竞品规范名，不在更名之列。

## 全模块 v0.4.0 + cli v0.5.0（2026-11-03，更名 lynx-mods → lynx-start）

- **仓更名与 module path 换系**：GitHub 仓 `lynxland/lynx-mods` →
  `lynxland/lynx-start`（appkit 更名后 "lynx-start" 名号空出——本项目正是
  自 lynx-start 拆出，名随实归，ADR-0007）。8 个模块与 cli
  `github.com/lynxland/lynx-mods/...` → `github.com/lynxland/lynx-start/...`
  （含 .proto go_package 与 genproto 同步替换）。
- **CLI 命令与全部产品工件随更名，不留兼容**（决策与迁移见 ADR-0007）：
  二进制 `lynx-mods` → `lynx-start`（包目录 cli/cmd/lynx-start）；应用工件
  lynx-mods.json → lynx-start.json、.lynx-mods/ → .lynx-start/、
  ~/.lynx-mods → ~/.lynx-start、~/.cache/lynx-mods → ~/.cache/lynx-start；
  env LYNXMODS_REGISTRY/LYNXMODS_E2E_MODULES → LYNXSTART_REGISTRY/
  LYNXSTART_E2E_MODULES（顺带修掉 init list 误写的 LYXSTART_REGISTRY——
  缺 N 的存量笔误）；注入锚点 `lynx-mods:module-import/module-entry/
  config-fragment` → `lynx-start:`（scaffold 模板同步）。
- **安装器前缀兼容扩为四系**：lynxland/lynx-start（新 canonical）+
  lynxland/lynx-mods + lynx-go/lynx-mods + lynx-go/lynx-start（历史 tag）；
  `TestDualPrefixCompat` 增 lynxland/lynx-mods legacy 行。
- **registry**：registry.json name、identity/session item.json 的
  configFragment `issuer` 随更名；advisory 新公告改用 LYNXSTART- 前缀自
  LYNXSTART-0002 顺延（0001 为旧基础库时代历史 ID，保留不复用）。
- **存量 lynx-mods 时代应用迁移**（手工，一次性）：

  | 旧 | 新 |
  | --- | --- |
  | `go install github.com/lynxland/lynx-mods/cli/cmd/lynx-mods@…` | `go install github.com/lynxland/lynx-start/cli/cmd/lynx-start@…` |
  | `lynx-mods.json` / `.lynx-mods/` | `git mv` 为 `lynx-start.json` / `.lynx-start/` |
  | 锚点 `lynx-mods:…`（modules.go 两处 + config.yaml 一处） | 文本替换为 `lynx-start:…` |
  | mise 任务 `lynx-mods generate` | `lynx-start generate` |
  | `LYNXMODS_REGISTRY` | `LYNXSTART_REGISTRY` |
  | `~/.lynx-mods`、`~/.cache/lynx-mods` | 弃置（基线/备份可由 registry + lock 确定性重建） |

- **发布顺序**：先在 GitHub 更名仓库（lynx-mods → lynx-start），后打推本条
  tag——旧仓名下打新路径 tag 会因 go.mod 路径与仓库不符被 module proxy
  拒绝。tag 形态不变：`cli/v0.5.0`、`modules/<m>/v0.4.0`。
- **发版任务 `mise run release`**：发布顺序收敛为前置守卫（origin 与
  module path 同仓名 / 干净树 / main 已推，fail-closed 不可重入）；
  模块版本唯一事实源 item.json，cli 版本钉于任务（与 CHANGELOG 同源）。
- 历史条目与 ADR 按既定政策保留旧名。

## 全模块 v0.3.0 + cli v0.4.0（2026-11-02，appkit 基座 + 机构更名 lynxland）

- **module path 换系（GitHub 机构更名 lynx-go → lynxland）**：8 个模块与 cli
  `github.com/lynx-go/lynx-mods/...` → `github.com/lynxland/lynx-mods/...`
  （含 .proto go_package 与 genproto 重生成；BSR dep 换
  `buf.build/lynxland/grpcapi`，buf.lock 重锁）。
- **appkit 基座切换随版发布**（`53373d6`）：契约依赖统一为 appkit v0.8.0 +
  contrib/bunx v0.5.0 + grpcapi v0.5.0（genproto v0.4.0）+ lynx v1.21.0；
  item.json 的 goDependencies 同步换 appkit（此前仍列 lynx-start），
  kernelConstraint `>=0.5.0` → `>=0.8.0`（appkit v0.8.0 起依赖 lynxland
  路径的 lynx 类型，与旧基座混用必然类型不兼容）。
- **cli**：init 地板 appkit v0.8.0 / contrib/bunx v0.5.0 / grpcapi v0.5.0，
  生成 go.mod 指新路径；安装器前缀兼容扩为三系——lynxland/lynx-mods（新
  canonical）+ lynx-go/lynx-mods + lynx-go/lynx-start（历史 tag），
  `canonicalBothPrefixes` 泛化为按前缀表求其余各系；`TestDualPrefixCompat`
  复原 legacy 行（此前两行经批量替换成同值，实际未测 legacy 前缀）。
- **修复**：`bridgeModule` 生成的桥接包仍 import `github.com/lynx-go/lynx-start`
  （包名 `kernel`）却用 `appkit.` 限定符——`add` 生成的应用编译失败
  （`53373d6` 遗漏）；改 import `github.com/lynxland/appkit`，
  `TestBridgeModule` 增 import 断言钉住。`test:scaffold` update 构建段的
  `go get` 同步由 lynx-start 换 appkit。
- 历史条目与 ADR 按既定政策保留旧名。

## cli v0.3.1 + session v0.2.1（2026-11-01，dogfood 首循环③）

- **cli：安装期自动应用 goDependencies**（ADR-0006 分发器改进提前落地）：
  add 成功后对 item.json 的 goDependencies 做钉版抬升——首选 `go get`，
  失败（离线/mise 未 trust）退回纯文本改写 go.mod（go.sum 由后续 tidy
  收口）。**只升不降**（go get 显式版本会降级，后装模块的旧依赖不得
  拖垮先装模块已抬的版本；require 缺失/历史路径一律跳过防毒化 tidy）。
  两轮试点（storeerr/occ）均被「应用地板未抬」绊倒后立项。
- **session v0.2.1**：吊销状态机换装 occ.Transition + bunx.IfState
  （transitionRevoke 成为 active→revoked 单一事实源；幂等/计数语义
  逐字保留）；契约依赖升至 lynx-start v0.5.3 + contrib/bunx v0.1.3。

## 0.2.0 迁移首发（2026-10-31）

- **module path 换系**：8 个模块 `lynx-go/lynx-start/modules/*` →
  `lynx-go/lynx-mods/modules/*`（含 .proto go_package 与 genproto 重生成）。
- **契约依赖版本化**：require lynx-start v0.5.0 + contrib/bunx v0.1.0 +
  grpcapi v0.4.0（统一，修版本分叉）；kernelConstraint >=0.5.0。
- **契约调用面换新**（lynx-start 纯契约化）：compose.ValidateModules/
  AssembleAll/…、chain.DefaultChain；Deps.DB→any，装配处 bunx.MustDB(deps)。
- **CLI 更名 lynx-mods**（cli/v0.3.0）；应用工件 lynx-mods.json/.lynx-mods/
  ~/.lynx-mods/~/.cache/lynx-mods；env LYNXMODS_REGISTRY/LYNXMODS_E2E_MODULES
  （LYNXSTART_TEST_PG_DSN 不变——属 lynx-start 侧 contrib/bunx/testutil）。
- **安装器双前缀兼容**：历史 tag（lynx-start 系）原样搬入并照常安装改写。
- 历史 tag（modules/*/v0.1.x）原样保留；advisories 沿用历史 ID LYNXSTART-0001，
  新公告用 LYNXMODS- 前缀。

---

# Changelog — lynx-start（旧基础库，今 appkit——2026-10-31 迁入时原样承接，勿与现仓名混淆）

两类版本:kernel(`kernel/vX.Y.Z`,semver)与 registry 模块(`modules/<name>/vX.Y.Z`,
源码分发,无稳定性承诺)。CLI / scaffold 变更随仓库 main 演进,不单独打 tag,
记在 Unreleased。推 tag 后 GitHub Actions 自动建 Release 并摘取本文对应小节。

## Unreleased

- **grpcapi 声明语法(policy.proto)改经 BSR 供给,弃 vendored 拷贝**
  (ADR-0006 修订):模块与应用的 `buf.yaml` 直接依赖
  `buf.build/lynx-go/grpcapi`(buf.lock 钉 commit),删除十处
  `proto/grpcapi/v1/policy.proto` vendored 副本(scaffold + 8 模块 + demo),
  `buf.gen.yaml` 的 exclude_paths 与 breaking.ignore 两处特例一并消失;
  Go 类型不变,仍由 `github.com/lynx-go/grpcapi` 提供。CLI `generate`
  对未装模块的空应用前置跳过 buf 管线(本地无 proto 时 buf lint 会因
  空模块报错)。**update 合并删除语义精化**(ADR-0006 同款修订):upstream
  删除且本地未改的文件改为自动删除(删除前留备份进 .lynx-start/backup,
  与冲突备份同款);本地有改动仍保留并提示——存量应用装旧 tag(载荷带
  vendored 副本)升新版即走此路径,残留副本会与 BSR dep 同路径冲突。
  **存量应用升级姿势**:逐模块 `lynx-start update <m> --force`(刷新
  基线/manifest,自动清掉 vendored 副本),buf.yaml deps 补
  `buf.build/lynx-go/grpcapi`,`buf dep update` 后一切照旧。
- **registry 第 8 个模块 `task` v0.1.0(组织内任务,第一个组织挂靠业务资源——
  ADR-0008 姿势首秀 + ADR-0007 通道②首秀)**:
  - 姿势落实:organization_id 是租户键(NOT NULL 只存 ID,契约④;本域无业务
    唯一约束,若有必含 organization_id);**租户语境进端口签名**——Get/List/
    Delete 一律携带 organizationID,ID 泄露也跨组织取不到(防漏的端口级表达,
    repo DB 测试固化跨组织负路径);成员资格经 organization domain 只读端口
    校验(userReader 模式第四处,写方法一律报错);资源路由挂组织层级
    /v1/organizations/{id}/tasks。
  - 权限语义:成员即可创建/查看(v1 不做组织级 RBAC);完成与删除限创建者
    (creator_id 是 v1 资源所有权裁决依据);Complete/Delete 幂等(重复为 no-op
    不重复发事件)。
  - 事件:task.task.created / task.task.completed;DependsOn organization
    >=0.1.0(成员资格 0.1.0 即有)。CI matrix 增 task 单模块组合(DAG 自动
    解析 identity+organization 的验证)与全量组合;**不进 saas 预设**(业务
    资源非底座)。
- **ADR-0008 软多租户横切约定:organization_id 即唯一租户键**(路线图挂了三批
  的"tenancy 越晚决定 retrofit 越痛"兑现——第一个业务资源模块出现前定约定,
  当前零 retrofit 成本)。四定案:①租户键不变量——organization_id 是唯一
  租户键,永不引入 tenant_id/workspace_id 平行概念(双租户概念并存是 retrofit
  最痛源);软→硬升级(行级过滤/分库/RLS)永远在既有键上加层,数据模型零改动。
  ②业务资源模块标准姿势:表挂 organization_id NOT NULL+复合唯一约束必含
  organization_id(跨组织重名合法,ADR-0002 教训继承)+索引打头;路由挂
  /v1/organizations/{id}/<resources>;成员资格经 ADR-0007 通道②校验。
  ③查询防漏走纪律姿势(app 层资格校验+repo 显式过滤),强制过滤横切暂缓
  (重启扳机:漏过滤真实出现——与 M4 同款哲学);④组织级 RBAC 路径预留:
  access Assignment 未来加可空 organization_id(空=全局角色语义不变),
  角色目录保持全局;顺序确认 access 组织维度→team→组织角色表达。账号域
  模块(identity/session/notification/audit)不 retrofit——用户与会话全局
  是 GitHub 模型本义。kernel 零改动(契约⑥原样)。弃选:硬多租户默认开
  (RLS 成本 ADR-0002 已否+强制过滤层 bug 是静默丢数据级故障)、
  schema-per-tenant(BaaS 特殊需求)、GitHub 完整 owner 多态(产品选择非
  通用默认,需要时 additive 演进)。module-authoring 双语加组织挂靠一节。
- **ADR-0007 跨模块调用面:供给方 domain 接口 + 根包导出构造器**(决策先行,
  不排期实现)。notification v0.1.0 砍显式发送时留下的口子:internal 可见性
  下应用代码/其他模块如何同步调用模块写能力。通道分级判断表:事件(异步)
  /userReader 只读适配器(读)/**新增③供给方 domain 能力接口+根包手写
  exports.go 转发构造器+组合根显式接线(写)**/模块 RPC 面(跨进程)。
  读写不对称是本质的:读可消费方自持,写必须供给方导出。零机制新增
  (文件清单/导入改写/三方合并照旧);同事务靠 ctx 携带(buntx.Conn)自然
  共享。弃选:kernel 服务注册表(运行时定位器与编译期显式哲学冲突,ADR-0005
  同款理由)、RPC 自调用(环回+认证主体尴尬+单进程事务优势作废)、事件
  请求-响应(异步强套同步)、userReader 推广到写路径(所有权漂移)。
  module-authoring 双语加通道分级一节。
- **registry 第 7 个模块 `notification` v0.1.0(站内通知,首个纯事件目录消费模块)**:
  - 定位:通知 = 事件的事实回执。订阅 `identity.user.password-changed` 与
    `organization.member.added` / `organization.member.removed` 自动生成
    通知——内容在发生时刻定格为快照(title/body 落库,不引用模板;
    payload 保留原始事件载荷供前端跳转)。**DependsOn identity+organization
    却零 Go import**(契约②字符串目录消费,ConsumedEvents 启动期交叉校验
    发布者在装)——依赖面与代码耦合面首次分离。
  - API 面(全部 authenticated,通知只可见于收件人本人,user_id 取自
    Principal 不信任请求参数):ListNotifications(分页+unread_only 过滤+
    整户未读计数)/MarkNotificationRead(幂等,他人通知与不存在统一
    NotFound 不泄露存在性)/MarkAllNotificationsRead(返回标记条数)。
    写入仅经事件订阅,无发送 RPC——跨模块调用面(Go internal 规则下他
    模块调不到本模块 UseCase)留待单独 ADR。
  - 工程:watcher 单 Service 订多 topic——topic 是 module.<m> 前缀收敛,
    同模块多事件共享 topic 必须去重后每话题只订一次(重复订阅会让事件
    投递多遍,内存总线对同名 handler 亦拒绝注册);迁移
    `notification_notifications`(jsonb payload + 部分索引未读计数);
    item.json 声明 moduleDependencies
    `[{"identity":">=0.1.1"},{"organization":">=0.1.1"}]` + kernelConstraint
    >=0.4.0;e2e 负路径新增 organization@0.1.0 在装时 add notification
    必须被版本约束拒绝(I-16 机制第二次实战);saas 预设纳入(六模块);
    CI matrix 增 `identity organization notification` 边与全量组合。
  - 顺手修 CLI:`registry check` 的依赖登记检查改顺序无关(原单遍扫描按
    登记顺序构建 registered 集合,字母序下 notification 先于依赖
    organization 出现即误报——既有缺陷被字母序掩盖至今)。
- **`organization` v0.1.1:member.added/removed 事件载荷补
  `organization_name`**(additive,通知文案可读:"你已被加入组织 Acme";
  notification 声明 organization>=0.1.1 即源于此——版本范围约束的
  真实需求用例)。
- **breaking：安装布局镜像挂载 + 基线缓存分离（ADR-0006，v0.x 阶段直接 breaking）**：
  - 模块装入应用 `modules/<m>/` 专用区（`modules/` = registry 镜像区，
    `internal/` = 应用自有代码），`{domain,internal/{app,api,infra}}` 下尾段
    模块名目录拍平；模块仓内源码结构零改动。Go internal 可见性下应用经
    CLI 生成的桥接包 `modules/<m>/module.go` 取得 Module/Assemble；
    modules.go 登记点移至 `modules/modules.go`。proto 契约源仍集中应用根。
  - 新命令 `lynx-start generate`：buf 管线（dep update/lint/generate），
    布局规则单一事实源收敛 CLI（不靠安装拷贝也不靠 mise shell 后处理）；
    scaffold 的 mise generate 任务变为一行转发。
  - 基线缓存迁至 `~/.lynx-start/baselines/<appModulePath>/<m>@<commit>/`
    （多版本并存、可重建、不进仓库）：应用只提交 lock.json+manifest；
    update/doctor 在缓存缺失时按 lock 记录的 commit 从 registry 自动重建
    （ensureBaseline，跨设备 rename 回退复制）；update 冲突备份移至
    `~/.lynx-start/backups/<appModulePath>/<m>/`；remove 整树清理
    `modules/<m>/`（含桥接与 genproto）与用户缓存。
  - 导入改写五条结构化规则（genproto/domain/三层 internal，逐依赖映射）+
    改写后残留扫描 fail-closed；domain guard_test 的相对路径深度随拍平
    修正（安装期确定性改写）。doctor 增旧布局检测（可行动重装指引）。
  - e2e 全量组合在新布局下全绿（含 update 三方合并/remove/doctor）；
    旧布局应用（如 lynx-start-demo）需按指引重装迁移。
- **registry 第 6 个模块 `organization` v0.1.0(组织与成员事实,首个账号域之外的业务模块;命名纪律:除局部短变量外一律全称,模块名/目录/文件名同为全称)**:
  - 定位:组织 = 结构与成员事实,不自带 RBAC——成员带 `IsOwner` 结构标记
    (创建者即 owner,owner-only 管理;最后一个 owner 不可离开,owner 不可被
    他人移除),角色/权限语义留给 access 的角色目录表达;slug 全局唯一
    (唯一索引,冲突映射 `ErrSlugTaken`),创建后不可变。
  - API 面(全部 authenticated,v1 不做组织级 RBAC——成员资格/owner 校验在
    app 层):CreateOrganization/GetOrganization(仅成员可见)/
    ListMyOrganizations/RenameOrganization/AddMember(幂等,经 identity
    只读端口校验用户存在)/RemoveMember(幂等,不可移 owner)/ListMembers/
    Leave(最后一个 owner 拒绝)。
  - 事件:`organization.organization.created` / `organization.member.added` /
    `organization.member.removed`;DependsOn identity(domain 级只读消费,
    access 同款 userReader 模式);v0.1 零配置段;迁移
    `organization_organizations` / `organization_members`(独立版本表,
    user_id 只存 ID 不设外键)。
  - 工程:item.json 声明 `moduleDependencies [{"identity": ">=0.1.1"}]` +
    `kernelConstraint ">=0.4.0"` + `goDependencies kernel@v0.4.0`;guard 两件
    (domain 纯度 + AssertDepsAtAPILayer)、policy_test(全方法非匿名)、
    DB 门控测试真库验证;CI e2e matrix 增 `identity organization` 边与
    全量组合;saas 预设纳入 organization(底座从纯账号域扩展到组织结构)。
- **M3 完结(2026-10-25 第四批)**:
  - I-14 供应链锁 `.lynx-start/lock.json`:每模块记解析版本、来源
    (tag/worktree)与 commit、基线文件 sha256。add/update 写入;update
    合并前复核基线哈希——被篡改的基线会静默污染三方合并,现在 fail-closed
    并给出重建指引;doctor 复核条目在、哈希符、钉版 tag 未移动;remove
    清理。实测:篡改基线后 update 被拒,doctor 全绿态零误报。
  - I-17 preset:`registry/presets.json` + `lynx-start init --preset saas`
    一次装齐组合(组合顺序即安装顺序,依赖由安装器校验);`list` 展示
    presets;首个预设 saas(identity+session+access+audit)四模块全链路实测。
  - 修复:doctor 改按已装版本解析 item(tag 冻结形态——工作区 item 声明的
    新版 env/约束拿来校验旧安装会误报);tag 漂移检查只对 tag 安装条目
    (worktree 记 HEAD,与旧 tag 比对无意义)。
- **M3 及格线(2026-10-25 第三批)**:
  - I-13 安全公告通道:`registry/advisories.json`(affectedVersions 精确
    匹配,不做范围解析)+ 新命令 `lynx-start audit`(命中即非零退出,
    CI 门禁;--format github 出注解);doctor 集成为 ⚠ 提示(不计数不过载
    体检语义);已回填首条真实公告 LYNXSTART-0001(identity 0.1.0 时序哑靶)。
  - I-16 版本范围约束:item.json `moduleDependencies` 支持
    `[{"identity": ">=0.1.1"}]` 对象形态(旧字符串形态兼容),add 安装期
    校验已装版本(启动期 fail-closed 前移到安装期);新增 `kernelConstraint`
    对照应用 go.mod 的 kernel require(add 校验 + doctor 复核);session
    回填真实约束 identity>=0.1.1。
  - I-15 env 声明:item.json `env` 字段([{key,description,secret}]);
    add/update 自动写 `.env.example`(secret 生成 32 hex 随机占位,变量带
    应用前缀),doctor 校验声明在位,remove 清理对应段;identity/session
    已声明主密钥变量。
  - I-12 贡献路径去手工化:新命令 `lynx-start registry add <dir>`(item.json
    骨架 + registry.json 登记 + go.work use,一处输入三处一致,幂等)+
    `lynx-start registry check`(一致性体检,可入 CI);CONTRIBUTING.md。
- **M2 断点修复(2026-10-25 第二批)**:
  - I-07 OpenAPI 接线:scaffold 打开 `grpcapiserver.Config.OpenAPI`,应用
    免费获得 `GET /openapi.json`(能力在 grpcapi v0.4.0 的 openapi 包);
  - I-08 scaffold 换用 `kernel.DefaultChain`(v0.3.0)——复刻默认链的
    易错逻辑从生成物移入 semver 库;
  - I-09 `update --dry-run`:合并计划(自动合并/冲突/upstream 删除)完整
    呈现,零写盘;
  - I-10 CI 组合矩阵:单模块 / 依赖边 / 全量五组合,e2e 经
    `LYNX_E2E_MODULES` 参数化;
  - I-11 `doctor --format github`:失败项输出 workflow command 注解,
    通过项静默,退出码稳定(0/1);
  - init go.mod 写入依赖地板(kernel v0.3.0 / grpcapi v0.4.0)。
- **英文文档(I-01)**:README 完整英文段、module-authoring.en.md、scaffold
  README 双语、GLOSSARY 英文对照;
- **README:go install 与 Non-goals(I-02/I-03)**:快速开始首选
  `go install github.com/lynx-go/lynx-start/cli@latest`;新增「非目标」章节;
- **scaffold:启动期 HTTP 路由表(I-04)**:服务启动打印 方法/路径/RPC/认证
  要求,免翻 proto 找路径;
- **scaffold:数据库连接失败排障提示(I-05)**:端口被既有 Postgres 占用等常见
  形态,报错附可执行动作(bootstrap.HintDB,migrate 与 server 共用);
- **发布自动化(I-06)**:本 CHANGELOG + `.github/workflows/release.yml`
  (tag push 自动建 Release,notes 从本文摘取)。

## cli v0.2.1 (2026-10-25)

ADR-0006 修订：**genproto 回归应用根** `genproto/<m>/v1/`（v0.2.0 曾归位
`modules/<m>/genproto/`）。复盘：genproto 是改写约定里最稳定的一条特例（目录名死约定），
生成物无人手工 diff、镜像完整性价值近乎零；generate 后搬移是长期维护税（首版即出过
目标路径 bug）且偏离 buf 生态惯例。generate 现在是纯 buf 管线、零后处理；remove 顺手
清理 `genproto/<m>/`。三目录各司其职：`proto/` 契约源、`genproto/` 生成物、`modules/`
模块源码。**v0.2.0 装的应用需重装迁移**（add -o 重写 genproto import），lynx-start-demo 已同步。

## cli v0.2.0 (2026-10-25)

安装布局重大变更（ADR-0006，**breaking**——旧布局应用按 doctor 指引重装迁移）：

- 模块装入应用 `modules/<m>/` 专用区（`modules/` = registry 镜像区，`internal/` = 应用自有代码），`{domain,internal/{app,api,infra}}` 尾段模块名目录拍平，模块仓源码结构零改动；应用经 CLI 生成的桥接包 `modules/<m>/module.go` 取得 Module/Assemble；modules.go 登记点移至 `modules/modules.go`。
- 新命令 `lynx-start generate`：buf 管线（dep update / lint / generate）+ genproto 按模块归位 `modules/<m>/genproto/<m>/v1/`——布局规则单一事实源收敛 CLI；scaffold 的 mise generate 任务变为一行转发。
- 基线缓存迁 `~/.lynx-start/baselines/<appModulePath>/<m>@<commit>/`（多版本并存、可重建、不进应用仓库；应用只提交 lock.json + lynx-start.json）；update/doctor 在缓存缺失时按 lock 记录的 commit 从 registry 自动重建；update 冲突备份移 `~/.lynx-start/backups/…`；remove 整树清理 `modules/<m>/` 与用户缓存。
- 导入改写五条结构化规则 + 残留扫描 fail-closed；guard_test 相对路径随拍平在安装期修正；doctor 增旧布局检测。

## cli v0.1.1 (2026-10-25)

- main 包移至 `cmd/lynx-start/`——`go install` 装出的二进制名随包目录,
  v0.1.0 会装成 `cli`;修复后 `go install
  github.com/lynx-go/lynx-start/cli/cmd/lynx-start@latest` 得到 `lynx-start`。

## cli v0.1.0 (2026-10-25)

- 首个独立版本 tag(此前随仓库 main 演进):init / add(含 name@version 钉版、
  安装基线)/ update(三方合并)/ remove(DAG 反向校验)/ doctor / list;
  registry 支持本仓库目录与 git URL(clone 缓存 + 增量刷新)。

## kernel v0.4.0 (2026-10-25)

- testutil 共享 fake(CapturePublisher/InlineRunner)+ AssertDepsAtAPILayer
  依赖护栏(ADR-0005 路线 A:装配保持手写,wire 借思想不借工具);
  详见 [kernel/CHANGELOG.md](kernel/CHANGELOG.md)。

## kernel v0.3.0 (2026-10-25)

- `DefaultChain(logger, set, agg, authnOpts)`(additive):模块贡献 Stage 时
  必须复刻的 server 默认链(recovery/requestid/clientinfo/authn/authz/
  validate)收进 kernel——修复可达性,详见
  [kernel/CHANGELOG.md](kernel/CHANGELOG.md)。

## kernel v0.2.0 (2026-10-25)

- 事件目录契约:`Module.PublishedEvents` / `ConsumedEvents` + 启动期交叉校验
  (消费的每个事件必须有已装发布方);
- `password.DummyHash()`:登录时序对齐哑靶(参数恒同参);
- 详见 [kernel/CHANGELOG.md](kernel/CHANGELOG.md)。

## kernel v0.1.0 (2026-10-25)

- 初始发布:装配协议 / errcode / contexts / events / uow+buntx / crud /
  goosekit / secretkey / clock / idgen / password / jwt / bunx / configkit /
  testutil;
- 详见 [kernel/CHANGELOG.md](kernel/CHANGELOG.md)。

## identity v0.1.1 (2026-10-25)

- 安全修复:登录时序对齐哑靶改调 `kernel/password.DummyHash()`(删模块内写死
  常量;kernel 升参数后旧哑靶失配、时序对齐静默失效);
- goDependencies 提升至 `kernel >= v0.2.0`。

## identity v0.1.0 (2026-10-25)

- 注册 / 登录(Argon2id + HS256 JWT,purpose 隔离)/ 自助改密;
- 向应用贡献 Bearer 认证器:未声明 `anonymous` 的方法自动要求认证(fail-closed);
- 事件:`identity.user.created` / `identity.user.password-changed`;
- 安全边界:令牌无吊销(TTL 默认 30m);生产必须更换 `identity.secret`。

## session v0.1.0 (2026-10-25)

- 首个跨模块依赖(identity);可吊销会话:访问(15m)+刷新(30d)令牌对,
  Refresh 轮换刷新令牌(旧 jti 重放即吊销全会话),Logout/LogoutAll;
- 订阅 `identity.user.password-changed` 自动全局吊销。

## access v0.1.0 (2026-10-25)

- RBAC:角色目录配置声明(`access.roles`)+ 分配落库(幂等 Grant/Revoke);
- Enricher 认证后注入 roles/permissions claims——任何模块可在 proto 声明
  `permissions` / `roles`,与令牌来源无关;
- bootstrap 管理员邮箱启动期收敛。

## audit v0.1.0 (2026-10-25)

- 审计三路径:RPC 拦截器自动记录(Order=50 槽位)+ 显式记录(同事务)+
  分页查询(append-only,零模块依赖)。

## demo v0.1.0 (2026-10-25)

- 单实体 CRUD 样例,验证装配管线(不承载业务语义)。
