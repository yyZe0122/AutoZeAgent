-- Memory productization (ADR-044): layered fields, FTS, transcript search projection.
-- Does not delete transcripts or agent_run_records.

ALTER TABLE memory_entries ADD COLUMN kind TEXT NOT NULL DEFAULT 'session';
ALTER TABLE memory_entries ADD COLUMN priority INTEGER NOT NULL DEFAULT 0;
ALTER TABLE memory_entries ADD COLUMN expires_at TEXT NOT NULL DEFAULT '';
ALTER TABLE memory_entries ADD COLUMN updated_at TEXT NOT NULL DEFAULT '';

UPDATE memory_entries SET updated_at = created_at WHERE updated_at = '';
UPDATE memory_entries SET kind = 'curated' WHERE session_id = '' AND kind = 'session';

CREATE INDEX IF NOT EXISTS memory_entries_kind_created_idx
    ON memory_entries (kind, created_at DESC);
CREATE INDEX IF NOT EXISTS memory_entries_expires_idx
    ON memory_entries (expires_at)
    WHERE expires_at != '';

-- FTS5 over curated/session/detail facts (content only; id in content table).
CREATE VIRTUAL TABLE IF NOT EXISTS memory_entries_fts USING fts5(
    entry_id UNINDEXED,
    content,
    tokenize = 'unicode61'
);

INSERT INTO memory_entries_fts(entry_id, content)
SELECT entry_id, content FROM memory_entries;

-- Transcript search projection (L3). Append-only source remains agent_run_records.
CREATE TABLE IF NOT EXISTS transcript_search (
    row_id INTEGER PRIMARY KEY AUTOINCREMENT,
    session_id TEXT NOT NULL,
    run_id TEXT NOT NULL,
    position INTEGER NOT NULL,
    record_type TEXT NOT NULL,
    content TEXT NOT NULL,
    created_at TEXT NOT NULL,
    UNIQUE (run_id, position)
);

CREATE INDEX IF NOT EXISTS transcript_search_session_created_idx
    ON transcript_search (session_id, created_at DESC);

CREATE VIRTUAL TABLE IF NOT EXISTS transcript_fts USING fts5(
    row_id UNINDEXED,
    session_id UNINDEXED,
    content,
    tokenize = 'unicode61'
);
