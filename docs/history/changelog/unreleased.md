# Unreleased (post v0.2.5)

Working notes for the next release. Promote into `docs/history/changelog/vX.Y.Z.md` at publish time ([`docs/release.md`](../../release.md)).

- Docs: three boxes under `docs/` — `wiki/` (ADRs + `database.md`), `history/changelog/`, `backlog/current.md`. Catalog: `docs/README.md`. Publish notes path is now `docs/history/changelog/vX.Y.Z.md`.
- Docs vs code: user root is `~/.yunmengze` (not XDG); chat resolve is **job pin → prefer → main**; README TUI/CLI + ADR-018 route table aligned; ADR-031 model-stream present tense; ADR-043 decide path + SSE payload; memory/skills query fields; install pin `v0.2.5`.
- Cleanup: removed `ymz approval` stub, unused plan-approval DTOs, scheduler ACK `waiting_approval`, kernel/TUI Planner leftover states, and systemd `planner.env` (now `/etc/yunmengze/env`). Migration 024 rebuilds `job_runs` CHECK without `waiting_approval`. No ADR-049 placeholder.
- No research-stage compat: drop `allow_session` alias, `chat.roots`, `EnsureConfig` project/cwd copy, `permission.mode=auto`. Migration 025 aligns permission states to once/similar/permanent.
