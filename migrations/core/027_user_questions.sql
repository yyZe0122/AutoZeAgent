-- Model-facing ask_user questions (ADR-052 R4). Separate from tool_permission_requests.
CREATE TABLE IF NOT EXISTS user_questions (
    question_id TEXT PRIMARY KEY,
    session_id TEXT NOT NULL DEFAULT '',
    task_id TEXT NOT NULL DEFAULT '',
    run_id TEXT NOT NULL DEFAULT '',
    tool_call_id TEXT NOT NULL DEFAULT '',
    questions_json TEXT NOT NULL DEFAULT '[]',
    state TEXT NOT NULL DEFAULT 'pending'
        CHECK (state IN ('pending', 'answered', 'unavailable', 'cancelled')),
    answers_json TEXT NOT NULL DEFAULT '{}',
    decided_by TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    decided_at TEXT
);

CREATE INDEX IF NOT EXISTS user_questions_pending_idx
    ON user_questions (state, created_at);

CREATE INDEX IF NOT EXISTS user_questions_session_idx
    ON user_questions (session_id, state, created_at);
