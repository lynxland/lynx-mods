# Launch post draft (Show HN / r/golang)


> 语境注（2026-11-03）：本文写于拆分与两次更名之前——文中 lynx-start 指当时
> 全仓项目（今 appkit + 本仓），`lynx-start` 命令为现 CLI 前身（曾随仓更名
> 为 lynx-mods，2026-11-03 起复名 lynx-start）。仓库链接均指旧仓，发布前
> 需按届时真实名称/URL 重写。
> 发布素材初稿（2026-10-26）。标题二选一；正文按 Show HN 惯例短而实，
> 细节由 README 承接。发布前用真实账号跑一遍 quickstart 再贴命令。

---

**Title option A:** Show HN: Lynx-Start – shadcn-style backend modules for Go (you install source code, you keep it)

**Title option B:** Lynx-Start – Start from modules, not from scratch (Go backend modules distributed as source)

---

shadcn changed how front-end components are distributed: you don't add a
dependency, you copy the code — and then it's yours to modify forever. Backends
never got that. We kept choosing between scaffolders that generate 10k lines
you'll fear to upgrade (JHipster) and frameworks you depend on at runtime
forever.

Lynx-Start is an experiment in applying the shadcn model to Go backends:

- a **kernel** library that owns the stable contracts (assembly protocol,
  error codes, event envelopes, unit-of-work, pagination) — semver'd, boring;
- a **registry** of business modules — each one a real Go module with domain
  logic, proto API, and migrations: `identity` (Argon2id + JWT), `session`
  (revocable token pairs), `access` (RBAC), `audit`, `organization`,
  `notification` (event-driven in-app notifications), `task` (the first
  organization-scoped business resource);
- a **CLI** that installs modules as source into your app and keeps them
  updatable.

The part I'm most proud of is `update`. Since installed code is yours and you
will edit it, updates are **three-way merges** (your edits / the installed
baseline / upstream). Your customizations survive; upstream security fixes
arrive. Conflicts produce honest markers, and a `.lynx-mods/lock.json` pins
what you installed with sha256 hashes of the baseline — tampering is detected,
`doctor` re-verifies everything offline.

Because "source you copied" is a supply-chain problem, the boring parts are
covered: a security-advisory channel with a CI-gate command (`lynx-start
audit` exits non-zero on known advisories), version-range constraints checked
at install time (not at 3am at startup), per-module e2e combination matrix in
CI (every dependency edge actually runs), and `doctor --format github` so
installed apps can gate their own CI.

What it deliberately is **not**: no second database dialect (Postgres + bun
only), no distributed events or outbox (in-process bus), HTTP surface is
generated from proto annotations, and it's a modular monolith — one process.
Multi-tenancy is "soft": organizations with membership facts, scoped queries
and an explicit upgrade path, not row-level security by default.

Honest status: bus factor 1, docs are bilingual (Chinese-first), the module
catalog is 8 modules deep. It's an early experiment looking for its first
external users and contributors — especially people who hit the walls I
haven't found yet.

```bash
go install github.com/lynx-go/lynx-start/cli/cmd/lynx-start@latest

export LYNXMODS_REGISTRY=https://github.com/lynx-go/lynx-start.git
lynx-start init myapp --module github.com/acme/myapp
cd myapp
lynx-start add identity && lynx-start add session   # deps DAG-checked
# ...or the whole base in one command:
# lynx-start init myapp --preset saas

lynx-start generate && go mod tidy                  # buf-pinned codegen (mise pins go/buf)
mise run db:up && mise run db:migrate && mise run dev
curl -X POST :8080/v1/identity:register -d '{"email":"a@b.c","password":"..."}'
```

Repo: https://github.com/lynx-go/lynx-start
A full dogfooded app (7 modules, real migrations, event flows):
https://github.com/lynx-go/lynx-start-demo

Happy to go deep on any design decision — assembly protocol, why update is a
merge instead of a codemod (yet), how tenancy stays soft on purpose. AMA.

---

## 内部备注（发布对照清单，发完可删）

- 命令块发布前在本机冷启动实测一遍（proxy @latest 缓存延迟的坑：
  刚发版时 @latest 可能指旧版）。
- HN 用 Title A；r/golang 用 Title B（避免 Self-promo 规则争议，正文同）。
- 第 7 行 modules 计数=8（demo/identity/session/access/audit/organization/
  notification/task）——后续加模块要同步改。
- 帖子刻意不提 lynx 生态绑定（grpcapi/lynx 框架是 kernel 契约的一部分，
  追问再答）。
