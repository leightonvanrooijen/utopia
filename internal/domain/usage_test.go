package domain

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// The three states have to survive the round trip to YAML, because the record on
// disk is what a report reads. An attempt whose accounting was never captured, one
// whose accounting could not be read, and one that genuinely spent nothing are
// three different facts, and only the third may be summed as zero.
func TestAttemptUsage_UnavailableIsDistinguishableFromZero(t *testing.T) {
	unavailable, err := yaml.Marshal(&ExecutorAttempt{
		Iteration: 1,
		Role:      ExecutorRoleDefault,
		Model:     "opus",
		Usage:     UnavailableUsage("the claude CLI produced no parseable terminal result object"),
	})
	if err != nil {
		t.Fatalf("yaml.Marshal() error = %v", err)
	}

	zero, err := yaml.Marshal(&ExecutorAttempt{
		Iteration: 2,
		Role:      ExecutorRoleDefault,
		Model:     "opus",
		Usage:     &AttemptUsage{Available: true, Model: "claude-opus-5-20260101", CostBasis: CostBasisCharged},
	})
	if err != nil {
		t.Fatalf("yaml.Marshal() error = %v", err)
	}

	never, err := yaml.Marshal(&ExecutorAttempt{Iteration: 3, Role: ExecutorRoleDefault, Model: "opus"})
	if err != nil {
		t.Fatalf("yaml.Marshal() error = %v", err)
	}

	if !strings.Contains(string(unavailable), "available: false") {
		t.Errorf("unavailable usage =\n%s\nwant an explicit available: false", unavailable)
	}
	if !strings.Contains(string(unavailable), "unavailable_reason:") {
		t.Errorf("unavailable usage =\n%s\nwant the reason recorded", unavailable)
	}
	if !strings.Contains(string(zero), "available: true") {
		t.Errorf("zero usage =\n%s\nwant an explicit available: true", zero)
	}
	if strings.Contains(string(zero), "input_tokens") {
		t.Errorf("zero usage =\n%s\nwant zero counts omitted, since the flag carries the meaning", zero)
	}
	if strings.Contains(string(never), "usage:") {
		t.Errorf("uncaptured attempt =\n%s\nwant no usage key at all", never)
	}

	// Reading them back keeps them apart, which is what a report has to rely on.
	var back [3]ExecutorAttempt
	for i, data := range [][]byte{unavailable, zero, never} {
		if err := yaml.Unmarshal(data, &back[i]); err != nil {
			t.Fatalf("yaml.Unmarshal() error = %v", err)
		}
	}

	if back[0].Usage == nil || back[0].Usage.IsAvailable() {
		t.Errorf("unavailable round-tripped to %+v, want a record that is not available", back[0].Usage)
	}
	if !back[1].Usage.IsAvailable() {
		t.Errorf("zero usage round-tripped to %+v, want available", back[1].Usage)
	}
	if back[1].Usage.InputTokens != 0 || back[1].Usage.CostUSD != 0 {
		t.Errorf("zero usage round-tripped to %+v, want zero counts", back[1].Usage)
	}
	if back[2].Usage != nil {
		t.Errorf("uncaptured attempt round-tripped to %+v, want nil", back[2].Usage)
	}
}

// Tokens are a fact under both auth modes; dollars are only money under api-key
// auth. The basis travels with the number so a reader cannot pool the two.
func TestCostBasisForAuth(t *testing.T) {
	tests := []struct {
		mode AuthMode
		want CostBasis
	}{
		{AuthModeAPIKey, CostBasisCharged},
		{AuthModeSubscription, CostBasisListPriceEstimate},
		{"", CostBasisUnknown},
	}

	for _, tt := range tests {
		if got := CostBasisForAuth(tt.mode); got != tt.want {
			t.Errorf("CostBasisForAuth(%q) = %q, want %q", tt.mode, got, tt.want)
		}
	}

	subscription := &AttemptUsage{Available: true, CostUSD: 12, CostBasis: CostBasisListPriceEstimate}
	if subscription.CostIsCharged() {
		t.Error("a subscription cost reports as charged, want it marked a list-price estimate")
	}

	var missing *AttemptUsage
	if missing.IsAvailable() || missing.CostIsCharged() {
		t.Error("a nil usage reports as available, want the nil case to read as not captured")
	}
}

// spentAttempts is a work item's attempt list as the loop leaves it: two attempts
// that spent tokens and failed, and a third that passed. The middle one's
// accounting could not be read, which is the case a total has to keep visible.
func spentAttempts() []ExecutorAttempt {
	return []ExecutorAttempt{
		{
			Iteration: 1, Role: ExecutorRoleDefault, Model: "sonnet", Effort: "medium",
			Outcome: AttemptFailed, FailureClass: "mechanical",
			Usage: &AttemptUsage{
				Available: true, Model: "claude-sonnet-5-20260101", Effort: "medium",
				InputTokens: 100, OutputTokens: 20, CacheReadTokens: 5, CacheCreationTokens: 2,
				Turns: 8, CostUSD: 0.25, CostBasis: CostBasisCharged,
			},
		},
		{
			Iteration: 2, Role: ExecutorRoleDefault, Model: "sonnet", Effort: "medium",
			Outcome: AttemptFailed, FailureClass: "comprehension",
			Usage: UnavailableUsage("the invocation reported no usage"),
		},
		{
			Iteration: 3, Role: ExecutorRoleEscalated, Model: "opus", Effort: "high",
			Outcome: AttemptPassed,
			Usage: &AttemptUsage{
				Available: true, Model: "claude-opus-5-20260101", Effort: "high",
				InputTokens: 400, OutputTokens: 60, Turns: 20, CostUSD: 3, CostBasis: CostBasisCharged,
			},
		},
	}
}

// One entry per iteration, in order, each saying which model at which effort ran
// it and what that attempt achieved. That adjacency is the whole point of the
// list: a model is only comparable against what it finished.
func TestUsageEntriesFor_OneOrderedEntryPerIteration(t *testing.T) {
	entries := UsageEntriesFor(spentAttempts())

	if len(entries) != 3 {
		t.Fatalf("entries = %d, want one per attempt (3)", len(entries))
	}
	for i, e := range entries {
		if e.Iteration != i+1 {
			t.Errorf("entry %d has iteration %d, want the attempts in iteration order", i, e.Iteration)
		}
	}

	first := entries[0]
	if first.Role != ExecutorRoleDefault || first.Model != "claude-sonnet-5-20260101" || first.Effort != "medium" {
		t.Errorf("entry 1 = %+v, want the role, resolved model and effort it ran on", first)
	}
	if first.Outcome != AttemptFailed || first.FailureClass != "mechanical" {
		t.Errorf("entry 1 outcome = %q/%q, want failed/mechanical", first.Outcome, first.FailureClass)
	}
	if first.InputTokens != 100 || first.CostUSD != 0.25 || first.CostBasis != CostBasisCharged {
		t.Errorf("entry 1 spend = %+v, want the counts and the cost with its basis", first.AttemptUsage)
	}
	// A passing attempt carries no class: there was no failure to classify.
	if last := entries[2]; last.Outcome != AttemptPassed || last.FailureClass != "" {
		t.Errorf("entry 3 = %q/%q, want passed with no failure class", last.Outcome, last.FailureClass)
	}
}

// An attempt whose accounting could not be read still ran, so it keeps its place
// in the list. Dropping it would make the record read like a run with fewer
// iterations than it had.
func TestUsageEntriesFor_UnreadableAccountingStaysAnEntry(t *testing.T) {
	entries := UsageEntriesFor(spentAttempts())

	second := entries[1]
	if second.Available {
		t.Errorf("entry 2 = %+v, want it marked unavailable", second.AttemptUsage)
	}
	if second.UnavailableReason == "" {
		t.Error("entry 2 records no reason, want why the accounting could not be read")
	}
	// The model routing asked for stands in when the CLI resolved none, so the row
	// still says which tier spent the tokens.
	if second.Model != "sonnet" || second.Effort != "medium" {
		t.Errorf("entry 2 = %q at %q, want the model and effort routing asked for", second.Model, second.Effort)
	}
	if second.Outcome != AttemptFailed || second.FailureClass != "comprehension" {
		t.Errorf("entry 2 outcome = %q/%q, want failed/comprehension", second.Outcome, second.FailureClass)
	}
}

// An attempt made before usage was captured at all becomes an entry too, marked
// unavailable rather than zero.
func TestUsageEntriesFor_UncapturedAttemptIsUnavailableNotZero(t *testing.T) {
	entries := UsageEntriesFor([]ExecutorAttempt{
		{Iteration: 1, Role: ExecutorRoleDefault, Model: "opus", Effort: "high"},
	})

	if len(entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(entries))
	}
	if entries[0].Available {
		t.Errorf("entry = %+v, want an uncaptured attempt marked unavailable", entries[0].AttemptUsage)
	}
	if UsageEntriesFor(nil) != nil {
		t.Error("no attempts produced a non-nil list, want no list at all")
	}
}

// The entry is one flat row on disk, so a reader scanning a column sees the
// available flag next to the counts it qualifies.
func TestUsageEntry_IsRecordedFlat(t *testing.T) {
	data, err := yaml.Marshal(UsageEntriesFor(spentAttempts()))
	if err != nil {
		t.Fatalf("yaml.Marshal() error = %v", err)
	}
	recorded := string(data)

	for _, want := range []string{"iteration: 1", "role: default-executor", "outcome: failed", "failure_class: mechanical", "available: true", "input_tokens: 100", "cost_basis: charged", "available: false"} {
		if !strings.Contains(recorded, want) {
			t.Errorf("recorded entries =\n%s\nwant %q", recorded, want)
		}
	}
	if strings.Contains(recorded, "usage:") {
		t.Errorf("recorded entries =\n%s\nwant the spend inline rather than nested under a usage key", recorded)
	}

	var back []UsageEntry
	if err := yaml.Unmarshal(data, &back); err != nil {
		t.Fatalf("yaml.Unmarshal() error = %v", err)
	}
	if len(back) != 3 || back[1].Available || !back[2].Available {
		t.Errorf("round-tripped entries = %+v, want the available flags kept apart", back)
	}
}

// The work item's total is a sum of its own entries - no transcript involved -
// and what could not be read is counted rather than folded in as zero.
func TestExecutionRun_UsageTotals(t *testing.T) {
	run := &ExecutionRun{Usage: UsageEntriesFor(spentAttempts())}

	got := run.UsageTotals()

	if got.Entries != 3 || got.Available != 2 || got.Unavailable != 1 {
		t.Errorf("totals = %d entries / %d available / %d unavailable, want 3/2/1", got.Entries, got.Available, got.Unavailable)
	}
	if got.InputTokens != 500 || got.OutputTokens != 80 || got.CacheReadTokens != 5 || got.CacheCreationTokens != 2 {
		t.Errorf("token totals = %+v, want only the readable attempts summed", got)
	}
	if got.TotalTokens() != 587 {
		t.Errorf("TotalTokens() = %d, want 587", got.TotalTokens())
	}
	if got.Turns != 28 {
		t.Errorf("turns = %d, want 28", got.Turns)
	}
	if got.ChargedCostUSD != 3.25 {
		t.Errorf("charged cost = %v, want 3.25", got.ChargedCostUSD)
	}
	// One attempt's accounting was unreadable, so the total is a floor and says so.
	if got.Complete() {
		t.Error("totals report complete, want an unreadable attempt to make them a floor")
	}
}

// Charged dollars and a list-price equivalent of subscription tokens are
// different quantities, so they are never summed into one figure.
func TestTotalUsage_KeepsCostBasesApart(t *testing.T) {
	got := TotalUsage([]UsageEntry{
		{Iteration: 1, AttemptUsage: AttemptUsage{Available: true, CostUSD: 1.5, CostBasis: CostBasisCharged}},
		{Iteration: 2, AttemptUsage: AttemptUsage{Available: true, CostUSD: 4, CostBasis: CostBasisListPriceEstimate}},
		{Iteration: 3, AttemptUsage: AttemptUsage{Available: true, CostUSD: 2, CostBasis: CostBasisUnknown}},
	})

	if got.ChargedCostUSD != 1.5 || got.ListPriceCostUSD != 4 || got.UnknownBasisCostUSD != 2 {
		t.Errorf("costs = %v charged / %v list-price / %v unknown basis, want 1.5/4/2", got.ChargedCostUSD, got.ListPriceCostUSD, got.UnknownBasisCostUSD)
	}
	if !got.Complete() {
		t.Error("totals report incomplete, want three readable entries to be complete")
	}
}

// A record written before usage was persisted is unknown spend. Reading it must
// still work, and its silence must not be published as a zero.
func TestUsageTotals_MissingListIsUnknownNotZero(t *testing.T) {
	legacy := []byte("workitem_id: item-old\ncr_id: cr-1\niterations: 2\noutcome: completed\ntranscript: |\n  did the work\n")

	var run ExecutionRun
	if err := yaml.Unmarshal(legacy, &run); err != nil {
		t.Fatalf("a record without usage entries must stay readable: %v", err)
	}
	if run.Usage != nil {
		t.Errorf("usage = %+v, want no list at all", run.Usage)
	}

	got := run.UsageTotals()
	if got.RecordsWithoutUsage != 1 {
		t.Errorf("records without usage = %d, want the record counted as unknown spend", got.RecordsWithoutUsage)
	}
	if got.Entries != 0 || got.TotalTokens() != 0 {
		t.Errorf("totals = %+v, want nothing invented for a record that recorded nothing", got)
	}
	if got.Complete() {
		t.Error("totals report complete, want a record with no usage list to make them incomplete")
	}
}

// A change request's total is its work items' totals folded together, which is
// one directory read and no transcripts.
func TestTotalRunUsage_SumsTheChangeRequestsRecords(t *testing.T) {
	got := TotalRunUsage([]*ExecutionRun{
		{WorkItemID: "item-a", Usage: UsageEntriesFor(spentAttempts())},
		{WorkItemID: "item-b", Usage: []UsageEntry{
			{Iteration: 1, AttemptUsage: AttemptUsage{Available: true, InputTokens: 50, OutputTokens: 10, CostUSD: 0.75, CostBasis: CostBasisCharged}},
		}},
		// A work item whose record predates usage: counted as unknown, not as zero.
		{WorkItemID: "item-c"},
		nil,
	})

	if got.Entries != 4 || got.Available != 3 || got.Unavailable != 1 {
		t.Errorf("totals = %d entries / %d available / %d unavailable, want 4/3/1", got.Entries, got.Available, got.Unavailable)
	}
	if got.RecordsWithoutUsage != 1 {
		t.Errorf("records without usage = %d, want 1", got.RecordsWithoutUsage)
	}
	if got.InputTokens != 550 || got.OutputTokens != 90 {
		t.Errorf("token totals = %d in / %d out, want 550/90", got.InputTokens, got.OutputTokens)
	}
	if got.ChargedCostUSD != 4 {
		t.Errorf("charged cost = %v, want 4", got.ChargedCostUSD)
	}
	if got.Complete() {
		t.Error("totals report complete, want the missing record to make them a floor")
	}
}
