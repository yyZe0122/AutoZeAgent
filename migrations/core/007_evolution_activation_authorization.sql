-- Core owns activation authorization. Optional Evolution modules may supply
-- immutable candidate and evaluation evidence, but they cannot write this table.
CREATE TABLE evolution_activation_authorizations (
    authorization_id TEXT PRIMARY KEY,
    candidate_id TEXT NOT NULL,
    candidate_type TEXT NOT NULL,
    candidate_content_hash TEXT NOT NULL,
    evaluation_id TEXT NOT NULL,
    evaluation_hash TEXT NOT NULL,
    target_module TEXT NOT NULL,
    target_version TEXT NOT NULL,
    plan_id TEXT NOT NULL REFERENCES plans(plan_id),
    plan_revision INTEGER NOT NULL CHECK (plan_revision > 0),
    plan_hash TEXT NOT NULL,
    step_id TEXT NOT NULL REFERENCES plan_steps(step_id),
    approval_id TEXT NOT NULL REFERENCES approvals(approval_id),
    authorized_by TEXT NOT NULL,
    authorized_at TEXT NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('authorized')),
    trace_id TEXT NOT NULL DEFAULT '',
    candidate_snapshot TEXT NOT NULL CHECK (json_valid(candidate_snapshot)),
    evaluation_snapshot TEXT NOT NULL CHECK (json_valid(evaluation_snapshot)),
    UNIQUE (candidate_id, candidate_content_hash, target_module, target_version)
);

CREATE INDEX evolution_activation_candidate_idx
    ON evolution_activation_authorizations (candidate_id, candidate_content_hash);
CREATE INDEX evolution_activation_plan_idx
    ON evolution_activation_authorizations (plan_id, plan_revision, step_id);
