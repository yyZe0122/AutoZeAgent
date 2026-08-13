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

### Session model preference + run-level resolve (O4)

Separate from role map: session row `metadata.model` (JSON) holds an optional **preference** string `provider/model`.

| Surface | Behavior |
| --- | --- |
| `PATCH /v1/sessions/{id}` `{ "preferred_model": "…" }` | Merge into metadata (preserves `workspace`); empty string clears |
| TUI `/model prefer [ref]` | Preference only — does **not** call `SelectModel` / rewrite config |
| TUI `/model provider/model` | Still **global** main only (ADR-048 hot path); may also record preference when a session is focused |
| Chat / agent run | **Resolve order:** job `model_ref` (H7, strict) → session prefer (if resolvable) → daemon main. Does **not** rewrite global config. Invalid session prefer → log + fall back to main; invalid job pin → fail start (no run) |
| Configured `Role` subagent/compact | Still wins over session prefer / job pin on those call sites (compact/subagent) |

Implementation: `internal/modelresolve` + `agent.RunRequest` override fields + `chatsession` (job pin + `PreferredModel`) before `agent.Run`.

Not a substitute for `models.subagent` / `models.compact`. H7 job model pin: `jobs.model_ref` + `schedulerapi` + `scheduledtasks` + same resolver (`ResolveStrict`).

### Out of scope

- Vision / image-gen / browser roles (no product tools yet)
- Per-call model in `task` tool input
- Gateway API for role map
- Hot-reload of `models.*`
- Concurrent multi-model UI beyond one session prefer + global main

## Consequences

- Operators can assign a cheaper model to compaction and sub-agents without switching the chat default mid-session via `/model`
- Main switch remains simple and persistent
- Session prefer applies per chat run without mutating global main
- Future roles: add whitelist key + one call-site `Role` assignment + schema property
- H7 pins jobs via `jobs.model_ref` and the same `modelresolve` path (strict on fire)

## References

- `internal/providerconfig` (`LoadModelRoles`, `validateModelsMap`)
- `internal/agent.Runner` (`Role`, `Roles`, `snapshotForRole`, model override, `ProposeMemoryFacts`)
- `internal/modelresolve`
- `internal/scheduler` / `pkg/schedulerapi` (`model_ref`)
- `internal/tools/task.go` (`Role: "subagent"`)
- `cmd/ymzd` (`BuildRoleEndpoints`, `NewStoreWithMainRef`)
- ADR-039, ADR-041, ADR-042
