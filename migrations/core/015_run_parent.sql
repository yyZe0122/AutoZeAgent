-- Logical child Runs (ADR-039): optional parent link. step_id stays nullable so
-- children can share plan grants without violating runs_plan_step_idx uniqueness.
ALTER TABLE runs ADD COLUMN parent_run_id TEXT REFERENCES runs(run_id);

CREATE INDEX runs_parent_run_idx ON runs(parent_run_id)
    WHERE parent_run_id IS NOT NULL;
