# ADR-046: Session workspace + permission tiers

## Status

Accepted (implemented 2026-08-10)

## Context

Auto-started `autozeagentd` used `DataDir` as cwd; empty `chat.roots` made tools see DataDir, not the directory where the user ran `aze`. Interactive permissions (ADR-043) only offered once/session/deny, with no permanent trust table.

## Decision

### Session workspace

1. Client (TUI/CLI) sends absolute `workspace` on task submit (= `os.Getwd()`).
2. Daemon stores it in `sessions.metadata.workspace` when ensuring the session.
3. Chat plan Paths / grants use **session workspace** (plus configured `chat.roots` / `chat.workspace.allow`).
4. Shared `PathGuard` starts with config ceiling; `AddRoot(session workspace)` on chat auth.
5. `chat.workspace.allow_all=true` disables path-root containment (local single-user only; audited).

Config (optional; defaults preserve client_cwd behavior when roots empty):

```json
"chat": {
  "workspace": {
    "default": "client_cwd",
    "allow": [],
    "allow_all": false
  },
  "roots": [],
  "permission": { "mode": "ask" }
}
```

Legacy `chat.roots` remains valid as extra/fixed roots.

### Permission decisions (ADR-043 extension)

| Decision | Meaning |
| --- | --- |
| `allow_once` | Single tool call (existing) |
| `allow_similar` | Session-scoped: same capability + path parent prefix (and exact command/args for process) |
| `allow_permanent` | Requires `confirm: true`; writes ConfigDir trust entry; future matches pre-grant |
| `deny` | Existing |

Jobs/cron still never wait.

## Consequences

- Multi-project use of one daemon is correct when each session carries its launch cwd.
- PathGuard and grants stay aligned via shared expandable roots.
- Permanent trust is explicit and confirm-gated.

## References

- ADR-038, ADR-043, ADR-011, ADR-012, ADR-037
