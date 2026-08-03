-- Context pressure snapshots (P4.1): last provider prompt fill per task/session.
-- Full transcripts remain in agent_run_records; this is monitoring + packing hints only.

CREATE TABLE IF NOT EXISTS context_snapshots (
    task_id TEXT PRIMARY KEY NOT NULL,
    session_id TEXT NOT NULL,
    model TEXT NOT NULL DEFAULT '',
    context_window INTEGER NOT NULL DEFAULT 0,
    max_output_tokens INTEGER NOT NULL DEFAULT 0,
    usable_tokens INTEGER NOT NULL DEFAULT 0,
    last_prompt_tokens INTEGER NOT NULL DEFAULT 0,
    last_output_tokens INTEGER NOT NULL DEFAULT 0,
    estimate_tokens INTEGER NOT NULL DEFAULT 0,
    cache_read_tokens INTEGER NOT NULL DEFAULT 0,
    cache_write_tokens INTEGER NOT NULL DEFAULT 0,
    source TEXT NOT NULL DEFAULT 'none',
    estimate_source TEXT NOT NULL DEFAULT 'local_estimate',
    ratio REAL NOT NULL DEFAULT 1.0,
    calibrated INTEGER NOT NULL DEFAULT 0,
    compacted INTEGER NOT NULL DEFAULT 0,
    history_messages INTEGER NOT NULL DEFAULT 0,
    updated_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS context_snapshots_session_idx ON context_snapshots(session_id);

CREATE TABLE IF NOT EXISTS session_compactions (
    compaction_id TEXT PRIMARY KEY NOT NULL,
    session_id TEXT NOT NULL,
    summary TEXT NOT NULL,
    through_message_id TEXT NOT NULL DEFAULT '',
    model TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS session_compactions_session_created_idx
    ON session_compactions(session_id, created_at);
