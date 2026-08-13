# YunmengZe Agent 当前状态

更新：2026-08-13（Hermes `skills_list`/`skill_view` + MCP 优先 · 基线 **v0.2.4**）

**本文件是唯一活着的优化/backlog 文档。** 只写未完成与暂缓项；已落地细节见 ADR（`docs/architecture/`）、changelog 与 git。

## 现状

生产形态稳定：`ymzd` + CLI·TUI（`ymz`）+ `core.db`。设计知识库：`docs/architecture/`。  
当前发布线：**v0.2.4**（v0.2.0 改名；v0.2.3 model 选择；v0.2.4 = O1–O4 + H7 + H1-lite + TUI Phase 2 气泡/SSE）。**未发版：** H5-lite + C4-skill / T8 + H3 / H4 / H5-skill / H6 + Hermes skill list/view + MCP 优先 + `AGENTS.md`。

| 对标 | 契约重叠（粗） | 说明 |
| --- | --- | --- |
| OpenCode 配置/协议 | ~80–90% | + `import-opencode`；stdio + 远程 MCP（O2） |
| OpenCode 产品手感 | ~80–90% | + skill-as-slash；Hermes `skills_list`/`skill_view`；`chat.commands`；`AGENTS.md` 规则；session prefer（O4） |
| OpenCode API/SDK | ~10–15% 路径类比 | 本地 `/v1/*` + Go `gatewayclient`；**不追**全量 OC OpenAPI |
| Crush TUI | 契约 ~95% | T1–T7 + C1–C4 + UX-A/B + T8 节流 live MD；无新 list 引擎 |
| Hermes 分层记忆 | 架构 ~90% | + H6；**H1-lite curator**；**H5-lite** `default_ttl` + 过期软归档（冻结块仍手动 refresh） |
| Hermes 自进化 | ~40% | H3 草稿+人工 apply；H4 习惯提示；H5-skill 软归档（ADR-050）；无自动 apply / yolo |
| Hermes 消息网关 | ~0% | 仅本机 UDS/loopback；**暂不上**飞书/微信（本机编码/定时为主） |

**产品焦点：** 本机编码 + 简单任务 + 定时任务。  
**下一优先：** 无强需求则先收敛（本波含 Hermes skill list/view + MCP 优先提示，未发版）。O5–O6 / H2 / M* **等用户再提**。

## 原则（不变）

- 三件套：daemon + CLI·TUI + `core.db`。不恢复 Module Runtime、多 DB、交互 **Planner**（plan-step 整单审批轨）。
- 工具副作用只经 Tool Broker；Policy → Approval → Capability Grant → containment → 限流 → Audit。
- Skill 仅指令文本，不扩大授权；`skill_ids` 仅显式预载（TUI/job）；模型经 `skills_list` → `skill_view` 按需加载（ADR-036）。用户规则：`<ConfigDir>/AGENTS.md` + 可选项目 `.yunmengze/AGENTS.md`。
- plan 永远只读；高风险工具仅 agent + `chat.tools` allowlist 预授权（ADR-038）；**tool-call** 交互 permission 见 ADR-043（≠ Planner）。
- 会话记忆为 in-process MemoryManager（ADR-044），非独立 Memory 进程。
- **客户端分层（ADR-018/022）：** 业务用例只在 daemon；Gateway 仅 HTTP 适配；CLI 与 TUI 经 `gatewayclient` 并列，TUI **不** exec CLI、**不** import tools/providers/agent。
- **消息通道（规划）：** 第二客户端 → `tasksubmission` / `taskcontrol`；**不**在 Gateway 内跑 tool/provider/grant。
- **Go 精神：** 具体类型 + 调用方小接口；composition root 在 `cmd/`；无 DI 容器 / ORM / 通用事件总线。

---

## 已关闭轨道（摘要）

### Crush TUI（T）— 契约完成

| ID | 项 | 状态 |
| --- | --- | --- |
| **T1–T7** | debounce / perm modal+grace / follow·freeze / loop+cancel / child usage / tool 分型 / 描述同址 | **已落地** |
| **T8** | Permission SSE + live 前缀缓存 + 完成态 glamour + 节流 live MD | **已落地**（未闭合围栏仍 plain） |

表现层只在 `internal/tui` 增量；不引入更重 list/viewport 依赖。  
**允许** Charm 生态小型本地库（纯 Go 进二进制）：`glamour`（完成态 + 节流 live MD）、`bubblezone`（鼠标 expand）、`bubbles/spinner`；**禁止** Crush lazy list / Ultraviolet。

### 日志（L）· 分发（D）

| ID | 项 | 状态 |
| --- | --- | --- |
| **L1** | 结构化日志 + `ymz logs` + ADR-047 | **已落地** |
| **D1** | GoReleaser → homebrew-tap / scoop-bucket | **已接线**（随 v0.2.x 发版） |
| — | 全链路 e2e / OTel；winget / npm | **不做** |

### 改名（R）— **已完成**

对外 **YunmengZe Agent**；可执行 `ymz` / `ymzd`；配置根 `~/.yunmengze`；module `github.com/yyZe0122/yunmengze-agent`；破坏性里程碑 **v0.2.0**，已发 **v0.2.4**。

| ID | Phase | 状态 |
| --- | --- | --- |
| **R0–R4** | 命名冻结 · 路径常量 · cmd · module · 打包文档 | **已落地** |
| **R5** | GitHub + 包仓改名 / remote / cask·scoop | **已完成** |
| **R6** | v0.2.0 改名发版 | **已完成**（后续 v0.2.1–v0.2.4） |

历史 changelog v0.1.x 可保留旧名作史料。**不做**旧 CLI/路径长期兼容。

### Wave A/B/C + Phase 1 OpenCode（O1–O4）— **已落地**

| ID | 项 | 状态 |
| --- | --- | --- |
| **O1** | `ymz config import-opencode` → `agent.local.json` + warnings | **已落地**（`internal/opencodeimport`） |
| **O2** | MCP remote：Streamable HTTP + legacy SSE；`type`/`url`/`headers` | **已落地**（`internal/mcp` Dial；ADR-040） |
| **O3** | skill-as-slash + `chat.commands` 模板 slash；import OC `command` | **已落地** |
| **O4** | session prefer + run-level resolve（prefer→main）；`modelresolve` | **已落地**（ADR-045） |
| **H6-min** | `injectscan`：memory 写入 + skill body + inject 路径 fail-closed | **已落地**（H6 完整化已并入） |
| **H5-lite** | memory `default_ttl` + 过期软归档 | **已落地**（未发版） |

---

## 有序 backlog（对标 OpenCode / Crush / Hermes）

依赖顺序：

```text
Phase 1：O1–O4 ✅ ──► O5–O6（用户再提）
Phase 2：C1–C4 + UX-A/B ✅ · T8 live MD ✅
Phase 3：H7 ✅ · H1-lite ✅ · H6 ✅ · H5-lite（memory）✅ · H3 ✅ · H4 ✅ · H5-skill ✅
H2 / O5–O6 / M* ── 用户再提
```

### Phase 1 — OpenCode 体验兼容（不追全量 API）

| ID | 项 | 目标 | 约束 / 验收 |
| --- | --- | --- | --- |
| **O1** | `ymz config import-opencode` | 映射 model/provider/mcp/commands/compaction | **已落地** |
| **O2** | MCP 远程（SSE/HTTP） | Streamable HTTP + legacy SSE；仍经 Broker | **已落地**（ADR-040 更新） |
| **O3** | 用户 slash 模板 | skill-as-slash + `chat.commands` | **已落地**（仅 instruction；不扩 grant） |
| **O4** | per-session model | prefer 存储 + run-level 解析 prefer→main | **已落地**（与 H7 共享 `modelresolve`） |
| **O5** | 薄 compat 子集（可选） | `/compat/opencode/*` 映射 session/message/compact/skills/health | 文档 **compat profile v0.1**；PTY/LSP → 501；**非** SDK drop-in |
| **O6** | 最小 OpenAPI + 可选 TS 客户端（可选） | 只覆盖真路径或 O5 | 不宣称全量 OC SDK |

### Phase 2 — Crush TUI 抛光

| ID | 项 | 目标 | 约束 / 验收 |
| --- | --- | --- | --- |
| **C1** | Permission SSE | poll → SSE | **已落地**：`permission.pending` / `permission.decided` 经 events store；TUI `applySSE` 触发 perm poll；decide 仍 `DecidePermission*` only |
| **C2** | live 前缀缓存 | streaming 卡顿时 | **已落地**：finished prefix cache + live tail re-render（plain blocks；非 glamour MD） |
| **C3** | 工具结果折叠增强 | 长输出可读 | **已落地**：thinking 框 + tail 窗；tool 卡片折叠；`/expand` · `e`/`E`/`c` |
| **C4** | journey 只读时间线 | memory + skill | **已落地**：`/journey` 叠 memory + `skill_events`；`/journey skills` / `/journey memory` |
| **T8** | live markdown | streaming reply 节流 glamour | **已落地**：防抖 ~200ms 或 +80 rune；未闭合围栏仍 plain；thinking/tool 仍 plain |
| **UX-A** | 信息架构 | thinking/reply/tool 分区 + 终态 | **已落地**：`contentBlock`；done 横幅；activity `thinking|writing|tool|idle` |
| **UX-B** | 气泡化 + 小库 | 圆角消息卡 / MD / 点击 | **已落地**：lipgloss 气泡；`glamour` 完成态 MD；`bubblezone` 点击 expand；`spinner` busy |

### Phase 3 — Hermes 记忆 / skill / 习惯（in-process）

| ID | 项 | 目标 | 约束 / 验收 |
| --- | --- | --- | --- |
| **H1** | LLM Memory Curator | turn 后 aux（`models.compact`/main）→ `memory_entries` | **H1-lite 已落地**（`CurateTurn`；不改冻结块；`chat.memory.curator`） |
| **H2** | write_approval | messaging/cron 来源的 memory/skill 写入先 stage | **等用户再提**（跟 M*） |
| **H3** | Skill 自改进草稿 | 工具写 ConfigDir/project `SKILL.md.draft` | **已落地**（ADR-050）：永不扩 grant；人工 apply + backup |
| **H4** | 工具习惯学习 | pending 只读 suggested once/similar | **已落地**：**非**自动 permanent；无 yolo；路径目录前缀 + 同会话 |
| **H5** | Curator 维护 | 过期 memory 软归档；未用 agent-skill archive | **H5-lite（memory）已落地**。**H5-skill 已落地**：`last_used` + `unused_ttl` 软归档（无 last-used 不自动归档；归档写 `skill_events`） |
| **H6** | 注入扫描 | memory/skill/draft 进 system 前扫 | **已落地**（更多 bidi/零宽/越狱标记） |
| **H7** | Job model pin | 创建时钉 model ref；空/失效 → skip+告警 | **已落地**（`jobs.model_ref` + `modelresolve` 严格解析） |

ADR：[050](../architecture/050-in-process-self-improvement.md)（≠ 独立 Evolution 进程）。

### Phase 4 — 消息通道（飞书 / 企微 / 微信）

形状：`internal/channel`（transport + 身份 + 限流）→ `tasksubmission` → chatsession → Broker → adapter 回消息。本地 Gateway 边界不变。

| ID | 项 | 目标 | 约束 / 验收 |
| --- | --- | --- | --- |
| **M0** | ADR-049 Channel Bridge | 架构 + 安全默认 | 默认 deny；session 映射在 `core.db` |
| **M1** | **飞书**（第一通道） | 优先出站 WebSocket（无需公网） | @提及 + open_id allowlist |
| **M2** | 企业微信 | 官方 bot WS / callback | 企业场景 |
| **M3** | 个人微信（可选） | iLink long-poll 类 | **DM 为主**；群弱；ToS/产品风险高 |
| **M4** | Pairing + allowlist | 与 M1 同发 | TUI/CLI 批准配对码；空名单=拒绝 |
| **M5** | 通道权限档 | 新远端默认 plan 或 ask | agent 写需显式 promote |
| **M6** | Cron 跨端投递 | job `deliver: feishu:chat_id` | delivery 仅 adapter；内容来自 completed run |
| **M7** | composition 接线 | `cmd/ymzd` 挂 adapter | Gateway **不**成为 tool 宿主 |

**安全默认：** 通道文本不可信；无 pairing 不跑；cron/messaging 高风险 fail-closed。

---

## 可选（非阻塞，未承诺）

| 项 | 说明 |
| --- | --- |
| **gatewayclient 薄 ops** | CLI↔TUI 组请求胶水痛时再抽；不新建第二控制面 |
| CLI skills | `run --skill` / `skills list`（主路径仍是 TUI `/skills`） |
| CJK FTS 扩展 | unicode61/trigram + LIKE 兜底；专用 C 扩展另议 |
| 更多 model roles | vision 等：有工具后再加 |
| 大文件同包再拆 | `kernel/repository`、`tools/fs`+`broker`、`tui/cmds`+`update`；`main.go` 仅摩擦大时再抽 `wire.go` |
| **O5–O6** | 仅有真实外部 OC 客户端绑定时（用户再提） |

```text
用例（daemon services）     → 已统一
外观（gatewayclient）       → 已共享
语法（CLI argv / TUI slash / HTTP）→ 故意分叉
通道（channel adapter）     → 规划中；与 gatewayclient 并列的 Core 客户端
```

## 暂缓 / 不做

| 项 | 说明 |
| --- | --- |
| 全量 OpenCode API/SDK 兼容 | ~162 路径 + 生成 SDK；与三件套冲突；只用 O5 子集或体验兼容 |
| 新 viewport/list 引擎 | 不引入 Crush-style lazy list / Ultraviolet（气泡 = lipgloss 自绘） |
| streaming 每 token 全量 glamour | 禁止；T8 必须节流 + 未闭合围栏 plain + 失败回退 C2 |
| Crush 三档 permission | 保持 once/similar/permanent/deny 四档 |
| 沙箱 phase-2+ | namespace / bubblewrap / seccomp |
| LSP | 另案 |
| Provider 费用真值 | 以后台账单为准 |
| Cron 表达式 | 固定 interval 已够（除非 M/cron 强需求再议） |
| `permission.mode=auto` | 预留；现等同 preauth |
| monorepo 全量 client DTO | alias 可接受 |
| TUI 与 tools 同进程 | 禁止 |
| yolo / per-tool 软权限 | 禁止 |
| 独立 Evolution / 多 DB / Module Runtime | 禁止 |
| 云端向量记忆默认 / Mem0 全家桶 | 禁止作默认路径 |
| 改名后旧路径/旧 CLI 长期兼容 | **不做** |
| winget / npm | **不做** |

## 不建议引入

通用模块框架、Actor/MQ、ORM、DI 容器、工作流 DSL、跨 CLI/TUI/Gateway 的 Command Bus、恢复独立 Planner 审批轨、TUI 开 DB/exec CLI、本地 token→$ 定价表、未达标即宣称「OS sandbox」、跨会话云端向量记忆、多业务 SQLite、Gateway 内执行 tool/provider。

## 常用命令

```bash
make check
make build
make all          # check + build + ymzd --check
go test ./... -count=1
```
