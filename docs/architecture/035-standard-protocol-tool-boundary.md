# ADR-035：标准协议与 Tool Broker 边界

- 状态：Accepted
- 日期：2026-07-21

## 决策

Tool Broker 是 Agent 发起可执行操作的唯一入口。文件、Git、Process、HTTP，以及未来通过 MCP 或 LSP 适配的可执行能力，都必须收敛到同一条链路：

```text
Agent -> Tool Broker -> Policy + Approval/Grant + Audit -> Tool
```

Skill 只是上下文，不能创建 Approval 或 Grant。Provider 只能返回文本、结构化 Plan 或 Tool 请求。Gateway 与 Scheduler 只能触发应用用例，不能绕过 Broker 直接执行模型请求的操作。

未来外部工具优先采用 MCP，代码诊断优先采用 LSP；只实现当前实际需要的窄适配器。不得为了协议接入恢复通用 Module Runtime、私有 RPC、Supervisor 或独立数据库。

## 实现要求

工具定义、输入校验、Authorization、Execute 和结果持久化保持显式。执行前重新验证 Policy 与有效 Grant；执行结果、失败、超时和取消都写入持久记录与 Audit。路径类工具继续执行 symlink/junction containment，进程类工具继续执行超时、输出上限和环境变量白名单。
