package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/leightonvanrooijen/utopia/internal/domain"
	"github.com/leightonvanrooijen/utopia/internal/infra/storage"
	chunkStrategy "github.com/leightonvanrooijen/utopia/internal/strategies/chunk"
	executeStrategy "github.com/leightonvanrooijen/utopia/internal/strategies/execute"
	"github.com/spf13/cobra"
)

// initiativeCoreOpts configures the behavior of executeInitiativeCore.
type initiativeCoreOpts struct {
	// showTimeoutDetails enables detailed timeout/interrupt messages (for standalone mode)
	showTimeoutDetails bool
	// showPhaseSummary enables the phase progress summary at completion
	showPhaseSummary bool
	// sessionStart is used for timeout duration reporting (only when showTimeoutDetails is true)
	sessionStart time.Time
	// autoMerge controls whether to auto-merge on completion
	autoMerge bool
}

// executeInitiativeCore contains the shared logic for executing initiative CRs.
// Both executeInitiative and executeSingleInitiative delegate to this function.
func executeInitiativeCore(
	ctx context.Context,
	cr *domain.ChangeRequest,
	store *storage.YAMLStore,
	config *domain.Config,
	projectDir, utopiaDir string,
	strategy executeStrategy.Strategy,
	chunkReg *chunkStrategy.Registry,
	opts initiativeCoreOpts,
) error {
	// Execute phases continuously until all complete or interrupted
	for cr.CurrentPhase < len(cr.Phases) {
		// Check context before each phase
		if ctx.Err() != nil {
			return ctx.Err()
		}

		phaseIndex := cr.CurrentPhase
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

			items, err = chunkPhase(cr.ID, phaseIndex, &phase, store, config, chunkReg, projectDir)
			if err != nil {
				return err
			}
		} else {
			fmt.Printf("Found %d existing work item(s) for phase %d\n", len(items), phaseIndex+1)
		}

		fmt.Printf("\nExecuting phase %d/%d (type: %s)\n", phaseIndex+1, len(cr.Phases), phase.Type)
		fmt.Printf("Using '%s' strategy: %s\n", strategy.Name(), strategy.Description())
		fmt.Printf("Work items: %d\n", len(items))
		if opts.showTimeoutDetails && executeTimeoutFlag > 0 {
			fmt.Printf("Timeout: %d minute(s)\n", executeTimeoutFlag)
		}
		fmt.Println()

		// Run the strategy for this phase's work items
		result, err := strategy.Execute(ctx, phaseWorkDir, store, config, projectDir)
		if err != nil {
			if opts.showTimeoutDetails && ctx.Err() == context.DeadlineExceeded {
				sessionDuration := time.Since(opts.sessionStart).Round(time.Second)
				fmt.Printf("\n⏱  TIMEOUT REACHED\n")
				fmt.Printf("Session duration: %s\n", sessionDuration)
				fmt.Printf("Phase %d completed: %d/%d work items\n", phaseIndex+1, result.Completed, result.Total)
				if result.StoppedAt != "" {
					fmt.Printf("Stopped at: %s\n", result.StoppedAt)
				}
				fmt.Printf("\nProgress saved. Run 'utopia execute %s' to resume.\n", cr.ID)
				return fmt.Errorf("execution timed out after %d minute(s)", executeTimeoutFlag)
			}
			if ctx.Err() != nil {
				if opts.showTimeoutDetails {
					fmt.Printf("\nExecution stopped by user.\n")
				} else {
					fmt.Printf("\nPhase %d stopped.\n", phaseIndex+1)
				}
				fmt.Printf("Completed: %d/%d work items\n", result.Completed, result.Total)
				if result.StoppedAt != "" {
					fmt.Printf("Stopped at: %s\n", result.StoppedAt)
				}
				if opts.showTimeoutDetails {
					fmt.Println("\nRun 'utopia execute " + cr.ID + "' to resume.")
				}
				return ctx.Err()
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

		fmt.Printf("\nPhase %d completed successfully! (%d/%d work items)\n", phaseIndex+1, result.Completed, result.Total)
	}

	// All phases complete - show final summary
	fmt.Printf("\nAll phases complete!\n")

	if opts.showPhaseSummary {
		fmt.Printf("\nInitiative progress:\n")
		for i, p := range cr.Phases {
			status := "pending"
			if p.Status != "" {
				status = string(p.Status)
			}
			marker := " "
			if p.Status == domain.PhaseStatusComplete {
				marker = "✓"
			}
			fmt.Printf("  %s [%d] %s (%s)\n", marker, i+1, p.Type, status)
		}
	}

	// Auto-merge: apply CR changes to specs and commit
	if opts.autoMerge {
		fmt.Println()
		fmt.Println("Merging initiative CR into specs...")
		if err := AutoMergeCR(cr, cr.ID, store, projectDir, utopiaDir); err != nil {
			fmt.Printf("\n⚠ Merge failed: %s\n", err)
			fmt.Printf("Work items remain completed. You can retry merge with: utopia merge %s\n", cr.ID)
			return nil // Don't return error - work items completed successfully
		}
	}

	return nil
}

// executeInitiative handles execution for initiative CRs, executing phases in order.
// Phases execute continuously until all complete or the user interrupts with Ctrl+C.
func executeInitiative(cmd *cobra.Command, cr *domain.ChangeRequest, store *storage.YAMLStore, config *domain.Config, projectDir, utopiaDir string, execRegistry *executeStrategy.Registry, chunkReg *chunkStrategy.Registry) error {
	fmt.Printf("Executing initiative: %s\n", cr.Title)
	fmt.Printf("Phases: %d total, current: %d\n", len(cr.Phases), cr.CurrentPhase+1)

	// Check if already complete
	if cr.CurrentPhase >= len(cr.Phases) {
		fmt.Printf("\nAll phases complete!\n")
		fmt.Printf("Run 'utopia merge %s' to finalize the initiative\n", cr.ID)
		return nil
	}

	// Determine which execution strategy to use (once, outside the loop)
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

	// Track session start time
	sessionStart := time.Now()

	// Set up context with optional timeout (shared across all phases)
	var ctx context.Context
	var cancel context.CancelFunc
	if executeTimeoutFlag > 0 {
		ctx, cancel = context.WithTimeout(context.Background(), time.Duration(executeTimeoutFlag)*time.Minute)
	} else {
		ctx, cancel = context.WithCancel(context.Background())
	}
	defer cancel()

	// Handle interrupt signals (shared across all phases)
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigChan
		fmt.Println("\n\nInterrupt received, saving state and stopping...")
		cancel()
	}()

	// Delegate to core function with standalone-mode options
	err := executeInitiativeCore(ctx, cr, store, config, projectDir, utopiaDir, strategy, chunkReg, initiativeCoreOpts{
		showTimeoutDetails: true,
		showPhaseSummary:   true,
		sessionStart:       sessionStart,
		autoMerge:          true,
	})

	// For standalone mode, convert context.Canceled to nil (user interrupted gracefully)
	if err == context.Canceled {
		return nil
	}
	return err
}

// executeSingleInitiative handles execution for initiative CRs within batch mode.
// Similar to executeInitiative but uses the provided context.
func executeSingleInitiative(ctx context.Context, cr *domain.ChangeRequest, store *storage.YAMLStore, config *domain.Config, projectDir, utopiaDir string, execRegistry *executeStrategy.Registry, chunkRegistry *chunkStrategy.Registry) error {
	fmt.Printf("Executing initiative: %s\n", cr.Title)
	fmt.Printf("Phases: %d total, current: %d\n", len(cr.Phases), cr.CurrentPhase+1)

	// Check if already complete
	if cr.CurrentPhase >= len(cr.Phases) {
		fmt.Printf("\nAll phases already complete!\n")
		return nil
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

	// Delegate to core function with batch-mode options
	return executeInitiativeCore(ctx, cr, store, config, projectDir, utopiaDir, strategy, chunkRegistry, initiativeCoreOpts{
		showTimeoutDetails: false,
		showPhaseSummary:   false,
		autoMerge:          true,
	})
}

// chunkPhase invokes the chunking strategy to produce work items for a single phase of an initiative.
func chunkPhase(crID string, phaseIndex int, phase *domain.Phase, store *storage.YAMLStore, config *domain.Config, registry *chunkStrategy.Registry, projectDir string) ([]*domain.WorkItem, error) {
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

	// Configure spec loader if the strategy supports it (needed for bugfix phases)
	if configurable, ok := strategy.(SpecLoaderConfigurable); ok {
		configurable.SetSpecLoader(store.LoadSpec)
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

	// Commit work items to git
	if err := gitCommitChunk(projectDir, crID); err != nil {
		// Log but don't fail - work items are saved, commit is non-critical
		fmt.Printf("⚠ Git commit warning: %s\n", err)
	} else {
		fmt.Printf("✓ Committed work items for %s phase %d\n", crID, phaseIndex+1)
	}

	// Print summary
	fmt.Println("Work items:")
	for _, item := range workItems {
		fmt.Printf("  [%d] %s\n", item.Order, item.ID)
	}
	fmt.Println()

	return workItems, nil
}
