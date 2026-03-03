package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"sort"
	"syscall"
	"time"

	"github.com/leightonvanrooijen/utopia/internal/domain"
	"github.com/leightonvanrooijen/utopia/internal/infra/storage"
	chunkStrategy "github.com/leightonvanrooijen/utopia/internal/strategies/chunk"
	executeStrategy "github.com/leightonvanrooijen/utopia/internal/strategies/execute"
	"github.com/spf13/cobra"
)

// runExecuteAll executes all CRs in .utopia/change-requests/ in alphabetical order.
// If any CR fails, execution stops and reports which CR failed.
func runExecuteAll(cmd *cobra.Command, store *storage.YAMLStore, config *domain.Config, projectDir, utopiaDir string, execRegistry *executeStrategy.Registry, chunkRegistry *chunkStrategy.Registry) error {
	// List all change requests
	crs, err := store.ListChangeRequests()
	if err != nil {
		return fmt.Errorf("failed to list change requests: %w", err)
	}

	if len(crs) == 0 {
		return fmt.Errorf("no change requests found in .utopia/change-requests/\n\nCreate one with: utopia cr")
	}

	// Sort CRs alphabetically by ID (filename)
	sort.Slice(crs, func(i, j int) bool {
		return crs[i].ID < crs[j].ID
	})

	totalCRs := len(crs)
	fmt.Printf("Found %d change request(s) to execute\n\n", totalCRs)

	// Track session start time
	sessionStart := time.Now()

	// Set up shared context for all CRs with optional timeout
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
		fmt.Println("\n\nInterrupt received, stopping batch execution...")
		cancel()
	}()

	// Track completion for summary
	completedCRs := 0

	for i, cr := range crs {
		// Check if context is cancelled (user interrupted or timeout)
		if ctx.Err() != nil {
			if ctx.Err() == context.DeadlineExceeded {
				sessionDuration := time.Since(sessionStart).Round(time.Second)
				fmt.Printf("\n⏱  TIMEOUT REACHED\n")
				fmt.Printf("Session duration: %s\n", sessionDuration)
				fmt.Printf("Completed: %d/%d CRs\n", completedCRs, totalCRs)
				return fmt.Errorf("batch execution timed out after %d minute(s)", executeTimeoutFlag)
			}
			// User interrupted
			fmt.Printf("\n\nBatch execution stopped by user.\n")
			fmt.Printf("Completed: %d/%d CRs\n", completedCRs, totalCRs)
			return nil
		}

		fmt.Printf("════════════════════════════════════════════════════════════════\n")
		fmt.Printf("Executing CR %d of %d: %s\n", i+1, totalCRs, cr.Title)
		fmt.Printf("════════════════════════════════════════════════════════════════\n\n")

		// Execute this CR using the shared context
		if err := executeSingleCR(ctx, cr, store, config, projectDir, utopiaDir, execRegistry, chunkRegistry); err != nil {
			// Check if it was a context cancellation
			if ctx.Err() != nil {
				if ctx.Err() == context.DeadlineExceeded {
					sessionDuration := time.Since(sessionStart).Round(time.Second)
					fmt.Printf("\n⏱  TIMEOUT REACHED\n")
					fmt.Printf("Session duration: %s\n", sessionDuration)
					fmt.Printf("Completed: %d/%d CRs\n", completedCRs, totalCRs)
					return fmt.Errorf("batch execution timed out after %d minute(s)", executeTimeoutFlag)
				}
				// User interrupted
				fmt.Printf("\n\nBatch execution stopped by user.\n")
				fmt.Printf("Completed: %d/%d CRs\n", completedCRs, totalCRs)
				return nil
			}

			// Actual CR failure - stop batch execution
			fmt.Printf("\n════════════════════════════════════════════════════════════════\n")
			fmt.Printf("BATCH EXECUTION FAILED\n")
			fmt.Printf("════════════════════════════════════════════════════════════════\n")
			fmt.Printf("Failed CR: %s (%s)\n", cr.Title, cr.ID)
			fmt.Printf("Error: %s\n", err)
			fmt.Printf("Completed: %d/%d CRs before failure\n", completedCRs, totalCRs)
			fmt.Printf("\nFix the issue and run 'utopia execute --all' to resume.\n")
			return fmt.Errorf("CR %q failed: %w", cr.ID, err)
		}

		completedCRs++
		fmt.Printf("\n✓ CR %d of %d completed: %s\n\n", i+1, totalCRs, cr.Title)
	}

	// Success summary
	sessionDuration := time.Since(sessionStart).Round(time.Second)
	fmt.Printf("════════════════════════════════════════════════════════════════\n")
	fmt.Printf("BATCH EXECUTION COMPLETE\n")
	fmt.Printf("════════════════════════════════════════════════════════════════\n")
	fmt.Printf("Successfully executed: %d/%d CRs\n", completedCRs, totalCRs)
	fmt.Printf("Total duration: %s\n", sessionDuration)

	return nil
}

// executeSingleCR executes a single CR with the given context.
// This is extracted from runExecute to allow reuse in batch execution.
func executeSingleCR(ctx context.Context, cr *domain.ChangeRequest, store *storage.YAMLStore, config *domain.Config, projectDir, utopiaDir string, execRegistry *executeStrategy.Registry, chunkRegistry *chunkStrategy.Registry) error {
	crID := cr.ID

	// Check if this is an initiative CR (needs per-phase execution)
	if cr.Type == domain.CRTypeInitiative {
		return executeSingleInitiative(ctx, cr, store, config, projectDir, utopiaDir, execRegistry, chunkRegistry)
	}

	// Check if work items already exist for this CR
	items, err := store.ListWorkItemsForSpec(crID)
	if err != nil {
		return fmt.Errorf("failed to load work items: %w", err)
	}

	// If no work items exist, chunk the CR first
	if len(items) == 0 {
		items, err = chunkCR(cr, crID, store, config, chunkRegistry, projectDir)
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
	fmt.Printf("Executing CR: %s (%d work items)\n\n", crID, len(items))

	// Run the strategy with the provided context
	result, err := strategy.Execute(ctx, crID, store, config, projectDir)
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded || ctx.Err() == context.Canceled {
			// Context error - let caller handle
			fmt.Printf("\nExecution stopped.\n")
			fmt.Printf("Completed: %d/%d work items\n", result.Completed, result.Total)
			if result.StoppedAt != "" {
				fmt.Printf("Stopped at: %s\n", result.StoppedAt)
			}
			return ctx.Err()
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

	// Auto-merge
	fmt.Println()
	fmt.Println("Merging CR into specs...")
	if err := AutoMergeCR(cr, crID, store, projectDir, utopiaDir); err != nil {
		fmt.Printf("\n⚠ Merge failed: %s\n", err)
		fmt.Printf("Work items remain completed. You can retry merge with: utopia merge %s\n", crID)
		return nil // Don't fail batch for merge errors - work is done
	}

	return nil
}
