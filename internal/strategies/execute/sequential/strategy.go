package sequential

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/leightonvanrooijen/utopia/internal/domain"
	"github.com/leightonvanrooijen/utopia/internal/infra/claude"
	"github.com/leightonvanrooijen/utopia/internal/infra/storage"
	"github.com/leightonvanrooijen/utopia/internal/strategies/execute"
	"github.com/leightonvanrooijen/utopia/internal/verification"
)

// CompletionToken is the marker that indicates Claude has finished the task.
const CompletionToken = "<COMPLETE>"

// Strategy implements sequential work item execution.
// It processes work items one at a time, in order, retrying until
// verification passes or max iterations is reached.
type Strategy struct{}

// New creates a new sequential execution strategy.
func New() *Strategy {
	return &Strategy{}
}

// Name returns the strategy identifier.
func (s *Strategy) Name() string {
	return "sequential"
}

// Description returns a human-readable description for CLI help.
func (s *Strategy) Description() string {
	return "Execute work items one at a time, in order, with retry on failure"
}

// Execute runs all work items for a spec sequentially.
func (s *Strategy) Execute(ctx context.Context, specID string, store *storage.YAMLStore, config *domain.Config, projectDir string) (*execute.Result, error) {
	// Load work items for this spec
	items, err := store.ListWorkItemsForSpec(specID)
	if err != nil {
		return nil, fmt.Errorf("failed to load work items: %w", err)
	}

	if len(items) == 0 {
		return &execute.Result{
			Completed: 0,
			Total:     0,
			Reason:    "no work items found",
		}, nil
	}

	// Sort by Order
	sort.Slice(items, func(i, j int) bool {
		return items[i].Order < items[j].Order
	})

	result := &execute.Result{
		Total: len(items),
	}

	// Create Claude CLI wrapper
	cli := claude.NewCLI()

	// Create verification runner
	verifier := verification.NewRunner(projectDir)

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
		err := s.executeWorkItem(ctx, item, specID, store, cli, verifier, config)
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
func (s *Strategy) executeWorkItem(
	ctx context.Context,
	item *domain.WorkItem,
	specID string,
	store *storage.YAMLStore,
	cli *claude.CLI,
	verifier *verification.Runner,
	config *domain.Config,
) error {
	maxIterations := config.Verification.MaxIterations
	verifyCommand := config.Verification.Command

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
		prompt := s.buildPrompt(item)

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
			return store.SaveWorkItemForSpec(specID, item)
		}

		verifyResult, err := verifier.Run(ctx, verifyCommand)
		if err != nil {
			return fmt.Errorf("verification command failed to execute: %w", err)
		}

		if verifyResult.Passed {
			fmt.Printf("  Iteration %d: verification passed!\n", item.IterationCount)
			item.Status = domain.WorkItemCompleted
			item.LastFailureOutput = ""
			return store.SaveWorkItemForSpec(specID, item)
		}

		// Verification failed - inject failure and retry
		fmt.Printf("  Iteration %d: verification failed, will retry with failure output\n", item.IterationCount)
		item.LastFailureOutput = verifyResult.Output
	}
}

// buildPrompt constructs the prompt for Claude, including failure injection.
func (s *Strategy) buildPrompt(item *domain.WorkItem) string {
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
