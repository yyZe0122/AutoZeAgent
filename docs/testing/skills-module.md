# Skills 测试说明

ADR 034 已将简单文件型 Skill 的当前边界改为 Core 内的 `internal/skillcatalog`。原进程外 Skills 命令、数据库迁移、RPC 合约与配置入口已删除；复杂 Candidate、市场、评分和自动演化能力继续暂缓。

## 文件型 Skill 聚焦验证

Windows 本地：

```powershell
$env:GOCACHE=Join-Path $PWD '.cache\go-build'
$env:GOMODCACHE=Join-Path $PWD '.cache\gomod'
$env:GOPATH=Join-Path $PWD '.cache\gopath'
$env:GOTELEMETRY='off'
go test ./internal/skillcatalog
```

Linux 目标机：

```bash
go test ./internal/skillcatalog
```

覆盖范围：

- 从 `<root>/<skill-id>/SKILL.md` 发现 `name` 和 `description`；
- Catalog 稳定排序，后配置的高优先级根目录覆盖同 ID；
- 正文按需读取，不在发现阶段加载；
- 非法 frontmatter、未知或重复选择、伪造选择 fail closed；
- 符号链接目录不会越界进入 Catalog；
- 上下文保持显式选择顺序、转义内容并严格执行字节预算。

## 真实调用链验证

Windows 本地：

```powershell
$env:GOCACHE=Join-Path $PWD '.cache\go-build'
$env:GOMODCACHE=Join-Path $PWD '.cache\gomod'
$env:GOPATH=Join-Path $PWD '.cache\gopath'
$env:GOTELEMETRY='off'
go test ./internal/kernel ./internal/store/sqlite ./internal/skillcatalog ./internal/tasksubmission ./internal/planner ./internal/gateway ./cmd/autozeagentd
```

Linux 目标机：

```bash
go test ./internal/kernel ./internal/store/sqlite ./internal/skillcatalog ./internal/tasksubmission ./internal/planner ./internal/gateway ./cmd/autozeagentd
```

覆盖范围：

- Task 创建事务持久化空或非空的不可变 Skill 快照，并保留显式选择顺序、正文、SHA-256 与创建时间；
- 数据库 trigger 拒绝快照 UPDATE/DELETE，读取时重新计算 SHA-256，内容被篡改则 fail closed；
- Task Submission 对未知 ID、重复 ID 或不可用 Catalog fail closed；
- 相同 Task ID 的幂等重试使用持久快照，不重读已经删除或变化的 Skill 文件；
- Planner 将 Skill context 注入独立 system message，Memory 仍位于 user message 并明确视为不可信数据；
- Skill 不能扩大 Planner capability schema，也不能创建 Approval/Grant、修改 Policy 或执行 Tool；
- `GET /v1/skills` 只公开稳定排序的元数据，不泄露文件路径或正文；
- `autozeagentd` 按配置目录、项目目录的优先级发现 Skill，同 ID 由项目目录覆盖，并保留非法文件 diagnostic。

## legacy artifact 删除回归

Windows 本地：

```powershell
go test ./internal/architecture
```

Linux 目标机：

```bash
go test ./internal/architecture
```

架构测试要求旧 `cmd/autozeagent-skills`、`internal/skills`、`pkg/skillsapi`、`migrations/skills` 与配置示例持续不存在，并继续验证模型可寻址的 Module 能力不能绕过 Tool Broker、Canonical Plan Hash、Approval、Grant 和 Audit。
