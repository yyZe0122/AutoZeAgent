ALTER TABLE jobs
    ADD COLUMN execution_mode TEXT NOT NULL DEFAULT 'agent'
    CHECK (execution_mode IN ('plan', 'agent'));

ALTER TABLE jobs
    ADD COLUMN skill_ids TEXT NOT NULL DEFAULT '[]';
