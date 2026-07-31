package ralph

import (
	"errors"
	"strings"
	"testing"

	"github.com/leightonvanrooijen/utopia/internal/domain"
	"github.com/leightonvanrooijen/utopia/internal/validators"
)

const (
	testDefaultModel   = "sonnet"
	testEscalatedModel = "opus"
)

// testCaps mirrors the shipped defaults but is written out here so a change to
// the defaults surfaces as an intentional edit rather than a silently different
// test.
func testCaps() EscalationCaps {
	return EscalationCaps{MechanicalRetries: 4, ComprehensionEscalations: 2, OpusExecutionAttempts: 2, ScopingEscalations: 1, InvocationErrors: 3}
}

func mechanicalAggregate() *validators.AggregateResult {
	return &validators.AggregateResult{
		FailureClass: validators.FailureMechanical,
		Failures: []validators.ValidatorFailure{{
			ID:      "go-style",
			Verdict: &validators.Verdict{Outcome: validators.OutcomeFail, FailureClass: validators.FailureMechanical, Diagnosis: "unused import", Confidence: validators.ConfidenceHigh},
		}},
	}
}

func comprehensionAggregate() *validators.AggregateResult {
	return &validators.AggregateResult{
		FailureClass: validators.FailureComprehension,
		Failures: []validators.ValidatorFailure{{
			ID: "spec-fidelity",
			Verdict: &validators.Verdict{
				Outcome:         validators.OutcomeFail,
				FailureClass:    validators.FailureComprehension,
				Diagnosis:       "aggregated per validator, spec asks per phase",
				CorrectedIntent: "Aggregate across the phase.",
				Confidence:      validators.ConfidenceHigh,
			},
		}},
	}
}

func TestRouteValidationFailure_MechanicalRetriesOnDefaultExecutor(t *testing.T) {
	item := &domain.WorkItem{ID: "item", IterationCount: 1}

	d := routeValidationFailure(item, mechanicalAggregate(), testCaps(), testDefaultModel, testEscalatedModel)

	if d.Route != RouteMechanicalRetry {
		t.Errorf("Route = %q, want %q", d.Route, RouteMechanicalRetry)
	}
	if d.Model != testDefaultModel {
		t.Errorf("Model = %q, want the default executor %q", d.Model, testDefaultModel)
	}
	if d.Class != validators.FailureMechanical {
		t.Errorf("Class = %q, want %q", d.Class, validators.FailureMechanical)
	}
	if item.MechanicalRetryCount != 1 {
		t.Errorf("MechanicalRetryCount = %d, want 1", item.MechanicalRetryCount)
	}
}

// A mechanical retry must not move the escalation counter: the executor was not
// the problem, so nothing about it has been learned.
func TestRouteValidationFailure_MechanicalDoesNotIncrementComprehensionCount(t *testing.T) {
	item := &domain.WorkItem{ID: "item"}
	caps := testCaps()

	for i := 0; i < caps.MechanicalRetries; i++ {
		d := routeValidationFailure(item, mechanicalAggregate(), caps, testDefaultModel, testEscalatedModel)
		if d.Route != RouteMechanicalRetry {
			t.Fatalf("retry %d: Route = %q, want %q", i+1, d.Route, RouteMechanicalRetry)
		}
		if item.ComprehensionCount != 0 {
			t.Fatalf("retry %d: ComprehensionCount = %d, want 0", i+1, item.ComprehensionCount)
		}
	}
}

// Exceeding the mechanical cap reclassifies rather than failing outright: enough
// slips in the same place is evidence the executor is solving the wrong problem.
func TestRouteValidationFailure_MechanicalCapReclassifies(t *testing.T) {
	item := &domain.WorkItem{ID: "item"}
	caps := testCaps()

	for i := 0; i < caps.MechanicalRetries; i++ {
		routeValidationFailure(item, mechanicalAggregate(), caps, testDefaultModel, testEscalatedModel)
	}

	d := routeValidationFailure(item, mechanicalAggregate(), caps, testDefaultModel, testEscalatedModel)

	if !d.Reclassified {
		t.Error("Reclassified = false, want true once the mechanical cap is exceeded")
	}
	if d.Class != validators.FailureComprehension {
		t.Errorf("Class = %q, want %q", d.Class, validators.FailureComprehension)
	}
	if d.Route != RouteEscalateExecutor {
		t.Errorf("Route = %q, want %q", d.Route, RouteEscalateExecutor)
	}
	if d.Model != testEscalatedModel {
		t.Errorf("Model = %q, want the escalated executor %q", d.Model, testEscalatedModel)
	}
	if item.ComprehensionCount != 1 {
		t.Errorf("ComprehensionCount = %d, want 1", item.ComprehensionCount)
	}
	if item.MechanicalRetryCount != 0 {
		t.Errorf("MechanicalRetryCount = %d, want 0 - the counter measures consecutive slips", item.MechanicalRetryCount)
	}
}

func TestRouteValidationFailure_ComprehensionEscalatesExecutor(t *testing.T) {
	item := &domain.WorkItem{ID: "item"}

	d := routeValidationFailure(item, comprehensionAggregate(), testCaps(), testDefaultModel, testEscalatedModel)

	if item.ComprehensionCount != 1 {
		t.Errorf("ComprehensionCount = %d, want 1", item.ComprehensionCount)
	}
	if d.Route != RouteEscalateExecutor {
		t.Errorf("Route = %q, want %q", d.Route, RouteEscalateExecutor)
	}
	if d.Model != testEscalatedModel {
		t.Errorf("Model = %q, want the escalated executor %q", d.Model, testEscalatedModel)
	}
}

func TestRouteValidationFailure_SecondComprehensionRoutesToScoping(t *testing.T) {
	item := &domain.WorkItem{ID: "item", ComprehensionCount: 1}

	d := routeValidationFailure(item, comprehensionAggregate(), testCaps(), testDefaultModel, testEscalatedModel)

	if item.ComprehensionCount != 2 {
		t.Errorf("ComprehensionCount = %d, want 2", item.ComprehensionCount)
	}
	if d.Route != RouteScopingEscalation {
		t.Errorf("Route = %q, want %q", d.Route, RouteScopingEscalation)
	}
	if d.Model != "" {
		t.Errorf("Model = %q, want empty - no execution attempt follows scoping escalation", d.Model)
	}
}

// A suspected spec defect is a claim about the specification, so it bypasses the
// executor counters entirely.
func TestRouteValidationFailure_SpecDefectRoutesToScoping(t *testing.T) {
	agg := mechanicalAggregate()
	agg.SpecDefectSuspected = true
	item := &domain.WorkItem{ID: "item"}

	d := routeValidationFailure(item, agg, testCaps(), testDefaultModel, testEscalatedModel)

	if d.Route != RouteScopingEscalation {
		t.Errorf("Route = %q, want %q", d.Route, RouteScopingEscalation)
	}
	if !d.SpecDefect {
		t.Error("SpecDefect = false, want true")
	}
	if item.ComprehensionCount != 0 || item.MechanicalRetryCount != 0 {
		t.Errorf("counters moved to mechanical=%d comprehension=%d, want both 0", item.MechanicalRetryCount, item.ComprehensionCount)
	}
}

// A gating connector carries no verdict. It never claimed anything about intent,
// so it retries on the same executor and the mechanical cap bounds it.
func TestRouteValidationFailure_NoAggregateRoutesMechanical(t *testing.T) {
	item := &domain.WorkItem{ID: "item"}

	d := routeValidationFailure(item, nil, testCaps(), testDefaultModel, testEscalatedModel)

	if d.Route != RouteMechanicalRetry {
		t.Errorf("Route = %q, want %q", d.Route, RouteMechanicalRetry)
	}
	if item.MechanicalRetryCount != 1 {
		t.Errorf("MechanicalRetryCount = %d, want 1", item.MechanicalRetryCount)
	}
}

// The escalation state lives on the persisted work item, so an item reloaded
// mid-escalation keeps running on the escalated executor.
func TestExecutorModelFor_EscalationSurvivesResume(t *testing.T) {
	tests := []struct {
		name string
		item *domain.WorkItem
		want string
	}{
		{"fresh item", &domain.WorkItem{}, testDefaultModel},
		{"mechanical retries only", &domain.WorkItem{MechanicalRetryCount: 3}, testDefaultModel},
		{"resumed after escalating", &domain.WorkItem{ComprehensionCount: 1, IterationCount: 4}, testEscalatedModel},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := executorModelFor(tt.item, testDefaultModel, testEscalatedModel); got != tt.want {
				t.Errorf("executorModelFor = %q, want %q", got, tt.want)
			}
		})
	}
}

// The default executor is a role like any other: it reads its own key off config
// and falls back through models.default. The --model flag overrides the resolved
// value for one invocation rather than being the only way to set it.
func TestResolveDefaultExecutorModel(t *testing.T) {
	tests := []struct {
		name     string
		mc       *domain.ModelConfig
		override string
		want     string
	}{
		{"no config, no flag", nil, "", "sonnet"},
		{"models.execute wins over models.default", &domain.ModelConfig{Default: "haiku", Execute: "opus"}, "", "opus"},
		{"missing execute falls back to models.default", &domain.ModelConfig{Default: "haiku"}, "", "haiku"},
		{"flag overrides config", &domain.ModelConfig{Default: "haiku", Execute: "opus"}, "fable", "fable"},
		{"flag with no config", nil, "opus", "opus"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := resolveDefaultExecutorModel(tt.mc, tt.override); got != tt.want {
				t.Errorf("resolveDefaultExecutorModel = %q, want %q", got, tt.want)
			}
		})
	}
}

// A project that configures models.execute and models.execute_escalated to the
// same model still escalates: routing reads the persisted comprehension counter,
// never a comparison of model strings.
func TestEscalationDoesNotDependOnModelDifference(t *testing.T) {
	mc := &domain.ModelConfig{Execute: "opus", ExecuteEscalated: "opus"}
	def := resolveDefaultExecutorModel(mc, "")
	esc := resolveEscalatedExecutorModel(mc)
	if def != esc {
		t.Fatalf("test setup: expected identical models, got %q and %q", def, esc)
	}

	item := &domain.WorkItem{}
	decision := routeValidationFailure(item, comprehensionAggregate(), testCaps(), def, esc)

	if decision.Route != RouteEscalateExecutor {
		t.Errorf("Route = %v, want %v", decision.Route, RouteEscalateExecutor)
	}
	if item.ComprehensionCount != 1 {
		t.Errorf("ComprehensionCount = %d, want 1", item.ComprehensionCount)
	}
}

func TestResolveEscalatedExecutorModel(t *testing.T) {
	tests := []struct {
		name string
		mc   *domain.ModelConfig
		want string
	}{
		{"no config", nil, DefaultEscalatedExecutorModel},
		{"unset falls back to opus", &domain.ModelConfig{Default: "haiku", Execute: "sonnet"}, DefaultEscalatedExecutorModel},
		{"configured wins", &domain.ModelConfig{ExecuteEscalated: "fable"}, "fable"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := resolveEscalatedExecutorModel(tt.mc); got != tt.want {
				t.Errorf("resolveEscalatedExecutorModel = %q, want %q", got, tt.want)
			}
		})
	}
}

// The routing log line is the operator's only view of the decision, so it carries
// the class, both counters against their caps, the outer iteration bound and the
// model selected.
func TestRoutingDecision_LogLine(t *testing.T) {
	item := &domain.WorkItem{ID: "item"}
	caps := testCaps()
	d := routeValidationFailure(item, comprehensionAggregate(), caps, testDefaultModel, testEscalatedModel)

	line := d.logLine(3, 6, caps)

	for _, want := range []string{
		"class=comprehension",
		"mechanical=0/4",
		"comprehension=1/2",
		"iteration=3/6",
		"route=escalate-executor",
		"model=opus",
	} {
		if !strings.Contains(line, want) {
			t.Errorf("log line %q missing %q", line, want)
		}
	}
}

func TestRoutingDecision_LogLine_ReclassifiedAndScoping(t *testing.T) {
	item := &domain.WorkItem{ID: "item", MechanicalRetryCount: 4, ComprehensionCount: 1}
	caps := testCaps()
	d := routeValidationFailure(item, mechanicalAggregate(), caps, testDefaultModel, testEscalatedModel)

	line := d.logLine(5, 0, caps)

	if !strings.Contains(line, "reclassified from mechanical") {
		t.Errorf("log line %q does not report the reclassification", line)
	}
	if !strings.Contains(line, "iteration=5/unlimited") {
		t.Errorf("log line %q does not report an unlimited outer bound", line)
	}
	if !strings.Contains(line, "route=scoping-escalation") {
		t.Errorf("log line %q = want the scoping escalation route", line)
	}
	if !strings.Contains(line, "model=none") {
		t.Errorf("log line %q should report no model on the scoping escalation path", line)
	}
}

func TestScopingEscalationError_MatchesAndCarriesDiagnoses(t *testing.T) {
	item := &domain.WorkItem{ID: "item", ComprehensionCount: 1}
	agg := comprehensionAggregate()
	d := routeValidationFailure(item, agg, testCaps(), testDefaultModel, testEscalatedModel)

	err := scopingEscalationError(item, agg, d)

	if !errors.Is(err, &ScopingEscalationError{}) {
		t.Error("errors.Is did not match a ScopingEscalationError")
	}
	if err.ComprehensionCount != 2 {
		t.Errorf("ComprehensionCount = %d, want 2", err.ComprehensionCount)
	}
	if len(err.Diagnoses) != 1 || !strings.Contains(err.Diagnoses[0], "spec-fidelity") {
		t.Errorf("Diagnoses = %v, want the failing validator's diagnosis", err.Diagnoses)
	}
}

func TestAggregateFromGate(t *testing.T) {
	agg := comprehensionAggregate()

	if got := aggregateFromGate(&GateError{Connector: "validators:after-workitem", Aggregate: agg}); got != agg {
		t.Error("aggregateFromGate did not return the gate's aggregate")
	}
	if got := aggregateFromGate(&GateError{Connector: "lint-hook"}); got != nil {
		t.Error("aggregateFromGate returned an aggregate for a connector gate")
	}
	if got := aggregateFromGate(errors.New("boom")); got != nil {
		t.Error("aggregateFromGate returned an aggregate for a non-gate error")
	}
}
