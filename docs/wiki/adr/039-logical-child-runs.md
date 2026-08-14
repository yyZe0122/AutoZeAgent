# ADR-039：逻辑子 Run（parent_run_id）

- 状态：Accepted（已实现首版）
- 日期：2026-07-31
- 更新：2026-08-11 可观测：`GET /v1/runs/{id}/usage` 父 Run self+children 上卷（T5）

## 背景

复杂任务需要委托子代理。生产边界禁止新 OS 进程、Module Runtime 或独立模块。ADR-001 已规定：子代理 = 带 `parent_run_id` 的逻辑子 Run，共享预算、授权与审计。

## 决策

### 触发方式

与 OpenCode 等 coding agent 一致：模型经 **Tool Broker** 调用内置工具 **`task`** 创建并同步等待子 Run。Gateway/API 不作为主路径；不新增旁路 agent 入口。

### 执行关系（首版）

**同步阻塞**：父 Run 在 `task` 工具返回前等待子 Run 终态；首版不并行多个子 Run。父 cancel / ctx 取消必须传播到子。

### 数据模型

- `runs.parent_run_id`：可空外键（逻辑引用父 Run ID）；新 migration 追加，不改历史 migration 文件。
- 子 Run 属于同一 Task（与父相同 `task_id`），共享 Session/transcript 投影规则由查询层定义。
- 深度上限：`max_depth`（建议默认 2）；超限 fail-closed，工具返回错误文本，不创建 Run。

### 授权与预算

- Grant：子 Run **不得扩大**父的 workspace roots / 读写天花板；父为 plan（只读）则子只读。
- 预算：子消耗计入父 Task 用量聚合；子 Run 使用父剩余 token/cost 上限（或明确份额），超限 fail-closed。
- 可观测（读路径）：`corequery.RunUsage(runID)` = 本 Run + 一层 `parent_run_id` 子 Run 的 assistant usage 汇总；Gateway `GET /v1/runs/{id}/usage`；TUI Metrics 在存在子 Run 时展示 parent/children 旁注。不改预算策略。
- 高风险工具：子 **不得** 自行扩大；仅当父 agent 已因 `chat.tools`（或其它合法 Grant）具备 `git_*` / `process_exec` / `process_shell` 时，子才可经 `allowed_tools ⊆ 父` 继承。`http_get` 仍不由 chat 预授权。首版：子 allowed_tools ⊆ 父 allowed_tools。

### 工具 `task` 语义（首版）

输入（示意）：prompt（必填）、可选 mode 覆盖（不得放宽父只读）、可选 tools 子集。  
输出：子 Run 终态摘要（content / error / run_id），写入父的 tool result（经 Broker 正常路径）。

实现必须：`Agent → Tool Broker → Policy/Grant/Audit →` 编排创建 child Run → 同步 `agent.Run` / chatsession 等价路径。禁止 Broker 外直接调 Runner。

### 恢复

子 Run 各自 `agent_run_records`（ADR-030）。父在 `task` 未完成时崩溃：按 tool_calls + records 恢复规则处理，不得自动重放已成功的子副作用。

### 不做

- 异步并行子 Run、跨 Task 子代理、新进程/模块框架。
- 恢复交互 Planner 审批轨。
- Job/cron 直接生成子 Run（定时 Job 只提交顶层 chat task，见 ADR-042；子 Run 仍仅由模型 `task` 工具触发）。

## 后果

- 首版已落地：migration 015、`task` 工具、`runmeta`、预算/深度 fail-closed。
- TUI timeline 在子 Run 标题展示 `←parent` 短链；完整父子树 UI 非目标。
- Run-scoped usage 上卷已落地（可观测 only）；Task 级 `…/usage` 仍含全部 runs。
