# ADR-011：审批与 Capability Grant 绑定

- 状态：Accepted
- 日期：2026-07-13

## 背景

Planner、Scheduler、Gateway 和 Agent（含 session chat）都可能提出会产生副作用的操作。如果审批只依赖自然语言提示或 Skill 约定，计划内容、命令参数或资源范围变化后仍可能沿用旧授权，无法形成可靠安全边界。

## 决策

Core 使用固定风险等级 R0-R4。默认策略只允许 R0 纯本地读取直接执行，R1-R4 必须获得显式审批；未知风险、缺失规则、非法规则和不可用的策略组件一律 Fail Closed。用户可通过逐风险等级的显式规则配置 `allow`、`require_approval` 或 `deny`，但代码中的未知状态不会继承宽松默认值。

审批对象是规范化 Plan。Plan 使用无 Map 的稳定结构，统一步骤顺序、路径分隔符、域名大小写和集合顺序，再序列化为规范 JSON，并计算 SHA-256 Plan Hash。Hash 覆盖 Task、Plan revision、目标、每个 Step 的风险等级以及完整 Capability 范围。任何步骤、路径、命令参数、域名、时限或调用次数变化都会生成不同 Hash。

审批可以覆盖整个 Plan，也可以只覆盖指定 Step，并支持 `approved`、`rejected` 和 `changes_requested`。审批记录绑定 Plan ID、revision、Plan Hash 和可选 Step ID。授权查询只接受当前 revision 与 Hash，因此 Plan 修改后旧审批即使尚未写入 `invalidated_at` 也无法使用；显式失效字段仅用于状态展示和审计。

Capability Grant 只能从已批准 Plan 中完全相同的 Capability Scope 签发，不能在签发时扩大范围。Grant 绑定 Approval、Task、Plan、Plan Hash、Step、Capability、路径、命令及参数、网络域名、最大运行时间、最大调用次数和过期时间。一次性授权固定为一次调用。每次授权校验成功后，在 SQLite 事务中原子增加使用次数；过期、撤销、次数耗尽或任一绑定字段不匹配时拒绝执行。

**Command/Arguments 匹配（scheme A）：**

- Grant 的 `command` 为空 → 接受请求中的任意 command（仍受 path / capability / 时限等约束）；
- Grant 的 `arguments` 为空 → 接受请求中的任意 arguments；
- Grant 的 `command` 或 `arguments` 非空 → 必须与请求**精确**相等。

因此 session chat 可为 `process_exec` / `git_*` 签发「仅路径范围」的预授权（空 command/args），而不必为每次动态 argv 预写死命令。非空 command/args 的 Grant 仍为精确匹配。路径、域名、次数、过期规则不变。

批准、拒绝、修改请求、Grant 签发和 Grant 撤销均写入不可变 Core Event Store。审批与 Grant 元数据保存在 `core.db`，不依赖 Skill 正文或已删除的进程外模块。Session chat 的 workspace 预授权（ADR-038/046，含可选 `chat.tools` 与 ask 模式 `allow_once|similar|permanent`）同样写入真实 Grant；永久信任表在 ConfigDir，不替代 Grant 校验。

## 边界

当前路径检查执行词法范围限制。符号链接解析、防 TOCTOU 打开方式和进程工作目录隔离属于 Tool Broker/Linux 执行器阶段，必须在真正访问文件或启动进程前再次强制检查，不能只依赖审批层。