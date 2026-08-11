# 047 — Structured logging and debug chain

## Status

Accepted (2026-08-11).

## Context

AutoZeAgent is a single long-running daemon. Operators and developers validate orchestration, provider behaviour, and TUI UX primarily on a real machine. Unit tests remain the CI guard for pure logic, security boundaries, and package architecture. A heavy full-stack test harness is not the preferred integration path.

Logging already uses `log/slog` JSON to `autozeagentd.jsonl` with rotation and secret redaction. Gaps remained on the chat write path (gateway / tasksubmission / chatsession start and success) and CLI filters (`session` / `task`).

## Decision

1. **Structured log chain** is the primary integration observability surface for daemon-side chat flows.
2. **Standard fields** on stage-boundary lines:

   | Field | When |
   | --- | --- |
   | `component` | always |
   | `operation` | always |
   | `result` | always: `started` \| `succeeded` \| `failed` \| `denied` \| `cancelled` \| `paused` \| `pending` \| `warning` |
   | `session_id` | session-scoped work |
   | `task_id` | task-scoped work |
   | `run_id` | run-scoped work (primary filter key) |
   | `plan_id` / `step_id` | when known |
   | `trace_id` | run path; **default equals `run_id` after chat run create** |
   | `duration_ms` | finished stages when cheap |
   | `error` | failed / denied |

3. **Component names** (stable): `daemon`, `gateway`, `tasksubmission`, `chatsession`, `agent`, `provider`, `tool_broker`, `toolpermission`, `taskcontrol`, `scheduledtasks`, `mcp`, `memory`, `skills`.
4. **Three channels stay distinct**:
   - **JSONL logs** — runtime diagnosis (`aze logs`)
   - **`audit_log`** — durable security / execution decisions
   - **events / SSE / modelstream** — client UX, not a substitute for logs
5. **Redaction** remains in daemon `replaceLogAttr` (API keys, bodies, prompts, arguments, etc.). Do not log tool argument payloads or model prompt/response text.
6. **Thin helper** `internal/runlog.Attrs` may build common key/value pairs; call sites still use `slog` directly. No DI logger, no OpenTelemetry, no global event bus.
7. **CLI**: `aze logs --run` / `--session` / `--task` / `--component` / `--level` / `--tail`.
8. **Tests vs logs**:
   - Keep package tests for policy, path containment, SSRF, grant atomicity, architecture import guards, critical chatsession state machines.
   - Do **not** build a full daemon→gateway→TUI e2e harness as the default integration strategy.
   - Optional live provider: `//go:build e2e` only.

## Consequences

- A single chat turn should be reconstructible with `aze logs --run <run_id>` across gateway → tasksubmission → chatsession → agent → tool_broker → terminal chatsession line.
- Log volume stays bounded by logging **stage boundaries**, not every internal branch.
- Contributors add logs when introducing new write-path stages; field names must match this ADR.

## Real-machine debug recipe

```bash
export AUTOZEAGENT_LOG_LEVEL=debug   # optional
# TUI or CLI: send a message; note run_id from response / TUI metrics
aze logs --run <run_id> --tail 500
aze logs --session <session_id> --tail 500
aze logs --task <task_id> --component tool_broker
```

Checklist: empty model reply, tool deny, permission ask, `/compact`, pause/cancel, scheduled job fire.
