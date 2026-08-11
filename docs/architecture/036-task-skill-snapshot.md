# ADR 036：Task Skill 选择与规划快照

## 状态

已接受，2026-07-17。  

## 背景

ADR 034 已把简单 Skill 收敛为文件型目录，但仅有发现和读取能力还不能形成真实调用链路。Task 的初次规划可能因 Provider 暂时失败而重试，也可能在进程重启后由 Planning Recovery 恢复。如果重试时重新读取当前 `SKILL.md`，同一 Task 会因为文件变化得到不同规划输入；如果只在 HTTP 请求中携带 Skill ID，恢复流程又会丢失选择。

Skill 是用户显式选择的受限指令内容，不能成为授权来源。不存在独立 Memory 模块；任何附加上下文若存在，须视为不可信数据，且不得与 Skill 指令混淆为权限来源。

## 决策

### 显式选择

本地 Gateway 的 `GET /v1/skills` 只返回已发现 Skill 的 `id`、`name`、`description` 和 `source`。`POST /v1/tasks` 接受可选 `skill_ids`，只允许调用方按稳定 ID 显式选择；Core 不根据标题、Objective 或模型输出自动选择 Skill。未知、空白或重复 ID fail closed。

运行时根目录按低到高优先级发现：

1. `<config_dir>/skills`，用户模式标记为 `user`，系统模式标记为 `system`；
2. 当前工作目录下 `.yunmengze/skills`，标记为 `project`，可覆盖同 ID 的配置目录 Skill。

不存在的根目录被忽略；非法文件作为诊断记录并从目录中排除。

### Task 持久事实

Skill 选择是 Task 初次规划输入的一部分。Task 创建时必须在同一 Core 数据库事务中持久化：

- 有序 `skill_ids`；
- 由目录按该顺序生成的完整受限上下文；
- 上下文 SHA-256；
- 创建时间。

快照不可更新或删除。相同 Task ID 的幂等重试必须匹配标题、Objective 和有序 `skill_ids`；Skill 文件在 Task 创建后变化、移动或删除，不改变该 Task 的规划输入。正文超出字节预算时整体拒绝，不静默截断。

数据库迁移为已有 Task 回填空 Skill 快照，因此恢复路径始终有明确事实，不用“缺少记录”表达空选择。

### Chat 注入（当前主路径）

交互式 Planner 路径已由 ADR-038 会话双轨 chat 取代。`chatsession` 在调用 Provider 前从 Core 数据库读取 Task Skill 快照，并把非空 `Instructions` 加入**独立的** system message（在模式 system prompt 之后、user 消息之前）。该 message 明确：Skill 只能指导本轮回复，不能增加允许的 capability、创建 Approval、发行 Grant、改变 Policy 或授权工具执行。本地 Policy / Grant / Tool Broker 仍为强校验；Skill 内容不能扩大它们。

注入只读快照，**从不**在运行时重新读取 `SKILL.md`。Skill 内容不写入 append-only Agent Run Record（属于任务输入，不是执行阶段对话恢复记录）。空选择不注入额外 system message。

主路径为 TUI `/skills`：经 Gateway `GET /v1/skills` 列出元数据，并在 `POST /v1/tasks` 时附带显式 `skill_ids`；不按 objective 自动匹配。CLI `skills list` / `run --skill` **未实现**（可选尾巴，见 `docs/optimization/current.md`）。

## 后果

- 文件型 Skill 获得 Gateway 发现、显式选择、确定性持久化和 chatsession 注入的真实主链路；
- Task 重试不会受后续文件变化影响；
- Skill 不会与不可信附加上下文混淆，也不会创建第二授权事实源；
- 执行阶段必须复用同一 Task 快照，不能重新读取文件。