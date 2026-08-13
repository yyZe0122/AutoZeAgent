# ADR 036：Task Skill 选择与规划快照

## 状态

已接受，2026-07-17。  
更新：2026-08-13 — 取消 submit 关键字整段注入；模型经 Hermes 式 `skills_list` / `skill_view` 按需加载。

## 背景

ADR 034 已把简单 Skill 收敛为文件型目录，但仅有发现和读取能力还不能形成真实调用链路。Task 的初次规划可能因 Provider 暂时失败而重试，也可能在进程重启后由 Planning Recovery 恢复。如果重试时重新读取当前 `SKILL.md`，同一 Task 会因为文件变化得到不同规划输入；如果只在 HTTP 请求中携带 Skill ID，恢复流程又会丢失选择。

Skill 是受限指令内容，不能成为授权来源。用户显式预载（TUI `/skills`、`/<id>`、job `skill_ids`）必须在 Task 创建时冻结。模型按需加载走工具结果（transcript），不写进快照，也不发 grant。

## 决策

### 显式预载（快照）

本地 Gateway 的 `GET /v1/skills` 只返回已发现 Skill 的 `id`、`name`、`description`、`source` 及可选 `draft` / `last_used_at` / `archived_at`（无正文、无绝对路径）。`include_archived=true` 只列归档行。`POST /v1/tasks` 接受可选 `skill_ids`（显式选择）。**仅**这些 ID 进入不可变 Task 快照。提交路径**忽略空白、保序去重**；**未知 ID fail closed**。`Catalog.Select`（内部）仍拒绝空 ID 与重复。归档 skill 仍可被显式选中（并记 used / 解归档）。

运行时根目录按低到高优先级发现：

1. `<config_dir>/skills`，用户模式标记为 `user`，系统模式标记为 `system`；
2. 当前工作目录下 `.yunmengze/skills`，标记为 `project`，可覆盖同 ID 的配置目录 Skill。

不存在的根目录被忽略；非法文件作为诊断记录并从目录中排除。

### 模型按需加载（不进快照）

Agent 与 plan 都获得只读工具（Risk R0，不扩 grant）：

- `skills_list({query?})`：返回未归档 skill 的 id / name / description / source；`query` 用确定性关键字打分标 `suggested`（triggers > id/name > description；词边界；description token ≥ 4 rune）。
- `skill_view({name, file_path?})`：无 `file_path` 返回 `SKILL.md` 正文 + `linked_files`；有则读取该 skill 目录内相对路径（拒绝 `..` / symlink / `SKILL.md` 自身）。正文与附件经 `injectscan` fail-closed。成功 view 记 `skill_events(used)`。

加载结果进入 tool transcript，**不**写入 `task_skill_snapshots`。同一 run 内相同 `(name, file_path)` 可短 stub 去重。

关键字匹配**不再**在 submit 时把正文 merge 进快照。

### Task 持久事实

显式 Skill 选择是 Task 初次规划输入的一部分。Task 创建时必须在同一 Core 数据库事务中持久化：

- 有序 `skill_ids`；
- 由目录按该顺序生成的完整受限上下文；
- 上下文 SHA-256；
- 创建时间。

快照不可更新或删除。相同 Task ID 的幂等重试必须匹配标题、Objective 和有序 `skill_ids`；Skill 文件在 Task 创建后变化、移动或删除，不改变该 Task 的规划输入。正文超出字节预算时整体拒绝，不静默截断。

数据库迁移为已有 Task 回填空 Skill 快照，因此恢复路径始终有明确事实，不用“缺少记录”表达空选择。

### Chat 注入（当前主路径）

`chatsession` 在调用 Provider 前从 Core 数据库读取 Task Skill 快照，并把非空 `Instructions` 加入**独立的** system message（在模式 system prompt 之后、user 消息之前）。该 message 明确：Skill 只能指导本轮回复，不能增加允许的 capability、创建 Approval、发行 Grant、改变 Policy 或授权工具执行。本地 Policy / Grant / Tool Broker 仍为强校验；Skill 内容不能扩大它们。

注入只读快照，**从不**在运行时重新读取 `SKILL.md`。空选择不注入额外 system message；模型应 `skills_list` 再 `skill_view`。

主路径为 TUI `/skills` 显式预载 + 模型 list/view。草稿 apply 不影响已创建 Task 快照（ADR-050）。CLI `skills list` / `run --skill` **未实现**（可选尾巴，见 `docs/optimization/current.md`）。

## 后果

- 文件型 Skill 获得 Gateway 发现、显式预载快照、以及 Hermes 式渐进披露主链路；
- 显式预载的 Task 重试不会受后续文件变化影响；
- 模型加载不成为第二授权事实源，也不污染快照；
- 执行阶段对预载正文必须复用同一 Task 快照，不能重新读取文件。
