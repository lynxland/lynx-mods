# 模块粒度与自足性——模块=产品能力单元

2026-11-06 决策。四轮评审反馈的收敛：「模块划分过细，单独拎出来基本都
无法独立使用」+「notification/task 太弱」。本 ADR 定模块目录的粒度判据
与自足性分级，并记录首次应用：identity+session+access 合并为 account
（目录 8→6，破坏性重发 v0.6.0）。

## 背景

- 拆分前（v0.5.0 及以前）的 8 模块中：session/access/organization/task
  因 DAG 强依赖不能单独装；notification 单独装启动即 fail（消费事件无
  供给方）；identity 单独装「能登录不能登出」；audit 单独装「能记录不能
  查」。**模块边界=账号域的技术分层切片，不是产品能力单元**——违背
  shadcn 参照系的核心价值（每个可分发单元独立可用、组合增强）。
- 根因：identity/session/access 是「账号与访问」一件事的三片——依赖边
  （session→identity、access→identity）不是模块间协作，是一个能力的
  内部分层。ADR-0004 的通道②（userReader 只读适配器）被用于跨越本不该
  存在的模块边界。

## Considered Options

- **A. 合并为粗粒度产品能力单元（选定）**：按「用户拿来干什么」划分，
  不按「内部怎么分层」划分。账号与访问=一件事=account。
- **B. 保持细分但让每个模块自足**：每个模块自带最小会话/权限……重复
  实现面爆炸，且 identity 内置会话与 session 模块职责冲突。
- **C. 分发单元与模块单元分离（bundle/preset 打包）**：CLI 层打包缓解
  安装摩擦，但模块数不减、依赖边不减，源码树仍是碎片。

## Decision

1. **粒度判据**：一个 registry 模块 = 一个可独立价值主张的产品能力
   （「认证授权」「组织协作」「后台任务调度」「审计」），不是一层技术
   切片。内部多域以 domain/<模块名>/<域>/ 子包保留边界。
2. **自足性分级（registry 元数据与 README 必须如实声明）**：
   - **独立模块**：单独安装即完整可用（account / task(调度重写后) /
     audit(记录面) / demo）；
   - **增强模块**：依赖明示、安装即拉齐完整能力（organization→account
     「装组织=得到完整账号底座」；notification→account+organization
     事件消费+Sender 显式发送）。
3. **合并的机械约定**（首次应用沉淀，后续合并照抄）：
   - proto 包与 genproto 路径原样保留（**API 面与客户端零改动**）；
   - 布局契约：源树 `domain/<模块名>/<域>/` 安装时剥掉模块名层（CLI
     relocateModule 尾段拍平规则的嵌套推广，零 CLI 改动）；
   - 事件名统一 `<模块名>.*` 前缀（compose 目录所有权校验）；**全部
     订阅方必须同版升级**（全量重发）；
   - 迁移合并：逐段 IF NOT EXISTS、表名保留（表名是存量应用的数据
     资产）；goose 版本表按新模块名从新起，旧表残留无害；
   - 配置段保留（已装应用零配置迁移）；ConfigSpec.Defaults 以段为键
     嵌套表达。
4. **域边界护栏**：合并≠边界融化——模块内各域的 internal/{app,infra}
   不得互相 import（域间经 domain 端口协作，与跨模块同纪律；account 的
   guard_test 含 TestNoCrossDomainInternalImport 机器固化）。
5. **未来拆分自由度保留**：合并不锁死——account 若需要可再拆（拆比合
   易：域边界已被护栏固化）；细粒度需求（如不要 RBAC）用配置/删码承接。

## Consequences

- 目录 8→6：account（合并）/ task（v0.7.0 重写为后台任务与调度，甩掉
  organization 依赖变零依赖自足）/ organization / notification / audit /
  demo。preset saas = account+audit+organization+notification。
- 依赖边 7→3（organization→account、task→organization[重写后清零]、
  notification→account+organization 事件），全部必要。
- session→identity 的「改密吊销」联动内化为 account 模块内 watcher；
  ConsumedEvents 保留声明（compose 校验对「自产自销」合法，typo 防护
  价值保留）。
- 权限字符串 `access.assignment.write`→`account.assignment.write`
  （proto 注解+角色目录同改）；audit.log.read 保持（audit 模块未改名）。
- 存量应用迁移路径与「订阅方同版升级」约束写入 CHANGELOG v0.6.0 迁移表；
  advisories 历史条目（指旧模块名）保留不动。
- task 的「组织挂靠业务资源示例」职责（ADR-0005 姿势首秀）随重定位
  移交文档（module-authoring 姿势章节）——demo 保持唯一示例模块。
