# proto —— 应用契约源根

应用全部业务 proto 集中于此（`<模块>/v1/*.proto`，由 `lyx add <模块>` 落入）。

`grpcapi/v1/policy.proto` 等声明语法**不在本目录**：经 `buf.yaml` 的
`buf.build/lynxland/grpcapi` 依赖从 BSR 解析，Go 类型由
`github.com/lynxland/grpcapi` Go 模块提供（`buf generate` 只以本目录为输入）。
