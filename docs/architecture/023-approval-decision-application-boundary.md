# ADR 023：审批决策应用服务边界

## 状态

Accepted，2026-07-16。

## 背景

Gateway 的 `POST /v1/approvals` 原本同时负责 HTTP 解码、读取已存 Plan Document、解析 canonical JSON、复核 revision/hash、生成 Approval ID，并直接调用 Approval Repository。该实现虽然没有绕过 Approval 的事务和不变量，但把审批用例编排留在传输层，使未来 CLI、桌面 UI 或其他本地适配器难以安全复用同一规则。

## 决策

新增 `internal/approvalsubmission.Service` 作为“记录审批决策”用例的唯一编排入口。它通过两个窄端口工作：

- `PlanDocumentLoader`：读取 revision、hash 与 canonical document；
- `Repository`：提交 `approval.DecisionInput`，由 Approval 领域仓储执行事务、不变量和事件写入。

应用服务负责：

1. 校验请求必须绑定 `plan_id`、revision 和 hash；
2. 从 Core 查询读模型加载已存 Plan Document；
3. 解码 canonical JSON，并重新计算 Hash；
4. 验证 document、数据库元数据和请求绑定三者一致；
5. 在未指定时生成 Approval ID；
6. 调用 Approval Repository 记录决策。

它公开稳定错误以供适配器映射：

- `ErrInvalidRequest`：请求缺少 Plan 身份信息；
- `ErrPlanChanged`：客户端提交的 revision/hash 已过期；
- `ErrPlanDocumentUnavailable`：数据库中的 document 无法解析或与其元数据不一致。

Gateway 现在只保留严格 JSON 解码、DTO 转换和 HTTP 状态码映射。`cmd/autozeagentd` 是组合根，负责连接 Core Query Store、Approval Repository、Approval Submission Service 和 Gateway。

## 安全边界

- 应用服务不能创建 Capability Grant、Run 或 Tool Call；
- Approval Repository 继续拥有 scope、decision、step、expiration、当前 Plan 和唯一性校验；
- canonical document 不信任客户端输入，客户端只能提交预期的 revision/hash；
- 请求校验成功与最终写入之间发生 Plan 变化时，Repository 仍以当前数据库状态 fail closed；
- `internal/architecture/dependencies_test.go` 禁止该应用服务直接导入 `database/sql` 或 Core SQLite 实现。

## 后果

- Gateway、未来 CLI 和桌面 UI 可以复用同一审批用例；
- HTTP Handler 不再负责 canonical Plan 编排；
- 查询读模型只读取数据，Approval Repository 只拥有领域写入，应用服务负责两者之间的用例协调；
- 不引入通用 Command Bus、ORM、DI 容器或泛型 Repository。
