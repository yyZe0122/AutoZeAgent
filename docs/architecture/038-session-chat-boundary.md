# ADR-038：Session 多轮 Chat 双轨（OpenCode build/plan）

- 状态：Accepted
- 日期：2026-07-30
- 更新：2026-08-13（AGENTS.md 注入；Hermes `skills_list` / `skill_view`）

## 背景

早期 `agent` 多轮 chat 与 `plan`（Planner → 人批 → plan-step）两条管道与 OpenCode 的 **build / plan**（同一会话、权限不同）不一致。本 ADR 统一为双轨 chat。

## 决策

### 双轨 = 权限姿势，不是两条管道

| Tab / `execution_mode` | 对齐 | 路径 | 工具（Grant） |
| --- | --- | --- | --- |
| **agent** | OpenCode **build** | 同一 `chatsession` | workspace **读 + 写** |
| **plan** | OpenCode **plan** | 同一 `chatsession` | workspace **只读** |

共性：

- 同一 Session / agent 循环 / transcript；
- 提交 `POST /v1/tasks` + `execution_mode`；
- synthetic plan + 系统审批记录 + Capability Grant（满足 Broker，非人类业务审批）；
- 副作用只经 Tool Broker（Policy → Grant → 路径/超时 → Audit）。

默认不预授权：`process_exec` / `http_get` / `git_*`。  
Agent 可通过 `chat.tools.git` / `chat.tools.process`（默认 false）opt-in。**plan 永不**获得这些能力。

### 不得恢复（反回归）

- `internal/planner`、`planningrecovery`、`agentautostart`、`approvalsubmission`；
- tasksubmission 异步 PlanTask / 人批 StartRuns；
- Gateway 人批与 plan-step `POST /v1/runs`（已删除，非 410 兼容面）；
- TUI a/r、`/approve`、整单审批 overlay；
- 旧 plan-step Job 语义（由 ADR-042 chat-native Job 取代）。

### 配置

```json
"chat": {
  "workspace": { "default": "client_cwd", "allow": [], "allow_all": false },
  "roots": [],
  "allow_write": true,
  "tools": { "git": false, "process": false },
  "compaction": { "enabled": true },
  "max_iterations": 16,
  "permission": { "mode": "preauth" }
}
```

- `workspace`（ADR-046）：会话根与天花板；
- `allow_write`：仅 agent 写权限天花板；**plan 永远只读**；
- `tools.git` / `tools.process`：仅 agent 预授权；
- `permission.mode`：tool-call 交互见 ADR-043 / 四档 ADR-046（≠ 整单 Planner）；
- 会话记忆：ADR-044；注入/skill 正文经 `injectscan`（H6）fail-closed；
- 会话模型偏好（O4）：`metadata.model` / `preferred_model`；不改全局 main；chat run 解析 prefer→main（见 ADR-045）；
- `chat.commands`（O3）：用户 slash 模板，仅 instruction；TUI 展开后作为 user 消息提交；不扩 grant。

### 任务控制

- `taskcontrol` 经 `ChatInterrupter` → `chatsession.Interrupt`；
- pause：task=paused；cancel：task=cancelled 并撤销 grants。

### 历史与上下文（ADR-041）

- transcript 在 `core.db` 全量保留；
- provider 视图经 `contextpack`；窗压经 Gateway context API 与 TUI。

### 不变

- Gateway 不执行 tool、不调 provider、不发 grant；
- 新任务状态机：`created → running → (paused) → completed|failed|cancelled`；历史 DB 行中的 `planning`/`waiting_approval`/`approved` 仅只读展示；
- Skill 与 `chat.commands` 仅指令文本，不扩大授权；TUI 可 `/skills`、`/skills apply|reject`、`/<skill-id>` 或 `/<command>` 显式使用（草稿见 ADR-050）。模型经 `skills_list` / `skill_view` 按需加载（ADR-036）。
- 可选用户规则：`<ConfigDir>/AGENTS.md`（EnsureConfig 缺则种子）始终注入；`<workspace>/.yunmengze/AGENTS.md` 存在则追加。经 `injectscan`；不扩 grant。

## 结果

- Tab 语义对齐 OpenCode；单一 chat 编排器；
- 定时 Job 见 ADR-042。
