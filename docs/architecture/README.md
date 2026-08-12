# Architecture ADRs

Numbered decision records for YunmengZe. This directory is the project **design knowledge base**.

Living status / backlog (only): [`docs/optimization/current.md`](../optimization/current.md).  
Agent/contributor entry: [`AGENTS.md`](../../AGENTS.md).

## Production shape

```text
ymzd   long-running daemon (composition root)
ymz    local CLI + TUI (Gateway clients only)
core.db        single SQLite source of truth
```

Do **not** restore deleted pieces: Module Runtime/Supervisor, out-of-process Memory/Scheduler/Evolution, multi-DB, `/v1/modules`, ORM, container DI, interactive Planner / plan-step Start.

## Start here

| Order | ADR | Topic |
| --- | --- | --- |
| 1 | [001](001-core-boundaries.md) | Core process and package boundaries |
| 2 | [004](004-database-ownership.md) | `core.db` ownership and migrations |
| 3 | [012](012-tool-broker-execution-boundary.md) | Tool Broker is the only effect path |
| 4 | [018](018-local-gateway-boundary.md) | Gateway + CLI/TUI client boundary |
| 5 | [037](037-cli-daemon-lifecycle.md) | Daemon ensure / stop semantics |
| 6 | [038](038-session-chat-boundary.md) | OpenCode-style agent build / plan RO chat |
| 7 | [022](022-application-query-boundaries.md) | Writes via services, reads via corequery |
| 8 | [039](039-logical-child-runs.md) | Logical child Runs (`task` tool, `parent_run_id`) |
| 9 | [040](040-mcp-tool-broker.md) | MCP stdio via Tool Broker only |
| 10 | [041](041-context-packing-and-pressure.md) | Provider-view packing, compaction, context API |
| 11 | [042](042-chat-native-jobs.md) | Chat-native Job/cron (timed chatsession submit) |
| 12 | [043](043-tool-call-permission-interaction.md) | Crush-style tool-call permission (≠ Planner) |
| 13 | [044](044-in-process-memory-boundary.md) | In-process MemoryManager (Hermes lifecycle) |
| 14 | [045](045-model-roles.md) | Optional model roles (main / subagent / compact) |
| 15 | [046](046-session-workspace-and-permission-tiers.md) | Session workspace + permission tiers |
| 16 | [047](047-structured-logging-and-debug-chain.md) | Structured logs + real-machine debug chain |
| 17 | [048](048-provider-config-hot-reload.md) | Provider config watch + main-stack hot-reload |

Also useful: [003](003-policy-invariants.md) policy, [011](011-approval-capability-binding.md) grants (domain), [013](013-provider-planner-boundary.md) provider boundary (**interactive Planner superseded**), [017](017-scheduler-module-boundary.md) in-process scheduler (+ [042](042-chat-native-jobs.md) product semantics), [034](034-file-based-skills-boundary.md) skills, [035](035-standard-protocol-tool-boundary.md) protocol tools.

Provider wire formats: [`docs/provider-protocols.md`](../provider-protocols.md).

## Index (by number)

| ADR | Title | Notes |
| --- | --- | --- |
| 001 | Core boundaries | Production three-piece shape |
| 003 | Policy invariants | Fail closed; grants bind plan hash |
| 004 | Database ownership | Single `core.db` |
| 006 | Linux runtime | XDG / system paths |
| 007 | Cross-platform abstraction | `internal/platform` |
| 008 | Threat model | Current host/agent threats |
| 009 | Event schema evolution | |
| 010 | Kernel state consistency | Optimistic concurrency |
| 011 | Approval / capability binding | |
| 012 | Tool Broker execution boundary | Nested executor unimportable |
| 013 | Provider / planner boundary | **Superseded** interactive path → 038; provider rules remain |
| 016 | Skills module boundary | **Deprecated** → 034 |
| 017 | Scheduler boundary | In-process on `core.db`; product = 042 |
| 018 | Local gateway boundary | CLI ‖ TUI via gatewayclient; TUI primary |
| 022 | Application query boundaries | Task submit + corequery (parts historical) |
| 023 | Approval decision application boundary | **Superseded** interactive path → 038 |
| 024 | Initial planning recovery | **Superseded** → 038 |
| 026 | Application error classification | |
| 027 | Query pagination / sorting / filtering | |
| 028 | Core identity types | |
| 029 | UTC time storage | |
| 030 | Agent run record recovery | |
| 031 | Unified provider streaming event | |
| 032 | Approval interaction semantics | **Superseded** interactive path → 038 |
| 033 | Target platform behavior | |
| 034 | File-based skills | Replaces 016 |
| 035 | Standard protocol tool boundary | |
| 036 | Task skill snapshot | Explicit `skill_ids`; chatsession injects snapshot |
| 037 | CLI / daemon lifecycle | |
| 038 | Session chat boundary | Dual-track agent/plan; optional `chat.tools` |
| 039 | Logical child runs | `parent_run_id` + `task` tool (sync) |
| 040 | MCP via Tool Broker | stdio client; no Module Runtime |
| 041 | Context packing / pressure | Token budget pack, compaction triggers, anti-thrash, `/compact` |
| 042 | Chat-native jobs | Timed session chat submit; lease from 017; TUI `/cron` primary |
| 043 | Tool-call permission interaction | `chat.permission.mode`; pending → TUI decide → scoped grant |
| 044 | In-process memory boundary | Layered L0–L3 memory; freeze inject; FTS; `/memory`; no Module Runtime |
| 045 | Model roles | Optional `models.subagent` / `models.compact`; fallback to top-level `model` |
| 046 | Session workspace + permission tiers | Client cwd session root; once/similar/permanent/deny |
| 047 | Structured logging / debug chain | slog JSON stage boundaries; `ymz logs`; tests vs real-machine |
| 048 | Provider config hot-reload | Main stack only; no late-bind chat |

Missing numbers (002, 005, 014–015, 019–021, 025, …) are **historical gaps**, not missing files to recreate.

## Conventions

- Filenames: `NNN-kebab-title.md`, lexicographic order.
- Significant architecture changes get a new ADR (see `CONTRIBUTING.md`).
- Do not invent parallel optimization notes; update `docs/optimization/current.md` only.
