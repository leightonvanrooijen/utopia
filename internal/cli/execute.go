package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/leightonvanrooijen/utopia/internal/infra/storage"
	executeStrategy "github.com/leightonvanrooijen/utopia/internal/strategies/execute"
	"github.com/spf13/cobra"
)

var (
	executeStrategyFlag string
	executeTimeoutFlag  int
	executeRegistry     *executeStrategy.Registry
)

var executeCmd = &cobra.Command{
	Use:   "execute <spec-id>",
	Short: "Execute work items for a spec",
	Long: `Execute work items for a specification using the Ralph loop.

The execution strategy determines how work items are processed:
  - sequential: Execute one at a time, in order, with retry on failure

Work items are loaded from .utopia/work-items/<spec-id>/ and executed
until all are completed or max iterations is reached.

Press Ctrl+C to gracefully stop execution (current state will be saved).`,
	Args: cobra.ExactArgs(1),
	RunE: runExecute,
}

func init() {
	rootCmd.AddCommand(executeCmd)

	executeCmd.Flags().StringVarP(&executeStrategyFlag, "strategy", "s", "",
		"execution strategy (sequential)")
	executeCmd.Flags().IntVarP(&executeTimeoutFlag, "timeout", "t", 0,
		"timeout in minutes (0 means no timeout)")

	// Initialize registry - strategies will be registered at startup
	executeRegistry = executeStrategy.NewRegistry()
}

// RegisterExecuteStrategy adds a strategy to the registry (called from main)
func RegisterExecuteStrategy(s executeStrategy.Strategy) {
	executeRegistry.Register(s)
}

func runExecute(cmd *cobra.Command, args []string) error {
	specID := args[0]
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

	// Check that work items exist for this spec
	items, err := store.ListWorkItemsForSpec(specID)
	if err != nil {
		return fmt.Errorf("failed to load work items: %w", err)
	}
	if len(items) == 0 {
		return fmt.Errorf("no work items found for spec %q\n\nRun 'utopia chunk %s' first", specID, specID)
	}

	// Determine which strategy to use
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
	fmt.Printf("Executing spec: %s (%d work items)\n", specID, len(items))
	if executeTimeoutFlag > 0 {
		fmt.Printf("Timeout: %d minute(s)\n", executeTimeoutFlag)
	}
	fmt.Println()

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
	result, err := strategy.Execute(ctx, specID, store, config, absPath)
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			// Timeout reached
			fmt.Printf("\nExecution timed out after %d minute(s).\n", executeTimeoutFlag)
			fmt.Printf("Completed: %d/%d work items\n", result.Completed, result.Total)
			if result.StoppedAt != "" {
				fmt.Printf("Stopped at: %s\n", result.StoppedAt)
			}
			fmt.Println("\nRun 'utopia execute " + specID + "' to resume from where you left off.")
			return fmt.Errorf("execution timed out after %d minute(s)", executeTimeoutFlag)
		}
		if ctx.Err() == context.Canceled {
			// Interrupted by user
			fmt.Printf("\nExecution stopped by user.\n")
			fmt.Printf("Completed: %d/%d work items\n", result.Completed, result.Total)
			if result.StoppedAt != "" {
				fmt.Printf("Stopped at: %s\n", result.StoppedAt)
			}
			fmt.Println("\nRun 'utopia execute " + specID + "' to resume from where you left off.")
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
