# ADR-013：Provider 边界（Planner 路径已删除）

- 状态：Accepted（Provider 规则）；交互 Planner **Superseded** → [ADR-038](038-session-chat-boundary.md)
- 日期：2026-07-13

## 背景

模型 Provider 是不可信输入源。即使兼容服务声称 Structured Outputs，仍可能返回格式错误 JSON、伪造风险、请求未授权 Capability，或直接返回 Tool Call。任何消费 Provider 的路径都不得因模型“看起来遵守提示词”就执行工具或签发 Capability Grant。

## 决策

### Provider 中立接口

`pkg/providerapi` 定义 Complete、Stream、Token/Cost/Usage、健康检查和带重试元数据的 Provider Error。实现位于 `internal/providers`，不依赖 Memory、Skills、Scheduler 或 Evolution。

OpenAI-compatible 实现（`internal/providers/openai` 等）：

- 普通请求 `/v1/chat/completions`；流式解析 SSE，向上层暴露 Delta、Tool Call 和完成事件；
- Structured Output 经 `response_format.type=json_schema`；
- `/v1/models` 轻量健康检查；
- 429、5xx、网络/超时可重试；认证与无效请求不自动回退；
- 响应体有界；错误中不含响应正文、Authorization 或完整请求体。

Provider Router 按数字优先级尝试；仅 Retryable 错误允许回退。Stream 一旦向调用者发出任何事件，禁止切换 Provider。

### Secret 引用

配置只保存 `APIKeyRef` / `SecretResolver`，请求时临时解析。Provider 结构体不长期保存明文密钥，错误消息不含密钥或服务端错误正文。

### 消费方（现行）

Provider **仅**服务 Agent Runner / `chatsession`（双轨 chat）。交互 Planner 包与人批路径已删除（ADR-038）；不得恢复。

历史上“Planner 只提案、不执行、不发 Grant”的规则仍作反回归：任何未来规划类组件若再出现，须新开 ADR，且仍不得绕过 Tool Broker。

## 结果

- Provider 的 Tool Call 与风险声明不被直接信任；
- 密钥与错误正文不泄漏到日志/错误字符串；
- 删除可选模块不影响 Provider 与 Agent 边界。
