package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
)

const accountHealthSettingsKey = "account_health_controller_settings"

const (
	AccountHealthUnknown               = "unknown"
	AccountHealthHealthy               = "healthy"
	AccountHealthProbation             = "probation"
	AccountHealthTransientError        = "transient_error"
	AccountHealthAuthQuarantine        = "auth_quarantine"
	AccountHealthEntitlementQuarantine = "entitlement_quarantine"
	AccountHealthPermissionQuarantine  = "permission_quarantine"

	AccountHealthProbeNew            = "new"
	AccountHealthProbeTransient      = "transient"
	AccountHealthProbeHealthySample  = "healthy_sample"
	AccountHealthProbeTerminalCanary = "terminal_canary"
)

type AccountHealthSettings struct {
	Enabled                    bool   `json:"enabled"`
	AutoAssignGroup            bool   `json:"auto_assign_group"`
	TargetGroupID              int64  `json:"target_group_id"`
	WorkerCount                int    `json:"worker_count"`
	BatchSize                  int    `json:"batch_size"`
	DispatchIntervalSeconds    int    `json:"dispatch_interval_seconds"`
	HealthyIntervalSeconds     int    `json:"healthy_interval_seconds"`
	RecoveryIntervalSeconds    int    `json:"recovery_interval_seconds"`
	AuthIntervalSeconds        int    `json:"auth_interval_seconds"`
	EntitlementIntervalSeconds int    `json:"entitlement_interval_seconds"`
	PermissionIntervalSeconds  int    `json:"permission_interval_seconds"`
	ReconcileIntervalSeconds   int    `json:"reconcile_interval_seconds"`
	MaxProbeQPS                int    `json:"max_probe_qps"`
	FailureThreshold           int    `json:"failure_threshold"`
	SuccessThreshold           int    `json:"success_threshold"`
	TimeoutSeconds             int    `json:"timeout_seconds"`
	ModelID                    string `json:"model_id"`
	AutoRecover                bool   `json:"auto_recover"`
	AutoBlock                  bool   `json:"auto_block"`
}

func DefaultAccountHealthSettings() AccountHealthSettings {
	return AccountHealthSettings{
		Enabled:                    true,
		WorkerCount:                64,
		BatchSize:                  256,
		DispatchIntervalSeconds:    1,
		HealthyIntervalSeconds:     900,
		RecoveryIntervalSeconds:    10,
		AuthIntervalSeconds:        1800,
		EntitlementIntervalSeconds: 21600,
		PermissionIntervalSeconds:  3600,
		ReconcileIntervalSeconds:   600,
		MaxProbeQPS:                32,
		FailureThreshold:           3,
		SuccessThreshold:           3,
		TimeoutSeconds:             30,
		AutoRecover:                true,
		AutoBlock:                  true,
	}
}

func (s AccountHealthSettings) Validate() error {
	if s.TargetGroupID < 0 {
		return errors.New("target_group_id must not be negative")
	}
	if s.AutoAssignGroup && s.TargetGroupID == 0 {
		return errors.New("target_group_id is required when auto_assign_group is enabled")
	}
	checks := []struct {
		name     string
		value    int
		min, max int
	}{
		{"worker_count", s.WorkerCount, 1, 256},
		{"batch_size", s.BatchSize, 1, 1000},
		{"dispatch_interval_seconds", s.DispatchIntervalSeconds, 1, 60},
		{"healthy_interval_seconds", s.HealthyIntervalSeconds, 15, 86400},
		{"recovery_interval_seconds", s.RecoveryIntervalSeconds, 3, 3600},
		{"auth_interval_seconds", s.AuthIntervalSeconds, 60, 86400},
		{"entitlement_interval_seconds", s.EntitlementIntervalSeconds, 300, 604800},
		{"permission_interval_seconds", s.PermissionIntervalSeconds, 60, 86400},
		{"reconcile_interval_seconds", s.ReconcileIntervalSeconds, 60, 86400},
		{"max_probe_qps", s.MaxProbeQPS, 1, 1000},
		{"failure_threshold", s.FailureThreshold, 1, 20},
		{"success_threshold", s.SuccessThreshold, 1, 10},
		{"timeout_seconds", s.TimeoutSeconds, 5, 300},
	}
	for _, check := range checks {
		if check.value < check.min || check.value > check.max {
			return fmt.Errorf("%s must be between %d and %d", check.name, check.min, check.max)
		}
	}
	return nil
}

func normalizeAccountHealthSettings(settings AccountHealthSettings) AccountHealthSettings {
	defaults := DefaultAccountHealthSettings()
	if settings.WorkerCount == 0 {
		settings.WorkerCount = defaults.WorkerCount
	}
	if settings.BatchSize == 0 {
		settings.BatchSize = defaults.BatchSize
	}
	if settings.DispatchIntervalSeconds == 0 {
		settings.DispatchIntervalSeconds = defaults.DispatchIntervalSeconds
	}
	if settings.HealthyIntervalSeconds == 0 {
		settings.HealthyIntervalSeconds = defaults.HealthyIntervalSeconds
	}
	if settings.RecoveryIntervalSeconds == 0 {
		settings.RecoveryIntervalSeconds = defaults.RecoveryIntervalSeconds
	}
	if settings.AuthIntervalSeconds == 0 {
		settings.AuthIntervalSeconds = defaults.AuthIntervalSeconds
	}
	if settings.EntitlementIntervalSeconds == 0 {
		settings.EntitlementIntervalSeconds = defaults.EntitlementIntervalSeconds
	}
	if settings.PermissionIntervalSeconds == 0 {
		settings.PermissionIntervalSeconds = defaults.PermissionIntervalSeconds
	}
	if settings.ReconcileIntervalSeconds == 0 {
		settings.ReconcileIntervalSeconds = defaults.ReconcileIntervalSeconds
	}
	if settings.MaxProbeQPS == 0 {
		settings.MaxProbeQPS = defaults.MaxProbeQPS
	}
	if settings.FailureThreshold == 0 {
		settings.FailureThreshold = defaults.FailureThreshold
	}
	if settings.SuccessThreshold == 0 {
		settings.SuccessThreshold = defaults.SuccessThreshold
	}
	if settings.TimeoutSeconds == 0 {
		settings.TimeoutSeconds = defaults.TimeoutSeconds
	}
	return settings
}

type AccountHealthCandidate struct {
	AccountID           int64
	Name                string
	Status              string
	Schedulable         bool
	AutoBlocked         bool
	State               string
	ProbeClass          string
	ConsecutiveFailures int
}

type AccountHealthRecord struct {
	State                string `json:"state"`
	ProbeClass           string `json:"probe_class"`
	ConsecutiveSuccesses int    `json:"consecutive_successes"`
	ConsecutiveFailures  int    `json:"consecutive_failures"`
}

type AccountHealthSummary struct {
	Total                 int64 `json:"total"`
	SchedulerEligible     int64 `json:"scheduler_eligible"`
	Healthy               int64 `json:"healthy"`
	Degraded              int64 `json:"degraded"`
	Recovering            int64 `json:"recovering"`
	Blocked               int64 `json:"blocked"`
	Unknown               int64 `json:"unknown"`
	Probation             int64 `json:"probation"`
	TransientError        int64 `json:"transient_error"`
	AuthQuarantine        int64 `json:"auth_quarantine"`
	EntitlementQuarantine int64 `json:"entitlement_quarantine"`
	PermissionQuarantine  int64 `json:"permission_quarantine"`
	InFlight              int64 `json:"in_flight"`
}

type AccountHealthListParams struct {
	Page     int
	PageSize int
	Search   string
	State    string
	GroupID  int64
}

type AccountHealthItem struct {
	AccountID            int64      `json:"account_id"`
	Name                 string     `json:"name"`
	AccountStatus        string     `json:"account_status"`
	Schedulable          bool       `json:"schedulable"`
	SchedulerEligible    bool       `json:"scheduler_eligible"`
	State                string     `json:"state"`
	ProbeClass           string     `json:"probe_class"`
	AutoBlocked          bool       `json:"auto_blocked"`
	ErrorCategory        string     `json:"error_category"`
	ErrorMessage         string     `json:"error_message"`
	ConsecutiveSuccesses int        `json:"consecutive_successes"`
	ConsecutiveFailures  int        `json:"consecutive_failures"`
	LastProbeAt          *time.Time `json:"last_probe_at"`
	LastSuccessAt        *time.Time `json:"last_success_at"`
	NextProbeAt          *time.Time `json:"next_probe_at"`
	ProbeLatencyMs       int64      `json:"probe_latency_ms"`
	GroupNames           []string   `json:"group_names"`
}

type AccountHealthRepository interface {
	EnsureAccounts(ctx context.Context) error
	CleanupStale(ctx context.Context) (int64, error)
	SyncAccounts(ctx context.Context, accountIDs []int64, reset bool, autoAssign bool, groupID int64) (int64, error)
	ValidateTargetGroup(ctx context.Context, groupID int64) error
	AssignMissingToGroup(ctx context.Context, groupID int64) (int64, error)
	LeaseDue(ctx context.Context, probeClass string, limit int, leaseUntil time.Time) ([]AccountHealthCandidate, error)
	RecordSuccess(ctx context.Context, accountID int64, latencyMs int64, successThreshold int, nextProbe time.Time) (*AccountHealthRecord, error)
	RecordFailure(ctx context.Context, accountID int64, state, probeClass, category, message string, latencyMs int64, nextProbe time.Time) (*AccountHealthRecord, error)
	MarkAutoBlocked(ctx context.Context, accountID int64, state, probeClass string, nextProbe time.Time) error
	MarkRecovered(ctx context.Context, accountID int64) error
	ReleaseLease(ctx context.Context, accountID int64, nextProbe time.Time, message string) error
	Summary(ctx context.Context, groupID int64) (*AccountHealthSummary, error)
	List(ctx context.Context, params AccountHealthListParams) ([]AccountHealthItem, int64, error)
	ForceDue(ctx context.Context, groupID int64) (int64, error)
}

type AccountHealthProbe interface {
	RunTestBackground(ctx context.Context, accountID int64, modelID string) (*ScheduledTestResult, error)
}

type AccountHealthEvent struct {
	Type      string `json:"type"`
	AccountID int64  `json:"account_id,omitempty"`
	State     string `json:"state,omitempty"`
	Message   string `json:"message,omitempty"`
}

type accountHealthProbeLimiter struct {
	mu     sync.Mutex
	rate   float64
	burst  float64
	tokens float64
	last   time.Time
}

func newAccountHealthProbeLimiter(perSecond, burst int) *accountHealthProbeLimiter {
	now := time.Now()
	return &accountHealthProbeLimiter{rate: float64(perSecond), burst: float64(burst), tokens: float64(burst), last: now}
}

func (l *accountHealthProbeLimiter) configure(perSecond, burst int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.refillLocked(time.Now())
	l.rate = float64(perSecond)
	l.burst = float64(burst)
	if l.tokens > l.burst {
		l.tokens = l.burst
	}
}

func (l *accountHealthProbeLimiter) take(max int) int {
	if max <= 0 {
		return 0
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.refillLocked(time.Now())
	allowed := int(l.tokens)
	if allowed > max {
		allowed = max
	}
	l.tokens -= float64(allowed)
	return allowed
}

func (l *accountHealthProbeLimiter) refillLocked(now time.Time) {
	if now.Before(l.last) {
		l.last = now
		return
	}
	l.tokens += now.Sub(l.last).Seconds() * l.rate
	if l.tokens > l.burst {
		l.tokens = l.burst
	}
	l.last = now
}

type accountHealthEventHub struct {
	mu          sync.RWMutex
	subscribers map[chan AccountHealthEvent]struct{}
}

func newAccountHealthEventHub() *accountHealthEventHub {
	return &accountHealthEventHub{subscribers: make(map[chan AccountHealthEvent]struct{})}
}

func (h *accountHealthEventHub) subscribe() (<-chan AccountHealthEvent, func()) {
	ch := make(chan AccountHealthEvent, 32)
	h.mu.Lock()
	h.subscribers[ch] = struct{}{}
	h.mu.Unlock()
	return ch, func() {
		h.mu.Lock()
		if _, ok := h.subscribers[ch]; ok {
			delete(h.subscribers, ch)
			close(ch)
		}
		h.mu.Unlock()
	}
}

func (h *accountHealthEventHub) publish(event AccountHealthEvent) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for ch := range h.subscribers {
		select {
		case ch <- event:
		default:
		}
	}
}

type AccountHealthController struct {
	repo          AccountHealthRepository
	accountRepo   AccountRepository
	probe         AccountHealthProbe
	rateLimitSvc  *RateLimitService
	settingRepo   SettingRepository
	settings      atomic.Pointer[AccountHealthSettings]
	inFlight      atomic.Int64
	hub           *accountHealthEventHub
	ctx           context.Context
	cancel        context.CancelFunc
	wake          chan struct{}
	probeLimiter  *accountHealthProbeLimiter
	lastReconcile atomic.Int64
	startOnce     sync.Once
	stopOnce      sync.Once
	wg            sync.WaitGroup
}

func NewAccountHealthController(repo AccountHealthRepository, accountRepo AccountRepository, probe AccountHealthProbe, rateLimitSvc *RateLimitService, settingRepo SettingRepository) *AccountHealthController {
	ctx, cancel := context.WithCancel(context.Background())
	controller := &AccountHealthController{
		repo: repo, accountRepo: accountRepo, probe: probe, rateLimitSvc: rateLimitSvc,
		settingRepo: settingRepo, hub: newAccountHealthEventHub(), ctx: ctx, cancel: cancel,
		wake: make(chan struct{}, 1),
	}
	defaults := DefaultAccountHealthSettings()
	controller.probeLimiter = newAccountHealthProbeLimiter(defaults.MaxProbeQPS, defaults.WorkerCount)
	controller.settings.Store(&defaults)
	return controller
}

func (s *AccountHealthController) Start() {
	if s == nil {
		return
	}
	s.startOnce.Do(func() {
		if err := s.reloadSettings(s.ctx); err != nil {
			logger.LegacyPrintf("service.account_health", "[AccountHealth] load settings failed, using defaults: %v", err)
		}
		if err := s.reconcile(s.ctx, s.Settings()); err != nil {
			logger.LegacyPrintf("service.account_health", "[AccountHealth] initial reconcile failed: %v", err)
		}
		s.wg.Add(2)
		go s.dispatchLoop()
		go s.settingsLoop()
		logger.LegacyPrintf("service.account_health", "[AccountHealth] started workers=%d", s.Settings().WorkerCount)
	})
}

func (s *AccountHealthController) Stop() {
	if s == nil {
		return
	}
	s.stopOnce.Do(func() {
		s.cancel()
		s.wg.Wait()
	})
}

func (s *AccountHealthController) Settings() AccountHealthSettings {
	current := s.settings.Load()
	if current == nil {
		return DefaultAccountHealthSettings()
	}
	return *current
}

func (s *AccountHealthController) UpdateSettings(ctx context.Context, settings AccountHealthSettings) error {
	settings = normalizeAccountHealthSettings(settings)
	settings.ModelID = strings.TrimSpace(settings.ModelID)
	if err := settings.Validate(); err != nil {
		return err
	}
	if settings.AutoAssignGroup {
		if err := s.repo.ValidateTargetGroup(ctx, settings.TargetGroupID); err != nil {
			return err
		}
	}
	raw, err := json.Marshal(settings)
	if err != nil {
		return err
	}
	if err := s.settingRepo.Set(ctx, accountHealthSettingsKey, string(raw)); err != nil {
		return err
	}
	s.settings.Store(&settings)
	s.configureProbeLimiter(settings)
	if err := s.reconcile(ctx, settings); err != nil {
		return err
	}
	s.hub.publish(AccountHealthEvent{Type: "settings"})
	s.signal()
	return nil
}

func (s *AccountHealthController) Summary(ctx context.Context, groupID int64) (*AccountHealthSummary, error) {
	summary, err := s.repo.Summary(ctx, groupID)
	if err != nil {
		return nil, err
	}
	summary.InFlight = s.inFlight.Load()
	return summary, nil
}

func (s *AccountHealthController) List(ctx context.Context, params AccountHealthListParams) ([]AccountHealthItem, int64, error) {
	return s.repo.List(ctx, params)
}

func (s *AccountHealthController) RunNow(ctx context.Context, groupID int64) (int64, error) {
	if err := s.reconcile(ctx, s.Settings()); err != nil {
		return 0, err
	}
	count, err := s.repo.ForceDue(ctx, groupID)
	if err == nil {
		s.signal()
		s.hub.publish(AccountHealthEvent{Type: "run", Message: fmt.Sprintf("queued %d accounts", count)})
	}
	return count, err
}

func (s *AccountHealthController) NotifyAccountsChanged(ctx context.Context, accountIDs []int64, reset bool) error {
	if s == nil || len(accountIDs) == 0 {
		return nil
	}
	settings := s.Settings()
	assigned, err := s.repo.SyncAccounts(ctx, accountIDs, reset, settings.AutoAssignGroup, settings.TargetGroupID)
	if err != nil {
		return err
	}
	s.hub.publish(AccountHealthEvent{Type: "sync", Message: fmt.Sprintf("accounts=%d assigned=%d", len(accountIDs), assigned)})
	s.signal()
	return nil
}

func (s *AccountHealthController) Subscribe() (<-chan AccountHealthEvent, func()) {
	return s.hub.subscribe()
}

func (s *AccountHealthController) settingsLoop() {
	defer s.wg.Done()
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
			if err := s.reloadSettings(s.ctx); err != nil && !errors.Is(err, ErrSettingNotFound) {
				logger.LegacyPrintf("service.account_health", "[AccountHealth] hot reload failed: %v", err)
			}
		}
	}
}

func (s *AccountHealthController) reloadSettings(ctx context.Context) error {
	raw, err := s.settingRepo.GetValue(ctx, accountHealthSettingsKey)
	if err != nil {
		if errors.Is(err, ErrSettingNotFound) {
			defaults := DefaultAccountHealthSettings()
			s.settings.Store(&defaults)
		}
		return err
	}
	settings := DefaultAccountHealthSettings()
	if err := json.Unmarshal([]byte(raw), &settings); err != nil {
		return err
	}
	if err := settings.Validate(); err != nil {
		return err
	}
	if settings.AutoAssignGroup {
		if err := s.repo.ValidateTargetGroup(ctx, settings.TargetGroupID); err != nil {
			return err
		}
	}
	settings.ModelID = strings.TrimSpace(settings.ModelID)
	current := s.Settings()
	if current != settings {
		s.settings.Store(&settings)
		s.configureProbeLimiter(settings)
		s.signal()
		s.hub.publish(AccountHealthEvent{Type: "settings"})
	}
	return nil
}

func (s *AccountHealthController) dispatchLoop() {
	defer s.wg.Done()
	for {
		settings := s.Settings()
		timer := time.NewTimer(time.Duration(settings.DispatchIntervalSeconds) * time.Second)
		select {
		case <-s.ctx.Done():
			timer.Stop()
			return
		case <-s.wake:
			timer.Stop()
		case <-timer.C:
		}
		settings = s.Settings()
		lastReconcile := time.Unix(0, s.lastReconcile.Load())
		if time.Since(lastReconcile) >= time.Duration(settings.ReconcileIntervalSeconds)*time.Second {
			if err := s.reconcile(s.ctx, settings); err != nil {
				logger.LegacyPrintf("service.account_health", "[AccountHealth] reconcile failed: %v", err)
			}
		}
		if !settings.Enabled {
			continue
		}
		s.dispatch(settings)
	}
}

func (s *AccountHealthController) reconcile(ctx context.Context, settings AccountHealthSettings) error {
	if cleaned, err := s.repo.CleanupStale(ctx); err != nil {
		return fmt.Errorf("cleanup stale account health states: %w", err)
	} else if cleaned > 0 {
		logger.LegacyPrintf("service.account_health", "[AccountHealth] cleaned stale states=%d", cleaned)
	}
	if err := s.repo.EnsureAccounts(ctx); err != nil {
		return fmt.Errorf("ensure account health states: %w", err)
	}
	s.lastReconcile.Store(time.Now().UnixNano())
	if !settings.AutoAssignGroup {
		return nil
	}
	assigned, err := s.repo.AssignMissingToGroup(ctx, settings.TargetGroupID)
	if err != nil {
		return fmt.Errorf("auto assign target group: %w", err)
	}
	if assigned > 0 {
		logger.LegacyPrintf("service.account_health", "[AccountHealth] auto assigned accounts=%d group=%d", assigned, settings.TargetGroupID)
		s.hub.publish(AccountHealthEvent{Type: "group", Message: fmt.Sprintf("assigned %d accounts", assigned)})
	}
	return nil
}

func (s *AccountHealthController) dispatch(settings AccountHealthSettings) {
	capacity := int64(settings.WorkerCount) - s.inFlight.Load()
	if capacity <= 0 {
		return
	}
	limit := settings.BatchSize
	if int64(limit) > capacity {
		limit = int(capacity)
	}
	limit = s.probeLimiter.take(limit)
	if limit <= 0 {
		return
	}
	leaseUntil := time.Now().Add(time.Duration(settings.TimeoutSeconds+30) * time.Second)
	terminalLimit := limit / 20
	if terminalLimit < 1 && limit >= 4 {
		terminalLimit = 1
	}
	newLimit := limit * 40 / 100
	transientLimit := limit * 35 / 100
	healthyLimit := limit - terminalLimit - newLimit - transientLimit
	plans := []struct {
		class string
		limit int
	}{
		{AccountHealthProbeNew, newLimit},
		{AccountHealthProbeTransient, transientLimit},
		{AccountHealthProbeTerminalCanary, terminalLimit},
		{AccountHealthProbeHealthySample, healthyLimit},
	}
	candidates := make([]AccountHealthCandidate, 0, limit)
	for _, plan := range plans {
		if plan.limit <= 0 {
			continue
		}
		leased, err := s.repo.LeaseDue(s.ctx, plan.class, plan.limit, leaseUntil)
		if err != nil {
			logger.LegacyPrintf("service.account_health", "[AccountHealth] lease due failed class=%s: %v", plan.class, err)
			continue
		}
		candidates = append(candidates, leased...)
	}
	remaining := limit - len(candidates)
	if remaining > 0 {
		for _, class := range []string{AccountHealthProbeNew, AccountHealthProbeTransient, AccountHealthProbeHealthySample} {
			leased, err := s.repo.LeaseDue(s.ctx, class, remaining, leaseUntil)
			if err != nil {
				continue
			}
			candidates = append(candidates, leased...)
			remaining = limit - len(candidates)
			if remaining <= 0 {
				break
			}
		}
	}
	for _, candidate := range candidates {
		s.inFlight.Add(1)
		s.wg.Add(1)
		go s.probeOne(candidate, settings)
	}
}

func (s *AccountHealthController) probeOne(candidate AccountHealthCandidate, settings AccountHealthSettings) {
	defer s.wg.Done()
	defer func() {
		s.inFlight.Add(-1)
		s.signal()
	}()

	probeCtx, cancelProbe := context.WithTimeout(s.ctx, time.Duration(settings.TimeoutSeconds)*time.Second)
	result, err := s.probe.RunTestBackground(probeCtx, candidate.AccountID, settings.ModelID)
	cancelProbe()

	// Persist the probe outcome with a fresh context. A timed-out probe context is
	// already canceled and cannot release its lease or schedule the recovery retry.
	persistCtx, cancelPersist := context.WithTimeout(s.ctx, 15*time.Second)
	defer cancelPersist()
	if err != nil || result == nil {
		message := "probe returned no result"
		if err != nil {
			message = err.Error()
		}
		s.handleFailure(persistCtx, candidate, settings, message, 0)
		return
	}
	if result.Status != "success" {
		s.handleFailure(persistCtx, candidate, settings, result.ErrorMessage, result.LatencyMs)
		return
	}

	nextDelay := time.Duration(settings.HealthyIntervalSeconds) * time.Second
	if candidate.AutoBlocked || candidate.State != AccountHealthHealthy {
		nextDelay = time.Duration(settings.RecoveryIntervalSeconds) * time.Second
	}
	next := time.Now().Add(accountHealthJitter(nextDelay, candidate.AccountID, candidate.ConsecutiveFailures))
	record, err := s.repo.RecordSuccess(persistCtx, candidate.AccountID, result.LatencyMs, settings.SuccessThreshold, next)
	if err != nil {
		logger.LegacyPrintf("service.account_health", "[AccountHealth] record success failed account=%d: %v", candidate.AccountID, err)
		return
	}
	needsRecovery := accountHealthCandidateNeedsRecovery(candidate)
	if needsRecovery && record.ConsecutiveSuccesses >= settings.SuccessThreshold && settings.AutoRecover && s.rateLimitSvc != nil {
		if _, err := s.rateLimitSvc.RecoverAccountAfterSuccessfulTest(persistCtx, candidate.AccountID); err != nil {
			_ = s.repo.ReleaseLease(persistCtx, candidate.AccountID, time.Now().Add(time.Duration(settings.RecoveryIntervalSeconds)*time.Second), err.Error())
			logger.LegacyPrintf("service.account_health", "[AccountHealth] recovery failed account=%d: %v", candidate.AccountID, err)
			return
		}
		if err := s.repo.MarkRecovered(persistCtx, candidate.AccountID); err != nil {
			logger.LegacyPrintf("service.account_health", "[AccountHealth] mark recovered failed account=%d: %v", candidate.AccountID, err)
		}
	}
	s.hub.publish(AccountHealthEvent{Type: "account", AccountID: candidate.AccountID, State: record.State})
}

func accountHealthCandidateNeedsRecovery(candidate AccountHealthCandidate) bool {
	return candidate.AutoBlocked || candidate.Status != StatusActive || !candidate.Schedulable
}

func (s *AccountHealthController) handleFailure(ctx context.Context, candidate AccountHealthCandidate, settings AccountHealthSettings, message string, latencyMs int64) {
	category := ClassifyAccountHealthError(message)
	state, probeClass, delay := accountHealthFailurePolicy(category, candidate.ConsecutiveFailures, settings)
	next := time.Now().Add(accountHealthJitter(delay, candidate.AccountID, candidate.ConsecutiveFailures))
	record, err := s.repo.RecordFailure(ctx, candidate.AccountID, state, probeClass, category, message, latencyMs, next)
	if err != nil {
		logger.LegacyPrintf("service.account_health", "[AccountHealth] record failure failed account=%d: %v", candidate.AccountID, err)
		return
	}
	state = record.State
	hardFailure := category == "authentication" || category == "entitlement" || category == "permission"
	if settings.AutoBlock && (hardFailure || record.ConsecutiveFailures >= settings.FailureThreshold) {
		if candidate.AutoBlocked {
			state = record.State
		} else if err := s.accountRepo.SetError(ctx, candidate.AccountID, "health-check["+category+"]: "+message); err != nil {
			logger.LegacyPrintf("service.account_health", "[AccountHealth] auto block failed account=%d: %v", candidate.AccountID, err)
		} else if err := s.repo.MarkAutoBlocked(ctx, candidate.AccountID, state, probeClass, next); err != nil {
			logger.LegacyPrintf("service.account_health", "[AccountHealth] persist auto block failed account=%d: %v", candidate.AccountID, err)
		} else {
			state = record.State
		}
	}
	s.hub.publish(AccountHealthEvent{Type: "account", AccountID: candidate.AccountID, State: state, Message: category})
}

func accountHealthRetryDelay(previousFailures int, settings AccountHealthSettings) time.Duration {
	base := time.Duration(settings.RecoveryIntervalSeconds) * time.Second
	schedule := []time.Duration{base, 3 * base, 12 * base, 30 * base}
	index := previousFailures
	if index >= len(schedule) {
		index = len(schedule) - 1
	}
	return schedule[index]
}

func accountHealthFailurePolicy(category string, previousFailures int, settings AccountHealthSettings) (string, string, time.Duration) {
	switch category {
	case "authentication":
		return AccountHealthAuthQuarantine, AccountHealthProbeTerminalCanary, time.Duration(settings.AuthIntervalSeconds) * time.Second
	case "entitlement":
		return AccountHealthEntitlementQuarantine, AccountHealthProbeTerminalCanary, time.Duration(settings.EntitlementIntervalSeconds) * time.Second
	case "permission", "model":
		return AccountHealthPermissionQuarantine, AccountHealthProbeTerminalCanary, time.Duration(settings.PermissionIntervalSeconds) * time.Second
	default:
		return AccountHealthTransientError, AccountHealthProbeTransient, accountHealthRetryDelay(previousFailures, settings)
	}
}

func accountHealthJitter(delay time.Duration, accountID int64, failures int) time.Duration {
	spread := delay / 10
	if spread <= 0 {
		return delay
	}
	seed := accountID*1103515245 + int64(failures+1)*12345
	if seed < 0 {
		seed = -seed
	}
	return delay + time.Duration(seed%int64(spread+1))
}

func (s *AccountHealthController) configureProbeLimiter(settings AccountHealthSettings) {
	if s.probeLimiter == nil {
		s.probeLimiter = newAccountHealthProbeLimiter(settings.MaxProbeQPS, settings.WorkerCount)
		return
	}
	s.probeLimiter.configure(settings.MaxProbeQPS, settings.WorkerCount)
}

func (s *AccountHealthController) signal() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

func ClassifyAccountHealthError(message string) string {
	lower := strings.ToLower(message)
	switch {
	case strings.Contains(lower, "402"), strings.Contains(lower, "deactivated_workspace"), strings.Contains(lower, "workspace deactivated"), strings.Contains(lower, "subscription required"):
		return "entitlement"
	case strings.Contains(lower, "401"), strings.Contains(lower, "unauthorized"), strings.Contains(lower, "invalid_grant"), strings.Contains(lower, "revoked"), strings.Contains(lower, "access token"):
		return "authentication"
	case strings.Contains(lower, "403"), strings.Contains(lower, "forbidden"):
		return "permission"
	case strings.Contains(lower, "429"), strings.Contains(lower, "rate limit"), strings.Contains(lower, "quota"):
		return "rate_limit"
	case strings.Contains(lower, "timeout"), strings.Contains(lower, "deadline exceeded"), strings.Contains(lower, "connection"), strings.Contains(lower, "proxy"), strings.Contains(lower, "dial tcp"), strings.Contains(lower, "tls"):
		return "network"
	case strings.Contains(lower, "500"), strings.Contains(lower, "502"), strings.Contains(lower, "503"), strings.Contains(lower, "504"), strings.Contains(lower, "upstream"):
		return "upstream"
	case strings.Contains(lower, "model"), strings.Contains(lower, "not found"):
		return "model"
	default:
		return "unknown"
	}
}
