# ADR-018：本地 Gateway 与 CLI 边界

- 状态：Accepted
- 日期：2026-07-14
- 更新：2026-07-30

## 决策

Gateway 是 `autozeagentd` 的本地控制面。`autozeagent` 的 **CLI 子命令**与 **TUI** 都只是它的客户端，经 `internal/gatewayclient` 访问 Gateway；二者并列，TUI **不** exec CLI。Gateway 不执行 Tool、不直接调用 Provider，也不能发行 Grant。

Linux/macOS 在 RuntimeDir 使用受文件权限保护的 Unix Domain Socket；Windows 使用仅监听 loopback 的随机端口和随机 Bearer Token。endpoint、Token 或 Socket 路径通过受限 discovery 文件发布。

当前 API 覆盖：

- health 与 Skill Catalog；
- 配置模型快照：`GET/PUT /v1/config/model`（无密钥；PUT 热切换并回写配置顶层 `model`）；
- Task 提交、查询及 pause/resume/cancel；
- Plan 和 Approval 查询、审批提交；
- Run 启动、查询与流式状态；
- Job 创建、列表、状态及 pause/resume/cancel；
- Event 查询和 SSE stream。

Gateway 不持有通用 `*sql.DB` 业务能力。只读查询进入 `internal/corequery.Store`；Task、Approval 和 Run 写入分别进入 `tasksubmission.Service`、`approvalsubmission.Service` 与 `runexecution.Service`；Job 操作进入进程内 `scheduler.Store`。

任何从 API 触发的模型工具调用最终仍必须进入 Tool Broker，并满足 Plan、Approval、Grant、路径、超时和审计约束。旧 `/v1/modules` 路由已经删除。

## 客户端包边界

```text
cmd/autozeagent          flag / daemon ensure / 子命令与 tui.Run 入口
internal/tui             Bubble Tea UI；只应依赖 gatewayclient（目标）
internal/gatewayclient   共享 HTTP/SSE 外观与工作流辅助
internal/gateway         服务端路由 + 低层 Client 传输
```

TUI 与 CLI 不得 import `tools`、`providers`、`store/sqlite`、`agent`/`planner` 实现。后续 DTO 收敛与窄接口见 `docs/optimization/current.md` §5.1。
