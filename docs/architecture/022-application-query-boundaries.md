# ADR 022：任务应用服务与 Core 查询边界

## 状态

Accepted，2026-07-16。  
**部分 supersede（2026-07-30/31）：** 交互 Planner → `waiting_approval` → 人批路径已删除（ADR-038）。`tasksubmission` + `corequery` 边界仍有效；chat 写入经 `chatsession`，控制经 `taskcontrol`。文中 `approvalsubmission` / Planner 状态机描述为历史。

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
6. 按 `execution_mode` 将运行委托给 `chatsession`（agent 写 / plan 只读）；不再调用交互 Planner。

该服务不复制 Kernel 状态机，也不写 SQL。Kernel Repository 继续拥有事务、版本控制和事件追加；PlanDocument 与 workspace grant 由 `chatsession` 按 mode 构造（见 ADR-038）。

本地 Gateway 的 `POST /v1/tasks` 调用该服务。定时 Job 经 `scheduledtasks` → 本服务提交 chat task（ADR-042）。

### 查询应用层

`internal/corequery.Store` 是 Core 本地读模型。它只执行查询和 `PRAGMA quick_check`，公开 Task、Plan、Plan Step、Approval、Run、TaskUsage 与已存 Plan Document 的窄 DTO/方法。DTO 与单资源查询使用 `internal/coreidentity` 的强类型 ID。列表查询接受资源专用的 options 类型，统一复用有上限的 offset 分页与 `asc`/`desc` 排序；详细契约见 ADR 027。

查询 DTO 的时间字段在 Store 读取时严格解析为 RFC3339Nano，并规范输出为 UTC。Gateway 不负责选择展示时区；CLI/UI 在展示边缘转换，见 ADR 029。

Gateway 只依赖自己声明的 `QueryService` 接口，不再持有 `*sql.DB`。查询层返回 revision、hash 和 document；交互人批编排（`approvalsubmission`）已删除，见 ADR 023 状态与 ADR-038。

### 组合根

`cmd/autozeagentd` 负责构造并连接：

```text
SQLite -> Kernel Repository -> TaskSubmission Service -> ChatSession
SQLite -> CoreQuery Store     -> Gateway
TaskSubmission Service        -> Gateway POST /v1/tasks
TaskControl Service           -> Gateway task pause/resume/cancel
```

不引入容器式 DI、ORM、通用 Repository 或事件溯源框架。当前接口均围绕真实用例定义。

### 应用错误边界

TaskSubmission 等应用服务使用 `internal/applicationerror` 对已知用例结果附加稳定错误码和可重试属性。Gateway 只依赖该应用分类并拥有 HTTP 映射；未知错误保持未分类并 fail closed。完整规则见 ADR 026。

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
