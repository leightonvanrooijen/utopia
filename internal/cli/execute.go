package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/leightonvanrooijen/utopia/internal/domain"
	"github.com/leightonvanrooijen/utopia/internal/infra/storage"
	chunkStrategy "github.com/leightonvanrooijen/utopia/internal/strategies/chunk"
	executeStrategy "github.com/leightonvanrooijen/utopia/internal/strategies/execute"
	"github.com/spf13/cobra"
)

var (
	executeStrategyFlag      string
	executeChunkStrategyFlag string
	executeTimeoutFlag       int
	executeRegistry          *executeStrategy.Registry
	executeChunkRegistry     *chunkStrategy.Registry
)

var executeCmd = &cobra.Command{
	Use:   "execute <cr-id>",
	Short: "Execute a change request using the Ralph loop",
	Long: `Execute a change request (CR) or spec using the Ralph loop.

This command handles the full workflow:
  1. Loads the change request from .utopia/change-requests/<cr-id>.yaml
  2. Chunks the CR into work items (if not already chunked)
  3. Executes work items until all complete or max iterations is reached

The chunking strategy determines how features/tasks become work items:
  - ralph-sequential: One work item per feature/task, executed in order

The execution strategy determines how work items are processed:
  - sequential: Execute one at a time, in order, with retry on failure

Press Ctrl+C to gracefully stop execution (current state will be saved).
Run the command again to resume from where you left off.`,
	Args: cobra.ExactArgs(1),
	RunE: runExecute,
}

func init() {
	rootCmd.AddCommand(executeCmd)

	executeCmd.Flags().StringVarP(&executeStrategyFlag, "strategy", "s", "",
		"execution strategy (sequential)")
	executeCmd.Flags().StringVar(&executeChunkStrategyFlag, "chunk-strategy", "",
		"chunking strategy (ralph-sequential)")
	executeCmd.Flags().IntVarP(&executeTimeoutFlag, "timeout", "t", 0,
		"timeout in minutes (0 means no timeout)")

	// Initialize registries - strategies will be registered at startup
	executeRegistry = executeStrategy.NewRegistry()
	executeChunkRegistry = chunkStrategy.NewRegistry()
}

// RegisterExecuteStrategy adds a strategy to the registry (called from main)
func RegisterExecuteStrategy(s executeStrategy.Strategy) {
	executeRegistry.Register(s)
}

// RegisterExecuteChunkStrategy adds a chunk strategy to the execute command's registry (called from main)
func RegisterExecuteChunkStrategy(s chunkStrategy.Strategy) {
	executeChunkRegistry.Register(s)
}

func runExecute(cmd *cobra.Command, args []string) error {
	crID := args[0]
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

	// Load the change request
	cr, crErr := store.LoadChangeRequest(crID)
	if crErr != nil {
		return fmt.Errorf("change request not found: %s\n\nCheck .utopia/change-requests/ for available change requests", crID)
	}

	// Check if this is an initiative CR (needs per-phase execution)
	if cr.Type == domain.CRTypeInitiative {
		return executeInitiative(cmd, cr, store, config, absPath, utopiaDir, executeRegistry, executeChunkRegistry)
	}

	// Check if work items already exist for this CR
	items, err := store.ListWorkItemsForSpec(crID)
	if err != nil {
		return fmt.Errorf("failed to load work items: %w", err)
	}

	// If no work items exist, chunk the CR first
	if len(items) == 0 {
		items, err = chunkCR(cr, crID, store, config, executeChunkRegistry)
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

	strategy, ok := executeRegistry.Get(strategyName)
	if !ok {
		available := executeRegistry.List()
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

	return nil
}

// chunkCR invokes the chunking strategy to produce work items from a change request.
func chunkCR(cr *domain.ChangeRequest, crID string, store *storage.YAMLStore, config *domain.Config, registry *chunkStrategy.Registry) ([]*domain.WorkItem, error) {
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

	// Print summary
	fmt.Println("Work items:")
	for _, item := range workItems {
		fmt.Printf("  [%d] %s\n", item.Order, item.ID)
	}
	fmt.Println()

	return workItems, nil
}

// chunkPhase invokes the chunking strategy to produce work items for a single phase of an initiative.
func chunkPhase(crID string, phaseIndex int, phase *domain.Phase, store *storage.YAMLStore, config *domain.Config, registry *chunkStrategy.Registry) ([]*domain.WorkItem, error) {
	fmt.Printf("Chunking phase %d (type: %s)\n", phaseIndex+1, phase.Type)

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

	fmt.Printf("Using '%s' chunk strategy: %s\n", strategy.Name(), strategy.Description())

	// Run the chunking strategy on this phase
	workItems, err := strategy.ChunkPhase(crID, phaseIndex, phase)
	if err != nil {
		return nil, fmt.Errorf("chunking failed: %w", err)
	}

	// Save work items to .utopia/work-items/<cr-id>/phase-<n>/
	phaseWorkDir := fmt.Sprintf("%s/phase-%d", crID, phaseIndex)
	for _, item := range workItems {
		if err := store.SaveWorkItemForSpec(phaseWorkDir, item); err != nil {
			return nil, fmt.Errorf("failed to save work item %s: %w", item.ID, err)
		}
	}

	fmt.Printf("Created %d work item(s)\n\n", len(workItems))

	// Print summary
	fmt.Println("Work items:")
	for _, item := range workItems {
		fmt.Printf("  [%d] %s\n", item.Order, item.ID)
	}
	fmt.Println()

	return workItems, nil
}

// executeInitiative handles execution for initiative CRs, executing phases in order.
// Each phase must complete before the next can begin.
func executeInitiative(cmd *cobra.Command, cr *domain.ChangeRequest, store *storage.YAMLStore, config *domain.Config, projectDir, utopiaDir string, execRegistry *executeStrategy.Registry, chunkReg *chunkStrategy.Registry) error {
	fmt.Printf("Executing initiative: %s\n", cr.Title)
	fmt.Printf("Phases: %d total, current: %d\n", len(cr.Phases), cr.CurrentPhase+1)

	// Find the current phase
	phaseIndex := cr.CurrentPhase
	if phaseIndex >= len(cr.Phases) {
		fmt.Printf("\nAll phases complete!\n")
		fmt.Printf("Run 'utopia merge %s' to finalize the initiative\n", cr.ID)
		return nil
	}

	phase := cr.Phases[phaseIndex]
	phaseWorkDir := fmt.Sprintf("%s/phase-%d", cr.ID, phaseIndex)

	// Check if work items exist for this phase
	items, err := store.ListWorkItemsForSpec(phaseWorkDir)
	if err != nil {
		return fmt.Errorf("failed to load work items: %w", err)
	}

	// If no work items exist, chunk this phase first
	if len(items) == 0 {
		// Update phase status to in-progress
		cr.Phases[phaseIndex].Status = domain.PhaseStatusInProgress
		if cr.Status != domain.ChangeRequestInProgress {
			cr.Status = domain.ChangeRequestInProgress
		}
		if err := store.SaveChangeRequest(cr); err != nil {
			return fmt.Errorf("failed to update CR status: %w", err)
		}

		items, err = chunkPhase(cr.ID, phaseIndex, &phase, store, config, chunkReg)
		if err != nil {
			return err
		}
	} else {
		fmt.Printf("Found %d existing work item(s) for phase %d\n", len(items), phaseIndex+1)
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

	fmt.Printf("\nExecuting phase %d/%d (type: %s)\n", phaseIndex+1, len(cr.Phases), phase.Type)
	fmt.Printf("Using '%s' strategy: %s\n", strategy.Name(), strategy.Description())
	fmt.Printf("Work items: %d\n", len(items))
	if executeTimeoutFlag > 0 {
		fmt.Printf("Timeout: %d minute(s)\n", executeTimeoutFlag)
	}
	fmt.Println()

	// Track session start time
	sessionStart := time.Now()

	// Set up context with optional timeout
	var ctx context.Context
	var cancel context.CancelFunc
	if executeTimeoutFlag > 0 {
		ctx, cancel = context.WithTimeout(context.Background(), time.Duration(executeTimeoutFlag)*time.Minute)
	} else {
		ctx, cancel = context.WithCancel(context.Background())
	}
	defer cancel()

	// Handle interrupt signals
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigChan
		fmt.Println("\n\nInterrupt received, saving state and stopping...")
		cancel()
	}()

	// Run the strategy for this phase's work items
	result, err := strategy.Execute(ctx, phaseWorkDir, store, config, projectDir)
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			sessionDuration := time.Since(sessionStart).Round(time.Second)
			fmt.Printf("\n⏱  TIMEOUT REACHED\n")
			fmt.Printf("Session duration: %s\n", sessionDuration)
			fmt.Printf("Phase %d completed: %d/%d work items\n", phaseIndex+1, result.Completed, result.Total)
			if result.StoppedAt != "" {
				fmt.Printf("Stopped at: %s\n", result.StoppedAt)
			}
			fmt.Printf("\nProgress saved. Run 'utopia execute %s' to resume.\n", cr.ID)
			return fmt.Errorf("execution timed out after %d minute(s)", executeTimeoutFlag)
		}
		if ctx.Err() == context.Canceled {
			fmt.Printf("\nExecution stopped by user.\n")
			fmt.Printf("Phase %d completed: %d/%d work items\n", phaseIndex+1, result.Completed, result.Total)
			if result.StoppedAt != "" {
				fmt.Printf("Stopped at: %s\n", result.StoppedAt)
			}
			fmt.Println("\nRun 'utopia execute " + cr.ID + "' to resume.")
			return nil
		}
		// Actual error
		fmt.Printf("\nExecution stopped: %s\n", err)
		fmt.Printf("Phase %d completed: %d/%d work items\n", phaseIndex+1, result.Completed, result.Total)
		if result.StoppedAt != "" {
			fmt.Printf("Stopped at: %s\n", result.StoppedAt)
		}
		return err
	}

	// Phase completed successfully - update status and advance to next phase
	cr.Phases[phaseIndex].Status = domain.PhaseStatusComplete
	cr.CurrentPhase = phaseIndex + 1

	if err := store.SaveChangeRequest(cr); err != nil {
		return fmt.Errorf("failed to update CR status: %w", err)
	}

	fmt.Printf("\nPhase %d completed successfully!\n", phaseIndex+1)
	fmt.Printf("Work items: %d/%d\n", result.Completed, result.Total)

	// Show initiative progress
	fmt.Printf("\nInitiative progress:\n")
	for i, p := range cr.Phases {
		status := "pending"
		if p.Status != "" {
			status = string(p.Status)
		}
		marker := " "
		if i == cr.CurrentPhase {
			marker = "→"
		} else if p.Status == domain.PhaseStatusComplete {
			marker = "✓"
		}
		fmt.Printf("  %s [%d] %s (%s)\n", marker, i+1, p.Type, status)
	}

	// Check if there are more phases
	if cr.CurrentPhase < len(cr.Phases) {
		fmt.Printf("\nNext: Phase %d (%s)\n", cr.CurrentPhase+1, cr.Phases[cr.CurrentPhase].Type)
		fmt.Printf("Run 'utopia execute %s' to continue with the next phase\n", cr.ID)
	} else {
		fmt.Printf("\nAll phases complete!\n")
		fmt.Printf("Run 'utopia merge %s' to finalize the initiative\n", cr.ID)
	}

	return nil
}
