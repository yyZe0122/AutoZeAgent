# Unreleased (post v0.3.0)

Working notes for the next release. Promote into `docs/history/changelog/vX.Y.Z.md` at publish time ([`docs/release.md`](../../release.md)).

| Area | Change |
| --- | --- |
| **Identity** | Chat Prefix is a short product contract (`YunmengZe Agent <version>`, roles, no vision, `/model` = global main) plus a pin-after `<env>` block (model / workspace / UTC date). User `AGENTS.md` stays a separate overlay. |
| **TUI** | Header and `/status` show daemon version. Live replies no longer fold mid-stream. Mid-turn refresh keeps the typewriter until the transcript covers it or the turn ends. Viewport pins to bottom instead of hard-jumping. |
| **Tab stance** | Tab cycles **plan → agent → auto**. Kernel `execution_mode` stays agent\|plan. Auto is session `permission_stance` (this session pre-grants process+git; leave to end). Interactive Agent waits on `/perm` for ungranted high-risk tools. CLI/`ymz run` stays fail-closed. |
| **Config** | `chat.permission.allow: ["process","git"]` remembers families (OR with `chat.tools.*`). `permission.mode` still loads `preauth`/`ask` but is ignored at runtime. `permission.mode=auto` is still rejected — Auto is a session stance. Omit `permission_stance` on submit does not overwrite an existing session. |
