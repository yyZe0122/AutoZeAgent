CREATE TABLE evolution_activation_applications (
    authorization_id TEXT PRIMARY KEY REFERENCES evolution_activation_authorizations(authorization_id),
    target_module TEXT NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('applying','active','failed')),
    attempt_count INTEGER NOT NULL CHECK (attempt_count > 0),
    approval_id TEXT NOT NULL REFERENCES approvals(approval_id),
    lease_token TEXT NOT NULL,
    lease_expires_at TEXT NOT NULL,
    receipt_json TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(receipt_json)),
    last_error TEXT NOT NULL DEFAULT '',
    applied_at TEXT,
    updated_at TEXT NOT NULL
);
CREATE INDEX evolution_activation_applications_status_idx
    ON evolution_activation_applications (status, lease_expires_at);
