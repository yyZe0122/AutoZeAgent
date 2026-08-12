# AGENTS.md

Local-first Go automation agent. Module: `github.com/yyZe0122/yunmengze-agent`. Go **1.26+**.

## Production shape

Only three production pieces:

- `ymzd` — long-running daemon (composition root: `cmd/ymzd`)
- `ymz` — local CLI client of the gateway (`cmd/ymz`); no-arg and `tui` open the TUI; TUI/`run` ensure a unique daemon via `internal/daemonctl` (`ymz start|stop|restart|status` or `ymz daemon …`)
- `core.db` — single SQLite source of truth (`modernc.org/sqlite`, pure Go; builds use `CGO_ENABLED=0`)

Do **not** restore deleted architecture: Module Runtime/Supervisor, out-of-process Memory/Scheduler/Evolution/Echo, `skills.db` / `scheduler.db`, `/v1/modules`, multi-DB, ORM, container DI, or generic event-bus frameworks.

## Commands

| Goal | Linux/macOS | Windows |
| --- | --- | --- |
| Format | `make format` | `.\scripts\dev.ps1 -Action format` |
| Check (fmt + vet + test [+ systemd unit]) | `make check` | `.\scripts\dev.ps1 -Action check` |
| Build → `bin/` | `make build` | `.\scripts\dev.ps1 -Action build` |
| Install to PATH | `make install` → `~/.local/bin` | `.\scripts\dev.ps1 -Action install` |
| check + build + daemon `--check` | `make all` | `.\scripts\dev.ps1 -Action all` |
| Uninstall from BINDIR | `make uninstall` | `.\scripts\dev.ps1 -Action uninstall` |
| Clean `bin/` `dist/` + go cache | `make clean` | — |

```bash
go test ./... -count=1
go test ./internal/<pkg>/ -count=1
go test ./internal/<pkg>/ -run TestName -count=1
```

Dependency edits: `go mod tidy && go mod verify` and keep `go.mod`/`go.sum` clean (CI fails on drift).

Local release matrix: `goreleaser release --snapshot --clean --parallelism 1`.

**Publish (root only on this host):** write `docs/changelog/vX.Y.Z.md` first, then  
`sudo -i && cd /home/yyze/projects/AutoZeAgent && ./scripts/publish-release.sh vX.Y.Z --commit-paths all --yes`  
Full runbook: [`docs/release.md`](docs/release.md) — do not invent parallel release steps.

`make check` on Linux also runs `scripts/check-systemd.sh` (no-ops on non-Linux / missing `systemd-analyze`).

## Layout that matters

| Path | Role |
| --- | --- |
| `cmd/ymzd` | Wires SQLite, kernel, chatsession, agent, tools, MCP, gateway, taskcontrol, scheduledtasks |
| `cmd/ymz` | Gateway client only — no tools, no provider, no grants; no-arg/`tui` → `internal/tui` |
| `internal/gateway` | Local HTTP/UDS server only (`api.go` + `handlers_*.go` + LocalRunner) |
| `internal/gatewayclient` | Shared CLI+TUI facade: HTTP/SSE transport + typed helpers (no import of gateway server) |
| `internal/tui` | Bubble Tea UI (primary UX); Gateway-only; **bubbles** (viewport/textinput/spinner) + **lipgloss** cards; optional **glamour** (completed MD), **bubblezone** (click expand); no list/viewport engine swap |
| `internal/chatsession` | Multi-turn chat for agent (build/write) and plan (read-only); workspace grants by mode |
| `internal/providerruntime` | Main provider load, `/model`, hot-reload (ADR-048) |
| `internal/configreload` | ConfigDir fsnotify debounce for provider files |
| `internal/contextpack` | Provider-view token estimate/pack/compact; snapshots (ADR-041) |
| `internal/memory` | In-process MemoryManager + `memory_entries` (ADR-044) |
| `internal/injectscan` | Fail-closed scan before memory/skill system inject (H6-min) |
| `internal/opencodeimport` | Map OpenCode `opencode.json` → `agent.local.json` (CLI import) |
| `internal/scheduler` + `scheduledtasks` | In-process job store + chat-native fire → tasksubmission (ADR-017/042) |
| `internal/runlog` | Shared slog field helpers for the daemon log chain (ADR-047) |
| `internal/*` | Domain + app services (not a monorepo) |
| `pkg/{event,provider,scheduler,tool}api` | Stable cross-boundary contracts |
| `migrations/core/*.sql` | Embedded ordered migrations (`embed.go`); applied in `internal/store/sqlite.Open` |
| `docs/architecture/` | Numbered ADRs — design KB; start at `docs/architecture/README.md` |
| `docs/optimization/current.md` | **Only** living optimization doc (backlog) |

### Wiring rules

- **Writes** go through narrow services: `tasksubmission` (submit)、`chatsession` (chat runs)、`taskcontrol` (pause/resume/cancel). **Reads** go through `corequery`. Gateway must not hold a business `*sql.DB`.
- Model-requested effects go **only** through Tool Broker (`internal/tools`, `RegisterBuiltins`). Nested `internal/tools/internal/executor` is intentionally unimportable outside `tools`.
- Scheduler is **in-process** on Core’s shared `*sql.DB` — no separate scheduler DB or process. Jobs are **chat-native** (ADR-042): due fire → `tasksubmission` → `chatsession`.
- Skills are file-based: `<config_dir>/skills` and `.yunmengze/skills` as `<id>/SKILL.md`. Instruction text only — never approvals, grants, or policy expansion. TUI: `/skills` multi-select or skill-as-slash `/<skill-id>`. Optional `chat.commands` templates (O3): `/<cmd> [args]` expands `$ARGUMENTS` into a user message only. Slash priority: built-in → `chat.commands` → skill id.
- Sub-agents (if any) = logical child Runs with `parent_run_id` + `task` tool, not new processes/modules (ADR-039).
- CLI and TUI are **peers**: both use `gatewayclient`; TUI does **not** shell out to CLI subcommands. **TUI is the primary UX**; CLI is secondary (scripts/automation).

## Hard constraints

- Preserve Policy → Approval → Capability Grant → path containment → timeout/output limits → Audit on every tool path. Fail closed.
- Gateway does not execute tools, call providers, or issue grants.
- Migration filenames are lexicographically ordered and **immutable after release** (keep history even when later migrations drop tables).
- Persist/serve times as UTC RFC3339Nano; convert for display only at CLI/UI edges.
- Never commit secrets, `agent.local.json`, `*.db`, logs, sockets, or `bin/`/`dist/` output.
- Prefer concrete types, small interfaces at call sites, explicit errors, and `context.Context` on cancellable work. No hidden global orchestration.

## Config / runtime

- Modes: `--mode user|system` (OS-specific dirs; Linux user uses XDG).
- Provider config is **ConfigDir only** (not project cwd): user mode is flat **`~/.yunmengze`** (all OS; override `YMZ_HOME`); system mode keeps OS system paths. Files: `agent.local.json` then `agent.json`. Daemon `EnsureConfig` migrates from project/data dir once if ConfigDir is empty, else seeds a template. Prefer `{env:VAR}` / file refs over literal API keys.
- **Hot-reload (ADR-048):** while `ymzd` runs, edits to `agent.json` / `agent.local.json` / `env` rebuild the **main** provider stack (~0.5s debounce; `internal/providerruntime` + `configreload`). Fingerprint hashes API key/headers (no secrets in logs). Process non-empty env still wins over `env` file. **Not** reloaded: `chat.*`, MCP, role map. If daemon started without agent/chat, fix config then **`ymz restart`** (no late-bind).
- Per-model options: `maxTokens` = output cap; optional `contextWindow` = model context length for **packing + UI pressure** (not the same field as `maxTokens`). See `docs/provider-protocols.md`, ADR-041.
- Provider selection is OpenCode-style: top-level `model` = `providerID/modelID…` (first `/` only; model segment may contain `/`). Catalog keys under `provider.<id>.models` match that segment; optional `models.<key>.id` is the wire/API id. Agent requests use the wire id, not a rewritten bare suffix.
- Optional **`models`** role map (ADR-045): `models.subagent` / `models.compact` as selection refs; omit → top-level `model` (main). `/model` only changes **global** main; role map changes need daemon restart. Unknown keys fail load.
- Session **model preference** (O4): `sessions.metadata.model` via `PATCH /v1/sessions/{id}` `{preferred_model}`; TUI `/model prefer [ref]` stores preference (no global switch); `/model provider/model` still switches global main (and may also record preference when a session is focused). Chat runs resolve **job pin (H7) → prefer → main** (`internal/modelresolve`); invalid prefer falls back to main; invalid job pin fails start. Configured subagent/compact roles still win on those call sites.
- Optional **`chat`** in the same JSON: `workspace` (`default` client_cwd\|daemon_cwd\|abs path; `allow[]`; `allow_all`; ADR-046), legacy `roots` (merged into ceiling), `allow_write` (optional ceiling for **agent** writes; omit = true), `tools.git` / `tools.process` (agent-only high-risk grants; default false), `compaction.enabled` (default true), `max_iterations` (1–64, default 16), `permission.mode` (`preauth` default \| `ask` \| `auto` reserved; ADR-043), `memory` (enabled/max_inject_runes/session_search; optional `curator` H1-lite; ADR-044), `commands` (O3 slash templates: map id → `{description, template}` with `$ARGUMENTS`; injectscan at load; no grants). Session workspace = client launch cwd on submit. Plan mode is always read-only. Manual compact: TUI `/compact [focus]`. Tool permission: TUI `/perm` once\|similar\|permanent\|deny (pending also via SSE `permission.*` + poll). Layered memory: TUI `/memory` `/refresh-memory` `/journey` (curator writes do not auto-refresh frozen inject). Foldable timeline: `/expand` · `e`/`E`/`c`. Inject path uses `injectscan` (fail-closed). See ADR-038, ADR-041, ADR-043, ADR-044, ADR-046.
- `ymz config validate` checks path layout + provider load/resolve + chat structure; never prints secrets.
- `ymz config import-opencode [path]` maps OpenCode config → `agent.local.json` (model/provider, stdio+remote MCP, `command`→`chat.commands`, compaction; drops plugins/LSP/oauth with warnings). Default path: `~/.config/opencode/opencode.json`.
- Daemon owns `core.db` lifecycle; components must not close the shared connection.
- Dual-track tasks (OpenCode-style): both modes → `chatsession`; **agent** = write grants, **plan** = read-only. No interactive Planner/approval path. TUI Tab switches draft mode.
- Scheduled jobs: fixed interval; default `execution_mode=agent`; **H7** pins `model_ref` at create (empty → current main); fire with empty/unresolvable pin → skip+fail ACK. Create via TUI `/cron [every objective]` or CLI `job create [--model]` (secondary). See ADR-042.
- Daemon structured logs: JSONL (`ymzd.jsonl`); stage fields `component` / `operation` / `result` + `session_id` / `task_id` / `run_id` / `trace_id` (ADR-047). Filter: `ymz logs --run|--session|--task|--component|--level`. Level: `YMZ_LOG_LEVEL`. Integration debug = real machine + logs; keep package tests for safety/architecture (no full e2e harness).

## Deep dives

Index: `docs/architecture/README.md`. Start with: `001-core-boundaries`, `004-database-ownership`, `012-tool-broker-execution-boundary`, `018-local-gateway-boundary`, `037-cli-daemon-lifecycle`, `038-session-chat-boundary`, `039-logical-child-runs`, `040-mcp-tool-broker`, `041-context-packing-and-pressure`, `042-chat-native-jobs`, `043-tool-call-permission-interaction`, `044-in-process-memory-boundary`, `045-model-roles`, `046-session-workspace-and-permission-tiers`, `047-structured-logging-and-debug-chain`, `048-provider-config-hot-reload`, `022-application-query-boundaries`. Status / backlog: `docs/optimization/current.md`. PR norms: `CONTRIBUTING.md`.
