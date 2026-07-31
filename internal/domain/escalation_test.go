package domain

import (
	"errors"
	"strings"
	"testing"
)

func capPtr(v int) *int { return &v }

func TestValidateEscalationConfig(t *testing.T) {
	tests := []struct {
		name    string
		config  *EscalationConfig
		wantErr bool
	}{
		{"omitted section", nil, false},
		{"empty section takes every default", &EscalationConfig{}, false},
		{"all positive", &EscalationConfig{
			MechanicalRetries:        capPtr(4),
			ComprehensionEscalations: capPtr(2),
			OpusExecutionAttempts:    capPtr(2),
			ScopingEscalations:       capPtr(1),
		}, false},
		{"one is the smallest cap that bounds anything", &EscalationConfig{ScopingEscalations: capPtr(1)}, false},
		{"zero mechanical retries", &EscalationConfig{MechanicalRetries: capPtr(0)}, true},
		{"zero comprehension escalations", &EscalationConfig{ComprehensionEscalations: capPtr(0)}, true},
		{"zero escalated execution attempts", &EscalationConfig{OpusExecutionAttempts: capPtr(0)}, true},
		{"zero invocation errors", &EscalationConfig{InvocationErrors: capPtr(0)}, true},
		{"positive invocation errors", &EscalationConfig{InvocationErrors: capPtr(3)}, false},
		{"negative scoping escalations", &EscalationConfig{ScopingEscalations: capPtr(-2)}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateEscalationConfig(tt.config)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateEscalationConfig = %v, wantErr %v", err, tt.wantErr)
			}
			if err != nil && !errors.Is(err, &InvalidEscalationCapError{}) {
				t.Errorf("error does not match InvalidEscalationCapError: %v", err)
			}
		})
	}
}

// The error names every offending key, because fixing one at a time is a
// load-config round trip per cap.
func TestValidateEscalationConfig_NamesEveryOffendingKey(t *testing.T) {
	err := ValidateEscalationConfig(&EscalationConfig{
		MechanicalRetries:        capPtr(0),
		ComprehensionEscalations: capPtr(2),
		OpusExecutionAttempts:    capPtr(-1),
		InvocationErrors:         capPtr(0),
	})
	if err == nil {
		t.Fatal("expected an error, got nil")
	}
	for _, want := range []string{"escalation.mechanical_retries: 0", "escalation.opus_execution_attempts: -1", "escalation.invocation_errors: 0", "at least 1"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q missing %q", err.Error(), want)
		}
	}
	if strings.Contains(err.Error(), "comprehension_escalations") {
		t.Errorf("error %q names a valid cap", err.Error())
	}
}

// An omitted key and an explicit value are distinguishable, which is the whole
// reason the caps are pointers.
func TestCapOr(t *testing.T) {
	if got := CapOr(nil, 4); got != 4 {
		t.Errorf("CapOr(nil, 4) = %d, want the fallback 4", got)
	}
	if got := CapOr(capPtr(7), 4); got != 7 {
		t.Errorf("CapOr(7, 4) = %d, want the configured 7", got)
	}
}

// needs_human is a distinct terminal state, not a synonym for failed: the
// operator action differs, so the two must never compare equal.
func TestWorkItemNeedsHuman_DistinctFromFailed(t *testing.T) {
	if WorkItemNeedsHuman == WorkItemFailed {
		t.Error("needs_human and failed are the same status, want distinct terminal states")
	}
	if WorkItemNeedsHuman != "needs_human" {
		t.Errorf("WorkItemNeedsHuman = %q, want \"needs_human\"", WorkItemNeedsHuman)
	}
}
