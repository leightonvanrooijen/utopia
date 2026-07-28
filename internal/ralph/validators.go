package ralph

import (
	"context"
	"errors"
	"fmt"

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

// selectRouted narrows the validator list to the relevance router's selection
// carried on the launch event's payload. When routing has not run (or fell back
// on failure), ValidatorsRouted is false and the full list is returned so the
// gate runs every applicable validator rather than silently skipping validation.
// The trigger filter in RunAllWithDiffLimited still narrows the result to this
// subscription's trigger, so a selection spanning triggers composes correctly.
func selectRouted(list []*domain.Validator, p EventPayload) []*domain.Validator {
	if !p.ValidatorsRouted {
		return list
	}
	chosen := make(map[string]bool, len(p.SelectedValidatorIDs))
	for _, id := range p.SelectedValidatorIDs {
		chosen[id] = true
	}
	var out []*domain.Validator
	for _, v := range list {
		if chosen[v.ID] {
			out = append(out, v)
		}
	}
	return out
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
		type outcome struct {
			agg *validators.AggregateResult
			err error
		}
		done := make(chan outcome, 1)
		go func() {
			// The diff is computed once here and shared across all validators
			// for this run. A failure to compute the diff is surfaced as an
			// error rather than silently swallowed into an empty diff, which
			// would let validators pass vacuously against no changes.
			//
			// after-workitem validators run before the work item is committed,
			// so "git diff HEAD" scopes to the changes under review. after-phase
			// validators run once every work item is already committed, so they
			// diff against the phase-start baseline recorded on the payload to
			// review the cumulative changes of the whole phase.
			var diff string
			var err error
			if trigger == domain.RunAfterPhase {
				diff, err = runner.GetGitDiffSince(ctx, e.Payload.PhaseStartSHA)
			} else {
				diff, err = runner.GetGitDiff(ctx)
			}
			if err != nil {
				done <- outcome{err: err}
				return
			}
			results := runner.RunAllWithDiffLimited(ctx, selectRouted(list, e.Payload), trigger, diff, concurrency)
			done <- outcome{agg: validators.AggregateResults(results)}
		}()
		return func() ConnectorResult {
			o := <-done
			res := ConnectorResult{Name: name, Event: e.Name}
			if o.err != nil {
				res.Err = fmt.Errorf("failed to compute git diff: %w", o.err)
				return res
			}
			if !o.agg.Passed {
				res.Stdout = o.agg.Feedback
				res.Err = errors.New("validators failed")
			}
			return res
		}
	}
}
