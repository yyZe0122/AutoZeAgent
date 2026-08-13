# ADR-040：MCP 经 Tool Broker 接入

- 状态：Accepted（stdio + 远程 SSE/HTTP）
- 日期：2026-07-31
- 更新：
  - 2026-07-31 实现：`providerconfig.MCP`、`internal/mcp`、`tools.RegisterMCP`（stdio）
  - 2026-08-12 **O2**：远程 Streamable HTTP + 旧版 HTTP+SSE；`type`/`url`/`headers`；import 映射
- 相关：ADR-012、ADR-035

## 背景

ADR-035 规定文件/Git/Process/HTTP 以及未来 MCP/LSP 必须收敛到 Tool Broker。需要可配置的 MCP 客户端，且不得恢复 Module Runtime 或独立 DB。

## 决策

### 配置

写在 ConfigDir 的 `agent.json` / `agent.local.json` 顶层 **`mcp`** 键（与 provider 配置同文件）：

```json
"mcp": {
  "servers": {
    "local-example": {
      "command": "npx",
      "args": ["-y", "some-mcp-server"],
      "env": {}
    },
    "remote-example": {
      "type": "remote",
      "url": "https://mcp.example.com/mcp",
      "headers": {
        "Authorization": "{env:MCP_TOKEN}"
      }
    }
  }
}
```

| 字段 | 说明 |
| --- | --- |
| `type` | 可选：`stdio` / `http` / `sse` / `remote`（及 import 别名 `local`→stdio）。省略时：有 `command`→stdio；仅有 `url`→`remote` |
| `command` / `args` / `env` | **stdio** 子进程 |
| `url` | **远程** 绝对 `http`/`https` URL |
| `headers` | 远程请求头；值支持 `{env:VAR}` / `{file:…}`，**永不**出现在 Gateway status |

远程模式语义：

| `type` | 行为 |
| --- | --- |
| `http` | 仅 Streamable HTTP（POST JSON 或 SSE body；`Mcp-Session-Id`） |
| `sse` | 仅旧版 HTTP+SSE（GET 取 `endpoint` 事件，POST 消息） |
| `remote` | 先试 Streamable；明确不支持（4xx 等）再回退 legacy SSE |

- `ymz config validate` 校验结构与 transport 字段；不打印 secrets。
- MCP **不**热更（ADR-048）：改配置后 `ymz restart`。

### 生命周期

- daemon（`ymzd`）在组合根启动/拨号 MCP、`initialize`、`tools/list`。
- 工具名注册为 **`mcp_<server>_<tool>`**，避免与 builtins 冲突。
- shutdown：stdio 杀子进程；远程可选 `DELETE` session / 关 SSE；单 server 失败不拖垮 daemon（fail-closed 该 server）。

### 执行路径

```text
Agent → Tool Broker → Policy + Grant + path/timeout/output 上限 → Audit
       → MCP tools/call (stdio | streamable HTTP | legacy SSE)
```

- MCP 工具与 builtins 同一 `Register` / `Execute` 契约。
- Skill 不得扩大 MCP 授权；无 “信任 server” 绕过 Grant。

### 可观测

- 真实连接/工具列表状态经 Gateway 聚合：`enabled/total/ok/error/tools`；可选 per-server `name`/`transport`/`connected`/`tool_count`/`error`。
- **禁止**在 status 中暴露 URL、headers、env、command 细节中的密钥。
- TUI MCP strip 无数据时隐藏，禁止假数据。

### 不做（本 ADR 范围）

- LSP（另案）。
- 独立 MCP 审批 UI 或绕过 Grant 的快捷路径。
- 通用 Module Runtime / Supervisor。
- MCP 配置热更（仍 restart）。

## 后果

- 实现：`internal/mcp`（stdio + remote client）、`tools.RegisterMCP`、`providerconfig.MCP*`、`opencodeimport` 远程映射。
- optimization O2 关闭后，O5–O6 仍仅在有外部 OC 客户端需求时推进。
