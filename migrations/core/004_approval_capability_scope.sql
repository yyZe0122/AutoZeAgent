ALTER TABLE approvals ADD COLUMN scope_type TEXT NOT NULL DEFAULT 'plan';
ALTER TABLE approvals ADD COLUMN step_id TEXT;
ALTER TABLE approvals ADD COLUMN reason TEXT NOT NULL DEFAULT '';
ALTER TABLE approvals ADD COLUMN invalidated_at TEXT;

ALTER TABLE capability_grants ADD COLUMN plan_hash TEXT NOT NULL DEFAULT '';
ALTER TABLE capability_grants ADD COLUMN paths_json TEXT NOT NULL DEFAULT '[]';
ALTER TABLE capability_grants ADD COLUMN command_name TEXT NOT NULL DEFAULT '';
ALTER TABLE capability_grants ADD COLUMN command_args_json TEXT NOT NULL DEFAULT '[]';
ALTER TABLE capability_grants ADD COLUMN network_domains_json TEXT NOT NULL DEFAULT '[]';
ALTER TABLE capability_grants ADD COLUMN max_duration_ms INTEGER NOT NULL DEFAULT 0;
ALTER TABLE capability_grants ADD COLUMN max_calls INTEGER NOT NULL DEFAULT 1;
ALTER TABLE capability_grants ADD COLUMN used_calls INTEGER NOT NULL DEFAULT 0;
ALTER TABLE capability_grants ADD COLUMN one_time INTEGER NOT NULL DEFAULT 1;
ALTER TABLE capability_grants ADD COLUMN updated_at TEXT NOT NULL DEFAULT '';

CREATE INDEX approvals_current_scope_idx
    ON approvals (plan_id, plan_revision, scope_hash, decision, scope_type, step_id);
CREATE INDEX capability_grants_active_idx
    ON capability_grants (grant_id, plan_hash, step_id, revoked_at, expires_at);