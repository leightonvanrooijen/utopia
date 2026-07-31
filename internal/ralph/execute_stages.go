package ralph

import (
	"context"
	"fmt"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/leightonvanrooijen/utopia/internal"
	"github.com/leightonvanrooijen/utopia/internal/domain"
	"github.com/leightonvanrooijen/utopia/internal/ui"
	"github.com/leightonvanrooijen/utopia/internal/validators"
	"github.com/leightonvanrooijen/utopia/internal/verification"
)

// workItemRun is one work item's pass through the Ralph loop: the collaborators
// every stage of that loop needs, plus the state accumulated across its
// iterations - the run record and the wall-clock breakdown.
//
// It exists so the loop body can read as the sequence of stages it already was.
// The stages need the same fifteen collaborators executeWorkItem is handed, and
// threading those through each stage as parameters would bury the sequence in
// its own argument lists. It is the same shape the scoper already uses for the
// rewrite path: a value built once per work item, holding what does not change
// while that item runs.
//
// Nothing here is shared between work items. The CLI is, which is why every
// execution attempt clones it rather than mutating it.
type workItemRun struct {
	ctx context.Context
	out *ui.Printer

	// item is the work item being executed. Every stage that mutates its status
	// or counters says so in its name, so the write sites are greppable.
	item   *domain.WorkItem
	specID string
	store  *internal.YAMLStore

	// crID, crTitle and operationType are resolved once from the change request:
	// the commit message, the execution log entry and the run record need them,
	// and none of them changes while the item runs.
	crID          string
	crTitle       string
	operationType string

	// cli is the run's shared CLI. Attempts clone it; the scoper borrows it.
	cli                    *internal.CLI
	defaultExecutorModel   string
	escalatedExecutorModel string
	efforts                roleEfforts
	// turnBudget is the per-iteration turn ceiling every execution attempt runs
	// under, escalated or not.
	turnBudget int

	verifier      *verification.Runner
	verifyCommand string
	// maxIterations is the outer bound on the item; caps are the inner bounds on
	// the retry paths. Both appear on the routing log line.
	maxIterations int
	caps          EscalationCaps

	projectDir string
	auth       domain.AuthMode

	dispatcher      *Dispatcher
	itemPayload     EventPayload
	validatorRunner *validators.Runner
	validatorList   []*domain.Validator

	// sc rewrites the change request when the executor keeps misreading it. It is
	// built once per work item rather than per escalation because its
	// dependencies do not change across one item's run.
	sc *scoper

	// rec is the run record written to .utopia/runs/ when the item finishes,
	// however it finishes.
	rec *runRecorder

	// tracer starts the spans this item's stages record: its own span, and a
	// child span for each Claude invocation, verification run, and validator
	// join. collector is where those spans land once ended, so the item's
	// completion line can read their durations back rather than accumulating
	// them by hand as the loop runs.
	tracer    trace.Tracer
	collector *spanCollector
	// itemSpan is this work item's span, started in newWorkItemRun and ended
	// exactly once by endItemSpan. Every stage's own span - Claude, verification,
	// the validator join - is a child of it, so it is what makes those children
	// siblings of each other and gives the item a single measured total: its own
	// duration, not a sum of theirs.
	itemSpan     trace.Span
	itemSpanDone bool
}

// newWorkItemRun resolves everything one work item's loop runs against: the
// bounds, the models, the scoper, the change request's identity and the record
// the item's outcome is written to.
func newWorkItemRun(
	ctx context.Context,
	out *ui.Printer,
	item *domain.WorkItem,
	specID string,
	store *internal.YAMLStore,
	cli *internal.CLI,
	defaultExecutorModel string,
	efforts roleEfforts,
	verifier *verification.Runner,
	config *domain.Config,
	projectDir string,
	auth domain.AuthMode,
	dispatcher *Dispatcher,
	basePayload EventPayload,
	validatorRunner *validators.Runner,
	validatorList []*domain.Validator,
	tracer trace.Tracer,
	collector *spanCollector,
) *workItemRun {
	r := &workItemRun{
		ctx:                    ctx,
		out:                    out,
		item:                   item,
		specID:                 specID,
		store:                  store,
		crID:                   extractCRID(specID),
		operationType:          "refactor", // default for refactor CRs
		cli:                    cli,
		defaultExecutorModel:   defaultExecutorModel,
		escalatedExecutorModel: resolveEscalatedExecutorModel(config.Models),
		efforts:                efforts,
		// The per-iteration turn ceiling. maxIterations bounds how many iterations
		// the item gets; this bounds what any one of them may spend, so the item's
		// ceiling is turnBudget x maxIterations rather than unbounded.
		turnBudget:    config.WorkItems.TurnBudgetOr(),
		verifier:      verifier,
		verifyCommand: config.Verification.Command,
		maxIterations: config.Verification.MaxIterations,
		// The escalation caps are the inner bounds on the retry paths;
		// maxIterations is the outer bound on the item as a whole. Both appear on
		// the routing log line so an operator can tell which one stopped the item.
		caps:            EscalationCapsFrom(config.Escalation),
		projectDir:      projectDir,
		auth:            auth,
		dispatcher:      dispatcher,
		validatorRunner: validatorRunner,
		validatorList:   validatorList,
		sc: &scoper{
			out:       out,
			store:     store,
			cli:       cli,
			model:     resolveScoperModel(config.Models),
			effort:    efforts.scoper,
			standards: store.LoadStandardsIndex(),
		},
	}

	// The item's own span, a child of the execution run's span carried on ctx.
	// Every stage's span - Claude, verification, the validator join - is
	// started against r.ctx below, so it nests as this span's child and the
	// item's completion line can read the durations back from the collector
	// rather than accumulating them by hand as the loop runs.
	itemCtx, itemSpan := tracer.Start(ctx, EventWorkItemStarted, trace.WithAttributes(
		attribute.String(attrCRID, r.crID),
		attribute.String(attrSpecRef, specID),
		attribute.String(attrWorkItemID, item.ID),
	))
	r.ctx = itemCtx
	r.tracer = tracer
	r.collector = collector
	r.itemSpan = itemSpan

	// The type the item's routing is recorded against, so escalation rate per
	// cr_type is a query over the run records. Left empty when the CR cannot be
	// loaded rather than defaulted to a type the item may not have.
	var crType domain.CRType
	if cr, err := store.LoadChangeRequest(r.crID); err == nil {
		r.crTitle = cr.Title
		r.operationType = deriveOperationType(cr, item.SpecRef)
		crType = deriveCRType(cr, specID)
	}

	// The run record: the transcript accumulated from the streamed Claude output
	// each iteration already produces for completion-token detection, plus the
	// wall clock and change request type its routing record needs.
	r.rec = newRunRecorder(crType)
	r.rec.out = out

	r.itemPayload = basePayload
	r.itemPayload.WorkItemID = item.ID
	r.itemPayload.IterationCount = item.IterationCount
	// Item-scoped events (workitem-completion-claimed, workitem-verified, ...)
	// dispatch through a Dispatcher wired once to the run's own context, so a
	// subscription action launched by one of them cannot see r.ctx's span.
	// Carrying the item span's context on the payload instead is what parents
	// its connector handles - a validator run, a gating connector - on the
	// item span rather than the execution run's.
	r.itemPayload.parentSpanCtx = itemSpan.SpanContext()

	return r
}

// endItemSpan ends the item's own span exactly once, carrying the final
// iteration count and how the item's run concluded. It is safe to call from
// both a deferred catch-all and a deliberate completion path: the second call
// is a no-op, guarded by itemSpanDone.
//
// The outcome favours the item's persisted status - completed, needs_human -
// over the bare presence of err, because a halted-needs-human item returns an
// error to stop the loop even though its own status already says what
// happened. Cancellation is called out on its own since it is neither a
// completion nor a status transition the item recorded.
func (r *workItemRun) endItemSpan(err error) {
	if r.itemSpanDone {
		return
	}
	r.itemSpanDone = true

	outcome := string(r.item.Status)
	switch {
	case r.ctx.Err() != nil:
		outcome = "cancelled"
	case err != nil && r.item.Status != domain.WorkItemNeedsHuman:
		outcome = "failed"
	}

	r.itemSpan.SetAttributes(
		attribute.Int(attrIterationCount, r.item.IterationCount),
		attribute.String(attrOutcome, outcome),
	)
	r.itemSpan.End()
}

// timingSummary renders the item's step-timing breakdown once its span has
// ended: the total is read back as the item span's own measured duration
// rather than summed from its children, so a validator run that overlapped
// verification is not double counted.
func (r *workItemRun) timingSummary() string {
	id := r.itemSpan.SpanContext().SpanID()
	return renderTimingSummary(
		r.collector.durationOf(id),
		r.collector.sumChildDurations(id, "claude"),
		r.collector.sumChildDurations(id, "verification"),
		r.collector.sumChildDurations(id, "validators"),
	)
}

// iterationPayload is the item's event payload stamped with the iteration
// currently running. Every dispatch from inside the loop carries it.
func (r *workItemRun) iterationPayload() EventPayload {
	p := r.itemPayload
	p.IterationCount = r.item.IterationCount
	return p
}

// abortIfCancelled reports a cancelled run (Ctrl+C), persisting the item's
// current state on the way out so the next run resumes from where this one
// stopped.
func (r *workItemRun) abortIfCancelled() error {
	select {
	case <-r.ctx.Done():
		persistWorkItemState(r.store, r.specID, r.item)
		return r.ctx.Err()
	default:
		return nil
	}
}

// chargeAndEscalateScoping is the resume path: an item that comes back already
// at the comprehension cap routes to scoping escalation before another execution
// attempt is spent on it. A decision made during this run is escalated at the
// gate instead, so reaching here means the counter was carried in on the
// persisted work item.
//
// The escalation is charged against the scoping cap before the rewrite runs, for
// the same reason an escalated execution attempt is: the cap bounds attempts, and
// an attempt that produces nothing has still been made. An item whose scoping cap
// is already spent has no path left - the change request was already rewritten as
// many times as it is allowed to be and still does not execute - so it halts,
// which is what stops the rewrite-then-retry cycle from becoming an unbounded
// loop.
//
// It reports whether it escalated, so the loop restarts rather than attempting
// execution against the specification the rewrite just replaced.
func (r *workItemRun) chargeAndEscalateScoping() (bool, error) {
	if r.item.ComprehensionCount < r.caps.ComprehensionEscalations {
		return false, nil
	}

	if r.item.ScopingEscalationCount >= r.caps.ScopingEscalations {
		return false, haltNeedsHuman(r.store, r.specID, r.crID, r.item, r.rec, &NeedsHumanError{
			WorkItemID: r.item.ID,
			Cap:        "escalation.scoping_escalations",
			Limit:      r.caps.ScopingEscalations,
			Detail: fmt.Sprintf("%d scoping escalation(s) did not produce a change request the executor could satisfy",
				r.item.ScopingEscalationCount),
		})
	}

	r.item.ScopingEscalationCount++
	esc := &ScopingEscalationError{WorkItemID: r.item.ID, ComprehensionCount: r.item.ComprehensionCount}
	if err := escalateScoping(r.ctx, r.sc, r.item, r.specID, r.crID, esc, r.caps, r.rec, escalationDiff(r.ctx, r.out, r.validatorRunner)); err != nil {
		return false, err
	}
	return true, nil
}

// startIteration opens an iteration on the work item: it counts the iteration,
// marks the item in progress and persists both, so a resume sees the iteration
// that was under way. An item past max_iterations is failed here rather than
// attempted - the outer bound is spent - and its run record written, because a
// run that never converged is a decision record too.
func (r *workItemRun) startIteration() error {
	r.item.IterationCount++
	r.item.Status = domain.WorkItemInProgress

	if r.maxIterations > 0 && r.item.IterationCount > r.maxIterations {
		persistWorkItemState(r.store, r.specID, r.item, func(item *domain.WorkItem) {
			item.Status = domain.WorkItemFailed
		})
		writeRunTranscript(r.store, r.crID, r.item, r.rec, domain.RunFailed)
		return fmt.Errorf("max iterations (%d) reached for work item %s", r.maxIterations, r.item.ID)
	}

	if err := r.store.SaveWorkItemForSpec(r.specID, r.item); err != nil {
		return fmt.Errorf("failed to save work item state: %w", err)
	}
	return nil
}

// buildAttemptPrompt constructs the prompt this attempt runs on. A mechanical
// retry gets the failure-injection prompt unchanged - the intent was right and
// the last diff is what it is fixing. An escalated attempt gets a freshly
// constructed context instead: the specification, the failures' conclusions and
// the evidence, without the attempts that produced them. Handing the escalated
// model the transcript of the attempts that just failed would anchor it to the
// reading that failed, which is the one thing escalation exists to escape.
//
// Escalation is read from the item's persisted comprehension counter, the same
// source the model resolution and the cap charge read, so all three agree on
// whether this attempt is escalated.
func (r *workItemRun) buildAttemptPrompt() (string, error) {
	if r.item.ComprehensionCount == 0 {
		return buildPrompt(r.item), nil
	}
	return buildEscalatedPrompt(r.store, r.item, r.crID, escalationDiff(r.ctx, r.out, r.validatorRunner))
}

// executorAttempt is what one execution attempt was booked to run as: the
// executor it runs on, the effort it runs at, and whether it was charged against
// the escalated-execution cap so it can be refunded if it never actually runs.
type executorAttempt struct {
	model   string
	effort  string
	charged bool
}

// chargeExecutorAttempt books the attempt the loop is about to make, against both
// the escalated-execution cap and the item's routing record. Both are charged
// before the attempt runs - so the halt costs nothing when the cap is already
// spent - and both are refunded together if the attempt never happens.
//
// Which executor the attempt runs on is derived from the item's persisted
// escalation state rather than from an in-memory flag, so a resumed item that
// already escalated does not reset to the default executor. The escalated
// executor's effort is its own role's level, not the default executor's raised: a
// mechanical retry stays on the default executor and so keeps that role's effort
// unchanged. Recording the model and effort here rather than reconstructing them
// later is what makes the record evidence - in particular the evidence that the
// default executor's effort was never raised.
func (r *workItemRun) chargeExecutorAttempt() (executorAttempt, error) {
	attempt := executorAttempt{
		model:  executorModelFor(r.item, r.defaultExecutorModel, r.escalatedExecutorModel),
		effort: executorEffortFor(r.item, r.efforts),
	}

	charged, capErr := chargeEscalatedAttempt(r.item, r.caps)
	if capErr != nil {
		r.item.IterationCount--
		return attempt, haltNeedsHuman(r.store, r.specID, r.crID, r.item, r.rec, capErr)
	}
	attempt.charged = charged

	recordExecutorAttempt(r.item, attempt.model, attempt.effort)
	return attempt, nil
}

// invocation is what one claude subprocess produced: its result, the wall clock
// it took measured from the outside, and the error it failed to run with.
type invocation struct {
	result  *internal.PromptResult
	elapsed time.Duration
	err     error
}

// invokeExecutor runs one execution attempt's claude subprocess.
//
// Every execution attempt runs under the turn budget, escalated or not: the
// budget bounds one iteration's spend, and an escalated attempt is still one
// iteration. The clone is what keeps the ceiling off the loop's shared CLI, which
// the scoper also borrows - a rewrite is not an execution iteration and is not
// budgeted as one. Usage capture is asked for on every execution attempt,
// escalated or not: the comparison the records exist to support is between the
// tiers, so an attempt missing its accounting is a hole in exactly the row that
// matters.
//
// The call is timed from the outside, so the duration covers whatever the agent
// did without the agent reporting anything; it is charged to the claude category
// on every path out, including the usage-limit retry, because the wall clock was
// spent either way.
func (r *workItemRun) invokeExecutor(prompt string, attempt executorAttempt) invocation {
	attemptCLI := r.cli.Clone().WithMaxTurns(r.turnBudget).WithUsageCapture(true)
	if attempt.model != r.defaultExecutorModel || attempt.effort != r.efforts.executor {
		attemptCLI = attemptCLI.WithModel(attempt.model).WithEffort(attempt.effort)
		r.out.Progressf("  Iteration %d: invoking Claude on the escalated executor (%s, effort %s)...\n", r.item.IterationCount, attempt.model, attempt.effort)
	} else {
		r.out.Progressf("  Iteration %d: invoking Claude...\n", r.item.IterationCount)
	}

	spanCtx, span := r.tracer.Start(r.ctx, "claude")
	start := time.Now()
	result, err := attemptCLI.Prompt(spanCtx, prompt)
	elapsed := time.Since(start)
	span.End()

	return invocation{result: result, elapsed: elapsed, err: err}
}

// refundAttemptOnUsageLimit detects and handles a Claude usage limit (rolling
// rate limit or org monthly spend limit) before the attempt is treated as a
// failed iteration.
//
// A handled limit means this attempt must not count against max iterations, so
// the increment is undone and the iteration re-runs with a normally rebuilt
// prompt. The attempt produced no work, so it is refunded: the escalated cap
// bounds spend on the expensive model and nothing was spent here, and the routing
// record is refunded with it so the attempt list stays a list of attempts that
// ran.
//
// A limit error means ctx was cancelled while waiting or probing (Ctrl+C or
// timeout), which takes the graceful shutdown path.
func (r *workItemRun) refundAttemptOnUsageLimit(attempt executorAttempt, inv invocation) (bool, error) {
	outcome, limitErr := handleClaudeLimits(r.ctx, r.out, inv.result, r.auth, r.projectDir)
	if limitErr != nil {
		persistWorkItemState(r.store, r.specID, r.item)
		return false, limitErr
	}
	if outcome != limitWaited {
		return false, nil
	}

	r.out.Progressf("  Iteration %d: usage limit handled, re-running this iteration (claude %s)\n", r.item.IterationCount, ui.Duration(inv.elapsed))
	r.item.IterationCount--
	refundEscalatedAttempt(r.item, attempt.charged)
	refundExecutorAttempt(r.item)
	return true, nil
}

// recordAttemptSpend books what the attempt spent onto its routing record. It
// runs before any of the paths out of the iteration - a turn-capped attempt, a
// failed invocation and a successful one all spent tokens, and all three are
// recorded.
func (r *workItemRun) recordAttemptSpend(inv invocation) {
	recordAttemptUsage(r.item, inv.result, inv.elapsed)
}

// appendIterationTranscript keeps this iteration's output on the run transcript
// before any of the paths out of the iteration take the loop elsewhere. A
// limit-handled attempt never reaches here: it produced no work and does not
// count as an iteration, so it would only misnumber the transcript.
func (r *workItemRun) appendIterationTranscript(inv invocation) {
	appendIterationOutput(&r.rec.transcript, r.item.IterationCount, inv.result)
}

// recordTurnExhaustion reports an attempt that spent its turn budget, which is
// expected operation rather than a fault: reporting it as such is what keeps it
// distinguishable from the non-zero exit of a crash or an auth error.
//
// Nothing else about the iteration changes: it counts against max iterations, no
// verification runs, and LastFailureOutput is deliberately left standing, because
// a capped iteration ran no verification and so has produced nothing that
// supersedes the last verification result there is.
//
// Nothing about the cap reaches the next prompt either. A message about running
// out of turns is a scarcity signal - it says hurry, and it invites shortcuts.
// The partial work is uncommitted in the working tree and the next iteration
// rebuilds its state from git and the files, so the capped iteration is a ratchet
// rather than wasted spend.
func (r *workItemRun) recordTurnExhaustion(inv invocation) bool {
	if inv.result == nil || !DetectTurnExhaustion(inv.result.Stdout, inv.result.Stderr) {
		return false
	}

	r.out.Progressf("  Iteration %d: turn budget of %d reached, continuing in a fresh iteration (claude %s)\n", r.item.IterationCount, r.turnBudget, ui.Duration(inv.elapsed))
	// A capped attempt claimed nothing, so nothing it produced was judged. Its
	// spend is recorded either way: the ratchet cost what it cost.
	recordAttemptOutcome(r.item, domain.AttemptIncomplete, "")
	// The invocation ran - it exhausted its turns doing work - so it is not an
	// infrastructure fault, and it clears the consecutive-error counter.
	clearInvocationErrors(r.item)
	return true
}

// chargeInvocationFailure books a claude invocation that failed to run against
// the item's consecutive-error counter, and reports whether the iteration ends
// here.
//
// The invocation produced no judgement about the work, so the escalated charge it
// was booked is returned: that cap bounds spend on evidence the executor got it
// wrong, and a crashed subprocess is not that evidence. Escalating on it would
// spend the expensive model on a fault the expensive model cannot fix.
//
// The attempt itself stays on the record, unlike the usage-limit refund: this one
// ran and spent, so what it cost is still accounted for even though its verdict
// is that there is none. The iteration stands too, so an item cannot run
// unbounded on errors alone. The halt is bounded on its own counter rather than
// on the comprehension budget: a claude that fails every time must not loop
// forever, and it must not consume the item's routing budget before a person
// reads the error.
func (r *workItemRun) chargeInvocationFailure(attempt executorAttempt, inv invocation) (bool, error) {
	if inv.err == nil {
		return false, nil
	}

	r.out.Progressf("  Iteration %d: Claude invocation failed: %v (claude %s)\n", r.item.IterationCount, inv.err, ui.Duration(inv.elapsed))
	recordAttemptOutcome(r.item, domain.AttemptErrored, "")
	refundEscalatedAttempt(r.item, attempt.charged)

	if haltErr := chargeInvocationError(r.item, r.caps, inv.err); haltErr != nil {
		return false, haltNeedsHuman(r.store, r.specID, r.crID, r.item, r.rec, haltErr)
	}
	// The attempt outcome and the escalation refund above are written before
	// chargeInvocationError reads them, so they stay at the call site rather than
	// moving into the transition.
	persistWorkItemState(r.store, r.specID, r.item)
	return true, nil
}

// recordCompletionClaim reports whether the attempt claimed the work was
// finished. An attempt that claimed nothing hit its step limit or got stuck, so
// it is recorded incomplete and the previous failure is cleared: this is a
// different failure mode from the one that output describes.
func (r *workItemRun) recordCompletionClaim(inv invocation) bool {
	if !strings.Contains(inv.result.Stdout, CompletionToken) {
		r.out.Progressf("  Iteration %d: no %s token found, retrying... (claude %s)\n", r.item.IterationCount, CompletionToken, ui.Duration(inv.elapsed))
		recordAttemptOutcome(r.item, domain.AttemptIncomplete, "")
		r.item.LastFailureOutput = ""
		r.item.LastValidatorFeedback = ""
		return false
	}

	r.out.Progressf("  Iteration %d: %s token found, running verification... (claude %s)\n", r.item.IterationCount, CompletionToken, ui.Duration(inv.elapsed))
	return true
}

// routeValidatorsOnce routes validators for this work item the first time it
// reaches the gate. The cheap relevance router picks which validators apply to
// the change; the selection is persisted on the item so retries and resumes reuse
// the one decision rather than re-routing every iteration. A router failure
// returns "all applicable" (still recorded so we don't retry the failing call); a
// diff failure leaves the item unrouted so the gate falls back to running all
// applicable validators next dispatch.
func (r *workItemRun) routeValidatorsOnce() {
	if r.item.ValidatorsRouted || len(r.validatorList) == 0 {
		return
	}

	diff, diffErr := r.validatorRunner.GetGitDiff(r.ctx)
	if diffErr != nil {
		r.out.Progressf("  Iteration %d: could not compute diff for validator routing: %v\n", r.item.IterationCount, diffErr)
		return
	}

	selected, routeErr := r.validatorRunner.SelectApplicable(r.ctx, r.validatorList, diff)
	if routeErr != nil {
		r.out.Progressf("  Iteration %d: validator router failed, running all applicable: %v\n", r.item.IterationCount, routeErr)
	}
	persistWorkItemState(r.store, r.specID, r.item, func(item *domain.WorkItem) {
		item.SelectedValidators = selected
		item.ValidatorsRouted = true
	})
}

// announceCompletionClaimed signals completion claimed but not yet verified. This
// launches speculative work: after-workitem validators start now (via the engine)
// and run concurrently with verification, to be joined at workitem-verified or
// abandoned at workitem-verification-failed. Notify connectors also start here.
// Fire-and-forget at this point: notify only. The router's selection rides on the
// payload so the validators action runs only the chosen subset.
func (r *workItemRun) announceCompletionClaimed() {
	p := r.iterationPayload()
	p.SelectedValidatorIDs = r.item.SelectedValidators
	p.ValidatorsRouted = r.item.ValidatorsRouted
	r.dispatcher.Dispatch(Event{Name: EventWorkItemCompletionClaimed, Payload: p})
}

// verificationOutcome is what the verification command concluded, and how long
// the loop waited for it.
type verificationOutcome struct {
	passed  bool
	output  string
	elapsed time.Duration
}

// runVerification runs the verification command. Validators are already running
// speculatively; the engine joins them at workitem-verified afterwards. An empty
// verification command is treated as trivially verified.
func (r *workItemRun) runVerification() (verificationOutcome, error) {
	if r.verifyCommand == "" {
		r.out.Progressf("  Iteration %d: no verification command configured, marking complete\n", r.item.IterationCount)
		return verificationOutcome{passed: true}, nil
	}

	spanCtx, span := r.tracer.Start(r.ctx, "verification")
	start := time.Now()
	result, err := r.verifier.Run(spanCtx, r.verifyCommand)
	elapsed := time.Since(start)
	span.End()
	if err != nil {
		return verificationOutcome{}, fmt.Errorf("verification command failed to execute: %w", err)
	}

	if result.Passed {
		r.out.Progressf("  Iteration %d: verification passed! (verification %s)\n", r.item.IterationCount, ui.Duration(elapsed))
	}
	return verificationOutcome{passed: result.Passed, output: result.Output, elapsed: elapsed}, nil
}

// recordVerificationFailure injects the failure for the next attempt to fix, and
// signals that the completion claim did not hold so the engine cancels the
// speculative validators; their feedback is discarded.
//
// The human running the loop sees exactly what the runner is about to be handed.
// The output is already truncated by verifier.Run, so the printed block is
// byte-identical to what lands in item.LastFailureOutput and in the retry prompt.
// Verification does not resolve through the engine, so this is the one failure
// source that renders its own block; everything else reaches the same helper
// through the resolution ledger.
func (r *workItemRun) recordVerificationFailure(v verificationOutcome) {
	r.out.Progressf("  Iteration %d: verification failed, will retry with failure output (verification %s)\n", r.item.IterationCount, ui.Duration(v.elapsed))
	printFailureBlock(r.out, failureSourceVerification, v.output)
	r.dispatcher.Dispatch(Event{Name: EventWorkItemVerificationFailed, Payload: r.iterationPayload()})
	// A failing verification command is a mechanical failure by the class
	// vocabulary's own terms - the intent was right and the execution slipped -
	// and it is the same default the routing applies to a failure no validator
	// classified.
	recordAttemptOutcome(r.item, domain.AttemptFailed, validators.FailureMechanical)
	r.item.LastFailureOutput = v.output
	r.item.LastValidatorFeedback = "" // Clear any previous validator feedback
}

// gateJoin is what the validation gate concluded, and how long the loop waited to
// join it.
type gateJoin struct {
	err     error
	elapsed time.Duration
}

// joinValidationGate joins the speculative validators (and any gating connectors)
// at workitem-verified. A blocking gate - a failing validator or connector -
// prevents the commit.
//
// The dispatch is the join point, so timing it measures what the loop still had
// to wait for the validators to finish - their run already overlapped
// verification. Charging the wait rather than the whole validator run keeps the
// categories a breakdown of real wall clock; the engine's resolution ledger
// reports each run's full duration.
func (r *workItemRun) joinValidationGate() gateJoin {
	_, span := r.tracer.Start(r.ctx, "validators")
	start := time.Now()
	err := r.dispatcher.Dispatch(Event{Name: EventWorkItemVerified, Payload: r.iterationPayload()})
	elapsed := time.Since(start)
	span.End()
	return gateJoin{err: err, elapsed: elapsed}
}

// routeGateFailure decides what a blocking validation gate means for the item: a
// gate that reached no verdict is retried on its own streak counter, and a gate
// that reached one is routed on the failure class the validators reported.
//
// Returning nil means the loop retries the item; any error ends it.
func (r *workItemRun) routeGateFailure(join gateJoin) error {
	aggregate := aggregateFromGate(join.err)
	if gateUnresolved(aggregate) {
		return r.chargeUnresolvedGateAndRetry(join, aggregate)
	}
	return r.recordAndRouteFailedGate(join, aggregate)
}

// chargeUnresolvedGateAndRetry books a gate that blocked because the validators
// could not be run, rather than because they rejected the work.
//
// No statement was made about the code, so none of the routing state moves:
// routeValidationFailure is not consulted, the comprehension counter stands, the
// executor is not escalated and no conclusions are recorded - there are none to
// record.
//
// The verbatim feedback stands too. Overwriting it with the infrastructure fault
// would lose a genuine earlier verdict the next attempt still has to answer, and
// would tell the executor a validator disapproved when no validator spoke.
// LastFailureOutput is cleared as on any other path out of a passing
// verification: that output is superseded whatever the gate did.
//
// The escalated-execution charge is deliberately not refunded. That cap bounds
// spend on the expensive model, and this attempt ran and spent; the unresolved
// streak is what bounds the retries.
func (r *workItemRun) chargeUnresolvedGateAndRetry(join gateJoin, aggregate *validators.AggregateResult) error {
	r.out.Progressf("  Iteration %d: gate blocked workitem-verified: the validators could not run (validators %s)\n", r.item.IterationCount, ui.Duration(join.elapsed))
	cause := unresolvedGateCause(aggregate)
	haltErr := chargeUnresolvedGate(r.item, r.caps, cause)
	r.out.Progressf("  %s\n", unresolvedGateLogLine(r.item, r.caps, r.item.IterationCount, r.maxIterations))
	// The attempt reached no verdict, which is what AttemptErrored records; it
	// carries no class, because a class is what a verdict would have said.
	recordAttemptOutcome(r.item, domain.AttemptErrored, "")
	r.item.LastFailureOutput = ""

	if haltErr != nil {
		return haltNeedsHuman(r.store, r.specID, r.crID, r.item, r.rec, haltErr)
	}
	if err := r.store.SaveWorkItemForSpec(r.specID, r.item); err != nil {
		return fmt.Errorf("failed to save work item state: %w", err)
	}
	return nil
}

// recordAndRouteFailedGate routes a gate that reached a verdict, on the failure
// class the validators reported rather than on the iteration count. A mechanical
// failure retries on the same executor; a comprehension failure escalates it;
// repeated comprehension failure, or a suspected spec defect, escalates the
// change request instead.
//
// The failing validators' feedback was already shown as a failure-output block by
// the engine's resolution ledger when the dispatch joined them, so the human sees
// it before the injection here and it is not printed again.
func (r *workItemRun) recordAndRouteFailedGate(join gateJoin, aggregate *validators.AggregateResult) error {
	// The gate reached a verdict, so the unresolved streak - if any - is over.
	clearUnresolvedGates(r.item)
	decision := routeValidationFailure(r.item, aggregate, r.caps, r.defaultExecutorModel, r.escalatedExecutorModel)
	r.out.Progressf("  Iteration %d: gate blocked workitem-verified (validators %s)\n", r.item.IterationCount, ui.Duration(join.elapsed))
	r.out.Progressf("  %s\n", decision.logLine(r.item.IterationCount, r.maxIterations, r.caps))
	r.item.LastFailureOutput = ""
	r.item.LastValidatorFeedback = gateFeedback(join.err)
	// What this attempt concluded is kept, persisted with the item, so a later
	// escalated attempt can be handed the conclusions without the attempt. The
	// verbatim feedback above is still only the previous iteration's, which is
	// what a mechanical retry is given.
	recordFailureConclusions(r.item, aggregate)
	// The attempt is recorded as rejected under the class the validators reported,
	// not the class the caps routed on: the attempt's own record says what was
	// concluded about it, and decision.Class already says what the routing did
	// next.
	recordAttemptOutcome(r.item, domain.AttemptFailed, reportedFailureClass(aggregate))
	if err := r.store.SaveWorkItemForSpec(r.specID, r.item); err != nil {
		return fmt.Errorf("failed to save work item state: %w", err)
	}

	switch decision.Route {
	case RouteNeedsHuman:
		return haltNeedsHuman(r.store, r.specID, r.crID, r.item, r.rec, &NeedsHumanError{
			WorkItemID: r.item.ID,
			Cap:        decision.CapExhausted,
			Limit:      r.caps.ScopingEscalations,
			Detail: fmt.Sprintf("%d scoping escalation(s) did not produce a change request the executor could satisfy",
				r.caps.ScopingEscalations),
			Cause: scopingEscalationError(r.item, aggregate, decision),
		})
	case RouteScopingEscalation:
		// The change request, not the code, is what gets rewritten here. A rewrite
		// the loop can resume against returns nil and execution carries on against
		// the new specification; anything else stops the item.
		return escalateScoping(r.ctx, r.sc, r.item, r.specID, r.crID, scopingEscalationError(r.item, aggregate, decision), r.caps, r.rec, escalationDiff(r.ctx, r.out, r.validatorRunner))
	}
	return nil
}

// markWorkItemCompleted is the item's terminal state change: it passed
// verification and the gate, so its status, its attempt record and its stale
// feedback are settled and persisted together.
func (r *workItemRun) markWorkItemCompleted() error {
	r.item.Status = domain.WorkItemCompleted
	clearUnresolvedGates(r.item)
	recordAttemptOutcome(r.item, domain.AttemptPassed, "")
	r.item.LastFailureOutput = ""
	r.item.LastValidatorFeedback = ""
	return r.store.SaveWorkItemForSpec(r.specID, r.item)
}

// recordCompletedRun writes the item's telemetry: the execution log entry on the
// conversations that reference the change request, and the run record. Both are
// written before the commit so the transcript is picked up by the same
// `git add -A` and lands in the work item's own commit.
func (r *workItemRun) recordCompletedRun() {
	logExecutionEntry(r.out, r.store, r.crID, r.item, r.operationType)
	writeRunTranscript(r.store, r.crID, r.item, r.rec, domain.RunCompleted)
}

// commitWorkItem commits the completed work item and announces the commit.
func (r *workItemRun) commitWorkItem() {
	p := r.iterationPayload()
	p.CommitSHA = gitCommitWorkItem(r.out, r.projectDir, r.item, r.crTitle)
	r.dispatcher.Dispatch(Event{Name: EventWorkItemCommitted, Payload: p})
}
