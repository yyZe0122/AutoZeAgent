# AutoZeAgent 当前状态

更新：2026-08-11

**本文件是唯一活着的优化/backlog 文档。** 只写未完成与暂缓项；已落地细节见 ADR（`docs/architecture/`）与 git。

## 现状

生产形态稳定：`autozeagentd` + CLI·TUI + `core.db`。设计知识库：`docs/architecture/`。

对照 Crush（`/tmp/opencode-compare/crush`）只读评估后：偷 **编排与 UX 契约**，不偷软权限、不把 agent/tools 拉进 TUI。

## 原则（不变）

- 三件套：`autozeagentd` / CLI·TUI / `core.db`。不恢复 Module Runtime、多 DB、交互 **Planner**（plan-step 整单审批轨）。
- 工具副作用只经 Tool Broker；Policy → Approval → Capability Grant → containment → 限流 → Audit。
- Skill 仅指令文本，不扩大授权；`skill_ids` 显式选择（ADR-036）。
- plan 永远只读；高风险工具仅 agent + `chat.tools` allowlist 预授权（ADR-038）；**tool-call** 交互 permission 见 ADR-043（≠ Planner）。
- 会话记忆为 in-process MemoryManager（ADR-044），非独立 Memory 进程。
- **客户端分层（ADR-018/022）：** 业务用例只在 daemon；Gateway 仅 HTTP 适配；CLI 与 TUI 经 `gatewayclient` 并列，TUI **不** exec CLI、**不** import tools/providers/agent。
- **Go 精神：** 具体类型 + 调用方小接口；composition root 在 `cmd/`；无 DI 容器 / ORM / 通用事件总线。

## 有序 backlog（Crush 启发）

执行顺序按依赖：先稳 daemon 事件源，再叠 TUI，再编排健壮与渲染分型。

| ID | 项 | 目标 | 约束 / 验收 |
| --- | --- | --- | --- |
| **T1** | Stream debounce + 终态 after flush | **已落地** `modelstream` 33ms 合并 delta/thinking；`PublishTerminal` 在 complete/fail/cancel 落库后 fan-out | 见 `internal/modelstream`、`chatsession` lifecycle |
| **T2** | Permission modal（四档 + grace） | **已落地** pending 自动 open + 400ms grace；**1–4** / o s p d；Enter 循环四档；permanent 走 confirm | 仍四档；`DecidePermission*` only |
| **T3** | Follow + Finished 冻结缓存 | **已落地** stickBottom follow（上滚关/触底开）；timeline 完成行稳定指纹、streaming 行不冻结 | 无新 viewport/list 依赖 |
| **T4** | Loop detection + cancel 清理写 | **已落地** loop：`agent/loop_detection`；cancel/fail：`Broker.CancelIncompleteToolCalls` + chatsession lifecycle | Broker fail-closed；ADR-012 |
| **T5** | 子 Run usage 上卷 parent | **已落地** `corequery.RunUsage` + `GET /v1/runs/{id}/usage` + TUI Metrics parent/children 旁注 | 可观测 only；ADR-039 |
| **T6** | Tool 行分型渲染 | **已落地** `tui/tool_render.go`：fs/process/git/task/http 预览；path-only；result 行关联 name | 无 diff 引擎；无 import tools |
| **T7** | 工具描述与 Register 同址 | **已落地** 描述在各 Tool `Definition()`；`RegisterBuiltins` 唯一注册 | 不改执行边界 |
| **T8** | 可选增强 | Permission SSE；live markdown 稳定前缀缓存 | 非阻塞 |

**TUI 渲染策略：** 表现层只在现有 `internal/tui` 增量；不引入更重的 list/viewport 依赖（相对已有 `bubbles/viewport` 不再加新合成层）。

## 日志链路（L1）

| ID | 项 | 状态 |
| --- | --- | --- |
| **L1** | 结构化日志链路 + `aze logs --session/--task` + ADR-047 | **已落地**（gateway/tasksubmission/chatsession/taskcontrol/scheduledtasks 阶段边界；`internal/runlog`） |
| — | 全链路 e2e harness / OTel | **不做**（真机 + 日志；单测护栏保留） |

排障：`AUTOZEAGENT_LOG_LEVEL=debug` + `aze logs --run <id>`。约定见 ADR-047。

## 可选（非阻塞，未承诺）

| 项 | 说明 |
| --- | --- |
| **gatewayclient 薄 ops** | CLI↔TUI 组请求胶水痛时再抽；不新建第二控制面 |
| CLI skills | `run --skill` / `skills list`（主路径仍是 TUI `/skills`） |
| CJK FTS 扩展 | unicode61/trigram + LIKE 兜底；专用 C 扩展另议 |
| `write_approval` 记忆门控 | 可后置（已有 ADR-043） |
| 更多 model roles | vision 等：有工具后再加 |
| 大文件同包再拆 | `kernel/repository`、`tools/fs`+`broker`、`tui/cmds`+`update`；`main.go` 仅摩擦大时再抽 `wire.go` |
| **T8 Permission SSE** | poll 体感差时：镜像 modelstream hub + gateway SSE；decide 仍只走 `DecidePermission*` |
| **T8 live md 前缀缓存** | 引入 markdown 渲染且 streaming 卡顿时：稳定前缀 + 只重渲 tail；叠 T3 freeze |

```text
用例（daemon services）     → 已统一
外观（gatewayclient）       → 已共享
语法（CLI argv / TUI slash / HTTP）→ 故意分叉
```

## 暂缓 / 不做

| 项 | 说明 |
| --- | --- |
| 新 viewport/list 引擎 | 不引入 Crush-style lazy list / Ultraviolet；不新增 list 库 |
| Crush 三档 permission | 保持 once/similar/permanent/deny 四档；只借 modal/grace/快捷键形式 |
| 沙箱 phase-2+ | namespace / bubblewrap / seccomp |
| LSP | 另案 |
| Provider 费用真值 | 以后台账单为准 |
| Cron 表达式 | 固定 interval 已够 |
| `permission.mode=auto` | 预留；现等同 preauth |
| monorepo 全量 client DTO | alias 可接受 |
| TUI 与 tools 同进程 | 禁止 |
| yolo / per-tool 软权限 | 禁止 |

## 不建议引入

通用模块框架、Actor/MQ、ORM、DI 容器、工作流 DSL、跨 CLI/TUI/Gateway 的 Command Bus、恢复独立 Planner 审批轨、TUI 开 DB/exec CLI、本地 token→$ 定价表、未达标即宣称「OS sandbox」、跨会话云端向量记忆、多业务 SQLite。

## 常用命令

```bash
make check
make build
make all          # check + build + autozeagentd --check
go test ./... -count=1
```
