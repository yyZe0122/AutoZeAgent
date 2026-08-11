# ADR-042：Chat 原生 Job / cron

- 状态：Accepted
- 日期：2026-08-03
- 相关：ADR-017（lease/claim）、ADR-038（双轨 chat）、ADR-036（skill 快照）

## 背景

Job 表、claim/lease 与 list/pause/resume/cancel 已存在（ADR-017）。本 ADR 将到期 fire 定义为 chat-native 提交（经 `tasksubmission` → `chatsession`），默认 `execution_mode=agent`。

## 决策

### Job = 定时 chatsession 提交

Job payload 描述「何时、在哪个 session、以何种 mode 提交一条 chat objective」：

| 字段 | 说明 |
| --- | --- |
| `session_id` | 已存在 session（create 时校验） |
| `execution_mode` | `agent`（默认）或 `plan` |
| `task_title` / `task_objective` | 每次 fire 的 chat 标题与用户文本 |
| `skill_ids` | 可选；显式 ID，经 tasksubmission 快照（指令文本 only） |
| interval / misfire / retry | 沿用 ADR-017；**不做** cron 表达式 |

到点路径：

```text
scheduler claim/lease → scheduledtasks → tasksubmission.Submit → chatsession
```

Scheduler **只**决定何时提交；不创建 Approval、Grant、Agent Run 或 Tool Call。副作用仍经 Tool Broker。

### 默认 mode = agent

与交互 chat 默认一致。无人值守 agent 与交互 agent **同权**（`chat.allow_write`、`chat.tools.*` 天花板）。需要只读周期任务时显式 `plan`。

### ACK 语义

Core 接受 task 并启动 chat handoff 后 ACK `task_created` + 稳定 `core_task_id`。不再把 `waiting_approval` / `ErrPlanning` 当作成功路径。失败 ACK `failed`，由既有 retry/backoff 处理。

幂等：`core_task_key` = `idempotency_key/scheduled_at`；Core `task_id` 由 key 派生；`AllowExisting` + 字段匹配。

### 禁止

- plan-step Start worker、交互 Planner
- 恢复旧 `requires_plan` 门闩或强制 plan mode
- 独立 scheduler 进程/DB、Module Runtime

### 客户端

**TUI 为主**（`/cron` list + create）。CLI `job create` 为次要脚本入口，不挡产品完成。

## 后果

- migration 为 jobs 增加 `execution_mode`、`skill_ids`（012 不可变）
- daemon 重新挂载 chat-native `scheduledtasks` runner
- Gateway `POST /v1/jobs` 解封
- 文档与 ADR-017/038 脚注对齐本 ADR
