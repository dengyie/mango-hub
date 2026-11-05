FROM alpine:3.21

# Go 的 Linux 证书池来自系统；alpine 默认不带 ca-certificates，缺了它连不上
# Cloudflare 前面板（x509: certificate signed by unknown authority）。
RUN apk add --no-cache ca-certificates tzdata

WORKDIR /app

# Docker buildx 会在构建时自动填充这些变量
ARG TARGETOS
ARG TARGETARCH

COPY --chmod=755 mangohub-agent-${TARGETOS}-${TARGETARCH}* /app/mangohub-agent

RUN ln -sf /app/mangohub-agent /app/mango-agent && \
    touch /.mangohub-agent-container /.mango-agent-container

ENTRYPOINT ["/app/mangohub-agent"]
# 运行时配置走环境变量（容器平台只需换镜像名，不必重写启动命令）：
#   AGENT_ENDPOINT=https://vps.mangoqwq.com  AGENT_TOKEN=<节点专属 token>
# 平台若支持自定义 Command，等价于 --endpoint=... --token=...（CLI 优先级高于 ENV）。
# 容器内存在 /.mangohub-agent-container 标记，agent 会跳过二进制自更新，
# 升级只能靠换镜像——镜像由 .github/workflows/agent-image.yml 发布。
