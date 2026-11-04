# 模块编写指南

> English version: [module-authoring.en.md](module-authoring.en.md) — 本文为源文档,两版分歧时以中文版为准。

面向两类读者：

- **应用开发者**——装了几个 registry 模块之后，要写自己的业务域；
- **模块贡献者**——想把一个业务域发布回本 registry。

两种姿势共用同一套结构与契约，差别只在最后一节（是否走 `lynx-start add` 分发）。
通读约 15 分钟；对照 `modules/demo`（466 行，最小完整样例）与
`modules/identity`（带认证器与配置段）阅读最佳。

---

## 0. 先记住三条

1. **源码归你**。模块装进应用后就是你的代码——改字段、换仓储、删方法都不需要
   任何人的许可，编译器兜底。本指南讲的是「按什么结构写，才能享受契约保护」，
   而不是「必须怎么写」。
2. **模块间只许 domain 级依赖**。跨模块 import 只允许对方的
   `domain/<name>` 包；app/infra/api 互引直接编不过（目录布局强制），
   编译器就是架构护栏。
3. **安全敏感实现住 kernel，模块源码只留编排**。源码分发与库的分界
   不是「重要不重要」，而是**「应用会不会想改」与「出了事要不要能远程修」**：

   | 代码性质 | 分发通道 | 例子 |
   |---|---|---|
   | 用户会改的业务逻辑 | 模块源码（`lynx-start add`） | 聚合、用例流程、policy 取舍 |
   | 用户不想改、出 CVE 必须能推送修复 | kernel（Go module，semver） | Argon2 参数、JWT 校验、时序对齐哑靶 |
   | 接口派生物 | 生成（buf / protoc 插件） | DTO、HTTP/GRPC 骨架 |

   模块源码一旦装进应用就与 registry 断连——`lynx-start update` 靠人触发
   三方合并，安全修复不能指望它。所以凡是「参数错了就是漏洞」的实现
   （哈希参数、恒定时间比较、令牌校验、随机数），一律下沉 kernel：
   应用 `go get kernel@fix` 即修复，无需逐模块合并。反例教训：identity
   曾把登录时序对齐的哑靶 Argon2 常量写死在模块源码里——kernel 升参数后
   哑靶失配、时序对齐静默失效；v0.1.1 起改调 `password.DummyHash()`
   （kernel 惰性生成，参数恒同参）。
4. **六条契约**（ADR-0004）速查：

   | # | 契约 | 落点 |
   |---|---|---|
   | 1 | 分层归属 | `domain/<m>` + `internal/{app,api,infra}/<m>` + `proto/<m>/v1` |
   | 2 | 模块间仅 domain 级 DAG | `appkit.Module.DependsOn` 声明，启动期校验 |
   | 3 | 配置自治 | 自有 Config struct + `configkit.LoadSection` |
   | 4 | 迁移隔离 | 独立 goose 版本表 `goose_<m>_db`，禁跨模块外键 |
   | 5 | 错误码区段 | `errcode.MustRegister("<m>")` |
   | 6 | Kernel 无租户预设 | 多租户是未来横切模块，不进 kernel |

## 1. 模块解剖

模块**仓内**结构（registry 侧、贡献者维护）与**装进应用后**的形态
（ADR-0006 安装映射，模块代码零改动）：

```
modules/<m>/（仓内）            <app>/（安装后，尾段拍平）
├── domain/<m>/                 ├── modules/<m>/
│   ├── item.go                 │   ├── module.go        # 桥接包（add 生成：转发 Module/Assemble）
│   ├── repo.go                 │   ├── domain/          # 对外契约面：聚合/端口/哨兵/事件常量
│   └── guard_test.go           │   ├── internal/app/    # 用例层：编排领域与端口
├── internal/{app,api,infra}/<m>/ │   ├── internal/api/    # Service 实现（薄转发）+ Module()/Assemble()
└── proto/<m>/v1/<m>.proto      │   └── internal/infra/  # bun 仓储 + migrations/（embed.FS）
                                └── proto/<m>/v1/<m>.proto  # 契约源集中应用根（genproto/ 生成物亦在应用根）
```

依赖方向恒定：`api → app → domain ← infra`。装配在模块内
`internal/api/<m>/module.go`；应用经桥接包 `modules/<m>`（Go internal
可见性：模块内 internal 只有 modules/<m>/ 树内可 import）。

## 2. 动手：写一个 `todo` 模块（应用内私有）

以下步骤在**你的应用仓库**里做；每步给出 demo 模块对应文件，直接对照抄。

### 第 1 步：domain——聚合、错误、仓储端口

```
domain/todo/task.go   # 聚合 Task + NewTask/Complete（不变量在此）
domain/todo/repo.go   # type Repository interface { Create/Get/List/Update... }
```

要点（对照 `modules/demo/domain/demo/item.go`）：

```go
var space = errcode.MustRegister("todo")          // 契约⑤：区段名=模块名

var (
    ErrTaskNotFound = space.New("TASK_NOT_FOUND", "todo task not found")
    ErrEmptyTitle   = space.New("EMPTY_TITLE", "title must not be empty")
)

// 事件目录（契约②）：app 层发布与 Module().PublishedEvents 共用同一常量。
const (
    EventTaskCreated = "todo.task.created"
)
```

哨兵错误由 app 层转 `grpc/status`（携带 ErrorInfo）；仓储接口方法一律接收
`context.Context`——infra 适配器从 ctx 取事务连接（`kernel/uow/buntx`）。

### 第 2 步：infra——bun 仓储 + 迁移

```
internal/infra/todo/repo.go                      # 实现 domain/todo.Repository
internal/infra/todo/migrations/00001_init.sql    # CREATE TABLE ...（goose 格式）
internal/infra/todo/migrations/embed.go          # //go:embed *.sql → var FS embed.FS
```

表名约定 `<module>_<entity>`；**不写跨模块外键**（契约④）。Update 用显式列
白名单（见 `modules/demo/internal/infra/demo/repo.go`）。迁移链被 goosekit
以独立版本表执行，回滚不牵连别的模块。

**组织挂靠资源（软多租户，ADR-0008）**：业务资源模块（todo/project 类，
DependsOn: organization）的表第一天就按标准姿势挂组织——
`organization_id text NOT NULL`（只存 ID，契约④）；**复合唯一约束必含
organization_id**（跨组织重名合法）；列表索引以 organization_id 打头；
资源路由挂 `/v1/organizations/{organization_id}/<resources>`；资源读写前
经 ADR-0007 通道②（organization domain 只读端口）校验成员资格，repo 查询
显式携带 organization_id。租户键不变量：organization_id 是**唯一**租户键，
不引入 tenant_id/workspace_id 等平行概念。

### 第 3 步：app——用例编排

```
internal/app/todo/todo.go    # type UseCase struct{repo, tx, events, now}
```

对照 `modules/demo/internal/app/demo/demo.go`：写操作包在
`u.tx.Run(ctx, func(ctx) error { ... })` 里，仓储与事件发布共享同一事务；
事件名引用 domain 常量（如 `todo.EventTaskCreated`），载荷必须 `json.Marshal`
编码（手搓字符串遇到含引号/反斜杠的值会产生非法 JSON）；发布走模块自己的
publish 帮手（demo/identity 同款）。

### 第 4 步：proto——API 契约与策略声明

```
proto/todo/v1/todo.proto
```

对照 `modules/demo/proto/demo/v1/demo.proto`：

```proto
import "google/api/annotations.proto";
import "grpcapi/v1/policy.proto";

option (grpcapi.v1.api) = { api: "public", default_exposure: EXPOSURE_PUBLIC };

service TodoService {
  // 默认要求认证（装了 identity 后即 Bearer）；公开方法显式声明：
  option (grpcapi.v1.service) = { default_policy: { anonymous: true } };

  rpc CreateTask(CreateTaskRequest) returns (Task) {
    option (google.api.http) = { post: "/v1/tasks" body: "*" };
  }
}
```

策略注解速查（`grpcapi/v1/policy.proto` 经 `buf.yaml` 的
`buf.build/lynxland/grpcapi` 依赖从 BSR 解析，事实源是 grpcapi 仓
`proto/grpcapi/v1/policy.proto`——模块与应用都不 vendor 该文件，Go 类型
由 `github.com/lynxland/grpcapi` 提供）：

- `anonymous: true`——方法公开；
- `permissions: [...]`（MATCH_ALL）/ `roles: [...]`（MATCH_ANY）——装了
  access 模块后生效（Enricher 注入 claims 后比对）；
- 未声明 `anonymous` 的方法在装有认证器时**自动要求认证**（fail-closed）。

### 第 5 步：api——Service 实现 + 装配

```
internal/api/todo/service.go   # 薄转发：参数校验交给 protovalidate，业务在用例层
internal/api/todo/module.go    # Module() + Assemble()
```

`module.go` 的骨架（对照 `modules/demo/internal/api/demo/module.go`，安装时
import 已改写为你的应用路径）：

```go
func Module() appkit.Module {
    return appkit.Module{
        Name:        "todo",
        Description: "待办任务",
        Policies:    []protoreflect.FileDescriptor{todov1.File_todo_v1_todo_proto},
        Migrations:  migrations.FS,
        PublishedEvents: []string{todo.EventTaskCreated},               // domain 常量，与发布站点同源
        ConsumedEvents: nil,                                           // 订阅跨模块事件时在此声明
        // DependsOn: []string{"identity"},   // 仅当 domain 层 import 了 identity 的 domain 包
    }
}

func Assemble(deps appkit.Deps) (appkit.Assembly, error) {
    uc := apptodo.New(infratodo.NewRepository(bunx.MustDB(deps)), deps.Tx, deps.Events)
    return appkit.Assembly{
        Registrations: []server.Registration{newRegistration(NewService(uc))},
    }, nil
}
```

**事件目录安全网**：`ValidateModules`（启动期执行，migrate 也跑）交叉校验
目录——发布名合法且首段归属本模块、无重复发布；**消费的每个事件都有已装
模块发布**。订阅别模块事件时的 typo、上游改名、漏装发布方，都在启动期
fail-closed，而不是运行期静默不触发。消费方按契约②不 import 发布者代码，
消费的事件名常量定义在自己的 domain（对照 session 的
`IdentityUserPasswordChanged`）。

**跨模块协作通道分级**（ADR-0007，优先从上往下）：异步联动/事实广播 →
事件目录；读他模块数据（简单键值查询）→ 消费方 domain 端口 + 自持**只读**
适配器（userReader 模式，写方法一律报错）；同步调他模块**写**能力/跨模块
业务步骤 → 供给方 domain 能力接口 + 模块根包 `exports.go` 转发构造器 +
应用组合根显式接线（同事务靠 ctx 携带，`buntx.Conn(ctx)` 自然共享）；
跨进程/外部触发 → 模块自身 RPC 面。**读写不对称是本质的：读可消费方
自持，写必须供给方导出**（消费方直写他人的表 = 所有权漂移）。

需要配置段时（对照 identity）：在 app 层定义 Config struct + `ApplyDefaults`，
Assemble 里 `configkit.LoadSection(deps.Config, "todo", cfg.ApplyDefaults, &cfg)`，
并把片段手写进 `configs/config.yaml`（registry 模块由安装器经 module.json 的
`configFragment` 自动追加）。

### 第 6 步：生成代码

```bash
mise run generate    # lynx-start generate：buf 管线（genproto/** 落应用根）
go mod tidy
```

### 第 7 步：登记

`modules/modules.go` 的 `Installed` 列表追加（锚点内；import 指向模块
桥接包，`lynx-start add` 自动注入——手写私有模块时照抄）：

```go
import todo "<app-module-path>/modules/todo"
// ...
{Module: todo.Module(), Assemble: todo.Assemble},
```

然后 `mise run db:migrate && mise run dev`。新方法的 HTTP 路由、认证要求、
权限声明全部由 proto 注解驱动，无需改 main.go——那是骨架的稳定部分，
模块增减只动 modules.go。

## 3. 装配贡献面速查

| 贡献 | 静态（`Module()`） | 动态（`Assemble()`→`Assembly`） | 实例 |
|---|---|---|---|
| gRPC/HTTP 服务 | — | `Registrations` | demo |
| 策略描述符 | `Policies` | — | 全部 |
| 迁移链 | `Migrations` | — | 全部 |
| 发布事件目录 | `PublishedEvents` | — | demo / identity / session / access |
| 消费事件目录 | `ConsumedEvents` | — | session（改密联动） |
| 事件订阅/后台服务 | — | `Services` | session（订阅改密事件） |
| 定时任务 | — | `Tasks` | — |
| 认证器 | — | `Authenticators` | identity（Bearer） |
| 认证后增强 | — | `Enrichers` | access（roles/permissions claims） |
| 拦截器槽位 | — | `Stages`（audit=50/ratelimit=40/usage=60） | — |
| 手写 HTTP 路由 | `ExtraRoutes` | `ExtraRoutes` | — |
| 依赖声明 | `DependsOn` | — | session→identity |

`Assemble` 收到的 `appkit.Deps`：`DB`（bun）、`Tx`（uow）、`Events`（发布端口）、
`Bus`（订阅侧）、`Config`、`Logger`。依赖显式、无生成式 DI。

## 4. 发布到 registry（模块贡献者）

把模块放进本仓 `modules/<name>/` 并让它成为**真实 Go module**：

1. 自带 `go.mod`（module path 形如
   `github.com/lynxland/lynx-start/modules/<name>`）、`buf.yaml`/`buf.gen.yaml`
   （deps 声明 `buf.build/lynxland/grpcapi`：policy.proto 从 BSR 解析，
   不 vendor 进模块），genproto 入库——在本仓可编译、可测试，kernel 破坏性
   变更在本仓 CI 立即暴露，而不是等到某个应用安装时才炸；
2. 写 `module.json`：

   ```json
   {
     "name": "todo",
     "version": "0.1.0",
     "description": "待办任务",
     "modulePath": "github.com/lynxland/lynx-start/modules/todo",
     "moduleDependencies": [],
     "goDependencies": [],
     "configFragment": "todo:\n  default_page_size: 50\n",
     "publishedEvents": ["todo.task.created"]
   }
   ```

   - `moduleDependencies`：安装器据此校验 DAG 并改写依赖方 import；
   - `goDependencies`：安装后需手动 `go get` 的第三方库；
   - `configFragment`：追加进应用 config.yaml 的段（YAML 文本）；
   - `publishedEvents`：展示用目录数据，**与 `Module().PublishedEvents` 保持
     一致**（后者是启动期被校验的事实源）；
一条命令完成登记与三处同步（module.json 骨架 / registry.json / go.work）：

   ```bash
   lynx-start registry add modules/<name>   # 骨架生成后补 description/publishedEvents
   lynx-start registry check                # 一致性体检（可入 CI）
   ```

   模块根另放 `README.md`（registry 元文档：安装/更新管线跳过，不入应用树；
   内容以 module.json 为事实源，参照任一内置模块）。

3. 在 `registry/registry.json` 的 `items` 里登记 `{ "name", "path" }`；
4. 加入根 `go.work`，`mise run lint && mise run test` 全绿后提交；
5. **发布版本 = 打 git tag** `modules/<name>/v<x.y.z>`（与 `kernel/v<x.y.z>`
   同款子目录前缀形式）。tag 必须指向「module.json 已写该版本号」的提交——
   安装器会校验 tag 与清单一致，漂移直接拒绝。版本语义（0.x 阶段）：
   破坏性变更（迁移不兼容 / proto 字段删改 / 装配签名变化）升 minor，
   安全修复与向后兼容追加升 patch。

### 版本安装与更新（应用侧）

- `lynx-start add todo@0.1.0` 从 tag 提取该版本源码（可复现安装）；
  不带 `@version` 取 registry 工作区最新形态；
- `add` 在用户目录 `~/.lynx-start/baselines/<appModulePath>/<name>@<commit>/`
  写**安装基线**（安装后最终形态：已改写导入路径、已 gofmt）。基线是可
  重建缓存（不进应用仓库）：随应用提交的只有 `.lynx-start/lock.json`
  （供应链锁）与 `lynx-start.json`（manifest）——缓存缺失时 update/doctor
  按 lock 记录的 commit 自动从 registry 重建，跨机器不再依赖提交基线；
- `lynx-start update todo` 三方合并到 registry 最新：**本地 / 基线 /
  upstream**。本地与 upstream 改动不重叠则自动合并；同区改动打 git
  冲突标记（`<<<<<<< yours / baseline / theirs`），覆盖前的本地副本留在
  `~/.lynx-start/backups/<appModulePath>/<name>/`；upstream 删除的文件只
  提示不自动删。
  合并写盘不可回滚——**update 前先 commit 应用**；
- 基线不变量：基线 ≡ upstream 最近一次同步形态。冲突文件解决后基线仍为
  upstream 形态，你的解决结果作为本地改动被下一次 update 自然保留。
  旧清单（无 `files` 字段）没有基线，update 会提示 `add <name> -o` 重建
  （会覆盖本地文件，先提交本地改动）。

## 5. 常见改动配方（改已装模块）

| 想要 | 动哪里 |
|---|---|
| 加字段 | proto message → `mise run generate` → 新迁移加列 → domain 聚合与 repo 白名单列 |
| 加方法 | proto service + http 注解 → generate → api Service 实现 → app 用例 |
| 改配置默认值 | app 层 `Config.ApplyDefaults` + 应用 config.yaml 对应段 |
| 订阅别模块事件 | domain 定义消费常量 + `Module().ConsumedEvents` 声明 + Assemble 经 `deps.Bus` 订阅（对照 session 订阅 `identity.user.password-changed`）；启动期校验确有发布方 |
| 收紧/放宽认证 | proto policy 注解（`anonymous` / `permissions` / `roles`），无需改代码 |

改完跑 `mise run lint && mise run test`。你的 guard_test 会拦住越界的
import（domain 不许 import infra/app/api）。

## 6. 替换模块内部实现(定制姿势)

源码归你,直接改就是正道;但不同改法与 `lynx-start update` 三方合并的相处
难度不同。按摩擦从低到高:

| 想改什么 | 推荐姿势 | update 冲突风险 |
|---|---|---|
| 仓储实现细节(某条 SQL/索引提示) | 直接改 `modules/<m>/internal/infra/repo.go` | 低——upstream 少动这里,动了你也有明确语义优势 |
| 换整套存储实现 | 新建 `repo2.go` 实现同一 domain 端口,旧文件留着不动;只在装配层换构造 | 低——新增文件 update 天然保留(本地独有文件不参与合并) |
| 换哈希/签名/密码学参数 | **不要改模块**——这类实现住在 kernel,`go get kernel@vX` 升级即可 | 无 |
| 用例流程/业务规则 | 直接改 `modules/<m>/internal/app` | 中——upstream 演进主区,但三方合并对「你改你的、上游修上游」友好 |
| 装配接线(构造参数/依赖替换) | 改 `modules/<m>/internal/api/module.go` 的 Assemble | **高——这是 upstream 热区**,改动保持最小(换构造一行),别顺手重构 |

要点:

- **新增文件零冲突**:合并只处理「安装基线里登记过的文件」;你新建的文件
  (repo2.go、额外的适配器)永远原样保留;
- **改 Assemble 前先想清楚**:它是模块与 kernel 的接缝,upstream 每版都可能
  动;若确要换实现,把「新实现文件」与「接线一行」分开提交,冲突时只需重放
  接线那一行;
- **domain 端口是你的稳定锚**:只要端口签名不动,infra 随便换——这正是契约
  ②「跨模块只依赖 domain」给你自己的定制同样的保护;
- 定制后跑 `lynx-start doctor`(基线/锁仍完整)与 `go build ./...`。
