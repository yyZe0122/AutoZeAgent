# Unreleased (post v0.2.4)

Working notes for the next release. Promote into `docs/changelog/vX.Y.Z.md` at publish time ([`docs/release.md`](../release.md)).

- **H5-lite memory：** `chat.memory.default_ttl`（Go duration；空=不自动过期）套到 session/detail 写入；global curated 永不自动过期。过期条目启动 + 每 turn 后软归档（`archived_at`），不再自动 `DELETE`。`GET /v1/memory?include_archived=true`；TUI `/memory archived`。
- **H3 skill 草稿（ADR-050）：** agent 工具 `skill_draft` 写 `SKILL.md.draft`；TUI `/skills apply|reject <id>`；apply 先 backup。system 源不可改。
- **H5-skill：** submit 记 `last_used`；`chat.skills.unused_ttl` 软归档（无 last-used 不自动归档）；`GET /v1/skills?include_archived=`；`/skills archived`。
- **C4 journey skill：** `/journey` / `/journey skills` 叠 `skill_events`。
- **H4：** pending permission 只读 `suggested_decision` / `suggested_reason`（once/similar；不自动 decide）。
- **H6：** injectscan 收紧更多零宽/bidi/越狱标记；草稿走同一套扫描。
- **T8：** streaming 回复节流 live glamour；未闭合围栏仍 plain。
- **Hermes skill list/view：** 模型经 `skills_list` → `skill_view` 按需加载；`/skills` 与 job `skill_ids` 仍显式预载快照。关键字不再自动注入正文。MCP 工具排在 `process_exec` 前，prompt/description 优先 MCP。
- **AGENTS.md：** `EnsureConfig` 在 ConfigDir 缺文件时写入默认规则；全局始终注入，`<workspace>/.yunmengze/AGENTS.md` 存在则追加。`injectscan` fail-closed；不扩 grant。默认第 4 条：仅清本轮可确认垃圾；拿不准先问。
- **H4 路径：** 双方皆空才比 tool+capability；一侧空不匹配；非空目录前缀；有 session 限同会话。
- **H5-skill archive 事件：** `Maintain` 新归档写 `skill_events(archive)`。
- **Skill apply HTTP：** `not_found` / `forbidden` 按 sentinel 映射，不再一律 400。
