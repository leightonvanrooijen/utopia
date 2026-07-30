package ralph

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/leightonvanrooijen/utopia/internal"
	"github.com/leightonvanrooijen/utopia/internal/domain"
	"github.com/leightonvanrooijen/utopia/internal/git"
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
func Execute(ctx context.Context, specID string, store *internal.YAMLStore, config *domain.Config, projectDir string, auth domain.AuthMode, over Overrides) (*Result, error) {
	// Every diagnostic this run emits carries the change request it belongs to, so
	// no call site below has to interpolate the ID into its message. Attached here,
	// before the printer reaches the claude subprocesses, the engine or the
	// work-item loop, so a run has no path that emits a diagnostic without it.
	out := ui.OrDefault(over.Out).WithAttrs(slog.String("cr_id", extractCRID(specID)))

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
	//
	// Effort is resolved once here, for the whole run: every role's level is fixed
	// before the first work item starts and nothing below recomputes it, which is
	// what makes "no code path raises effort on failure" a property of the
	// structure rather than a convention.
	cli := internal.NewCLI().WithVerbose(true).WithAuth(auth, filepath.Join(projectDir, ".utopia")).WithPrinter(out)
	efforts := resolveRoleEfforts(config.Effort, over.Effort)
	cli = cli.WithEffort(efforts.executor)
	defaultExecutorModel := ""
	if over.Model != "" {
		defaultExecutorModel = over.Model
		cli = cli.WithModel(defaultExecutorModel)
	}
	verifier := verification.NewRunner(projectDir)
	validatorRunner := validators.NewRunner(projectDir).WithModelConfig(config.Models).WithEffort(efforts.validators).WithAuth(auth)

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
		runner := &ConnectorRunner{engine: engine}
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
			out.Printf("[%d/%d] %s - already completed\n", i+1, len(items), item.ID)
			continue
		}

		// An item halted as needs_human on an earlier run is not retried. Its caps
		// are persisted, so re-entering the loop would exhaust them again and halt at
		// the same place; the change request has to change first.
		if item.Status == domain.WorkItemNeedsHuman {
			result.NeedsHuman = append(result.NeedsHuman, item.ID)
			out.Printf("[%d/%d] %s - halted, needs human attention (skipped)\n", i+1, len(items), item.ID)
			continue
		}

		out.Printf("[%d/%d] %s - starting execution\n", i+1, len(items), item.ID)

		// Execute this work item with the Ralph loop
		timings, err := executeWorkItem(ctx, itemOut, item, specID, store, cli, defaultExecutorModel, efforts, verifier, config, projectDir, auth, dispatcher, basePayload, validatorRunner, validatorList)
		if err != nil {
			// A halted item is skipped, not fatal. Batch execution runs every draft
			// change request in order, so aborting the run on one ambiguous change
			// request would strand every item behind it - and an item needing a human
			// is precisely the case where the rest of the batch is still worth running.
			var needsHuman *NeedsHumanError
			if errors.As(err, &needsHuman) {
				result.NeedsHuman = append(result.NeedsHuman, item.ID)
				out.Printf("[%d/%d] %s - halted after %d iteration(s), needs human attention: %s\n",
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
		out.Printf("[%d/%d] %s - completed in %d iteration(s)\n", i+1, len(items), item.ID, item.IterationCount)
		// Where the item's wall clock went, under the line that announced it
		// finished. Only a completed item carries timings.
		if timings != nil {
			out.Printf("  timing: %s\n", timings.summary())
		}
	}

	// Run after-phase validators once all work items are complete. This also
	// fires phase-verified and phase-completed (and their gating connectors)
	// even when no validators are configured.
	if result.Completed == result.Total {
		if err := runAfterPhaseValidators(ctx, out, cli, config, projectDir, auth, dispatcher, basePayload, validatorRunner, validatorList); err != nil {
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
) (timings *stepTimings, err error) {
	maxIterations := config.Verification.MaxIterations
	verifyCommand := config.Verification.Command

	// The per-iteration turn ceiling. maxIterations above bounds how many
	// iterations the item gets; this bounds what any one of them may spend, so the
	// item's ceiling is turnBudget x maxIterations rather than unbounded.
	turnBudget := config.WorkItems.TurnBudgetOr()

	// The escalation caps are the inner bounds on the retry paths; maxIterations
	// above is the outer bound on the item as a whole. Both appear on the routing
	// log line so an operator can tell which one stopped the item.
	caps := EscalationCapsFrom(config.Escalation)
	escalatedExecutorModel := resolveEscalatedExecutorModel(config.Models)

	// The scoper is the role that rewrites the change request when the executor
	// keeps misreading it. It is built once per work item rather than per
	// escalation because its dependencies do not change across one item's run.
	sc := &scoper{
		out:       out,
		store:     store,
		cli:       cli,
		model:     resolveScoperModel(config.Models),
		effort:    efforts.scoper,
		standards: store.LoadStandardsIndex(),
	}

	// Load CR title for commit message and operation type for execution log
	crID := extractCRID(specID)
	crTitle := ""
	operationType := "refactor" // default for refactor CRs
	// The type the item's routing is recorded against, so escalation rate per
	// cr_type is a query over the run records. Left empty when the CR cannot be
	// loaded rather than defaulted to a type the item may not have.
	var crType domain.CRType
	if cr, err := store.LoadChangeRequest(crID); err == nil {
		crTitle = cr.Title
		operationType = deriveOperationType(cr, item.SpecRef)
		crType = deriveCRType(cr, specID)
	}

	// Time every expensive step this loop brackets so the item can report where
	// its wall clock went when it completes.
	timings = newStepTimings()

	// The run record: the transcript accumulated from the streamed Claude output
	// each iteration already produces for completion-token detection, plus the wall
	// clock and change request type its routing record needs. It is written to
	// .utopia/runs/ when the item finishes, however it finishes.
	rec := newRunRecorder(crType)
	rec.out = out

	// Every way out of this loop leaves a record, not only the outcomes the loop
	// routed to. An item that aborted on an error nobody anticipated is exactly the
	// item worth having a record of, and no record at all reads like a change
	// request that was never attempted.
	defer func() { recordAbort(ctx, store, crID, item, rec, err) }()

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
				return nil, haltNeedsHuman(store, specID, crID, item, rec, &NeedsHumanError{
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
			if err := escalateScoping(ctx, sc, item, specID, crID, esc, caps, rec, escalationDiff(ctx, out, validatorRunner)); err != nil {
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
			writeRunTranscript(store, crID, item, rec, domain.RunFailed)
			return nil, fmt.Errorf("max iterations (%d) reached for work item %s", maxIterations, item.ID)
		}

		// Save current state
		if err := store.SaveWorkItemForSpec(specID, item); err != nil {
			return nil, fmt.Errorf("failed to save work item state: %w", err)
		}

		// Build the prompt. A mechanical retry gets the failure-injection prompt
		// unchanged - the intent was right and the last diff is what it is fixing.
		// An escalated attempt gets a freshly constructed context instead: the
		// specification, the failures' conclusions and the evidence, without the
		// attempts that produced them. Handing the escalated model the transcript of
		// the attempts that just failed would anchor it to the reading that failed,
		// which is the one thing escalation exists to escape.
		//
		// Escalation is read from the item's persisted comprehension counter, the
		// same source the model resolution and the cap charge below read, so all
		// three agree on whether this attempt is escalated.
		prompt := buildPrompt(item)
		if item.ComprehensionCount > 0 {
			escalated, err := buildEscalatedPrompt(store, item, crID, escalationDiff(ctx, out, validatorRunner))
			if err != nil {
				return nil, err
			}
			prompt = escalated
		}

		// Which executor this attempt runs on is derived from the item's persisted
		// escalation state rather than from an in-memory flag, so a resumed item
		// that already escalated does not reset to the default executor. The CLI is
		// cloned rather than mutated because it is shared with every other work
		// item in this run, and escalation is per item.
		attemptModel := executorModelFor(item, defaultExecutorModel, escalatedExecutorModel)
		// The escalated executor's effort is its own role's level, not the default
		// executor's raised: a mechanical retry stays on the default executor and so
		// keeps that role's effort unchanged.
		attemptEffort := executorEffortFor(item, efforts)

		// An escalated attempt is booked against its cap before it runs, so the
		// halt costs nothing when the cap is already spent. It is refunded below
		// if the attempt never actually happened.
		charged, capErr := chargeEscalatedAttempt(item, caps)
		if capErr != nil {
			item.IterationCount--
			return nil, haltNeedsHuman(store, specID, crID, item, rec, capErr)
		}

		// Book the attempt onto the item's routing record alongside the cap charge
		// above, and for the same reason: both are refunded together if the attempt
		// never actually runs. Recording the model and effort here rather than
		// reconstructing them later is what makes the record evidence - in particular
		// the evidence that the default executor's effort was never raised.
		recordExecutorAttempt(item, attemptModel, attemptEffort)

		// Every execution attempt runs under the turn budget, escalated or not: the
		// budget bounds one iteration's spend, and an escalated attempt is still one
		// iteration. The clone is what keeps the ceiling off the loop's shared CLI,
		// which the scoper also borrows - a rewrite is not an execution iteration and
		// is not budgeted as one.
		// Usage capture is asked for on every execution attempt, escalated or not: the
		// comparison the records exist to support is between the tiers, so an attempt
		// missing its accounting is a hole in exactly the row that matters.
		attemptCLI := cli.Clone().WithMaxTurns(turnBudget).WithUsageCapture(true)
		if attemptModel != defaultExecutorModel || attemptEffort != efforts.executor {
			attemptCLI = attemptCLI.WithModel(attemptModel).WithEffort(attemptEffort)
			out.Printf("  Iteration %d: invoking Claude on the escalated executor (%s, effort %s)...\n", item.IterationCount, attemptModel, attemptEffort)
		} else {
			out.Printf("  Iteration %d: invoking Claude...\n", item.IterationCount)
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
		if outcome, limitErr := handleClaudeLimits(ctx, out, claudeResult, auth, projectDir); limitErr != nil {
			_ = store.SaveWorkItemForSpec(specID, item)
			return nil, limitErr
		} else if outcome == limitWaited {
			out.Printf("  Iteration %d: usage limit handled, re-running this iteration (claude %s)\n", item.IterationCount, ui.Duration(claudeElapsed))
			item.IterationCount--
			// The attempt produced no work, so it is refunded: the cap bounds spend on
			// the escalated executor, and nothing was spent here. The routing record is
			// refunded with it, so the attempt list stays a list of attempts that ran.
			if charged {
				item.OpusExecutionAttempts--
			}
			refundExecutorAttempt(item)
			continue
		}

		// Book what the attempt spent onto its routing record. This runs before the
		// paths below leave the iteration - a turn-capped attempt, a failed invocation
		// and a successful one all spent tokens, and all three are recorded.
		recordAttemptUsage(item, claudeResult, claudeElapsed)

		// Keep this iteration's output on the run transcript before any of the
		// paths below take the loop elsewhere. A limit-handled attempt is
		// deliberately excluded above: it produced no work and does not count
		// as an iteration, so it would only misnumber the transcript.
		appendIterationOutput(&rec.transcript, item.IterationCount, claudeResult)

		// The turn budget being spent is expected operation, so it is reported as
		// such - before the branch below, which would otherwise report the non-zero
		// exit as an invocation failure indistinguishable from a crash or an auth
		// error. Nothing else about the iteration changes: it counts against max
		// iterations, no verification runs, and LastFailureOutput is deliberately
		// left standing, because a capped iteration ran no verification and so has
		// produced nothing that supersedes the last verification result there is.
		//
		// Nothing about the cap reaches the next prompt either. A message about
		// running out of turns is a scarcity signal - it says hurry, and it invites
		// shortcuts. The partial work is uncommitted in the working tree and the next
		// iteration rebuilds its state from git and the files, so the capped
		// iteration is a ratchet rather than wasted spend.
		if claudeResult != nil && DetectTurnExhaustion(claudeResult.Stdout, claudeResult.Stderr) {
			out.Printf("  Iteration %d: turn budget of %d reached, continuing in a fresh iteration (claude %s)\n", item.IterationCount, turnBudget, ui.Duration(claudeElapsed))
			// A capped attempt claimed nothing, so nothing it produced was judged. Its
			// spend is recorded either way: the ratchet cost what it cost.
			recordAttemptOutcome(item, domain.AttemptIncomplete, "")
			continue
		}

		if err != nil {
			out.Printf("  Iteration %d: Claude invocation failed: %v (claude %s)\n", item.IterationCount, err, ui.Duration(claudeElapsed))
			recordAttemptOutcome(item, domain.AttemptErrored, "")
			// Continue to next iteration - Claude may have hit an error
			continue
		}

		// Check for completion token
		if !strings.Contains(claudeResult.Stdout, CompletionToken) {
			out.Printf("  Iteration %d: no %s token found, retrying... (claude %s)\n", item.IterationCount, CompletionToken, ui.Duration(claudeElapsed))
			recordAttemptOutcome(item, domain.AttemptIncomplete, "")
			// No completion token - Claude hit step limit or got stuck
			// Clear any previous failure since this is a different failure mode
			item.LastFailureOutput = ""
			item.LastValidatorFeedback = ""
			continue
		}

		out.Printf("  Iteration %d: %s token found, running verification... (claude %s)\n", item.IterationCount, CompletionToken, ui.Duration(claudeElapsed))

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
					out.Printf("  Iteration %d: validator router failed, running all applicable: %v\n", item.IterationCount, routeErr)
				}
				item.SelectedValidators = selected
				item.ValidatorsRouted = true
				_ = store.SaveWorkItemForSpec(specID, item)
			} else {
				out.Printf("  Iteration %d: could not compute diff for validator routing: %v\n", item.IterationCount, diffErr)
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
			out.Printf("  Iteration %d: no verification command configured, marking complete\n", item.IterationCount)
		}

		if !verifyPassed {
			// Verification failed - inject failure and retry. Signal that the
			// completion claim did not hold so the engine cancels the
			// speculative validators; their feedback is discarded.
			out.Printf("  Iteration %d: verification failed, will retry with failure output (verification %s)\n", item.IterationCount, ui.Duration(verifyElapsed))
			// The human running the loop sees exactly what the runner is about to be
			// handed. verifyOutput is already truncated by verifier.Run, so this block
			// is byte-identical to what lands in item.LastFailureOutput below and in
			// the retry prompt. Verification does not resolve through the engine, so
			// this is the one failure source that renders its own block; everything
			// else reaches the same helper through the resolution ledger.
			printFailureBlock(out, failureSourceVerification, verifyOutput)
			failedPayload := itemPayload
			failedPayload.IterationCount = item.IterationCount
			dispatcher.Dispatch(Event{Name: EventWorkItemVerificationFailed, Payload: failedPayload})
			// A failing verification command is a mechanical failure by the class
			// vocabulary's own terms - the intent was right and the execution slipped -
			// and it is the same default the routing applies to a failure no validator
			// classified.
			recordAttemptOutcome(item, domain.AttemptFailed, validators.FailureMechanical)
			item.LastFailureOutput = verifyOutput
			item.LastValidatorFeedback = "" // Clear any previous validator feedback
			continue
		}

		if verifyCommand != "" {
			out.Printf("  Iteration %d: verification passed! (verification %s)\n", item.IterationCount, ui.Duration(verifyElapsed))
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
			//
			// The failing validators' feedback was already shown as a failure-output
			// block by the engine's resolution ledger when this dispatch joined them,
			// so the human sees it before the injection below and it is not printed
			// again here.
			aggregate := aggregateFromGate(gateErr)
			decision := routeValidationFailure(item, aggregate, caps, defaultExecutorModel, escalatedExecutorModel)
			out.Printf("  Iteration %d: gate blocked workitem-verified (validators %s)\n", item.IterationCount, ui.Duration(joinElapsed))
			out.Printf("  %s\n", decision.logLine(item.IterationCount, maxIterations, caps))
			item.LastFailureOutput = ""
			item.LastValidatorFeedback = gateFeedback(gateErr)
			// What this attempt concluded is kept, persisted with the item, so a later
			// escalated attempt can be handed the conclusions without the attempt. The
			// verbatim feedback above is still only the previous iteration's, which is
			// what a mechanical retry is given.
			recordFailureConclusions(item, aggregate)
			// The attempt is recorded as rejected under the class the validators
			// reported, not the class the caps routed on: the attempt's own record says
			// what was concluded about it, and decision.Class already says what the
			// routing did next.
			recordAttemptOutcome(item, domain.AttemptFailed, reportedFailureClass(aggregate))
			if err := store.SaveWorkItemForSpec(specID, item); err != nil {
				return nil, fmt.Errorf("failed to save work item state: %w", err)
			}
			if decision.Route == RouteNeedsHuman {
				return nil, haltNeedsHuman(store, specID, crID, item, rec, &NeedsHumanError{
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
				if err := escalateScoping(ctx, sc, item, specID, crID, scopingEscalationError(item, aggregate, decision), caps, rec, escalationDiff(ctx, out, validatorRunner)); err != nil {
					return nil, err
				}
			}
			continue
		}
		item.Status = domain.WorkItemCompleted
		recordAttemptOutcome(item, domain.AttemptPassed, "")
		item.LastFailureOutput = ""
		item.LastValidatorFeedback = ""
		if err := store.SaveWorkItemForSpec(specID, item); err != nil {
			return nil, err
		}
		logExecutionEntry(out, store, crID, item, operationType)
		// Written before the commit so the transcript is picked up by the same
		// `git add -A` and lands in the work item's own commit.
		writeRunTranscript(store, crID, item, rec, domain.RunCompleted)
		p.CommitSHA = gitCommitWorkItem(out, projectDir, item, crTitle)
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
func logExecutionEntry(out *ui.Printer, store *internal.YAMLStore, crID string, item *domain.WorkItem, operation string) {
	entry := domain.ExecutionLogEntry{
		WorkItemID:  item.ID,
		SpecRef:     item.SpecRef,
		Operation:   operation,
		CompletedAt: time.Now(),
	}
	if err := store.AppendExecutionLogEntry(crID, entry); err != nil {
		out.Printf("  warning: failed to log execution entry: %v\n", err)
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
		out.Printf("  warning: git add failed: %v\n", err)
		return ""
	}

	// Check if there are changes to commit
	if !git.HasStagedChanges(projectDir) {
		return ""
	}

	// Commit
	if err := git.Commit(projectDir, message); err != nil {
		out.Printf("  warning: git commit failed: %v\n", err)
		return ""
	}

	out.Printf("  Created commit for %s\n", item.ID)

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
				out.Printf("  After-phase: validator router failed, running all applicable: %v\n", routeErr)
			}
			basePayload.SelectedValidatorIDs = selected
			basePayload.ValidatorsRouted = true
		} else {
			out.Printf("  After-phase: could not compute diff for validator routing: %v\n", diffErr)
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
		out.Printf("  After-phase iteration %d: validators failed, will retry with feedback (validators %s)\n", iteration, ui.Duration(joinElapsed))

		// Build prompt for Claude to fix standards issues
		prompt := buildAfterPhasePrompt(feedback)

		out.Printf("  After-phase iteration %d: invoking Claude to fix standards issues...\n", iteration)

		// Invoke Claude, timed from the outside as in the work-item loop, and asking for
		// the same structured output: an after-phase fix is an execution attempt and its
		// spend is part of what the phase cost. It has no work item to book against, so
		// the usage rides on the result until the run record carries it.
		claudeStart := time.Now()
		claudeResult, err := cli.Clone().WithUsageCapture(true).Prompt(ctx, prompt)
		claudeElapsed := time.Since(claudeStart)

		// Detect and handle Claude usage limits without counting the attempt
		// against max iterations. A limit error means ctx was cancelled while
		// waiting/probing (Ctrl+C or timeout) - take the graceful shutdown path.
		if outcome, limitErr := handleClaudeLimits(ctx, out, claudeResult, auth, projectDir); limitErr != nil {
			return limitErr
		} else if outcome == limitWaited {
			out.Printf("  After-phase iteration %d: usage limit handled, re-running this iteration (claude %s)\n", iteration, ui.Duration(claudeElapsed))
			iteration--
			continue
		}

		if err != nil {
			out.Printf("  After-phase iteration %d: Claude invocation failed: %v (claude %s)\n", iteration, err, ui.Duration(claudeElapsed))
			// Continue to next iteration - Claude may have hit an error
			continue
		}

		// Check for completion token
		if !strings.Contains(claudeResult.Stdout, CompletionToken) {
			out.Printf("  After-phase iteration %d: no %s token found, retrying... (claude %s)\n", iteration, CompletionToken, ui.Duration(claudeElapsed))
			continue
		}

		out.Printf("  After-phase iteration %d: %s token found, re-running validators... (claude %s)\n", iteration, CompletionToken, ui.Duration(claudeElapsed))
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
		out.Printf("  warning: git add failed: %v\n", err)
		return ""
	}

	// Check if there are changes to commit
	if !git.HasStagedChanges(projectDir) {
		return ""
	}

	// Commit
	if err := git.Commit(projectDir, message); err != nil {
		out.Printf("  warning: git commit failed: %v\n", err)
		return ""
	}

	out.Println("  Created commit for after-phase validator fixes")

	sha, err := git.HeadSHA(projectDir)
	if err != nil {
		return ""
	}
	return sha
}
