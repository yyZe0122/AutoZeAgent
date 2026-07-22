# ADR 022：任务应用服务与 Core 查询边界

## 状态

Accepted，2026-07-16。

## 背景

Gateway 和 Scheduler 都需要把外部请求转换为 Core Task。若每个适配器分别创建 Session、Task、调用 Planner 并处理幂等重试，编排规则会逐渐分叉。此前 Gateway 还直接持有 `*sql.DB` 并查询 `tasks`、`plans`、`plan_steps`、`approvals` 和 `runs`，使传输层了解 Core 表结构。

## 决策

### 命令应用层

`internal/tasksubmission.Service` 是“提交任务”用例的唯一编排入口。它负责：

1. 校验并规范化标题和目标；
2. 按请求生成或接受 Session、Task 和 Plan ID；
3. 在用户入口需要时确保 Session 存在且仍为 active；
4. 通过 Kernel Repository 创建 Task；
5. 对客户端指定的 Task ID 提供内容一致的幂等重试，并拒绝冲突复用；
6. 在 Planner 可用时调用现有 Planner Service；
7. 规划失败时保留已提交的 `planning` Task，并返回可识别的 `ErrPlanning`，供调用方决定重试和响应方式。

该服务不复制 Kernel 状态机，不生成 Plan 内容，也不写 SQL。Kernel Repository 继续拥有事务、版本控制和事件追加；Planner Service 继续拥有 `created -> planning -> waiting_approval` 过程。

本地 Gateway 的 `POST /v1/tasks` 与 Scheduler Core bridge 都调用该服务。Scheduler 继续使用幂等键派生稳定 Task/Plan ID，并保持 ACK 丢失和 Provider 暂时失败时的重投语义。

### 查询应用层

`internal/corequery.Store` 是 Core 本地读模型。它只执行查询和 `PRAGMA quick_check`，公开 Task、Plan、Plan Step、Approval、Run 与已存 Plan Document 的窄 DTO/方法。DTO 与单资源查询使用 `internal/coreidentity` 的强类型 ID，避免在应用边界把 Task、Plan、Step、Run 与 Approval 标识退化为可互换字符串。列表查询接受资源专用的 options 类型，统一复用有上限的 offset 分页与 `asc`/`desc` 排序，并只开放固定的 state 或 decision 过滤字段；详细契约见 ADR 027。

查询 DTO 的时间字段在 Store 读取时严格解析为 RFC3339Nano，并规范输出为 UTC；合法的历史 offset 值会被转换，非法值返回错误。Gateway 不负责选择展示时区，CLI/UI/client 在展示边缘转换，完整契约见 ADR 029。

Gateway 只依赖自己声明的 `QueryService` 接口，不再持有 `*sql.DB`，也不再了解 Core 表结构。查询层只返回数据库中存储的 revision、hash 和 document；Plan Document 的解析、Hash 复核与 Approval 决策编排由 `internal/approvalsubmission.Service` 执行，最终写入仍由 Approval Repository 拥有。详见 ADR 023。

### 组合根

`cmd/autozeagentd` 负责构造并连接：

```text
SQLite -> Kernel Repository -> TaskSubmission Service <- Planner Service (optional)
SQLite -> CoreQuery Store     -> Gateway
                         -> ApprovalSubmission Service
TaskSubmission Service        -> Gateway POST /v1/tasks
TaskSubmission Service        -> ScheduledTasks Runner
ApprovalSubmission Service    -> Gateway POST /v1/approvals
```

不引入容器式 DI、ORM、通用 Repository 或事件溯源框架。当前接口均围绕真实用例定义。

### 应用错误边界

TaskSubmission 与 ApprovalSubmission 使用 `internal/applicationerror` 对已知用例结果附加稳定错误码和可重试属性，同时通过错误链保留 Kernel、Approval 和 CoreQuery 原因。Gateway 只依赖该应用分类并拥有 HTTP 映射，不再枚举领域或持久化 sentinel；未知错误保持未分类并 fail closed。完整规则见 ADR 026。

## 自动边界

`internal/architecture/dependencies_test.go` 扫描生产 Go 文件并阻止：

- `pkg/*` 导入 `internal/*`；
- Core 静态导入可选模块实现；
- 可选模块实现导入 Core 私有包；
- 非对应模块命令导入可选实现；
- Gateway、TaskSubmission、ApprovalSubmission 和 ScheduledTasks 直接导入 `database/sql` 或 Core SQLite 实现。

## 后果

- 新传输入口可以复用 TaskSubmission 和 ApprovalSubmission，而不复制领域编排；
- Core 表结构变化只需调整 Query Store 和领域仓储；
- 规划失败后的 Task ID 会返回给客户端，客户端可用相同 ID 重试；
- 查询读模型与写模型有明确分工，但仍共享单个 Core SQLite 数据库和本地事务，不引入不必要的分布式复杂度；
- 应用层拥有用例结果语义，传输层只拥有协议映射，原始领域错误仍可诊断。
