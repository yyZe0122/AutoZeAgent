# ADR-043：Tool-call 交互 Permission（Crush 式）

- 状态：Accepted（已实现）
- 日期：2026-08-06

## 背景

Session chat（ADR-038）对 workspace 工具做 **preauth grant**；高风险工具（`process_exec` / `git_*`）仅经 `chat.tools` opt-in。未覆盖 grant 且 Policy 为 `require_approval` 时，Broker 默认 **立即 deny**（fail closed）。

Crush 等产品在 **单次 tool call** 边界提供 allow/deny 队列，无需整单 plan 审批。产品需要：在 **agent** 模式下，对未预授权的高风险 call 可挂起 → 人在 TUI 决策 → 发 **scoped grant** → **同一 agent 循环**继续。

**不得**恢复交互 Planner / `waiting_approval` 整单轨 / plan-step `POST /v1/runs`（ADR-038 已删）。

## 决策

### 与 Planner 的区分

| | 交互 Planner（禁止） | Tool-call permission（本 ADR） |
| --- | --- | --- |
| 对象 | 整份 plan / plan-step Start | **单次** tool call（Broker 边界） |
| 任务状态 | `waiting_approval` 等 | task/run 保持 running；call 挂起 |
| 授权 | 批 plan 后批量 grant | decide 后 **scoped** grant 再 `AuthorizeAndConsume` |
| Gateway | 已删除的人批 / plan-step Start | `GET/POST /v1/permissions…` |

### 配置：`chat.permission.mode`

```json
"chat": {
  "permission": { "mode": "preauth" }
}
```

| 值 | 行为 |
| --- | --- |
| **`preauth`**（默认） | 无 matching grant + `require_approval` → **立即 deny** |
| **`ask`** | 同上条件且为 **交互** chat → **pending**；TUI/Gateway decide 后继续或 deny |
| **`auto`** | 预留；当前解析为 `preauth` |

- **plan 模式**：永不因 permission 扩大写/exec。
- **Job/cron**：`scheduled_*` task id 或 actor `scheduler` → **不 wait**，立即 deny（fail closed）。

### 运行时流

```text
agent loop → Broker.Execute
  Policy deny → denied
  require_approval && 无 matching grant:
    mode=preauth 或 非交互 → denied
    mode=ask && interactive → pending
      → tool_permission_requests + Waiter
      → TUI /perm 或 API decide
      → IssueGrant（plan 内 once/session scope）→ 同 call 再 Execute（禁嵌套 wait）
  deny → tool 结果 denied 回模型
```

### 持久化与 API（已实现）

- migration **018** `tool_permission_requests`
- `internal/toolpermission`：Store、Waiter、Service.Decide、Gate（Broker）
- Gateway：
  - `GET /v1/permissions?session_id=&limit=`
  - `POST /v1/permissions/{id}/decide` body：`{ "decision": "allow_once"|"allow_similar"|"allow_permanent"|"deny", "actor": "…", "confirm": false }`
- TUI：`/perm` 列表；`/perm once|similar|permanent|deny <id-prefix>`（`allow_session` 仍接受为 similar 别名）
- ask 模式：chat plan **嵌入** process/git 的 once + session CapabilityScope，**不**预发这些 grant（`issueChatGrants` 跳过）

### Grant 范围

- **allow_once**：单次 call；从 plan once scope 签发。
- **allow_similar**（原 allow_session）：本会话同 capability，路径尽量收窄到请求路径所属 plan 根；TTL ~24h。
- **allow_permanent**：需 `confirm:true` 二次确认；写 ConfigDir `permissions-trust.json`；长 TTL grant。
- **deny**：不发 grant。
- scheme A 路径/command 规则不变。见 ADR-046。

### 边界

- 副作用仍只经 Tool Broker；decide 只发 grant。
- Gateway 不调 provider、不跑 executor。
- 无专用 permission SSE（可轮询 list）。

## 后果

- 默认 `preauth` 与历史行为一致。
- `ask` 提供 Crush 级交互，且不恢复 Planner。
- Job 路径 fail-closed。

## 相关

- ADR-011 grants；ADR-012 Broker；ADR-038 session chat；ADR-018 Gateway；**ADR-046** session workspace + permission tiers（`allow_similar` / `allow_permanent`）。
