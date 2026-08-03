# ADR-010：Kernel 状态一致性

- 状态：Accepted
- 日期：2026-07-13
- 更新：2026-07-30

## 背景

Kernel 必须能独立维护 Session 与 Task 生命周期，不依赖已删除的进程外 Memory/Evolution 或独立 Skills/Scheduler 进程。状态查询需要高效，历史记录需要不可篡改，并发写入不能静默覆盖彼此。

（Scheduler 现为 daemon **进程内**组件，与 Kernel 共享 `core.db`，但仍不得嵌入 Kernel 状态机包内。）

## 决策

Kernel 的 Session、Task、Plan、PlanStep 与 Run 表保存聚合的当前状态，作为重启恢复和日常查询使用的当前状态投影。Core Event Store 保存对应聚合的不可变历史。

每个聚合使用单调递增的 `version`。修改操作必须携带调用方已读取的期望版本，并通过 `UPDATE ... WHERE id = ? AND version = ?` 执行；受影响行数为零时返回版本冲突，拒绝旧写入覆盖新状态。

当前状态更新与事件追加必须在同一个 `core.db` SQLite 事务中完成。任一操作失败时整个事务回滚，从而保证一个已提交的状态版本恰好有一个对应事件，且不会出现只有状态或只有事件的部分提交。

Event Store 继续作为追加写历史，不承担聚合当前状态的同步重放。Core 重启后直接从当前状态表读取未完成 Task；事件用于审计与后续投影重建。

Kernel 生命周期代码只能依赖 Core 数据库、Event Store 和公共 Core 类型，不导入 Gateway、TUI、providers 或 tools 实现。

## 结果

- 并发修改通过乐观并发控制显式冲突，不会最后写入者静默覆盖。
- 状态与审计历史具有事务原子性。
- 重启恢复不依赖外部模块进程或第二数据库。
- 后续 Policy、Approval 与 Capability Grant 可以沿用聚合版本和事务事件模式。
