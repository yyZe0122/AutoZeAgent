# ADR-013：Provider 与 Planner 强制边界

- 状态：Accepted
- 日期：2026-07-13

## 背景

AutoZeAgent 的模型 Provider 是不可信输入源。即使某个兼容服务声称支持 Structured Outputs，它仍可能返回格式错误的 JSON、伪造较低风险等级、请求未授权 Capability，或直接返回 Tool Call。Planner 不能因为模型“看起来遵守提示词”就执行工具，也不能签发 Capability Grant。

## 决策

### Provider 中立接口

`pkg/providerapi` 定义 Complete、Stream、Token/Cost/Usage、健康检查和带重试元数据的 Provider Error。Provider 实现位于 `internal/providers`，不会依赖 Memory、Skills、Scheduler 或 Evolution。

第一个实现位于 `internal/providers/openai`，使用 OpenAI-compatible Chat Completions 协议：

- 普通请求使用 `/v1/chat/completions`。
- 流式请求解析 SSE，并显式向上层暴露 Delta、Tool Call 和完成事件。
- Structured Output 通过 `response_format.type=json_schema` 发送。
- `/v1/models` 用于轻量健康检查。
- 429、5xx、网络不可用和超时标记为可重试；认证和无效请求不自动回退。
- 响应体有固定大小上限，错误中不包含响应正文、Authorization Header 或完整请求体。

Provider Router 按数字优先级从小到大尝试 Provider。只有显式标记为 Retryable 的错误才允许回退。Stream 一旦向调用者发出任何事件，就禁止切换 Provider，避免重复输出或拼接两个模型的结果。

### Secret 引用

OpenAI-compatible Provider 配置只保存 `APIKeyRef` 和 `SecretResolver`，每次请求时临时解析密钥。Provider 结构体不长期保存解析后的明文，也不把密钥或服务端错误正文写入错误消息。后续可以替换为环境变量、文件权限隔离的 Secret Store 或系统密钥环，而不修改 Provider 协议。

### Planner 只产出提案

`internal/planner` 只能调用 Provider 并返回 `approval.PlanDocument`。它不导入 Tool Broker 内部执行器，不创建 Capability Grant，也不执行模型返回的 Tool Call。

默认 Planner Capability Catalog 只有：

```text
fs.read
fs.list
fs.stat
git.status
git.diff
```

Git 只读工具被标记为 R0；`git.add` 为 R1，`git.commit` 为 R2。调用方若要规划写操作，必须通过可信代码显式构造更大的 Catalog；模型不能自行扩展 Catalog。

### 双重结构化校验

Planner 把带 `additionalProperties: false`、required、enum 和数值下限的固定 JSON Schema 发送给 Provider，同时在本地再次执行等价的强制校验：

1. 响应必须是单个合法 JSON 值，不接受 Markdown 代码块。
2. 每一层必须包含 Schema 的 required 属性。
3. 未知属性被拒绝。
4. 风险必须为 R0-R4。
5. Capability 必须存在于可信 Catalog。
6. Step 风险不得低于 Capability 的代码定义风险。
7. 副作用、回滚、Step Timeout、Capability Timeout 和总预算必须一致且有界。
8. 最终 Plan 必须能生成稳定 Canonical JSON 和 Plan Hash。

Provider 返回任何 Tool Call 时，Planner 直接返回 `ErrProviderToolCall`，不会把调用传给 Tool Broker。

### 审批和恢复

模型不能决定 TaskID、PlanID、Revision 或 StepID；这些标识由 Core 注入。Plan 包含预算、预期副作用、风险、回滚和超时，并全部进入 Canonical Plan Hash。

Phase 6 采用保守规则：Canonical Plan Hash 发生任何变化都必须重新审批。这样权限扩大一定重新审批，也不会错误复用旧 Grant。后续若要允许“纯缩权”免审批，必须先实现经过证明的 Scope 偏序比较。

Planning Service 在调用 Provider 前把 Task 转为 `planning`。Provider 不可用或输出无效时，不把 Task 转成 `failed`，也不创建 Plan、Grant 或 Tool Call；Task 保持 `planning`。Core 组合根在配置了 Planner 时启动 `internal/planningrecovery.Runner`，立即并按有界周期重试“尚无任何已持久化 Plan”的初始规划任务。已有 Plan 的显式重规划不属于该恢复器。只有本地校验通过并原子持久化 Plan 后，Task 才进入 `waiting_approval`。

## 结果

- 用户确认计划前不存在写工具执行路径。
- Provider 的 Tool Call、风险声明和 Structured Output 都不被直接信任。
- Provider 故障可以回退或恢复，不会让 Task 丢失。
- 删除任何可选模块都不影响 Provider/Planner/Core 状态机边界。
