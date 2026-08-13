-- H5-lite: soft-archive expired memory (no automatic hard delete).
ALTER TABLE memory_entries ADD COLUMN archived_at TEXT NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS memory_entries_archived_idx
    ON memory_entries (archived_at)
    WHERE archived_at != '';
