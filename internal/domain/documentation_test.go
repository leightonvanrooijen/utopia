package domain

import "testing"

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
