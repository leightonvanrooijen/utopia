package domain

import (
	"strings"
	"testing"
)

// The note is the record's own statement about its gaps, so it has to name them.
// A reader who cannot tell an unmeasured attempt from a free one will read the
// missing tokens as zero, which understates every total built on these records.
func TestCostNoteFor_NamesTheAttemptsWithNoTokensOrCost(t *testing.T) {
	charged := func(iteration int) ExecutorAttempt {
		return ExecutorAttempt{Iteration: iteration, Usage: &AttemptUsage{
			Available: true, InputTokens: 100, CostUSD: 0.5, CostBasis: CostBasisCharged,
		}}
	}

	tests := []struct {
		name     string
		attempts []ExecutorAttempt
		want     []string
		notWant  []string
	}{
		{
			name:     "every attempt accounted for",
			attempts: []ExecutorAttempt{charged(1), charged(2)},
			want:     []string{"Every attempt's token counts and cost were read"},
			notWant:  []string{"unavailable for attempt(s)", "no cost"},
		},
		{
			name: "an attempt whose usage could not be read",
			attempts: []ExecutorAttempt{
				charged(1),
				{Iteration: 2, Usage: UnavailableUsage("the invocation reported no usage")},
				{Iteration: 3},
			},
			want:    []string{"Token counts and cost were unavailable for attempt(s) 2, 3", "unknown, not zero"},
			notWant: []string{"Every attempt's"},
		},
		{
			name: "tokens read but no cost reported",
			attempts: []ExecutorAttempt{
				charged(1),
				{Iteration: 2, Usage: &AttemptUsage{Available: true, InputTokens: 50}},
			},
			want:    []string{"Attempt(s) 2 reported token counts but no cost"},
			notWant: []string{"Every attempt's", "unavailable for attempt(s)"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			note := CostNoteFor(tt.attempts)

			if !strings.Contains(note, costNotePreamble) {
				t.Errorf("note = %q, want it to open by saying cost is measured rather than approximated", note)
			}
			for _, want := range tt.want {
				if !strings.Contains(note, want) {
					t.Errorf("note = %q, want it to contain %q", note, want)
				}
			}
			for _, notWant := range tt.notWant {
				if strings.Contains(note, notWant) {
					t.Errorf("note = %q, want it not to contain %q", note, notWant)
				}
			}
		})
	}
}

// The record's spend is a read of what each attempt recorded, never a function of
// how many attempts ran at which tier.
func TestRoutingRecord_UsageTotalsReadFromTheAttemptsNotTheCounters(t *testing.T) {
	record := &RoutingRecord{
		SonnetAttempts:        2,
		OpusExecutionAttempts: 1,
		Attempts: []ExecutorAttempt{
			{Iteration: 1, Role: ExecutorRoleDefault, Usage: &AttemptUsage{
				Available: true, InputTokens: 100, OutputTokens: 20, CostUSD: 0.5, CostBasis: CostBasisCharged,
			}},
			{Iteration: 2, Role: ExecutorRoleDefault, Usage: UnavailableUsage("no usage")},
			{Iteration: 3, Role: ExecutorRoleEscalated, Usage: &AttemptUsage{
				Available: true, InputTokens: 400, CostUSD: 4.0, CostBasis: CostBasisListPriceEstimate,
			}},
		},
	}

	totals := record.UsageTotals()

	if totals.Entries != 3 || totals.Available != 2 || totals.Unavailable != 1 {
		t.Errorf("totals = %+v, want 3 attempts, 2 readable and 1 unavailable", totals)
	}
	if got := totals.TotalTokens(); got != 520 {
		t.Errorf("tokens = %d, want only the readable attempts' 520", got)
	}
	if totals.ChargedCostUSD != 0.5 || totals.ListPriceCostUSD != 4.0 {
		t.Errorf("cost = %v charged / %v list-price, want them kept apart", totals.ChargedCostUSD, totals.ListPriceCostUSD)
	}
	if totals.Complete() {
		t.Error("a record with an unavailable attempt reports complete totals; it must report a floor")
	}
}

func TestSourceType_IsSystemTruth(t *testing.T) {
	tests := []struct {
		name   string
		source SourceType
		want   bool
	}{
		{name: "exploratory is not system truth", source: SourceExploratory, want: false},
		{name: "executed conversation is system truth", source: SourceSystemTruth, want: true},
		{name: "execution run is system truth", source: SourceExecution, want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.source.IsSystemTruth(); got != tt.want {
				t.Errorf("%s.IsSystemTruth() = %v, want %v", tt.source, got, tt.want)
			}
		})
	}
}

func TestConversation_Type(t *testing.T) {
	tests := []struct {
		name string
		conv *Conversation
		want SourceType
	}{
		{
			name: "no CR is exploratory",
			conv: &Conversation{ID: "conv-1"},
			want: SourceExploratory,
		},
		{
			name: "CR without execution is exploratory",
			conv: &Conversation{ID: "conv-2", CRsCreated: []CRCommit{{CRID: "cr-1"}}},
			want: SourceExploratory,
		},
		{
			name: "CR with execution is system truth",
			conv: &Conversation{
				ID:           "conv-3",
				CRsCreated:   []CRCommit{{CRID: "cr-1"}},
				ExecutionLog: []ExecutionLogEntry{{WorkItemID: "wi-1"}},
			},
			want: SourceSystemTruth,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.conv.Type(); got != tt.want {
				t.Errorf("Type() = %q, want %q", got, tt.want)
			}
			if got, want := tt.conv.IsSystemTruth(), tt.want.IsSystemTruth(); got != want {
				t.Errorf("IsSystemTruth() = %v, want %v", got, want)
			}
		})
	}
}

// An execution run is its own source type, and it is system-truth whatever its
// outcome: a failed run still records real decisions taken against real code.
func TestExecutionRun_Type(t *testing.T) {
	for _, outcome := range []RunOutcome{RunCompleted, RunFailed} {
		t.Run(string(outcome), func(t *testing.T) {
			run := &ExecutionRun{WorkItemID: "wi-1", CRID: "cr-1", Outcome: outcome}

			if got := run.Type(); got != SourceExecution {
				t.Errorf("Type() = %q, want %q", got, SourceExecution)
			}
			if !run.IsSystemTruth() {
				t.Error("execution runs should be system truth")
			}
			if run.Type() == SourceSystemTruth {
				t.Error("execution runs should be a distinct source type, not conflated with executed conversations")
			}
		})
	}
}

// A run carries the same processing state as a conversation, and a run written
// before harvest tracked that state must still be harvested rather than
// silently skipped.
func TestExecutionRun_IsUnprocessed(t *testing.T) {
	tests := []struct {
		name   string
		status ConversationStatus
		want   bool
	}{
		{name: "no status field (written before runs were tracked)", status: "", want: true},
		{name: "unprocessed", status: ConversationUnprocessed, want: true},
		{name: "processed", status: ConversationProcessed, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			run := &ExecutionRun{WorkItemID: "wi-1", CRID: "cr-1", Status: tt.status}

			if got := run.IsUnprocessed(); got != tt.want {
				t.Errorf("IsUnprocessed() with status %q = %v, want %v", tt.status, got, tt.want)
			}
		})
	}
}
