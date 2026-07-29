package ralph

import "github.com/leightonvanrooijen/utopia/internal/domain"

// roleEfforts is the effort level each role in the loop runs at, resolved once
// per run before any work item starts.
//
// It is a value rather than a lookup the loop performs per attempt on purpose:
// the levels cannot drift as a run progresses, so there is nowhere for a failure,
// a retry or an escalation to raise one. Failures change which model runs - see
// ADR-004 for why that is the cheaper move - never how hard the cheap one tries.
type roleEfforts struct {
	// executor is the level every attempt on the default executor runs at, first
	// attempt and mechanical retry alike.
	executor string
	// escalatedExecutor is the level an escalated execution attempt runs at. It is
	// higher by default because the escalated executor is a different role, not
	// the default executor trying harder.
	escalatedExecutor string
	// scoper is the level a change-request rewrite runs at.
	scoper string
	// validators is the level every validator invocation runs at.
	validators string
}

// resolveRoleEfforts resolves each role's effort from config, with override -
// the --effort flag - winning for every role when it is set. An empty override
// and an omitted effort section leave each role on its built-in default.
func resolveRoleEfforts(ec *domain.EffortConfig, override string) roleEfforts {
	if override != "" {
		return roleEfforts{
			executor:          override,
			escalatedExecutor: override,
			scoper:            override,
			validators:        override,
		}
	}

	return roleEfforts{
		executor:          ec.ExecutorEffort(),
		escalatedExecutor: ec.EscalatedExecutorEffort(),
		scoper:            ec.ScoperEffort(),
		validators:        ec.ValidatorEffort(),
	}
}

// executorEffortFor resolves the effort the next attempt on this work item runs
// at. Like executorModelFor, it reads the item's persisted comprehension counter,
// so a resumed item that already escalated keeps the escalated executor's level
// rather than resetting to the default executor's.
//
// It reports which role is running, not how badly things are going: a mechanical
// retry has not escalated, so it returns the same level the first attempt got.
func executorEffortFor(item *domain.WorkItem, efforts roleEfforts) string {
	if item.ComprehensionCount > 0 {
		return efforts.escalatedExecutor
	}
	return efforts.executor
}
