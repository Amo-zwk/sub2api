CREATE TABLE IF NOT EXISTS account_health_states (
    account_id BIGINT PRIMARY KEY REFERENCES accounts(id) ON DELETE CASCADE,
    state VARCHAR(24) NOT NULL DEFAULT 'unknown',
    auto_blocked BOOLEAN NOT NULL DEFAULT FALSE,
    error_category VARCHAR(32) NOT NULL DEFAULT '',
    error_message TEXT NOT NULL DEFAULT '',
    consecutive_successes INTEGER NOT NULL DEFAULT 0,
    consecutive_failures INTEGER NOT NULL DEFAULT 0,
    last_probe_at TIMESTAMPTZ,
    last_success_at TIMESTAMPTZ,
    next_probe_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    probe_latency_ms BIGINT NOT NULL DEFAULT 0,
    lease_until TIMESTAMPTZ,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT account_health_state_valid CHECK (
        state IN ('unknown', 'healthy', 'degraded', 'recovering', 'blocked')
    ),
    CONSTRAINT account_health_counters_non_negative CHECK (
        consecutive_successes >= 0 AND consecutive_failures >= 0
    )
);

CREATE INDEX IF NOT EXISTS idx_account_health_due
    ON account_health_states (next_probe_at, lease_until);
CREATE INDEX IF NOT EXISTS idx_account_health_state
    ON account_health_states (state);

INSERT INTO account_health_states (
    account_id,
    state,
    auto_blocked,
    error_category,
    error_message,
    next_probe_at
)
SELECT
    a.id,
    CASE WHEN a.status = 'error' THEN 'recovering' ELSE 'unknown' END,
    a.status = 'error',
    CASE WHEN a.status = 'error' THEN 'legacy_error' ELSE '' END,
    COALESCE(a.error_message, ''),
    NOW()
FROM accounts a
WHERE a.deleted_at IS NULL
  AND a.platform = 'openai'
ON CONFLICT (account_id) DO NOTHING;
