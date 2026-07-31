package cli

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/leightonvanrooijen/utopia/internal/domain"
	"github.com/leightonvanrooijen/utopia/internal/ui"
)

// NewRequeueCmd builds the command that returns a halted work item to the queue.
//
// Halting is a decision to stop and ask a person, not a decision to make the
// item permanently unrunnable: once the person has looked and acted, this is the
// supported way back in. Without it, resuming an item means hand-editing the
// counters in its YAML.
func NewRequeueCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "requeue <work-item-id>",
		Short: "Return a halted work item to the queue",
		Long: `Set a work item halted as needs_human back to pending.

The routing state that caused the halt is cleared - the iteration, escalation
and invocation-error counters, and the stale validator feedback and failure
conclusions - so the next attempt is bounded by the full caps and is not handed
a diagnosis reached before you intervened.

What the item cost is kept: its executor attempts and their usage, and the run
record it belongs to, are left intact.`,
		Args: cobra.ExactArgs(1),
		RunE: runRequeue,
	}
}

func init() {
	rootCmd.AddCommand(NewRequeueCmd())
}

func runRequeue(cmd *cobra.Command, args []string) error {
	out := ui.NewPrinter(cmd.OutOrStdout(), cmd.ErrOrStderr())
	_, _, store, err := ResolveProject(cmd)
	if err != nil {
		return err
	}

	itemID := args[0]
	item, specID, err := store.FindWorkItem(itemID)
	if err != nil {
		var nfe *domain.NotFoundError
		if errors.As(err, &nfe) {
			return fmt.Errorf("work item %q not found (use 'utopia status' to see work items)", itemID)
		}
		return fmt.Errorf("failed to load work item %s: %w", itemID, err)
	}

	reset, err := item.Requeue()
	if err != nil {
		var halted *domain.NotHaltedError
		if errors.As(err, &halted) {
			cmd.SilenceUsage = true
			return err
		}
		return fmt.Errorf("failed to requeue work item %s: %w", itemID, err)
	}

	if err := store.SaveWorkItemForSpec(specID, item); err != nil {
		return fmt.Errorf("failed to save work item %s: %w", itemID, err)
	}

	out.Successf("%s requeued: %s -> %s", item.ID, reset.From, item.Status)
	if len(reset.Cleared) == 0 {
		out.Printf("  nothing to clear - the item halted with no counters or feedback recorded\n")
	}
	for _, field := range reset.Cleared {
		out.Printf("  %s cleared %s (was %s)\n", ui.Bullet, field.Name, field.Was)
	}
	out.Printf("  kept: recorded spend and the run record\n")
	return nil
}
