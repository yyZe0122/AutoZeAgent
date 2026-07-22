CREATE TABLE sessions (
    session_id TEXT PRIMARY KEY,
    state TEXT NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    metadata TEXT NOT NULL DEFAULT '{}'
        CHECK (json_valid(metadata))
);

CREATE TABLE tasks (
    task_id TEXT PRIMARY KEY,
    session_id TEXT REFERENCES sessions(session_id),
    title TEXT NOT NULL,
    objective TEXT NOT NULL,
    state TEXT NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE TABLE plans (
    plan_id TEXT PRIMARY KEY,
    task_id TEXT NOT NULL REFERENCES tasks(task_id),
    revision INTEGER NOT NULL CHECK (revision > 0),
    state TEXT NOT NULL,
    scope_hash TEXT NOT NULL,
    created_at TEXT NOT NULL,
    UNIQUE (task_id, revision)
);

CREATE TABLE plan_steps (
    step_id TEXT PRIMARY KEY,
    plan_id TEXT NOT NULL REFERENCES plans(plan_id),
    position INTEGER NOT NULL CHECK (position >= 0),
    title TEXT NOT NULL,
    state TEXT NOT NULL,
    effect_level TEXT NOT NULL,
    created_at TEXT NOT NULL,
    UNIQUE (plan_id, position)
);

CREATE TABLE approvals (
    approval_id TEXT PRIMARY KEY,
    plan_id TEXT NOT NULL REFERENCES plans(plan_id),
    plan_revision INTEGER NOT NULL CHECK (plan_revision > 0),
    decision TEXT NOT NULL,
    scope_hash TEXT NOT NULL,
    decided_by TEXT NOT NULL,
    decided_at TEXT NOT NULL,
    expires_at TEXT
);

CREATE TABLE capability_grants (
    grant_id TEXT PRIMARY KEY,
    approval_id TEXT NOT NULL REFERENCES approvals(approval_id),
    task_id TEXT NOT NULL REFERENCES tasks(task_id),
    plan_id TEXT NOT NULL REFERENCES plans(plan_id),
    step_id TEXT NOT NULL REFERENCES plan_steps(step_id),
    capability TEXT NOT NULL,
    resource_scope TEXT NOT NULL,
    issued_at TEXT NOT NULL,
    expires_at TEXT NOT NULL,
    revoked_at TEXT
);

CREATE TABLE runs (
    run_id TEXT PRIMARY KEY,
    task_id TEXT NOT NULL REFERENCES tasks(task_id),
    plan_id TEXT NOT NULL REFERENCES plans(plan_id),
    state TEXT NOT NULL,
    started_at TEXT NOT NULL,
    finished_at TEXT,
    error TEXT
);

CREATE TABLE tool_calls (
    tool_call_id TEXT PRIMARY KEY,
    run_id TEXT NOT NULL REFERENCES runs(run_id),
    step_id TEXT NOT NULL REFERENCES plan_steps(step_id),
    grant_id TEXT NOT NULL REFERENCES capability_grants(grant_id),
    tool_name TEXT NOT NULL,
    state TEXT NOT NULL,
    request TEXT NOT NULL CHECK (json_valid(request)),
    response TEXT CHECK (response IS NULL OR json_valid(response)),
    started_at TEXT NOT NULL,
    finished_at TEXT
);

CREATE TABLE events (
    sequence INTEGER PRIMARY KEY AUTOINCREMENT,
    event_id TEXT NOT NULL UNIQUE,
    event_type TEXT NOT NULL,
    aggregate_type TEXT NOT NULL,
    aggregate_id TEXT NOT NULL,
    aggregate_version INTEGER NOT NULL CHECK (aggregate_version > 0),
    occurred_at TEXT NOT NULL,
    producer TEXT NOT NULL,
    schema_version INTEGER NOT NULL CHECK (schema_version > 0),
    trace_id TEXT NOT NULL DEFAULT '',
    causation_id TEXT NOT NULL DEFAULT '',
    correlation_id TEXT NOT NULL DEFAULT '',
    payload TEXT NOT NULL CHECK (json_valid(payload)),
    UNIQUE (aggregate_type, aggregate_id, aggregate_version)
);

CREATE INDEX events_aggregate_sequence_idx
    ON events (aggregate_type, aggregate_id, aggregate_version);
CREATE INDEX events_occurred_at_idx ON events (occurred_at);

CREATE TRIGGER events_reject_update
BEFORE UPDATE ON events
BEGIN
    SELECT RAISE(ABORT, 'events are append-only');
END;

CREATE TRIGGER events_reject_delete
BEFORE DELETE ON events
BEGIN
    SELECT RAISE(ABORT, 'events are append-only');
END;

CREATE TABLE audit_log (
    audit_id INTEGER PRIMARY KEY AUTOINCREMENT,
    occurred_at TEXT NOT NULL,
    actor TEXT NOT NULL,
    action TEXT NOT NULL,
    resource_type TEXT NOT NULL,
    resource_id TEXT NOT NULL,
    outcome TEXT NOT NULL,
    trace_id TEXT NOT NULL DEFAULT '',
    details TEXT NOT NULL DEFAULT '{}'
        CHECK (json_valid(details))
);

CREATE INDEX audit_log_occurred_at_idx ON audit_log (occurred_at);
CREATE INDEX audit_log_resource_idx ON audit_log (resource_type, resource_id);

CREATE TABLE module_registry (
    module_id TEXT PRIMARY KEY,
    module_version TEXT NOT NULL,
    protocol_version TEXT NOT NULL,
    state TEXT NOT NULL,
    endpoint TEXT NOT NULL,
    manifest TEXT NOT NULL CHECK (json_valid(manifest)),
    registered_at TEXT NOT NULL,
    last_seen_at TEXT NOT NULL
);

CREATE TABLE module_offsets (
    module_id TEXT NOT NULL REFERENCES module_registry(module_id) ON DELETE CASCADE,
    subscription TEXT NOT NULL,
    last_sequence INTEGER NOT NULL DEFAULT 0 CHECK (last_sequence >= 0),
    updated_at TEXT NOT NULL,
    PRIMARY KEY (module_id, subscription)
);

CREATE TABLE artifacts (
    artifact_id TEXT PRIMARY KEY,
    content_hash TEXT NOT NULL UNIQUE,
    media_type TEXT NOT NULL,
    size_bytes INTEGER NOT NULL CHECK (size_bytes >= 0),
    storage_path TEXT NOT NULL,
    created_at TEXT NOT NULL,
    metadata TEXT NOT NULL DEFAULT '{}'
        CHECK (json_valid(metadata))
);
