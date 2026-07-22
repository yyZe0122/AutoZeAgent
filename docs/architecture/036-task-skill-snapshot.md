# ADR 036：Task Skill 选择与规划快照

## 状态

已接受，2026-07-17。

## 背景

ADR 034 已把简单 Skill 收敛为文件型目录，但仅有发现和读取能力还不能形成真实调用链路。Task 的初次规划可能因 Provider 暂时失败而重试，也可能在进程重启后由 Planning Recovery 恢复。如果重试时重新读取当前 `SKILL.md`，同一 Task 会因为文件变化得到不同规划输入；如果只在 HTTP 请求中携带 Skill ID，恢复流程又会丢失选择。

Skill 与 Memory 的语义也不同：Memory 是不可信、仅作数据参考的可降级上下文；Skill 是用户显式选择的受限指令内容，不能混入 Memory 的 `ContextProvider`，更不能成为授权来源。

## 决策

### 显式选择

本地 Gateway 的 `GET /v1/skills` 只返回已发现 Skill 的 `id`、`name`、`description` 和 `source`。`POST /v1/tasks` 接受可选 `skill_ids`，只允许调用方按稳定 ID 显式选择；Core 不根据标题、Objective、Memory 或模型输出自动选择 Skill。未知、空白或重复 ID fail closed。

运行时根目录按低到高优先级发现：

1. `<config_dir>/skills`，用户模式标记为 `user`，系统模式标记为 `system`；
2. 当前工作目录下 `.autozeagent/skills`，标记为 `project`，可覆盖同 ID 的配置目录 Skill。

不存在的根目录被忽略；非法文件作为诊断记录并从目录中排除。

### Task 持久事实

Skill 选择是 Task 初次规划输入的一部分。Task 创建时必须在同一 Core 数据库事务中持久化：

- 有序 `skill_ids`；
- 由目录按该顺序生成的完整受限上下文；
- 上下文 SHA-256；
- 创建时间。

快照不可更新或删除。相同 Task ID 的幂等重试必须匹配标题、Objective 和有序 `skill_ids`；Skill 文件在 Task 创建后变化、移动或删除，不改变该 Task 的规划输入。正文超出字节预算时整体拒绝，不静默截断。

数据库迁移为已有 Task 回填空 Skill 快照，因此恢复路径始终有明确事实，不用“缺少记录”表达空选择。

### Planner 注入

Planning Service 在调用 Provider 前从 Core 数据库读取 Task Skill 快照，并把非空内容加入独立的 system message。该 message 明确：Skill 只能指导规划，不能增加允许的 capability、创建 Approval、发行 Grant、改变 Policy 或触发工具执行。Planner 的结构化 Schema 与允许 capability 白名单继续作为本地强校验；Skill 内容不能扩大它们。

Provider 暂时失败时 Task 可停留在 `planning`，后续 Planning Recovery 仍按 Task ID 读取同一快照。Skill 内容不写入 append-only Agent Run Record，因为它属于规划输入，不是执行阶段对话恢复记录。

## 后果

- 文件型 Skill 获得 Gateway 发现、显式选择、确定性持久化和 Planner 注入的真实主链路；
- Task 重试和重启恢复不会受后续文件变化影响；
- Skill 不会与 Memory 数据上下文混淆，也不会创建第二授权事实源；
- 当前只接入 Planner，不自动接入 Agent Runner；如未来执行阶段也需要 Skill，必须复用同一 Task 快照并新增明确的恢复测试，不能重新读取文件。