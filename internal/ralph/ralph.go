package ralph

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/leightonvanrooijen/utopia/internal"
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
}

// Execute runs all work items for a spec sequentially.
// Work items are processed one at a time, in order, retrying until
// verification passes or max iterations is reached.
// The optional model parameter specifies a Claude model override (e.g., "claude-sonnet-4-20250514").
func Execute(ctx context.Context, specID string, store *internal.YAMLStore, config *domain.Config, projectDir string, model ...string) (*Result, error) {
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

	// Create dependencies
	cli := internal.NewCLI().WithVerbose(true)
	if len(model) > 0 && model[0] != "" {
		cli = cli.WithModel(model[0])
	}
	verifier := verification.NewRunner(projectDir)
	validatorRunner := validators.NewRunner(projectDir).WithModelConfig(config.Models)

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

		fmt.Printf("[%d/%d] %s - starting execution\n", i+1, len(items), item.ID)

		// Execute this work item with the Ralph loop
		err := executeWorkItem(ctx, item, specID, store, cli, verifier, config, projectDir, dispatcher, basePayload, validatorRunner, validatorList)
		if err != nil {
			result.StoppedAt = item.ID
			result.Reason = err.Error()
			failPayload := basePayload
			failPayload.Reason = err.Error()
			dispatcher.Dispatch(Event{Name: EventExecutionFailed, Payload: failPayload})
			return result, err
		}

		result.Completed++
		fmt.Printf("[%d/%d] %s - completed in %d iteration(s)\n", i+1, len(items), item.ID, item.IterationCount)
	}

	// Run after-phase validators once all work items are complete. This also
	// fires phase-verified and phase-completed (and their gating connectors)
	// even when no validators are configured.
	if result.Completed == result.Total {
		if err := runAfterPhaseValidators(ctx, cli, config, projectDir, dispatcher, basePayload, validatorRunner, validatorList); err != nil {
			result.Reason = err.Error()
			failPayload := basePayload
			failPayload.Reason = err.Error()
			dispatcher.Dispatch(Event{Name: EventExecutionFailed, Payload: failPayload})
			return result, err
		}
		dispatcher.Dispatch(Event{Name: EventExecutionCompleted, Payload: basePayload})
	}

	return result, nil
}

// executeWorkItem runs the Ralph loop for a single work item until completion.
func executeWorkItem(
	ctx context.Context,
	item *domain.WorkItem,
	specID string,
	store *internal.YAMLStore,
	cli *internal.CLI,
	verifier *verification.Runner,
	config *domain.Config,
	projectDir string,
	dispatcher *Dispatcher,
	basePayload EventPayload,
	validatorRunner *validators.Runner,
	validatorList []*domain.Validator,
) error {
	maxIterations := config.Verification.MaxIterations
	verifyCommand := config.Verification.Command

	// Load CR title for commit message and operation type for execution log
	crID := extractCRID(specID)
	crTitle := ""
	operationType := "refactor" // default for refactor CRs
	if cr, err := store.LoadChangeRequest(crID); err == nil {
		crTitle = cr.Title
		operationType = deriveOperationType(cr, item.SpecRef)
	}

	itemPayload := basePayload
	itemPayload.WorkItemID = item.ID
	itemPayload.IterationCount = item.IterationCount
	// A gate blocking workitem-started aborts the run before the item runs.
	if gateErr := dispatcher.Dispatch(Event{Name: EventWorkItemStarted, Payload: itemPayload}); gateErr != nil {
		return gateErr
	}

	for {
		// Check context cancellation (Ctrl+C)
		select {
		case <-ctx.Done():
			// Save current state before exiting
			_ = store.SaveWorkItemForSpec(specID, item)
			return ctx.Err()
		default:
		}

		// Increment iteration count
		item.IterationCount++
		item.Status = domain.WorkItemInProgress

		// Check max iterations
		if maxIterations > 0 && item.IterationCount > maxIterations {
			item.Status = domain.WorkItemFailed
			_ = store.SaveWorkItemForSpec(specID, item)
			return fmt.Errorf("max iterations (%d) reached for work item %s", maxIterations, item.ID)
		}

		// Save current state
		if err := store.SaveWorkItemForSpec(specID, item); err != nil {
			return fmt.Errorf("failed to save work item state: %w", err)
		}

		// Build the prompt (includes failure injection if applicable)
		prompt := buildPrompt(item)

		fmt.Printf("  Iteration %d: invoking Claude...\n", item.IterationCount)

		// Invoke Claude
		claudeResult, err := cli.Prompt(ctx, prompt)

		// Detect and handle Claude usage limits (rolling rate limit or org
		// monthly spend limit) before treating this as a failed iteration.
		// A handled limit means this attempt must not count against max
		// iterations, so we undo the increment and retry with a normally
		// rebuilt prompt. A limit error means ctx was cancelled while
		// waiting/probing (Ctrl+C or timeout) - take the graceful shutdown path.
		if outcome, limitErr := handleClaudeLimits(ctx, claudeResult); limitErr != nil {
			_ = store.SaveWorkItemForSpec(specID, item)
			return limitErr
		} else if outcome == limitWaited {
			item.IterationCount--
			continue
		}

		if err != nil {
			fmt.Printf("  Iteration %d: Claude invocation failed: %v\n", item.IterationCount, err)
			// Continue to next iteration - Claude may have hit an error
			continue
		}

		// Check for completion token
		if !strings.Contains(claudeResult.Stdout, CompletionToken) {
			fmt.Printf("  Iteration %d: no %s token found, retrying...\n", item.IterationCount, CompletionToken)
			// No completion token - Claude hit step limit or got stuck
			// Clear any previous failure since this is a different failure mode
			item.LastFailureOutput = ""
			item.LastValidatorFeedback = ""
			continue
		}

		fmt.Printf("  Iteration %d: %s token found, running verification...\n", item.IterationCount, CompletionToken)

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
		if verifyCommand != "" {
			verifyResult, err := verifier.Run(ctx, verifyCommand)
			if err != nil {
				return fmt.Errorf("verification command failed to execute: %w", err)
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
			fmt.Printf("  Iteration %d: verification failed, will retry with failure output\n", item.IterationCount)
			failedPayload := itemPayload
			failedPayload.IterationCount = item.IterationCount
			dispatcher.Dispatch(Event{Name: EventWorkItemVerificationFailed, Payload: failedPayload})
			item.LastFailureOutput = verifyOutput
			item.LastValidatorFeedback = "" // Clear any previous validator feedback
			continue
		}

		if verifyCommand != "" {
			fmt.Printf("  Iteration %d: verification passed!\n", item.IterationCount)
		}

		// Verification passed - join the speculative validators (and any gating
		// connectors) at workitem-verified. A blocking gate - a failing
		// validator or connector - injects its stdout as feedback into the next
		// iteration and prevents the commit.
		p := itemPayload
		p.IterationCount = item.IterationCount
		if gateErr := dispatcher.Dispatch(Event{Name: EventWorkItemVerified, Payload: p}); gateErr != nil {
			fmt.Printf("  Iteration %d: gate blocked workitem-verified, will retry with feedback\n", item.IterationCount)
			item.LastFailureOutput = ""
			item.LastValidatorFeedback = gateFeedback(gateErr)
			continue
		}
		item.Status = domain.WorkItemCompleted
		item.LastFailureOutput = ""
		item.LastValidatorFeedback = ""
		if err := store.SaveWorkItemForSpec(specID, item); err != nil {
			return err
		}
		logExecutionEntry(store, crID, item, operationType)
		p.CommitSHA = gitCommitWorkItem(projectDir, item, crTitle)
		dispatcher.Dispatch(Event{Name: EventWorkItemCommitted, Payload: p})
		return nil
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
		gateErr := dispatcher.Dispatch(Event{Name: EventPhaseVerified, Payload: basePayload})
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
		feedback := gateFeedback(gateErr)
		fmt.Printf("  After-phase iteration %d: validators failed, will retry with feedback\n", iteration)
		fmt.Printf("\n--- Validator Failure Feedback ---\n%s\n--- End Validator Feedback ---\n\n", feedback)

		// Build prompt for Claude to fix standards issues
		prompt := buildAfterPhasePrompt(feedback)

		fmt.Printf("  After-phase iteration %d: invoking Claude to fix standards issues...\n", iteration)

		// Invoke Claude
		claudeResult, err := cli.Prompt(ctx, prompt)

		// Detect and handle Claude usage limits without counting the attempt
		// against max iterations. A limit error means ctx was cancelled while
		// waiting/probing (Ctrl+C or timeout) - take the graceful shutdown path.
		if outcome, limitErr := handleClaudeLimits(ctx, claudeResult); limitErr != nil {
			return limitErr
		} else if outcome == limitWaited {
			iteration--
			continue
		}

		if err != nil {
			fmt.Printf("  After-phase iteration %d: Claude invocation failed: %v\n", iteration, err)
			// Continue to next iteration - Claude may have hit an error
			continue
		}

		// Check for completion token
		if !strings.Contains(claudeResult.Stdout, CompletionToken) {
			fmt.Printf("  After-phase iteration %d: no %s token found, retrying...\n", iteration, CompletionToken)
			continue
		}

		fmt.Printf("  After-phase iteration %d: %s token found, re-running validators...\n", iteration, CompletionToken)
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
