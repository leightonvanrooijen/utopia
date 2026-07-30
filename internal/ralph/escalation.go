package ralph

import (
	"errors"
	"fmt"

	"github.com/leightonvanrooijen/utopia/internal"
	"github.com/leightonvanrooijen/utopia/internal/domain"
	"github.com/leightonvanrooijen/utopia/internal/validators"
)

// DefaultEscalatedExecutorModel is the model an escalated execution attempt runs
// on when config sets no models.execute_escalated override. The escalated
// executor resolves its own model rather than deriving one from models.execute,
// following the precedent the validator relevance router set: a role with a
// different cost profile and a different failure consequence resolves
// independently.
const DefaultEscalatedExecutorModel = domain.DefaultEscalatedModel

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
	// DefaultOpusExecutionAttemptCap is how many execution attempts may run on the
	// escalated executor before the work item halts. Two attempts on the expensive
	// model is what keeps the worst case roughly cost-neutral against having run
	// the expensive model throughout.
	DefaultOpusExecutionAttemptCap = 2
	// DefaultScopingEscalationCap is how many times one change request may be
	// routed to scoping escalation. One rewrite is the bound: a change request that
	// is still misread after being rewritten is not a change request a second
	// rewrite fixes.
	DefaultScopingEscalationCap = 1
)

// EscalationCaps bounds each escalation path. Every cap is independently
// configurable because every one of them is a guess.
//
// The caps compose as a chain of last resorts, and only the end of the chain
// halts. Exceeding MechanicalRetries reclassifies the failure as comprehension,
// because an escalation is the only move that can change a stuck retry's
// outcome. Reaching ComprehensionEscalations routes to scoping escalation, for
// the same reason one step further out. Exhausting OpusExecutionAttempts or
// ScopingEscalations leaves nothing further to route to, so the work item halts
// as needs_human. verification.max_iterations remains the outer bound on the
// item as a whole.
type EscalationCaps struct {
	// MechanicalRetries caps consecutive mechanical retries on the same executor.
	MechanicalRetries int
	// ComprehensionEscalations caps comprehension failures before the work item
	// routes to scoping escalation.
	ComprehensionEscalations int
	// OpusExecutionAttempts caps execution attempts on the escalated executor.
	OpusExecutionAttempts int
	// ScopingEscalations caps scoping escalations for one change request.
	ScopingEscalations int
}

// DefaultEscalationCaps returns the caps the loop runs with absent configuration.
func DefaultEscalationCaps() EscalationCaps {
	return EscalationCaps{
		MechanicalRetries:        DefaultMechanicalRetryCap,
		ComprehensionEscalations: DefaultComprehensionEscalationCap,
		OpusExecutionAttempts:    DefaultOpusExecutionAttemptCap,
		ScopingEscalations:       DefaultScopingEscalationCap,
	}
}

// EscalationCapsFrom resolves the caps one run executes with: each configured cap
// overrides its default independently, and an omitted section or key keeps the
// default. The values are known-positive here because config load already
// rejected a non-positive cap.
func EscalationCapsFrom(ec *domain.EscalationConfig) EscalationCaps {
	caps := DefaultEscalationCaps()
	if ec == nil {
		return caps
	}
	caps.MechanicalRetries = domain.CapOr(ec.MechanicalRetries, caps.MechanicalRetries)
	caps.ComprehensionEscalations = domain.CapOr(ec.ComprehensionEscalations, caps.ComprehensionEscalations)
	caps.OpusExecutionAttempts = domain.CapOr(ec.OpusExecutionAttempts, caps.OpusExecutionAttempts)
	caps.ScopingEscalations = domain.CapOr(ec.ScopingEscalations, caps.ScopingEscalations)
	return caps
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
	// RouteNeedsHuman halts the work item. Every path that could have changed the
	// outcome is exhausted, so a further attempt would only spend money to fail
	// the same way.
	RouteNeedsHuman Route = "needs-human"
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
	// MechanicalRetries, ComprehensionCount and ScopingEscalations are the counters
	// after this decision was applied to the work item.
	MechanicalRetries  int
	ComprehensionCount int
	ScopingEscalations int
	// CapExhausted names the config key whose cap left no path to route to, set
	// only on RouteNeedsHuman. It is what the halt reports, so an operator can see
	// which bound to raise rather than guessing.
	CapExhausted string
}

// reportedFailureClass is the class a failing validation gate reported, before any
// cap reclassifies it. A gate carrying no aggregate - a gating connector rather
// than a validator - is mechanical: a connector never claimed anything about
// intent, so it cannot have concluded the specification was misread.
func reportedFailureClass(agg *validators.AggregateResult) validators.FailureClass {
	if agg != nil && agg.FailureClass != "" {
		return agg.FailureClass
	}
	return validators.FailureMechanical
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
	class := reportedFailureClass(agg)
	specDefect := agg != nil && agg.SpecDefectSuspected

	// Tally the reported class before any of the routing below reclassifies it, so
	// the persisted ratio of mechanical to comprehension failures measures what the
	// validators concluded rather than what the caps did with it.
	recordReportedFailureClass(item, class)

	// A suspected spec defect is a claim about the specification, so it routes to
	// scoping escalation without consulting or moving the executor counters - they
	// measure the executor, which is not what is being doubted. The scoping counter
	// still moves: it bounds rewrites, and a spec defect asks for one.
	if specDefect {
		return routeScoping(item, caps, RoutingDecision{
			Class:              class,
			SpecDefect:         true,
			MechanicalRetries:  item.MechanicalRetryCount,
			ComprehensionCount: item.ComprehensionCount,
		})
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
		recordReclassifiedFailure(item)
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
		ScopingEscalations: item.ScopingEscalationCount,
	}
	if item.ComprehensionCount >= caps.ComprehensionEscalations {
		return routeScoping(item, caps, decision)
	}
	decision.Route = RouteEscalateExecutor
	decision.Model = escalatedModel
	return decision
}

// routeScoping completes a decision that has reached the scoping escalation path,
// counting the escalation against the scoping cap. Exhausting that cap halts the
// item: rewriting a change request is the last path available, so there is
// nothing beyond it to route to.
func routeScoping(item *domain.WorkItem, caps EscalationCaps, decision RoutingDecision) RoutingDecision {
	item.ScopingEscalationCount++
	decision.ScopingEscalations = item.ScopingEscalationCount
	if item.ScopingEscalationCount > caps.ScopingEscalations {
		decision.Route = RouteNeedsHuman
		decision.CapExhausted = "escalation.scoping_escalations"
		return decision
	}
	decision.Route = RouteScopingEscalation
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

	line := fmt.Sprintf("routing: class=%s mechanical=%d/%d comprehension=%d/%d scoping=%d/%d iteration=%d/%s route=%s",
		class,
		d.MechanicalRetries, caps.MechanicalRetries,
		d.ComprehensionCount, caps.ComprehensionEscalations,
		d.ScopingEscalations, caps.ScopingEscalations,
		iteration, outer,
		d.Route)

	switch d.Route {
	case RouteNeedsHuman:
		return line + " model=none (" + d.CapExhausted + " exhausted, work item halted)"
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
	err.Diagnoses = scopingDiagnoses(agg)
	return err
}

// NeedsHumanError reports a work item halted because every bounded path
// available to it is exhausted. It is a typed error so the caller can branch on
// it rather than parsing a message: the run continues with the next work item,
// while the halted item waits for a person to re-scope its change request.
//
// It is deliberately distinct from a verification failure. A work item that
// failed verification can be retried as it stands; a work item that needs a human
// would exhaust the same caps again, so retrying it unchanged only spends money.
type NeedsHumanError struct {
	// WorkItemID is the item that halted.
	WorkItemID string
	// Cap is the config key whose bound was exhausted.
	Cap string
	// Limit is that cap's configured value.
	Limit int
	// Detail says what was attempted up to the bound, in the terms the cap counts.
	Detail string
	// Cause is the decision that led here, when there was one - a scoping
	// escalation with no rewrite left to try, for instance. It is unwrapped, so a
	// caller matching on the underlying reason still matches.
	Cause error
}

func (e *NeedsHumanError) Error() string {
	msg := fmt.Sprintf("work item %s needs human attention: %s (%s = %d)", e.WorkItemID, e.Detail, e.Cap, e.Limit)
	if e.Cause != nil {
		return msg + ": " + e.Cause.Error()
	}
	return msg
}

// Unwrap exposes the cause so errors.As reaches it through the halt.
func (e *NeedsHumanError) Unwrap() error { return e.Cause }

// Is allows errors.Is to match any NeedsHumanError.
func (e *NeedsHumanError) Is(target error) bool {
	_, ok := target.(*NeedsHumanError)
	return ok
}

// haltNeedsHuman terminates a work item at the exhausted cap: the item is
// persisted as needs_human so a resume skips it rather than exhausting the same
// cap again, and the run record of what was tried is written because a run that
// gave up is what a person re-scoping the change request has to read.
//
// The status is what makes the halt distinguishable from a verification failure
// on disk; the returned typed error is what makes it distinguishable to the
// caller, which continues with the next work item. The status is set before the
// record is written so the routing record reports the halt as needs_human rather
// than as an abandonment.
func haltNeedsHuman(store *internal.YAMLStore, specID, crID string, item *domain.WorkItem, rec *runRecorder, halt *NeedsHumanError) error {
	item.Status = domain.WorkItemNeedsHuman
	_ = store.SaveWorkItemForSpec(specID, item)
	writeRunTranscript(store, crID, item, rec, domain.RunFailed)
	return halt
}

// resolveDefaultExecutorModel determines which model a first-attempt execution
// runs on. Priority: the run's --model override > models.execute >
// models.default > sonnet. The override is per invocation and so wins over
// config, but it is not the only way to set the model: without it the default
// executor resolves its own role's key like every other role does.
func resolveDefaultExecutorModel(mc *domain.ModelConfig, override string) string {
	if override != "" {
		return override
	}
	return mc.ExecutorModel()
}

// resolveEscalatedExecutorModel determines which model an escalated execution
// attempt runs on. Priority: models.execute_escalated > opus. It never falls
// through to models.execute or models.default, because escalating to the model
// that just failed is not an escalation.
func resolveEscalatedExecutorModel(mc *domain.ModelConfig) string {
	return mc.EscalatedExecutorModel()
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

// chargeEscalatedAttempt books the attempt the loop is about to make against the
// escalated-execution cap, and reports the halt when that cap is already
// exhausted - before the attempt is spent rather than after.
//
// Whether an attempt is escalated is read from the item's persisted comprehension
// counter, the same source the model resolution uses, rather than by comparing
// model strings: a project that configures models.execute and
// models.execute_escalated to the same model still escalated, and its expensive
// attempts still need bounding.
//
// It returns whether the attempt was charged, so an attempt that never ran can be
// refunded. Only a real attempt costs money, which is what this cap bounds.
func chargeEscalatedAttempt(item *domain.WorkItem, caps EscalationCaps) (bool, *NeedsHumanError) {
	if item.ComprehensionCount == 0 {
		return false, nil
	}
	if item.OpusExecutionAttempts >= caps.OpusExecutionAttempts {
		return false, &NeedsHumanError{
			WorkItemID: item.ID,
			Cap:        "escalation.opus_execution_attempts",
			Limit:      caps.OpusExecutionAttempts,
			Detail: fmt.Sprintf("%d escalated execution attempt(s) failed to satisfy the validators",
				item.OpusExecutionAttempts),
		}
	}
	item.OpusExecutionAttempts++
	return true, nil
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
