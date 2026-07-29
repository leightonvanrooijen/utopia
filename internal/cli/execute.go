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
	executeAuthFlag    string
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

Use --all to execute all CRs in .utopia/change-requests/. CRs run in filename
order: a leading numeric prefix (e.g. "1_", "2_") controls the sequence and is
compared numerically, so "2_" runs before "10_" regardless of zero-padding. CRs
without a numeric prefix run after all prefixed CRs, in alphabetical order.
If any CR fails, execution stops and reports which CR failed.

Press Ctrl+C to gracefully stop execution (current state will be saved).
Run the command again to resume from where you left off.`,
		Args: cobra.MaximumNArgs(1),
		RunE: runExecute,
	}
	cmd.Flags().IntVarP(&executeTimeoutFlag, "timeout", "t", 0, "timeout in minutes (0 means no timeout)")
	cmd.Flags().BoolVar(&executeAllFlag, "all", false, "execute all CRs in .utopia/change-requests/ in filename order (leading numeric prefix controls the sequence)")
	cmd.Flags().StringVar(&executeModelFlag, "model", "", "model alias (haiku, sonnet, opus, fable) or a full model identifier")
	cmd.Flags().StringVar(&executeAuthFlag, "auth", "", "credential to use (api-key, subscription), overriding config.auth.mode")
	cmd.AddCommand(newExecuteRunCmd())
	return cmd
}

func runExecute(cmd *cobra.Command, args []string) error {
	out := ui.NewPrinter(cmd.OutOrStdout(), cmd.ErrOrStderr())

	// Validate model flag early before any work
	modelID, err := ResolveModelFlag(cmd)
	if err != nil {
		return err
	}

	// Resolve and report the credential this invocation runs with, before any
	// work starts
	authMode, err := ResolveAuth(cmd)
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
		return runExecuteAll(out, store, config, absPath, utopiaDir, modelID, authMode)
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

	cr, err := store.ResolveChangeRequest(crID)
	if err != nil {
		return err
	}
	// Pivot from the requested name to the CR's internal id: work items,
	// chunking, execution, and merge all key off the id, so a prefixed file
	// (06_ai-chat.yaml, id ai-chat) always uses work-items/ai-chat/ - and is
	// not re-chunked - whether it was run as "ai-chat" or "06_ai-chat".
	crID = cr.ID

	if cr.Type == domain.CRTypeInitiative {
		return executeInitiative(out, cr, store, config, absPath, utopiaDir, modelID, authMode)
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

	result, err := ralph.Execute(ctx, crID, store, config, absPath, authMode, modelID)
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

// lessCRExecutionOrder is the batch execution ordering rule: CRs run in
// filename order, where a leading numeric prefix on the filename (e.g. "1_",
// "2_") controls the sequence and is compared numerically ("2_" before "10_"
// regardless of zero-padding). CRs without a numeric filename prefix run after
// all prefixed CRs, in alphabetical filename order.
//
// It reads the basename, never cr.ID: the ordering prefix lives on the filename
// while the internal id stays clean (01_reusable-core.yaml holds
// id: reusable-core), so sorting on the id sees no prefix at all and silently
// degrades to alphabetical id order. The id remains the key for work items,
// progress output and commit messages - only sequencing is filename-driven.
//
// Kept as a pure function so the ordering policy is testable in isolation from
// the batch loop.
func lessCRExecutionOrder(a, b internal.ChangeRequestFile) bool {
	na, _, oka := internal.NumericFilenamePrefix(a.Basename)
	nb, _, okb := internal.NumericFilenamePrefix(b.Basename)
	switch {
	case oka && okb:
		if na != nb {
			return na < nb
		}
		return a.Basename < b.Basename // same sequence number: stable alphabetical tie-break
	case oka != okb:
		return oka // prefixed CRs run before non-prefixed ones
	default:
		return a.Basename < b.Basename // neither prefixed: alphabetical
	}
}

func runExecuteAll(out *ui.Printer, store *internal.YAMLStore, config *domain.Config, projectDir, utopiaDir, modelID string, authMode domain.AuthMode) error {
	// The filenames come back alongside the CRs because they, not the internal
	// ids, carry the ordering prefix.
	crFiles, err := store.ListChangeRequestFiles()
	if err != nil {
		return fmt.Errorf("failed to list change requests: %w", err)
	}
	if len(crFiles) == 0 {
		return fmt.Errorf("no change requests found in .utopia/change-requests/\n\nCreate one with: utopia cr")
	}

	sort.Slice(crFiles, func(i, j int) bool { return lessCRExecutionOrder(crFiles[i], crFiles[j]) })
	totalCRs := len(crFiles)
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
	for i, crFile := range crFiles {
		// Sequencing used the filename; everything downstream - work item keying,
		// progress output, commit messages - keys off the CR's internal id.
		cr := crFile.CR
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

		if err := executeSingleCR(ctx, out, cr, store, config, projectDir, utopiaDir, modelID, authMode); err != nil {
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

func executeSingleCR(ctx context.Context, out *ui.Printer, cr *domain.ChangeRequest, store *internal.YAMLStore, config *domain.Config, projectDir, utopiaDir, modelID string, authMode domain.AuthMode) error {
	crID := cr.ID
	if cr.Type == domain.CRTypeInitiative {
		return executeSingleInitiative(ctx, out, cr, store, config, projectDir, utopiaDir, modelID, authMode)
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
	result, err := ralph.Execute(ctx, crID, store, config, projectDir, authMode, modelID)
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

func executeInitiative(out *ui.Printer, cr *domain.ChangeRequest, store *internal.YAMLStore, config *domain.Config, projectDir, utopiaDir, modelID string, authMode domain.AuthMode) error {
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

	err := executeInitiativeCore(ctx, out, cr, store, config, projectDir, utopiaDir, true, true, sessionStart, true, modelID, authMode)
	if err == context.Canceled {
		return nil
	}
	return err
}

func executeSingleInitiative(ctx context.Context, out *ui.Printer, cr *domain.ChangeRequest, store *internal.YAMLStore, config *domain.Config, projectDir, utopiaDir, modelID string, authMode domain.AuthMode) error {
	out.Progressf("Executing initiative: %s\n", cr.Title)
	out.Progressf("Phases: %d total, current: %d\n", len(cr.Phases), cr.CurrentPhase+1)

	if cr.CurrentPhase >= len(cr.Phases) {
		out.Printf("\nAll phases already complete!\n")
		return nil
	}
	return executeInitiativeCore(ctx, out, cr, store, config, projectDir, utopiaDir, false, false, time.Time{}, true, modelID, authMode)
}

func executeInitiativeCore(
	ctx context.Context, out *ui.Printer, cr *domain.ChangeRequest, store *internal.YAMLStore, config *domain.Config,
	projectDir, utopiaDir string, showTimeoutDetails, showPhaseSummary bool, sessionStart time.Time, autoMerge bool, modelID string,
	authMode domain.AuthMode,
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

		result, err := ralph.Execute(ctx, phaseWorkDir, store, config, projectDir, authMode, modelID)
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

// gitCommitCleanup stages the post-merge removal of a CR and its work items as
// one commit. crFile is the CR's real on-disk path, resolved by the caller via
// store.ChangeRequestPath before deletion, so a CR saved under a numeric
// ordering prefix (.utopia/change-requests/01_reusable-core.yaml) has its
// removal staged - a path reconstructed as change-requests/<id>.yaml would not
// match the tracked file and the deletion would silently never be committed.
// Work items always live under the internal id, so that path is reconstructed.
// The commit message stays keyed to the internal id.
func gitCommitCleanup(projectDir, crFile, crID, utopiaDir string) error {
	workItemsDir := filepath.Join(utopiaDir, "work-items", crID)
	msg := fmt.Sprintf("cleanup: complete %s", crID)
	return git.CommitIfChanged(projectDir, msg, workItemsDir, crFile)
}

// GitCommitCR stages and commits a single change request after validation.
// crID is the CR's internal id; the on-disk file is resolved via the store so a
// numeric filename prefix (01_reusable-core.yaml) is staged correctly, while the
// commit message stays keyed to the internal id ("cr: create reusable-core").
func GitCommitCR(projectDir string, store *internal.YAMLStore, crID string) (string, error) {
	crFile, err := store.ChangeRequestPath(crID)
	if err != nil {
		return "", fmt.Errorf("failed to resolve CR file for %s: %w", crID, err)
	}
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

func GitCommitSpecMerge(projectDir string, cr *domain.ChangeRequest, mergeResult *MergeResult, specsDir string) error {
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

	return git.CommitIfChanged(projectDir, msg, specsDir)
}
