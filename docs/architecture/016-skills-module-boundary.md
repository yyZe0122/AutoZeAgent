# ADR 016：Skills 可选模块边界（已废弃）

## 状态

已废弃，2026-07-17。当前实现以 [ADR 034：文件型 Skills 与标准协议边界](034-file-based-skills-boundary.md) 为准。

## 说明

本 ADR 曾提出为 Skills 建立独立进程、独立数据库和私有 RPC。该设计没有进入当前生产主链路，相关命令、数据库迁移、协议、配置和构建入口已经删除。

当前 Skill 只是由 Core 发现并按需读取的 `SKILL.md` 文件内容，不是授权来源。它引导的工具调用仍必须经过 Tool Broker、Policy、Approval、Grant 和 Audit。

若未来出现需要事务化维护、独立数据所有权或强隔离执行的真实需求，必须基于具体用户故事新增 ADR；不得直接恢复本 ADR 的旧设计。
