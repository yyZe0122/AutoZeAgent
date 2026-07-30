# AutoZeAgent 长期运行 Agent 优化方案

更新：2026-07-30（§5.3 Phase 0–2 代码已落地：chat agent + transcript 过滤；§5.2/§5.1 此前已完成）

## 1. 目标形态

AutoZeAgent 采用最小可生产架构：

```text
autozeagent CLI ──┐
autozeagent TUI ──┼──► gatewayclient ──► 本地 Gateway HTTP/SSE
                  │                           │
                  └───────────────────────────┘
                                              │
                                              v
                                         autozeagentd
                                              │
                                    Session / Task / Plan / Run
                                              │
                                       进程内 Agent Runner
                                              │
                                           Tool Broker
                                              │
                                    Approval / Capability Grant
                                              │
                                    File / Git / Process / HTTP
                                              │
                                           core.db
```

生产发布只包含：

```text
autozeagentd   一个长期运行守护进程
autozeagent    一个本地 CLI（含 TUI；无参 / aze 进入 TUI）
core.db        一个 SQLite 事实源
```

`Task` 表示持久目标，`Run` 表示可恢复执行尝试；二者都不是独立操作系统进程。未来子代理优先建模为带 `parent_run_id` 的逻辑子 Run，在同一 daemon 内使用 goroutine 执行，并继承预算、审批、授权、取消和审计约束。

客户端（CLI 子命令与 TUI）**并列**，都只通过 `internal/gatewayclient` 访问 Gateway；**不** exec CLI、不打开 `core.db`、不调 Provider、不经 Tool Broker。边界见 ADR-018、ADR-037。

## 2. 已实施优化

### P0：可重复验证基线

验证链路：

```text
gofmt check -> go vet -> go test -> build -> autozeagentd --check
```

开发脚本只构建 `autozeagent` 与 `autozeagentd`，健康检查使用隔离的工作区数据目录。

### P1：暂停、取消与重启恢复

已完成：

- Task 新增 `paused` 状态；
- CLI 和 Gateway 支持 pause/resume/cancel；
- pause/cancel 可取消正在进行的 Provider 与 Tool 调用；
- paused Run 保持可恢复记录，resume 后由 runner 继续；
- canceled Task 不再被 worker 领取，并撤销该 Task 的 Grant；
- Agent Provider/Tool 记录持久化到 `core.db`；
- fake Provider + 真实 RecordStore/Tool 持久化测试证明 daemon 重启后不会重复成功 Tool Call，并能继续后续 Provider 回合。

仍需环境验证：

- 真实外部 Provider 的断网、限流和恢复；
- Linux systemd kill/restart 与断电场景。

### P2：进程内 Scheduler

已完成：

- Scheduler 表迁入 `core.db`；
- Scheduler Store 直接使用 Core 的 `*sql.DB`；
- `autozeagentd` 启动单个 Scheduler heartbeat；
- SQLite claim/lease、lease 到期恢复和幂等 Core Task ID；
- Scheduler 通过 `tasksubmission.Service` 创建 Task；
- Job Gateway API 与 `autozeagent job create|list|status|pause|resume|cancel`；
- ACK 正确处理 waiting-approval Task；
- interval、retry/backoff 及 `run_once|skip|catch_up` misfire 策略；
- Scheduler → Task 垂直集成测试。

暂缓：

- cron 表达式；
- 分布式 Scheduler；
- 每个 Job 独立进程或 systemd unit。

固定间隔已经覆盖长期心跳和周期任务的当前需求。

### P3：执行预算与重试

已完成：

- Agent 级 Provider turn 与 Tool call 上限；
- Run 与 Plan 级 duration、token、cost 预算；
- Provider 响应超出预算时不执行其中的 Tool Call；
- usage 持久化，重启不能绕过预算；
- 只有 `ProviderError{Retryable:true}` 才重试；
- Provider 最多尝试三次，默认等待 100ms、200ms；
- `RetryAfter` 最高按 5 秒处理；
- context 取消可中止等待和调用；
- 精确达到 token 上限可完成当前响应，但不能开始下一回合；
- `MaxCostMicros == 0` 表示不限额或 Provider 未报告成本，正数成本上限严格执行。

### P4：本地客户端边界与 TUI

已完成：

- `internal/gatewayclient`：CLI 与 TUI 共用的 Gateway 外观（无 DB / tools / provider / grants）；
- `internal/tui`：Bubble Tea 交互层；`cmd/autozeagent` 仅解析 flag、`ensureDaemon`、调用 `tui.Run`；
- TUI 与 `autozeagent run|task|approval|…` **并列**调用同一 client，**不**转发 CLI 子进程；
- 无参 / `aze` 进入 TUI；TUI 与 `run` 经 `daemonctl` 确保唯一 daemon（退出不杀 daemon）；
- TUI 斜杠：`/quit`（及 `/q` `/exit`）为**唯一**退出；Ctrl+C 清空输入并提示；
- `/sessions`（及 `/back` `/clear`）回任务列表；`/tasks [id]` 列表或聚焦；
- `/model` 无参仅 status 一行文案；`/model provider/model` 经 `PUT /v1/config/model` 热切换（**可选列表 UX 见 §5.1**）；
- SSE 事件流 + 轮询刷新任务/计划/审批/run 视图。

TUI 生产 import 已收敛到 `gatewayclient` + `platform/paths` + `pkg/*` + charmbracelet；领域 DTO 经 `gatewayclient` 类型别名与状态常量暴露。§5.1 **边界/DTO/拆分主项已完成**；**交互 UX（模型选择、chrome、slash 对齐）见 §5.1 backlog**。

### P5：真实 Provider 现场发现（2026-07-29/30，证据而非厂商补丁）

本机 user 模式日志：`~/.local/share/autozeagent/logs/autozeagentd.jsonl`。  
复现时配置了外部模型（日志字段 `provider=deepseek`、`model=deepseek-v4-pro`）——**仅作证据**；下列缺口属于 **Tool 注册 → Broker → Agent Runner → Grant** 共用链，与具体厂商无关。禁止做成「某 Provider 兼容层」。

| 时间 | 现象 | 基础分类 |
| --- | --- | --- |
| 7/29 18:16 | Provider `invalid_request` 400：tool function name 不符合 `^[a-zA-Z0-9_-]+$` | 出站 tool **名契约**未在系统入口强制 |
| 7/29 18:21 | Provider 成功 1 tool call → Broker deny：`fs_list` 路径问题（日志：`paths must be absolute`） | 参数/路径 fail-closed 正确；**deny 直接杀 Run** 错误 |
| 7/29 18:26 | Provider 成功 → grant deny：`duration exceeds grant` | **ToolTimeoutMillis 与 grant MaxDurationMillis** 不一致 |
| 7/30 00:59 起 | daemon 空闲约 10h，进程/socket 存活，无 Task/Provider 流量 | 空闲稳定 **≠** 真实 Provider e2e 通过；**非** systemd |

三次历史 Run **均未 completed**。安全边界（deny、grant）在生效；产品缺口是契约与错误处理，不是「放宽策略」。

改进项见 **§5.2**（跨 Provider 基础能力）。

## 3. 已删除的复杂度

以下内容已从生产实现、构建或启动链路删除：

- `autozeagent-module-echo`；
- `autozeagent-scheduler`、`autozeagent-memory`、`autozeagent-evolution` 独立进程；
- Scheduler 独立 Service 包装、`scheduler.db` 和专用 migration runner；
- Memory、Evolution、Evolution Activation 未进入当前主链路的实现；
- Module Runtime、Registry、RPC、Supervisor、Manifest 和协议版本协商；
- 无消费者的 Event Dispatcher、delivery offsets；
- `/v1/modules` 与 ModuleDir；
- 构建、安装和示例配置中的模块入口。

Core migration 013 清理 `module_registry`、`module_offsets` 与旧 Evolution Activation 表。历史 migration 保留原序号，保证已有 `core.db` 可顺序升级。

## 4. 必须保留的安全边界

简化不等于绕过控制。以下能力继续保留：

- Tool Broker 是模型请求执行的唯一入口；
- Approval 与 Capability Grant；
- Canonical Plan Hash 绑定；
- symlink/junction 路径越界防护；
- 进程超时、输出上限、环境变量白名单和取消；
- Provider Secret Resolver 与日志脱敏；
- SQLite 持久化、Event Store 与 Audit；
- Task/Run/Job 幂等 ID 与恢复语义；
- 文件型 Skill Catalog 和每 Task Skill 快照。

## 5. 后续顺序

1. 真实 Provider 完整 Task 复验（环境；宜多厂商，至少覆盖曾失败路径；§5.2 代码已就绪）；
2. Linux 目标机 systemd 长期运行、Socket 权限、kill/restart 与断电恢复（环境）；
3. 基于 `parent_run_id` 的逻辑子 Run（需求驱动）；
4. 明确需求出现后再增加 **cron 表达式**（进程内固定 interval 已有；TUI `/cron` 只读列表见 §5.1）；
5. MCP/LSP 只通过 Tool Broker 增加窄适配器。

§5.2 与 §5.1（含 P3 TUI UX）代码项已完成（见下）。

当前不建议引入：通用模块框架、Actor 系统、消息队列、ORM、DI 容器、工作流 DSL、Judge Agent、多数据库事务、TUI 内嵌 daemon、TUI exec CLI、第二套客户端业务库、**按厂商 fork 的工具协议或改名映射层**、通用 slash 框架、IDE 级可拖拽分栏布局引擎。

### 5.1 客户端层（TUI / CLI）

原则：保持 **Gateway-only**；加强类型与包边界；避免为「对称」而造框架。列表 UI **一套壳 + `listKind` 枚举**，不拆三套独立列表页。

#### 已完成（边界）

| 优先级 | 项 | 状态 |
| --- | --- | --- |
| P0 | 维持 Gateway-only | 已完成（生产 import 无 tools/providers/store/agent） |
| P1 | DTO 收敛到 `gatewayclient` | 已完成（类型别名 + 状态常量；TUI 无 corequery/approvalsubmission/runexecution/kernel） |
| P1 | 小接口注入 `tui.Run` | 已完成（`tui.Gateway` + fake 测试；生产 `*gatewayclient.Client`） |
| P2 | 拆分 oversized `model.go` | 已完成（`model` / `update` / `cmds` / `view_frame`） |
| P2 | 与 CLI 共用校验 | 部分完成（`ParseApprovalAction` / `ParseTaskAction` / `PromptAllows` 在 gatewayclient；CLI workflow 可继续改用别名） |

#### P3：TUI 交互 UX（已完成）

对标 OpenCode/Crush 的「slash 进可交互列表 + 常驻上下文」，**不**抄其 Effect/fantasy 整栈。

**布局**

```text
┌─ autozeagent  user  ● ok  [theme] ─────────────────────────────────┐
│  task·abc  running                                                  │
├──────────────────────────────────────────┬──────────────────────────┤
│  主内容：唯一会话时间线（可 PgUp 滚动）     │  Metrics                 │
├──────────────────────────────────────────┴──────────────────────────┤
│  浮动层（输入上方）：sessions/models/jobs/slash/审批 · 不占 viewport │
│  strip：model · cwd · ctx · ♥ 运行态                                 │
│  ┌─ agent|plan 边框 ─────────────────────────────────────────────┐  │
│  │ › 输入   左下角 mode chip · Tab 切换 agent↔plan                 │  │
│  └───────────────────────────────────────────────────────────────┘  │
└─────────────────────────────────────────────────────────────────────┘
```

- **主区唯一**：会话时间线；list 不再替换 viewport。
- **浮动 picker**：`/sessions` `/model` `/cron`、slash 补全、审批面板叠在输入上方；↑↓ 归 picker，**PgUp/PgDn 始终滚会话**。
- **Agent/Plan = 权限模式**（非内容 Tab）：Tab 切换 `draftMode`；输入框边框/chip 变色；提交带 `execution_mode`。
- **plan 模式**：提交后规划并停在审批；批准后可 `Start`；工具权威仍是 Policy + Grant（非 mode 永久 R0）。
- **strip / Metrics / `/theme`**：同前。

**一套 list 壳 + `listKind`（必须复用）**

```text
listNone | listSessions | listModels | listJobs
         ↓
  renderListView：title + hint + cursor 行（同一布局/样式）
         ↓
  listLen / listTitle / listLine / listEnter 按 kind 分支
```

| Overlay | 触发 | 数据 | Enter | Esc |
| --- | --- | --- | --- | --- |
| Sessions | `/sessions`、`/tasks`（无参） | `ListTasks` | 聚焦 task + refresh | 关 overlay / 保持列表 |
| Models | `/model`（无参） | `ModelConfig.Models` | `SetModelConfig` | 关 |
| Jobs | `/cron` | `ListJobs` | 详情到 status（本轮只读） | 关 |

键盘：`inListMode()` 在任一种 list 打开时 ↑↓/Enter/Esc，与现任务列表同一路径。**不要**再拆三套完整 `renderModelList` / `renderJobList`（最多 `format*Line` 小函数）。

**Slash 语义**

| 命令 | 行为 |
| --- | --- |
| `/sessions`、`/tasks` | **等价（无参）**：清当前聚焦 + 打开 Sessions 列表（标题 *Session history*），`refreshCmd`；`/tasks [id-prefix]` 仍匹配聚焦 |
| `/back`、`/clear` | 清聚焦；默认可与 sessions 同效 |
| `/model` | 打开 Models 列表（当前项 `*`）；`/model x/y` 仍直接切换 |
| `/cron` | 新增：`ListJobs(false)` → Jobs 列表（id / name / status / next）；**本轮只读** |
| `/help` | 更新文案 |

**Chrome 数据来源（无新架构）**

| 字段 | 来源 |
| --- | --- |
| model | 已有 `loadStatusCmd` / `/model` |
| cwd | `os.Getwd()` 在 TUI 进程 init（客户端 cwd，非 daemon workspace API） |
| theme | ConfigDir `tui.json`；`/theme` 切换 day↔night |
| draftMode agent/plan | Tab 本地切换；Submit 带 `execution_mode`；落库 `tasks.execution_mode` |
| task token usage | `GET /v1/tasks/{id}/usage`（`corequery.TaskUsage` 汇总 `agent_run_records`） |
| context window / cache / MCP | TUI `SessionMetrics` 接口占位（底层未接线时 `—`） |
| 运行态 / 时长 | 本地 task/run state + `StartedAt`；心跳动画 `tickMsg` |
| floating pickers | sessions/models/jobs/slash/approval 叠在输入上，不替换时间线 |
| task / runs / budget max | 已有 `refreshDoneMsg` / plan prompt（budget 作 token 分母展示） |

**实现顺序**

1. `tui.Gateway` + fake：`ListJobs`；status 缓存 `dataDir` / `cwd`；
2. `listKind` + 单一 `renderListView` + 键盘路径统一；
3. `/model` picker、`/sessions`≡`/tasks`、`/cron`；
4. context strip + 宽屏右侧 Context（固定比例拼装，非可拖拽 IDE）；
5. 测试 + 本节状态勾选。

**明确不做（边界展开）**

| 不做 | 含义 |
| --- | --- |
| TUI 开 DB / 调 Provider | 不 `import` store/sqlite/agent/tools/providers；不持有业务 `*sql.DB` |
| TUI exec CLI | 不 `exec.Command("autozeagent", …)`；CLI 与 TUI **并列** Gateway 客户端 |
| 通用 slash 框架 / arg 补全 DSL | 保持 `slashCommands` 切片 + `handleLineCmd` switch；不做 RegisterCommand/插件/参数级补全引擎/UI 状态机 DSL |
| `/cron` 本轮写操作 | 只读 `ListJobs`；pause/resume/cancel 二期再复用 `JobAction` |
| IDE 级双栏布局引擎 | 不做可拖拽分栏、多 pane、布局持久化；宽屏固定 body‖context，窄屏仅 strip |
| 强制 Jobs 第三 Tab | 本轮用 `/cron` 打开 list overlay；不必新增 Tab |

**验收**

```bash
go test ./internal/tui/ ./internal/gatewayclient/ ./cmd/autozeagent/ -count=1
# tui 生产 import：gatewayclient + platform/paths + pkg/* + charmbracelet
```

- `/model` → 主区可选列表，当前模型高亮；Enter 热切换并更新页眉；
- `/sessions` 与 `/tasks`（无参）行为一致 → Session history；Enter 打开任务；
- `/cron` → 定时 Job 只读列表；
- 输入上方 strip 可见 cwd + model + task/run 摘要；宽屏右侧有 data dir 与 budget。

设计锚点：`docs/architecture/018-local-gateway-boundary.md`、`037-cli-daemon-lifecycle.md`。

### 5.2 基础 Tool/Agent 契约硬化（跨 Provider）

原则：问题在 **共用执行链**，不在某个 HTTP 适配器。业界 coding agent（OpenCode、Crush 等）已把「可恢复 tool 失败回灌会话」做成通法；我们吸收 **行为契约**，不抄 Effect/fantasy 整栈，也不做厂商特判。

```text
Register(tool)  →  名 / schema 契约（真源）
      ↓
Agent 选工具定义 → providerapi.ToolDefinition（不改名）
      ↓
任意 Provider   →  只序列化；不发明命名规则
      ↓
模型 tool_call
      ↓
Broker.Execute  →  参数 / 路径 / grant / timeout  fail-closed
      ↓
Agent Runner    →  可恢复错误回灌 tool 消息；不可恢复才 fail Run
```

#### 参考实现（只读对照，2026-07-30）

| 主题 | OpenCode（`anomalyco/opencode`） | Crush（`charmbracelet/crush`） | AutoZe 应对齐的行为 |
| --- | --- | --- | --- |
| 参数/路径/业务失败 | `InvalidArgumentsError`：明确「错什么 + rewrite input」；失败进 tool result | 多数路径 `NewTextErrorResponse(msg), nil`（**error 不致命**） | deny/invalid args → **tool 消息回灌**，继续 turn |
| 用户拒绝权限 | permission 流 | `NewPermissionDeniedResponse` + **`StopTurn`**（结束本轮，避免对同一 deny 死循环） | 用户硬拒绝可结束 Run/Turn；策略 deny 优先回灌可改参数的文案 |
| 相对路径 | `path.resolve(workspace, path)`；schema 写 absolute，实现仍接受相对 | `SmartJoin(workingDir, path)` + expand | `Join(workspace, rel)` → containment；描述写「优先绝对，相对=相对 workspace」 |
| 出 workspace | `assertExternalDirectory` → ask | 出 workdir → permission.Request | **保持** root/grant 更严；越界 deny 后 **回灌**，不杀 Run 当唯一手段 |
| Tool 名 | 内置 `read`/`write`/`bash` 等简单标识符；协议差在 provider 层 | 内置 `view`/`ls`/`bash`；`fantasy` 抽象多厂商 | **Register** 强制 `^[a-zA-Z0-9_-]+$`；无 per-vendor 改名 |
| 大输出/超时 | truncate + metadata | 读上限 → TextErrorResponse，会话继续 | 超限可读错误；**grant 时长与 ToolTimeout 签发时对齐**（AutoZe 特有） |

**明确不照搬：** Effect 全栈、fantasy 工具框架、默认宽松任意盘符、按厂商 fork 工具协议。

#### Backlog

| 优先级 | 项 | 说明 | 主要落点 | 状态 |
| --- | --- | --- | --- | --- |
| **P0** | 可恢复 tool 错误回灌 | 对标 OpenCode/Crush 核心。`ErrToolDenied`/`toolapi.ErrDenied`、参数/路径校验失败：序列化为 tool result（含「如何改参数」），继续 turn。context 取消、预算、持久化失败仍终止 Run。可选：用户硬拒绝类比 Crush `StopTurn`。 | `internal/agent/runner.go`（`broker.Execute` 错误分支） | 已完成 |
| **P0** | Tool 名契约 | 注册时强制 `^[a-zA-Z0-9_-]+$`。`pkg/toolapi.ValidName`，`Broker.Register` 调用。非法名启动失败。 | `internal/tools` Register；`pkg/toolapi` | 已完成 |
| **P1** | 路径与提示 | 相对 → workspace Join 再 containment（PathGuard/`resolveRelative`）；schema/description 与 OpenCode/Crush 同口径；安装二进制与源码一致。越界仍 deny，经回灌通道。 | `internal/tools` | 已完成（代码）；二进制一致为环境 |
| **P1** | 超时 ⊆ grant | `ToolTimeoutMillis` 取 step 与各 capability `MaxDurationMillis` 的最小值，请求 duration 不得超过 grant。 | `internal/runexecution` | 已完成 |
| **P2** | 空闲可观测（可选） | 低频 heartbeat/启动摘要；勿刷屏。 | daemon 日志 | 未做 |
| **—** | 明确不做 | 按厂商 fork 工具协议；单个 provider 包改名/映射；放宽 Policy/Grant；绕过 Broker；为兼容某 API 放宽路径越界；引入 Effect/fantasy 整栈 | | |

#### 实现顺序（建议）

1. **回灌**（收益最大，直接对应现场 18:21/18:26 deny 杀 Run）；
2. **路径** Join + 描述 + 确认已安装 daemon 与源码一致；
3. **Tool 名** Register 校验（防出站 400）；
4. **timeout ⊆ grant**；
5. 可选空闲可观测。

**不要**先写 `if provider == …`。各 provider 出站层仅在名契约已保证后可删重复校验。

回灌策略（默认）：

- **回灌并继续**：`errors.Is(err, tools.ErrToolDenied)` 及明确的 invalid-arguments 类 deny；文案说明错因与改法（绝对路径、合法 path、timeout 等）；
- **仍可终止 Run**：context 取消、预算错误、记录/持久化失败、空最终回复、迭代上限等；
- **可选后续**：用户明确拒绝执行 → 结束 Turn/Run（Crush `StopTurn`），避免对同一权限提示空转重试。

验收：

```bash
go test ./internal/tools/ ./internal/agent/ ./internal/runexecution/ ./internal/providers/... -count=1
# 环境：任意已配置 Provider，完成含 fs 的 Task（合法路径 + 一致 grant）
# 期望：Task completed；jsonl 有 provider iteration 与 tool 成功（或 deny 回灌后模型纠正）
# 回归：fake broker 返回 ErrToolDenied → 有 tool record、runner 继续、不 fatal
```

安全边界（§4）不变：deny 仍 fail-closed（不执行副作用）；改变的是 **Agent 是否把 deny 当会话可恢复信号**，而不是「放行未授权工具」。

### 5.3 Session 多轮 Chat Agent（双轨：agent chat / plan 工作流）

**状态：Phase 0–2 已落地（2026-07-30）** — agent 提交走 `chatsession.StartChat`（跳过 Planner）；plan 仍规划→审批；transcript 过滤 step 模板「鬼消息」。

#### 问题（现场）

当前 `execution_mode=agent` 仍走：

```text
用户话 → Task.objective → Planner → Plan → (agentautostart) approve+Start
  → 每 step 的 executionMessages（内部 user 模板）→ Agent → tool
```

后果：

1. 「你好」被当成 **objective**，Planner 可能规划「列表当前目录」等步骤；
2. `runexecution.executionMessages` 把 `Task objective / Current step / Approved capabilities` 写成 `role=user` 并落入 `agent_run_records`；
3. TUI transcript 原样渲成 `▸ you`，像「鬼消息 / 串会话」——实际是 **内部 step 提示泄漏**，不是 Session 串线。

`agent` 在现实现里只表示「允许最终执行」，**不是** OpenCode/Crush 的「直接多轮聊天」。

#### 目标体验

| 模式 | 行为 |
| --- | --- |
| **agent**（默认） | 输入直接进 **当前 Session 多轮对话**；模型可调工具；**不经 Planner / 不经 waiting_approval** |
| **plan** | 保持：规划 → 显式审批 → 按 step 执行；内部 `executionMessages` **不进**聊天气泡 |

对齐 OpenCode/Crush：**Session = 聊天容器**；Task/Run = 底层执行尝试。安全仍走 **Broker + path containment + timeout + audit**（Gateway 不执行 tool、不调 provider、不发 grant）。

#### 现状 vs 目标

```text
现状 agent:
  用户话 → Task.objective → Planner → Plan → auto approve → step 模板 → Agent
  transcript 把 step 模板渲成 ▸ you

目标 agent:
  用户话 → Session transcript 追加 user
        → Chat Run（历史 = 过滤后的会话消息）
        → Agent loop + workspace 预授权 grants
        → stream + assistant/tool 写入 records
        → UI 只显示真人话 + assistant + tool（含 thinking）
```

#### 关键设计

**1. 双轨入口（`execution_mode`）**

| `execution_mode` | Submit 之后 |
| --- | --- |
| `agent` | **不调用** `PlanTask`；走 chat 编排（`chatsession` / `runexecution.StartChat`） |
| `plan` | 现有异步规划；停在 `waiting_approval` 等人批（与 chat 分离） |

`agentautostart`：agent 轨 **离开 Planner** 后不再依赖「plan 完再 start」；wrapper 仅服务 plan 轨（若仍需要）或收敛删除 agent 路径。

**2. 多轮历史（每用户一轮 = 一个 Chat Run）**

1. 从过滤后的 `SessionTranscript` 重建 `[]providerapi.Message`；
2. 追加本轮 user；
3. `agent.Run` → 新 `run_id`，records 只追加本轮 assistant/tool；
4. 下一轮再读全量 transcript。

建议：**每轮 user 仍建一个 Task**（`execution_mode=agent`，`running→completed`），便于用量/取消；**跳过 Planner**。Session 串起多 Task。

**3. Session 工作区预授权（工具）**

现 `IssueGrant` 绑定已批准 Plan 上的 capability；`validateRunRequest` 强制 PlanID/PlanHash/StepID。

为少拆 grant 内核：首次 agent 发言时 **EnsureSessionWorkspaceAuth**——创建（或复用）**用户不可见**的 session 级 synthetic Plan + 自动 Approved + `IssueGrant`（形状对齐 `issueGrants`）：

| 工具 | 默认预授权 |
| --- | --- |
| `fs_read` / `fs_list` / `fs_stat` | workspace root 内 |
| `fs_write` / `fs_patch` / `fs_mkdir` | workspace root 内（可配置） |
| `process_exec` | 默认关或 allowlist |
| `http_get` | 默认关 |
| `git_*` | root 内只读优先 |

配置：`autozeagent.json` 的 `chat.tools` / `chat.roots`（缺省 = daemon cwd roots，与 `RegisterBuiltins` 一致）。Broker/PathGuard/timeout/audit **不变**。Chat 路径 **不用** `executionMessages` 用户模板。

**4. Transcript 展示规则**

| 来源 | 展示 |
| --- | --- |
| `task-user:` / 真实用户 objective | `▸ you` |
| `input_message` 匹配 `Task objective:` + `Current step:` / `Approved plan` / `Approved capabilities` | **丢弃** |
| `role=system` | **丢弃** |
| `assistant_message` / `tool_result` | 展示（thinking / tool） |
| synthetic session plan 的 input | **丢弃** |

落点：`internal/corequery/session.go`（`SessionTranscript` / `TaskTranscript`）。

**5. Chat system prompt**

替换 step 模板为会话向提示（工具仅在需要时用；路径优先 workspace 绝对路径；跟用户语言）。**禁止**「Execute exactly one approved plan step…」。

**6. Agent 请求**

`RunKind: plan_step | session_chat`；chat 仍带 synthetic plan/grant id 以满足 Broker，但不要求人类 plan 语义。`validateRunRequest` 按 kind 分支。

#### 数据流

```text
TUI (agent, session focused)
  │ plain text
  ▼
Gateway POST /v1/sessions/{id}/messages  （或 SubmitTask agent 语义升级）
  │
  ▼
chatsession / runexecution.StartChat
  ├─ ensure Session
  ├─ EnsureSessionWorkspaceAuth（synthetic plan+grants，一次）
  ├─ 轻量 Task（本轮）+ history from filtered transcript
  └─ agent.Run → modelstream + agent_run_records
```

#### 实现阶段

| Phase | 内容 | 状态 |
| --- | --- | --- |
| **0** | Transcript 过滤 plan-step `input_message`；测试「你好」不出现 Current step 气泡 | **已完成**（`corequery.skipTranscriptRecord` / `isInternalStepPrompt`） |
| **1** | `StartChat` + agent 跳过 Planner + workspace 预授权 grants + chat system prompt + `agent.History` | **已完成**（`internal/chatsession`；`tasksubmission` agent 分支） |
| **2** | Submit 升级（agent→chat）；TUI 文案 agent=chat / plan=规划；model-stream 复用 | **已完成**（无新 POST messages；同 `POST /v1/tasks` + `execution_mode`） |
| **3** | `chat` 配置、审计 actor、短 ADR / 本节验收勾选 | 部分（审计 `chat.start`/`chat.execute`；配置仍默认写开） |
| **4** | 取消当前 chat run、请求侧裁剪长 tool 输出、plan 轨同样过滤内部 prompt | 未做（过滤已覆盖 plan 轨 transcript） |

落地要点：

- `tasksubmission`：`execution_mode=agent` → `ChatStarter.StartChat`；`plan` → 原异步 Planner。
- `chatsession`：`CreateApprovedWorkspacePlan`（task `created→running`，plan 直接 `approved`）+ `RecordSystemApproval` + grants；**禁止** `planning`/`waiting_approval`。
- `runexecution.nextExecution` 跳过 `step_id=chat-step`，避免与 chat 抢跑。
- Scheduler 提交强制 `execution_mode=plan`。

#### 主要落点

| 区域 | 路径 |
| --- | --- |
| Chat 编排 | 新 `internal/chatsession/` 或扩 `runexecution` |
| 跳过规划 | `internal/tasksubmission/service.go` |
| Agent | `internal/agent/runner.go`（kind、prompt） |
| Grants | session ensure + 现有 `issueGrants` 模式 |
| Transcript | `internal/corequery/session.go` |
| 自动启动 | `internal/agentautostart` 收敛为 plan-only |
| 装配 | `cmd/autozeagentd/main.go` |
| API / 客户端 | `internal/gateway`、`internal/gatewayclient` |
| TUI | `internal/tui`（cmds / update / chat timeline） |

#### 明确不做（本阶段）

- stream delta 写入 DB（ADR-031）；
- 恢复 Module Runtime / 第二套 DB；
- chat 绕过 Broker 或 Gateway 内调 Provider；
- 完整 OpenCode 式每工具弹窗矩阵（预授权 + 可选后续 per-tool ask）；
- 为 UI 放宽路径越界。

#### 风险与缓解

| 风险 | 缓解 |
| --- | --- |
| Synthetic plan 暴露在列表/UI | transcript 过滤；可选 `plan.kind=session_chat` 隐藏 |
| 预授权过宽 | 默认只读 fs；write/exec 配置开关 |
| 与 agentautostart 冲突 | agent 不再 plan；A 仅 plan 轨或删除 agent wrapper |
| `validateRunRequest`/grant 耦合 | 显式 `RunKind` + 双路径测试 |

#### 验收

1. **agent**：只发「你好」→ 一条 `you: 你好` + assistant 寒暄；**无**「列表当前目录 / Task objective / Current step」气泡；**不**强制 `fs_list`；
2. **agent 多轮**：第二句同一 Session，历史可见；
3. **agent + 工具**：用户要求列目录时 workspace 内成功；越界 fail-closed；
4. **plan**：仍 planning → waiting_approval → a/r → Start；Grant 约束工具；
5. **stream**：chat 打字机；complete 后 transcript 与 records 对齐；
6. `make check` / `go test ./...`。

设计锚点：`docs/architecture/012-tool-broker-execution-boundary.md`、`018-local-gateway-boundary.md`、`030-agent-run-record-recovery.md`、`031-unified-provider-streaming-event.md`；TUI 会话壳见 §5.1。

## 6. Go 简单精神

- 领域对象使用具体结构体；
- 并发使用 `context.Context`、goroutine 和明确 owner；
- 一个组合根直接装配依赖；
- 一个 SQLite 数据库承担持久事实源；
- 小接口只放在调用方；
- 错误显式返回，恢复语义由测试固定；
- 新抽象必须由当前用户故事证明；
- 客户端：TUI 是 `cmd` 的一种入口，业务能力只在 daemon；UI 状态用 Bubble Tea `Model`，副作用用 `Cmd`，不用全局 UI 总线。

## 7. 当前验收状态

已通过：

- [x] Task pause/resume/cancel；
- [x] Agent 持久化与重启恢复；
- [x] Provider/Tool 取消与有界重试；
- [x] Scheduler heartbeat 与 daemon 恢复；
- [x] token/cost/duration/turn/tool 预算；
- [x] 发布命令仅 `autozeagentd` 与 `autozeagent`；
- [x] 事实源仅 `core.db`；
- [x] 生产启动链路无通用 Module Runtime；
- [x] Gateway-only CLI/TUI + `gatewayclient`；
- [x] TUI `/quit` 专退、`/sessions` 回列表、`/model` 热切换并回写配置；
- [x] 客户端层 §5.1 边界（DTO / Gateway 接口 / model 拆分）+ P3 TUI UX（list 壳 / slash / chrome）；
- [x] §5.2 Tool/Agent 契约硬化（回灌 / 路径 / 名 / timeout⊆grant）；
- [x] `go test -count=1 ./...`；
- [x] `go vet ./...`；
- [x] `scripts/dev.ps1 -Action check`；
- [x] `scripts/dev.ps1 -Action all`；
- [x] `bin/autozeagentd.exe --check`；
- [x] Windows 原生构建与 Linux amd64 `CGO_ENABLED=0` 交叉构建；
- [x] `git diff --check` 无空白错误，仅有现存换行符转换警告。

本机环境观察（2026-07-30，非 systemd）：

- [x] user 模式 `autozeagentd` 空闲约 10h 无崩溃（无 Task 负载；**不能**替代真实 Provider e2e）；
- [ ] 真实 Provider **成功**完整 Task（历史 3 次 Run 均 failed，见 §2 P5）；
- [ ] Linux systemd 长期运行、Socket 权限、失败重启和断电恢复。

代码 backlog（§5.2 — 已完成）：

- [x] 可恢复 tool deny 回灌（`agent.Runner`；对标 OpenCode/Crush tool result；`toolapi.ErrDenied`）；
- [x] 路径：workspace Join（PathGuard）+ fs schema/description 口径；安装二进制需与源码一致（环境）；
- [x] Tool 名契约（`toolapi.ValidName` + `Broker.Register`）；
- [x] Tool timeout ⊆ grant MaxDuration（`runexecution.toolTimeoutMillis`）。

客户端层（§5.1 边界 — 已完成）：

- [x] TUI import 仅限 `gatewayclient`（DTO 收敛）；
- [x] `tui.Run` 窄接口 + fake 测试；
- [x] 拆分 oversized `model.go`（`model` / `update` / `cmds` / `view_frame`）。

客户端层（§5.1 P3 TUI UX — 已完成）：

- [x] 一套 list 壳 + `listKind`（sessions / models / jobs）；
- [x] `/sessions` ≡ `/tasks`（无参）→ Session history；
- [x] `/model` 可选列表 + Enter 热切换（`/model x/y` 保留）；
- [x] `/cron` 只读 Job 列表（`Gateway.ListJobs`）；
- [x] context strip（cwd · model · task/run）+ 宽屏右侧 Context（data dir · budget）；
- [x] 测试 + help 文案更新。

会话 / Chat Agent（§5.3）：

- [x] Phase 0：transcript 过滤 plan-step 内部 `input_message`（消「鬼消息」）；
- [x] Phase 1：agent 跳过 Planner + `StartChat` + Session workspace 预授权 grants；
- [x] Phase 2：Submit/TUI 接线（agent=多轮 chat，plan=审批工作流）；
- [ ] Phase 3：`chat` 配置开关（write/exec）与验收勾选补全；
- [ ] Phase 4：取消 chat run 与 runexecution 中断对齐、长上下文裁剪。
