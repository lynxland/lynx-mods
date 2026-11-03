# 贡献指南 / Contributing

## 新增一个业务域模块 / Adding a module

1. **写模块**:按 [docs/module-authoring.md](docs/module-authoring.md)([English](docs/module-authoring.en.md))
   的四层结构在 `modules/<name>/` 实现一个真实 Go module(自带 go.mod 与 genproto)。
2. **登记**(一处输入三处一致,item.json 骨架 / registry.json / go.work):

   ```bash
   lynx-start registry add modules/<name>
   lynx-start registry check        # 三处一致性体检(可入 CI)
   ```

3. **补齐清单**:填 item.json 的 `description` 与 `publishedEvents`
   (后者必须与 `Module().PublishedEvents` 一致——启动期校验的事实源)。
4. **验证**:`mise run lint && mise run test`,并确认 CI 的 e2e 组合矩阵
   (`.github/workflows/ci.yml`,新增模块后补一个组合)。
5. **发版**:`git tag modules/<name>/v<x.y.z>`(tag 必须指向 item.json 已写
   该版本的提交,安装器校验漂移);推送 tag 自动建 GitHub Release。

组合预设(preset)在 `registry/presets.json` 登记(组合顺序即安装顺序)。

## 安全公告 / Security advisories

模块安装即断连,已装用户**只能靠公告**得知安全修复。修复合入后,在
`registry/advisories.json` 追加条目(affectedVersions 精确列出受影响版本,
不写范围);应用侧 `lynx-start doctor` / `lynx-start audit` 比对已装版本
报告。安全敏感实现优先下沉 kernel(semver 推修复),公告兜底模块侧残余。

## 发布 kernel / cli / English docs

- kernel:破坏性变更(0.x 阶段)升 minor;`kernel/CHANGELOG.md` 与根
  `CHANGELOG.md` 各加小节;`git tag kernel/vX.Y.Z`。
- cli:`git tag cli/vX.Y.Z`(根 CHANGELOG 加 `## cli vX.Y.Z` 小节)。
- 文档双语并行:中文为源,英文镜像;两版分歧时以中文为准并尽快同步。

---

1. **Write the module** following [docs/module-authoring.en.md](docs/module-authoring.en.md)
   ([中文](docs/module-authoring.md)) — a real Go module under `modules/<name>/`.
2. **Register** (one command keeps item.json / registry.json / go.work consistent):

   ```bash
   lynx-start registry add modules/<name>
   lynx-start registry check
   ```

3. **Fill the manifest**: `description` and `publishedEvents` in item.json
   (the latter must match `Module().PublishedEvents`).
4. **Verify**: `mise run lint && mise run test`; add a combo to the CI e2e matrix.
5. **Release**: `git tag modules/<name>/v<x.y.z>` (the tag must point at a
   commit whose item.json already carries that version); the release workflow
   creates the GitHub Release automatically.

Security fixes: append an entry to `registry/advisories.json` (exact
`affectedVersions`, no ranges); app-side `lynx-start doctor`/`audit` reports
installed versions against it. Prefer sinking security-sensitive code into
kernel (fixes ship via semver); advisories cover the module-side remainder.
