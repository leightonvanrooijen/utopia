package cli

// execute_run_policy.go isolates the daemon's failure policy - the one decision
// in `utopia execute run` that is a genuine judgment call rather than a
// mechanical default. Keep it here, separate from the loop, so the policy is
// easy to find and change.

import (
	"github.com/leightonvanrooijen/utopia/internal/domain"
	"github.com/leightonvanrooijen/utopia/internal/ui"
)

// handleWatchFailure decides what the daemon does when a ready CR fails to
// execute (a real execution error - Ctrl+C and timeouts are handled by the
// caller before this is ever reached).
//
// Return nil to keep the daemon alive: log the failure, skip this CR, and carry
// on watching the queue. Note that a failed CR has already been chunked, so its
// status is now `in-progress`, not `approved` - it will NOT be re-picked on the
// next scan, so "keep watching" does not mean "retry in a tight loop".
//
// Return a non-nil error to stop the daemon entirely and surface the failure to
// the operator (this becomes the process's non-zero exit).
//
// TODO(you): this is the decision worth making deliberately. The trade-off:
//
//   - Resilient (return nil): one broken CR never takes down an unattended
//     daemon; other approved CRs still get done. Risk: failures scroll past and
//     a CR quietly ends up stuck in-progress until someone notices.
//   - Fail-fast (return the error): mirrors `utopia execute --all`, which stops
//     on first failure. Loud and safe for interactive use; wrong for a daemon
//     you expect to leave running overnight.
//
// The default below is resilient. Adjust it to match how you intend to run this.
func handleWatchFailure(out *ui.Printer, cr *domain.ChangeRequest, execErr error) error {
	out.Progressf("\n  CR failed: %s (%s): %s\n", cr.Title, cr.ID, execErr)
	out.Progressf("  Skipping and continuing to watch. Resume it later with: utopia execute %s\n\n", cr.ID)
	return nil
}
