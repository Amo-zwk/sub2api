ALTER TABLE account_health_states
    ADD COLUMN IF NOT EXISTS probe_class VARCHAR(24) NOT NULL DEFAULT 'new';

ALTER TABLE account_health_states
    DROP CONSTRAINT IF EXISTS account_health_state_valid;

UPDATE account_health_states
SET state = CASE
        WHEN state = 'healthy' THEN 'healthy'
        WHEN state = 'unknown' THEN 'unknown'
        WHEN error_category = 'authentication' THEN 'auth_quarantine'
        WHEN error_category = 'entitlement' THEN 'entitlement_quarantine'
        WHEN error_category IN ('permission', 'model') THEN 'permission_quarantine'
        WHEN state IN ('degraded', 'recovering', 'blocked') THEN 'transient_error'
        ELSE 'unknown'
    END,
    probe_class = CASE
        WHEN state = 'healthy' THEN 'healthy_sample'
        WHEN state = 'unknown' THEN 'new'
        WHEN error_category IN ('authentication', 'entitlement', 'permission', 'model') THEN 'terminal_canary'
        ELSE 'transient'
    END,
    next_probe_at = CASE
        WHEN error_category = 'entitlement' THEN GREATEST(next_probe_at, NOW() + INTERVAL '6 hours')
        WHEN error_category = 'authentication' THEN GREATEST(next_probe_at, NOW() + INTERVAL '30 minutes')
        WHEN error_category IN ('permission', 'model') THEN GREATEST(next_probe_at, NOW() + INTERVAL '1 hour')
        ELSE next_probe_at
    END;

ALTER TABLE account_health_states
    ADD CONSTRAINT account_health_state_valid CHECK (
        state IN (
            'unknown',
            'healthy',
            'probation',
            'transient_error',
            'auth_quarantine',
            'entitlement_quarantine',
            'permission_quarantine'
        )
    );

ALTER TABLE account_health_states
    ADD CONSTRAINT account_health_probe_class_valid CHECK (
        probe_class IN ('new', 'transient', 'healthy_sample', 'terminal_canary')
    );

DROP INDEX IF EXISTS idx_account_health_due;
CREATE INDEX IF NOT EXISTS idx_account_health_due_class
    ON account_health_states (probe_class, next_probe_at, lease_until, account_id);

DELETE FROM account_health_states h
WHERE NOT EXISTS (
    SELECT 1
    FROM accounts a
    WHERE a.id = h.account_id
      AND a.deleted_at IS NULL
      AND a.platform = 'openai'
);
