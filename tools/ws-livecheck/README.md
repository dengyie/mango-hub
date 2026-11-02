# ws-livecheck

部署后从外部验证 MangoHub WebSocket 契约的一次性工具（独立 go module，不参与 hub/agent 构建）。

## 检查内容

1. **重复连接契约**：用 admin API 取一个**在线**节点的 token，再发起第二个携带同一 token 的 WebSocket——必须被拒：`close 1008`，reason `duplicate connection rejected: client <uuid> already has an active connection`；随后校验该节点上报时间戳持续推进（真实 agent 未被踢、无风暴）。
2. **无效 token**：WS 握手应被 401 拒绝（升级前），POST 上报同样 401。
3. `/api/version`（构建 hash 是否注入）。

## 用法

```bash
MANGO_BASE=https://vps.mangoqwq.com \
MANGO_ADMIN_KEY=<admin api key> \
go run .
```

凭据只经环境变量读取，输出经 redaction 过滤（admin key 与 client token 均不回显）。测试对线上零扰动：被拒连接不留状态，目标节点选用"1 小时内有上报"的活节点。

> 仓库根有 `go.work`（仅含 agent/hub），本目录是独立 module：在本目录内构建请用 `GOWORK=off go build .`。
