CREATE TABLE IF NOT EXISTS jobs (
    job_id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    session_id TEXT NOT NULL REFERENCES sessions(session_id),
    task_title TEXT NOT NULL,
    task_objective TEXT NOT NULL,
    interval_seconds INTEGER NOT NULL CHECK (interval_seconds > 0),
    next_run_at TEXT NOT NULL,
    timeout_seconds INTEGER NOT NULL CHECK (timeout_seconds > 0),
    max_retries INTEGER NOT NULL CHECK (max_retries >= 0),
    backoff_seconds INTEGER NOT NULL CHECK (backoff_seconds >= 0),
    misfire_policy TEXT NOT NULL CHECK (misfire_policy IN ('skip','catch_up','run_once')),
    idempotency_key TEXT NOT NULL UNIQUE,
    status TEXT NOT NULL CHECK (status IN ('active','paused','archived')),
    retry_attempt INTEGER NOT NULL DEFAULT 0 CHECK (retry_attempt >= 0),
    retry_at TEXT NOT NULL DEFAULT '',
    retry_origin_at TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS job_runs (
    run_id TEXT PRIMARY KEY,
    job_id TEXT NOT NULL REFERENCES jobs(job_id) ON DELETE RESTRICT,
    scheduled_at TEXT NOT NULL,
    attempt INTEGER NOT NULL CHECK (attempt > 0),
    status TEXT NOT NULL CHECK (status IN ('claimed','task_created','waiting_approval','completed','failed','timed_out','cancelled')),
    task_request_key TEXT NOT NULL UNIQUE,
    core_task_id TEXT NOT NULL DEFAULT '',
    core_task_key TEXT NOT NULL DEFAULT '',
    error TEXT NOT NULL DEFAULT '',
    started_at TEXT NOT NULL,
    finished_at TEXT NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS job_leases (
    job_id TEXT PRIMARY KEY REFERENCES jobs(job_id) ON DELETE CASCADE,
    run_id TEXT NOT NULL UNIQUE REFERENCES job_runs(run_id) ON DELETE CASCADE,
    lease_id TEXT NOT NULL UNIQUE,
    owner TEXT NOT NULL,
    acquired_at TEXT NOT NULL,
    expires_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_jobs_due ON jobs(status, next_run_at);
CREATE INDEX IF NOT EXISTS idx_jobs_retry ON jobs(status, retry_at);
CREATE INDEX IF NOT EXISTS idx_job_runs_job ON job_runs(job_id, scheduled_at);
CREATE INDEX IF NOT EXISTS idx_job_runs_core_task_key ON job_runs(core_task_key);
CREATE INDEX IF NOT EXISTS idx_leases_expiry ON job_leases(expires_at);
