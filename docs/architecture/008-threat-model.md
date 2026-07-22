# ADR-008：初始威胁模型

- 状态：Accepted
- 日期：2026-07-13

## 保护目标

保护主机文件、凭据、外部账户、系统服务、Agent Policy、审批记录和长期记忆，防止模型、外部内容或可选模块扩大权限。

## 初始威胁

- Prompt Injection 导致工具滥用或持久化恶意记忆；
- 计划批准后参数被替换；
- 路径穿越和符号链接逃逸；
- Scheduler 或模块绕过审批；
- 模块伪造身份、事件重放或数据库越界；
- Evolution 修改核心安全代码；
- 日志泄漏 Secret。

## 初始控制

使用结构化 Plan、Plan Hash、Capability Grant、类型化工具、路径规范化、事件幂等、模块进程隔离、Secret 脱敏和默认拒绝策略。更强 Linux sandbox 在 Tool Broker 稳定后加入。
