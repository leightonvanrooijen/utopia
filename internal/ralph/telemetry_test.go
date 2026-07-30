package ralph

import (
	"context"
	"errors"
	"testing"

	"github.com/leightonvanrooijen/utopia/internal"
	"github.com/leightonvanrooijen/utopia/internal/domain"
	"github.com/leightonvanrooijen/utopia/internal/validators"
)

// recorderWith builds a run recorder already carrying a transcript, for tests
// that care about the record rather than about how the transcript accumulated.
func recorderWith(crType domain.CRType, transcript string) *runRecorder {
	rec := newRunRecorder(crType)
	rec.transcript.WriteString(transcript)
	return rec
}

// routedItem is a work item that went the whole way: two mechanical slips on the
// default executor, a comprehension failure that escalated it, and a rewrite it
// resumed against.
func routedItem() *domain.WorkItem {
	return &domain.WorkItem{
		ID:                        "cr-1-phase-0-add-thing",
		SpecRef:                   "my-spec.add-thing",
		IterationCount:            4,
		MechanicalFailureTotal:    2,
		ComprehensionFailureTotal: 1,
		ScopingEscalationCount:    1,
		SpecRewritten:             true,
		ExecutorAttempts: []domain.ExecutorAttempt{
			{Iteration: 1, Role: domain.ExecutorRoleDefault, Model: "sonnet", Effort: "medium"},
			{Iteration: 2, Role: domain.ExecutorRoleDefault, Model: "sonnet", Effort: "medium"},
			{Iteration: 3, Role: domain.ExecutorRoleDefault, Model: "sonnet", Effort: "medium"},
			{Iteration: 4, Role: domain.ExecutorRoleEscalated, Model: "opus", Effort: "high"},
		},
	}
}

func TestWriteRunTranscript_CarriesTheRoutingRecord(t *testing.T) {
	store := internal.NewYAMLStore(t.TempDir())
	item := routedItem()

	writeRunTranscript(store, "cr-1", item, recorderWith(domain.CRTypeFeature, "out"), domain.RunCompleted)

	run, err := internal.Load[domain.ExecutionRun](store, "runs/cr-1/cr-1-phase-0-add-thing.yaml")
	if err != nil {
		t.Fatalf("run record should be written: %v", err)
	}
	if run.Routing == nil {
		t.Fatal("run must carry a routing record")
	}
	r := run.Routing

	// Every field the record is required to carry. cr_id and spec_ref live on the
	// run itself, which is what joins the routing back to a spec and a CR.
	if run.CRID != "cr-1" || run.SpecRef != "my-spec.add-thing" {
		t.Errorf("cr_id/spec_ref = %q/%q, want cr-1/my-spec.add-thing", run.CRID, run.SpecRef)
	}
	if r.CRType != domain.CRTypeFeature {
		t.Errorf("cr_type = %q, want feature", r.CRType)
	}
	if len(r.Attempts) != 4 {
		t.Fatalf("attempts = %d, want one per attempt (4)", len(r.Attempts))
	}
	if r.Attempts[0].Model != "sonnet" || r.Attempts[0].Effort != "medium" {
		t.Errorf("attempt 1 = %+v, want the model and effort it ran on", r.Attempts[0])
	}
	if r.SonnetAttempts != 3 {
		t.Errorf("sonnet_attempts = %d, want 3", r.SonnetAttempts)
	}
	if r.OpusExecutionAttempts != 1 {
		t.Errorf("opus_execution_attempts = %d, want 1", r.OpusExecutionAttempts)
	}
	if r.MechanicalFailures != 2 || r.ComprehensionFailures != 1 {
		t.Errorf("failures = %d mechanical / %d comprehension, want 2/1", r.MechanicalFailures, r.ComprehensionFailures)
	}
	if r.ScopingEscalations != 1 || !r.SpecRewritten {
		t.Errorf("scoping = %d, spec_rewritten = %v, want 1/true", r.ScopingEscalations, r.SpecRewritten)
	}
	if r.Outcome != domain.RoutingPassed {
		t.Errorf("outcome = %q, want passed", r.Outcome)
	}
	if r.DurationSeconds < 0 || r.Duration == "" {
		t.Errorf("wall clock = %v (%q), want a measured duration", r.DurationSeconds, r.Duration)
	}
	// The absence of token counts must not read as zero to whoever finds this file.
	if r.CostNote != domain.CostNotCapturedNote {
		t.Errorf("cost_note = %q, want the not-captured note", r.CostNote)
	}
}

// The record has to exist for the outcomes that are worth arguing about, which
// are the ones where nothing was delivered.
func TestWriteRunTranscript_WrittenOnEveryOutcome(t *testing.T) {
	tests := []struct {
		name   string
		status domain.WorkItemStatus
		run    domain.RunOutcome
		want   domain.RoutingOutcome
	}{
		{"completed", domain.WorkItemCompleted, domain.RunCompleted, domain.RoutingPassed},
		{"every path exhausted", domain.WorkItemNeedsHuman, domain.RunFailed, domain.RoutingNeedsHuman},
		{"aborted", domain.WorkItemFailed, domain.RunFailed, domain.RoutingAbandoned},
		{"stopped mid-flight", domain.WorkItemInProgress, domain.RunFailed, domain.RoutingAbandoned},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := internal.NewYAMLStore(t.TempDir())
			item := routedItem()
			item.Status = tt.status

			writeRunTranscript(store, "cr-1", item, recorderWith(domain.CRTypeFeature, "out"), tt.run)

			run, err := internal.Load[domain.ExecutionRun](store, "runs/cr-1/cr-1-phase-0-add-thing.yaml")
			if err != nil {
				t.Fatalf("a record must be written for outcome %q: %v", tt.want, err)
			}
			if run.Routing == nil || run.Routing.Outcome != tt.want {
				t.Fatalf("routing outcome = %v, want %q", run.Routing, tt.want)
			}
		})
	}
}

// haltNeedsHuman is the only path that ends an item at an exhausted cap, and the
// record it writes is what a person re-scoping the change request reads.
func TestHaltNeedsHuman_RecordsTheHaltAsNeedsHuman(t *testing.T) {
	store := internal.NewYAMLStore(t.TempDir())
	item := routedItem()

	err := haltNeedsHuman(store, "my-spec", "cr-1", item, recorderWith(domain.CRTypeFeature, "out"),
		&NeedsHumanError{WorkItemID: item.ID, Cap: "escalation.scoping_escalations", Limit: 1})
	if err == nil {
		t.Fatal("halt must return the typed error")
	}

	run, loadErr := internal.Load[domain.ExecutionRun](store, "runs/cr-1/cr-1-phase-0-add-thing.yaml")
	if loadErr != nil {
		t.Fatalf("a halted item must still write its record: %v", loadErr)
	}
	if run.Routing == nil || run.Routing.Outcome != domain.RoutingNeedsHuman {
		t.Fatalf("routing outcome = %v, want needs_human", run.Routing)
	}
}

// The persisted evidence for ADR-004: a failure changes which model runs, never
// how hard the cheap one tries. Every attempt on the default executor carries the
// same effort, whatever the routing did in between.
func TestRoutingRecord_DefaultExecutorEffortIdenticalAcrossAttempts(t *testing.T) {
	r := routingRecordFor(routedItem(), domain.CRTypeFeature, domain.RoutingPassed, 0)

	effort, stable := r.DefaultExecutorEffort()
	if !stable {
		t.Error("the default executor's effort differs between attempts; no path may raise it (ADR-004)")
	}
	if effort != "medium" {
		t.Errorf("default executor effort = %q, want medium on every attempt", effort)
	}

	// An escalated attempt runs at its own role's higher level, which is a
	// different role rather than the default executor trying harder.
	for _, a := range r.Attempts {
		if a.Role == domain.ExecutorRoleEscalated && a.Effort == effort {
			t.Errorf("escalated attempt effort = %q, want the escalated role's own level", a.Effort)
		}
	}
}

// A raised effort would be caught here rather than only in review.
func TestRoutingRecord_DefaultExecutorEffortReportsDrift(t *testing.T) {
	item := routedItem()
	item.ExecutorAttempts[2].Effort = "xhigh"

	if _, stable := routingRecordFor(item, domain.CRTypeFeature, domain.RoutingPassed, 0).DefaultExecutorEffort(); stable {
		t.Error("an attempt that raised the default executor's effort must be reported as drift")
	}
}

func TestRecordExecutorAttempt_RoleFollowsTheEscalationState(t *testing.T) {
	item := &domain.WorkItem{IterationCount: 1}
	recordExecutorAttempt(item, "sonnet", "medium")

	item.ComprehensionCount = 1
	item.IterationCount = 2
	recordExecutorAttempt(item, "opus", "high")

	if len(item.ExecutorAttempts) != 2 {
		t.Fatalf("attempts = %d, want 2", len(item.ExecutorAttempts))
	}
	if item.ExecutorAttempts[0].Role != domain.ExecutorRoleDefault {
		t.Errorf("first attempt role = %q, want %q", item.ExecutorAttempts[0].Role, domain.ExecutorRoleDefault)
	}
	if item.ExecutorAttempts[1].Role != domain.ExecutorRoleEscalated {
		t.Errorf("escalated attempt role = %q, want %q", item.ExecutorAttempts[1].Role, domain.ExecutorRoleEscalated)
	}
}

// An attempt refunded for a usage limit produced no work, so it is not evidence
// of anything - the same reason its iteration and its cap charge are refunded.
func TestRefundExecutorAttempt_DropsTheAttemptThatNeverRan(t *testing.T) {
	item := &domain.WorkItem{IterationCount: 1}
	recordExecutorAttempt(item, "sonnet", "medium")
	refundExecutorAttempt(item)

	if len(item.ExecutorAttempts) != 0 {
		t.Errorf("attempts = %d, want the refunded attempt dropped", len(item.ExecutorAttempts))
	}
	// Refunding with nothing recorded must not panic - the loop calls it on every
	// limit-handled iteration, including one where recording failed.
	refundExecutorAttempt(item)
}

// The tallies count the class the validators reported. A mechanical failure the
// cap routed as comprehension stays mechanical here and is counted as
// reclassified, which is what reconciles the tallies with the routing counters.
func TestRouteValidationFailure_TalliesTheReportedClass(t *testing.T) {
	caps := EscalationCaps{MechanicalRetries: 1, ComprehensionEscalations: 2, OpusExecutionAttempts: 2, ScopingEscalations: 1}
	item := &domain.WorkItem{ID: "item"}

	mechanical := &validators.AggregateResult{FailureClass: validators.FailureMechanical}
	routeValidationFailure(item, mechanical, caps, "sonnet", "opus")
	if item.MechanicalFailureTotal != 1 || item.ComprehensionFailureTotal != 0 {
		t.Errorf("after a mechanical failure: %d/%d, want 1 mechanical / 0 comprehension",
			item.MechanicalFailureTotal, item.ComprehensionFailureTotal)
	}

	// The second mechanical failure exceeds the cap and is routed as comprehension.
	// It is still a mechanical failure as far as the validators were concerned.
	routeValidationFailure(item, mechanical, caps, "sonnet", "opus")
	if item.MechanicalFailureTotal != 2 || item.ComprehensionFailureTotal != 0 {
		t.Errorf("after reclassification: %d/%d, want 2 mechanical / 0 comprehension",
			item.MechanicalFailureTotal, item.ComprehensionFailureTotal)
	}
	if item.ReclassifiedFailureTotal != 1 {
		t.Errorf("reclassified = %d, want 1", item.ReclassifiedFailureTotal)
	}

	routeValidationFailure(item, &validators.AggregateResult{FailureClass: validators.FailureComprehension}, caps, "sonnet", "opus")
	if item.ComprehensionFailureTotal != 1 {
		t.Errorf("comprehension = %d, want 1", item.ComprehensionFailureTotal)
	}
}

// The tallies are lifetime totals precisely because the routing counters reset.
// A rewrite clears the comprehension counter; it must not clear the history the
// hypothesis is argued from.
func TestRoutingTallies_SurviveTheCounterResets(t *testing.T) {
	caps := EscalationCaps{MechanicalRetries: 4, ComprehensionEscalations: 2, OpusExecutionAttempts: 2, ScopingEscalations: 1}
	item := &domain.WorkItem{ID: "item"}

	routeValidationFailure(item, &validators.AggregateResult{FailureClass: validators.FailureMechanical}, caps, "sonnet", "opus")
	routeValidationFailure(item, &validators.AggregateResult{FailureClass: validators.FailureComprehension}, caps, "sonnet", "opus")

	if item.MechanicalRetryCount != 0 {
		t.Fatalf("setup: a comprehension failure should clear the mechanical counter, got %d", item.MechanicalRetryCount)
	}
	if item.MechanicalFailureTotal != 1 {
		t.Errorf("mechanical total = %d, want 1 kept across the counter reset", item.MechanicalFailureTotal)
	}

	// A rewrite resets ComprehensionCount; the tally is what the ratio is read from.
	item.ComprehensionCount = 0
	if item.ComprehensionFailureTotal != 1 {
		t.Errorf("comprehension total = %d, want 1 kept across the rewrite", item.ComprehensionFailureTotal)
	}
}

// An item that aborted on an error nobody anticipated is exactly the item worth
// having a record of. No record at all reads like a change request that was never
// attempted.
func TestRecordAbort_WritesTheRecordForAnUnhandledError(t *testing.T) {
	store := internal.NewYAMLStore(t.TempDir())
	item := routedItem()
	item.Status = domain.WorkItemInProgress
	rec := recorderWith(domain.CRTypeFeature, "out")

	recordAbort(context.Background(), store, "cr-1", item, rec, errors.New("verification command could not be executed"))

	run, err := internal.Load[domain.ExecutionRun](store, "runs/cr-1/cr-1-phase-0-add-thing.yaml")
	if err != nil {
		t.Fatalf("an aborted item must leave a record: %v", err)
	}
	if run.Routing == nil || run.Routing.Outcome != domain.RoutingAbandoned {
		t.Fatalf("routing outcome = %v, want abandoned", run.Routing)
	}
}

func TestRecordAbort_LeavesADeliberateOutcomeAlone(t *testing.T) {
	store := internal.NewYAMLStore(t.TempDir())
	item := routedItem()
	rec := recorderWith(domain.CRTypeFeature, "out")

	// The halt wrote the item's record; the catch-all must not overwrite it with an
	// abandonment on the way out of the loop.
	halt := haltNeedsHuman(store, "my-spec", "cr-1", item, rec, &NeedsHumanError{WorkItemID: item.ID})
	recordAbort(context.Background(), store, "cr-1", item, rec, halt)

	run, err := internal.Load[domain.ExecutionRun](store, "runs/cr-1/cr-1-phase-0-add-thing.yaml")
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	if run.Routing.Outcome != domain.RoutingNeedsHuman {
		t.Errorf("routing outcome = %q, want the halt's needs_human preserved", run.Routing.Outcome)
	}
}

func TestRecordAbort_SkipsWhatIsNotAnOutcome(t *testing.T) {
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()

	tests := []struct {
		name string
		ctx  context.Context
		err  error
	}{
		// A completing item writes its own record on the way past.
		{"no error", context.Background(), nil},
		// A cancelled run leaves the item resumable, so it has no outcome yet.
		{"cancelled run", cancelled, context.Canceled},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := internal.NewYAMLStore(t.TempDir())
			item := routedItem()

			recordAbort(tt.ctx, store, "cr-1", item, recorderWith(domain.CRTypeFeature, "out"), tt.err)

			if _, err := internal.Load[domain.ExecutionRun](store, "runs/cr-1/cr-1-phase-0-add-thing.yaml"); err == nil {
				t.Error("no record should be written")
			}
		})
	}
}

func TestDeriveCRType(t *testing.T) {
	initiative := &domain.ChangeRequest{
		ID:   "cr-1",
		Type: domain.CRTypeInitiative,
		Phases: []domain.Phase{
			{Type: domain.CRTypeEnhancement},
			{Type: domain.CRTypeFeature},
		},
	}

	tests := []struct {
		name   string
		cr     *domain.ChangeRequest
		specID string
		want   domain.CRType
	}{
		// A phase's type is the meaningful one: every phase of an initiative would
		// otherwise report "initiative" and there would be nothing to group by.
		{"initiative phase", initiative, "cr-1-phase-1", domain.CRTypeFeature},
		{"earlier phase", initiative, "cr-1-phase-0", domain.CRTypeEnhancement},
		{"phase out of range", initiative, "cr-1-phase-9", domain.CRTypeInitiative},
		{"non-phased CR", &domain.ChangeRequest{Type: domain.CRTypeRefactor}, "some-spec", domain.CRTypeRefactor},
		// A CR that could not be loaded groups as empty rather than as a guess.
		{"no change request", nil, "some-spec", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := deriveCRType(tt.cr, tt.specID); got != tt.want {
				t.Errorf("deriveCRType() = %q, want %q", got, tt.want)
			}
		})
	}
}
