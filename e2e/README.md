# 业务级 e2e（模板注入式）

业务级端到端测试在**生成的应用内**执行（Go internal 可见性：模块的
Module/Assemble 经应用桥接包暴露，树外测试拿不到——本目录因此是模板
而非独立测试包）。`mise run test:business` 一条命令完成：

1. init 一个临时应用并安装 preset saas + task（全量业务模块）；
2. 把 `template/saas_test.go.tmpl`（`{{.ModulePath}}` 占位）渲染进应用
   `test/` 目录；
3. 带真实 PG（`APPIKIT_TEST_PG_DSN`）跑 `go test`。

覆盖主链：注册→邮箱验证→登录锁定→密码重置（枚举防护）→会话设备→
组织邀请→所有权转让→软删→任务入队防重→审计可查（状态码面）。
