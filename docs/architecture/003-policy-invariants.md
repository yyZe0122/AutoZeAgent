# ADR-003：安全策略不变量

- 状态：Accepted
- 日期：2026-07-13

## 决策

Planner 默认只有只读能力。所有副作用必须关联 Task、Plan、Step 和未过期 Capability Grant。Grant 绑定规范化 Plan Hash、路径、命令、域名、次数和时限。

计划扩大范围后旧 Grant 立即失效。Scheduler、Gateway 和任何模块都不得绕过 Kernel 与 Tool Broker。默认策略为 Fail Closed。

Skill、Memory 与 Evolution 只能提出权限或变更请求，不能授予权限或修改 Policy。
