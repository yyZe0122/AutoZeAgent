# ADR-004：数据库所有权

- 状态：Accepted
- 日期：2026-07-13
- 更新：2026-07-31（context_snapshots / session_compactions，ADR-041）

## 决策

`autozeagentd` 独占一个 SQLite 数据库 `core.db`，它是 Session、Task、Plan、Approval、Grant、Run、Agent 记录、Tool Call、Event、Audit、Skill 快照、Scheduler Job/Run/Lease，以及 **context 窗压快照 / session 摘要**（migration 016，ADR-041）的事实源。摘要只影响 provider 请求视图，不删除 transcript 或 `agent_run_records`。

Scheduler 通过 Core 已打开的 `*sql.DB` 工作，不创建 `scheduler.db`。旧的 `memory.db`、`evolution.db` 和其他模块数据库不再属于生产架构，也不会被 daemon 自动读取或迁移。

Core migration runner 统一拥有 schema 版本。Migration 013 删除旧 Module Registry、消费位点和 Evolution Activation 遗留表，同时保留历史 migration 序号，避免破坏已经存在的 `core.db` 升级路径。

大对象仍进入 Artifact Store；SQLite 保存元数据、路径和内容哈希。Provider 配置及 Secret 引用保存在配置文件或环境中，不把明文密钥写入 `core.db`。

## 后果

- daemon 是唯一数据库生命周期所有者；子组件不得关闭共享连接；
- 同一业务动作尽量在一个 SQLite 事务中提交；
- 不再需要跨模块数据库事务、消费位点或最终一致性补偿框架；
- 备份和恢复以 `core.db`、Artifact 目录和必要配置为边界。
