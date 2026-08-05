package service

import (
	"testing"
	"time"
)

func TestDefaultAccountHealthSettingsAreValid(t *testing.T) {
	settings := DefaultAccountHealthSettings()
	if err := settings.Validate(); err != nil {
		t.Fatalf("default settings must be valid: %v", err)
	}
	if settings.WorkerCount != 64 || settings.MaxProbeQPS != 32 || settings.HealthyIntervalSeconds != 900 || settings.SuccessThreshold != 3 || !settings.AutoRecover || !settings.AutoBlock {
		t.Fatalf("unexpected production defaults: %+v", settings)
	}
}

func TestAccountHealthSettingsRejectUnsafeWorkerCount(t *testing.T) {
	settings := DefaultAccountHealthSettings()
	settings.WorkerCount = 257
	if err := settings.Validate(); err == nil {
		t.Fatal("expected worker_count validation error")
	}
}

func TestAccountHealthSettingsRequireTargetGroupWhenAutoAssignEnabled(t *testing.T) {
	settings := DefaultAccountHealthSettings()
	settings.AutoAssignGroup = true
	if err := settings.Validate(); err == nil {
		t.Fatal("expected target_group_id validation error")
	}
	settings.TargetGroupID = 42
	if err := settings.Validate(); err != nil {
		t.Fatalf("expected valid auto assignment settings: %v", err)
	}
}

func TestClassifyAccountHealthError(t *testing.T) {
	tests := map[string]string{
		`API returned 402: {"detail":{"code":"deactivated_workspace"}}`: "entitlement",
		"upstream returned 401 unauthorized":                            "authentication",
		"403 forbidden":                                                 "permission",
		"429 rate limit exceeded":                                       "rate_limit",
		"dial tcp: connection timeout":                                  "network",
		"upstream request failed with status code 503":                  "upstream",
		"model is not supported":                                        "model",
		"unexpected response":                                           "unknown",
	}
	for message, expected := range tests {
		if actual := ClassifyAccountHealthError(message); actual != expected {
			t.Errorf("ClassifyAccountHealthError(%q) = %q, want %q", message, actual, expected)
		}
	}
}

func TestAccountHealthRetryDelayUsesTransientSchedule(t *testing.T) {
	settings := DefaultAccountHealthSettings()
	tests := []struct {
		failures int
		want     time.Duration
	}{
		{0, 10 * time.Second},
		{1, 30 * time.Second},
		{2, 120 * time.Second},
		{3, 300 * time.Second},
		{99, 300 * time.Second},
	}
	for _, test := range tests {
		if got := accountHealthRetryDelay(test.failures, settings); got != test.want {
			t.Errorf("failures=%d: got %s, want %s", test.failures, got, test.want)
		}
	}
}

func TestAccountHealthFailurePolicySeparatesTerminalFailures(t *testing.T) {
	settings := DefaultAccountHealthSettings()
	tests := []struct {
		category string
		state    string
		class    string
		delay    time.Duration
	}{
		{"authentication", AccountHealthAuthQuarantine, AccountHealthProbeTerminalCanary, 30 * time.Minute},
		{"entitlement", AccountHealthEntitlementQuarantine, AccountHealthProbeTerminalCanary, 6 * time.Hour},
		{"permission", AccountHealthPermissionQuarantine, AccountHealthProbeTerminalCanary, time.Hour},
		{"network", AccountHealthTransientError, AccountHealthProbeTransient, 10 * time.Second},
	}
	for _, test := range tests {
		state, class, delay := accountHealthFailurePolicy(test.category, 0, settings)
		if state != test.state || class != test.class || delay != test.delay {
			t.Errorf("category=%s got (%s,%s,%s), want (%s,%s,%s)", test.category, state, class, delay, test.state, test.class, test.delay)
		}
	}
}

func TestAccountHealthJitterIsDeterministicAndBounded(t *testing.T) {
	base := 10 * time.Minute
	first := accountHealthJitter(base, 42, 3)
	second := accountHealthJitter(base, 42, 3)
	if first != second {
		t.Fatalf("jitter must be deterministic: %s != %s", first, second)
	}
	if first < base || first > base+base/10 {
		t.Fatalf("jitter %s outside expected range", first)
	}
}

func TestAccountHealthProbeLimiterCapsBurst(t *testing.T) {
	limiter := newAccountHealthProbeLimiter(2, 3)
	if got := limiter.take(10); got != 3 {
		t.Fatalf("initial burst = %d, want 3", got)
	}
	if got := limiter.take(1); got != 0 {
		t.Fatalf("empty limiter returned %d tokens", got)
	}
}

func TestAccountHealthCandidateNeedsRecoverySkipsHealthySamplingWrites(t *testing.T) {
	healthy := AccountHealthCandidate{Status: StatusActive, Schedulable: true, State: AccountHealthHealthy}
	if accountHealthCandidateNeedsRecovery(healthy) {
		t.Fatal("healthy schedulable account must not trigger account recovery")
	}
	for _, candidate := range []AccountHealthCandidate{
		{Status: StatusActive, Schedulable: true, AutoBlocked: true},
		{Status: StatusError, Schedulable: false},
		{Status: StatusActive, Schedulable: false},
	} {
		if !accountHealthCandidateNeedsRecovery(candidate) {
			t.Fatalf("candidate should require recovery: %+v", candidate)
		}
	}
}
