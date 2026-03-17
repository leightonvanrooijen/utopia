package ralph

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"sort"
	"strings"
	"time"

	"github.com/leightonvanrooijen/utopia/internal"
	"github.com/leightonvanrooijen/utopia/internal/domain"
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
		output, err := cli.Prompt(ctx, prompt)
		if err != nil {
			fmt.Printf("  Iteration %d: Claude invocation failed: %v\n", item.IterationCount, err)
			// Continue to next iteration - Claude may have hit an error
			continue
		}

		// Check for completion token
		if !strings.Contains(output, CompletionToken) {
			fmt.Printf("  Iteration %d: no %s token found, retrying...\n", item.IterationCount, CompletionToken)
			// No completion token - Claude hit step limit or got stuck
			// Clear any previous failure since this is a different failure mode
			item.LastFailureOutput = ""
			continue
		}

		fmt.Printf("  Iteration %d: %s token found, running verification...\n", item.IterationCount, CompletionToken)

		// Token found - run verification
		if verifyCommand == "" {
			// No verification configured - consider it done
			fmt.Printf("  Iteration %d: no verification command configured, marking complete\n", item.IterationCount)
			item.Status = domain.WorkItemCompleted
			item.LastFailureOutput = ""
			if err := store.SaveWorkItemForSpec(specID, item); err != nil {
				return err
			}
			logExecutionEntry(store, crID, item, operationType)
			gitCommitWorkItem(projectDir, item, crTitle)
			return nil
		}

		// Compute git diff ONCE before spawning parallel operations
		// This is shared between verification (for context) and validators
		gitDiff, _ := validatorRunner.GetGitDiff(ctx)

		// Run verification and validators in parallel
		// If verification fails, validators are cancelled immediately
		result := runVerificationWithValidators(ctx, verifier, verifyCommand, validatorRunner, validatorList, gitDiff)

		if result.VerifyErr != nil {
			return fmt.Errorf("verification command failed to execute: %w", result.VerifyErr)
		}

		if !result.VerifyPassed {
			// Verification failed - inject failure and retry
			// Validators were cancelled, their feedback is discarded
			fmt.Printf("  Iteration %d: verification failed, will retry with failure output\n", item.IterationCount)
			item.LastFailureOutput = result.VerifyOutput
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
			fmt.Printf("  Iteration %d: validators failed, will retry with feedback\n", item.IterationCount)
			item.LastFailureOutput = validatorFeedback.String()
			continue
		}

		// All checks passed!
		item.Status = domain.WorkItemCompleted
		item.LastFailureOutput = ""
		if err := store.SaveWorkItemForSpec(specID, item); err != nil {
			return err
		}
		logExecutionEntry(store, crID, item, operationType)
		gitCommitWorkItem(projectDir, item, crTitle)
		return nil
	}
}

// buildPrompt constructs the prompt for Claude, including failure injection.
func buildPrompt(item *domain.WorkItem) string {
	// Start with the base prompt from the work item
	prompt := item.Prompt

	// If there's a previous failure, inject it
	if item.LastFailureOutput != "" {
		// The prompt template already has a PREVIOUS FAILURES section placeholder.
		// However, for execution we need to dynamically inject failures into
		// an already-baked prompt. We'll append a new section.
		prompt = prompt + "\n\n## PREVIOUS FAILURES\n\nThe previous attempt failed with the following output:\n\n```\n" + item.LastFailureOutput + "\n```\n\nPlease address these failures in your implementation."
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

// runVerificationWithValidators runs verification and validators concurrently.
// If verification fails, validators are cancelled immediately to save compute.
// Validators only run when there are validators configured and verification passes.
func runVerificationWithValidators(
	ctx context.Context,
	verifier *verification.Runner,
	verifyCommand string,
	validatorRunner *validators.Runner,
	validatorList []*domain.Validator,
	gitDiff string,
) *VerificationWithValidation {
	result := &VerificationWithValidation{}

	// Create a cancellable context for validators
	// If verification fails, we'll cancel this to stop validators early
	validatorCtx, cancelValidators := context.WithCancel(ctx)
	defer cancelValidators()

	// Channel for verification result (buffered to prevent blocking)
	verifyCh := make(chan struct {
		result *verification.Result
		err    error
	}, 1)

	// Channel for validator results (buffered to prevent blocking)
	validatorsCh := make(chan []validators.ValidatorResult, 1)

	// Start verification in a goroutine
	go func() {
		verifyResult, err := verifier.Run(ctx, verifyCommand)
		verifyCh <- struct {
			result *verification.Result
			err    error
		}{verifyResult, err}
	}()

	// Start validators in a goroutine (only if we have validators)
	hasValidators := len(validatorList) > 0
	if hasValidators {
		go func() {
			results := validatorRunner.RunAllWithDiff(validatorCtx, validatorList, domain.RunAfterWorkitem, gitDiff)
			validatorsCh <- results
		}()
	}

	// Wait for verification result first
	verifyResult := <-verifyCh

	if verifyResult.err != nil {
		// Verification command failed to execute - cancel validators
		cancelValidators()
		result.VerifyErr = verifyResult.err
		return result
	}

	if !verifyResult.result.Passed {
		// Verification failed - cancel validators immediately
		cancelValidators()
		result.VerifyPassed = false
		result.VerifyOutput = verifyResult.result.Output
		return result
	}

	// Verification passed
	result.VerifyPassed = true

	// Wait for validators if we started them
	if hasValidators {
		result.ValidatorResults = <-validatorsCh
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
// Returns nil on success, logs warning and returns nil on failure (non-blocking).
func gitCommitWorkItem(projectDir string, item *domain.WorkItem, crTitle string) {
	// Build commit message: subject line + body with CR title
	subject := fmt.Sprintf("workitem: %s", item.ID)
	body := crTitle
	message := fmt.Sprintf("%s\n\n%s", subject, body)

	// Stage all changes
	addCmd := exec.Command("git", "add", "-A")
	addCmd.Dir = projectDir
	var addStderr bytes.Buffer
	addCmd.Stderr = &addStderr
	if err := addCmd.Run(); err != nil {
		fmt.Printf("  warning: git add failed: %v (%s)\n", err, strings.TrimSpace(addStderr.String()))
		return
	}

	// Check if there are changes to commit
	diffCmd := exec.Command("git", "diff", "--cached", "--quiet")
	diffCmd.Dir = projectDir
	if err := diffCmd.Run(); err == nil {
		// No changes to commit (exit code 0 means no diff)
		return
	}

	// Commit
	commitCmd := exec.Command("git", "commit", "-m", message)
	commitCmd.Dir = projectDir
	var commitStderr bytes.Buffer
	commitCmd.Stderr = &commitStderr
	if err := commitCmd.Run(); err != nil {
		fmt.Printf("  warning: git commit failed: %v (%s)\n", err, strings.TrimSpace(commitStderr.String()))
		return
	}

	fmt.Printf("  Created commit for %s\n", item.ID)
}
