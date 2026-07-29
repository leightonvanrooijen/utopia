package ralph

import (
	"errors"
	"strings"
	"testing"

	"github.com/leightonvanrooijen/utopia/internal/domain"
)

func TestDefaultEscalationCaps(t *testing.T) {
	caps := DefaultEscalationCaps()

	if caps.MechanicalRetries != 4 {
		t.Errorf("MechanicalRetries = %d, want 4", caps.MechanicalRetries)
	}
	if caps.ComprehensionEscalations != 2 {
		t.Errorf("ComprehensionEscalations = %d, want 2", caps.ComprehensionEscalations)
	}
	if caps.OpusExecutionAttempts != 2 {
		t.Errorf("OpusExecutionAttempts = %d, want 2", caps.OpusExecutionAttempts)
	}
	if caps.ScopingEscalations != 1 {
		t.Errorf("ScopingEscalations = %d, want 1", caps.ScopingEscalations)
	}
}

// Every cap overrides on its own, and the ones left out keep their defaults.
func TestEscalationCapsFrom_EachCapOverridesIndependently(t *testing.T) {
	cap := func(v int) *int { return &v }
	defaults := DefaultEscalationCaps()

	tests := []struct {
		name   string
		config *domain.EscalationConfig
		want   EscalationCaps
	}{
		{"omitted section", nil, defaults},
		{"empty section", &domain.EscalationConfig{}, defaults},
		{
			"mechanical only",
			&domain.EscalationConfig{MechanicalRetries: cap(9)},
			EscalationCaps{9, defaults.ComprehensionEscalations, defaults.OpusExecutionAttempts, defaults.ScopingEscalations},
		},
		{
			"comprehension only",
			&domain.EscalationConfig{ComprehensionEscalations: cap(5)},
			EscalationCaps{defaults.MechanicalRetries, 5, defaults.OpusExecutionAttempts, defaults.ScopingEscalations},
		},
		{
			"escalated execution attempts only",
			&domain.EscalationConfig{OpusExecutionAttempts: cap(6)},
			EscalationCaps{defaults.MechanicalRetries, defaults.ComprehensionEscalations, 6, defaults.ScopingEscalations},
		},
		{
			"scoping only",
			&domain.EscalationConfig{ScopingEscalations: cap(3)},
			EscalationCaps{defaults.MechanicalRetries, defaults.ComprehensionEscalations, defaults.OpusExecutionAttempts, 3},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := EscalationCapsFrom(tt.config); got != tt.want {
				t.Errorf("EscalationCapsFrom = %+v, want %+v", got, tt.want)
			}
		})
	}
}

// The first scoping escalation is allowed; the second has nothing left to route
// to, so the item halts rather than looping through the rewrite path forever.
func TestRouteValidationFailure_SecondScopingEscalationHalts(t *testing.T) {
	caps := testCaps()
	item := &domain.WorkItem{ID: "item", ComprehensionCount: 1, ScopingEscalationCount: 1}

	d := routeValidationFailure(item, comprehensionAggregate(), caps, testDefaultModel, testEscalatedModel)

	if d.Route != RouteNeedsHuman {
		t.Fatalf("Route = %q, want %q", d.Route, RouteNeedsHuman)
	}
	if d.CapExhausted != "escalation.scoping_escalations" {
		t.Errorf("CapExhausted = %q, want the scoping cap", d.CapExhausted)
	}
	line := d.logLine(7, 12, caps)
	for _, want := range []string{"route=needs-human", "scoping=2/1", "escalation.scoping_escalations", "halted"} {
		if !strings.Contains(line, want) {
			t.Errorf("log line %q missing %q", line, want)
		}
	}
}

// A spec defect routes to scoping without touching the executor counters, but it
// is still a rewrite, so it counts against the scoping cap and halts at it.
func TestRouteValidationFailure_RepeatedSpecDefectHalts(t *testing.T) {
	agg := mechanicalAggregate()
	agg.SpecDefectSuspected = true
	item := &domain.WorkItem{ID: "item", ScopingEscalationCount: 1}

	d := routeValidationFailure(item, agg, testCaps(), testDefaultModel, testEscalatedModel)

	if d.Route != RouteNeedsHuman {
		t.Errorf("Route = %q, want %q", d.Route, RouteNeedsHuman)
	}
}

// The escalated-execution cap bounds spend on the expensive path: it charges only
// escalated attempts, and halts before spending the attempt past the bound.
func TestChargeEscalatedAttempt(t *testing.T) {
	caps := testCaps() // OpusExecutionAttempts: 2

	t.Run("unescalated attempt is not charged", func(t *testing.T) {
		item := &domain.WorkItem{ID: "item", MechanicalRetryCount: 3}
		charged, err := chargeEscalatedAttempt(item, caps)
		if charged || err != nil {
			t.Errorf("charged = %v, err = %v, want false and no error", charged, err)
		}
		if item.OpusExecutionAttempts != 0 {
			t.Errorf("OpusExecutionAttempts = %d, want 0", item.OpusExecutionAttempts)
		}
	})

	t.Run("escalated attempts charge up to the cap", func(t *testing.T) {
		item := &domain.WorkItem{ID: "item", ComprehensionCount: 1}
		for i := 1; i <= caps.OpusExecutionAttempts; i++ {
			charged, err := chargeEscalatedAttempt(item, caps)
			if !charged || err != nil {
				t.Fatalf("attempt %d: charged = %v, err = %v, want true and no error", i, charged, err)
			}
			if item.OpusExecutionAttempts != i {
				t.Errorf("attempt %d: OpusExecutionAttempts = %d, want %d", i, item.OpusExecutionAttempts, i)
			}
		}
	})

	t.Run("the attempt past the cap halts before it is spent", func(t *testing.T) {
		item := &domain.WorkItem{ID: "item", ComprehensionCount: 1, OpusExecutionAttempts: caps.OpusExecutionAttempts}
		charged, err := chargeEscalatedAttempt(item, caps)
		if charged {
			t.Error("charged = true, want the attempt refused rather than spent")
		}
		if err == nil {
			t.Fatal("err = nil, want the halt")
		}
		if err.Cap != "escalation.opus_execution_attempts" || err.Limit != caps.OpusExecutionAttempts {
			t.Errorf("halt reports %s = %d, want the escalated-execution cap", err.Cap, err.Limit)
		}
		if item.OpusExecutionAttempts != caps.OpusExecutionAttempts {
			t.Errorf("OpusExecutionAttempts = %d, want the counter left at the cap", item.OpusExecutionAttempts)
		}
	})

	t.Run("a raised cap allows further attempts", func(t *testing.T) {
		item := &domain.WorkItem{ID: "item", ComprehensionCount: 1, OpusExecutionAttempts: 2}
		charged, err := chargeEscalatedAttempt(item, EscalationCaps{OpusExecutionAttempts: 3})
		if !charged || err != nil {
			t.Errorf("charged = %v, err = %v, want the attempt allowed under the raised cap", charged, err)
		}
	})
}

// The halt is distinguishable from a verification failure and from a scoping
// escalation, because the operator action differs for each.
func TestNeedsHumanError_DistinguishableAndNamesTheCap(t *testing.T) {
	halt := &NeedsHumanError{
		WorkItemID: "item",
		Cap:        "escalation.opus_execution_attempts",
		Limit:      2,
		Detail:     "2 escalated execution attempt(s) failed to satisfy the validators",
	}

	if !errors.Is(halt, &NeedsHumanError{}) {
		t.Error("errors.Is did not match a NeedsHumanError")
	}
	if errors.Is(halt, &ScopingEscalationError{}) {
		t.Error("a halt matched a ScopingEscalationError, want the two distinguishable")
	}
	for _, want := range []string{"item", "needs human attention", "escalation.opus_execution_attempts = 2"} {
		if !strings.Contains(halt.Error(), want) {
			t.Errorf("message %q missing %q", halt.Error(), want)
		}
	}

	// Wrapping the reason that led here keeps both matchable, so a caller looking
	// for either still finds it.
	wrapped := &NeedsHumanError{WorkItemID: "item", Cap: "escalation.scoping_escalations", Limit: 1,
		Cause: &ScopingEscalationError{WorkItemID: "item", ComprehensionCount: 2}}
	if !errors.Is(wrapped, &NeedsHumanError{}) || !errors.Is(wrapped, &ScopingEscalationError{}) {
		t.Error("a wrapped halt did not match both the halt and its cause")
	}
}
