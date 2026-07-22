# ADR-026：应用错误分类与可重试语义

- 状态：Accepted
- 日期：2026-07-16

## 背景

Task Submission 与 Approval Submission 已经承担用例编排，但 Gateway 仍直接判断 Kernel、Approval、CoreQuery 等内部错误。这样会让 HTTP 适配器了解领域和持久化错误分类，也无法向调用方稳定表达某个失败是否适合稍后重试。

Provider 层已有供应商专用的错误类型，但它描述的是单次 Provider 调用，不应扩展为 Core 的通用应用错误框架。

## 决策

新增内部包 `internal/applicationerror`，只提供最小应用结果契约：

- 稳定错误码：`invalid_request`、`not_found`、`conflict`、`planning_pending`、`plan_changed`、`plan_document_unavailable`、`unavailable`；
- `Retryable` 布尔属性；
- 通过 `Unwrap` 保留原始错误，使 `errors.Is` / `errors.As`、日志和测试仍可识别领域原因；
- 未分类错误保持原样，并由外层按内部错误、不可重试处理。

应用服务负责把已知用例结果分类：

- Task 输入错误为 `invalid_request`；重复或状态冲突为 `conflict`；缺失聚合为 `not_found`；已持久化 Task 但规划尚未完成为 `planning_pending` 且可重试。
- Approval 输入错误、Plan 不存在、客户端 Plan 已变化、已存 canonical document 不可用及重复决策分别映射为对应应用错误码。

Gateway 不再枚举这些用例的 Kernel、Approval、CoreQuery 错误。它只把应用错误码映射到 HTTP 状态和固定消息，并在错误响应中始终返回 `retryable`。HTTP 状态、消息和 JSON 结构仍由传输层拥有；应用服务不知道 HTTP。

## 可重试语义

`retryable=true` 表示：在调用本身具备幂等键或幂等语义时，调用方可以稍后重试同一应用请求。它不表示立即重试一定成功，也不允许绕过 Policy、Approval、Capability Grant 或 Tool Broker。

当前明确可重试的应用结果是 `planning_pending` 和临时 `unavailable`。数据库健康检查的 503 响应也标记为可重试。冲突、Plan 变化、无效输入和损坏的已存 Plan Document 都不可重试，调用方必须先刷新状态或修正请求。

不在本 ADR 中引入统一退避时长、自动重试执行器或跨进程错误协议；出现真实需求后再扩展。Planning Recovery 继续按 ADR 024 使用有界轮询和幂等 Plan ID。

## 后果

- Gateway 与领域/持久化错误分类解耦，新增传输适配器可复用相同应用语义。
- API 调用方获得稳定错误码与显式重试提示，且不会看到任意内部错误文本。
- 原始 sentinel 仍可通过 `errors.Is` 诊断和断言。
- 未知基础设施错误默认 fail closed 为 `internal_error` 且不可重试，避免把未知副作用错误误标为安全重试。
