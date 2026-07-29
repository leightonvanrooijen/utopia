package ralph

import (
	"fmt"
	"time"

	"github.com/leightonvanrooijen/utopia/internal/cli/ui"
)

// stepTimings accumulates the wall-clock time one work item spends in each
// expensive step the loop brackets, so a completed item can report where its
// time went instead of only how many iterations it took. Every step is timed
// as a black-box call - start marker before, elapsed after - so the Claude and
// validator agents need no timing API of their own.
//
// It is written only from the loop goroutine that owns the work item, so it
// carries no lock.
type stepTimings struct {
	// start is when the work item began, the baseline for total wall clock
	start time.Time
	// claude is time spent inside Claude invocations
	claude time.Duration
	// verification is time spent running the verification command
	verification time.Duration
	// validators is time the loop spent joining validator runs
	validators time.Duration
}

// newStepTimings starts the wall clock for a work item.
func newStepTimings() *stepTimings {
	return &stepTimings{start: time.Now()}
}

// summary renders the total wall clock with its per-category breakdown.
//
// Total is measured, not summed: the categories account for part of it rather
// than partitioning it. Validators launch speculatively at
// workitem-completion-claimed and run alongside verification, so their share
// here is only what the loop still had to wait for at the join - the engine's
// resolution ledger reports each run's full duration. Time spent waiting out a
// Claude usage limit belongs to no step and shows up as the shortfall.
func (t *stepTimings) summary() string {
	return fmt.Sprintf("total %s (claude %s, verification %s, validators %s)",
		ui.Duration(time.Since(t.start)),
		ui.Duration(t.claude),
		ui.Duration(t.verification),
		ui.Duration(t.validators),
	)
}
