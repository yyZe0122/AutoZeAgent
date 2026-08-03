# ADR-024：初始规划恢复应用边界

- 状态：**Superseded** — 2026-07-30：`planningrecovery` 与交互 Planner 已删除。见 ADR-038。
- 原状态：Accepted
- 日期：2026-07-16

## 背景

Task Submission 在创建 Task 后调用 Planning Service。Planning Service 会先把 Task 从 `created` 转为 `planning`，再请求 Provider 生成 Plan。Provider 暂时不可用、返回无效结构或进程在 Plan 持久化前退出时，Task 会安全地保留在 `planning`，且不会产生 Plan、Approval、Grant、Run 或 Tool Call。

Kernel 原有 `RecoverableTasks` 能查询非终态 Task，但没有执行恢复编排。仅依赖调用方重新提交请求会把恢复责任留给 Gateway、Scheduler 或用户，也无法保证 daemon 重启后自动收敛。

## 决策

新增 `internal/planningrecovery.Runner`，作为 Core-owned 后台应用边界。它只依赖两个窄接口：

- `Repository.InitialPlanningTasks`：返回状态为 `planning` 且从未持久化任何 Plan 的 Task；
- `Planner.PlanTask`：复用现有 Planning Service，由 Planner 生成提案并由 Kernel Repository 原子提交 Plan 与 Task 状态。

Runner 在 Core 启动时立即执行一次，之后默认每 30 秒执行一次；单次默认最多读取 20 个候选，按顺序处理并汇总错误。Provider 失败不会改变 Task 状态，后续周期可以继续重试。

恢复 Plan ID 由 Task ID 通过 SHA-256 确定性生成，Revision 固定为 1。该规则仅对“从未存在 Plan”的初始规划成立。Repository 使用 `NOT EXISTS` 明确排除已有 Plan 的 Task，因此恢复器不会猜测重规划 Revision，也不会错误覆盖或复用旧 Plan。

当恢复器与其他入口并发完成同一 Task 时，Kernel 的乐观并发控制只允许一个提交成功。恢复器把 `kernel.ErrVersionConflict` 视为并发完成，而不是持续故障；其他错误继续上报。

`cmd/autozeagentd` 仅在 Planner 已配置时把 Runner 注入 Core。未配置 Planner 时，零可选模块 Core 仍可启动，但不会尝试生成 Plan。

## 边界与不变量

- Runner 不直接导入 `database/sql` 或 Core SQLite 实现；
- Runner 不创建 Approval、Capability Grant、Run 或 Tool Call；
- Provider 输出仍必须经过 Planner 的本地 Schema、Capability、Risk、Budget 与 canonical hash 校验；
- Plan 与 Task `waiting_approval` 状态仍由 Kernel Repository 在一个事务中提交；
- 已存在 Plan 的重规划需要独立用例决定 Revision、旧 Plan supersede 和重新审批策略，不在本 ADR 范围内；
- 不引入通用队列、分布式锁、DI 容器或任务框架。

## 后果

- Gateway 创建但初始规划失败的 Task 可以在 daemon 重启后自动恢复，也可以在 Provider 恢复后按周期收敛；
- Scheduler 与 Gateway 不需要各自实现规划恢复逻辑；
- 初始规划恢复与显式重规划被明确分离，避免用 Revision 1 覆盖已有 Plan；
- 自动化架构测试保护 Planning Recovery 应用边界不直接访问 SQLite。