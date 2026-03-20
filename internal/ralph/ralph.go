package ralph

import (
	"context"
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
func Execute(ctx context.Context, specID string, store *internal.YAMLStore, config *domain.Config, projectDir string) (*Result, error) {
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
	verifier := verification.NewRunner(projectDir)
	validatorRunner := validators.NewRunner(projectDir)

	// Load validators from config
	var validatorList []*domain.Validator
	for _, path := range config.Validators {
		v, err := store.LoadValidator(path)
		if err != nil {
			return nil, fmt.Errorf("failed to load validator %s: %w", path, err)
		}
		validatorList = append(validatorList, v)
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
		err := executeWorkItem(ctx, item, specID, store, cli, verifier, validatorRunner, validatorList, config, projectDir)
		if err != nil {
			result.StoppedAt = item.ID
			result.Reason = err.Error()
			return result, err
		}

		result.Completed++
		fmt.Printf("[%d/%d] %s - completed in %d iteration(s)\n", i+1, len(items), item.ID, item.IterationCount)
	}

	// Run after-phase validators once all work items are complete
	if result.Completed == result.Total && len(validatorList) > 0 {
		if err := runAfterPhaseValidators(ctx, validatorRunner, validatorList); err != nil {
			result.Reason = err.Error()
			return result, err
		}
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
	validatorRunner *validators.Runner,
	validatorList []*domain.Validator,
	config *domain.Config,
	projectDir string,
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
		if err != nil {
			// Check for rate limit before counting this as a failed iteration
			if claudeResult != nil && DetectRateLimit(claudeResult.Stdout, claudeResult.Stderr) {
				// Rate limit detected - wait and retry without counting this iteration
				item.IterationCount-- // Undo the increment since this shouldn't count

				waitDuration, parseErr := ParseRateLimitWait(claudeResult.Stdout, claudeResult.Stderr)
				if parseErr != nil {
					fmt.Printf("  Rate limit detected but failed to parse reset time: %v\n", parseErr)
					fmt.Printf("  Falling back to %v wait...\n", DefaultRateLimitWait)
				}

				fmt.Printf("  %s\n", FormatWaitMessage(claudeResult.Stdout, claudeResult.Stderr))

				// Sleep until rate limit resets
				select {
				case <-ctx.Done():
					_ = store.SaveWorkItemForSpec(specID, item)
					return ctx.Err()
				case <-time.After(waitDuration):
					// Rate limit should have reset, retry
					continue
				}
			}

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

		// Token found - run verification
		if verifyCommand == "" {
			// No verification configured - consider it done
			fmt.Printf("  Iteration %d: no verification command configured, marking complete\n", item.IterationCount)
			item.Status = domain.WorkItemCompleted
			item.LastFailureOutput = ""
			item.LastValidatorFeedback = ""
			if err := store.SaveWorkItemForSpec(specID, item); err != nil {
				return err
			}
			logExecutionEntry(store, crID, item, operationType)
			gitCommitWorkItem(projectDir, item, crTitle)
			return nil
		}

		// Compute git diff once before verification/validation
		gitDiff, _ := validatorRunner.GetGitDiff(ctx)

		// Run verification first, then validators if verification passes
		// Validators run in parallel up to the configured concurrency limit
		result := runVerificationWithValidators(ctx, verifier, verifyCommand, validatorRunner, validatorList, gitDiff, config.Verification.ValidatorConcurrency)

		if result.VerifyErr != nil {
			return fmt.Errorf("verification command failed to execute: %w", result.VerifyErr)
		}

		if !result.VerifyPassed {
			// Verification failed - inject failure and retry
			// Validators were cancelled, their feedback is discarded
			fmt.Printf("  Iteration %d: verification failed, will retry with failure output\n", item.IterationCount)
			item.LastFailureOutput = result.VerifyOutput
			item.LastValidatorFeedback = "" // Clear any previous validator feedback
			continue
		}

		fmt.Printf("  Iteration %d: verification passed!\n", item.IterationCount)

		// Verification passed - check validator results
		allValidatorsPassed := true
		var validatorFeedback strings.Builder
		for _, vr := range result.ValidatorResults {
			if vr.Err != nil {
				fmt.Printf("  Iteration %d: validator %s error: %v\n", item.IterationCount, vr.ID, vr.Err)
				allValidatorsPassed = false
				validatorFeedback.WriteString(fmt.Sprintf("Validator %s error: %v\n\n", vr.ID, vr.Err))
			} else if vr.Result != nil && !vr.Result.Passed {
				fmt.Printf("  Iteration %d: validator %s failed\n", item.IterationCount, vr.ID)
				allValidatorsPassed = false
				validatorFeedback.WriteString(fmt.Sprintf("Validator %s failed:\n%s\n\n", vr.ID, vr.Result.Feedback))
			} else {
				fmt.Printf("  Iteration %d: validator %s passed\n", item.IterationCount, vr.ID)
			}
		}

		if !allValidatorsPassed {
			// Validators failed - inject their feedback and retry
			// Test failures are cleared since verification passed
			feedback := validatorFeedback.String()
			fmt.Printf("  Iteration %d: validators failed, will retry with feedback\n", item.IterationCount)
			// Print validator feedback to stdout so humans can see the same content fed to the AI
			fmt.Printf("\n--- Validator Failure Feedback ---\n%s\n--- End Validator Feedback ---\n\n", feedback)
			item.LastFailureOutput = "" // Tests passed, clear test failures
			item.LastValidatorFeedback = feedback
			continue
		}

		// All checks passed!
		item.Status = domain.WorkItemCompleted
		item.LastFailureOutput = ""
		item.LastValidatorFeedback = ""
		if err := store.SaveWorkItemForSpec(specID, item); err != nil {
			return err
		}
		logExecutionEntry(store, crID, item, operationType)
		gitCommitWorkItem(projectDir, item, crTitle)
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

// VerificationWithValidation holds the result of running verification and validators in parallel.
type VerificationWithValidation struct {
	// VerifyPassed is true if the verification command succeeded
	VerifyPassed bool
	// VerifyOutput contains verification output (for failure injection)
	VerifyOutput string
	// VerifyErr is set if verification failed to execute (not just failed tests)
	VerifyErr error
	// ValidatorResults contains results from all validators (only populated if verification passed)
	ValidatorResults []validators.ValidatorResult
}

// runVerificationWithValidators runs verification first, then validators sequentially.
// Validators only start after verification passes, avoiding wasted compute.
// Validators run in parallel up to the configured concurrency limit.
func runVerificationWithValidators(
	ctx context.Context,
	verifier *verification.Runner,
	verifyCommand string,
	validatorRunner *validators.Runner,
	validatorList []*domain.Validator,
	gitDiff string,
	validatorConcurrency int,
) *VerificationWithValidation {
	result := &VerificationWithValidation{}

	// Run verification first
	verifyResult, err := verifier.Run(ctx, verifyCommand)

	if err != nil {
		// Verification command failed to execute
		result.VerifyErr = err
		return result
	}

	if !verifyResult.Passed {
		// Verification failed - skip validators entirely (no wasted compute)
		result.VerifyPassed = false
		result.VerifyOutput = verifyResult.Output
		return result
	}

	// Verification passed
	result.VerifyPassed = true

	// Run validators only if verification passed
	if len(validatorList) > 0 {
		result.ValidatorResults = validatorRunner.RunAllWithDiffLimited(ctx, validatorList, domain.RunAfterWorkitem, gitDiff, validatorConcurrency)
	}

	return result
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
func gitCommitWorkItem(projectDir string, item *domain.WorkItem, crTitle string) {
	// Build commit message: subject line + body with CR title
	subject := fmt.Sprintf("workitem: %s", item.ID)
	body := crTitle
	message := fmt.Sprintf("%s\n\n%s", subject, body)

	// Stage all changes first to check if there's anything to commit
	if err := git.Add(projectDir, "-A"); err != nil {
		fmt.Printf("  warning: git add failed: %v\n", err)
		return
	}

	// Check if there are changes to commit
	if !git.HasStagedChanges(projectDir) {
		return
	}

	// Commit
	if err := git.Commit(projectDir, message); err != nil {
		fmt.Printf("  warning: git commit failed: %v\n", err)
		return
	}

	fmt.Printf("  Created commit for %s\n", item.ID)
}

// runAfterPhaseValidators executes validators with the "after-phase" trigger.
// These validators run once after all work items in a phase have completed.
// Returns an error if any after-phase validator fails.
func runAfterPhaseValidators(
	ctx context.Context,
	validatorRunner *validators.Runner,
	validatorList []*domain.Validator,
) error {
	// Run validators with after-phase trigger
	results := validatorRunner.RunAll(ctx, validatorList, domain.RunAfterPhase)

	if len(results) == 0 {
		// No after-phase validators configured
		return nil
	}

	fmt.Printf("Running %d after-phase validator(s)...\n", len(results))

	// Aggregate results
	aggregate := validators.AggregateResults(results)

	if !aggregate.Passed {
		return fmt.Errorf("after-phase validators failed:\n%s", aggregate.Feedback)
	}

	fmt.Println("All after-phase validators passed!")
	return nil
}
