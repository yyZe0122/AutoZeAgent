-- Tool-call interactive permission queue (ADR-043). Pending rows block Broker until decide.
CREATE TABLE IF NOT EXISTS tool_permission_requests (
    permission_id TEXT PRIMARY KEY,
    session_id TEXT NOT NULL DEFAULT '',
    task_id TEXT NOT NULL,
    run_id TEXT NOT NULL,
    plan_id TEXT NOT NULL DEFAULT '',
    plan_hash TEXT NOT NULL DEFAULT '',
    step_id TEXT NOT NULL DEFAULT '',
    tool_call_id TEXT NOT NULL,
    tool_name TEXT NOT NULL,
    arguments_json TEXT NOT NULL DEFAULT '{}',
    capability TEXT NOT NULL DEFAULT '',
    path TEXT NOT NULL DEFAULT '',
    command_name TEXT NOT NULL DEFAULT '',
    command_args_json TEXT NOT NULL DEFAULT '[]',
    network_domain TEXT NOT NULL DEFAULT '',
    risk TEXT NOT NULL DEFAULT '',
    state TEXT NOT NULL DEFAULT 'pending'
        CHECK (state IN ('pending', 'allowed_once', 'allowed_session', 'denied', 'expired', 'cancelled')),
    grant_id TEXT,
    decision TEXT,
    decided_by TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    decided_at TEXT,
    expires_at TEXT
);

CREATE INDEX IF NOT EXISTS tool_permission_requests_pending_idx
    ON tool_permission_requests (state, created_at);

CREATE INDEX IF NOT EXISTS tool_permission_requests_session_idx
    ON tool_permission_requests (session_id, state, created_at);

CREATE INDEX IF NOT EXISTS tool_permission_requests_run_idx
    ON tool_permission_requests (run_id, state);
