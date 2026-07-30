package ralph

import (
	"context"
	"errors"
	"testing"

	"github.com/leightonvanrooijen/utopia/internal/domain"
	"github.com/leightonvanrooijen/utopia/internal/validators"
)

func TestCompileValidators_AfterWorkitemSpeculativeShape(t *testing.T) {
	runner := validators.NewRunner(t.TempDir())
	subs := CompileValidators(runner, []*domain.Validator{
		{ID: "style", Run: domain.RunAfterWorkitem},
	}, 4)

	if len(subs) != 1 {
		t.Fatalf("expected one after-workitem subscription, got %d", len(subs))
	}
	sub := subs[0]
	if sub.Launch != EventWorkItemCompletionClaimed {
		t.Errorf("after-workitem must launch on %q, got %q", EventWorkItemCompletionClaimed, sub.Launch)
	}
	if sub.Join != EventWorkItemVerified {
		t.Errorf("after-workitem must join on %q, got %q", EventWorkItemVerified, sub.Join)
	}
	if len(sub.Cancel) != 1 || sub.Cancel[0] != EventWorkItemVerificationFailed {
		t.Errorf("after-workitem must cancel on %q, got %v", EventWorkItemVerificationFailed, sub.Cancel)
	}
}

func TestCompileValidators_AfterPhaseGatingShape(t *testing.T) {
	runner := validators.NewRunner(t.TempDir())
	subs := CompileValidators(runner, []*domain.Validator{
		{ID: "arch", Run: domain.RunAfterPhase},
	}, 4)

	if len(subs) != 1 {
		t.Fatalf("expected one after-phase subscription, got %d", len(subs))
	}
	sub := subs[0]
	if sub.Launch != EventPhaseVerified || sub.Join != EventPhaseVerified {
		t.Errorf("after-phase must launch and join on %q, got launch %q join %q", EventPhaseVerified, sub.Launch, sub.Join)
	}
	if len(sub.Cancel) != 0 {
		t.Errorf("after-phase must have no cancel events, got %v", sub.Cancel)
	}
}

func TestCompileValidators_OnDemandNotSubscribed(t *testing.T) {
	runner := validators.NewRunner(t.TempDir())
	subs := CompileValidators(runner, []*domain.Validator{
		{ID: "manual", Run: domain.RunOnDemand},
	}, 4)

	if len(subs) != 0 {
		t.Errorf("on-demand validators must not be subscribed to any event, got %d subscriptions", len(subs))
	}
}

// TestCompileValidators_AggregatesPerTriggerIntoOneSubscription proves that all
// validators of a trigger collapse into a single subscription. That single
// action is what combines every failing validator's feedback: per-validator
// subscriptions would let Emit's join pass surface only the first failure.
func TestCompileValidators_AggregatesPerTriggerIntoOneSubscription(t *testing.T) {
	runner := validators.NewRunner(t.TempDir())
	subs := CompileValidators(runner, []*domain.Validator{
		{ID: "style", Run: domain.RunAfterWorkitem},
		{ID: "security", Run: domain.RunAfterWorkitem},
		{ID: "arch", Run: domain.RunAfterPhase},
		{ID: "manual", Run: domain.RunOnDemand},
	}, 4)

	if len(subs) != 2 {
		t.Fatalf("expected one subscription per active trigger (after-workitem, after-phase), got %d", len(subs))
	}
	if subs[0].Launch != EventWorkItemCompletionClaimed {
		t.Errorf("after-workitem subscription must be registered first, got launch %q", subs[0].Launch)
	}
	if subs[1].Launch != EventPhaseVerified {
		t.Errorf("after-phase subscription must be registered second, got launch %q", subs[1].Launch)
	}
}

// TestValidatorAction_CancelledRunReportsCancellation covers the abandoned
// after-workitem validator: the git diff and any validator subprocess die with
// the cancellation, so the action must report the cancellation rather than the
// kill's fallout, which the engine then resolves as cancelled.
func TestValidatorAction_CancelledRunReportsCancellation(t *testing.T) {
	action := validatorAction(validators.NewRunner(t.TempDir()), []*domain.Validator{
		{ID: "style", Run: domain.RunAfterWorkitem},
	}, domain.RunAfterWorkitem, 1)

	ctx, cancel := context.WithCancel(context.Background())
	wait := action(ctx, Event{Name: EventWorkItemCompletionClaimed})
	cancel()
	res := wait()

	if !errors.Is(res.Err, context.Canceled) {
		t.Fatalf("cancelled run must report the cancellation, got %v", res.Err)
	}
	if !causedByCancellation(res.Err) {
		t.Error("the reported error must resolve the handle as cancelled, not failed")
	}
	if res.Stdout != "" || res.Aggregate != nil {
		t.Errorf("cancelled run must carry no verdict, got stdout %q aggregate %v", res.Stdout, res.Aggregate)
	}
}

func TestSelectRouted(t *testing.T) {
	list := []*domain.Validator{
		{ID: "a"}, {ID: "b"}, {ID: "c"},
	}

	t.Run("unrouted payload runs full list as fallback", func(t *testing.T) {
		got := selectRouted(list, EventPayload{ValidatorsRouted: false, SelectedValidatorIDs: []string{"a"}})
		if len(got) != 3 {
			t.Errorf("expected full list when not routed, got %d validators", len(got))
		}
	})

	t.Run("routed payload narrows to the selection", func(t *testing.T) {
		got := selectRouted(list, EventPayload{ValidatorsRouted: true, SelectedValidatorIDs: []string{"a", "c"}})
		var ids []string
		for _, v := range got {
			ids = append(ids, v.ID)
		}
		if len(ids) != 2 || ids[0] != "a" || ids[1] != "c" {
			t.Errorf("expected [a c], got %v", ids)
		}
	})

	t.Run("routed with empty selection runs nothing", func(t *testing.T) {
		got := selectRouted(list, EventPayload{ValidatorsRouted: true, SelectedValidatorIDs: nil})
		if len(got) != 0 {
			t.Errorf("expected no validators for empty routed selection, got %d", len(got))
		}
	})
}
