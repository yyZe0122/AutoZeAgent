# ADR-038：Session 多轮 Chat 双轨（OpenCode build/plan）

- 状态：Accepted
- 日期：2026-07-30
- 更新：2026-08-03（`chat.tools` 可选预授权 git/process；grant scheme A）
- 更新：2026-07-31（TUI 审批残留清理、metrics budget/contextWindow；provider 视图装配见 ADR-041）
- 更新：2026-07-30（删除交互 Planner 审批轨）

## 背景

早期存在两条产品路径：`agent` 多轮 chat，`plan` 则走 Planner → `waiting_approval` → 人批 → plan-step worker。这与 OpenCode 的 **build / plan**（同一会话、权限不同）不一致，也造成「plan 像另一条管道」的心智负担。

## 决策

### 双轨 = 权限姿势，不是两条管道

| Tab / `execution_mode` | 对齐 | 路径 | 工具（Grant） |
| --- | --- | --- | --- |
| **agent** | OpenCode **build** | 同一 `chatsession` | workspace **读 + 写** |
| **plan** | OpenCode **plan** | 同一 `chatsession` | workspace **只读** |

共性：

- 同一 Session / 同一 agent 循环 / 同一 transcript；
- 提交 `POST /v1/tasks` + `execution_mode`；
- synthetic plan + 系统审批记录 + Capability Grant（满足 Broker，非人类业务审批）；
- 所有副作用仍经 Tool Broker（Policy → Grant → 路径/超时 → Audit）。

默认不预授权：`process_exec` / `http_get` / `git_*`。  
Agent 可通过 `chat.tools.git` / `chat.tools.process`（默认 false）opt-in 路径范围预授权（grant scheme A：空 command/args = 路径内任意调用）。**plan 永不**获得这些能力。`http_get` 仍不预授权。

### 已删除（交互产品面）

- `internal/planner`、`planningrecovery`、`agentautostart`、`approvalsubmission`；
- tasksubmission 异步 PlanTask；
- Gateway 人批 / plan-step `POST /v1/runs`（返回 410 Gone）；
- TUI a/r、`/approve`、审批 overlay、`ApprovalPrompt`/`StartRuns`/`DecideApproval` 消费面（TUI `Gateway` 已收窄）；
- CLI 等待规划/审批的 `run` 工作流（改为 chat 提交）；
- 旧 plan-step Job 语义（已由 ADR-042 chat-native Job 取代）。

### 配置

```json
"chat": {
  "roots": ["/absolute/workspace"],
  "allow_write": true,
  "tools": { "git": false, "process": false },
  "compaction": { "enabled": true },
  "max_iterations": 8
}
```

- `roots`：工作区绝对路径；空 = daemon cwd。
- `allow_write`：仅 **agent 写权限天花板**；`null`/省略 = true；`false` 时 agent 也只读。**plan 永远只读**，不受此字段开启写。
- `tools.git` / `tools.process`：仅 **agent** 预授权对应工具（默认 false）；与 `allow_write` 独立。
- `compaction.enabled`：会话 head 摘要（默认 true）；见 ADR-041。
- `max_iterations`：agent tool 循环上限（1–64，默认 8）。

### 任务控制

- `taskcontrol.ControlTask`（pause/cancel）经 `ChatInterrupter` 调用 `chatsession.Interrupt`，取消 in-flight `agent.Run`；
- pause：task=paused，chat run 保持可检查（不 fail）；cancel：task=cancelled，chat run → `cancelled` 并撤销 grants。

### 历史与上下文（ADR-041）

- 会话 transcript 在 `core.db` 中**全量保留**；
- 发给 provider 的消息视图经 `contextpack`：token 预算装配、旧 tool 清空、可选默认开的 head 摘要；
- `contextWindow`（model 配置）参与可用窗计算；窗压经 Gateway `…/context` 与 TUI 展示。

### 不变

- Gateway 不执行 tool、不调 provider、不发 grant；
- migration 与 `planning`/`waiting_approval` 状态字保留（历史行）；新交互任务不再进入这些状态；
- Skill 仍是指令文本，不扩大授权。

## 结果

- Tab 语义对齐 OpenCode：plan = 只读分析对话，agent = 可写构建对话；
- 架构更简单：单一 chat 编排器；
- 定时 Job 见 ADR-042（chat-native cron）。
