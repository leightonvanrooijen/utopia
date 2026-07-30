package ralph

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/leightonvanrooijen/utopia/internal"
	"github.com/leightonvanrooijen/utopia/internal/cli/ui"
	"github.com/leightonvanrooijen/utopia/internal/domain"
	"github.com/leightonvanrooijen/utopia/internal/validators"
)

// runRecorder is what one work item's execution accumulates for its run record:
// the streamed Claude output, the wall clock, and the change request type the
// routing has to be reported against.
//
// The routing counters are deliberately not here. They live on the work item,
// which is persisted every iteration, so an item that resumes mid-run reports its
// whole history rather than whatever this process happened to observe. The
// recorder holds only what cannot be persisted per iteration - the transcript
// being assembled, and a start marker that would be meaningless after a resume
// anyway.
type runRecorder struct {
	// transcript accumulates each iteration's streamed Claude output.
	transcript strings.Builder
	// start is when the item began, the baseline for the record's wall clock. It is
	// the recorder's own marker rather than a borrowed reference to stepTimings,
	// because the routing record must keep reporting wall clock when that type is
	// replaced by the OpenTelemetry model.
	start time.Time
	// crType is the change request's type, resolved once when the item starts.
	crType domain.CRType
	// written records that the item's run record has already been persisted, so the
	// catch-all write on the way out of the loop cannot overwrite the record a
	// deliberate path already wrote.
	written bool
}

// newRunRecorder starts a work item's run record.
func newRunRecorder(crType domain.CRType) *runRecorder {
	return &runRecorder{start: time.Now(), crType: crType}
}

// elapsed is the item's wall clock so far.
func (r *runRecorder) elapsed() time.Duration {
	return time.Since(r.start)
}

// deriveCRType resolves the change request type a work item's routing is recorded
// against. For an initiative the meaningful type is the phase's, not the
// initiative's: every phase of an initiative would otherwise report the same
// type, and "features escalate more than enhancements" would have nothing to
// group by.
//
// A CR that cannot be loaded, or a phase index that does not resolve, leaves the
// type empty rather than guessing one. An empty type groups separately, which is
// visible; a guessed one is not.
func deriveCRType(cr *domain.ChangeRequest, specID string) domain.CRType {
	if cr == nil {
		return ""
	}
	if cr.Type != domain.CRTypeInitiative {
		return cr.Type
	}
	if idx, ok := phaseIndexFromSpecID(specID); ok && idx >= 0 && idx < len(cr.Phases) {
		return cr.Phases[idx].Type
	}
	return cr.Type
}

// recordExecutorAttempt books the attempt the loop is about to make onto the work
// item, with the role, model and effort it runs under.
//
// The role is read from the item's persisted comprehension counter, the same
// source the model and effort resolution read, so the record cannot disagree with
// what actually ran.
func recordExecutorAttempt(item *domain.WorkItem, model, effort string) {
	role := domain.ExecutorRoleDefault
	if item.ComprehensionCount > 0 {
		role = domain.ExecutorRoleEscalated
	}
	item.ExecutorAttempts = append(item.ExecutorAttempts, domain.ExecutorAttempt{
		Iteration: item.IterationCount,
		Role:      role,
		Model:     model,
		Effort:    effort,
	})
}

// recordAttemptUsage attaches what the attempt just made spent to that attempt's
// routing record. It is called after the usage-limit refund has had its say, so a
// refunded attempt - one that never ran - is never the one written to.
//
// An attempt whose accounting could not be read is recorded as unavailable rather
// than dropped or defaulted to zero: the tokens were spent either way, and a
// missing number that reads as zero would understate every total derived from these
// records. Nothing here can fail the work item.
//
// The measured wall clock stands in when the CLI reported no duration, which is the
// unparseable case. The loop times the invocation from the outside anyway, so that
// number exists whether or not the agent accounted for itself.
func recordAttemptUsage(item *domain.WorkItem, result *internal.PromptResult, elapsed time.Duration) {
	n := len(item.ExecutorAttempts)
	if n == 0 {
		return
	}
	attempt := &item.ExecutorAttempts[n-1]

	usage := result.GetUsage()
	if usage == nil {
		usage = domain.UnavailableUsage("the invocation reported no usage")
	}
	if usage.Effort == "" {
		usage.Effort = attempt.Effort
	}
	if usage.DurationMS == 0 {
		usage.DurationMS = elapsed.Milliseconds()
	}
	attempt.Usage = usage
}

// recordAttemptOutcome books what the attempt just made achieved onto that
// attempt's routing record: whether its work passed, was rejected, or never reached
// a verdict, and the class it was rejected as.
//
// It is recorded as the loop leaves the iteration rather than reconstructed
// afterwards, because only the loop knows which of the paths out of an iteration
// was taken. The class is passed empty on every outcome but AttemptFailed.
func recordAttemptOutcome(item *domain.WorkItem, outcome domain.AttemptOutcome, class validators.FailureClass) {
	n := len(item.ExecutorAttempts)
	if n == 0 {
		return
	}
	attempt := &item.ExecutorAttempts[n-1]
	attempt.Outcome = outcome
	attempt.FailureClass = string(class)
}

// refundExecutorAttempt drops the last recorded attempt, for an attempt that never
// ran. It is the record's half of the usage-limit refund: the iteration and the
// escalation charge are both undone there, and an attempt that produced no work
// is not evidence of anything.
func refundExecutorAttempt(item *domain.WorkItem) {
	if n := len(item.ExecutorAttempts); n > 0 {
		item.ExecutorAttempts = item.ExecutorAttempts[:n-1]
	}
}

// recordReportedFailureClass tallies one failing validation gate by the class the
// validators reported, before the caps reclassify anything.
//
// The reported class is what gets counted because the ratio these tallies exist
// to produce is a measurement of the validators' verdicts, not of the routing.
func recordReportedFailureClass(item *domain.WorkItem, class validators.FailureClass) {
	if class == validators.FailureComprehension {
		item.ComprehensionFailureTotal++
		return
	}
	item.MechanicalFailureTotal++
}

// recordReclassifiedFailure tallies a mechanical failure the mechanical cap routed
// as comprehension instead. It is counted separately rather than moved between the
// two tallies above, because that is what lets a reader reconcile the reported
// classes against the routing counters on the work item.
func recordReclassifiedFailure(item *domain.WorkItem) {
	item.ReclassifiedFailureTotal++
}

// routingRecordFor builds the routing record for a finished work item from the
// counters persisted on it.
//
// Attempts at each tier are counted from the attempt list rather than read from
// the cap counters, so the counts and the per-attempt evidence behind them cannot
// drift apart: a refunded attempt disappears from both or neither.
func routingRecordFor(item *domain.WorkItem, crType domain.CRType, outcome domain.RoutingOutcome, elapsed time.Duration) *domain.RoutingRecord {
	sonnet, opus := 0, 0
	for _, a := range item.ExecutorAttempts {
		if a.Role == domain.ExecutorRoleEscalated {
			opus++
			continue
		}
		sonnet++
	}

	return &domain.RoutingRecord{
		CRType:                crType,
		Attempts:              item.ExecutorAttempts,
		SonnetAttempts:        sonnet,
		OpusExecutionAttempts: opus,
		MechanicalFailures:    item.MechanicalFailureTotal,
		ComprehensionFailures: item.ComprehensionFailureTotal,
		ReclassifiedFailures:  item.ReclassifiedFailureTotal,
		ScopingEscalations:    item.ScopingEscalationCount,
		SpecRewritten:         item.SpecRewritten,
		Outcome:               outcome,
		DurationSeconds:       elapsed.Seconds(),
		Duration:              ui.Duration(elapsed),
		CostNote:              domain.CostApproximationNote,
	}
}

// recordAbort writes the run record for a work item whose loop is leaving on an
// error no deliberate path handled - a store write that failed, a verification
// command that could not be executed, a blocking gate at workitem-started.
//
// Those are the paths that would otherwise leave a change request with no record
// at all, which reads exactly like a change request that was never attempted. A
// cancelled run is excluded: the item is resumable and has no outcome yet, so
// recording one would invent an abandonment that did not happen.
//
// It is a no-op once a record has been written, so the deliberate outcomes above
// keep theirs.
func recordAbort(ctx context.Context, store *internal.YAMLStore, crID string, item *domain.WorkItem, rec *runRecorder, err error) {
	if err == nil || rec.written || ctx.Err() != nil {
		return
	}
	writeRunTranscript(store, crID, item, rec, domain.RunFailed)
}

// routingOutcomeFor maps how the loop left a work item onto the routing
// vocabulary. A completed item passed; an item the loop halted because every
// escalation path was spent needs a human; anything else - max iterations, an
// error that ended the item - was abandoned.
func routingOutcomeFor(item *domain.WorkItem, outcome domain.RunOutcome) domain.RoutingOutcome {
	switch {
	case outcome == domain.RunCompleted:
		return domain.RoutingPassed
	case item.Status == domain.WorkItemNeedsHuman:
		return domain.RoutingNeedsHuman
	default:
		return domain.RoutingAbandoned
	}
}

// writeRunTranscript persists the work item's execution run to
// .utopia/runs/<cr-id>/<workitem-id>.yaml, carrying both the transcript and the
// routing record.
//
// It is called on every path out of the loop, not only the completing one, so an
// item that aborted or halted as needs_human is recorded too, carrying the usage of
// the attempts it did make. A change request's outcome is the set of these records
// in its run directory: one per work item, each carrying cr_id, spec_ref and
// cr_type, which is what makes escalation rate per spec and per cr_type a read of
// the records rather than of the transcripts.
//
// The usage list is projected from the attempts persisted on the work item, so a
// resumed item reports every attempt it ever made rather than the ones this process
// happened to observe.
//
// Logs a warning and returns on failure (non-blocking): a lost record costs
// future harvests some signal, the routing evidence and the spend for one item,
// none of which is worth stopping a run over.
func writeRunTranscript(store *internal.YAMLStore, crID string, item *domain.WorkItem, rec *runRecorder, outcome domain.RunOutcome) {
	rec.written = true
	run := &domain.ExecutionRun{
		WorkItemID:  item.ID,
		CRID:        crID,
		SpecRef:     item.SpecRef,
		Iterations:  item.IterationCount,
		CompletedAt: time.Now(),
		Outcome:     outcome,
		// A fresh run has never been reviewed, so it enters the harvest queue
		// unprocessed, exactly as a conversation does.
		Status:     domain.ConversationUnprocessed,
		Transcript: rec.transcript.String(),
		Routing:    routingRecordFor(item, rec.crType, routingOutcomeFor(item, outcome), rec.elapsed()),
		Usage:      domain.UsageEntriesFor(item.ExecutorAttempts),
	}
	if err := store.SaveExecutionRun(run); err != nil {
		fmt.Printf("  warning: failed to write run record for %s: %v\n", item.ID, err)
	}
}
