# ADR-045: Optional model roles (main / subagent / compact)

## Status

Accepted (2026-08-10)

## Context

Provider config already holds a multi-model catalog (`provider.*.models`) and a single active selection (`model`). All LLM work — main chat, `task` sub-agents, and session compaction — used that one runner endpoint. Operators want Hermes-style cheap/fast models for sub-agents and compaction without losing a strong main model.

Constraints (unchanged):

- One daemon, one `core.db`, Gateway does not call providers
- Sub-agents remain logical child runs (ADR-039)
- Compaction remains dual-track durable transcript (ADR-041)
- Fail closed on invalid config

## Decision

### Config

Optional top-level map:

```json
{
  "model": "provider/main-model",
  "models": {
    "subagent": "provider/worker-model",
    "compact": "provider/cheap-model"
  }
}
```

| Field | Meaning |
| --- | --- |
| `model` | **Main** model (required). TUI `/model` and `PUT /v1/config/model` only change this. |
| `models.subagent` | Optional. Used by `task` tool child runs. |
| `models.compact` | Optional. Used by `CompactSummary*` / mid-turn compact LLM path. |

Rules:

- Omit `models` or a key → that role uses main
- Empty string value → treat as unset (fallback main)
- Values must be `provider/model` present in the catalog
- Allowed keys only: `subagent`, `compact` (no `models.main`; unknown keys fail load)
- Changing `models.*` requires **daemon restart** (no hot rewrite). Main `model` / provider options may hot-reload (ADR-048); role endpoints are built only at start.

### Runtime

- Daemon builds optional role endpoints at startup (`ResolveModel` + `providers.NewConfigured` per distinct ref ≠ main)
- `agent.Runner` holds main provider/model plus `map[role]RoleEndpoint`
- `RunRequest.Role`: empty/`main` → main; `subagent` when set by `task` tool; unconfigured → main
- `CompactSummary*` always selects role `compact` (fallback main)
- `SetProvider` / `SetModel` / `SetContextWindow` update **main only** (aligned with `/model`)

### Out of scope

- Vision / image-gen / browser roles (no product tools yet)
- Per-call model in `task` tool input
- Gateway API for role map
- Hot-reload of `models.*`

## Consequences

- Operators can assign a cheaper model to compaction and sub-agents without switching the chat default mid-session via `/model`
- Main switch remains simple and persistent
- Future roles: add whitelist key + one call-site `Role` assignment + schema property

## References

- `internal/providerconfig` (`LoadModelRoles`, `validateModelsMap`)
- `internal/agent.Runner` (`Role`, `Roles`, `snapshotForRole`)
- `internal/tools/task.go` (`Role: "subagent"`)
- `cmd/ymzd` (`buildRoleEndpoints`)
- ADR-039, ADR-041
