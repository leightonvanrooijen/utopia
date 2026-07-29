package ralph

import (
	"testing"

	"github.com/leightonvanrooijen/utopia/internal/domain"
)

func TestResolveRoleEfforts_BuiltInDefaults(t *testing.T) {
	efforts := resolveRoleEfforts(nil, "")

	if efforts.executor != "medium" {
		t.Errorf("executor = %q, want medium", efforts.executor)
	}
	if efforts.escalatedExecutor != "high" {
		t.Errorf("escalatedExecutor = %q, want high", efforts.escalatedExecutor)
	}
	if efforts.scoper != "high" {
		t.Errorf("scoper = %q, want high", efforts.scoper)
	}
	if efforts.validators != "medium" {
		t.Errorf("validators = %q, want medium", efforts.validators)
	}
}

func TestResolveRoleEfforts_PerRoleConfig(t *testing.T) {
	efforts := resolveRoleEfforts(&domain.EffortConfig{
		Default:          "low",
		Execute:          "medium",
		ExecuteEscalated: "max",
		Scoper:           "xhigh",
	}, "")

	if efforts.executor != "medium" {
		t.Errorf("executor = %q, want medium", efforts.executor)
	}
	if efforts.escalatedExecutor != "max" {
		t.Errorf("escalatedExecutor = %q, want max", efforts.escalatedExecutor)
	}
	if efforts.scoper != "xhigh" {
		t.Errorf("scoper = %q, want xhigh", efforts.scoper)
	}
	// validators has no key of its own, so effort.default applies.
	if efforts.validators != "low" {
		t.Errorf("validators = %q, want the configured default low", efforts.validators)
	}
}

func TestResolveRoleEfforts_FlagOverridesEveryRole(t *testing.T) {
	efforts := resolveRoleEfforts(&domain.EffortConfig{Execute: "low", Scoper: "medium"}, "max")

	if efforts.executor != "max" || efforts.escalatedExecutor != "max" ||
		efforts.scoper != "max" || efforts.validators != "max" {
		t.Errorf("resolveRoleEfforts with --effort max = %+v, want every role at max", efforts)
	}
}

// The rule this whole file exists for: a mechanical retry runs at the same effort
// as the first attempt. A failure is answered by escalating the model, which is
// strictly cheaper than cranking effort - see ADR-004 - so a well-meaning change
// that makes the cheap executor try harder must fail here.
func TestExecutorEffortFor_MechanicalRetryMatchesFirstAttempt(t *testing.T) {
	efforts := resolveRoleEfforts(nil, "")
	item := &domain.WorkItem{ID: "item"}
	caps := testCaps()

	firstAttempt := executorEffortFor(item, efforts)

	for i := 0; i < caps.MechanicalRetries; i++ {
		d := routeValidationFailure(item, mechanicalAggregate(), caps, testDefaultModel, testEscalatedModel)
		if d.Route != RouteMechanicalRetry {
			t.Fatalf("retry %d: Route = %q, want %q", i+1, d.Route, RouteMechanicalRetry)
		}
		if got := executorEffortFor(item, efforts); got != firstAttempt {
			t.Fatalf("retry %d: effort = %q, want the first attempt's %q - a retry escalates the model, never the effort",
				i+1, got, firstAttempt)
		}
	}
}

// An iteration that produced no completion token is retried the same way: same
// executor, same effort, because nothing about the role has changed.
func TestExecutorEffortFor_UnchangedAcrossIterations(t *testing.T) {
	efforts := resolveRoleEfforts(&domain.EffortConfig{Execute: "low"}, "")

	for _, iteration := range []int{1, 2, 7, 42} {
		item := &domain.WorkItem{ID: "item", IterationCount: iteration}
		if got := executorEffortFor(item, efforts); got != "low" {
			t.Errorf("iteration %d: effort = %q, want low", iteration, got)
		}
	}
}

// An escalated attempt runs at the escalated executor's own level. That is the
// level of a different role, not the default executor's raised - which is why it
// is read from the persisted comprehension counter and not from a failure count.
func TestExecutorEffortFor_EscalatedAttemptUsesEscalatedRoleEffort(t *testing.T) {
	efforts := resolveRoleEfforts(nil, "")
	item := &domain.WorkItem{ID: "item", ComprehensionCount: 1}

	if got := executorEffortFor(item, efforts); got != efforts.escalatedExecutor {
		t.Errorf("effort = %q, want the escalated executor's %q", got, efforts.escalatedExecutor)
	}

	// A resumed item that already escalated keeps that role's level rather than
	// resetting to the default executor's.
	resumed := &domain.WorkItem{ID: "item", ComprehensionCount: 2, IterationCount: 5}
	if got := executorEffortFor(resumed, efforts); got != efforts.escalatedExecutor {
		t.Errorf("resumed effort = %q, want the escalated executor's %q", got, efforts.escalatedExecutor)
	}
}

// With --effort set, even the escalated attempt runs at the overridden level: the
// flag fixes effort for the whole invocation, so escalation still only changes
// which model runs.
func TestExecutorEffortFor_FlagOverrideSurvivesEscalation(t *testing.T) {
	efforts := resolveRoleEfforts(nil, "low")
	item := &domain.WorkItem{ID: "item"}

	first := executorEffortFor(item, efforts)
	item.ComprehensionCount = 1

	if got := executorEffortFor(item, efforts); got != first {
		t.Errorf("escalated effort = %q, want the overridden %q", got, first)
	}
}
