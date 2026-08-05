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
	AccountHealthUnknown    = "unknown"
	AccountHealthHealthy    = "healthy"
	AccountHealthDegraded   = "degraded"
	AccountHealthRecovering = "recovering"
	AccountHealthBlocked    = "blocked"
)

type AccountHealthSettings struct {
	Enabled                 bool   `json:"enabled"`
	AutoAssignGroup         bool   `json:"auto_assign_group"`
	TargetGroupID           int64  `json:"target_group_id"`
	WorkerCount             int    `json:"worker_count"`
	BatchSize               int    `json:"batch_size"`
	DispatchIntervalSeconds int    `json:"dispatch_interval_seconds"`
	HealthyIntervalSeconds  int    `json:"healthy_interval_seconds"`
	RecoveryIntervalSeconds int    `json:"recovery_interval_seconds"`
	FailureThreshold        int    `json:"failure_threshold"`
	SuccessThreshold        int    `json:"success_threshold"`
	TimeoutSeconds          int    `json:"timeout_seconds"`
	ModelID                 string `json:"model_id"`
	AutoRecover             bool   `json:"auto_recover"`
	AutoBlock               bool   `json:"auto_block"`
}

func DefaultAccountHealthSettings() AccountHealthSettings {
	return AccountHealthSettings{
		Enabled:                 true,
		WorkerCount:             30,
		BatchSize:               256,
		DispatchIntervalSeconds: 1,
		HealthyIntervalSeconds:  20,
		RecoveryIntervalSeconds: 10,
		FailureThreshold:        3,
		SuccessThreshold:        1,
		TimeoutSeconds:          30,
		AutoRecover:             true,
		AutoBlock:               true,
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

type AccountHealthCandidate struct {
	AccountID           int64
	Name                string
	Status              string
	Schedulable         bool
	AutoBlocked         bool
	State               string
	ConsecutiveFailures int
}

type AccountHealthRecord struct {
	State                string `json:"state"`
	ConsecutiveSuccesses int    `json:"consecutive_successes"`
	ConsecutiveFailures  int    `json:"consecutive_failures"`
}

type AccountHealthSummary struct {
	Total             int64 `json:"total"`
	SchedulerEligible int64 `json:"scheduler_eligible"`
	Healthy           int64 `json:"healthy"`
	Degraded          int64 `json:"degraded"`
	Recovering        int64 `json:"recovering"`
	Blocked           int64 `json:"blocked"`
	Unknown           int64 `json:"unknown"`
	InFlight          int64 `json:"in_flight"`
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
	ValidateTargetGroup(ctx context.Context, groupID int64) error
	AssignMissingToGroup(ctx context.Context, groupID int64) (int64, error)
	LeaseDue(ctx context.Context, limit int, leaseUntil time.Time) ([]AccountHealthCandidate, error)
	RecordSuccess(ctx context.Context, accountID int64, latencyMs int64, successThreshold int, nextProbe time.Time) (*AccountHealthRecord, error)
	RecordFailure(ctx context.Context, accountID int64, category, message string, latencyMs int64, nextProbe time.Time) (*AccountHealthRecord, error)
	MarkAutoBlocked(ctx context.Context, accountID int64) error
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
	repo         AccountHealthRepository
	accountRepo  AccountRepository
	probe        AccountHealthProbe
	rateLimitSvc *RateLimitService
	settingRepo  SettingRepository
	settings     atomic.Pointer[AccountHealthSettings]
	inFlight     atomic.Int64
	hub          *accountHealthEventHub
	ctx          context.Context
	cancel       context.CancelFunc
	wake         chan struct{}
	startOnce    sync.Once
	stopOnce     sync.Once
	wg           sync.WaitGroup
}

func NewAccountHealthController(repo AccountHealthRepository, accountRepo AccountRepository, probe AccountHealthProbe, rateLimitSvc *RateLimitService, settingRepo SettingRepository) *AccountHealthController {
	ctx, cancel := context.WithCancel(context.Background())
	controller := &AccountHealthController{
		repo: repo, accountRepo: accountRepo, probe: probe, rateLimitSvc: rateLimitSvc,
		settingRepo: settingRepo, hub: newAccountHealthEventHub(), ctx: ctx, cancel: cancel,
		wake: make(chan struct{}, 1),
	}
	defaults := DefaultAccountHealthSettings()
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
	var settings AccountHealthSettings
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
		if err := s.reconcile(s.ctx, settings); err != nil {
			logger.LegacyPrintf("service.account_health", "[AccountHealth] reconcile failed: %v", err)
		}
		if !settings.Enabled {
			continue
		}
		s.dispatch(settings)
	}
}

func (s *AccountHealthController) reconcile(ctx context.Context, settings AccountHealthSettings) error {
	if err := s.repo.EnsureAccounts(ctx); err != nil {
		return fmt.Errorf("ensure account health states: %w", err)
	}
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
	leaseUntil := time.Now().Add(time.Duration(settings.TimeoutSeconds+30) * time.Second)
	candidates, err := s.repo.LeaseDue(s.ctx, limit, leaseUntil)
	if err != nil {
		logger.LegacyPrintf("service.account_health", "[AccountHealth] lease due failed: %v", err)
		return
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

	next := time.Now().Add(time.Duration(settings.HealthyIntervalSeconds) * time.Second)
	record, err := s.repo.RecordSuccess(persistCtx, candidate.AccountID, result.LatencyMs, settings.SuccessThreshold, next)
	if err != nil {
		logger.LegacyPrintf("service.account_health", "[AccountHealth] record success failed account=%d: %v", candidate.AccountID, err)
		return
	}
	if record.ConsecutiveSuccesses >= settings.SuccessThreshold && settings.AutoRecover && s.rateLimitSvc != nil {
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

func (s *AccountHealthController) handleFailure(ctx context.Context, candidate AccountHealthCandidate, settings AccountHealthSettings, message string, latencyMs int64) {
	category := ClassifyAccountHealthError(message)
	if category == "rate_limit" && isAccountHealthTooManyRequests(message) {
		if err := s.accountRepo.Delete(ctx, candidate.AccountID); err != nil {
			logger.LegacyPrintf("service.account_health", "[AccountHealth] delete 429 account failed account=%d: %v", candidate.AccountID, err)
		} else {
			logger.LegacyPrintf("service.account_health", "[AccountHealth] deleted account=%d after upstream 429", candidate.AccountID)
			s.hub.publish(AccountHealthEvent{Type: "account_deleted", AccountID: candidate.AccountID, State: "deleted", Message: "rate_limit_429"})
			return
		}
	}
	next := time.Now().Add(accountHealthRetryDelay(candidate.ConsecutiveFailures, settings))
	record, err := s.repo.RecordFailure(ctx, candidate.AccountID, category, message, latencyMs, next)
	if err != nil {
		logger.LegacyPrintf("service.account_health", "[AccountHealth] record failure failed account=%d: %v", candidate.AccountID, err)
		return
	}
	state := record.State
	if settings.AutoBlock && record.ConsecutiveFailures >= settings.FailureThreshold {
		if candidate.AutoBlocked {
			state = AccountHealthBlocked
		} else if err := s.accountRepo.SetError(ctx, candidate.AccountID, "health-check["+category+"]: "+message); err != nil {
			logger.LegacyPrintf("service.account_health", "[AccountHealth] auto block failed account=%d: %v", candidate.AccountID, err)
		} else if err := s.repo.MarkAutoBlocked(ctx, candidate.AccountID); err != nil {
			logger.LegacyPrintf("service.account_health", "[AccountHealth] persist auto block failed account=%d: %v", candidate.AccountID, err)
		} else {
			state = AccountHealthBlocked
		}
	}
	s.hub.publish(AccountHealthEvent{Type: "account", AccountID: candidate.AccountID, State: state, Message: category})
}

func isAccountHealthTooManyRequests(message string) bool {
	lower := strings.ToLower(message)
	return strings.Contains(lower, "429") || strings.Contains(lower, "too many requests")
}

func accountHealthRetryDelay(previousFailures int, settings AccountHealthSettings) time.Duration {
	base := time.Duration(settings.RecoveryIntervalSeconds) * time.Second
	maximum := time.Duration(settings.HealthyIntervalSeconds) * time.Second
	if maximum < base {
		maximum = base
	}
	steps := previousFailures / settings.FailureThreshold
	if steps > 4 {
		steps = 4
	}
	delay := base * time.Duration(1<<steps)
	if delay > maximum {
		return maximum
	}
	return delay
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
