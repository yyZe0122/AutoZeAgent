-- Phase Q coding loop: session todos (QE). edit_revisions (QG) may be added to this file before release.
CREATE TABLE IF NOT EXISTS session_todos (
    session_id TEXT NOT NULL,
    item_id TEXT NOT NULL,
    content TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending', 'in_progress', 'completed', 'cancelled')),
    position INTEGER NOT NULL DEFAULT 0,
    updated_at TEXT NOT NULL,
    PRIMARY KEY (session_id, item_id)
);
CREATE INDEX IF NOT EXISTS session_todos_session_pos_idx
    ON session_todos (session_id, position, item_id);

CREATE TABLE IF NOT EXISTS edit_revisions (
    revision_id TEXT PRIMARY KEY NOT NULL,
    session_id TEXT NOT NULL,
    path TEXT NOT NULL,
    sha_before TEXT NOT NULL DEFAULT '',
    sha_after TEXT NOT NULL DEFAULT '',
    artifact_id TEXT NOT NULL DEFAULT '',
    run_id TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS edit_revisions_session_created_idx
    ON edit_revisions (session_id, created_at);
