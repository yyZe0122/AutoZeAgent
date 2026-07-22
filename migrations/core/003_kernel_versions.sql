ALTER TABLE sessions
    ADD COLUMN version INTEGER NOT NULL DEFAULT 1 CHECK (version > 0);

ALTER TABLE tasks
    ADD COLUMN version INTEGER NOT NULL DEFAULT 1 CHECK (version > 0);

ALTER TABLE plans
    ADD COLUMN version INTEGER NOT NULL DEFAULT 1 CHECK (version > 0);
ALTER TABLE plans
    ADD COLUMN updated_at TEXT NOT NULL DEFAULT '';
UPDATE plans SET updated_at = created_at WHERE updated_at = '';

ALTER TABLE plan_steps
    ADD COLUMN version INTEGER NOT NULL DEFAULT 1 CHECK (version > 0);
ALTER TABLE plan_steps
    ADD COLUMN updated_at TEXT NOT NULL DEFAULT '';
UPDATE plan_steps SET updated_at = created_at WHERE updated_at = '';

ALTER TABLE runs
    ADD COLUMN version INTEGER NOT NULL DEFAULT 1 CHECK (version > 0);
ALTER TABLE runs
    ADD COLUMN updated_at TEXT NOT NULL DEFAULT '';
UPDATE runs SET updated_at = started_at WHERE updated_at = '';
