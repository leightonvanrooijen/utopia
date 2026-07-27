package ralph

import (
	"context"
	"errors"

	"github.com/leightonvanrooijen/utopia/internal/domain"
	"github.com/leightonvanrooijen/utopia/internal/validators"
)

// CompileValidators compiles the configured validators into engine
// subscriptions, mirroring CompileConnectors as a pure config -> subscription
// mapping. Validators of the same trigger compile to a single subscription
// whose action runs them all and aggregates their feedback, so combined
// feedback and the concurrency limit are preserved (per-validator
// subscriptions would let Emit's join pass drop all but the first failure):
//
//   - after-workitem validators launch on workitem-completion-claimed, join on
//     workitem-verified, and cancel on workitem-verification-failed. They start
//     speculatively when completion is claimed, are joined once verification
//     passes, and are abandoned if verification fails.
//   - after-phase validators launch and join on phase-verified (gating shape).
//   - on-demand validators are not subscribed to any event.
//
// after-workitem uses the configured validator_concurrency; after-phase uses
// the runner default, matching the pre-migration call sites.
func CompileValidators(runner *validators.Runner, list []*domain.Validator, concurrency int) []Subscription {
	var subs []Subscription
	if hasTrigger(list, domain.RunAfterWorkitem) {
		subs = append(subs, Subscription{
			Name:   "validators:after-workitem",
			Launch: EventWorkItemCompletionClaimed,
			Join:   EventWorkItemVerified,
			Cancel: []string{EventWorkItemVerificationFailed},
			Action: validatorAction(runner, list, domain.RunAfterWorkitem, concurrency),
		})
	}
	if hasTrigger(list, domain.RunAfterPhase) {
		subs = append(subs, Subscription{
			Name:   "validators:after-phase",
			Launch: EventPhaseVerified,
			Join:   EventPhaseVerified,
			Action: validatorAction(runner, list, domain.RunAfterPhase, 0),
		})
	}
	return subs
}

// hasTrigger reports whether any validator is configured with the given trigger.
func hasTrigger(list []*domain.Validator, trigger domain.RunTrigger) bool {
	for _, v := range list {
		if v.GetRun() == trigger {
			return true
		}
	}
	return false
}

// validatorAction builds the engine action that runs the configured validators
// for a single trigger in-process, reusing the validators runner. The run
// starts in a goroutine when the launch event fires (early start) so it can
// proceed concurrently with verification; the returned wait function blocks
// until the run finishes and reports its aggregated outcome. A failing run
// reports a ConnectorResult carrying the combined feedback as stdout and a
// non-nil error, so joining it surfaces a *GateError whose feedback is injected
// into the next iteration - exactly as the bespoke loop did. Cancelling ctx
// kills in-flight validator subprocesses so the run returns promptly.
func validatorAction(runner *validators.Runner, list []*domain.Validator, trigger domain.RunTrigger, concurrency int) Action {
	name := "validators:" + string(trigger)
	return func(ctx context.Context, e Event) func() ConnectorResult {
		done := make(chan *validators.AggregateResult, 1)
		go func() {
			// The diff is computed once here and shared across all validators
			// for this run, matching the pre-migration diff scoping. A diff
			// error yields an empty diff, as the after-workitem call site did.
			diff, _ := runner.GetGitDiff(ctx)
			results := runner.RunAllWithDiffLimited(ctx, list, trigger, diff, concurrency)
			done <- validators.AggregateResults(results)
		}()
		return func() ConnectorResult {
			agg := <-done
			res := ConnectorResult{Name: name, Event: e.Name}
			if !agg.Passed {
				res.Stdout = agg.Feedback
				res.Err = errors.New("validators failed")
			}
			return res
		}
	}
}
