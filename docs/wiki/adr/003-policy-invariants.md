# ADR-003：安全策略不变量

- 状态：Accepted
- 日期：2026-07-13
- 更新：2026-07-30
- 更新：2026-08-03（`chat.tools` agent-only 高风险预授权）

## 决策

所有副作用必须关联 Task、Plan、Step 和未过期 Capability Grant。Grant 绑定规范化 Plan Hash、路径、命令、域名、次数和时限（command/args 匹配见 ADR-011 scheme A）。交互 Planner 审批轨已删除（ADR-038）；chat 按 mode 签发 workspace grant。

计划扩大范围后旧 Grant 立即失效。Scheduler、Gateway 和任何客户端都不得绕过 Kernel 与 Tool Broker。定时 Job 只提交 chat task（ADR-042），不直接执行工具。默认策略为 Fail Closed。

Skill 只能提供指令文本，不能授予权限或修改 Policy。进程外 Memory/Evolution 模块已删除；不得以「记忆」或「进化」名义绕过 Policy/Grant。

Agent 模式 session chat 的 workspace 预授权（ADR-038/046）仍签发真实 Grant，并受 **session workspace**（客户端唤起目录）+ `chat.workspace`/`roots` 天花板 / `allow_write` / 可选 `chat.tools.{git,process}` 与 Broker 全链路约束；不是 Skill 或提示词授权。Plan 模式永远只读且不得获得 git/process 预授权。`allow_all` 仅本地单用户、需明确配置。
