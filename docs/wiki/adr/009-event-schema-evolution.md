# ADR-009：事件 Schema 演进

- 状态：Accepted
- 日期：2026-07-13
- 更新：2026-08-12（登记 `permission.*` 事件类型）

## 决策

Event Store 永久保存事件写入时的原始 `schema_version` 和 Payload，不通过 UPDATE 将历史事件改写为新格式。

消费者在读取阶段通过 Core 的 Upcaster Registry 将旧版本逐级转换为当前版本。每个 Upcaster 只允许执行一个连续版本步骤，例如 v1 到 v2；如果转换链缺失，投递必须失败且不得推进消费位点。

每种事件类型显式声明当前 Schema 版本。高于当前版本的事件视为未来版本并拒绝投递，防止旧消费者错误解释新数据。

Upcaster 必须输出合法 JSON。转换只作用于投递副本，不改变事件 ID、全局 sequence、aggregate version、追踪信息或数据库中的原始事件。

## 当前额外事件类型（摘录）

| `event_type` | Producer | 说明 |
| --- | --- | --- |
| `permission.pending` | `toolpermission` | 交互 tool-call 挂起（ADR-043）；schema_version=1 |
| `permission.decided` | `toolpermission` | 用户 decide 之后；schema_version=1 |

上述类型无 upcast 链时按 v1 原样投递。TUI/Gateway SSE 消费者按 `event_type` 前缀分支即可。
