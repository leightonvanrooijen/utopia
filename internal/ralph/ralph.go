package ralph

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/leightonvanrooijen/utopia/internal"
	"github.com/leightonvanrooijen/utopia/internal/cli/ui"
	"github.com/leightonvanrooijen/utopia/internal/domain"
	"github.com/leightonvanrooijen/utopia/internal/git"
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

// Execute runs all work items for a spec sequentially.
// Work items are processed one at a time, in order, retrying until
// verification passes or max iterations is reached.
// The optional model parameter specifies a Claude model override: an alias the
// CLI resolves (e.g. "opus") or a full model identifier.
//
// auth selects the credential every claude subprocess this loop spawns
// authenticates with - the work-item agents, the validators they gate on, and
// the spend-limit probe alike. One loop can run for hours across hundreds of
// subprocesses, so a mode that reached only some of them would split a single
// run's usage across two accounts. The empty mode inherits the ambient
// environment, which is the pre-auth behaviour.
func Execute(ctx context.Context, specID string, store *internal.YAMLStore, config *domain.Config, projectDir string, auth domain.AuthMode, model ...string) (*Result, error) {
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

	result := &Result{
		Total: len(items),
	}

	// Create dependencies. The resolved override is also the default executor
	// model the escalation routing retries on - empty means no --model flag, so
	// the claude CLI default applies, which is the pre-escalation behaviour.
	cli := internal.NewCLI().WithVerbose(true).WithAuth(auth, filepath.Join(projectDir, ".utopia"))
	defaultExecutorModel := ""
	if len(model) > 0 && model[0] != "" {
		defaultExecutorModel = model[0]
		cli = cli.WithModel(defaultExecutorModel)
	}
	verifier := verification.NewRunner(projectDir)
	validatorRunner := validators.NewRunner(projectDir).WithModelConfig(config.Models).WithAuth(auth)

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
		runner := &ConnectorRunner{engine: NewEngine(subs)}
		dispatcher.Subscribe(func(e Event) error {
			return runner.Handle(ctx, e)
		})
	}
	basePayload := EventPayload{
		CRID:   extractCRID(specID),
		SpecID: specID,
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
		// Skip completed items
		if item.Status == domain.WorkItemCompleted {
			result.Completed++
			fmt.Printf("[%d/%d] %s - already completed\n", i+1, len(items), item.ID)
			continue
		}

		// An item halted as needs_human on an earlier run is not retried. Its caps
		// are persisted, so re-entering the loop would exhaust them again and halt at
		// the same place; the change request has to change first.
		if item.Status == domain.WorkItemNeedsHuman {
			result.NeedsHuman = append(result.NeedsHuman, item.ID)
			fmt.Printf("[%d/%d] %s - halted, needs human attention (skipped)\n", i+1, len(items), item.ID)
			continue
		}

		fmt.Printf("[%d/%d] %s - starting execution\n", i+1, len(items), item.ID)

		// Execute this work item with the Ralph loop
		timings, err := executeWorkItem(ctx, item, specID, store, cli, defaultExecutorModel, verifier, config, projectDir, auth, dispatcher, basePayload, validatorRunner, validatorList)
		if err != nil {
			// A halted item is skipped, not fatal. Batch execution runs every draft
			// change request in order, so aborting the run on one ambiguous change
			// request would strand every item behind it - and an item needing a human
			// is precisely the case where the rest of the batch is still worth running.
			var needsHuman *NeedsHumanError
			if errors.As(err, &needsHuman) {
				result.NeedsHuman = append(result.NeedsHuman, item.ID)
				fmt.Printf("[%d/%d] %s - halted after %d iteration(s), needs human attention: %s\n",
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
		fmt.Printf("[%d/%d] %s - completed in %d iteration(s)\n", i+1, len(items), item.ID, item.IterationCount)
		// Where the item's wall clock went, under the line that announced it
		// finished. Only a completed item carries timings.
		if timings != nil {
			fmt.Printf("  timing: %s\n", timings.summary())
		}
	}

	// Run after-phase validators once all work items are complete. This also
	// fires phase-verified and phase-completed (and their gating connectors)
	// even when no validators are configured.
	if result.Completed == result.Total {
		if err := runAfterPhaseValidators(ctx, cli, config, projectDir, auth, dispatcher, basePayload, validatorRunner, validatorList); err != nil {
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
// A completed item returns the wall-clock time it spent in each step it
// bracketed, so the caller can report the breakdown alongside the item's
// completion line; an item that stops early returns nil timings.
func executeWorkItem(
	ctx context.Context,
	item *domain.WorkItem,
	specID string,
	store *internal.YAMLStore,
	cli *internal.CLI,
	defaultExecutorModel string,
	verifier *verification.Runner,
	config *domain.Config,
	projectDir string,
	auth domain.AuthMode,
	dispatcher *Dispatcher,
	basePayload EventPayload,
	validatorRunner *validators.Runner,
	validatorList []*domain.Validator,
) (*stepTimings, error) {
	maxIterations := config.Verification.MaxIterations
	verifyCommand := config.Verification.Command

	// The escalation caps are the inner bounds on the retry paths; maxIterations
	// above is the outer bound on the item as a whole. Both appear on the routing
	// log line so an operator can tell which one stopped the item.
	caps := EscalationCapsFrom(config.Escalation)
	escalatedExecutorModel := resolveEscalatedExecutorModel(config.Models)

	// The scoper is the role that rewrites the change request when the executor
	// keeps misreading it. It is built once per work item rather than per
	// escalation because its dependencies do not change across one item's run.
	sc := &scoper{
		store:     store,
		cli:       cli,
		model:     resolveScoperModel(config.Models),
		standards: store.LoadStandardsIndex(),
	}

	// Load CR title for commit message and operation type for execution log
	crID := extractCRID(specID)
	crTitle := ""
	operationType := "refactor" // default for refactor CRs
	if cr, err := store.LoadChangeRequest(crID); err == nil {
		crTitle = cr.Title
		operationType = deriveOperationType(cr, item.SpecRef)
	}

	// Time every expensive step this loop brackets so the item can report where
	// its wall clock went when it completes.
	timings := newStepTimings()

	// The run transcript, accumulated from the streamed Claude output each
	// iteration already produces for completion-token detection. It is written
	// to .utopia/runs/ when the item finishes, however it finishes.
	var transcript strings.Builder

	itemPayload := basePayload
	itemPayload.WorkItemID = item.ID
	itemPayload.IterationCount = item.IterationCount
	// A gate blocking workitem-started aborts the run before the item runs.
	if gateErr := dispatcher.Dispatch(Event{Name: EventWorkItemStarted, Payload: itemPayload}); gateErr != nil {
		return nil, gateErr
	}

	for {
		// Check context cancellation (Ctrl+C)
		select {
		case <-ctx.Done():
			// Save current state before exiting
			_ = store.SaveWorkItemForSpec(specID, item)
			return nil, ctx.Err()
		default:
		}

		// An item already at the comprehension cap routes to scoping escalation
		// before another execution attempt is spent on it. This is the resume path:
		// a decision made during this run is escalated at the gate below, so
		// reaching here means the counter was carried in on the persisted work item.
		if item.ComprehensionCount >= caps.ComprehensionEscalations {
			// Unless the scoping cap is already spent, in which case there is no path
			// left: the change request was already rewritten as many times as it is
			// allowed to be and still does not execute. Halting here is what stops the
			// rewrite-then-retry cycle from becoming the unbounded loop.
			if item.ScopingEscalationCount >= caps.ScopingEscalations {
				return nil, haltNeedsHuman(store, specID, crID, item, &transcript, &NeedsHumanError{
					WorkItemID: item.ID,
					Cap:        "escalation.scoping_escalations",
					Limit:      caps.ScopingEscalations,
					Detail: fmt.Sprintf("%d scoping escalation(s) did not produce a change request the executor could satisfy",
						item.ScopingEscalationCount),
				})
			}
			// Charged before the rewrite runs, for the same reason an escalated
			// execution attempt is: the cap bounds attempts, and an attempt that
			// produces nothing has still been made.
			item.ScopingEscalationCount++
			esc := &ScopingEscalationError{WorkItemID: item.ID, ComprehensionCount: item.ComprehensionCount}
			if err := escalateScoping(ctx, sc, item, specID, crID, esc, caps, &transcript); err != nil {
				return nil, err
			}
			continue
		}

		// Increment iteration count
		item.IterationCount++
		item.Status = domain.WorkItemInProgress

		// Check max iterations
		if maxIterations > 0 && item.IterationCount > maxIterations {
			item.Status = domain.WorkItemFailed
			_ = store.SaveWorkItemForSpec(specID, item)
			// A run that gave up is still worth harvesting - what was tried and
			// why it never converged is a decision record too.
			writeRunTranscript(store, crID, item, transcript.String(), domain.RunFailed)
			return nil, fmt.Errorf("max iterations (%d) reached for work item %s", maxIterations, item.ID)
		}

		// Save current state
		if err := store.SaveWorkItemForSpec(specID, item); err != nil {
			return nil, fmt.Errorf("failed to save work item state: %w", err)
		}

		// Build the prompt (includes failure injection if applicable)
		prompt := buildPrompt(item)

		// Which executor this attempt runs on is derived from the item's persisted
		// escalation state rather than from an in-memory flag, so a resumed item
		// that already escalated does not reset to the default executor. The CLI is
		// cloned rather than mutated because it is shared with every other work
		// item in this run, and escalation is per item.
		attemptModel := executorModelFor(item, defaultExecutorModel, escalatedExecutorModel)

		// An escalated attempt is booked against its cap before it runs, so the
		// halt costs nothing when the cap is already spent. It is refunded below
		// if the attempt never actually happened.
		charged, capErr := chargeEscalatedAttempt(item, caps)
		if capErr != nil {
			item.IterationCount--
			return nil, haltNeedsHuman(store, specID, crID, item, &transcript, capErr)
		}

		attemptCLI := cli
		if attemptModel != defaultExecutorModel {
			attemptCLI = cli.Clone().WithModel(attemptModel)
			fmt.Printf("  Iteration %d: invoking Claude on the escalated executor (%s)...\n", item.IterationCount, attemptModel)
		} else {
			fmt.Printf("  Iteration %d: invoking Claude...\n", item.IterationCount)
		}

		// Invoke Claude. The call is timed from the outside, so the duration
		// covers whatever the agent did without the agent reporting anything;
		// it is charged to the claude category on every path out, including the
		// usage-limit retry below, because the wall clock was spent either way.
		claudeStart := time.Now()
		claudeResult, err := attemptCLI.Prompt(ctx, prompt)
		claudeElapsed := time.Since(claudeStart)
		timings.claude += claudeElapsed

		// Detect and handle Claude usage limits (rolling rate limit or org
		// monthly spend limit) before treating this as a failed iteration.
		// A handled limit means this attempt must not count against max
		// iterations, so we undo the increment and retry with a normally
		// rebuilt prompt. A limit error means ctx was cancelled while
		// waiting/probing (Ctrl+C or timeout) - take the graceful shutdown path.
		if outcome, limitErr := handleClaudeLimits(ctx, claudeResult, auth, projectDir); limitErr != nil {
			_ = store.SaveWorkItemForSpec(specID, item)
			return nil, limitErr
		} else if outcome == limitWaited {
			fmt.Printf("  Iteration %d: usage limit handled, re-running this iteration (claude %s)\n", item.IterationCount, ui.Duration(claudeElapsed))
			item.IterationCount--
			// The attempt produced no work, so it is refunded: the cap bounds spend on
			// the escalated executor, and nothing was spent here.
			if charged {
				item.OpusExecutionAttempts--
			}
			continue
		}

		// Keep this iteration's output on the run transcript before any of the
		// paths below take the loop elsewhere. A limit-handled attempt is
		// deliberately excluded above: it produced no work and does not count
		// as an iteration, so it would only misnumber the transcript.
		appendIterationOutput(&transcript, item.IterationCount, claudeResult)

		if err != nil {
			fmt.Printf("  Iteration %d: Claude invocation failed: %v (claude %s)\n", item.IterationCount, err, ui.Duration(claudeElapsed))
			// Continue to next iteration - Claude may have hit an error
			continue
		}

		// Check for completion token
		if !strings.Contains(claudeResult.Stdout, CompletionToken) {
			fmt.Printf("  Iteration %d: no %s token found, retrying... (claude %s)\n", item.IterationCount, CompletionToken, ui.Duration(claudeElapsed))
			// No completion token - Claude hit step limit or got stuck
			// Clear any previous failure since this is a different failure mode
			item.LastFailureOutput = ""
			item.LastValidatorFeedback = ""
			continue
		}

		fmt.Printf("  Iteration %d: %s token found, running verification... (claude %s)\n", item.IterationCount, CompletionToken, ui.Duration(claudeElapsed))

		// Route validators once for this work item, the first time it reaches the
		// gate. The cheap relevance router picks which validators apply to the
		// change; the selection is persisted on the item so retries and resumes
		// reuse the one decision rather than re-routing every iteration. A router
		// failure returns "all applicable" (still recorded so we don't retry the
		// failing call); a diff failure leaves the item unrouted so the gate falls
		// back to running all applicable validators next dispatch.
		if !item.ValidatorsRouted && len(validatorList) > 0 {
			if diff, diffErr := validatorRunner.GetGitDiff(ctx); diffErr == nil {
				selected, routeErr := validatorRunner.SelectApplicable(ctx, validatorList, diff)
				if routeErr != nil {
					fmt.Printf("  Iteration %d: validator router failed, running all applicable: %v\n", item.IterationCount, routeErr)
				}
				item.SelectedValidators = selected
				item.ValidatorsRouted = true
				_ = store.SaveWorkItemForSpec(specID, item)
			} else {
				fmt.Printf("  Iteration %d: could not compute diff for validator routing: %v\n", item.IterationCount, diffErr)
			}
		}

		// Completion claimed but not yet verified. This launches speculative
		// work: after-workitem validators start now (via the engine) and run
		// concurrently with verification, to be joined at workitem-verified or
		// abandoned at workitem-verification-failed. Notify connectors also
		// start here. Fire-and-forget at this point: notify only. The router's
		// selection rides on the payload so the validators action runs only the
		// chosen subset.
		claimedPayload := itemPayload
		claimedPayload.IterationCount = item.IterationCount
		claimedPayload.SelectedValidatorIDs = item.SelectedValidators
		claimedPayload.ValidatorsRouted = item.ValidatorsRouted
		dispatcher.Dispatch(Event{Name: EventWorkItemCompletionClaimed, Payload: claimedPayload})

		// Run verification. Validators are already running speculatively; the
		// engine joins them at workitem-verified below. An empty verification
		// command is treated as trivially verified.
		verifyPassed := true
		verifyOutput := ""
		var verifyElapsed time.Duration
		if verifyCommand != "" {
			verifyStart := time.Now()
			verifyResult, err := verifier.Run(ctx, verifyCommand)
			verifyElapsed = time.Since(verifyStart)
			timings.verification += verifyElapsed
			if err != nil {
				return nil, fmt.Errorf("verification command failed to execute: %w", err)
			}
			verifyPassed = verifyResult.Passed
			verifyOutput = verifyResult.Output
		} else {
			fmt.Printf("  Iteration %d: no verification command configured, marking complete\n", item.IterationCount)
		}

		if !verifyPassed {
			// Verification failed - inject failure and retry. Signal that the
			// completion claim did not hold so the engine cancels the
			// speculative validators; their feedback is discarded.
			fmt.Printf("  Iteration %d: verification failed, will retry with failure output (verification %s)\n", item.IterationCount, ui.Duration(verifyElapsed))
			failedPayload := itemPayload
			failedPayload.IterationCount = item.IterationCount
			dispatcher.Dispatch(Event{Name: EventWorkItemVerificationFailed, Payload: failedPayload})
			item.LastFailureOutput = verifyOutput
			item.LastValidatorFeedback = "" // Clear any previous validator feedback
			continue
		}

		if verifyCommand != "" {
			fmt.Printf("  Iteration %d: verification passed! (verification %s)\n", item.IterationCount, ui.Duration(verifyElapsed))
		}

		// Verification passed - join the speculative validators (and any gating
		// connectors) at workitem-verified. A blocking gate - a failing
		// validator or connector - injects its stdout as feedback into the next
		// iteration and prevents the commit.
		//
		// The dispatch is the join point, so timing it measures what the loop
		// still had to wait for the validators to finish - their run already
		// overlapped verification. Charging the wait rather than the whole
		// validator run keeps the categories a breakdown of real wall clock;
		// the engine's resolution ledger reports each run's full duration.
		p := itemPayload
		p.IterationCount = item.IterationCount
		joinStart := time.Now()
		gateErr := dispatcher.Dispatch(Event{Name: EventWorkItemVerified, Payload: p})
		joinElapsed := time.Since(joinStart)
		timings.validators += joinElapsed
		if gateErr != nil {
			// Route on the failure class the validators reported rather than on the
			// iteration count. A mechanical failure retries on the same executor; a
			// comprehension failure escalates it; repeated comprehension failure, or
			// a suspected spec defect, escalates the change request instead.
			aggregate := aggregateFromGate(gateErr)
			decision := routeValidationFailure(item, aggregate, caps, defaultExecutorModel, escalatedExecutorModel)
			fmt.Printf("  Iteration %d: gate blocked workitem-verified (validators %s)\n", item.IterationCount, ui.Duration(joinElapsed))
			fmt.Printf("  %s\n", decision.logLine(item.IterationCount, maxIterations, caps))
			item.LastFailureOutput = ""
			item.LastValidatorFeedback = gateFeedback(gateErr)
			if err := store.SaveWorkItemForSpec(specID, item); err != nil {
				return nil, fmt.Errorf("failed to save work item state: %w", err)
			}
			if decision.Route == RouteNeedsHuman {
				return nil, haltNeedsHuman(store, specID, crID, item, &transcript, &NeedsHumanError{
					WorkItemID: item.ID,
					Cap:        decision.CapExhausted,
					Limit:      caps.ScopingEscalations,
					Detail: fmt.Sprintf("%d scoping escalation(s) did not produce a change request the executor could satisfy",
						caps.ScopingEscalations),
					Cause: scopingEscalationError(item, aggregate, decision),
				})
			}
			if decision.Route == RouteScopingEscalation {
				// The change request, not the code, is what gets rewritten here. A
				// rewrite the loop can resume against returns nil and execution carries
				// on against the new specification; anything else stops the item.
				if err := escalateScoping(ctx, sc, item, specID, crID, scopingEscalationError(item, aggregate, decision), caps, &transcript); err != nil {
					return nil, err
				}
			}
			continue
		}
		item.Status = domain.WorkItemCompleted
		item.LastFailureOutput = ""
		item.LastValidatorFeedback = ""
		if err := store.SaveWorkItemForSpec(specID, item); err != nil {
			return nil, err
		}
		logExecutionEntry(store, crID, item, operationType)
		// Written before the commit so the transcript is picked up by the same
		// `git add -A` and lands in the work item's own commit.
		writeRunTranscript(store, crID, item, transcript.String(), domain.RunCompleted)
		p.CommitSHA = gitCommitWorkItem(projectDir, item, crTitle)
		dispatcher.Dispatch(Event{Name: EventWorkItemCommitted, Payload: p})
		return timings, nil
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
func logExecutionEntry(store *internal.YAMLStore, crID string, item *domain.WorkItem, operation string) {
	entry := domain.ExecutionLogEntry{
		WorkItemID:  item.ID,
		SpecRef:     item.SpecRef,
		Operation:   operation,
		CompletedAt: time.Now(),
	}
	if err := store.AppendExecutionLogEntry(crID, entry); err != nil {
		fmt.Printf("  warning: failed to log execution entry: %v\n", err)
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

// writeRunTranscript persists the work item's execution run to
// .utopia/runs/<cr-id>/<workitem-id>.yaml.
// Logs a warning and returns on failure (non-blocking): a lost transcript
// costs future harvests some signal, which is never worth stopping a run over.
func writeRunTranscript(store *internal.YAMLStore, crID string, item *domain.WorkItem, transcript string, outcome domain.RunOutcome) {
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
		Transcript: transcript,
	}
	if err := store.SaveExecutionRun(run); err != nil {
		fmt.Printf("  warning: failed to write run transcript for %s: %v\n", item.ID, err)
	}
}

// gitCommitWorkItem creates a git commit after a work item passes verification.
// Logs warning and returns on failure (non-blocking).
// Returns the SHA of the created commit, or "" if no commit was created.
func gitCommitWorkItem(projectDir string, item *domain.WorkItem, crTitle string) string {
	// Build commit message: subject line + body with CR title
	subject := fmt.Sprintf("workitem: %s", item.ID)
	body := crTitle
	message := fmt.Sprintf("%s\n\n%s", subject, body)

	// Stage all changes first to check if there's anything to commit
	if err := git.Add(projectDir, "-A"); err != nil {
		fmt.Printf("  warning: git add failed: %v\n", err)
		return ""
	}

	// Check if there are changes to commit
	if !git.HasStagedChanges(projectDir) {
		return ""
	}

	// Commit
	if err := git.Commit(projectDir, message); err != nil {
		fmt.Printf("  warning: git commit failed: %v\n", err)
		return ""
	}

	fmt.Printf("  Created commit for %s\n", item.ID)

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
	cli *internal.CLI,
	config *domain.Config,
	projectDir string,
	auth domain.AuthMode,
	dispatcher *Dispatcher,
	basePayload EventPayload,
	validatorRunner *validators.Runner,
	validatorList []*domain.Validator,
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
				fmt.Printf("  After-phase: validator router failed, running all applicable: %v\n", routeErr)
			}
			basePayload.SelectedValidatorIDs = selected
			basePayload.ValidatorsRouted = true
		} else {
			fmt.Printf("  After-phase: could not compute diff for validator routing: %v\n", diffErr)
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
		joinStart := time.Now()
		gateErr := dispatcher.Dispatch(Event{Name: EventPhaseVerified, Payload: basePayload})
		joinElapsed := time.Since(joinStart)
		if gateErr == nil {
			p := basePayload
			// Create commit for a successful fix if this wasn't the first iteration
			if iteration > 1 {
				p.CommitSHA = gitCommitAfterPhase(projectDir)
			}
			dispatcher.Dispatch(Event{Name: EventPhaseCompleted, Payload: p})
			return nil
		}

		// Validators or a gate failed - inject feedback and retry with Claude
		// After-phase validators launch and join on the same event, so the join
		// wait is their whole run rather than a residual overlap.
		feedback := gateFeedback(gateErr)
		fmt.Printf("  After-phase iteration %d: validators failed, will retry with feedback (validators %s)\n", iteration, ui.Duration(joinElapsed))
		fmt.Printf("\n--- Validator Failure Feedback ---\n%s\n--- End Validator Feedback ---\n\n", feedback)

		// Build prompt for Claude to fix standards issues
		prompt := buildAfterPhasePrompt(feedback)

		fmt.Printf("  After-phase iteration %d: invoking Claude to fix standards issues...\n", iteration)

		// Invoke Claude, timed from the outside as in the work-item loop.
		claudeStart := time.Now()
		claudeResult, err := cli.Prompt(ctx, prompt)
		claudeElapsed := time.Since(claudeStart)

		// Detect and handle Claude usage limits without counting the attempt
		// against max iterations. A limit error means ctx was cancelled while
		// waiting/probing (Ctrl+C or timeout) - take the graceful shutdown path.
		if outcome, limitErr := handleClaudeLimits(ctx, claudeResult, auth, projectDir); limitErr != nil {
			return limitErr
		} else if outcome == limitWaited {
			fmt.Printf("  After-phase iteration %d: usage limit handled, re-running this iteration (claude %s)\n", iteration, ui.Duration(claudeElapsed))
			iteration--
			continue
		}

		if err != nil {
			fmt.Printf("  After-phase iteration %d: Claude invocation failed: %v (claude %s)\n", iteration, err, ui.Duration(claudeElapsed))
			// Continue to next iteration - Claude may have hit an error
			continue
		}

		// Check for completion token
		if !strings.Contains(claudeResult.Stdout, CompletionToken) {
			fmt.Printf("  After-phase iteration %d: no %s token found, retrying... (claude %s)\n", iteration, CompletionToken, ui.Duration(claudeElapsed))
			continue
		}

		fmt.Printf("  After-phase iteration %d: %s token found, re-running validators... (claude %s)\n", iteration, CompletionToken, ui.Duration(claudeElapsed))
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
func gitCommitAfterPhase(projectDir string) string {
	message := "fix: after-phase validator standards issues"

	// Stage all changes first
	if err := git.Add(projectDir, "-A"); err != nil {
		fmt.Printf("  warning: git add failed: %v\n", err)
		return ""
	}

	// Check if there are changes to commit
	if !git.HasStagedChanges(projectDir) {
		return ""
	}

	// Commit
	if err := git.Commit(projectDir, message); err != nil {
		fmt.Printf("  warning: git commit failed: %v\n", err)
		return ""
	}

	fmt.Println("  Created commit for after-phase validator fixes")

	sha, err := git.HeadSHA(projectDir)
	if err != nil {
		return ""
	}
	return sha
}
