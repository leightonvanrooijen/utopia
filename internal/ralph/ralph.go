package ralph

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/leightonvanrooijen/utopia/internal"
	"github.com/leightonvanrooijen/utopia/internal/domain"
	"github.com/leightonvanrooijen/utopia/internal/git"
	"github.com/leightonvanrooijen/utopia/internal/layout"
	"github.com/leightonvanrooijen/utopia/internal/ui"
	"github.com/leightonvanrooijen/utopia/internal/validators"
	"github.com/leightonvanrooijen/utopia/internal/verification"
)

// CompletionToken is the marker that indicates Claude has finished the task.
const CompletionToken = "<COMPLETE>"

// Result represents the outcome of executing work items.
type Result struct {
	// Completed is the count of successfully completed work items
	Completed int
	// Total is the total number of work items attempted
	Total int
	// StoppedAt is the ID of the work item where execution stopped (if not all completed)
	StoppedAt string
	// Reason explains why execution stopped (if not all completed)
	Reason string
	// NeedsHuman lists the work items halted as needs_human. They neither completed
	// nor stopped the run: the loop moved on to the next item and left them for a
	// person. A non-empty list means the phase is incomplete even though the run
	// returned no error.
	NeedsHuman []string
}

// Overrides carries the per-invocation flag overrides for one execution run.
// Each field is empty unless the corresponding flag was passed, in which case it
// wins over what config resolves for every role in the run.
//
// Model and Effort are separate levers with different economics, so they are
// separate fields rather than one setting: --model changes which model runs,
// --effort changes how much reasoning a turn may spend. Neither is touched again
// once the run starts.
//
// Out rides along here rather than as a positional parameter because the call
// tree below Execute is wide: the printer reaches the work-item loop, the
// escalation paths, the engine's resolution ledger and the claude subprocesses
// from one place the caller already constructs.
type Overrides struct {
	// Model is a Claude model override: an alias the CLI resolves (e.g. "opus")
	// or a full model identifier.
	Model string
	// Effort is a reasoning-effort override (low, medium, high, xhigh, max).
	Effort string
	// Out receives everything the run prints. The CLI hands in the printer it
	// built over cobra's writers, so a run's output is capturable; nil falls back
	// to the process's own streams.
	Out *ui.Printer
}

// Execute runs all work items for a spec sequentially.
// Work items are processed one at a time, in order, retrying until
// verification passes or max iterations is reached.
// The over parameter carries this invocation's --model and --effort overrides.
//
// auth selects the credential every claude subprocess this loop spawns
// authenticates with - the work-item agents, the validators they gate on, and
// the spend-limit probe alike. One loop can run for hours across hundreds of
// subprocesses, so a mode that reached only some of them would split a single
// run's usage across two accounts. The empty mode inherits the ambient
// environment, which is the pre-auth behaviour.
func Execute(ctx context.Context, specID string, store *internal.YAMLStore, config *domain.Config, projectDir string, auth domain.AuthMode, over Overrides) (result *Result, err error) {
	// Every diagnostic this run emits carries the change request it belongs to, so
	// no call site below has to interpolate the ID into its message. Attached here,
	// before the printer reaches the claude subprocesses, the engine or the
	// work-item loop, so a run has no path that emits a diagnostic without it.
	out := ui.OrDefault(over.Out).WithAttrs(slog.String("cr_id", extractCRID(specID)))

	// The run's own span tree: an in-process collector receives every span this
	// run produces and nothing else - no exporter, no collector process, no
	// network call - so adding one later is registering it alongside this
	// processor rather than re-modelling how the loop reports timing.
	tp, collector := newTracerProvider()
	tracer := tp.Tracer(tracerName)
	execCtx, execSpan := tracer.Start(ctx, EventExecutionStarted, trace.WithAttributes(
		attribute.String(attrCRID, extractCRID(specID)),
		attribute.String(attrSpecRef, specID),
	))
	ctx = execCtx
	defer func() {
		outcome := "completed"
		switch {
		case err != nil:
			outcome = "failed"
		case result != nil && len(result.NeedsHuman) > 0:
			outcome = "needs_human"
		case result != nil && result.Reason != "":
			outcome = "failed"
		}
		execSpan.SetAttributes(attribute.String(attrOutcome, outcome))
		execSpan.End()
	}()

	// Load work items for this spec
	items, err := store.ListWorkItemsForSpec(specID)
	if err != nil {
		return nil, fmt.Errorf("failed to load work items: %w", err)
	}

	if len(items) == 0 {
		return &Result{
			Completed: 0,
			Total:     0,
			Reason:    "no work items found",
		}, nil
	}

	// Sort by Order
	sort.Slice(items, func(i, j int) bool {
		return items[i].Order < items[j].Order
	})

	result = &Result{
		Total: len(items),
	}

	// Create dependencies. The default executor's model is the one the escalation
	// routing retries mechanical failures on, so it is resolved once here from the
	// same fallback chain every other role uses - models.execute > models.default >
	// sonnet - with --model winning for this invocation only. It is always set on
	// the shared CLI, so every default-executor attempt carries --model explicitly
	// rather than inheriting whatever the claude binary defaults to.
	//
	// Effort is resolved once here, for the whole run: every role's level is fixed
	// before the first work item starts and nothing below recomputes it, which is
	// what makes "no code path raises effort on failure" a property of the
	// structure rather than a convention.
	// The executor's transcript is an info-level diagnostic: watching claude work
	// is what `utopia execute` is for, so it appears at the default level and
	// falls silent only when the run is asked to be quieter than info.
	cli := internal.NewCLI().WithStreamLevel(slog.LevelInfo).WithAuth(auth, layout.Root(projectDir)).WithPrinter(out)
	efforts := resolveRoleEfforts(config.Effort, over.Effort)
	cli = cli.WithEffort(efforts.executor)
	defaultExecutorModel := resolveDefaultExecutorModel(config.Models, over.Model)
	cli = cli.WithModel(defaultExecutorModel)
	verifier := verification.NewRunner(projectDir)
	validatorRunner := validators.NewRunner(projectDir).WithModelConfig(config.Models).WithEffort(efforts.validators).WithAuth(auth).
		WithInvocationRetries(domain.CapOr(config.Verification.ValidatorInvocationRetries, validators.DefaultValidatorInvocationRetries))

	// Load validators from config
	var validatorList []*domain.Validator
	for _, vc := range config.Validators {
		v, err := store.LoadValidator(vc.GetPath())
		if err != nil {
			return nil, fmt.Errorf("failed to load validator %s: %w", vc.GetPath(), err)
		}
		// Apply config overrides
		if vc.GetModel() != "" {
			v.ModelOverride = vc.GetModel()
		}
		if vc.GetRun() != "" {
			v.Run = domain.RunTrigger(vc.GetRun())
		}
		v.Always = vc.GetAlways()
		validatorList = append(validatorList, v)
	}

	// Create the lifecycle event dispatcher and wire the subscription engine.
	// Configured validators and connectors both compile to engine
	// subscriptions: after-workitem validators launch speculatively at
	// completion-claimed and join/cancel around verification, while connectors
	// keep their notify/gating shapes. Validators are registered before
	// connectors so a failing validator's feedback wins over a gating connector
	// on the same event, matching the pre-migration order where validators were
	// checked first. Notify failures are logged warnings; gating failures
	// propagate as *GateError through Dispatch to block loop progression.
	dispatcher := NewDispatcher()
	subs := CompileValidators(validatorRunner, validatorList, config.Verification.ValidatorConcurrency)
	subs = append(subs, CompileConnectors(config.Connectors, projectDir)...)
	if len(subs) > 0 {
		engine := NewEngine(subs)
		engine.out = out
		engine.tracer = tracer
		runner := &ConnectorRunner{engine: engine}
		dispatcher.Subscribe(func(e Event) error {
			return runner.Handle(ctx, e)
		})
	}
	basePayload := EventPayload{
		CRID:   extractCRID(specID),
		SpecID: specID,
		// Run-scoped events (execution-started, phase-verified, execution-completed)
		// dispatch through a Dispatcher wired once to this ctx, so a subscription
		// action they launch cannot see whatever span ctx carries at dispatch time.
		// Carrying the execution span's context on the payload is what parents
		// those connector handles on the run's own span.
		parentSpanCtx: execSpan.SpanContext(),
	}
	if cr, err := store.LoadChangeRequest(basePayload.CRID); err == nil {
		basePayload.CRTitle = cr.Title
	}
	// Record HEAD before any work item runs. After-phase validators diff
	// against this baseline so they review the cumulative changes of every
	// commit produced during the phase, not just the last work item. A repo
	// with no commits yet yields an empty SHA; the diff falls back gracefully.
	if sha, err := git.HeadSHA(projectDir); err == nil {
		basePayload.PhaseStartSHA = sha
	}

	// A gate blocking execution-started aborts the run before any work starts.
	if gateErr := dispatcher.Dispatch(Event{Name: EventExecutionStarted, Payload: basePayload}); gateErr != nil {
		result.Reason = gateErr.Error()
		failPayload := basePayload
		failPayload.Reason = gateErr.Error()
		dispatcher.Dispatch(Event{Name: EventExecutionFailed, Payload: failPayload})
		return result, gateErr
	}

	// Execute each work item in order
	for i, item := range items {
		// The item's printer adds it to the run's attributes, so every diagnostic
		// raised while this item is the one being considered says which item it
		// belongs to - the skip decisions below included.
		itemOut := out.WithAttrs(slog.String("work_item_id", item.ID))
		itemOut.Debug("work item reached",
			slog.Int("position", i+1),
			slog.Int("total", len(items)),
			slog.String("status", string(item.Status)),
			slog.Int("iteration_count", item.IterationCount))

		// Skip completed items
		if item.Status == domain.WorkItemCompleted {
			result.Completed++
			itemOut.Progressf("[%d/%d] %s - already completed\n", i+1, len(items), item.ID)
			continue
		}

		// An item halted as needs_human on an earlier run is not retried. Its caps
		// are persisted, so re-entering the loop would exhaust them again and halt at
		// the same place; the change request has to change first. Once a person has
		// acted, 'utopia requeue' clears those caps and returns the item to pending,
		// and the next run picks it up here in its usual order.
		if item.Status == domain.WorkItemNeedsHuman {
			result.NeedsHuman = append(result.NeedsHuman, item.ID)
			itemOut.Progressf("[%d/%d] %s - halted, needs human attention (skipped)\n", i+1, len(items), item.ID)
			continue
		}

		itemOut.Progressf("[%d/%d] %s - starting execution\n", i+1, len(items), item.ID)

		// Execute this work item with the Ralph loop
		timingLine, err := executeWorkItem(ctx, itemOut, item, specID, store, cli, defaultExecutorModel, efforts, verifier, config, projectDir, auth, dispatcher, basePayload, validatorRunner, validatorList, tracer, collector)
		if err != nil {
			// A halted item is skipped, not fatal. Batch execution runs every draft
			// change request in order, so aborting the run on one ambiguous change
			// request would strand every item behind it - and an item needing a human
			// is precisely the case where the rest of the batch is still worth running.
			var needsHuman *NeedsHumanError
			if errors.As(err, &needsHuman) {
				result.NeedsHuman = append(result.NeedsHuman, item.ID)
				itemOut.Progressf("[%d/%d] %s - halted after %d iteration(s), needs human attention: %s\n",
					i+1, len(items), item.ID, item.IterationCount, err)
				continue
			}
			result.StoppedAt = item.ID
			result.Reason = err.Error()
			failPayload := basePayload
			failPayload.Reason = err.Error()
			dispatcher.Dispatch(Event{Name: EventExecutionFailed, Payload: failPayload})
			return result, err
		}

		result.Completed++
		itemOut.Progressf("[%d/%d] %s - completed in %d iteration(s)\n", i+1, len(items), item.ID, item.IterationCount)
		// Where the item's wall clock went, under the line that announced it
		// finished. Only a completed item carries a timing line.
		if timingLine != "" {
			itemOut.Progressf("  timing: %s\n", timingLine)
		}
	}

	// Run after-phase validators once all work items are complete. This also
	// fires phase-verified and phase-completed (and their gating connectors)
	// even when no validators are configured.
	if result.Completed == result.Total {
		if err := runAfterPhaseValidators(ctx, out, cli, config, projectDir, auth, dispatcher, basePayload, validatorRunner, validatorList, tracer); err != nil {
			result.Reason = err.Error()
			failPayload := basePayload
			failPayload.Reason = err.Error()
			dispatcher.Dispatch(Event{Name: EventExecutionFailed, Payload: failPayload})
			return result, err
		}
		dispatcher.Dispatch(Event{Name: EventExecutionCompleted, Payload: basePayload})
	}

	// The run itself did not fail - every item that could be attempted was - so the
	// halted items are reported on the result rather than as an error, and the
	// caller decides what an incomplete phase means for merging.
	if len(result.NeedsHuman) > 0 {
		result.Reason = fmt.Sprintf("%d work item(s) halted needing human attention", len(result.NeedsHuman))
	}

	return result, nil
}

// executeWorkItem runs the Ralph loop for a single work item until completion.
// A completed item returns the rendered step-timing line, so the caller can
// report the breakdown alongside the item's completion line; an item that
// stops early returns an empty line.
//
// The body is the loop's stage list: every iteration builds a prompt, invokes
// the executor, accounts for what it spent, gates it, and either routes the
// failure or commits the work. Each stage is a method on workItemRun, and every
// stage that writes to the item's status or counters says so in its name.
func executeWorkItem(
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
) (timingLine string, err error) {
	r := newWorkItemRun(ctx, out, item, specID, store, cli, defaultExecutorModel, efforts,
		verifier, config, projectDir, auth, dispatcher, basePayload, validatorRunner, validatorList, tracer, collector)

	// Every way out of this loop leaves a record, not only the outcomes the loop
	// routed to. An item that aborted on an error nobody anticipated is exactly the
	// item worth having a record of, and no record at all reads like a change
	// request that was never attempted.
	defer func() { recordAbort(ctx, store, r.crID, item, r.rec, err) }()
	// The item's span closes on every way out of this loop, carrying the final
	// iteration count and outcome; a deliberate completion below ends it early
	// so the collector has the item's total by the time the summary is read.
	defer func() { r.endItemSpan(err) }()

	// A gate blocking workitem-started aborts the run before the item runs.
	if gateErr := dispatcher.Dispatch(Event{Name: EventWorkItemStarted, Payload: r.itemPayload}); gateErr != nil {
		return "", gateErr
	}

	for {
		if err := r.abortIfCancelled(); err != nil {
			return "", err
		}

		escalated, err := r.chargeAndEscalateScoping()
		if err != nil {
			return "", err
		}
		if escalated {
			continue
		}

		if err := r.startIteration(); err != nil {
			return "", err
		}

		prompt, err := r.buildAttemptPrompt()
		if err != nil {
			return "", err
		}

		attempt, err := r.chargeExecutorAttempt()
		if err != nil {
			return "", err
		}

		inv := r.invokeExecutor(prompt, attempt)

		refunded, err := r.refundAttemptOnUsageLimit(attempt, inv)
		if err != nil {
			return "", err
		}
		if refunded {
			continue
		}

		r.recordAttemptSpend(inv)
		r.appendIterationTranscript(inv)

		if r.recordTurnExhaustion(inv) {
			continue
		}

		invocationFailed, err := r.chargeInvocationFailure(attempt, inv)
		if err != nil {
			return "", err
		}
		if invocationFailed {
			continue
		}
		// The invocation ran, so whatever comes of what it produced, the fault the
		// error counter bounds is not currently happening.
		clearInvocationErrors(item)

		if !r.recordCompletionClaim(inv) {
			continue
		}

		r.routeValidatorsOnce()
		r.announceCompletionClaimed()

		verified, err := r.runVerification()
		if err != nil {
			return "", err
		}
		if !verified.passed {
			r.recordVerificationFailure(verified)
			continue
		}

		if join := r.joinValidationGate(); join.err != nil {
			if err := r.routeGateFailure(join); err != nil {
				return "", err
			}
			continue
		}

		if err := r.markWorkItemCompleted(); err != nil {
			return "", err
		}
		// The item's span ends here, before the record is written, so the
		// persisted spans include the item's own total rather than only its
		// children - the deferred catch-all above is a no-op once this runs.
		r.endItemSpan(nil)
		r.recordCompletedRun()
		r.commitWorkItem()
		return r.timingSummary(), nil
	}
}

// buildPrompt constructs the prompt for Claude, including failure injection.
// Test failures and validator feedback are injected as separate sections.
func buildPrompt(item *domain.WorkItem) string {
	// Start with the base prompt from the work item
	prompt := item.Prompt

	// Inject test failures (verification failures) if present
	if item.LastFailureOutput != "" {
		prompt = prompt + "\n\n## PREVIOUS FAILURES\n\nThe previous attempt failed with the following test output:\n\n```\n" + item.LastFailureOutput + "\n```\n\nYou must fix all failures regardless of whether they were introduced by this work item. The verification command must pass before this work item can be completed."
	}

	// Inject validator feedback as a separate section if present
	// This section is only reached when verification passed but validators failed
	if item.LastValidatorFeedback != "" {
		prompt = prompt + "\n\n## PROJECT STANDARDS FEEDBACK\n\n" +
			"**Your implementation meets all acceptance criteria** (tests pass), but violates project standards.\n\n" +
			"The following validators detected standards issues:\n\n" +
			item.LastValidatorFeedback + "\n" +
			"Please fix these standards violations while preserving all functionality. Do not break any tests."
	}

	return prompt
}

// gateFeedback formats a blocking gate error for prompt injection, carrying
// the gating connector's stdout into the next iteration's feedback section.
func gateFeedback(err error) string {
	var ge *GateError
	if errors.As(err, &ge) && strings.TrimSpace(ge.Stdout) != "" {
		return fmt.Sprintf("Gating connector %s blocked %s:\n%s\n", ge.Connector, ge.Event, ge.Stdout)
	}
	return err.Error() + "\n"
}

// extractCRID extracts the change request ID from a specID.
// For regular CRs, specID is the CR ID directly.
// For initiatives, specID is "cr-id/phase-N", so we extract the first part.
func extractCRID(specID string) string {
	if idx := strings.Index(specID, "/"); idx != -1 {
		return specID[:idx]
	}
	return specID
}

// deriveOperationType determines the operation type for a work item from its CR.
// Returns "add", "modify", "remove" for feature/enhancement/removal CRs,
// or "refactor" for refactor/bugfix CRs.
func deriveOperationType(cr *domain.ChangeRequest, specRef string) string {
	// For refactor or bugfix CRs, use "refactor"
	if cr.Type == domain.CRTypeRefactor || cr.Type == domain.CRTypeBugfix {
		return "refactor"
	}

	// For feature/enhancement/removal CRs, find the matching change by spec ref
	// SpecRef format is "spec-id.feature-id"
	for _, change := range cr.Changes {
		// Check if this change matches the work item's spec ref
		if change.Feature != nil && change.Spec+"."+change.Feature.ID == specRef {
			return change.Operation
		}
		if change.FeatureID != "" && change.Spec+"."+change.FeatureID == specRef {
			return change.Operation
		}
	}

	// Default based on CR type
	switch cr.Type {
	case domain.CRTypeFeature:
		return "add"
	case domain.CRTypeEnhancement:
		return "modify"
	case domain.CRTypeRemoval:
		return "remove"
	default:
		return "refactor"
	}
}

// logExecutionEntry appends an execution log entry to conversations that reference the CR.
func logExecutionEntry(out *ui.Printer, store *internal.YAMLStore, crID string, item *domain.WorkItem, operation string) {
	entry := domain.ExecutionLogEntry{
		WorkItemID:  item.ID,
		SpecRef:     item.SpecRef,
		Operation:   operation,
		CompletedAt: time.Now(),
	}
	if err := store.AppendExecutionLogEntry(crID, entry); err != nil {
		out.Progressf("  warning: failed to log execution entry: %v\n", err)
	}
}

// appendIterationOutput accumulates one iteration's streamed Claude output onto
// the run transcript. Every iteration is kept, not only the one that completed:
// an abandoned attempt carries the reasoning for what was tried and dropped,
// which is exactly the kind of implementation decision a harvest looks for.
// An iteration that produced no output (Claude failed before writing anything)
// contributes nothing.
func appendIterationOutput(transcript *strings.Builder, iteration int, result *internal.PromptResult) {
	if result == nil || result.Stdout == "" {
		return
	}
	if transcript.Len() > 0 {
		transcript.WriteString("\n")
	}
	fmt.Fprintf(transcript, "--- iteration %d ---\n%s", iteration, result.Stdout)
}

// gitCommitWorkItem creates a git commit after a work item passes verification.
// Logs warning and returns on failure (non-blocking).
// Returns the SHA of the created commit, or "" if no commit was created.
func gitCommitWorkItem(out *ui.Printer, projectDir string, item *domain.WorkItem, crTitle string) string {
	// Build commit message: subject line + body with CR title
	subject := fmt.Sprintf("workitem: %s", item.ID)
	body := crTitle
	message := fmt.Sprintf("%s\n\n%s", subject, body)

	// Stage all changes first to check if there's anything to commit
	if err := git.Add(projectDir, "-A"); err != nil {
		out.Progressf("  warning: git add failed: %v\n", err)
		return ""
	}

	// Check if there are changes to commit
	if !git.HasStagedChanges(projectDir) {
		return ""
	}

	// Commit
	if err := git.Commit(projectDir, message); err != nil {
		out.Progressf("  warning: git commit failed: %v\n", err)
		return ""
	}

	out.Progressf("  Created commit for %s\n", item.ID)

	sha, err := git.HeadSHA(projectDir)
	if err != nil {
		return ""
	}
	return sha
}

// runAfterPhaseValidators executes validators with the "after-phase" trigger.
// These validators run once after all work items in a phase have completed.
// If validators fail, aggregated feedback is injected into a prompt, Claude is
// invoked to fix the issues, and validators are re-run. This loop continues
// until all validators pass or max iterations is reached.
func runAfterPhaseValidators(
	ctx context.Context,
	out *ui.Printer,
	cli *internal.CLI,
	config *domain.Config,
	projectDir string,
	auth domain.AuthMode,
	dispatcher *Dispatcher,
	basePayload EventPayload,
	validatorRunner *validators.Runner,
	validatorList []*domain.Validator,
	tracer trace.Tracer,
) error {
	maxIterations := config.Verification.MaxIterations
	iteration := 0

	// Route after-phase validators once, over the cumulative phase diff, before
	// the fix-retry loop so a retry reuses the same selection rather than
	// re-routing. The selection rides on basePayload into every phase-verified
	// dispatch below. A router failure runs all applicable; a diff failure leaves
	// the payload unrouted so the gate falls back to running all applicable.
	if len(validatorList) > 0 {
		if diff, diffErr := validatorRunner.GetGitDiffSince(ctx, basePayload.PhaseStartSHA); diffErr == nil {
			selected, routeErr := validatorRunner.SelectApplicable(ctx, validatorList, diff)
			if routeErr != nil {
				out.Progressf("  After-phase: validator router failed, running all applicable: %v\n", routeErr)
			}
			basePayload.SelectedValidatorIDs = selected
			basePayload.ValidatorsRouted = true
		} else {
			out.Progressf("  After-phase: could not compute diff for validator routing: %v\n", diffErr)
		}
	}

	for {
		// Check context cancellation (Ctrl+C)
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		iteration++

		// Check max iterations
		if maxIterations > 0 && iteration > maxIterations {
			return fmt.Errorf("max iterations (%d) reached for after-phase validators", maxIterations)
		}

		// Run after-phase validators (and any gating connectors) by emitting
		// phase-verified. After-phase validators launch and join on this event,
		// so the dispatch runs them in-process and returns a *GateError
		// carrying their aggregated feedback if any fail. No after-phase
		// validators configured means the phase is trivially verified; gating
		// connectors on phase-verified still run.
		_, joinSpan := tracer.Start(ctx, "validators")
		joinStart := time.Now()
		gateErr := dispatcher.Dispatch(Event{Name: EventPhaseVerified, Payload: basePayload})
		joinElapsed := time.Since(joinStart)
		joinSpan.End()
		if gateErr == nil {
			p := basePayload
			// Create commit for a successful fix if this wasn't the first iteration
			if iteration > 1 {
				p.CommitSHA = gitCommitAfterPhase(out, projectDir)
			}
			dispatcher.Dispatch(Event{Name: EventPhaseCompleted, Payload: p})
			return nil
		}

		// Validators or a gate failed - inject feedback and retry with Claude
		// After-phase validators launch and join on the same event, so the join
		// wait is their whole run rather than a residual overlap.
		// The feedback block was already printed by the engine's resolution ledger
		// when the gate joined, above - printing it again here would show one
		// validator failure twice.
		feedback := gateFeedback(gateErr)
		out.Progressf("  After-phase iteration %d: validators failed, will retry with feedback (validators %s)\n", iteration, ui.Duration(joinElapsed))

		// Build prompt for Claude to fix standards issues
		prompt := buildAfterPhasePrompt(feedback)

		out.Progressf("  After-phase iteration %d: invoking Claude to fix standards issues...\n", iteration)

		// Invoke Claude, timed from the outside as in the work-item loop, and asking for
		// the same structured output: an after-phase fix is an execution attempt and its
		// spend is part of what the phase cost. It has no work item to book against, so
		// the usage rides on the result until the run record carries it.
		spanCtx, claudeSpan := tracer.Start(ctx, "claude")
		claudeStart := time.Now()
		claudeResult, err := cli.Clone().WithUsageCapture(true).Prompt(spanCtx, prompt)
		claudeElapsed := time.Since(claudeStart)
		claudeSpan.End()

		// Detect and handle Claude usage limits without counting the attempt
		// against max iterations. A limit error means ctx was cancelled while
		// waiting/probing (Ctrl+C or timeout) - take the graceful shutdown path.
		if outcome, limitErr := handleClaudeLimits(ctx, out, claudeResult, auth, projectDir); limitErr != nil {
			return limitErr
		} else if outcome == limitWaited {
			out.Progressf("  After-phase iteration %d: usage limit handled, re-running this iteration (claude %s)\n", iteration, ui.Duration(claudeElapsed))
			iteration--
			continue
		}

		if err != nil {
			out.Progressf("  After-phase iteration %d: Claude invocation failed: %v (claude %s)\n", iteration, err, ui.Duration(claudeElapsed))
			// Continue to next iteration - Claude may have hit an error
			continue
		}

		// Check for completion token
		if !strings.Contains(claudeResult.Stdout, CompletionToken) {
			out.Progressf("  After-phase iteration %d: no %s token found, retrying... (claude %s)\n", iteration, CompletionToken, ui.Duration(claudeElapsed))
			continue
		}

		out.Progressf("  After-phase iteration %d: %s token found, re-running validators... (claude %s)\n", iteration, CompletionToken, ui.Duration(claudeElapsed))
		// Loop continues to re-run validators
	}
}

// buildAfterPhasePrompt constructs a prompt for Claude to fix after-phase validator failures.
func buildAfterPhasePrompt(validatorFeedback string) string {
	return "## TASK\n\n" +
		"Fix the project standards violations identified by the after-phase validators.\n\n" +
		"## PROJECT STANDARDS FEEDBACK\n\n" +
		"The following validators detected standards issues:\n\n" +
		validatorFeedback + "\n" +
		"Please fix these standards violations. When complete, output: <COMPLETE>"
}

// gitCommitAfterPhase creates a git commit after fixing after-phase validator issues.
// Returns the SHA of the created commit, or "" if no commit was created.
func gitCommitAfterPhase(out *ui.Printer, projectDir string) string {
	message := "fix: after-phase validator standards issues"

	// Stage all changes first
	if err := git.Add(projectDir, "-A"); err != nil {
		out.Progressf("  warning: git add failed: %v\n", err)
		return ""
	}

	// Check if there are changes to commit
	if !git.HasStagedChanges(projectDir) {
		return ""
	}

	// Commit
	if err := git.Commit(projectDir, message); err != nil {
		out.Progressf("  warning: git commit failed: %v\n", err)
		return ""
	}

	out.Progressf("  Created commit for after-phase validator fixes\n")

	sha, err := git.HeadSHA(projectDir)
	if err != nil {
		return ""
	}
	return sha
}
