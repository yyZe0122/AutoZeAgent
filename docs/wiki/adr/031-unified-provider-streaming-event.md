# ADR-031：统一 Provider Streaming Event

- 状态：Accepted
- 日期：2026-07-17
- 更新：2026-08-13（`GET /v1/model-stream` 已落地）

## 背景

OpenAI-compatible 流式响应会把文本和 Tool Call 拆成厂商协议片段。若 Agent / chatsession 或 Gateway 分别理解这些片段，就会形成多套聚合、完成判断和错误处理逻辑，并可能把不完整 Tool Call 当成可执行请求。

YunmengZe 同时已有 Core 领域事件流 `eventapi.Envelope`。领域事件是已持久化事实，Provider Streaming 是模型响应的临时传输，两者不能因为都使用 SSE 就合并成同一种业务事件。

## 决策

### 唯一模型输出事件

`pkg/providerapi.StreamEvent` 是 YunmengZe 唯一的模型输出流式事件：

- `delta`：非空文本增量；
- `tool_call`：已经由 Provider 边界聚合完成的 Tool Call；
- `complete`：唯一终止事件，携带 Finish Reason 和 Usage。

调用方不得解析 OpenAI SSE chunk，也不得定义另一套文本、Tool Call 或完成 DTO。`providerapi.StreamAccumulator` 和 `providerapi.CollectStream` 是把该事件序列归并为 `CompletionResponse` 的唯一公共实现；Agent / chatsession 使用它（交互 Planner 已删除，见 ADR-038）。

### Provider 边界职责

OpenAI Provider 在发出 `tool_call` 前按厂商 `index` 聚合 ID、Name 和 Arguments 片段，并验证最终 ID、Name 和 Arguments JSON。只有收到合法 Finish Reason 和 `[DONE]` 后才发出 `complete`。以下情况返回 Protocol Error，不伪装成功：

- `[DONE]` 前没有 Finish Reason；
- Finish Reason 后仍出现 Choice；
- Tool Call index 非法、ID/Name 冲突或最终 Tool Call 不完整；
- 响应体提前结束、超限或 JSON 无效。

Stream Handler 返回的错误原样终止调用；Router 一旦已经发出事件，禁止切换 Provider。

### 持久化与恢复

流式增量不是执行事实，不写入 `agent_run_records`，也不修改已有 Assistant Message。Agent / chatsession 只有在 `CollectStream` 收到合法 `complete` 后才得到完整响应，并继续执行本地校验或追加最终记录。半途失败不会持久化成成功 Assistant Message，也不会触发 Tool Broker。

### Gateway 边界

`GET /v1/events/stream` 继续传输已持久化的 `eventapi.Envelope`，不把 Core Domain Event 包装成 Provider Streaming Event。

`GET /v1/model-stream`（可选 `session_id` / `run_id`）已落地：SSE `event: model`，`data` 为 `modelstream.Envelope`（`seq` + session/task/run id + 内层 `providerapi.StreamEvent`）。内层不得另造 Delta/Tool Call/Complete 模型；外层 Envelope 只加路由元数据，不改 `StreamEvent` 语义。

## 后果

- Provider 厂商片段只在 Provider 实现内出现；
- Agent / chatsession 共享相同的聚合和终止语义；
- 不完整 Tool Call 不会越过 Provider 边界；
- Gateway 领域事件流保持兼容；模型输出流的内层仍是 `StreamEvent`（外层仅 Envelope 元数据）；
- 流式增量仍是临时展示数据，恢复以 append-only Agent Run Records 的完整消息为准。
