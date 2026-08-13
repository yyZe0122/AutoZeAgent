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

- 从 `<root>/<skill-id>/SKILL.md` 发现 `name`、`description` 与可选 `triggers`；
- `Match(query)` 按 triggers > id/name > description 词边界打分（上限 3；供 `skills_list` suggested）；
- `ListLinked` / `ReadLinked` 只读 skill 目录内相对文件，拒绝越界与 symlink；
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
go test ./internal/kernel ./internal/store/sqlite ./internal/skillcatalog ./internal/tasksubmission ./internal/chatsession ./internal/gateway ./cmd/ymzd
```

Linux 目标机：

```bash
go test ./internal/kernel ./internal/store/sqlite ./internal/skillcatalog ./internal/tasksubmission ./internal/chatsession ./internal/gateway ./cmd/ymzd
```

覆盖范围：

- Task 创建事务持久化空或非空的不可变 Skill 快照，并保留显式选择顺序、正文、SHA-256 与创建时间；
- 数据库 trigger 拒绝快照 UPDATE/DELETE，读取时重新计算 SHA-256，内容被篡改则 fail closed；
- Task Submission 忽略空白、保序去重；对未知 ID 或不可用 Catalog fail closed（`Catalog.Select` 仍拒空/重复）；
- 相同 Task ID 的幂等重试使用持久快照，不重读已经删除或变化的 Skill 文件；
- Chat（`chatsession`）从 Task Skill 快照注入**独立** system 消息（ADR-036）；只读快照、不重读 `SKILL.md`；不存在 Memory 模块；
- Skill 不能扩大 capability schema，也不能创建 Approval/Grant、修改 Policy 或执行 Tool；
- `GET /v1/skills` 只公开稳定排序的元数据（可含 draft/last_used/archived），不泄露文件路径或正文；`gatewayclient.ListSkills` + TUI `/skills` 显式选择并随 `POST /v1/tasks` 附带 `skill_ids`（仅显式进快照）；模型经 `skills_list` / `skill_view` 按需加载（归档隐藏；加载不发 grant）；`/skills apply|reject` 经 `POST /v1/skills/actions`（ADR-050）；
- `chatsession` 注入 `<ConfigDir>/AGENTS.md`，有则再追加 `<workspace>/.yunmengze/AGENTS.md`（`injectscan`；不扩 grant）；
- `ymzd` 按配置目录、项目目录的优先级发现 Skill，同 ID 由项目目录覆盖，并保留非法文件 diagnostic。

额外建议：

```bash
go test ./internal/gatewayclient ./internal/tui ./internal/chatsession -count=1
```

## legacy artifact 删除回归

若仓库含 `internal/architecture` 包：

```bash
go test ./internal/architecture
```

架构意图：旧 `cmd/ymz-skills`、`internal/skills`、`pkg/skillsapi`、`migrations/skills` 与配置示例持续不存在；模型可寻址能力不能绕过 Tool Broker、Canonical Plan Hash、Grant 和 Audit。
