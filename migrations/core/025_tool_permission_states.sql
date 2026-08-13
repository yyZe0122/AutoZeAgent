-- Align tool_permission_requests.state with once/similar/permanent (drop allowed_session).
CREATE TABLE tool_permission_requests_new (
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
        CHECK (state IN ('pending', 'allowed_once', 'allowed_similar', 'allowed_permanent', 'denied', 'expired', 'cancelled')),
    grant_id TEXT,
    decision TEXT,
    decided_by TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    decided_at TEXT,
    expires_at TEXT
);

INSERT INTO tool_permission_requests_new
SELECT
    permission_id, session_id, task_id, run_id, plan_id, plan_hash, step_id,
    tool_call_id, tool_name, arguments_json, capability, path, command_name,
    command_args_json, network_domain, risk,
    CASE WHEN state = 'allowed_session' THEN 'allowed_similar' ELSE state END,
    grant_id,
    CASE WHEN decision = 'allow_session' THEN 'allow_similar' ELSE decision END,
    decided_by, created_at, decided_at, expires_at
FROM tool_permission_requests;

DROP TABLE tool_permission_requests;
ALTER TABLE tool_permission_requests_new RENAME TO tool_permission_requests;

CREATE INDEX IF NOT EXISTS tool_permission_requests_pending_idx
    ON tool_permission_requests (state, created_at);
CREATE INDEX IF NOT EXISTS tool_permission_requests_session_idx
    ON tool_permission_requests (session_id, state, created_at);
CREATE INDEX IF NOT EXISTS tool_permission_requests_run_idx
    ON tool_permission_requests (run_id, state);
