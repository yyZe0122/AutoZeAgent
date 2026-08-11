# Contributing to AutoZeAgent

Thank you for considering a contribution. AutoZeAgent favors small, reviewable changes that preserve explicit security boundaries and straightforward Go code.

## Before you start

- Search existing issues and pull requests for related work.
- Open an issue before a large feature, protocol change, database migration, or architectural refactor.
- Report vulnerabilities privately according to [SECURITY.md](SECURITY.md).
- Never include API keys, credentials, private logs, personal paths, local databases, or generated binaries in a contribution.

## Development setup

Requirements are listed in [README.md](README.md).

Windows:

```powershell
.\scripts\dev.ps1 -Action check
.\scripts\dev.ps1 -Action build
```

Linux/macOS:

```bash
make check
make build
```

## Change guidelines

1. Keep the change focused on one problem.
2. Add or update tests for behavior changes.
3. Preserve existing policy, approval, capability, path, timeout, and audit boundaries.
4. Use explicit errors and deterministic recovery behavior.
5. Add a numbered ADR under `docs/architecture/` for a significant architectural decision; update `docs/architecture/README.md` index when adding one.
6. Keep living status / optional tails only in `docs/optimization/current.md` — do not create parallel optimization notes.
7. Update public documentation (especially `README.md` dual-track / `chat` / TUI) and configuration examples when user-facing behavior changes.
8. Do not edit generated or runtime files unless the change specifically concerns their generator or format.

## Code style

- Run `gofmt` on changed Go files.
- Prefer small interfaces at the point of use.
- Avoid hidden global state and unnecessary abstractions.
- Pass `context.Context` through cancellable or long-running operations.
- Wrap errors with enough operation context to diagnose failures without exposing secrets.
- Keep logs structured (`component` / `operation` / `result` + IDs) and use the existing redaction rules for sensitive fields. Field conventions: ADR-047; prefer `internal/runlog.Attrs` on new daemon stage boundaries.

## Tests

At minimum, run the platform-appropriate check command before opening a pull request. Changes to concurrency, recovery, scheduling, providers, tool execution, or persistence should include focused tests for failure and restart behavior.

If you change dependencies, also run:

```bash
go mod tidy
go mod verify
git diff -- go.mod go.sum
```

## Pull requests

A pull request should include:

- a concise problem statement;
- the chosen approach and important tradeoffs;
- tests performed;
- security or migration implications;
- documentation updates;
- follow-up work that is intentionally out of scope.

Before submission, inspect the staged set:

```bash
git status --short
git diff --cached --check
git diff --cached
```

Confirm that no local configuration, secrets, personal notes, databases, logs, runtime state, or build output are staged.

## License

By contributing, you agree that your contributions will be licensed under the [Apache License 2.0](LICENSE).
