# ADR-0007: 更名 lynx-mods → lynx-start

日期：2026-11-03
状态：已接受

## 背景

本仓 2026-10-31 自 lynx-start（模块分发拆出，ADR-0006）得名 lynx-mods；
2026-11-02 基础库更名 appkit（原 lynx-start，见 appkit 仓 ADR 谱系），
"lynx-start" 名号空出。模块分发是 shadcn 语义的「起点套件」——装上即得
一个能跑的 lynx 应用骨架——与 lynx-start（起点）语义天然契合，且本仓正
是自 lynx-start 拆出，名随实归。GitHub 仓随之 `lynxland/lynx-mods` →
`lynxland/lynx-start`。

## 决策

1. **module path 换系**：8 个模块与 cli
   `github.com/lynxland/lynx-mods/...` → `github.com/lynxland/lynx-start/...`
   （互 import、go.mod module/replace/require、item.json.modulePath、
   `.proto` go_package、genproto 生成物、registry 缺省推导同步）。
2. **CLI 命令与产品工件全部随更名**：二进制与 cmd 目录 `lynx-mods` →
   `lynx-start`；应用清单 `lynx-mods.json` → `lynx-start.json`；应用目录
   `.lynx-mods/` → `.lynx-start/`；用户缓存 `~/.lynx-mods` → `~/.lynx-start`、
   `~/.cache/lynx-mods` → `~/.cache/lynx-start`；env
   `LYNXMODS_REGISTRY`/`LYNXMODS_E2E_MODULES` → `LYNXSTART_REGISTRY`/
   `LYNXSTART_E2E_MODULES`（顺带修掉 init list 误写的 `LYXSTART_REGISTRY`
   存量笔误）；注入锚点 `lynx-mods:module-import/module-entry/
   config-fragment` → `lynx-start:`（scaffold 模板同步）。
3. **不留兼容层**（与 0.2.0 的 CLI 更名同款做法）：不做旧清单文件名回退
   读取、不做旧锚点双识别。理由：项目尚在 dogfood 早期，存量应用（自装
   dogfood 仓库）数量小且归自己所有；兼容分支会长期滞留代码面，换取的
   只是一次性手工迁移的省力，不值。迁移表见 CHANGELOG「全模块 v0.4.0 +
   cli v0.5.0」条目。与工件名不同，**模块路径历史 tag 必须继续可装**——
   前缀兼容表扩为四系：lynxland/lynx-start（新 canonical）+
   lynxland/lynx-mods + lynx-go/lynx-mods + lynx-go/lynx-start（历史）。
4. **抬版重发**：module path 变更使旧 tag 对新路径失效，全模块统一抬版
   `modules/<m>/v0.4.0`、`cli/v0.5.0`。发布顺序：先 GitHub 更名仓库，
   后打推 tag（旧仓名下打新路径 tag 会被 module proxy 判 go.mod 路径与
   仓库不符）。
5. **advisory 前缀**：新公告用 `LYNXSTART-` 前缀自 LYNXSTART-0002 顺延
   （LYNXSTART-0001 为旧基础库时代历史 ID，保留不复用）。
6. **历史文档零改写**：CHANGELOG 历史条目、ADR-0001～0006、拆分前提案
   文档（optimization-plan、m4-update-proposal、launch-post-draft）按既定
   政策保留旧名，仅允许加日期化语境注/括注（本次给三文档加了语境注，
   CHANGELOG 继承节标题加了限定括注防同文异义——更名后 "lynx-start"
   既指本仓又出现在历史叙述里）。

## 后果

- 存量 lynx-mods 时代应用需一次性手工迁移（清单/应用目录 git mv、锚点
  文本替换、env/mise 任务改名、用户缓存弃置重建）；未迁移前新 CLI 报
  `lynx-start.json not found`（预期行为）。
- 前缀表四系后 `canonicalBothPrefixes` 对每个 canonical 生成全系改写规则，
  混代安装（历史 tag + 工作区 item）继续成立；`TestDualPrefixCompat`
  增 lynxland/lynx-mods legacy 行钉住。
- 文档与代码里 "lynx-start" 一词自此指本仓；叙述旧基础库时必须写
  「lynx-start（今 appkit）」。ADR-0006 迁移时就存在的 X→X 文本损坏
  （待从旧仓 git 历史修复）不在本 ADR 处理。
