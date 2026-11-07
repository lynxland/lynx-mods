# 多阶段构建：mise 钉版工具链（与开发环境一致）→ 单文件产物。
# 构建上下文是应用仓库根：docker build -t myapp .
FROM ghcr.io/jdxcode/mise:latest AS build
WORKDIR /src
COPY . .
# mise.toml 钉版 go/buf；genproto 已入库——镜像内无需重新 generate。
RUN mise install && mise exec -- go build -o /out/server ./cmd/server && mise exec -- go build -o /out/migrate ./cmd/migrate

FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates && rm -rf /var/lib/apt/lists/*
WORKDIR /app
COPY --from=build /out/server /out/migrate ./
# 配置走环境变量（{{.EnvPrefix}}_ 前缀覆盖约定）或挂载 configs/；secret
# 永不进镜像（.env.example 是文档，不是运行时输入）。
ENV {{.EnvPrefix}}_DATA_DATABASE_SOURCE=""
EXPOSE 8080 9060
USER nobody
ENTRYPOINT ["./server"]
