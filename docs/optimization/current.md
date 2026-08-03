# AutoZeAgent 当前优化状态

更新：2026-08-03

**本文件是唯一活着的优化/backlog 文档。** 只写现状与未完成项；已落地细节见 ADR（`docs/architecture/`）与代码，不在此堆历史验收清单。

## 0. 路线图

| 阶段 | 目标 | 状态 |
| --- | --- | --- |
| **P1–P4** | 质量、子 Run、MCP、上下文、Skills、高风险工具、卫生 | **已完成**（见 ADR / 代码） |
| **P5a** | Chat 原生 Job/cron（经 chatsession 提交；非 plan-step worker） | **已完成**（ADR-042） |
| **P5b** | Linux process isolation baseline（systemd/cgroup，`process_exec`） | **下一步** |
| 暂缓 | LSP；本地费用定价；沙箱 phase-2+（ns/bwrap/seccomp） | 不在范围 |

### 原则（不变）

- 三件套：`autozeagentd` / CLI·TUI / `core.db`。不恢复 Module Runtime、多 DB、交互 Planner。
- 工具副作用只经 Tool Broker；Policy → Approval → Grant → containment → 限流 → Audit。
- Skill 仅指令文本，不扩大授权；`skill_ids` 显式选择（ADR-036）。
- plan 永远只读；高风险工具仅 agent + `chat.tools` allowlist 预授权（ADR-038）。

### P5 原则

- **P5a（已完成）**：Job payload = 定时提交 session chat（agent/plan）；复用 job 表与 in-process scheduler（ADR-017/042）；**TUI `/cron` 为主**，CLI 次要。
- **P5b**：对齐 `docs/security/linux-sandbox-roadmap.md` 第一阶段；无 systemd 须显式降级 + Audit；未达标前只称 **process isolation baseline**。
- 依赖：打开 `chat.tools.process` 后 P5b 价值最大。

## 1. 生产形态

```text
autozeagent CLI ──┐
autozeagent TUI ──┼──► gatewayclient ──► 本地 Gateway HTTP/SSE
                  └─────────────────────► autozeagentd
                                              │
                                    Session / Task / Run
                                              │
                                       chatsession (双轨)
                                              │
                                           Tool Broker
                                              │
                                    Approval / Capability Grant
                                              │
                                           core.db
```

生产只含：`autozeagentd`、`autozeagent`（含 TUI）、`core.db`。  
边界：ADR-018、037、038；子 Run 039；MCP 040；上下文 041；Skill 快照 036；Job 042。

| Tab | Grant |
| --- | --- |
| **agent** | 读 + 写（`chat.allow_write`）+ `task` + MCP；git/process 经 `chat.tools` opt-in（默认关） |
| **plan** | 只读 + `task` + MCP |

## 2. 安全边界（必须保留）

- Tool Broker 唯一副作用入口
- Policy → Approval → Capability Grant → path containment → timeout/output 上限 → Audit
- Provider Secret Resolver 与日志脱敏
- 单 `core.db`；migration 文件不可变
- Skill 仅指令文本，不扩大授权

## 3. 不恢复

Module Runtime/Supervisor、独立 Memory/Scheduler/Evolution 进程、多 DB、`/v1/modules`、交互 Planner 审批轨、plan-step Start/worker、旧 `requires_plan` Job 语义。

## 4. Backlog（仅未完成）

### 4.1 P5b — process isolation baseline（下一步）

| 项 | 说明 / 入口 |
| --- | --- |
| 实现 | systemd transient scope + cgroups v2；`internal/tools/internal/executor` |
| 降级 | 无 systemd 显式降级 + Audit，不静默宣称已限制 |
| 文档/UI | 只称 process isolation baseline；见 `docs/security/linux-sandbox-roadmap.md` |

### 4.2 可选小尾巴（不挡 P5b）

| 项 | 说明 |
| --- | --- |
| CLI skills | `run --skill` / `skills list`（P4.2 可选补全；主路径仍是 TUI `/skills`） |
| 子 Run badge | `parent_run_id` 进 corequery + 时间线标题后缀（ADR-039） |
| `http_get` SSRF 基线 | 私网/metadata 拒绝；仍不 chat 预授权 |
| Job skill 在 TUI create | `/cron` create 已跟 draft skills；CLI `--skill` 已支持 |

### 4.3 暂缓

| 项 | 说明 |
| --- | --- |
| Linux 主机验收矩阵 | 包装已有；完整矩阵另议 |
| LSP | 另案 |
| Provider 费用真值 | 以后台账单为准 |
| 沙箱 phase-2+ | namespace / bubblewrap / seccomp |
| Cron 表达式 | 固定 interval 已够；表达式待用户故事 |

### 4.4 不建议引入

通用模块框架、Actor/MQ、ORM、DI 容器、工作流 DSL、恢复独立 Planner 审批轨、TUI 开 DB/exec CLI、本地 token→$ 定价表、未达标即宣称「OS sandbox」。

## 5. 常用命令

```bash
make check
make build
go test ./... -count=1
AUTOZEAGENT_E2E_PROVIDER=1 go test -tags e2e ./internal/agent/ -run TestE2EProviderAgentRun
```

(End of file)
