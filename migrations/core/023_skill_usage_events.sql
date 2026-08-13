-- ADR-050: skill last-used / soft-archive + draft/apply event log.
CREATE TABLE IF NOT EXISTS skill_usage (
    skill_id TEXT PRIMARY KEY,
    last_used_at TEXT NOT NULL DEFAULT '',
    archived_at TEXT NOT NULL DEFAULT '',
    updated_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS skill_usage_archived_idx
    ON skill_usage (archived_at)
    WHERE archived_at != '';

CREATE TABLE IF NOT EXISTS skill_events (
    event_id TEXT PRIMARY KEY,
    skill_id TEXT NOT NULL,
    action TEXT NOT NULL CHECK (action IN ('draft', 'apply', 'reject', 'used', 'archive')),
    actor TEXT NOT NULL DEFAULT '',
    path TEXT NOT NULL DEFAULT '',
    content_hash TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS skill_events_created_idx
    ON skill_events (created_at DESC, event_id DESC);
