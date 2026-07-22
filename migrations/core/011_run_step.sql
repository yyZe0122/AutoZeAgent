ALTER TABLE runs ADD COLUMN step_id TEXT REFERENCES plan_steps(step_id);

CREATE UNIQUE INDEX runs_plan_step_idx ON runs(plan_id, step_id)
    WHERE step_id IS NOT NULL;
CREATE INDEX runs_state_started_idx ON runs(state, started_at);
