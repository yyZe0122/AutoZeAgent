# ADR 032：审批交互语义（已取代）

## 状态

**Superseded** — 2026-07-30。整单 plan 人批交互（prompt DTO、a/r、allow_once|plan|reject 等）已删除。见 [ADR-038](038-session-chat-boundary.md)。

## 说明

本 ADR 曾定义人批 Prompt 展示与决策动作语义。当前产品面：

- **双轨 chat**（agent 可写 / plan 只读）经 `chatsession` + 系统审批记录；
- **单工具** permission 交互见 [ADR-043](043-tool-call-permission-interaction.md)（`/perm` once|similar|permanent|deny），**不是**本 ADR 的 plan-step 人批。

**不得**恢复 plan 级人批 UI/API 或本 ADR 的 Action 集合。
