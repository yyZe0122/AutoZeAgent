-- Drop unused job_runs status leftover from interactive Planner ACK.
-- Rebuild CHECK via table copy. Park leases first so FK ON allows DROP job_runs.
CREATE TABLE job_leases_bak AS SELECT * FROM job_leases;
DROP TABLE job_leases;

CREATE TABLE job_runs_new (
    run_id TEXT PRIMARY KEY,
    job_id TEXT NOT NULL REFERENCES jobs(job_id) ON DELETE RESTRICT,
    scheduled_at TEXT NOT NULL,
    attempt INTEGER NOT NULL CHECK (attempt > 0),
    status TEXT NOT NULL CHECK (status IN ('claimed','task_created','completed','failed','timed_out','cancelled')),
    task_request_key TEXT NOT NULL UNIQUE,
    core_task_id TEXT NOT NULL DEFAULT '',
    core_task_key TEXT NOT NULL DEFAULT '',
    error TEXT NOT NULL DEFAULT '',
    started_at TEXT NOT NULL,
    finished_at TEXT NOT NULL DEFAULT ''
);

INSERT INTO job_runs_new (
    run_id, job_id, scheduled_at, attempt, status, task_request_key,
    core_task_id, core_task_key, error, started_at, finished_at
)
SELECT
    run_id, job_id, scheduled_at, attempt,
    CASE WHEN status = 'waiting_approval' THEN 'failed' ELSE status END,
    task_request_key, core_task_id, core_task_key, error, started_at, finished_at
FROM job_runs;

DROP TABLE job_runs;
ALTER TABLE job_runs_new RENAME TO job_runs;

CREATE INDEX IF NOT EXISTS idx_job_runs_job ON job_runs(job_id, scheduled_at);
CREATE INDEX IF NOT EXISTS idx_job_runs_core_task_key ON job_runs(core_task_key);

CREATE TABLE job_leases (
    job_id TEXT PRIMARY KEY REFERENCES jobs(job_id) ON DELETE CASCADE,
    run_id TEXT NOT NULL UNIQUE REFERENCES job_runs(run_id) ON DELETE CASCADE,
    lease_id TEXT NOT NULL UNIQUE,
    owner TEXT NOT NULL,
    acquired_at TEXT NOT NULL,
    expires_at TEXT NOT NULL
);
INSERT INTO job_leases SELECT * FROM job_leases_bak;
DROP TABLE job_leases_bak;
CREATE INDEX IF NOT EXISTS idx_leases_expiry ON job_leases(expires_at);
