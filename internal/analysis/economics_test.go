package analysis

import (
	"testing"

	"github.com/leightonvanrooijen/utopia/internal/domain"
)

// available builds a usage entry whose accounting was read.
func available(iteration int, role, model, effort string, outcome domain.AttemptOutcome, tokens int, cost float64, basis domain.CostBasis) domain.UsageEntry {
	return domain.UsageEntry{
		Iteration: iteration,
		Role:      role,
		Outcome:   outcome,
		AttemptUsage: domain.AttemptUsage{
			Available:    true,
			Model:        model,
			Effort:       effort,
			InputTokens:  tokens,
			OutputTokens: 0,
			CostUSD:      cost,
			CostBasis:    basis,
		},
	}
}

// unavailable builds a usage entry for an attempt that ran with its accounting
// unreadable: the model and effort are known, the spend is not.
func unavailable(iteration int, role, model, effort string, outcome domain.AttemptOutcome) domain.UsageEntry {
	e := domain.UsageEntry{Iteration: iteration, Role: role, Outcome: outcome}
	e.AttemptUsage = domain.AttemptUsage{
		Available:         false,
		UnavailableReason: "the invocation reported no usage",
		Model:             model,
		Effort:            effort,
	}
	return e
}

func TestModelComparison_OneRowPerModelAndEffortPair(t *testing.T) {
	runs := []*domain.ExecutionRun{
		{
			CRID: "cr-1", WorkItemID: "wi-1", SpecRef: "spec-a.feat", Iterations: 1, Outcome: domain.RunCompleted,
			Usage: []domain.UsageEntry{
				available(1, domain.ExecutorRoleDefault, "sonnet-x", "high", domain.AttemptPassed, 100, 0.5, domain.CostBasisCharged),
			},
		},
		{
			CRID: "cr-1", WorkItemID: "wi-2", SpecRef: "spec-a.feat", Iterations: 2, Outcome: domain.RunCompleted,
			Usage: []domain.UsageEntry{
				available(1, domain.ExecutorRoleDefault, "sonnet-x", "high", domain.AttemptFailed, 100, 0.5, domain.CostBasisCharged),
				available(2, domain.ExecutorRoleDefault, "sonnet-x", "high", domain.AttemptPassed, 200, 1.0, domain.CostBasisCharged),
			},
		},
		{
			CRID: "cr-2", WorkItemID: "wi-3", SpecRef: "spec-b.feat", Iterations: 1, Outcome: domain.RunCompleted,
			Usage: []domain.UsageEntry{
				available(1, domain.ExecutorRoleDefault, "sonnet-x", "low", domain.AttemptPassed, 50, 0.25, domain.CostBasisCharged),
			},
		},
	}

	report := ModelComparison(runs, GroupByModel)

	if len(report.Groups) != 1 || report.Groups[0].Key != "" {
		t.Fatalf("groups = %+v, want one ungrouped group", report.Groups)
	}
	rows := report.Groups[0].Rows
	if len(rows) != 2 {
		t.Fatalf("rows = %+v, want one per model and effort pair", rows)
	}
	// Effort is part of the key: the same model at two efforts is two rows.
	if rows[0].Effort != "high" || rows[1].Effort != "low" {
		t.Fatalf("row efforts = %q, %q, want high and low as separate rows", rows[0].Effort, rows[1].Effort)
	}

	high := rows[0]
	if high.Attempts != 3 {
		t.Errorf("attempts = %d, want 3", high.Attempts)
	}
	if high.WorkItems != 2 || high.Completed != 2 {
		t.Errorf("work items = %d completed = %d, want 2 and 2", high.WorkItems, high.Completed)
	}
	if high.FirstPass != 1 || high.FirstPassRate() != 0.5 {
		t.Errorf("first pass = %d rate = %v, want 1 and 0.5", high.FirstPass, high.FirstPassRate())
	}
	if got := high.MeanIterationsToCompletion(); got != 1.5 {
		t.Errorf("mean iterations = %v, want 1.5", got)
	}
	if got := high.Usage.TotalTokens(); got != 400 {
		t.Errorf("tokens = %d, want 400", got)
	}
	charged, listPrice, unknownBasis := high.CostPerCompleted()
	if charged != 1.0 || listPrice != 0 || unknownBasis != 0 {
		t.Errorf("cost per completed = %v/%v/%v, want 1.0 charged only", charged, listPrice, unknownBasis)
	}
}

func TestModelComparison_UnavailableAttemptsCountedAndExcludedFromTotals(t *testing.T) {
	runs := []*domain.ExecutionRun{{
		CRID: "cr-1", WorkItemID: "wi-1", Iterations: 2, Outcome: domain.RunCompleted,
		Usage: []domain.UsageEntry{
			unavailable(1, domain.ExecutorRoleDefault, "sonnet-x", "high", domain.AttemptFailed),
			available(2, domain.ExecutorRoleDefault, "sonnet-x", "high", domain.AttemptPassed, 300, 2.0, domain.CostBasisCharged),
		},
	}}

	row := ModelComparison(runs, GroupByModel).Groups[0].Rows[0]

	if row.Attempts != 2 {
		t.Errorf("attempts = %d, want both attempts counted", row.Attempts)
	}
	if row.Unavailable != 1 {
		t.Errorf("unavailable = %d, want 1 counted in its own column", row.Unavailable)
	}
	if got := row.Usage.TotalTokens(); got != 300 {
		t.Errorf("tokens = %d, want only the available attempt's 300 - not a zero folded in", got)
	}
	if row.Usage.ChargedCostUSD != 2.0 {
		t.Errorf("charged cost = %v, want 2.0", row.Usage.ChargedCostUSD)
	}
	if row.Usage.Complete() {
		t.Error("totals report complete, want a floor because one attempt's usage was unreadable")
	}
}

func TestModelComparison_SubscriptionCostNotSummedWithAPIKeyCost(t *testing.T) {
	runs := []*domain.ExecutionRun{
		{
			CRID: "cr-1", WorkItemID: "wi-1", Iterations: 1, Outcome: domain.RunCompleted,
			Usage: []domain.UsageEntry{
				available(1, domain.ExecutorRoleDefault, "sonnet-x", "high", domain.AttemptPassed, 100, 3.0, domain.CostBasisCharged),
			},
		},
		{
			CRID: "cr-1", WorkItemID: "wi-2", Iterations: 1, Outcome: domain.RunCompleted,
			Usage: []domain.UsageEntry{
				available(1, domain.ExecutorRoleDefault, "sonnet-x", "high", domain.AttemptPassed, 100, 5.0, domain.CostBasisListPriceEstimate),
			},
		},
	}

	row := ModelComparison(runs, GroupByModel).Groups[0].Rows[0]

	if row.Usage.ChargedCostUSD != 3.0 {
		t.Errorf("charged = %v, want 3.0 alone", row.Usage.ChargedCostUSD)
	}
	if row.Usage.ListPriceCostUSD != 5.0 {
		t.Errorf("list-price estimate = %v, want 5.0 alone", row.Usage.ListPriceCostUSD)
	}
	charged, listPrice, _ := row.CostPerCompleted()
	if charged != 1.5 || listPrice != 2.5 {
		t.Errorf("per-completion cost = %v charged / %v list-price, want the bases divided separately", charged, listPrice)
	}
}

func TestModelComparison_GroupsBySpecAndCRType(t *testing.T) {
	runs := []*domain.ExecutionRun{
		{
			CRID: "cr-1", WorkItemID: "wi-1", SpecRef: "spec-a.feat-one", Iterations: 1, Outcome: domain.RunCompleted,
			Routing: &domain.RoutingRecord{CRType: domain.CRTypeFeature},
			Usage: []domain.UsageEntry{
				available(1, domain.ExecutorRoleDefault, "sonnet-x", "high", domain.AttemptPassed, 100, 1, domain.CostBasisCharged),
			},
		},
		{
			CRID: "cr-2", WorkItemID: "wi-2", SpecRef: "spec-b.feat-two", Iterations: 1, Outcome: domain.RunCompleted,
			Routing: &domain.RoutingRecord{CRType: domain.CRTypeEnhancement},
			Usage: []domain.UsageEntry{
				available(1, domain.ExecutorRoleDefault, "sonnet-x", "high", domain.AttemptPassed, 100, 1, domain.CostBasisCharged),
			},
		},
	}

	bySpec := ModelComparison(runs, GroupBySpec)
	if len(bySpec.Groups) != 2 || bySpec.Groups[0].Key != "spec-a" || bySpec.Groups[1].Key != "spec-b" {
		t.Fatalf("spec groups = %+v, want spec-a and spec-b keyed on the spec id half of spec_ref", bySpec.Groups)
	}

	byType := ModelComparison(runs, GroupByCRType)
	if len(byType.Groups) != 2 || byType.Groups[0].Key != "enhancement" || byType.Groups[1].Key != "feature" {
		t.Fatalf("cr_type groups = %+v, want enhancement and feature", byType.Groups)
	}
	for _, group := range byType.Groups {
		if len(group.Rows) != 1 || group.Rows[0].Model != "sonnet-x" {
			t.Errorf("group %q rows = %+v, want the same model row inside the group", group.Key, group.Rows)
		}
	}
}

func TestModelComparison_RecordWithoutUsageIsUnknownNotZero(t *testing.T) {
	runs := []*domain.ExecutionRun{
		{CRID: "cr-1", WorkItemID: "wi-old", Iterations: 3, Outcome: domain.RunCompleted},
		{
			CRID: "cr-1", WorkItemID: "wi-new", Iterations: 1, Outcome: domain.RunCompleted,
			Usage: []domain.UsageEntry{
				available(1, domain.ExecutorRoleDefault, "sonnet-x", "high", domain.AttemptPassed, 100, 1, domain.CostBasisCharged),
			},
		},
	}

	report := ModelComparison(runs, GroupByModel)

	if report.Records != 2 || report.RecordsWithoutUsage != 1 {
		t.Fatalf("records = %d without usage = %d, want 2 and 1", report.Records, report.RecordsWithoutUsage)
	}
	row := report.Groups[0].Rows[0]
	if row.Attempts != 1 || row.Completed != 1 {
		t.Errorf("row = %+v, want only the record that carries usage folded in", row)
	}
}

func TestModelComparison_CompletionCreditedToTheConcludingAttempt(t *testing.T) {
	// The item started on the default executor, failed, escalated and passed. The
	// completion belongs to the escalated model, not the one that failed first.
	runs := []*domain.ExecutionRun{{
		CRID: "cr-1", WorkItemID: "wi-1", Iterations: 2, Outcome: domain.RunCompleted,
		Usage: []domain.UsageEntry{
			available(1, domain.ExecutorRoleDefault, "sonnet-x", "high", domain.AttemptFailed, 100, 1, domain.CostBasisCharged),
			available(2, domain.ExecutorRoleEscalated, "opus-x", "high", domain.AttemptPassed, 100, 4, domain.CostBasisCharged),
		},
	}}

	rows := ModelComparison(runs, GroupByModel).Groups[0].Rows

	byModel := map[string]ModelRow{}
	for _, row := range rows {
		byModel[row.Model] = row
	}
	if got := byModel["opus-x"]; got.Completed != 1 || got.WorkItems != 1 {
		t.Errorf("opus-x row = %+v, want the completion credited to the attempt that passed", got)
	}
	if got := byModel["sonnet-x"]; got.Completed != 0 || got.WorkItems != 0 {
		t.Errorf("sonnet-x row = %+v, want its attempt counted but no completion credited", got)
	}
	if got := byModel["sonnet-x"].Attempts; got != 1 {
		t.Errorf("sonnet-x attempts = %d, want its failed attempt still counted", got)
	}
	if got := byModel["sonnet-x"].MeanIterationsToCompletion(); got != 0 {
		t.Errorf("mean iterations with no completions = %v, want 0 so a caller renders it undefined", got)
	}
}

func TestModelComparison_NoRunRecordsIsEmpty(t *testing.T) {
	report := ModelComparison(nil, GroupByModel)

	if !report.Empty() {
		t.Errorf("report over no records = %+v, want empty", report)
	}
	if len(report.Groups) != 0 {
		t.Errorf("groups = %+v, want none", report.Groups)
	}
}

func TestParseGroupBy(t *testing.T) {
	for _, tc := range []struct {
		value string
		want  GroupBy
		ok    bool
	}{
		{"", GroupByModel, true},
		{"spec", GroupBySpec, true},
		{"cr_type", GroupByCRType, true},
		{"model", "", false},
	} {
		got, err := ParseGroupBy(tc.value)
		if tc.ok && err != nil {
			t.Errorf("ParseGroupBy(%q) errored: %v", tc.value, err)
		}
		if !tc.ok && err == nil {
			t.Errorf("ParseGroupBy(%q) = %q, want an error", tc.value, got)
		}
		if tc.ok && got != tc.want {
			t.Errorf("ParseGroupBy(%q) = %q, want %q", tc.value, got, tc.want)
		}
	}
}

func escalatedRuns() []*domain.ExecutionRun {
	return []*domain.ExecutionRun{
		{
			CRID: "cr-escalated", WorkItemID: "wi-1", Iterations: 3, Outcome: domain.RunCompleted,
			Routing: &domain.RoutingRecord{CRType: domain.CRTypeFeature, OpusExecutionAttempts: 1, Outcome: domain.RoutingPassed},
			Usage: []domain.UsageEntry{
				available(1, domain.ExecutorRoleDefault, "sonnet-x", "high", domain.AttemptFailed, 100, 0.5, domain.CostBasisCharged),
				unavailable(2, domain.ExecutorRoleDefault, "sonnet-x", "high", domain.AttemptFailed),
				available(3, domain.ExecutorRoleEscalated, "opus-x", "high", domain.AttemptPassed, 400, 4.0, domain.CostBasisCharged),
			},
		},
		{
			CRID: "cr-stuck", WorkItemID: "wi-2", Iterations: 2, Outcome: domain.RunFailed,
			Routing: &domain.RoutingRecord{CRType: domain.CRTypeFeature, OpusExecutionAttempts: 1, Outcome: domain.RoutingNeedsHuman},
			Usage: []domain.UsageEntry{
				available(1, domain.ExecutorRoleDefault, "sonnet-x", "high", domain.AttemptFailed, 100, 0.5, domain.CostBasisCharged),
				available(2, domain.ExecutorRoleEscalated, "opus-x", "high", domain.AttemptFailed, 200, 2.0, domain.CostBasisCharged),
			},
		},
		{
			CRID: "cr-easy", WorkItemID: "wi-3", Iterations: 1, Outcome: domain.RunCompleted,
			Routing: &domain.RoutingRecord{CRType: domain.CRTypeFeature, Outcome: domain.RoutingPassed},
			Usage: []domain.UsageEntry{
				available(1, domain.ExecutorRoleDefault, "sonnet-x", "high", domain.AttemptPassed, 100, 0.5, domain.CostBasisCharged),
			},
		},
	}
}

func TestEscalations_OneRowPerEscalatedChangeRequestSplitAtTheBoundary(t *testing.T) {
	report := Escalations(escalatedRuns())

	if got := report.EscalatedCRs(); got != 2 {
		t.Fatalf("escalated change requests = %d, want 2 (the non-escalated one is baseline)", got)
	}
	if report.Rows[0].CRID != "cr-escalated" || report.Rows[1].CRID != "cr-stuck" {
		t.Fatalf("rows = %q, %q, want them ordered by id", report.Rows[0].CRID, report.Rows[1].CRID)
	}

	row := report.Rows[0]
	if len(row.Before.Models) != 1 || row.Before.Models[0] != "sonnet-x" {
		t.Errorf("before models = %v, want the default executor's model", row.Before.Models)
	}
	if len(row.After.Models) != 1 || row.After.Models[0] != "opus-x" {
		t.Errorf("after models = %v, want the escalated executor's model", row.After.Models)
	}
	if row.Before.Attempts != 2 || row.Before.Unavailable != 1 {
		t.Errorf("before attempts = %d unavailable = %d, want 2 and 1", row.Before.Attempts, row.Before.Unavailable)
	}
	if got := row.Before.Usage.TotalTokens(); got != 100 {
		t.Errorf("before tokens = %d, want only the readable attempt's 100", got)
	}
	if got := row.After.Usage.TotalTokens(); got != 400 {
		t.Errorf("after tokens = %d, want 400", got)
	}
	if row.Outcome != "completed" || !row.Completed {
		t.Errorf("outcome = %q completed = %v, want completed", row.Outcome, row.Completed)
	}
	if got := report.Rows[1].Outcome; got != string(domain.RoutingNeedsHuman) {
		t.Errorf("stuck change request outcome = %q, want needs_human", got)
	}
}

func TestEscalations_AggregateMarginalCostAndCompletionRate(t *testing.T) {
	report := Escalations(escalatedRuns())

	if got := report.CompletedAfterEscalating(); got != 1 {
		t.Fatalf("completed after escalating = %d, want 1", got)
	}
	if got := report.MarginalCompletionRate(); got != 0.5 {
		t.Errorf("marginal completion rate = %v, want 0.5", got)
	}
	marginal := report.MarginalUsage()
	if marginal.ChargedCostUSD != 6.0 {
		t.Errorf("after-escalation charged spend = %v, want 6.0", marginal.ChargedCostUSD)
	}
	charged, listPrice, unknownBasis := report.MarginalCostPerEscalation()
	if charged != 3.0 || listPrice != 0 || unknownBasis != 0 {
		t.Errorf("marginal cost per escalation = %v/%v/%v, want 3.0 charged only", charged, listPrice, unknownBasis)
	}
	if report.BaselineCRs != 1 || report.BaselineCompleted != 1 || report.BaselineCompletionRate() != 1 {
		t.Errorf("baseline = %d of %d (%v), want the one non-escalated change request", report.BaselineCompleted, report.BaselineCRs, report.BaselineCompletionRate())
	}
}

func TestEscalations_RewriteOnlyEscalationHasNoAfterAttempts(t *testing.T) {
	report := Escalations([]*domain.ExecutionRun{{
		CRID: "cr-rewritten", WorkItemID: "wi-1", Iterations: 1, Outcome: domain.RunCompleted,
		Routing: &domain.RoutingRecord{ScopingEscalations: 1, SpecRewritten: true, Outcome: domain.RoutingPassed},
		Usage: []domain.UsageEntry{
			available(1, domain.ExecutorRoleDefault, "sonnet-x", "high", domain.AttemptPassed, 100, 0.5, domain.CostBasisCharged),
		},
	}})

	if got := report.EscalatedCRs(); got != 1 {
		t.Fatalf("escalated change requests = %d, want the rewrite counted as an escalation", got)
	}
	row := report.Rows[0]
	if row.ScopingEscalations != 1 {
		t.Errorf("scoping escalations = %d, want 1", row.ScopingEscalations)
	}
	if row.After.Attempts != 0 || len(row.After.Models) != 0 {
		t.Errorf("after side = %+v, want no attempts: the escalated executor never ran", row.After)
	}
}

func TestEscalations_NoRunRecordsIsEmpty(t *testing.T) {
	report := Escalations(nil)

	if !report.Empty() {
		t.Errorf("report over no records = %+v, want empty", report)
	}
	if report.EscalatedCRs() != 0 || report.MarginalCompletionRate() != 0 {
		t.Errorf("report = %+v, want no rows and no rate", report)
	}
}
