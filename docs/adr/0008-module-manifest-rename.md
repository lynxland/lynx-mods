# ADR-0008: registry 清单更名 item.json → module.json

日期：2026-11-04
状态：已接受

## 背景

模块清单文件自拆分以来一直叫 `modules/<name>/item.json`——名字继承自
registry「条目」的实现视角，对读者不表意：它不是某个列表里的条目，而是
一个模块的清单（name/version/依赖约束/configFragment/publishedEvents/env，
模块版本的唯一事实源）。与仓库级索引 `registry/registry.json` 并置时，
「registry.json 列出模块、module.json 描述单个模块」的对称命名才成立。

## 决策

1. **文件更名**：`modules/<name>/item.json` → `modules/<name>/module.json`。
   候选里否决 `manifest.json`（比 item.json 更通用，repo 里 registry.json
   本质也是 manifest，反而添歧义）与 `lynx-module.json`（品牌前缀在
   错误信息/文档里更显眼，但 module.json 在 `modules/<name>/` 目录语境下
   已自明，无需再钉品牌）。
2. **Go 标识符随更名**：`registry.Item` → `registry.ModuleManifest`，
   方法 `Item`/`ItemAt`/`ItemAtCommit` → `Manifest`/`ManifestAt`/
   `ManifestAtCommit`，`loadItem`/`LoadItem` → `loadManifest`/`LoadManifest`；
   错误信息（tag/manifest 漂移、commit 版本对账）与渲染 skip 过滤同步。
   `Index.Items`（registry.json 的 `items` 数组）不动——那是索引条目，
   与清单是两个概念。
3. **不留兼容层**（ADR-0007 政策）：CLI 不回退读取旧文件名。理由同前：
   dogfood 早期、存量应用归自己所有、兼容分支长期滞留代码面不值。
4. **抬版重发**：清单随 git tag 的树分发，旧 tag 树内只有 item.json——
   改名后 `read module.json` 直接失败，旧版本不可安装。全模块统一抬版
   `modules/<m>/v0.5.0`、`cli/v0.6.0`（mise release 任务改读 module.json）。
5. **历史文档零改写**（ADR-0007 第 6 条政策）：ADR-0006/0007、m4 提案、
   CHANGELOG 历史条目保留 item.json 原文；活文档（CONTRIBUTING 中英、
   module-authoring 中英、optimization-plan、domain 注释）随更名。
   注意 optimization-plan 第 58 行 shadcn 的 `registry-item.json` 是竞品
   规范名，不在更名之列。

## 后果

- 存量应用（lock pin 0.4.0）升级路径：先取新 cli，再 `lynx-start add <m> -o`
  重建基线（lock 重建为 0.5.0），此后 update 恢复可用；doctor 会对未迁移
  应用报 lock/registry 版本对账失败（预期行为，报错含可行动指引）。
- 贡献者视角 `registry add` 生成的骨架、`registry check` 的体检项、
  CONTRIBUTING 发版说明均指向 module.json；`demo.item.created` 等事件名
  与文件名无关，不受影响。
