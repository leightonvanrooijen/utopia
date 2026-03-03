package cli

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/leightonvanrooijen/utopia/internal/domain"
	"github.com/leightonvanrooijen/utopia/internal/infra/storage"
	chunkStrategy "github.com/leightonvanrooijen/utopia/internal/strategies/chunk"
	executeStrategy "github.com/leightonvanrooijen/utopia/internal/strategies/execute"
	"github.com/spf13/cobra"
)

// Flags for execute command (package-level for Cobra compatibility)
var (
	executeStrategyFlag      string
	executeChunkStrategyFlag string
	executeTimeoutFlag       int
	executeAllFlag           bool
)

// InitExecuteCmd creates and registers the execute command with the root command.
// This is called from main to wire up the strategy registries.
func InitExecuteCmd(execRegistry *executeStrategy.Registry, chunkRegistry *chunkStrategy.Registry) {
	rootCmd.AddCommand(NewExecuteCmd(execRegistry, chunkRegistry))
}

// NewExecuteCmd creates the execute command with the given strategy registries.
// This allows multiple command instances (useful for testing) and explicit dependency injection.
func NewExecuteCmd(execRegistry *executeStrategy.Registry, chunkRegistry *chunkStrategy.Registry) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "execute [cr-id]",
		Short: "Execute a change request using the Ralph loop",
		Long: `Execute a change request (CR) or spec using the Ralph loop.

This command handles the full workflow:
  1. Loads the change request from .utopia/change-requests/<cr-id>.yaml
  2. Chunks the CR into work items (if not already chunked)
  3. Executes work items until all complete or max iterations is reached

If no CR ID is provided, lists available change requests for interactive selection.

Use --all to execute all CRs in .utopia/change-requests/ in alphabetical order.
If any CR fails, execution stops and reports which CR failed.

The chunking strategy determines how features/tasks become work items:
  - ralph-sequential: One work item per feature/task, executed in order

The execution strategy determines how work items are processed:
  - sequential: Execute one at a time, in order, with retry on failure

Press Ctrl+C to gracefully stop execution (current state will be saved).
Run the command again to resume from where you left off.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runExecute(cmd, args, execRegistry, chunkRegistry)
		},
	}

	cmd.Flags().StringVarP(&executeStrategyFlag, "strategy", "s", "",
		"execution strategy (sequential)")
	cmd.Flags().StringVar(&executeChunkStrategyFlag, "chunk-strategy", "",
		"chunking strategy (ralph-sequential)")
	cmd.Flags().IntVarP(&executeTimeoutFlag, "timeout", "t", 0,
		"timeout in minutes (0 means no timeout)")
	cmd.Flags().BoolVar(&executeAllFlag, "all", false,
		"execute all CRs in .utopia/change-requests/ in alphabetical order")

	return cmd
}

func runExecute(cmd *cobra.Command, args []string, execRegistry *executeStrategy.Registry, chunkRegistry *chunkStrategy.Registry) error {
	projectDir := GetProjectDir(cmd)

	// Validate timeout flag
	if executeTimeoutFlag < 0 {
		return fmt.Errorf("invalid timeout value: %d (must be a positive integer)", executeTimeoutFlag)
	}

	absPath, err := filepath.Abs(projectDir)
	if err != nil {
		return fmt.Errorf("failed to resolve project path: %w", err)
	}

	utopiaDir := filepath.Join(absPath, ".utopia")

	// Check if initialized
	if _, err := os.Stat(utopiaDir); os.IsNotExist(err) {
		return fmt.Errorf("not a Utopia project (run 'utopia init' first)")
	}

	// Load config
	store := storage.NewYAMLStore(utopiaDir)
	config, err := store.LoadConfig()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Handle --all flag for batch execution
	if executeAllFlag {
		if len(args) > 0 {
			return fmt.Errorf("cannot specify CR ID with --all flag")
		}
		return runExecuteAll(cmd, store, config, absPath, utopiaDir, execRegistry, chunkRegistry)
	}

	// Get CR ID from args or interactive selection
	var crID string
	if len(args) > 0 {
		crID = args[0]
	} else {
		// Interactive selection
		selectedID, err := selectChangeRequest(store)
		if err != nil {
			return err
		}
		crID = selectedID
	}

	// Load the change request
	cr, crErr := store.LoadChangeRequest(crID)
	if crErr != nil {
		return fmt.Errorf("change request not found: %s\n\nCheck .utopia/change-requests/ for available change requests", crID)
	}

	// Check if this is an initiative CR (needs per-phase execution)
	if cr.Type == domain.CRTypeInitiative {
		return executeInitiative(cmd, cr, store, config, absPath, utopiaDir, execRegistry, chunkRegistry)
	}

	// Check if work items already exist for this CR
	items, err := store.ListWorkItemsForSpec(crID)
	if err != nil {
		return fmt.Errorf("failed to load work items: %w", err)
	}

	// If no work items exist, chunk the CR first
	if len(items) == 0 {
		items, err = chunkCR(cr, crID, store, config, chunkRegistry, absPath)
		if err != nil {
			return err
		}
	} else {
		fmt.Printf("Found %d existing work item(s) for %s\n", len(items), crID)
	}

	// Determine which execution strategy to use
	strategyName := executeStrategyFlag
	if strategyName == "" {
		strategyName = config.Strategies.Execute
	}

	strategy, ok := execRegistry.Get(strategyName)
	if !ok {
		available := execRegistry.List()
		if len(available) == 0 {
			return fmt.Errorf("no execution strategies registered")
		}
		return fmt.Errorf("unknown strategy %q (available: %v)", strategyName, available)
	}

	fmt.Printf("Using '%s' strategy: %s\n", strategy.Name(), strategy.Description())
	fmt.Printf("Executing CR: %s (%d work items)\n", crID, len(items))
	if executeTimeoutFlag > 0 {
		fmt.Printf("Timeout: %d minute(s)\n", executeTimeoutFlag)
	}
	fmt.Println()

	// Track session start time for duration reporting
	sessionStart := time.Now()

	// Set up context with optional timeout and cancellation for Ctrl+C handling
	var ctx context.Context
	var cancel context.CancelFunc
	if executeTimeoutFlag > 0 {
		ctx, cancel = context.WithTimeout(context.Background(), time.Duration(executeTimeoutFlag)*time.Minute)
	} else {
		ctx, cancel = context.WithCancel(context.Background())
	}
	defer cancel()

	// Handle interrupt signals gracefully
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigChan
		fmt.Println("\n\nInterrupt received, saving state and stopping...")
		cancel()
	}()

	// Run the strategy
	result, err := strategy.Execute(ctx, crID, store, config, absPath)
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			// Timeout reached - clearly differentiate from success
			sessionDuration := time.Since(sessionStart).Round(time.Second)
			fmt.Printf("\n⏱  TIMEOUT REACHED\n")
			fmt.Printf("Session duration: %s\n", sessionDuration)
			fmt.Printf("Completed: %d/%d work items\n", result.Completed, result.Total)
			if result.StoppedAt != "" {
				fmt.Printf("Stopped at: %s\n", result.StoppedAt)
			}
			fmt.Printf("\nProgress has been saved. Run 'utopia execute %s' to resume from where you left off.\n", crID)
			return fmt.Errorf("execution timed out after %d minute(s)", executeTimeoutFlag)
		}
		if ctx.Err() == context.Canceled {
			// Interrupted by user
			fmt.Printf("\nExecution stopped by user.\n")
			fmt.Printf("Completed: %d/%d work items\n", result.Completed, result.Total)
			if result.StoppedAt != "" {
				fmt.Printf("Stopped at: %s\n", result.StoppedAt)
			}
			fmt.Println("\nRun 'utopia execute " + crID + "' to resume from where you left off.")
			return nil // Don't return error for user interrupt
		}
		// Actual error (e.g., max iterations)
		fmt.Printf("\nExecution stopped: %s\n", err)
		fmt.Printf("Completed: %d/%d work items\n", result.Completed, result.Total)
		if result.StoppedAt != "" {
			fmt.Printf("Stopped at: %s\n", result.StoppedAt)
		}
		return err
	}

	// Success!
	fmt.Printf("\nAll work items completed successfully!\n")
	fmt.Printf("Completed: %d/%d work items\n", result.Completed, result.Total)

	// Auto-merge: apply CR changes to specs and commit
	fmt.Println()
	fmt.Println("Merging CR into specs...")
	if err := autoMergeCR(cr, crID, store, absPath, utopiaDir); err != nil {
		// Merge failed but work items are complete - preserve completion state
		fmt.Printf("\n⚠ Merge failed: %s\n", err)
		fmt.Printf("Work items remain completed. You can retry merge with: utopia merge %s\n", crID)
		return nil // Don't return error - work items completed successfully
	}

	return nil
}

// selectChangeRequest lists available CRs and prompts the user to select one.
func selectChangeRequest(store *storage.YAMLStore) (string, error) {
	crs, err := store.ListChangeRequests()
	if err != nil {
		return "", fmt.Errorf("failed to list change requests: %w", err)
	}

	if len(crs) == 0 {
		return "", fmt.Errorf("no change requests found in .utopia/change-requests/\n\nCreate one with: utopia cr")
	}

	fmt.Println("Available change requests:")
	fmt.Println()
	for i, cr := range crs {
		fmt.Printf("  [%d] %s\n", i+1, cr.Title)
	}
	fmt.Println()

	reader := bufio.NewReader(os.Stdin)
	fmt.Print("Select a change request (number): ")
	input, err := reader.ReadString('\n')
	if err != nil {
		return "", fmt.Errorf("failed to read input: %w", err)
	}

	input = strings.TrimSpace(input)
	selection, err := strconv.Atoi(input)
	if err != nil || selection < 1 || selection > len(crs) {
		return "", fmt.Errorf("invalid selection: %s (enter a number between 1 and %d)", input, len(crs))
	}

	selectedCR := crs[selection-1]
	fmt.Printf("\nSelected: %s\n\n", selectedCR.Title)

	return selectedCR.ID, nil
}

// SpecLoaderConfigurable is an optional interface that chunking strategies can implement
// to receive a spec loader for loading referenced specs during bugfix chunking.
type SpecLoaderConfigurable interface {
	SetSpecLoader(loader func(specID string) (*domain.Spec, error))
}

// chunkCR invokes the chunking strategy to produce work items from a change request.
func chunkCR(cr *domain.ChangeRequest, crID string, store *storage.YAMLStore, config *domain.Config, registry *chunkStrategy.Registry, projectDir string) ([]*domain.WorkItem, error) {
	fmt.Printf("Chunking change request: %s\n", cr.Title)

	// Update CR status to in-progress when chunking begins
	cr.Status = domain.ChangeRequestInProgress
	if err := store.SaveChangeRequest(cr); err != nil {
		return nil, fmt.Errorf("failed to update CR status: %w", err)
	}

	// Determine which chunking strategy to use
	strategyName := executeChunkStrategyFlag
	if strategyName == "" {
		strategyName = config.Strategies.Chunk
	}

	strategy, ok := registry.Get(strategyName)
	if !ok {
		available := registry.List()
		if len(available) == 0 {
			return nil, fmt.Errorf("no chunking strategies registered")
		}
		return nil, fmt.Errorf("unknown chunk strategy %q (available: %v)", strategyName, available)
	}

	// Configure spec loader if the strategy supports it (needed for bugfix CRs)
	if configurable, ok := strategy.(SpecLoaderConfigurable); ok {
		configurable.SetSpecLoader(store.LoadSpec)
	}

	fmt.Printf("Using '%s' chunk strategy: %s\n", strategy.Name(), strategy.Description())

	// Run the chunking strategy (includes validation)
	workItems, err := strategy.Chunk(cr)
	if err != nil {
		return nil, fmt.Errorf("chunking failed: %w", err)
	}

	// Save work items to .utopia/work-items/<id>/
	for _, item := range workItems {
		if err := store.SaveWorkItemForSpec(crID, item); err != nil {
			return nil, fmt.Errorf("failed to save work item %s: %w", item.ID, err)
		}
	}

	fmt.Printf("Created %d work item(s)\n\n", len(workItems))

	// Commit work items to git
	if err := gitCommitChunk(projectDir, crID); err != nil {
		// Log but don't fail - work items are saved, commit is non-critical
		fmt.Printf("⚠ Git commit warning: %s\n", err)
	} else {
		fmt.Printf("✓ Committed work items for %s\n", crID)
	}

	// Print summary
	fmt.Println("Work items:")
	for _, item := range workItems {
		fmt.Printf("  [%d] %s\n", item.Order, item.ID)
	}
	fmt.Println()

	return workItems, nil
}

// gitCommitChunk creates a git commit for newly generated work items.
// Only stages and commits the work items for this specific CR, not pre-existing items.
func gitCommitChunk(projectDir, crID string) error {
	// Stage only the work items for this CR
	workItemsDir := filepath.Join(projectDir, ".utopia", "work-items", crID)
	addCmd := exec.Command("git", "add", workItemsDir)
	addCmd.Dir = projectDir
	var addStderr bytes.Buffer
	addCmd.Stderr = &addStderr
	if err := addCmd.Run(); err != nil {
		return fmt.Errorf("git add failed: %w (%s)", err, addStderr.String())
	}

	// Check if there are changes to commit
	diffCmd := exec.Command("git", "diff", "--cached", "--quiet")
	diffCmd.Dir = projectDir
	if err := diffCmd.Run(); err == nil {
		// No changes to commit (exit code 0 means no diff)
		return nil
	}

	// Commit with chunk message format
	msg := fmt.Sprintf("chunk: %s", crID)
	commitCmd := exec.Command("git", "commit", "-m", msg)
	commitCmd.Dir = projectDir
	var commitStderr bytes.Buffer
	commitCmd.Stderr = &commitStderr
	if err := commitCmd.Run(); err != nil {
		return fmt.Errorf("git commit failed: %w (%s)", err, commitStderr.String())
	}

	return nil
}

// gitCommitCleanup creates a git commit for the removal of CR and work items after merge.
// Stages removal of .utopia/work-items/<cr-id>/ and .utopia/change-requests/<cr-id>.yaml
func gitCommitCleanup(projectDir, crID, utopiaDir string) error {
	// Stage removal of work items directory
	workItemsDir := filepath.Join(utopiaDir, "work-items", crID)
	addWorkItemsCmd := exec.Command("git", "add", workItemsDir)
	addWorkItemsCmd.Dir = projectDir
	// Ignore errors - directory may not exist or may already be staged

	var addStderr bytes.Buffer
	addWorkItemsCmd.Stderr = &addStderr
	addWorkItemsCmd.Run() // Best effort

	// Stage removal of CR file
	crFile := filepath.Join(utopiaDir, "change-requests", crID+".yaml")
	addCRCmd := exec.Command("git", "add", crFile)
	addCRCmd.Dir = projectDir
	addCRCmd.Stderr = &addStderr
	addCRCmd.Run() // Best effort

	// Check if there are changes to commit
	diffCmd := exec.Command("git", "diff", "--cached", "--quiet")
	diffCmd.Dir = projectDir
	if err := diffCmd.Run(); err == nil {
		// No changes to commit (exit code 0 means no diff)
		return nil
	}

	// Commit with cleanup message format
	msg := fmt.Sprintf("cleanup: complete %s", crID)
	commitCmd := exec.Command("git", "commit", "-m", msg)
	commitCmd.Dir = projectDir
	var commitStderr bytes.Buffer
	commitCmd.Stderr = &commitStderr
	if err := commitCmd.Run(); err != nil {
		return fmt.Errorf("git commit failed: %w (%s)", err, commitStderr.String())
	}

	return nil
}

// autoMergeCR performs the merge after all work items complete successfully.
// It applies CR changes to specs, creates a git commit, then cleans up CR/work items.
// On failure, work item completion state is preserved for manual retry.
func autoMergeCR(cr *domain.ChangeRequest, crID string, store *storage.YAMLStore, projectDir, utopiaDir string) error {
	// Step 1: Apply changes to specs (without deleting CR/work items)
	mergeResult, err := PerformMerge(cr, store)
	if err != nil {
		return fmt.Errorf("failed to apply spec changes: %w", err)
	}

	// Print merge summary
	if mergeResult.IsRefactor {
		fmt.Println("Refactor CR - no spec modifications")
	} else {
		for _, specID := range mergeResult.SpecsModified {
			fmt.Printf("✓ Updated spec: %s\n", specID)
		}
		for _, specID := range mergeResult.SpecsDeleted {
			fmt.Printf("✓ Deleted spec: %s\n", specID)
		}
	}

	// Step 2: Create git commit for spec changes
	if err := GitCommitSpecMerge(projectDir, cr, mergeResult); err != nil {
		return fmt.Errorf("failed to create git commit: %w", err)
	}
	fmt.Println("✓ Created git commit for spec merge")

	// Step 3: Clean up CR and work items (now safe - commit exists for rollback)
	if err := CleanupAfterMerge(cr, crID, utopiaDir, store); err != nil {
		// Log but don't fail - commit succeeded, cleanup is non-critical
		fmt.Printf("⚠ Cleanup warning: %s\n", err)
	} else {
		fmt.Printf("✓ Cleaned up CR and work items\n")

		// Step 4: Create cleanup commit for removed CR and work items
		if err := gitCommitCleanup(projectDir, crID, utopiaDir); err != nil {
			fmt.Printf("⚠ Cleanup commit warning: %s\n", err)
		} else {
			fmt.Printf("✓ Created cleanup commit\n")
		}
	}

	// Step 5: Mark conversations that reference this CR as ready for harvest
	// Transitions pending-execution → unprocessed so harvest can find them
	if err := store.MarkConversationsReadyForHarvest(crID); err != nil {
		fmt.Printf("⚠ Failed to update conversation status: %s\n", err)
	}

	fmt.Printf("\nSuccessfully merged: %s\n", cr.Title)
	return nil
}

