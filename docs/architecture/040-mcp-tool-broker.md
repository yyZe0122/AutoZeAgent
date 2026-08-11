# ADR-040：MCP 经 Tool Broker 接入

- 状态：Accepted（已实现首版 stdio）
- 日期：2026-07-31
- 更新：2026-07-31 实现：`providerconfig.MCP`、`internal/mcp`、`tools.RegisterMCP`
- 相关：ADR-012、ADR-035

## 背景

ADR-035 规定文件/Git/Process/HTTP 以及未来 MCP/LSP 必须收敛到 Tool Broker。需要可配置的 MCP 客户端，且不得恢复 Module Runtime 或独立 DB。

## 决策

### 配置

写在 ConfigDir 的 `agent.json` / `agent.local.json` 顶层 **`mcp`** 键（与 provider 配置同文件）：

```json
"mcp": {
  "servers": {
    "example": {
      "command": "npx",
      "args": ["-y", "some-mcp-server"],
      "env": {}
    }
  }
}
```

- 仅 **stdio** 传输（首版）。
- `ymz config validate` 校验结构与 command 非空；不打印 secrets。
- 环境变量引用沿用现有 `{env:VAR}` 约定（若 daemon 已支持）。

### 生命周期

- daemon（`ymzd`）在组合根启动 MCP 子进程、initialize、`tools/list`。
- 工具名注册为 **`mcp_<server>_<tool>`**，避免与 builtins 冲突。
- shutdown / 崩溃：干净终止子进程；单 server 失败不拖垮 daemon，该 server 工具不可用（fail-closed）。

### 执行路径

```text
Agent → Tool Broker → Policy + Grant + path/timeout/output 上限 → Audit
       → MCP tools/call (stdio)
```

- MCP 工具与 builtins 同一 `Register` / `Execute` 契约。
- 路径类参数若可识别则做 containment；进程类沿用 timeout 与输出上限。
- Skill 不得扩大 MCP 授权。

### 可观测

- 有真实连接/工具列表状态后，再经 Gateway 暴露；TUI MCP strip **禁止假数据**。
- 首版：日志 + 可选 status API；TUI 接数据后再显示。

### 不做（本 ADR 范围）

- LSP（另案）。
- 远程 SSE/HTTP MCP 多传输（可后续扩展，须仍经 Broker）。
- 独立 MCP 审批 UI 或绕过 Grant 的“信任 server”快捷路径。
- 通用 Module Runtime / Supervisor。

## 后果

- 实现落在 `internal/tools`（或窄 `internal/mcp` 仅客户端，注册仍进 Broker）。
- optimization：TUI MCP metrics 在 Broker 有状态前保持隐藏。
