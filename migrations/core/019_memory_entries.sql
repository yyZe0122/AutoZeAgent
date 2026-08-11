-- In-process session/user memory (ADR-044). Does not delete transcripts.
CREATE TABLE IF NOT EXISTS memory_entries (
    entry_id TEXT PRIMARY KEY,
    session_id TEXT NOT NULL DEFAULT '',
    content TEXT NOT NULL,
    source TEXT NOT NULL DEFAULT 'builtin',
    tags_json TEXT NOT NULL DEFAULT '[]',
    created_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS memory_entries_session_created_idx
    ON memory_entries (session_id, created_at DESC);

CREATE INDEX IF NOT EXISTS memory_entries_created_idx
    ON memory_entries (created_at DESC);
