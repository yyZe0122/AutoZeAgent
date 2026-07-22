# 032 Approval 交互语义与授权边界

## 状态

已接受，2026-07-17。

## 背景

AutoZeAgent 已经将 Approval 决定、Capability Grant 和 Tool Broker 分离：Approval 记录操作者对当前 Plan Revision/Hash 的决定，Grant 描述可消费的具体能力范围，Tool Broker 是唯一执行入口。此前 Gateway 直接接收 `scope` 和 `decision`，交互层也缺少统一的 Approval Prompt DTO，客户端既无法完整展示工具风险和资源范围，也可能尝试提交超出 Canonical Plan 的授权参数。

当前生产链路不会在 Approval 后自动发行 Grant。Grant 仍必须由明确的应用流程通过 `approval.Repository.IssueGrant` 创建，并且其 `CapabilityScope` 必须与 Canonical Plan 中的能力范围完全一致。

## 决策

### Prompt 只来自 Canonical Plan

`approvalsubmission.Service.Prompt` 从已持久化并通过 Revision、Hash 和 Canonical JSON 校验的 Plan 生成展示 DTO。DTO 包含：

- Plan ID、Task ID、Revision、Plan Hash、目标和预算；
- Step 标识、顺序、标题、风险等级、预期副作用、回滚说明和超时；
- 每项 Capability 的工具名、路径、命令、参数、网络域名、最长持续时间、最大调用次数和一次性标记；
- 当前 Plan 或选中 Step 可用的交互动作及其作用域。

客户端不得提交自定义 Approval Scope、Decision、Grant Scope 或 MaxCalls。Gateway 对未知 JSON 字段 fail closed。

### 五类动作

交互层只接受以下动作，并安全映射到现有 Approval 模型：

| 动作 | Approval Scope | Decision | 约束 |
|---|---|---|---|
| `allow_once` | Step | Approved | 必须选择 Step，且该 Step 恰好包含一个 `OneTime=true`、`MaxCalls=1` 的 Capability |
| `allow_limited` | Step | Approved | 必须选择含 Capability 的 Step；次数沿用 Canonical Plan 中每项 Capability 的 `MaxCalls` |
| `allow_plan` | Plan | Approved | 只批准当前 Revision/Plan Hash，不批准未来修改后的 Plan |
| `reject` | Step 或 Plan | Rejected | 有 Step ID 时拒绝该 Step，否则拒绝当前 Plan |
| `request_changes` | Step 或 Plan | Changes Requested | 有 Step ID 时请求修改该 Step，否则请求修改当前 Plan |

未知动作、缺失 Step、Step 不属于当前 Plan、`allow_once` 不满足严格条件，以及 Revision/Hash 变化都返回分类后的应用错误，不持久化决定。

### Approval 不等于 Grant

动作处理只调用现有 Approval 持久化服务，不发行 Capability Grant。后续 Grant 发行必须继续满足：

1. Approval 仍然有效并绑定同一 Plan Revision/Hash；
2. Grant Scope 与 Canonical Plan 中的 CapabilityScope 完全一致；
3. MaxCalls、OneTime、路径、命令、参数、域名和持续时间不能由客户端扩大；
4. 工具只能通过 Tool Broker 校验并消费 Grant 后执行。

因此 Approval Prompt 是 Canonical Plan 的只读投影，用户动作是受限的意图映射，不构成第二套授权事实源。

## 后果

- UI、CLI 或其他客户端可以使用同一 DTO 完整展示风险和授权范围；
- 客户端不能通过直接提交 Scope、Decision 或次数来扩大 Plan；
- “一次允许”和“有限次数允许”具有可测试、fail-closed 的明确前置条件；
- “当前 Plan 允许”继续绑定 Revision 和 Hash，Plan 变化后必须重新审批；
- Approval 与 Grant 的职责保持分离，Tool Broker 仍是唯一执行入口；
- 当前阶段不新增自动 Grant 发行器、通用 Grant API 或新的权限事实源。
