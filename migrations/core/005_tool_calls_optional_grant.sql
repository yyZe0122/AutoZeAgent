ALTER TABLE tool_calls RENAME TO tool_calls_v1;

CREATE TABLE tool_calls (
    tool_call_id TEXT PRIMARY KEY,
    run_id TEXT NOT NULL REFERENCES runs(run_id),
    step_id TEXT NOT NULL REFERENCES plan_steps(step_id),
    grant_id TEXT REFERENCES capability_grants(grant_id),
    tool_name TEXT NOT NULL,
    state TEXT NOT NULL,
    request TEXT NOT NULL CHECK (json_valid(request)),
    response TEXT CHECK (response IS NULL OR json_valid(response)),
    started_at TEXT NOT NULL,
    finished_at TEXT
);

INSERT INTO tool_calls (
    tool_call_id, run_id, step_id, grant_id, tool_name, state,
    request, response, started_at, finished_at
)
SELECT
    tool_call_id, run_id, step_id, grant_id, tool_name, state,
    request, response, started_at, finished_at
FROM tool_calls_v1;

DROP TABLE tool_calls_v1;

CREATE INDEX tool_calls_run_started_idx ON tool_calls (run_id, started_at);
CREATE INDEX tool_calls_step_started_idx ON tool_calls (step_id, started_at);