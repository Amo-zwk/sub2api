package repository

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
)

type accountHealthRepository struct {
	db *sql.DB
}

func NewAccountHealthRepository(db *sql.DB) service.AccountHealthRepository {
	return &accountHealthRepository{db: db}
}

func (r *accountHealthRepository) EnsureAccounts(ctx context.Context) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO account_health_states (account_id, state, probe_class, next_probe_at)
		SELECT a.id, 'unknown', 'new', NOW()
		FROM accounts a
		WHERE a.deleted_at IS NULL AND a.platform = 'openai'
		ON CONFLICT (account_id) DO NOTHING
	`)
	return err
}

func (r *accountHealthRepository) CleanupStale(ctx context.Context) (int64, error) {
	result, err := r.db.ExecContext(ctx, `
		DELETE FROM account_health_states h
		WHERE NOT EXISTS (
			SELECT 1 FROM accounts a
			WHERE a.id = h.account_id
			  AND a.deleted_at IS NULL
			  AND a.platform = 'openai'
		)
	`)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

func (r *accountHealthRepository) SyncAccounts(ctx context.Context, accountIDs []int64, reset bool, autoAssign bool, groupID int64) (int64, error) {
	if len(accountIDs) == 0 {
		return 0, nil
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()

	ids := pq.Array(accountIDs)
	if _, err := tx.ExecContext(ctx, `
		DELETE FROM account_health_states h
		WHERE h.account_id = ANY($1)
		  AND NOT EXISTS (
			SELECT 1 FROM accounts a
			WHERE a.id = h.account_id
			  AND a.deleted_at IS NULL
			  AND a.platform = 'openai'
		  )
	`, ids); err != nil {
		return 0, err
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO account_health_states (account_id, state, probe_class, next_probe_at)
		SELECT a.id, 'unknown', 'new', NOW()
		FROM accounts a
		WHERE a.id = ANY($1)
		  AND a.deleted_at IS NULL
		  AND a.platform = 'openai'
		ON CONFLICT (account_id) DO UPDATE
		SET state = CASE WHEN $2 THEN 'unknown' ELSE account_health_states.state END,
			probe_class = CASE WHEN $2 THEN 'new' ELSE account_health_states.probe_class END,
			consecutive_successes = CASE WHEN $2 THEN 0 ELSE account_health_states.consecutive_successes END,
			consecutive_failures = CASE WHEN $2 THEN 0 ELSE account_health_states.consecutive_failures END,
			next_probe_at = CASE WHEN $2 THEN NOW() ELSE account_health_states.next_probe_at END,
			lease_until = CASE WHEN $2 THEN NULL ELSE account_health_states.lease_until END,
			updated_at = NOW()
	`, ids, reset); err != nil {
		return 0, err
	}

	if !autoAssign {
		if err := tx.Commit(); err != nil {
			return 0, err
		}
		return 0, nil
	}
	if groupID <= 0 {
		return 0, fmt.Errorf("target_group_id must be positive")
	}
	var valid bool
	if err := tx.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM groups
			WHERE id = $1 AND deleted_at IS NULL AND status = $2 AND platform = $3
		)
	`, groupID, service.StatusActive, service.PlatformOpenAI).Scan(&valid); err != nil {
		return 0, err
	}
	if !valid {
		return 0, fmt.Errorf("target_group_id %d must reference an active OpenAI group", groupID)
	}

	rows, err := tx.QueryContext(ctx, `
		INSERT INTO account_groups (account_id, group_id, priority)
		SELECT a.id, $2,
			COALESCE((SELECT MAX(existing.priority) + 1 FROM account_groups existing WHERE existing.account_id = a.id), 1)
		FROM accounts a
		WHERE a.id = ANY($1)
		  AND a.deleted_at IS NULL
		  AND a.platform = $3
		ON CONFLICT (account_id, group_id) DO NOTHING
		RETURNING account_id
	`, ids, groupID, service.PlatformOpenAI)
	if err != nil {
		return 0, err
	}
	assignedIDs := make([]int64, 0, len(accountIDs))
	for rows.Next() {
		var accountID int64
		if err := rows.Scan(&accountID); err != nil {
			_ = rows.Close()
			return 0, err
		}
		assignedIDs = append(assignedIDs, accountID)
	}
	if err := rows.Close(); err != nil {
		return 0, err
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	payload := buildSchedulerGroupPayload([]int64{groupID})
	for _, accountID := range assignedIDs {
		id := accountID
		if err := enqueueSchedulerOutbox(ctx, tx, service.SchedulerOutboxEventAccountGroupsChanged, &id, nil, payload); err != nil {
			return 0, err
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return int64(len(assignedIDs)), nil
}

func (r *accountHealthRepository) ValidateTargetGroup(ctx context.Context, groupID int64) error {
	if groupID <= 0 {
		return fmt.Errorf("target_group_id must be positive")
	}
	var valid bool
	if err := r.db.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM groups
			WHERE id = $1
			  AND deleted_at IS NULL
			  AND status = $2
			  AND platform = $3
		)
	`, groupID, service.StatusActive, service.PlatformOpenAI).Scan(&valid); err != nil {
		return err
	}
	if !valid {
		return fmt.Errorf("target_group_id %d must reference an active OpenAI group", groupID)
	}
	return nil
}

func (r *accountHealthRepository) AssignMissingToGroup(ctx context.Context, groupID int64) (int64, error) {
	if groupID <= 0 {
		return 0, fmt.Errorf("target_group_id must be positive")
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()

	var valid bool
	if err := tx.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM groups
			WHERE id = $1 AND deleted_at IS NULL AND status = $2 AND platform = $3
		)
	`, groupID, service.StatusActive, service.PlatformOpenAI).Scan(&valid); err != nil {
		return 0, err
	}
	if !valid {
		return 0, fmt.Errorf("target_group_id %d must reference an active OpenAI group", groupID)
	}

	rows, err := tx.QueryContext(ctx, `
		INSERT INTO account_groups (account_id, group_id, priority)
		SELECT
			a.id,
			$1,
			COALESCE((
				SELECT MAX(existing.priority) + 1
				FROM account_groups existing
				WHERE existing.account_id = a.id
			), 1)
		FROM accounts a
		WHERE a.deleted_at IS NULL
		  AND a.platform = $2
		ON CONFLICT (account_id, group_id) DO NOTHING
		RETURNING account_id
	`, groupID, service.PlatformOpenAI)
	if err != nil {
		return 0, err
	}
	accountIDs := make([]int64, 0)
	for rows.Next() {
		var accountID int64
		if err := rows.Scan(&accountID); err != nil {
			_ = rows.Close()
			return 0, err
		}
		accountIDs = append(accountIDs, accountID)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return 0, err
	}
	if err := rows.Close(); err != nil {
		return 0, err
	}

	payload := buildSchedulerGroupPayload([]int64{groupID})
	for _, accountID := range accountIDs {
		id := accountID
		if err := enqueueSchedulerOutbox(ctx, tx, service.SchedulerOutboxEventAccountGroupsChanged, &id, nil, payload); err != nil {
			return 0, err
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return int64(len(accountIDs)), nil
}

func (r *accountHealthRepository) LeaseDue(ctx context.Context, probeClass string, limit int, leaseUntil time.Time) ([]service.AccountHealthCandidate, error) {
	if limit <= 0 {
		return nil, nil
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	rows, err := tx.QueryContext(ctx, `
		WITH due AS (
			SELECT h.account_id
			FROM account_health_states h
			JOIN accounts a ON a.id = h.account_id
			WHERE a.deleted_at IS NULL
			  AND a.platform = 'openai'
			  AND h.next_probe_at <= NOW()
			  AND h.probe_class = $3
			  AND (h.lease_until IS NULL OR h.lease_until <= NOW())
			  AND (
				a.status = 'error'
				OR h.auto_blocked IS TRUE
				OR (a.status = 'active' AND a.schedulable IS TRUE)
			  )
			ORDER BY h.next_probe_at,
			  h.account_id
			FOR UPDATE OF h SKIP LOCKED
			LIMIT $1
		)
		UPDATE account_health_states h
		SET lease_until = $2,
			updated_at = NOW()
		FROM due, accounts a
		WHERE h.account_id = due.account_id AND a.id = h.account_id
		RETURNING h.account_id, a.name, a.status, a.schedulable, h.auto_blocked, h.state, h.probe_class, h.consecutive_failures
	`, limit, leaseUntil, probeClass)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]service.AccountHealthCandidate, 0, limit)
	for rows.Next() {
		var item service.AccountHealthCandidate
		if err := rows.Scan(&item.AccountID, &item.Name, &item.Status, &item.Schedulable, &item.AutoBlocked, &item.State, &item.ProbeClass, &item.ConsecutiveFailures); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return items, nil
}

func (r *accountHealthRepository) RecordSuccess(ctx context.Context, accountID int64, latencyMs int64, successThreshold int, nextProbe time.Time) (*service.AccountHealthRecord, error) {
	if successThreshold < 1 {
		successThreshold = 1
	}
	record := &service.AccountHealthRecord{}
	err := r.db.QueryRowContext(ctx, `
		UPDATE account_health_states
		SET state = CASE WHEN consecutive_successes + 1 >= $3 THEN 'healthy' ELSE 'probation' END,
			probe_class = CASE WHEN consecutive_successes + 1 >= $3 THEN 'healthy_sample' ELSE 'transient' END,
			error_category = '',
			error_message = '',
			consecutive_successes = consecutive_successes + 1,
			consecutive_failures = 0,
			last_probe_at = NOW(),
			last_success_at = NOW(),
			next_probe_at = $4,
			probe_latency_ms = $2,
			lease_until = NULL,
			updated_at = NOW()
		WHERE account_id = $1
		RETURNING state, probe_class, consecutive_successes, consecutive_failures
	`, accountID, latencyMs, successThreshold, nextProbe).Scan(&record.State, &record.ProbeClass, &record.ConsecutiveSuccesses, &record.ConsecutiveFailures)
	return record, err
}

func (r *accountHealthRepository) RecordFailure(ctx context.Context, accountID int64, state, probeClass, category, message string, latencyMs int64, nextProbe time.Time) (*service.AccountHealthRecord, error) {
	record := &service.AccountHealthRecord{}
	err := r.db.QueryRowContext(ctx, `
		UPDATE account_health_states
		SET state = $2,
			probe_class = $3,
			error_category = $4,
			error_message = LEFT($5, 2000),
			consecutive_successes = 0,
			consecutive_failures = consecutive_failures + 1,
			last_probe_at = NOW(),
			next_probe_at = $7,
			probe_latency_ms = $6,
			lease_until = NULL,
			updated_at = NOW()
		WHERE account_id = $1
		RETURNING state, probe_class, consecutive_successes, consecutive_failures
	`, accountID, state, probeClass, category, message, latencyMs, nextProbe).Scan(&record.State, &record.ProbeClass, &record.ConsecutiveSuccesses, &record.ConsecutiveFailures)
	return record, err
}

func (r *accountHealthRepository) MarkAutoBlocked(ctx context.Context, accountID int64, state, probeClass string, nextProbe time.Time) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE account_health_states
		SET state = $2, probe_class = $3, auto_blocked = TRUE, next_probe_at = $4, lease_until = NULL, updated_at = NOW()
		WHERE account_id = $1
	`, accountID, state, probeClass, nextProbe)
	return err
}

func (r *accountHealthRepository) MarkRecovered(ctx context.Context, accountID int64) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE account_health_states
		SET state = 'healthy', probe_class = 'healthy_sample', auto_blocked = FALSE, error_category = '', error_message = '', lease_until = NULL, updated_at = NOW()
		WHERE account_id = $1
	`, accountID)
	return err
}

func (r *accountHealthRepository) ReleaseLease(ctx context.Context, accountID int64, nextProbe time.Time, message string) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE account_health_states
		SET state = 'transient_error', probe_class = 'transient', error_category = 'recovery', error_message = LEFT($3, 2000), next_probe_at = $2, lease_until = NULL, updated_at = NOW()
		WHERE account_id = $1
	`, accountID, nextProbe, message)
	return err
}

func (r *accountHealthRepository) Summary(ctx context.Context, groupID int64) (*service.AccountHealthSummary, error) {
	summary := &service.AccountHealthSummary{}
	err := r.db.QueryRowContext(ctx, `
		SELECT
			COUNT(*),
			COUNT(*) FILTER (WHERE a.status = 'active' AND a.schedulable IS TRUE
				AND (a.expires_at IS NULL OR a.auto_pause_on_expired IS NOT TRUE OR a.expires_at > NOW())
				AND (a.rate_limit_reset_at IS NULL OR a.rate_limit_reset_at <= NOW())
				AND (a.overload_until IS NULL OR a.overload_until <= NOW())
				AND (a.temp_unschedulable_until IS NULL OR a.temp_unschedulable_until <= NOW())),
			COUNT(*) FILTER (WHERE COALESCE(h.state, 'unknown') = 'healthy'),
			COUNT(*) FILTER (WHERE COALESCE(h.state, 'unknown') = 'transient_error'),
			COUNT(*) FILTER (WHERE COALESCE(h.state, 'unknown') = 'probation'),
			COUNT(*) FILTER (WHERE COALESCE(h.state, 'unknown') IN ('auth_quarantine', 'entitlement_quarantine', 'permission_quarantine')),
			COUNT(*) FILTER (WHERE COALESCE(h.state, 'unknown') = 'unknown'),
			COUNT(*) FILTER (WHERE COALESCE(h.state, 'unknown') = 'probation'),
			COUNT(*) FILTER (WHERE COALESCE(h.state, 'unknown') = 'transient_error'),
			COUNT(*) FILTER (WHERE COALESCE(h.state, 'unknown') = 'auth_quarantine'),
			COUNT(*) FILTER (WHERE COALESCE(h.state, 'unknown') = 'entitlement_quarantine'),
			COUNT(*) FILTER (WHERE COALESCE(h.state, 'unknown') = 'permission_quarantine')
		FROM accounts a
		LEFT JOIN account_health_states h ON h.account_id = a.id
		WHERE a.deleted_at IS NULL
		  AND a.platform = 'openai'
		  AND ($1::BIGINT = 0 OR EXISTS (
			SELECT 1 FROM account_groups ag WHERE ag.account_id = a.id AND ag.group_id = $1
		  ))
	`, groupID).Scan(
		&summary.Total,
		&summary.SchedulerEligible,
		&summary.Healthy,
		&summary.Degraded,
		&summary.Recovering,
		&summary.Blocked,
		&summary.Unknown,
		&summary.Probation,
		&summary.TransientError,
		&summary.AuthQuarantine,
		&summary.EntitlementQuarantine,
		&summary.PermissionQuarantine,
	)
	return summary, err
}

func (r *accountHealthRepository) List(ctx context.Context, params service.AccountHealthListParams) ([]service.AccountHealthItem, int64, error) {
	if params.Page < 1 {
		params.Page = 1
	}
	if params.PageSize < 1 || params.PageSize > 200 {
		params.PageSize = 50
	}
	where, args := healthListWhere(params)
	var total int64
	if err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM accounts a LEFT JOIN account_health_states h ON h.account_id = a.id `+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	args = append(args, params.PageSize, (params.Page-1)*params.PageSize)
	limitPos := len(args) - 1
	offsetPos := len(args)
	query := fmt.Sprintf(`
		SELECT
			a.id, a.name, a.status, a.schedulable,
			(a.status = 'active' AND a.schedulable IS TRUE
			 AND (a.expires_at IS NULL OR a.auto_pause_on_expired IS NOT TRUE OR a.expires_at > NOW())
			 AND (a.rate_limit_reset_at IS NULL OR a.rate_limit_reset_at <= NOW())
			 AND (a.overload_until IS NULL OR a.overload_until <= NOW())
			 AND (a.temp_unschedulable_until IS NULL OR a.temp_unschedulable_until <= NOW())),
			COALESCE(h.state, 'unknown'), COALESCE(h.probe_class, 'new'), COALESCE(h.auto_blocked, FALSE),
			COALESCE(h.error_category, ''), COALESCE(h.error_message, ''),
			COALESCE(h.consecutive_successes, 0), COALESCE(h.consecutive_failures, 0),
			h.last_probe_at, h.last_success_at, h.next_probe_at, COALESCE(h.probe_latency_ms, 0),
			COALESCE(ARRAY_AGG(DISTINCT g.name) FILTER (WHERE g.name IS NOT NULL), ARRAY[]::TEXT[])
		FROM accounts a
		LEFT JOIN account_health_states h ON h.account_id = a.id
		LEFT JOIN account_groups ag ON ag.account_id = a.id
		LEFT JOIN groups g ON g.id = ag.group_id AND g.deleted_at IS NULL
		%s
		GROUP BY a.id, h.account_id
		ORDER BY CASE COALESCE(h.state, 'unknown') WHEN 'auth_quarantine' THEN 0 WHEN 'entitlement_quarantine' THEN 1 WHEN 'permission_quarantine' THEN 2 WHEN 'transient_error' THEN 3 WHEN 'probation' THEN 4 WHEN 'unknown' THEN 5 ELSE 6 END,
		         h.last_probe_at DESC NULLS FIRST, a.id
		LIMIT $%d OFFSET $%d
	`, where, limitPos, offsetPos)
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := make([]service.AccountHealthItem, 0, params.PageSize)
	for rows.Next() {
		var item service.AccountHealthItem
		var groups pq.StringArray
		if err := rows.Scan(
			&item.AccountID, &item.Name, &item.AccountStatus, &item.Schedulable, &item.SchedulerEligible,
			&item.State, &item.ProbeClass, &item.AutoBlocked, &item.ErrorCategory, &item.ErrorMessage,
			&item.ConsecutiveSuccesses, &item.ConsecutiveFailures, &item.LastProbeAt, &item.LastSuccessAt,
			&item.NextProbeAt, &item.ProbeLatencyMs, &groups,
		); err != nil {
			return nil, 0, err
		}
		item.GroupNames = []string(groups)
		items = append(items, item)
	}
	return items, total, rows.Err()
}

func healthListWhere(params service.AccountHealthListParams) (string, []any) {
	clauses := []string{"a.deleted_at IS NULL", "a.platform = 'openai'"}
	args := make([]any, 0, 3)
	if search := strings.TrimSpace(params.Search); search != "" {
		args = append(args, "%"+search+"%")
		clauses = append(clauses, fmt.Sprintf("a.name ILIKE $%d", len(args)))
	}
	if state := strings.TrimSpace(params.State); state != "" {
		args = append(args, state)
		clauses = append(clauses, fmt.Sprintf("COALESCE(h.state, 'unknown') = $%d", len(args)))
	}
	if params.GroupID > 0 {
		args = append(args, params.GroupID)
		clauses = append(clauses, fmt.Sprintf("EXISTS (SELECT 1 FROM account_groups agf WHERE agf.account_id = a.id AND agf.group_id = $%d)", len(args)))
	}
	return "WHERE " + strings.Join(clauses, " AND "), args
}

func (r *accountHealthRepository) ForceDue(ctx context.Context, groupID int64) (int64, error) {
	result, err := r.db.ExecContext(ctx, `
		UPDATE account_health_states h
		SET next_probe_at = NOW(), lease_until = NULL, updated_at = NOW()
		FROM accounts a
		WHERE a.id = h.account_id
		  AND a.deleted_at IS NULL
		  AND a.platform = 'openai'
		  AND (a.status = 'error' OR h.auto_blocked IS TRUE OR (a.status = 'active' AND a.schedulable IS TRUE))
		  AND ($1::BIGINT = 0 OR EXISTS (
			SELECT 1 FROM account_groups ag WHERE ag.account_id = a.id AND ag.group_id = $1
		  ))
	`, groupID)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}
