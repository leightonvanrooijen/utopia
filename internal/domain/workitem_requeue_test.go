package domain

import (
	"errors"
	"testing"
)

// haltedItem is an item that halted with every counter and diagnosis a halt can
// leave behind, plus the accounting a requeue must not touch.
func haltedItem() *WorkItem {
	return &WorkItem{
		ID:                        "auth-signup",
		Status:                    WorkItemNeedsHuman,
		IterationCount:            7,
		ComprehensionCount:        2,
		OpusExecutionAttempts:     3,
		ComprehensionFailureTotal: 4,
		InvocationErrorCount:      2,
		UnresolvedGateCount:       1,
		MechanicalRetryCount:      2,
		LastFailureOutput:         "FAIL ./internal/auth",
		LastValidatorFeedback:     "the handler never validates the token",
		FailureConclusions:        []FailureConclusion{{Iteration: 6, ValidatorID: "spec-intent", Diagnosis: "misread"}},
		MechanicalFailureTotal:    5,
		ReclassifiedFailureTotal:  1,
		ScopingEscalationCount:    1,
		SpecRewritten:             true,
		ExecutorAttempts:          []ExecutorAttempt{{Iteration: 6, Role: ExecutorRoleEscalated, Model: "opus"}},
	}
}

func TestRequeueClearsRoutingState(t *testing.T) {
	item := haltedItem()

	reset, err := item.Requeue()
	if err != nil {
		t.Fatalf("Requeue() error = %v, want nil", err)
	}

	if item.Status != WorkItemPending {
		t.Errorf("Status = %q, want %q", item.Status, WorkItemPending)
	}
	if reset.From != WorkItemNeedsHuman {
		t.Errorf("reset.From = %q, want %q", reset.From, WorkItemNeedsHuman)
	}

	counters := map[string]int{
		"IterationCount":            item.IterationCount,
		"ComprehensionCount":        item.ComprehensionCount,
		"OpusExecutionAttempts":     item.OpusExecutionAttempts,
		"ComprehensionFailureTotal": item.ComprehensionFailureTotal,
		"InvocationErrorCount":      item.InvocationErrorCount,
		"UnresolvedGateCount":       item.UnresolvedGateCount,
		"MechanicalRetryCount":      item.MechanicalRetryCount,
	}
	for name, got := range counters {
		if got != 0 {
			t.Errorf("%s = %d, want 0: a counter carried across a requeue halts the item at the same bound", name, got)
		}
	}

	if item.LastValidatorFeedback != "" {
		t.Errorf("LastValidatorFeedback = %q, want empty", item.LastValidatorFeedback)
	}
	if item.LastFailureOutput != "" {
		t.Errorf("LastFailureOutput = %q, want empty", item.LastFailureOutput)
	}
	if item.FailureConclusions != nil {
		t.Errorf("FailureConclusions = %v, want nil", item.FailureConclusions)
	}
}

func TestRequeueKeepsAccounting(t *testing.T) {
	item := haltedItem()

	if _, err := item.Requeue(); err != nil {
		t.Fatalf("Requeue() error = %v, want nil", err)
	}

	if len(item.ExecutorAttempts) != 1 {
		t.Errorf("ExecutorAttempts = %v, want the recorded attempt kept: the spend describes the last attempt", item.ExecutorAttempts)
	}
	if item.MechanicalFailureTotal != 5 {
		t.Errorf("MechanicalFailureTotal = %d, want 5", item.MechanicalFailureTotal)
	}
	if item.ReclassifiedFailureTotal != 1 {
		t.Errorf("ReclassifiedFailureTotal = %d, want 1", item.ReclassifiedFailureTotal)
	}
	if item.ScopingEscalationCount != 1 {
		t.Errorf("ScopingEscalationCount = %d, want 1: the rewrite path stays bounded across a requeue", item.ScopingEscalationCount)
	}
	if !item.SpecRewritten {
		t.Error("SpecRewritten = false, want true: a rewrite execution ran against still happened")
	}
}

func TestRequeueReportsWhatItCleared(t *testing.T) {
	item := haltedItem()

	reset, err := item.Requeue()
	if err != nil {
		t.Fatalf("Requeue() error = %v, want nil", err)
	}

	got := map[string]string{}
	for _, field := range reset.Cleared {
		got[field.Name] = field.Was
	}
	want := map[string]string{
		"iteration_count":              "7",
		"comprehension_count":          "2",
		"opus_execution_attempts":      "3",
		"comprehension_failures_total": "4",
		"invocation_error_count":       "2",
		"unresolved_gate_count":        "1",
		"mechanical_retry_count":       "2",
		"last_validator_feedback":      "set",
		"last_failure_output":          "set",
		"failure_conclusions":          "1 conclusion(s)",
	}
	if len(got) != len(want) {
		t.Fatalf("cleared %d field(s) (%v), want %d", len(got), got, len(want))
	}
	for name, wantWas := range want {
		if gotWas, ok := got[name]; !ok || gotWas != wantWas {
			t.Errorf("cleared[%q] = %q (present=%v), want %q", name, gotWas, ok, wantWas)
		}
	}
}

func TestRequeueOmitsFieldsThatWereAlreadyEmpty(t *testing.T) {
	item := &WorkItem{ID: "quiet-halt", Status: WorkItemNeedsHuman, IterationCount: 1}

	reset, err := item.Requeue()
	if err != nil {
		t.Fatalf("Requeue() error = %v, want nil", err)
	}
	if len(reset.Cleared) != 1 || reset.Cleared[0].Name != "iteration_count" {
		t.Errorf("cleared = %v, want only iteration_count: the report describes this item, not the type", reset.Cleared)
	}
}

func TestRequeueRefusesAnItemThatIsNotHalted(t *testing.T) {
	for _, status := range []WorkItemStatus{WorkItemPending, WorkItemInProgress, WorkItemCompleted, WorkItemFailed} {
		t.Run(string(status), func(t *testing.T) {
			item := haltedItem()
			item.Status = status

			reset, err := item.Requeue()
			if err == nil {
				t.Fatalf("Requeue() on a %s item = %v, want an error", status, reset)
			}
			var notHalted *NotHaltedError
			if !errors.As(err, &notHalted) {
				t.Fatalf("Requeue() error = %v, want *NotHaltedError", err)
			}
			if notHalted.Status != status {
				t.Errorf("NotHaltedError.Status = %q, want %q", notHalted.Status, status)
			}
			if item.Status != status || item.IterationCount != 7 {
				t.Errorf("refused requeue mutated the item: status %q, iteration_count %d", item.Status, item.IterationCount)
			}
		})
	}
}
