package ralph

import (
	"errors"
	"fmt"

	"github.com/leightonvanrooijen/utopia/internal/domain"
	"github.com/leightonvanrooijen/utopia/internal/validators"
)

// DefaultEscalatedExecutorModel is the model an escalated execution attempt runs
// on when config sets no models.execute_escalated override. The escalated
// executor resolves its own model rather than deriving one from models.execute,
// following the precedent the validator relevance router set: a role with a
// different cost profile and a different failure consequence resolves
// independently.
const DefaultEscalatedExecutorModel = string(domain.ModelOpus)

// Escalation caps. They are the inner bounds on the retry paths, sitting inside
// verification.max_iterations, which still bounds total iterations for a work
// item. The values are guesses chosen so the worst case is roughly cost-neutral
// against running the expensive model throughout; the telemetry that would
// confirm them does not exist yet, which is why they are named rather than
// inlined.
const (
	// DefaultMechanicalRetryCap is how many consecutive mechanical failures are
	// retried on the same executor before the failure is reclassified as
	// comprehension. Enough mechanical slips in the same place is itself evidence
	// the executor is solving the wrong problem.
	DefaultMechanicalRetryCap = 4
	// DefaultComprehensionEscalationCap is the comprehension_count at which the
	// work item stops escalating the executor and escalates the scoping instead.
	// One comprehension failure escalates the executor; the second says the
	// change request, not the executor, is what needs rewriting.
	DefaultComprehensionEscalationCap = 2
)

// EscalationCaps bounds each escalation path. Both caps are independently
// configurable because both are guesses.
type EscalationCaps struct {
	// MechanicalRetries caps consecutive mechanical retries on the same executor.
	MechanicalRetries int
	// ComprehensionEscalations caps comprehension failures before the work item
	// routes to scoping escalation.
	ComprehensionEscalations int
}

// DefaultEscalationCaps returns the caps the loop runs with absent configuration.
func DefaultEscalationCaps() EscalationCaps {
	return EscalationCaps{
		MechanicalRetries:        DefaultMechanicalRetryCap,
		ComprehensionEscalations: DefaultComprehensionEscalationCap,
	}
}

// Route is what the loop does next with a work item whose validation gate failed.
type Route string

const (
	// RouteMechanicalRetry retries on the default executor. The intent was right
	// and the execution slipped, so the executor was not the problem.
	RouteMechanicalRetry Route = "mechanical-retry"
	// RouteEscalateExecutor retries on the escalated executor. The same executor
	// re-reading the same specification would re-derive the same wrong intent.
	RouteEscalateExecutor Route = "escalate-executor"
	// RouteScopingEscalation stops execution and escalates the change request.
	// Repeated comprehension failure is evidence about the specification rather
	// than only about the executor.
	RouteScopingEscalation Route = "scoping-escalation"
)

// RoutingDecision records how one failing validation gate was routed, including
// the counter values it routed on. It is what the per-iteration log line reports,
// so an operator can reconstruct the routing from the run output alone.
type RoutingDecision struct {
	// Route is the path taken.
	Route Route
	// Class is the failure class routed on, after any reclassification.
	Class validators.FailureClass
	// Reclassified is true when a mechanical failure exceeded the mechanical cap
	// and was routed as comprehension instead.
	Reclassified bool
	// SpecDefect is true when a validator suspected the specification itself,
	// which routes to scoping escalation whatever the counters say.
	SpecDefect bool
	// Model is the executor model the next attempt runs on, empty on the scoping
	// escalation path where no execution attempt follows. An empty model on a
	// retry path means no override: the claude CLI default applies.
	Model string
	// MechanicalRetries and ComprehensionCount are the counters after this
	// decision was applied to the work item.
	MechanicalRetries  int
	ComprehensionCount int
}

// routeValidationFailure decides what to do with a work item whose validation
// gate failed, and applies the decision's counter changes to the item so they are
// persisted with it and survive a resume.
//
// The classification comes from the validators' aggregate. A gate that carries no
// aggregate - a gating connector rather than a validator - is routed as
// mechanical: a connector never claimed anything about intent, and a connector
// that keeps blocking is reclassified by the mechanical cap like any other
// repeated slip.
func routeValidationFailure(item *domain.WorkItem, agg *validators.AggregateResult, caps EscalationCaps, defaultModel, escalatedModel string) RoutingDecision {
	class := validators.FailureMechanical
	specDefect := false
	if agg != nil {
		specDefect = agg.SpecDefectSuspected
		if agg.FailureClass != "" {
			class = agg.FailureClass
		}
	}

	// A suspected spec defect is a claim about the specification, so it routes to
	// scoping escalation without consulting or moving the counters - they measure
	// the executor, which is not what is being doubted.
	if specDefect {
		return RoutingDecision{
			Route:              RouteScopingEscalation,
			Class:              class,
			SpecDefect:         true,
			MechanicalRetries:  item.MechanicalRetryCount,
			ComprehensionCount: item.ComprehensionCount,
		}
	}

	reclassified := false
	if class == validators.FailureMechanical {
		item.MechanicalRetryCount++
		if item.MechanicalRetryCount <= caps.MechanicalRetries {
			return RoutingDecision{
				Route:              RouteMechanicalRetry,
				Class:              validators.FailureMechanical,
				Model:              defaultModel,
				MechanicalRetries:  item.MechanicalRetryCount,
				ComprehensionCount: item.ComprehensionCount,
			}
		}
		// The cap is exceeded. Reclassifying rather than failing outright is the
		// load-bearing choice: it converts a stuck retry loop into an escalation,
		// which is the only move that can change the outcome.
		reclassified = true
		class = validators.FailureComprehension
	}

	// The mechanical counter counts consecutive slips, so a comprehension failure
	// - reported or reclassified - clears it.
	item.MechanicalRetryCount = 0
	item.ComprehensionCount++

	decision := RoutingDecision{
		Class:              class,
		Reclassified:       reclassified,
		MechanicalRetries:  item.MechanicalRetryCount,
		ComprehensionCount: item.ComprehensionCount,
	}
	if item.ComprehensionCount >= caps.ComprehensionEscalations {
		decision.Route = RouteScopingEscalation
		return decision
	}
	decision.Route = RouteEscalateExecutor
	decision.Model = escalatedModel
	return decision
}

// logLine renders the routing decision for one iteration: the failure class, both
// counters against their caps, the iteration against max_iterations, and the model
// the next attempt runs on. Both bounds are on the line because they compose - the
// caps are the inner bounds and max_iterations the outer one - so an operator can
// tell which one stopped a work item.
func (d RoutingDecision) logLine(iteration, maxIterations int, caps EscalationCaps) string {
	class := string(d.Class)
	if d.Reclassified {
		class += " (reclassified from mechanical at the retry cap)"
	}
	if d.SpecDefect {
		class += " (spec defect suspected)"
	}

	outer := "unlimited"
	if maxIterations > 0 {
		outer = fmt.Sprintf("%d", maxIterations)
	}

	line := fmt.Sprintf("routing: class=%s mechanical=%d/%d comprehension=%d/%d iteration=%d/%s route=%s",
		class,
		d.MechanicalRetries, caps.MechanicalRetries,
		d.ComprehensionCount, caps.ComprehensionEscalations,
		iteration, outer,
		d.Route)

	switch d.Route {
	case RouteScopingEscalation:
		return line + " model=none (execution halted pending scoping escalation)"
	default:
		model := d.Model
		if model == "" {
			model = "claude CLI default"
		}
		return line + " model=" + model
	}
}

// ScopingEscalationError reports a work item whose repeated comprehension
// failures - or a suspected spec defect - routed it to scoping escalation. It is a
// typed error so the caller can branch on it rather than parsing a message: the
// work item did not fail verification, it is waiting for its change request to be
// rewritten.
type ScopingEscalationError struct {
	// WorkItemID is the item that routed to scoping escalation.
	WorkItemID string
	// ComprehensionCount is the item's comprehension counter at the decision.
	ComprehensionCount int
	// SpecDefectSuspected is true when a validator suspected the specification
	// rather than the execution, which routes here whatever the counter says.
	SpecDefectSuspected bool
	// Diagnoses carries the failing validators' diagnoses, which are the evidence
	// a rewrite is built from.
	Diagnoses []string
}

func (e *ScopingEscalationError) Error() string {
	reason := fmt.Sprintf("comprehension failures (%d)", e.ComprehensionCount)
	if e.SpecDefectSuspected {
		reason = "a suspected spec defect"
	}
	return fmt.Sprintf("work item %s routed to scoping escalation after %s", e.WorkItemID, reason)
}

// Is allows errors.Is to match any ScopingEscalationError.
func (e *ScopingEscalationError) Is(target error) bool {
	_, ok := target.(*ScopingEscalationError)
	return ok
}

// scopingEscalationError builds the error for a work item routed to scoping
// escalation, carrying the diagnoses behind the decision when the gate supplied
// an aggregate.
func scopingEscalationError(item *domain.WorkItem, agg *validators.AggregateResult, d RoutingDecision) *ScopingEscalationError {
	err := &ScopingEscalationError{
		WorkItemID:          item.ID,
		ComprehensionCount:  d.ComprehensionCount,
		SpecDefectSuspected: d.SpecDefect,
	}
	if agg != nil {
		for _, f := range agg.Failures {
			if f.Verdict != nil && f.Verdict.Diagnosis != "" {
				err.Diagnoses = append(err.Diagnoses, f.ID+": "+f.Verdict.Diagnosis)
			}
		}
	}
	return err
}

// resolveEscalatedExecutorModel determines which model an escalated execution
// attempt runs on. Priority: models.execute_escalated > opus. It never falls
// through to models.execute or models.default, because escalating to the model
// that just failed is not an escalation.
func resolveEscalatedExecutorModel(mc *domain.ModelConfig) string {
	if mc != nil && mc.ExecuteEscalated != "" {
		return mc.ExecuteEscalated
	}
	return DefaultEscalatedExecutorModel
}

// executorModelFor resolves the model the next attempt on this work item runs on.
// It is derived from the persisted comprehension counter rather than from an
// in-memory flag, so a resumed work item that already escalated stays escalated
// instead of resetting to the default executor.
func executorModelFor(item *domain.WorkItem, defaultModel, escalatedModel string) string {
	if item.ComprehensionCount > 0 {
		return escalatedModel
	}
	return defaultModel
}

// aggregateFromGate extracts the validators' aggregate from a blocking gate
// error, or nil when the gate was something other than a failing validator.
func aggregateFromGate(err error) *validators.AggregateResult {
	var ge *GateError
	if errors.As(err, &ge) {
		return ge.Aggregate
	}
	return nil
}
