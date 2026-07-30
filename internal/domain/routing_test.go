package domain

import "testing"

// routingRun is a run record carrying routing, for the aggregation tests below.
func routingRun(specRef string, crType CRType, r RoutingRecord) *ExecutionRun {
	r.CRType = crType
	return &ExecutionRun{WorkItemID: specRef, CRID: "cr-1", SpecRef: specRef, Routing: &r}
}

// Escalation clustering by spec is the aggregation these records exist for: a
// spec whose change requests escalate repeatedly has a boundary problem rather
// than a model problem. It is read off the records, never off a transcript.
func TestSummariseRoutingBySpec_EscalationRateAndFailureRatio(t *testing.T) {
	runs := []*ExecutionRun{
		routingRun("leaky-spec.one", CRTypeFeature, RoutingRecord{
			SonnetAttempts: 3, OpusExecutionAttempts: 1, MechanicalFailures: 2,
			ComprehensionFailures: 1, Outcome: RoutingPassed,
		}),
		routingRun("leaky-spec.two", CRTypeFeature, RoutingRecord{
			SonnetAttempts: 2, OpusExecutionAttempts: 1, ScopingEscalations: 1,
			SpecRewritten: true, MechanicalFailures: 2, ComprehensionFailures: 3,
			Outcome: RoutingNeedsHuman,
		}),
		routingRun("leaky-spec.three", CRTypeFeature, RoutingRecord{
			SonnetAttempts: 1, Outcome: RoutingPassed,
		}),
		routingRun("clean-spec.one", CRTypeEnhancement, RoutingRecord{
			SonnetAttempts: 1, Outcome: RoutingPassed,
		}),
	}

	bySpec := SummariseRoutingBySpec(runs)
	if len(bySpec) != 2 {
		t.Fatalf("groups = %d, want one per spec (2)", len(bySpec))
	}

	leaky := bySpec["leaky-spec"]
	if leaky.Records != 3 || leaky.Escalated != 2 {
		t.Fatalf("leaky-spec = %d records / %d escalated, want 3/2", leaky.Records, leaky.Escalated)
	}
	if got := leaky.EscalationRate(); got < 0.66 || got > 0.67 {
		t.Errorf("leaky-spec escalation rate = %v, want 2/3", got)
	}
	// 4 mechanical failures to 4 comprehension failures across the spec.
	if got := leaky.MechanicalToComprehensionRatio(); got != 1 {
		t.Errorf("leaky-spec mechanical:comprehension = %v, want 1", got)
	}
	if leaky.NeedsHuman != 1 || leaky.Passed != 2 || leaky.SpecRewrites != 1 {
		t.Errorf("leaky-spec outcomes = %+v, want 2 passed / 1 needs_human / 1 rewrite", leaky)
	}

	clean := bySpec["clean-spec"]
	if clean.EscalationRate() != 0 {
		t.Errorf("clean-spec escalation rate = %v, want 0", clean.EscalationRate())
	}
}

func TestSummariseRoutingByCRType(t *testing.T) {
	runs := []*ExecutionRun{
		routingRun("a.one", CRTypeFeature, RoutingRecord{OpusExecutionAttempts: 1, Outcome: RoutingPassed}),
		routingRun("b.one", CRTypeFeature, RoutingRecord{Outcome: RoutingPassed}),
		routingRun("c.one", CRTypeEnhancement, RoutingRecord{Outcome: RoutingPassed}),
	}

	byType := SummariseRoutingByCRType(runs)
	if got := byType[string(CRTypeFeature)].EscalationRate(); got != 0.5 {
		t.Errorf("feature escalation rate = %v, want 0.5", got)
	}
	if got := byType[string(CRTypeEnhancement)].EscalationRate(); got != 0 {
		t.Errorf("enhancement escalation rate = %v, want 0", got)
	}
}

// A run written before routing was recorded carries none. Counting it as
// non-escalating would understate every rate, so it is skipped instead.
func TestSummariseRouting_SkipsRunsWithoutRouting(t *testing.T) {
	runs := []*ExecutionRun{
		{SpecRef: "a.one", Outcome: RunCompleted},
		nil,
		routingRun("a.two", CRTypeFeature, RoutingRecord{OpusExecutionAttempts: 1, Outcome: RoutingPassed}),
	}

	summary := SummariseRoutingBySpec(runs)["a"]
	if summary.Records != 1 {
		t.Fatalf("records = %d, want only the run that carries routing", summary.Records)
	}
	if summary.EscalationRate() != 1 {
		t.Errorf("escalation rate = %v, want 1", summary.EscalationRate())
	}
}

func TestRoutingSummary_RatioEdgeCases(t *testing.T) {
	if got := (RoutingSummary{}).MechanicalToComprehensionRatio(); got != 0 {
		t.Errorf("no failures: ratio = %v, want 0", got)
	}
	// Mechanical failures with no comprehension failures has no finite ratio, and
	// is reported as one rather than as a division by zero.
	if got := (RoutingSummary{MechanicalFailures: 3}).MechanicalToComprehensionRatio(); got != -1 {
		t.Errorf("no comprehension failures: ratio = %v, want -1", got)
	}
	if got := (RoutingSummary{}).EscalationRate(); got != 0 {
		t.Errorf("empty group: escalation rate = %v, want 0", got)
	}
}

func TestRoutingRecord_Escalated(t *testing.T) {
	tests := []struct {
		name   string
		record RoutingRecord
		want   bool
	}{
		{"default executor only", RoutingRecord{SonnetAttempts: 3}, false},
		{"escalated executor", RoutingRecord{OpusExecutionAttempts: 1}, true},
		// A spec defect routes straight to a rewrite without an escalated execution
		// attempt, and that is still an escalation.
		{"scoping only", RoutingRecord{ScopingEscalations: 1}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.record.Escalated(); got != tt.want {
				t.Errorf("Escalated() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRoutingOutcome_Values(t *testing.T) {
	// The record's outcome vocabulary is exactly these three.
	want := map[RoutingOutcome]string{
		RoutingPassed:     "passed",
		RoutingNeedsHuman: "needs_human",
		RoutingAbandoned:  "abandoned",
	}
	for outcome, literal := range want {
		if string(outcome) != literal {
			t.Errorf("%v serialises as %q, want %q", outcome, string(outcome), literal)
		}
	}
}
