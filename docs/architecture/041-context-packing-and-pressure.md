# ADR-041：Provider 视图上下文装配与窗压监控

- 状态：Accepted
- 日期：2026-07-31

## 背景

多轮 chat 与 tool 循环会把 transcript 与 tool 输出堆进 provider 请求。P1 仅做了单条 tool/assistant **rune 裁剪**（`internal/agent/trim.go`），会话历史用硬 `Limit: 200`，且 `contextWindow` 只用于 TUI。长会话易触达模型窗上限，且无法区分「任务累计 token 花费」与「当前窗填充」。

业界 coding agent（OpenCode / Cline / Claude / LangChain 等）普遍采用分层策略：单条瘦身 → 旧 tool 清空 → 保 tail 的摘要；持久全文与请求视图分离。

## 决策

### 双轨数据

| 轨 | 用途 | 真值来源 |
| --- | --- | --- |
| **Pre-flight 估计** | 装配 / 裁切预算 | 本地启发式（runes/4 + 消息 framing）× 按 model 的 EMA 校准 |
| **Post-flight 账本** | 累计花费、校准、窗压 | Provider `usage`（`Usage.PromptTokens()` = uncached + cache_read + cache_write） |

禁止用 `TaskUsage.TotalTokens`（寿命累计）充当 context ring。

### 装配管线（`internal/contextpack` + agent/chatsession）

1. **L1** 单条 body trim（与历史 trim 策略一致；DB 全量保留，ADR-030）。
2. **L2** 旧 tool 结果改 placeholder（保护最近 ~40k 估计 tool tokens；回收不足阈值则跳过）。
3. **L3** pair-safe 丢弃最旧完整 turn（保留至少最后一轮 user turn 与 leading system）。
4. **摘要（默认开）**：压力 ≥ 可用窗 75% 且确定性裁切后仍不够时，对 head 做 LLM 摘要（失败则 extractive）；写入 `session_compactions`；下次加载 = summary + tail。
5. 可用窗：`contextWindow - maxOutput - reserve`（`contextWindow` 来自 model 配置，参与 packing，不仅 UI）。

### 持久化（migration 016）

- `context_snapshots`：按 task 的最近窗压（last_prompt、usable、estimate、source、ratio…）。
- `session_compactions`：会话级摘要；**不删除** transcript / `agent_run_records`。

### Gateway 只读 API

- `GET /v1/tasks/{id}/usage` — 累计 spend（既有）。
- `GET /v1/tasks/{id}/context` — 窗压快照。
- `GET /v1/sessions/{id}/context` — 该 session 最近一条快照。

响应中 `source` 标明 `provider_usage` / `local_estimate` / `none`，不宣称与账单 token 逐位一致。费用仍以后台为准。

### 配置（`chat` 块）

```json
"chat": {
  "roots": [],
  "allow_write": true,
  "compaction": { "enabled": true },
  "max_iterations": 8
}
```

| 字段 | 默认 | 说明 |
| --- | --- | --- |
| `compaction.enabled` | `true`（省略整块或字段时） | `false` 时仅 L1–L3，不调 LLM 摘要、不写入新 `session_compactions`；已有摘要仍可被加载 |
| `max_iterations` | `8` | 每 chat run 的 agent tool 循环上限，范围 1–64 |

`contextWindow` 仍在 **model** 配置上（非 `chat`）。

### 边界

- Gateway 不跑 tokenizer、不调 provider 做 count。
- 装配不绕过 Tool Broker / Grant；摘要调用不经 tools。
- 不接本地 token→$ 定价；不绑单一厂商 opaque server compact 作为主路径。

## 后果

- 长会话由 token 预算驱动，而非仅消息条数。
- TUI 可展示 window pressure；CLI/客户端可读 context API。
- 摘要默认开；可用 `chat.compaction.enabled=false` 关闭额外 provider 调用。
- 循环上限由 `chat.max_iterations` 配置，与窗压独立。

## 相关

- 实现：`internal/contextpack`、`internal/agent/runner.go`、`internal/chatsession`、`internal/corequery`、`providerconfig.ChatConfig`、migration 016。
- Backlog：`docs/optimization/current.md` P4.1（完成）。
- Provider 字段：`docs/provider-protocols.md`（`contextWindow`）。
