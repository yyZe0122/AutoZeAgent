# ADR-050：进程内自改进（Skill 草稿 / 习惯提示 / Skill 用量）

- 状态：Accepted
- 日期：2026-08-13

## 背景

Hermes 式自进化在本产品里只能是 **in-process**：Skill 是文件型指令（ADR-034），工具副作用只经 Broker（ADR-012），单库 `core.db`（ADR-004）。不得恢复独立 Evolution 进程、`skills.db`、或自动扩大授权。

H3 / H4 / H5-skill 已落地（未发版）；H2（通道/cron 写入先 stage）仍等消息通道。

## 决策

### 不是什么

- 不是独立 Evolution / Module Runtime / 多 DB。
- Skill 正文与草稿 **永不** 发 grant、改 Policy、绕过 Broker。
- **不**自动 apply 草稿；**不**自动 `allow_permanent`；无 yolo。
- Job/cron 路径保持 ADR-043 fail-closed（不 wait permission；H2 不做）。

### H3 — Skill 草稿

```text
agent (only) ── skill_draft ── Broker ── skillcatalog 写 <id>/SKILL.md.draft
                                              │
TUI /skills apply|reject ── Gateway ── skillmaintain ── backup + 原子替换 SKILL.md
                                              │
                                       skill_events (core.db)
```

- 工具 `skill_draft`：`propose {skill_id, body}` / `discard {skill_id}`。`body` 为完整 `SKILL.md`（含 frontmatter）。仅 **agent**；plan 无此工具。
- 根：`<config_dir>/skills`（user）与项目 `.yunmengze/skills`。**禁止**改 `system` 源。
- 正文经 `injectscan` fail-closed；frontmatter 解析 `name` / `description` / 可选 `triggers`。
- Apply 仅人：`POST /v1/skills/actions` `{action: apply|reject, skill_id}`；TUI `/skills apply <id>` / `/skills reject <id>`。
- Apply 先写 `SKILL.md.bak.<UTC>` 再原子替换；跑中 Task **仍用 ADR-036 快照**。
- Apply 后 catalog `Reload`（同进程发现），不影响已创建 Task。

### H4 — 习惯提示

- 从已 decide 的 `tool_permission_requests` 查同 `tool` + `capability`（有 session 则限同会话）。路径：双方皆空才匹配；一侧空不匹配；非空须目录前缀（见 ADR-043）。
- List/SSE pending 只读字段：`suggested_decision` + `suggested_reason`。
- 只建议 `allow_once` / `allow_similar`；曾 `deny` 可提示「上次拒绝」，**不**自动 deny。
- **从不**建议 `allow_permanent`；不改热键默认；不预勾选。
- Job/cron 仍立即 deny，不参与习惯。

### H5-skill — last-used + 软归档

- 表 `skill_usage`：`last_used_at` / `archived_at`（空=未归档）。
- `tasksubmission` 在显式 `skill_ids` 落库后 upsert last-used + `skill_events(used)`；选中即清归档。`skill_view` 成功加载正文时同样记 used。归档 skill 不出现在 `skills_list`，工具也不可 view。
- 配置 `chat.skills.unused_ttl`（Go duration；空=不自动归档）。启动与用量更新后将超 TTL 且曾有 last-used 的条目标归档，并写 `skill_events(archive)`（actor=`system`）。
- **无 last-used 的旧 skill 不自动归档**（避免误伤）。
- `GET /v1/skills` 默认排除归档；`include_archived=true` 只返回归档行。文件不删。
- 归档 skill 仍可 `/<id>` 显式选；submit 记 used 并解除归档。

### 存储（migration 023）

`skill_usage(skill_id PK, last_used_at, archived_at, updated_at)`  
`skill_events(event_id, skill_id, action, actor, path, content_hash, created_at)`  
`action` ∈ `draft | apply | reject | used | archive`。时间 UTC RFC3339Nano。

读：`corequery.ListSkillEvents`。写：`internal/skillmaintain`（窄 service）。Gateway **不**持业务 `*sql.DB`。

### 可见性

- `GET /v1/skills`：元数据 + `draft` / `last_used_at` / `archived_at`（无正文、无绝对路径）。
- `GET /v1/skills/events?limit=&skill_id=`
- TUI `/journey` 可叠 skill 事件轨（C4）；`/journey skills` / `/journey memory` 可分列。

## 后果

- 本机可提议并人工落地 skill 改进，授权边界不变。
- `/perm` 能提示上次同类选择，决策仍在人。
- 长期不用的 skill 可软归档，不丢文件。

## 相关

- ADR-012 Broker；ADR-034 文件型 skills；ADR-036 快照；ADR-043/046 permission；ADR-018 Gateway；ADR-022 corequery；ADR-044 memory（并行维护，非本 ADR）。
