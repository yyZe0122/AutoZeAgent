ALTER TABLE tasks
    ADD COLUMN execution_mode TEXT NOT NULL DEFAULT 'agent'
    CHECK (execution_mode IN ('plan', 'agent'));
