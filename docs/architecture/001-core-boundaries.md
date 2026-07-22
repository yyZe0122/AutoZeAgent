# ADR-001：核心边界

- 状态：Accepted
- 日期：2026-07-13
- 更新：2026-07-21

## 决策

AutoZeAgent 的生产核心是一个 `autozeagentd` 进程、一个 `autozeagent` CLI 和一个 `core.db`。Core 直接组合 Kernel、Planner、Agent Runner、Policy、Approval、Grant、Tool Broker、Scheduler、Event、Audit、Skill Catalog、Gateway 以及对应的窄应用服务。

Task、Plan、Run 和 Job 是持久化领域对象，不是操作系统进程。需要并发时优先使用受 `context.Context` 控制的 goroutine；只有实际 Tool 执行需要时才启动外部进程。

通用 Module Runtime、Supervisor、私有模块 RPC、Capability Registry 聚合和进程外 Memory/Scheduler/Evolution 已从生产路径删除。未来子代理应建模为带 `parent_run_id` 的逻辑子 Run，共享现有预算、审批、授权和审计边界，而不是恢复通用模块框架。

Gateway 和后台 Runner 不直接拼装领域状态：任务提交、审批、运行和恢复分别进入窄应用服务。所有模型请求的可执行操作必须经过 Tool Broker；Skill、Provider、Scheduler 和 Gateway 都不能绕过 Policy、Approval、Grant 与 Audit。

## Go 实现原则

- 组合根集中在 `cmd/autozeagentd`，使用具体类型和小接口；
- 状态机和不变量留在领域包与仓储中；
- 不引入 ORM、通用 Repository、容器式 DI 或事件总线框架；
- 新抽象必须由当前用户故事证明，而不是为假设中的扩展预留。
