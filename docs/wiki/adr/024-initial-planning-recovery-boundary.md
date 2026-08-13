# ADR 024：初始规划恢复边界（已取代）

## 状态

**Superseded** — 2026-07-30。交互 Planner 与 `planningrecovery` 已删除。见 [ADR-038](038-session-chat-boundary.md)。

## 说明

本 ADR 曾定义对 `planning` 且无 plan 的 plan-mode Task 的重启恢复。当前双轨 chat 路径为 `created → running`，不经 `planning` / 人批；相关恢复器与 `InitialPlanningTasks` 查询已移除。

**不得**恢复 LLM Planner 恢复环；进程重启后的 Run/Agent 恢复见 [ADR-030](030-agent-run-record-recovery.md)。
