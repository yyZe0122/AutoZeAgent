# ADR-018：本地 Gateway 与 CLI 边界

- 状态：Accepted
- 日期：2026-07-14
- 更新：2026-07-31（context 窗压 API，ADR-041）

## 决策

Gateway 是 `autozeagentd` 的本地控制面。`autozeagent` 的 **CLI 子命令**与 **TUI** 都只是它的客户端，经 `internal/gatewayclient` 访问 Gateway；二者并列，TUI **不** exec CLI。Gateway 不执行 Tool、不直接调用 Provider，也不能发行 Grant。

Linux/macOS 在 RuntimeDir 使用受文件权限保护的 Unix Domain Socket；Windows 使用仅监听 loopback 的随机端口和随机 Bearer Token。endpoint、Token 或 Socket 路径通过受限 discovery 文件发布。

当前 API 覆盖：

- health 与 Skill Catalog；
- 配置模型快照：`GET/PUT /v1/config/model`（无密钥；可选 `context_window`；PUT 热切换并回写配置顶层 `model`）；
- Task 提交、查询及 pause/resume/cancel（`execution_mode=agent|plan`；双轨 chat 见 ADR-038）；
- Session 列表与 transcript 查询；
- Plan 查询、Task usage 聚合（寿命累计 spend）；
- Task/Session **context** 窗压：`GET /v1/tasks/{id}/context`、`GET /v1/sessions/{id}/context`（last prompt / usable / pressure；见 ADR-041；**不是** usage 累计）；
- Run 查询与流式状态（含 model-stream）；
- Job **列表/读**（create 与 runner 已停用，见 backlog）；
- Event 查询和 SSE stream。

**已移除的交互面（HTTP 410 Gone，勿再文档化为可用能力）：**

- 人批 `POST /v1/approvals`、`GET /v1/approvals/prompt`；
- plan-step `POST /v1/runs`（StartRuns）。

Gateway 不持有通用 `*sql.DB` 业务能力。只读查询进入 `internal/corequery.Store`；Task 提交进入 `tasksubmission.Service`；agent/plan chat 进入 `chatsession`；pause/resume/cancel 进入 `taskcontrol.ControlTask`；Job 读进入进程内 `scheduler.Store`。

任何从 API 触发的模型工具调用最终仍必须进入 Tool Broker，并满足 Plan、Approval、Grant、路径、超时和审计约束（chat 路径上的 grant 由 `chatsession` 按 mode 签发，非交互人批）。旧 `/v1/modules` 路由已经删除。

## 客户端包边界

```text
cmd/autozeagent          flag / daemon ensure / 子命令与 tui.Run 入口
internal/tui             Bubble Tea UI；消费窄 `tui.Gateway`（由 gatewayclient 满足）
internal/gatewayclient   共享 HTTP/SSE 外观
internal/gateway         服务端路由 + 低层 Client 传输
```

TUI 与 CLI 不得 import `tools`、`providers`、`store/sqlite`、`agent`、`chatsession` 实现。生产 TUI 依赖 `gatewayclient` + `platform/paths` + `pkg/*` + charmbracelet，并允许 `internal/modelstream` 类型（经 `tui.Gateway` 注入）。

TUI 不消费人批/StartRuns API；metrics 接线与 backlog 见 `docs/optimization/current.md`。
