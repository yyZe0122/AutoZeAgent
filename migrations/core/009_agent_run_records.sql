CREATE TABLE agent_run_records (
    run_id TEXT NOT NULL REFERENCES runs(run_id),
    position INTEGER NOT NULL CHECK (position >= 0),
    record_type TEXT NOT NULL CHECK (record_type IN ('input_message', 'assistant_message', 'tool_result')),
    message TEXT NOT NULL CHECK (json_valid(message)),
    usage TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(usage)),
    finish_reason TEXT NOT NULL DEFAULT '',
    tool_call_id TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    PRIMARY KEY (run_id, position)
);

CREATE INDEX agent_run_records_run_type_idx
    ON agent_run_records (run_id, record_type, position);

CREATE UNIQUE INDEX agent_run_records_tool_result_idx
    ON agent_run_records (run_id, tool_call_id)
    WHERE record_type = 'tool_result';

CREATE TRIGGER agent_run_records_reject_update
BEFORE UPDATE ON agent_run_records
BEGIN
    SELECT RAISE(ABORT, 'agent run records are append-only');
END;

CREATE TRIGGER agent_run_records_reject_delete
BEFORE DELETE ON agent_run_records
BEGIN
    SELECT RAISE(ABORT, 'agent run records are append-only');
END;
