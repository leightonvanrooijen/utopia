package cli

// execute_run.go implements `utopia execute run` - a long-running daemon that
// watches the change-request queue and executes work as it becomes ready,
// instead of the one-shot `utopia execute [cr-id]` / `utopia execute --all`.
//
// Design:
//   - Readiness: a CR is "ready" when its status is `approved`. Picking it up
//     immediately transitions it to `in-progress` (via chunking), so it leaves
//     the ready set and is not re-processed on the next scan.
//   - Detection: polling. Every --interval seconds the loop re-scans
//     .utopia/change-requests/ from disk, so CRs approved by another process
//     (a human editing YAML, another `utopia` invocation) are picked up without
//     any in-process coordination.
//   - Concurrency: strictly sequential. The Ralph engine is single-goroutine and
//     not concurrency-safe, and parallel CRs would collide on git. Ready CRs are
//     drained one at a time.
//
// The loop is deliberately thin: the only branch worth unit-testing is the
// readiness predicate, which is factored out into readyChangeRequests.

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/leightonvanrooijen/utopia/internal/cli/ui"
	"github.com/leightonvanrooijen/utopia/internal/domain"
	"github.com/spf13/cobra"
)

func newExecuteRunCmd() *cobra.Command {
	var intervalSec int
	cmd := &cobra.Command{
		Use:   "run",
		Short: "Watch the queue and execute approved change requests as they become ready",
		Long: `Run continuously as a daemon, executing work instead of exiting after one CR.

Every --interval seconds the queue (.utopia/change-requests/) is re-scanned.
Any change request whose status is 'approved' is picked up and executed with the
Ralph loop, then merged into specs - exactly as 'utopia execute <cr-id>' would.
Ready CRs are drained one at a time (never in parallel).

Approve a CR (set its status to 'approved') from anywhere - another terminal, an
editor, another tool - and this loop will pick it up on the next scan. When the
queue has nothing ready, the loop idles until the next scan.

Press Ctrl+C to stop the daemon (any in-flight CR saves its state first; resume
it later with 'utopia execute <cr-id>').`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runExecuteWatch(cmd, intervalSec)
		},
	}
	cmd.Flags().IntVar(&intervalSec, "interval", 10, "seconds to wait between queue scans")
	cmd.Flags().StringVar(&executeModelFlag, "model", "", "model to use (haiku, sonnet, opus)")
	cmd.Flags().StringVar(&executeAuthFlag, "auth", "", "credential to use (api-key, subscription), overriding config.auth.mode")
	return cmd
}

// readyChangeRequests returns the subset of crs the daemon should execute, in the
// order they should run. A CR is ready when it has been approved but not yet
// started or completed; any other status (draft, in-progress, complete) is left
// alone. Extracted as a pure function so the readiness policy is testable in
// isolation from the polling loop.
func readyChangeRequests(crs []*domain.ChangeRequest) []*domain.ChangeRequest {
	var ready []*domain.ChangeRequest
	for _, cr := range crs {
		if cr.Status == domain.ChangeRequestApproved {
			ready = append(ready, cr)
		}
	}
	return ready
}

// runExecuteWatch is the poll loop: scan → drain ready CRs sequentially → sleep.
func runExecuteWatch(cmd *cobra.Command, intervalSec int) error {
	out := ui.NewPrinter(cmd.OutOrStdout(), cmd.ErrOrStderr())

	modelID, err := ResolveModelFlag(cmd)
	if err != nil {
		return err
	}

	// Report the credential this invocation runs with, before any work starts
	if _, err := ResolveAuth(cmd); err != nil {
		return err
	}
	if intervalSec <= 0 {
		return fmt.Errorf("invalid interval value: %d (must be a positive integer)", intervalSec)
	}

	absPath, utopiaDir, store, err := ResolveProject(cmd)
	if err != nil {
		return err
	}
	config, err := store.LoadConfig()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigChan
		out.Progressf("\n\nInterrupt received, stopping watch (in-flight CR state is saved)...\n")
		cancel()
	}()

	interval := time.Duration(intervalSec) * time.Second
	out.Progressf("Watching queue in %s\n", utopiaDir)
	out.Progressf("Scanning every %s for approved change requests. Press Ctrl+C to stop.\n\n", interval)

	for {
		if ctx.Err() != nil {
			out.Progressf("\nWatch stopped.\n")
			return nil
		}

		crs, err := store.ListChangeRequests()
		if err != nil {
			// A transient read error (e.g. a CR file being rewritten mid-scan)
			// should not kill the daemon; log it and try again next tick.
			out.Progressf("  Scan failed: %s (retrying in %s)\n", err, interval)
			if sleepInterrupted(ctx, interval) {
				continue
			}
			continue
		}

		ready := readyChangeRequests(crs)
		if len(ready) == 0 {
			if sleepInterrupted(ctx, interval) {
				continue
			}
			continue
		}

		out.Progressf("Found %d ready change request(s)\n\n", len(ready))
		for _, cr := range ready {
			if ctx.Err() != nil {
				break
			}

			out.Progressf("================================================================\n")
			out.Progressf("Executing ready CR: %s (%s)\n", cr.Title, cr.ID)
			out.Progressf("================================================================\n\n")

			if execErr := executeSingleCR(ctx, out, cr, store, config, absPath, utopiaDir, modelID); execErr != nil {
				// Ctrl+C / timeout during execution: stop the whole daemon cleanly.
				if ctx.Err() != nil {
					out.Progressf("\nWatch stopped.\n")
					return nil
				}
				// A genuine CR failure. The failure policy decides whether the
				// daemon keeps watching or shuts down - see handleWatchFailure.
				if stopErr := handleWatchFailure(out, cr, execErr); stopErr != nil {
					return stopErr
				}
				continue
			}

			out.Progressf("\n  Completed ready CR: %s\n\n", cr.Title)
		}
	}
}

// sleepInterrupted waits for d, or until the context is cancelled. It returns
// true if the wait completed normally and false if the context was cancelled
// (in which case the caller should let the loop observe ctx.Err() and exit).
func sleepInterrupted(ctx context.Context, d time.Duration) bool {
	select {
	case <-ctx.Done():
		return false
	case <-time.After(d):
		return true
	}
}
