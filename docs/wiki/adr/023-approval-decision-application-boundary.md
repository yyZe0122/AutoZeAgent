# ADR 023：审批决策应用服务边界（已取代）

## 状态

**Superseded** — 2026-07-30。交互人批路径（`approvalsubmission`、`POST /v1/approvals` 写入）已删除。见 [ADR-038](038-session-chat-boundary.md)。

## 说明

本 ADR 曾定义 `internal/approvalsubmission` 作为人批决策用例入口。该路径与 OpenCode 式双轨 chat 不一致，生产中已移除；Gateway 不再提供人批 prompt/decide API。

**仍有效：** Approval **领域**（`internal/approval`：PlanDocument、Capability Grant、hash 绑定）与 Tool Broker 校验；会话 workspace 使用 `RecordSystemApproval` + `IssueGrant`，非人类整单审批。

**不得**恢复人批编排包或 plan-step 审批轨；若未来有真实产品需求，须新开 ADR。
