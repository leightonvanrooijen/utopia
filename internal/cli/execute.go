package cli

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/leightonvanrooijen/utopia/internal"
	"github.com/leightonvanrooijen/utopia/internal/chunk"
	"github.com/leightonvanrooijen/utopia/internal/cli/ui"
	"github.com/leightonvanrooijen/utopia/internal/domain"
	"github.com/leightonvanrooijen/utopia/internal/git"
	"github.com/leightonvanrooijen/utopia/internal/ralph"
	"github.com/spf13/cobra"
)

var (
	executeTimeoutFlag int
	executeAllFlag     bool
	executeModelFlag   string
)

func InitExecuteCmd() {
	rootCmd.AddCommand(NewExecuteCmd())
}

func NewExecuteCmd() *cobra.Command {
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

Press Ctrl+C to gracefully stop execution (current state will be saved).
Run the command again to resume from where you left off.`,
		Args: cobra.MaximumNArgs(1),
		RunE: runExecute,
	}
	cmd.Flags().IntVarP(&executeTimeoutFlag, "timeout", "t", 0, "timeout in minutes (0 means no timeout)")
	cmd.Flags().BoolVar(&executeAllFlag, "all", false, "execute all CRs in .utopia/change-requests/ in alphabetical order")
	cmd.Flags().StringVar(&executeModelFlag, "model", "", "model to use (haiku, sonnet, opus)")
	return cmd
}

func runExecute(cmd *cobra.Command, args []string) error {
	out := ui.NewPrinter(cmd.OutOrStdout(), cmd.ErrOrStderr())

	// Validate model flag early before any work
	modelID, err := ResolveModelFlag(cmd)
	if err != nil {
		return err
	}

	if executeTimeoutFlag < 0 {
		return fmt.Errorf("invalid timeout value: %d (must be a positive integer)", executeTimeoutFlag)
	}

	absPath, utopiaDir, store, err := ResolveProject(cmd)
	if err != nil {
		return err
	}
	config, err := store.LoadConfig()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	if executeAllFlag {
		if len(args) > 0 {
			return fmt.Errorf("cannot specify CR ID with --all flag")
		}
		return runExecuteAll(out, store, config, absPath, utopiaDir, modelID)
	}

	var crID string
	if len(args) > 0 {
		crID = args[0]
	} else {
		selectedID, err := selectChangeRequest(out, store)
		if err != nil {
			return err
		}
		crID = selectedID
	}

	cr, crErr := store.LoadChangeRequest(crID)
	if crErr != nil {
		return &domain.NotFoundError{Resource: "change request", ID: crID}
	}

	if cr.Type == domain.CRTypeInitiative {
		return executeInitiative(out, cr, store, config, absPath, utopiaDir, modelID)
	}

	items, err := store.ListWorkItemsForSpec(crID)
	if err != nil {
		return fmt.Errorf("failed to load work items: %w", err)
	}
	if len(items) == 0 {
		items, err = chunkCR(out, cr, crID, store, config, absPath)
		if err != nil {
			return err
		}
	} else {
		out.Progressf("Found %d existing work item(s) for %s\n", len(items), crID)
	}

	out.Progressf("Executing CR: %s (%d work items)\n", crID, len(items))
	if executeTimeoutFlag > 0 {
		out.Progressf("Timeout: %d minute(s)\n", executeTimeoutFlag)
	}
	out.Progressf("\n")

	sessionStart := time.Now()
	var ctx context.Context
	var cancel context.CancelFunc
	if executeTimeoutFlag > 0 {
		ctx, cancel = context.WithTimeout(context.Background(), time.Duration(executeTimeoutFlag)*time.Minute)
	} else {
		ctx, cancel = context.WithCancel(context.Background())
	}
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigChan
		out.Progressf("\n\nInterrupt received, saving state and stopping...\n")
		cancel()
	}()

	result, err := ralph.Execute(ctx, crID, store, config, absPath, modelID)
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			sessionDuration := time.Since(sessionStart).Round(time.Second)
			out.Progressf("\n  TIMEOUT REACHED\n")
			out.Progressf("Session duration: %s\n", sessionDuration)
			out.Progressf("Completed: %d/%d work items\n", result.Completed, result.Total)
			if result.StoppedAt != "" {
				out.Progressf("Stopped at: %s\n", result.StoppedAt)
			}
			out.Progressf("\nProgress has been saved. Run 'utopia execute %s' to resume from where you left off.\n", crID)
			return fmt.Errorf("execution timed out after %d minute(s)", executeTimeoutFlag)
		}
		if ctx.Err() == context.Canceled {
			out.Progressf("\nExecution stopped by user.\n")
			out.Progressf("Completed: %d/%d work items\n", result.Completed, result.Total)
			if result.StoppedAt != "" {
				out.Progressf("Stopped at: %s\n", result.StoppedAt)
			}
			out.Progressf("\nRun 'utopia execute %s' to resume from where you left off.\n", crID)
			return nil
		}
		out.Progressf("\nExecution stopped: %s\n", err)
		out.Progressf("Completed: %d/%d work items\n", result.Completed, result.Total)
		if result.StoppedAt != "" {
			out.Progressf("Stopped at: %s\n", result.StoppedAt)
		}
		return fmt.Errorf("CR %q failed: %w", crID, err)
	}

	out.Printf("\nAll work items completed successfully!\n")
	out.Printf("Completed: %d/%d work items\n", result.Completed, result.Total)
	out.Progressf("\nMerging CR into specs...\n")
	if err := AutoMergeCR(out, cr, crID, store, absPath, utopiaDir); err != nil {
		out.Progressf("\n  Merge failed: %s\n", err)
		out.Progressf("Work items remain completed. You can retry merge with: utopia merge %s\n", crID)
		return nil
	}
	return nil
}

// ============================================================================
// BATCH EXECUTION
// ============================================================================

func runExecuteAll(out *ui.Printer, store *internal.YAMLStore, config *domain.Config, projectDir, utopiaDir, modelID string) error {
	crs, err := store.ListChangeRequests()
	if err != nil {
		return fmt.Errorf("failed to list change requests: %w", err)
	}
	if len(crs) == 0 {
		return fmt.Errorf("no change requests found in .utopia/change-requests/\n\nCreate one with: utopia cr")
	}

	sort.Slice(crs, func(i, j int) bool { return crs[i].ID < crs[j].ID })
	totalCRs := len(crs)
	out.Progressf("Found %d change request(s) to execute\n\n", totalCRs)

	sessionStart := time.Now()
	var ctx context.Context
	var cancel context.CancelFunc
	if executeTimeoutFlag > 0 {
		ctx, cancel = context.WithTimeout(context.Background(), time.Duration(executeTimeoutFlag)*time.Minute)
	} else {
		ctx, cancel = context.WithCancel(context.Background())
	}
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigChan
		out.Progressf("\n\nInterrupt received, stopping batch execution...\n")
		cancel()
	}()

	completedCRs := 0
	for i, cr := range crs {
		if ctx.Err() != nil {
			if ctx.Err() == context.DeadlineExceeded {
				sessionDuration := time.Since(sessionStart).Round(time.Second)
				out.Progressf("\n  TIMEOUT REACHED\n")
				out.Progressf("Session duration: %s\n", sessionDuration)
				out.Progressf("Completed: %d/%d CRs\n", completedCRs, totalCRs)
				return fmt.Errorf("batch execution timed out after %d minute(s)", executeTimeoutFlag)
			}
			out.Progressf("\n\nBatch execution stopped by user.\n")
			out.Progressf("Completed: %d/%d CRs\n", completedCRs, totalCRs)
			return nil
		}

		out.Progressf("================================================================\n")
		out.Progressf("Executing CR %d of %d: %s\n", i+1, totalCRs, cr.Title)
		out.Progressf("================================================================\n\n")

		if err := executeSingleCR(ctx, out, cr, store, config, projectDir, utopiaDir, modelID); err != nil {
			if ctx.Err() != nil {
				if ctx.Err() == context.DeadlineExceeded {
					sessionDuration := time.Since(sessionStart).Round(time.Second)
					out.Progressf("\n  TIMEOUT REACHED\n")
					out.Progressf("Session duration: %s\n", sessionDuration)
					out.Progressf("Completed: %d/%d CRs\n", completedCRs, totalCRs)
					return fmt.Errorf("batch execution timed out after %d minute(s)", executeTimeoutFlag)
				}
				out.Progressf("\n\nBatch execution stopped by user.\n")
				out.Progressf("Completed: %d/%d CRs\n", completedCRs, totalCRs)
				return nil
			}
			out.Progressf("\n================================================================\n")
			out.Progressf("BATCH EXECUTION FAILED\n")
			out.Progressf("================================================================\n")
			out.Progressf("Failed CR: %s (%s)\n", cr.Title, cr.ID)
			out.Progressf("Error: %s\n", err)
			out.Progressf("Completed: %d/%d CRs before failure\n", completedCRs, totalCRs)
			out.Progressf("\nFix the issue and run 'utopia execute --all' to resume.\n")
			return fmt.Errorf("CR %q failed: %w", cr.ID, err)
		}

		completedCRs++
		out.Progressf("\n  CR %d of %d completed: %s\n\n", i+1, totalCRs, cr.Title)
	}

	sessionDuration := time.Since(sessionStart).Round(time.Second)
	out.Printf("================================================================\n")
	out.Printf("BATCH EXECUTION COMPLETE\n")
	out.Printf("================================================================\n")
	out.Printf("Successfully executed: %d/%d CRs\n", completedCRs, totalCRs)
	out.Printf("Total duration: %s\n", sessionDuration)
	return nil
}

func executeSingleCR(ctx context.Context, out *ui.Printer, cr *domain.ChangeRequest, store *internal.YAMLStore, config *domain.Config, projectDir, utopiaDir, modelID string) error {
	crID := cr.ID
	if cr.Type == domain.CRTypeInitiative {
		return executeSingleInitiative(ctx, out, cr, store, config, projectDir, utopiaDir, modelID)
	}

	items, err := store.ListWorkItemsForSpec(crID)
	if err != nil {
		return fmt.Errorf("failed to load work items: %w", err)
	}
	if len(items) == 0 {
		items, err = chunkCR(out, cr, crID, store, config, projectDir)
		if err != nil {
			return err
		}
	} else {
		out.Progressf("Found %d existing work item(s) for %s\n", len(items), crID)
	}

	out.Progressf("Executing CR: %s (%d work items)\n\n", crID, len(items))
	result, err := ralph.Execute(ctx, crID, store, config, projectDir, modelID)
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded || ctx.Err() == context.Canceled {
			out.Progressf("\nExecution stopped.\n")
			out.Progressf("Completed: %d/%d work items\n", result.Completed, result.Total)
			if result.StoppedAt != "" {
				out.Progressf("Stopped at: %s\n", result.StoppedAt)
			}
			return ctx.Err()
		}
		out.Progressf("\nExecution stopped: %s\n", err)
		out.Progressf("Completed: %d/%d work items\n", result.Completed, result.Total)
		if result.StoppedAt != "" {
			out.Progressf("Stopped at: %s\n", result.StoppedAt)
		}
		return err
	}

	out.Printf("\nAll work items completed successfully!\n")
	out.Printf("Completed: %d/%d work items\n", result.Completed, result.Total)
	out.Progressf("\nMerging CR into specs...\n")
	if err := AutoMergeCR(out, cr, crID, store, projectDir, utopiaDir); err != nil {
		out.Progressf("\n  Merge failed: %s\n", err)
		out.Progressf("Work items remain completed. You can retry merge with: utopia merge %s\n", crID)
		return nil
	}
	return nil
}

// ============================================================================
// INITIATIVE EXECUTION
// ============================================================================

func executeInitiative(out *ui.Printer, cr *domain.ChangeRequest, store *internal.YAMLStore, config *domain.Config, projectDir, utopiaDir, modelID string) error {
	out.Progressf("Executing initiative: %s\n", cr.Title)
	out.Progressf("Phases: %d total, current: %d\n", len(cr.Phases), cr.CurrentPhase+1)

	if cr.CurrentPhase >= len(cr.Phases) {
		out.Printf("\nAll phases complete!\n")
		out.Printf("Run 'utopia merge %s' to finalize the initiative\n", cr.ID)
		return nil
	}

	sessionStart := time.Now()
	var ctx context.Context
	var cancel context.CancelFunc
	if executeTimeoutFlag > 0 {
		ctx, cancel = context.WithTimeout(context.Background(), time.Duration(executeTimeoutFlag)*time.Minute)
	} else {
		ctx, cancel = context.WithCancel(context.Background())
	}
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigChan
		out.Progressf("\n\nInterrupt received, saving state and stopping...\n")
		cancel()
	}()

	err := executeInitiativeCore(ctx, out, cr, store, config, projectDir, utopiaDir, true, true, sessionStart, true, modelID)
	if err == context.Canceled {
		return nil
	}
	return err
}

func executeSingleInitiative(ctx context.Context, out *ui.Printer, cr *domain.ChangeRequest, store *internal.YAMLStore, config *domain.Config, projectDir, utopiaDir, modelID string) error {
	out.Progressf("Executing initiative: %s\n", cr.Title)
	out.Progressf("Phases: %d total, current: %d\n", len(cr.Phases), cr.CurrentPhase+1)

	if cr.CurrentPhase >= len(cr.Phases) {
		out.Printf("\nAll phases already complete!\n")
		return nil
	}
	return executeInitiativeCore(ctx, out, cr, store, config, projectDir, utopiaDir, false, false, time.Time{}, true, modelID)
}

func executeInitiativeCore(
	ctx context.Context, out *ui.Printer, cr *domain.ChangeRequest, store *internal.YAMLStore, config *domain.Config,
	projectDir, utopiaDir string, showTimeoutDetails, showPhaseSummary bool, sessionStart time.Time, autoMerge bool, modelID string,
) error {
	for cr.CurrentPhase < len(cr.Phases) {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		phaseIndex := cr.CurrentPhase
		phase := cr.Phases[phaseIndex]
		phaseWorkDir := fmt.Sprintf("%s/phase-%d", cr.ID, phaseIndex)

		items, err := store.ListWorkItemsForSpec(phaseWorkDir)
		if err != nil {
			return fmt.Errorf("failed to load work items: %w", err)
		}

		if len(items) == 0 {
			cr.Phases[phaseIndex].Status = domain.PhaseStatusInProgress
			if cr.Status != domain.ChangeRequestInProgress {
				cr.Status = domain.ChangeRequestInProgress
			}
			if err := store.SaveChangeRequest(cr); err != nil {
				return fmt.Errorf("failed to update CR status: %w", err)
			}
			items, err = chunkPhase(out, cr.ID, phaseIndex, &phase, store, config, projectDir)
			if err != nil {
				return err
			}
		} else {
			out.Progressf("Found %d existing work item(s) for phase %d\n", len(items), phaseIndex+1)
		}

		out.Progressf("\nExecuting phase %d/%d (type: %s)\n", phaseIndex+1, len(cr.Phases), phase.Type)
		out.Progressf("Work items: %d\n", len(items))
		if showTimeoutDetails && executeTimeoutFlag > 0 {
			out.Progressf("Timeout: %d minute(s)\n", executeTimeoutFlag)
		}
		out.Progressf("\n")

		result, err := ralph.Execute(ctx, phaseWorkDir, store, config, projectDir, modelID)
		if err != nil {
			if showTimeoutDetails && ctx.Err() == context.DeadlineExceeded {
				sessionDuration := time.Since(sessionStart).Round(time.Second)
				out.Progressf("\n  TIMEOUT REACHED\n")
				out.Progressf("Session duration: %s\n", sessionDuration)
				out.Progressf("Phase %d completed: %d/%d work items\n", phaseIndex+1, result.Completed, result.Total)
				if result.StoppedAt != "" {
					out.Progressf("Stopped at: %s\n", result.StoppedAt)
				}
				out.Progressf("\nProgress saved. Run 'utopia execute %s' to resume.\n", cr.ID)
				return fmt.Errorf("execution timed out after %d minute(s)", executeTimeoutFlag)
			}
			if ctx.Err() != nil {
				if showTimeoutDetails {
					out.Progressf("\nExecution stopped by user.\n")
				} else {
					out.Progressf("\nPhase %d stopped.\n", phaseIndex+1)
				}
				out.Progressf("Completed: %d/%d work items\n", result.Completed, result.Total)
				if result.StoppedAt != "" {
					out.Progressf("Stopped at: %s\n", result.StoppedAt)
				}
				if showTimeoutDetails {
					out.Progressf("\nRun 'utopia execute %s' to resume.\n", cr.ID)
				}
				return ctx.Err()
			}
			out.Progressf("\nExecution stopped: %s\n", err)
			out.Progressf("Phase %d completed: %d/%d work items\n", phaseIndex+1, result.Completed, result.Total)
			if result.StoppedAt != "" {
				out.Progressf("Stopped at: %s\n", result.StoppedAt)
			}
			return fmt.Errorf("phase %d of CR %q failed: %w", phaseIndex+1, cr.ID, err)
		}

		cr.Phases[phaseIndex].Status = domain.PhaseStatusComplete
		cr.CurrentPhase = phaseIndex + 1
		if err := store.SaveChangeRequest(cr); err != nil {
			return fmt.Errorf("failed to update CR status: %w", err)
		}
		out.Progressf("\nPhase %d completed successfully! (%d/%d work items)\n", phaseIndex+1, result.Completed, result.Total)
	}

	out.Printf("\nAll phases complete!\n")
	if showPhaseSummary {
		out.Printf("\nInitiative progress:\n")
		for i, p := range cr.Phases {
			status := "pending"
			if p.Status != "" {
				status = string(p.Status)
			}
			out.Printf("  [%d] %s (%s)\n", i+1, p.Type, status)
		}
	}

	if autoMerge {
		out.Progressf("\nMerging initiative CR into specs...\n")
		if err := AutoMergeCR(out, cr, cr.ID, store, projectDir, utopiaDir); err != nil {
			out.Progressf("\n  Merge failed: %s\n", err)
			out.Progressf("Work items remain completed. You can retry merge with: utopia merge %s\n", cr.ID)
			return nil
		}
	}
	return nil
}

// ============================================================================
// CHUNKING
// ============================================================================

func chunkCR(out *ui.Printer, cr *domain.ChangeRequest, crID string, store *internal.YAMLStore, config *domain.Config, projectDir string) ([]*domain.WorkItem, error) {
	out.Progressf("Chunking change request: %s\n", cr.Title)

	cr.Status = domain.ChangeRequestInProgress
	if err := store.SaveChangeRequest(cr); err != nil {
		return nil, fmt.Errorf("failed to update CR status: %w", err)
	}

	workItems, err := chunk.Chunk(cr, store.LoadSpec, store.LoadStandardsIndex())
	if err != nil {
		return nil, fmt.Errorf("chunking failed: %w", err)
	}

	for _, item := range workItems {
		if err := store.SaveWorkItemForSpec(crID, item); err != nil {
			return nil, fmt.Errorf("failed to save work item %s: %w", item.ID, err)
		}
	}

	out.Progressf("Created %d work item(s)\n\n", len(workItems))
	if err := gitCommitChunk(projectDir, crID); err != nil {
		out.Progressf("  Git commit warning: %s\n", err)
	} else {
		out.Progressf("  Committed work items for %s\n", crID)
	}

	out.Progressf("Work items:\n")
	for _, item := range workItems {
		out.Progressf("  [%d] %s\n", item.Order, item.ID)
	}
	out.Progressf("\n")
	return workItems, nil
}

func chunkPhase(out *ui.Printer, crID string, phaseIndex int, phase *domain.Phase, store *internal.YAMLStore, config *domain.Config, projectDir string) ([]*domain.WorkItem, error) {
	out.Progressf("Chunking phase %d (type: %s)\n", phaseIndex+1, phase.Type)

	workItems, err := chunk.ChunkPhase(crID, phaseIndex, phase, store.LoadSpec, store.LoadStandardsIndex())
	if err != nil {
		return nil, fmt.Errorf("chunking failed: %w", err)
	}

	phaseWorkDir := fmt.Sprintf("%s/phase-%d", crID, phaseIndex)
	for _, item := range workItems {
		if err := store.SaveWorkItemForSpec(phaseWorkDir, item); err != nil {
			return nil, fmt.Errorf("failed to save work item %s: %w", item.ID, err)
		}
	}

	out.Progressf("Created %d work item(s)\n\n", len(workItems))
	if err := gitCommitChunk(projectDir, crID); err != nil {
		out.Progressf("  Git commit warning: %s\n", err)
	} else {
		out.Progressf("  Committed work items for %s phase %d\n", crID, phaseIndex+1)
	}

	out.Progressf("Work items:\n")
	for _, item := range workItems {
		out.Progressf("  [%d] %s\n", item.Order, item.ID)
	}
	out.Progressf("\n")
	return workItems, nil
}

// ============================================================================
// CR SELECTION
// ============================================================================

func selectChangeRequest(out *ui.Printer, store *internal.YAMLStore) (string, error) {
	crs, err := store.ListChangeRequests()
	if err != nil {
		return "", fmt.Errorf("failed to list change requests: %w", err)
	}
	if len(crs) == 0 {
		return "", fmt.Errorf("no change requests found in .utopia/change-requests/\n\nCreate one with: utopia cr")
	}

	out.Printf("Available change requests:\n\n")
	for i, cr := range crs {
		out.Printf("  [%d] %s\n", i+1, cr.Title)
	}
	out.Printf("\n")

	reader := bufio.NewReader(os.Stdin)
	out.Printf("Select a change request (number): ")
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
	out.Printf("\nSelected: %s\n\n", selectedCR.Title)
	return selectedCR.ID, nil
}

// ============================================================================
// GIT OPERATIONS
// ============================================================================

func gitCommitChunk(projectDir, crID string) error {
	workItemsDir := filepath.Join(projectDir, ".utopia", "work-items", crID)
	msg := fmt.Sprintf("chunk: %s", crID)
	return git.CommitIfChanged(projectDir, msg, workItemsDir)
}

func gitCommitCleanup(projectDir, crID, utopiaDir string) error {
	workItemsDir := filepath.Join(utopiaDir, "work-items", crID)
	crFile := filepath.Join(utopiaDir, "change-requests", crID+".yaml")
	msg := fmt.Sprintf("cleanup: complete %s", crID)
	return git.CommitIfChanged(projectDir, msg, workItemsDir, crFile)
}

func GitCommitCR(projectDir, crID string) (string, error) {
	crFile := filepath.Join(projectDir, ".utopia", "change-requests", crID+".yaml")
	if err := git.Add(projectDir, crFile); err != nil {
		return "", fmt.Errorf("failed to stage CR file %s: %w", crFile, err)
	}
	if !git.HasStagedChanges(projectDir) {
		return "", nil
	}
	msg := fmt.Sprintf("cr: create %s", crID)
	if err := git.Commit(projectDir, msg); err != nil {
		return "", fmt.Errorf("failed to commit CR %s: %w", crID, err)
	}
	return git.HeadSHA(projectDir)
}

func GitCommitSpecMerge(projectDir string, cr *domain.ChangeRequest, mergeResult *MergeResult) error {
	var msg string
	if mergeResult.IsRefactor {
		msg = fmt.Sprintf("spec: merge refactor CR '%s'\n\nNo spec modifications (refactor only).", cr.Title)
	} else {
		msg = fmt.Sprintf("spec: merge CR '%s'", cr.Title)
		if len(mergeResult.SpecsModified) > 0 || len(mergeResult.SpecsDeleted) > 0 {
			msg += "\n\nModified specs:"
			for _, s := range mergeResult.SpecsModified {
				msg += fmt.Sprintf("\n  - %s", s)
			}
			for _, s := range mergeResult.SpecsDeleted {
				msg += fmt.Sprintf("\n  - %s (deleted)", s)
			}
		}
	}

	specsDir := filepath.Join(projectDir, ".utopia", "specs")
	return git.CommitIfChanged(projectDir, msg, specsDir)
}
