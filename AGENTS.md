# AGENTS.md

Local-first Go automation agent. Module: `autozeagent.local/autozeagent`. Go **1.26+**.

## Production shape

Only three production pieces:

- `autozeagentd` — long-running daemon (composition root: `cmd/autozeagentd`)
- `autozeagent` / `aze` — local CLI client of the gateway (`cmd/autozeagent`); no-arg and `aze` open the TUI; TUI/`run` ensure a unique daemon via `internal/daemonctl` (stop only via `daemon stop`)
- `core.db` — single SQLite source of truth (`modernc.org/sqlite`, pure Go; builds use `CGO_ENABLED=0`)

Do **not** restore deleted architecture: Module Runtime/Supervisor, out-of-process Memory/Scheduler/Evolution/Echo, `skills.db` / `scheduler.db`, `/v1/modules`, multi-DB, ORM, container DI, or generic event-bus frameworks.

## Commands

| Goal | Linux/macOS | Windows |
| --- | --- | --- |
| Format | `make format` | `.\scripts\dev.ps1 -Action format` |
| Check (fmt + vet + test [+ systemd unit]) | `make check` | `.\scripts\dev.ps1 -Action check` |
| Build → `bin/` | `make build` | `.\scripts\dev.ps1 -Action build` |
| Install to PATH | `make install` → `~/.local/bin` | `.\scripts\dev.ps1 -Action install` |
| check + build | `make all` | `.\scripts\dev.ps1 -Action all` (also runs isolated `autozeagentd --check`) |

```bash
go test ./... -count=1
go test ./internal/<pkg>/ -count=1
go test ./internal/<pkg>/ -run TestName -count=1
```

Dependency edits: `go mod tidy && go mod verify` and keep `go.mod`/`go.sum` clean (CI fails on drift).

Local release matrix: `goreleaser release --snapshot --clean --parallelism 1`.

`make check` on Linux also runs `scripts/check-systemd.sh` (no-ops on non-Linux / missing `systemd-analyze`).

## Layout that matters

| Path | Role |
| --- | --- |
| `cmd/autozeagentd` | Wires SQLite, kernel, planner, agent, tools, gateway, scheduler heartbeat |
| `cmd/autozeagent` | Gateway client only — no tools, no provider, no grants; no-arg/`tui` → `internal/tui` |
| `internal/gatewayclient` | Shared CLI+TUI facade over Gateway HTTP/SSE |
| `internal/tui` | Bubble Tea UI; Gateway-only (target: only import `gatewayclient` + paths + pkg) |
| `internal/*` | Domain + app services (not a monorepo) |
| `pkg/{event,provider,scheduler,tool}api` | Stable cross-boundary contracts |
| `migrations/core/*.sql` | Embedded ordered migrations (`embed.go`); applied in `internal/store/sqlite.Open` |
| `docs/architecture/` | Numbered ADRs — project design knowledge base |
| `docs/optimization/current.md` | **Only** living optimization doc (target shape, backlog §5.1) |

### Wiring rules

- **Writes** go through narrow services: `tasksubmission`, `approvalsubmission`, `runexecution`. **Reads** go through `corequery`. Gateway must not hold a business `*sql.DB`.
- Model-requested effects go **only** through Tool Broker (`internal/tools`, `RegisterBuiltins`). Nested `internal/tools/internal/executor` is intentionally unimportable outside `tools`.
- Scheduler is **in-process** on Core’s shared `*sql.DB` — no separate scheduler DB or process.
- Skills are file-based: `<config_dir>/skills` and `.autozeagent/skills` as `<id>/SKILL.md`. Instruction text only — never approvals, grants, or policy expansion.
- Sub-agents (if any) = logical child Runs with `parent_run_id`, not new processes/modules.
- CLI and TUI are **peers**: both use `gatewayclient`; TUI does **not** shell out to CLI subcommands.

## Hard constraints

- Preserve Policy → Approval → Capability Grant → path containment → timeout/output limits → Audit on every tool path. Fail closed.
- Gateway does not execute tools, call providers, or issue grants.
- Migration filenames are lexicographically ordered and **immutable after release** (keep history even when later migrations drop tables).
- Persist/serve times as UTC RFC3339Nano; convert for display only at CLI/UI edges.
- Never commit secrets, `autozeagent.local.json`, `*.db`, logs, sockets, or `bin/`/`dist/` output.
- Prefer concrete types, small interfaces at call sites, explicit errors, and `context.Context` on cancellable work. No hidden global orchestration.

## Config / runtime

- Modes: `--mode user|system` (OS-specific dirs; Linux user uses XDG).
- Provider config is **ConfigDir only** (not project cwd): `autozeagent.local.json` then `autozeagent.json` under user/system config dir (Linux `~/.config/autozeagent`, Windows `%APPDATA%\AutoZeAgent`). Daemon `EnsureConfig` migrates from project/data dir once if ConfigDir is empty, else seeds a template. Prefer `{env:VAR}` / file refs over literal API keys.
- Daemon owns `core.db` lifecycle; components must not close the shared connection.

## Deep dives

Start with: `docs/architecture/001-core-boundaries.md`, `004-database-ownership.md`, `012-tool-broker-execution-boundary.md`, `018-local-gateway-boundary.md`, `022-application-query-boundaries.md`, `037-cli-daemon-lifecycle.md`. Optimization backlog: `docs/optimization/current.md`. PR norms: `CONTRIBUTING.md`.
