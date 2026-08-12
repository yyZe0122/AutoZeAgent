# ADR-018：本地 Gateway 与 CLI 边界

- 状态：Accepted
- 日期：2026-07-14
- 更新：2026-08-12（TUI 主路径 slash / permission SSE 事件类型）

## 决策

Gateway 是 `ymzd` 的本地控制面。`ymz` 的 **CLI 子命令**与 **TUI** 都只是它的客户端，经 `internal/gatewayclient` 访问 Gateway；二者并列，TUI **不** exec CLI。**TUI 为主产品路径**，CLI 为脚本/自动化次要入口。Gateway 不执行 Tool、不直接调用 Provider，也不能发行 Grant。

Linux/macOS 在 RuntimeDir 使用受文件权限保护的 Unix Domain Socket；Windows 使用仅监听 loopback 的随机端口和随机 Bearer Token。endpoint、Token 或 Socket 路径通过受限 discovery 文件发布。

当前 API 覆盖：

- health 与 Skill Catalog；
- 配置模型快照：`GET/PUT /v1/config/model`（无密钥；可选 `context_window`；PUT 热切换并回写配置顶层 `model`）；
- 可选 MCP 状态：`GET /v1/config/mcp`（无密钥；见 ADR-040）；
- Task 提交、查询及 pause/resume/cancel（`execution_mode=agent|plan`；双轨 chat 见 ADR-038）；
- Session 列表与 transcript 查询；`GET /v1/sessions/{id}`；`PATCH /v1/sessions/{id}` 写 `preferred_model`（O4 会话偏好；chat run 解析 prefer→main，不改全局）；
- `GET /v1/config/commands`：`chat.commands` 列表（id/description/template；无密钥）；
- Plan 查询、Task usage 聚合（寿命累计 spend）；
- Task/Session **context** 窗压：`GET /v1/tasks/{id}/context`、`GET /v1/sessions/{id}/context`（见 ADR-041）；
- 手动压缩：`POST /v1/sessions/{id}/compact`（见 ADR-041；TUI `/compact`）；
- Tool-call permission：`GET /v1/permissions`、`POST /v1/permissions/{id}/decide`（见 ADR-043/046；TUI `/perm`；≠ 整单 plan 审批）；
- Run 查询与流式状态（含 model-stream）；`GET /v1/runs`、`GET /v1/runs/{id}`；
- 可选：`GET /v1/approvals`（只读列表历史系统审批记录）；
- Job 创建 / 列表 / 读 / pause·resume·cancel（chat-native；见 ADR-042）；
- Event 查询和 SSE stream（含 `permission.pending` / `permission.decided`，见 ADR-043；TUI 用 SSE 触发 permission 刷新，decide 仍走 HTTP）。

**不得恢复：** 人批 prompt/decide、plan-step `POST /v1/runs`、`/v1/modules`。

Gateway 不持有通用 `*sql.DB` 业务能力。只读查询进入 `internal/corequery.Store`；Task 提交进入 `tasksubmission.Service`；agent/plan chat 进入 `chatsession`；pause/resume/cancel 进入 `taskcontrol`；Job 进入进程内 `scheduler.Store`（fire 由 `scheduledtasks` 桥接）。

任何从 API 触发的模型工具调用最终仍必须进入 Tool Broker。chat 路径上的 grant 由 `chatsession` 按 mode 签发。

## 客户端包边界

```text
cmd/ymz          flag / daemon ensure / 子命令与 tui.Run 入口
internal/tui             Bubble Tea UI；消费窄 `tui.Gateway`（由 gatewayclient 满足）；主 UX
                         表现：lipgloss 气泡卡 + contentBlock；完成态 glamour；bubblezone 点击 expand；
                         无新 list/viewport 引擎（见 docs/optimization/current.md）
internal/gatewayclient   共享 HTTP/SSE 外观 + transport（不 import gateway server）
internal/gateway         服务端 only：路由 / handlers / LocalRunner
```

TUI 与 CLI 不得 import `tools`、`providers`、`store/sqlite`、`agent`、`chatsession` 实现。主交互斜杠：`/cron`、`/compact`、`/perm`、`/expand`、`/journey`、`/skills`、`/<skill-id>`、`/<command>`（`chat.commands`）、`/model`（全局）/ `/model prefer`（会话偏好并在 run 时生效）。折叠快捷键：`e` / `E` / `c`（输入为空时）。CLI：`ymz config import-opencode`（离线写 ConfigDir，不经 Gateway）。可选尾巴见 `docs/optimization/current.md`。
