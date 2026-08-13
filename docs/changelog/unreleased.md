# Unreleased (post v0.2.4)

Working notes for the next release. Promote into `docs/changelog/vX.Y.Z.md` at publish time ([`docs/release.md`](../release.md)).

- **H5-lite memory：** `chat.memory.default_ttl`（Go duration；空=不自动过期）套到 session/detail 写入；global curated 永不自动过期。过期条目启动 + 每 turn 后软归档（`archived_at`），不再自动 `DELETE`。`GET /v1/memory?include_archived=true`；TUI `/memory archived`。
