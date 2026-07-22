# ADR 034：文件型 Skills 与标准协议边界

## 状态

已接受，2026-07-17。本决策取代 ADR 016 对简单文件型 Skill 的进程外模块与独立 `skills.db` 决策。

## 背景

旧 `cmd/autozeagent-skills`、`internal/skills` 和 `skills.db` 曾实现 Candidate、评估、审批、隔离、版本、指标和 FTS 等完整生命周期，但 Core Agent 与 Planner 从未使用这条链路。当前真实需求只是发现本地 Skill、展示元数据、按选择读取正文并注入受限上下文。为这类文件内容启动独立进程和数据库，会提前引入数据所有权、RPC、迁移和运行维护成本。

Crush 的实现证明了更小的可行边界：以目录中的 `SKILL.md` 为事实源，发现阶段只建立名称、描述、来源和路径目录，实际正文按 ID 读取；用户、项目和内置来源通过明确优先级解析，不依赖 SQLite。

## 决策

### 文件型 Skills

当前简单 Skill 以 `<root>/<skill-id>/SKILL.md` 为唯一事实源，不使用独立 `skills.db`，也不启动独立 Skills 进程。Core 内的文件型目录能力只负责：

1. **Discover**：从已配置根目录发现 `SKILL.md`，解析 `name` 和 `description` 元数据；
2. **Catalog / Select**：返回稳定目录，并按显式 ID 选择有效 Skill；
3. **Read**：只允许读取发现后仍位于对应根目录内的 Skill，正文按需加载；
4. **Context**：以确定顺序生成有字节上限的上下文，超出预算时整体失败，不静默截断。

根目录按低到高优先级提供，后面的根目录覆盖同 ID 的前项，因此项目来源可覆盖用户来源。发现结果必须稳定排序。目录或文件的符号链接越界、非法 frontmatter、未知 ID 和发现后的路径替换都 fail closed。

### 权限边界

Skill 是指令内容，不是授权来源。frontmatter 和正文不能创建 Approval、发行 Grant、扩大 Grant Scope 或绕过 Policy。Skill 引导的任何工具调用仍必须进入现有 Agent -> Tool Broker -> Plan/Approval/Grant/Audit 主链路，并绑定 Canonical Plan Hash。

### MCP、LSP 与可执行工具

- 外部工具能力优先通过 MCP 接入，不新增等价私有 RPC；
- 代码诊断、跳转、引用和补全优先通过 LSP 接入，不自定义代码智能协议；
- 通用 Module Runtime 已从生产架构移除；只有出现已验证的强隔离需求时，才考虑新增窄边界适配器，而不是恢复通用模块框架；
- MCP、LSP 或其他外部适配器暴露的可执行工具最终都必须由 Tool Broker 与 Approval 边界强制执行。

## 迁移

真实调用入口迁移和回归已完成。2026-07-17 已删除 `cmd/autozeagent-skills`、`internal/skills`、`pkg/skillsapi`、`migrations/skills`、配置示例及构建/打包入口；架构测试阻止这些 legacy artifact 恢复。已有用户环境中的 `skills.db` 不自动迁移、不读取，也不作为当前文件型 Catalog 的事实源。

## 后果

- 当前主链路不再为简单文件型 Skill 承担数据库、迁移、RPC 和子进程成本；
- Skill 内容可直接由版本控制和文件权限管理，发现与读取行为可用普通文件测试验证；
- 复杂候选生命周期、市场、评分、自动生成、自动演化与跨设备同步继续暂缓；
- 如果未来出现必须事务化维护、长期运行或独立数据所有权的真实用户故事，需要新的 ADR 和迁移方案，不能直接恢复旧边界。
