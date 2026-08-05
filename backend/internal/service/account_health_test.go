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
	if settings.WorkerCount != 64 || !settings.AutoRecover || !settings.AutoBlock {
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

func TestAccountHealthRetryDelayBacksOffAndCapsAtHealthyInterval(t *testing.T) {
	settings := DefaultAccountHealthSettings()
	tests := []struct {
		failures int
		want     time.Duration
	}{
		{0, 10 * time.Second},
		{3, 20 * time.Second},
		{6, 40 * time.Second},
		{9, 80 * time.Second},
		{12, 120 * time.Second},
		{99, 120 * time.Second},
	}
	for _, test := range tests {
		if got := accountHealthRetryDelay(test.failures, settings); got != test.want {
			t.Errorf("failures=%d: got %s, want %s", test.failures, got, test.want)
		}
	}
}
