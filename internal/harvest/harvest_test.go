package harvest

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/leightonvanrooijen/utopia/internal"
	"github.com/leightonvanrooijen/utopia/internal/domain"
)

// Runs and conversations are two halves of one harvest queue, and only an empty
// queue skips the session. A project with neither directory yet must report zero
// of both rather than failing - that is the outcome the command frames as
// "nothing to harvest".
func TestRun_NoSourcesSkipsTheSession(t *testing.T) {
	utopiaDir := filepath.Join(t.TempDir(), ".utopia")
	if err := os.MkdirAll(utopiaDir, 0755); err != nil {
		t.Fatalf("failed to create .utopia dir: %v", err)
	}

	result, err := Run(context.Background(), internal.NewYAMLStore(utopiaDir), Options{UtopiaDir: utopiaDir})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.UnprocessedConversations != 0 || result.UnprocessedRuns != 0 {
		t.Errorf("Run() = %+v, want zero counts for both source types", result)
	}
}

// Runs are an opt-in source: without them, unprocessed runs on disk are counted
// but do not on their own justify the cost of a session. The runs stay
// unprocessed, so a later --runs harvest still sees them.
func TestRun_WithoutIncludeRunsSkipsTheSessionWhenOnlyRunsAreUnprocessed(t *testing.T) {
	utopiaDir := filepath.Join(t.TempDir(), ".utopia")
	if err := os.MkdirAll(utopiaDir, 0755); err != nil {
		t.Fatalf("failed to create .utopia dir: %v", err)
	}
	store := internal.NewYAMLStore(utopiaDir)
	run := &domain.ExecutionRun{
		WorkItemID: "wi-1",
		CRID:       "cr-001",
		Outcome:    domain.RunCompleted,
		Transcript: "chose a per-CR directory layout",
	}
	if err := store.SaveExecutionRun(run); err != nil {
		t.Fatalf("failed to save execution run: %v", err)
	}

	// A session here would need a live Claude CLI; returning before starting one
	// is exactly the behaviour under test.
	result, err := Run(context.Background(), store, Options{UtopiaDir: utopiaDir})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.UnprocessedRuns != 1 {
		t.Errorf("UnprocessedRuns = %d, want 1 - the run should still be counted from disk", result.UnprocessedRuns)
	}

	// The run must be left alone so a later --runs harvest can still read it.
	stillUnprocessed, err := store.ListUnprocessedExecutionRuns()
	if err != nil {
		t.Fatalf("ListUnprocessedExecutionRuns() error = %v", err)
	}
	if len(stillUnprocessed) != 1 {
		t.Errorf("got %d unprocessed runs after harvest, want 1", len(stillUnprocessed))
	}
}

func TestFormatRunsExcludedHint(t *testing.T) {
	tests := []struct {
		name        string
		includeRuns bool
		runCount    int
		want        string
	}{
		{
			name:     "names the count and the flag",
			runCount: 3,
			want:     "3 unprocessed execution runs not scanned - pass --runs to include them.",
		},
		{
			name:     "singular",
			runCount: 1,
			want:     "1 unprocessed execution run not scanned - pass --runs to include them.",
		},
		{
			name:     "no runs, no hint",
			runCount: 0,
			want:     "",
		},
		{
			name:        "runs opted in, no hint",
			includeRuns: true,
			runCount:    3,
			want:        "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := formatRunsExcludedHint(tt.includeRuns, tt.runCount); got != tt.want {
				t.Errorf("formatRunsExcludedHint(%v, %d) = %q, want %q", tt.includeRuns, tt.runCount, got, tt.want)
			}
		})
	}
}

// The session must never be told to mark files it was not shown.
func TestBuildMarkProcessedInstructions(t *testing.T) {
	withRuns := buildMarkProcessedInstructions("/p/.utopia/conversations", "/p/.utopia/runs", true)
	if !strings.Contains(withRuns, "/p/.utopia/runs/{run-file}") {
		t.Errorf("runs included should name the runs directory, got:\n%s", withRuns)
	}

	withoutRuns := buildMarkProcessedInstructions("/p/.utopia/conversations", "/p/.utopia/runs", false)
	if !strings.Contains(withoutRuns, "/p/.utopia/conversations/{id}.yaml") {
		t.Errorf("conversations are always marked, got:\n%s", withoutRuns)
	}
	if strings.Contains(withoutRuns, "{run-file}") {
		t.Errorf("runs excluded should carry no run-marking procedure, got:\n%s", withoutRuns)
	}
	if !strings.Contains(withoutRuns, "were NOT scanned") {
		t.Errorf("runs excluded should say so explicitly, got:\n%s", withoutRuns)
	}
}

// A prompt built without runs must embed no run transcript - that embedding is
// the whole cost the flag exists to avoid.
func TestBuildHarvestSourcesSummary_ExcludedRunsEmbedNoTranscript(t *testing.T) {
	conv := &domain.Conversation{
		ID:         "conv-explore",
		Timestamp:  time.Date(2026, 2, 16, 10, 0, 0, 0, time.UTC),
		Transcript: "just exploring options",
	}

	got := buildHarvestSourcesSummary([]*domain.Conversation{conv}, nil, nil)

	if strings.Contains(got, "## Execution Runs") {
		t.Errorf("no runs passed should emit no Execution Runs section, got:\n%s", got)
	}
	if strings.Contains(got, "chose a per-CR directory layout") {
		t.Errorf("no run transcript should appear, got:\n%s", got)
	}
}

func TestGetNextADRID(t *testing.T) {
	tests := []struct {
		name string
		adrs []*domain.ADR
		want string
	}{
		{
			name: "no existing ADRs",
			adrs: nil,
			want: "ADR-001",
		},
		{
			name: "sequential ADRs",
			adrs: []*domain.ADR{{ID: "ADR-001"}, {ID: "ADR-002"}},
			want: "ADR-003",
		},
		{
			name: "gaps use max",
			adrs: []*domain.ADR{{ID: "ADR-001"}, {ID: "ADR-007"}},
			want: "ADR-008",
		},
		{
			name: "malformed IDs ignored",
			adrs: []*domain.ADR{{ID: "adr-5"}, {ID: "ADR-abc"}, {ID: "ADR-002"}},
			want: "ADR-003",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := getNextADRID(tt.adrs); got != tt.want {
				t.Errorf("getNextADRID() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestBuildHarvestSourcesSummary_Empty(t *testing.T) {
	got := buildHarvestSourcesSummary(nil, nil, nil)
	if got != "(No unprocessed sources found)" {
		t.Errorf("unexpected summary: %q", got)
	}
}

func TestBuildHarvestSourcesSummary_GroupsByType(t *testing.T) {
	systemTruth := &domain.Conversation{
		ID:         "conv-system",
		Timestamp:  time.Date(2026, 2, 17, 10, 0, 0, 0, time.UTC),
		Branch:     "main",
		CRsCreated: []domain.CRCommit{{CRID: "cr-001", CommitSHA: "abcdef1234567890"}},
		ExecutionLog: []domain.ExecutionLogEntry{
			{WorkItemID: "wi-1", SpecRef: "spec.feature", Operation: "add"},
		},
		Commits:    []string{"abcdef1234567890"},
		Transcript: "we decided to use YAML",
	}
	exploratory := &domain.Conversation{
		ID:         "conv-explore",
		Timestamp:  time.Date(2026, 2, 16, 10, 0, 0, 0, time.UTC),
		Branch:     "main",
		Transcript: "just exploring options",
	}

	got := buildHarvestSourcesSummary([]*domain.Conversation{exploratory, systemTruth}, nil, nil)

	if !strings.Contains(got, "**Total: 2 sources (0 execution runs, 1 system-truth conversations, 1 exploratory conversations)**") {
		t.Errorf("missing total line, got:\n%s", got)
	}
	systemIdx := strings.Index(got, "## System-Truth Conversations")
	exploreIdx := strings.Index(got, "## Exploratory Conversations")
	if systemIdx == -1 || exploreIdx == -1 {
		t.Fatalf("missing type section headers, got:\n%s", got)
	}
	if systemIdx > exploreIdx {
		t.Error("system-truth section should come before exploratory section")
	}
	if !strings.Contains(got, "**CRs Created:** cr-001") {
		t.Errorf("missing CRs created line, got:\n%s", got)
	}
	if !strings.Contains(got, "**Executed WorkItems:** 1") {
		t.Errorf("missing executed workitems line, got:\n%s", got)
	}
	if !strings.Contains(got, "**Commits:** abcdef12") {
		t.Errorf("expected abbreviated commit sha, got:\n%s", got)
	}
}

func TestBuildHarvestSourcesSummary_SortsNewestFirst(t *testing.T) {
	older := &domain.Conversation{ID: "conv-older", Timestamp: time.Date(2026, 2, 15, 0, 0, 0, 0, time.UTC)}
	newer := &domain.Conversation{ID: "conv-newer", Timestamp: time.Date(2026, 2, 17, 0, 0, 0, 0, time.UTC)}

	got := buildHarvestSourcesSummary([]*domain.Conversation{older, newer}, nil, nil)

	newerIdx := strings.Index(got, "### conv-newer")
	olderIdx := strings.Index(got, "### conv-older")
	if newerIdx == -1 || olderIdx == -1 {
		t.Fatalf("missing conversation headers, got:\n%s", got)
	}
	if newerIdx > olderIdx {
		t.Error("conversations should be sorted newest first")
	}
}

func TestBuildHarvestSourcesSummary_PrioritizesExecutionRuns(t *testing.T) {
	systemTruth := &domain.Conversation{
		ID:           "conv-system",
		Timestamp:    time.Date(2026, 2, 17, 10, 0, 0, 0, time.UTC),
		CRsCreated:   []domain.CRCommit{{CRID: "cr-001"}},
		ExecutionLog: []domain.ExecutionLogEntry{{WorkItemID: "wi-1"}},
		Transcript:   "we decided to use YAML",
	}
	exploratory := &domain.Conversation{
		ID:         "conv-explore",
		Timestamp:  time.Date(2026, 2, 16, 10, 0, 0, 0, time.UTC),
		Transcript: "just exploring options",
	}
	run := &domain.ExecutionRun{
		WorkItemID:  "cr-001-phase-0-add-thing",
		CRID:        "cr-001",
		SpecRef:     "spec.feature",
		Iterations:  2,
		CompletedAt: time.Date(2026, 2, 18, 10, 0, 0, 0, time.UTC),
		Outcome:     domain.RunCompleted,
		Transcript:  "chose a per-CR directory layout",
	}

	got := buildHarvestSourcesSummary([]*domain.Conversation{exploratory, systemTruth}, []*domain.ExecutionRun{run}, nil)

	if !strings.Contains(got, "**Total: 3 sources (1 execution runs, 1 system-truth conversations, 1 exploratory conversations)**") {
		t.Errorf("missing total line, got:\n%s", got)
	}
	runIdx := strings.Index(got, "## Execution Runs")
	systemIdx := strings.Index(got, "## System-Truth Conversations")
	exploreIdx := strings.Index(got, "## Exploratory Conversations")
	if runIdx == -1 || systemIdx == -1 || exploreIdx == -1 {
		t.Fatalf("missing source type section headers, got:\n%s", got)
	}
	// Runs must rank at least as highly as executed conversations for ADR signals.
	if runIdx > systemIdx || systemIdx > exploreIdx {
		t.Errorf("sections should be ordered runs, system-truth, exploratory, got:\n%s", got)
	}
}

func TestBuildHarvestSourcesSummary_RunsOnly(t *testing.T) {
	run := &domain.ExecutionRun{WorkItemID: "wi-1", CRID: "cr-001", Outcome: domain.RunFailed}

	got := buildHarvestSourcesSummary(nil, []*domain.ExecutionRun{run}, nil)

	if !strings.Contains(got, "## Execution Runs") {
		t.Errorf("runs alone should still produce a summary, got:\n%s", got)
	}
}

func TestWriteExecutionRunSummary(t *testing.T) {
	var sb strings.Builder
	run := &domain.ExecutionRun{
		WorkItemID:  "cr-001-phase-0-add-thing",
		CRID:        "cr-001",
		SpecRef:     "spec.feature",
		Iterations:  1,
		CompletedAt: time.Date(2026, 2, 18, 9, 30, 0, 0, time.UTC),
		Outcome:     domain.RunCompleted,
		Transcript:  strings.Repeat("x", 3000),
	}

	writeExecutionRunSummary(&sb, run, nil)

	got := sb.String()
	for _, want := range []string{
		"### cr-001-phase-0-add-thing",
		"**Type:** execution",
		"**CR:** cr-001",
		"**Spec Ref:** spec.feature",
		"**Outcome:** completed (1 iteration)",
		"**Completed:** 2026-02-18 09:30",
		"**Run File:** cr-001/cr-001-phase-0-add-thing.yaml",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q, got:\n%s", want, got)
		}
	}
	// A run whose CR was never discussed in a captured conversation says so,
	// rather than leaving the cross-reference to be invented.
	if !strings.Contains(got, "**Originating Conversation:** (none found") {
		t.Errorf("expected an explicit no-origin marker, got:\n%s", got)
	}
	if !strings.Contains(got, "... [transcript truncated for length]") {
		t.Error("expected long run transcripts to be truncated like conversations")
	}
}

// A decision found in a run has to be traceable to the CR it was built under
// and the conversation that asked for the work.
func TestWriteExecutionRunSummary_CrossReferencesOriginatingConversations(t *testing.T) {
	var sb strings.Builder
	run := &domain.ExecutionRun{WorkItemID: "wi-1", CRID: "cr-001", Outcome: domain.RunCompleted}

	writeExecutionRunSummary(&sb, run, []string{"conv-a", "conv-b"})

	got := sb.String()
	if !strings.Contains(got, "**CR:** cr-001") {
		t.Errorf("missing CR cross-reference, got:\n%s", got)
	}
	if !strings.Contains(got, "**Originating Conversation:** conv-a, conv-b") {
		t.Errorf("missing conversation cross-reference, got:\n%s", got)
	}
}

func TestIndexRunOrigins(t *testing.T) {
	convs := []*domain.Conversation{
		{
			ID:         "conv-a",
			Status:     domain.ConversationProcessed,
			CRsCreated: []domain.CRCommit{{CRID: "cr-001"}, {CRID: "cr-002"}},
		},
		{
			ID:         "conv-b",
			Status:     domain.ConversationUnprocessed,
			CRsCreated: []domain.CRCommit{{CRID: "cr-001"}},
		},
		{ID: "conv-exploratory"},
	}

	origins := indexRunOrigins(convs)

	// conv-a is already processed and still has to appear: a run's origin is
	// usually a conversation an earlier harvest consumed.
	if got, want := strings.Join(origins["cr-001"], ","), "conv-a,conv-b"; got != want {
		t.Errorf("origins[cr-001] = %q, want %q", got, want)
	}
	if got, want := strings.Join(origins["cr-002"], ","), "conv-a"; got != want {
		t.Errorf("origins[cr-002] = %q, want %q", got, want)
	}
	if len(origins) != 2 {
		t.Errorf("a conversation with no CRs should contribute no origins, got %v", origins)
	}
}

func TestBuildHarvestSourcesSummary_JoinsRunsToTheirConversations(t *testing.T) {
	conv := &domain.Conversation{
		ID:           "conv-system",
		Timestamp:    time.Date(2026, 2, 17, 10, 0, 0, 0, time.UTC),
		CRsCreated:   []domain.CRCommit{{CRID: "cr-001"}},
		ExecutionLog: []domain.ExecutionLogEntry{{WorkItemID: "wi-1"}},
	}
	run := &domain.ExecutionRun{WorkItemID: "wi-1", CRID: "cr-001", Outcome: domain.RunCompleted}

	got := buildHarvestSourcesSummary(
		[]*domain.Conversation{conv},
		[]*domain.ExecutionRun{run},
		indexRunOrigins([]*domain.Conversation{conv}),
	)

	if !strings.Contains(got, "**Originating Conversation:** conv-system") {
		t.Errorf("run should be cross-referenced to the conversation that created its CR, got:\n%s", got)
	}
}

func TestBuildHarvestSourcesSummary_SortsRunsNewestFirst(t *testing.T) {
	older := &domain.ExecutionRun{WorkItemID: "wi-older", CRID: "cr-001", CompletedAt: time.Date(2026, 2, 15, 0, 0, 0, 0, time.UTC)}
	newer := &domain.ExecutionRun{WorkItemID: "wi-newer", CRID: "cr-001", CompletedAt: time.Date(2026, 2, 17, 0, 0, 0, 0, time.UTC)}
	runs := []*domain.ExecutionRun{older, newer}

	got := buildHarvestSourcesSummary(nil, runs, nil)

	newerIdx := strings.Index(got, "### wi-newer")
	olderIdx := strings.Index(got, "### wi-older")
	if newerIdx == -1 || olderIdx == -1 {
		t.Fatalf("missing run headers, got:\n%s", got)
	}
	if newerIdx > olderIdx {
		t.Error("runs should be sorted newest first")
	}
	if runs[0] != older {
		t.Error("caller's slice should not be reordered")
	}
}

func TestWriteConversationSummary_TruncatesLongTranscript(t *testing.T) {
	var sb strings.Builder
	conv := &domain.Conversation{
		ID:         "conv-long",
		Timestamp:  time.Date(2026, 2, 17, 0, 0, 0, 0, time.UTC),
		Transcript: strings.Repeat("x", 3000),
	}

	writeConversationSummary(&sb, conv)

	got := sb.String()
	if !strings.Contains(got, "... [transcript truncated for length]") {
		t.Error("expected truncation marker for long transcript")
	}
	if strings.Contains(got, strings.Repeat("x", 2001)) {
		t.Error("transcript should be capped at 2000 characters")
	}
}

func TestWriteConversationSummary_EmptyTranscript(t *testing.T) {
	var sb strings.Builder
	conv := &domain.Conversation{ID: "conv-empty", Timestamp: time.Date(2026, 2, 17, 0, 0, 0, 0, time.UTC)}

	writeConversationSummary(&sb, conv)

	if !strings.Contains(sb.String(), "**Transcript:** (empty)") {
		t.Errorf("expected empty transcript marker, got:\n%s", sb.String())
	}
}

func TestBuildHarvestADRsSummary(t *testing.T) {
	if got := buildHarvestADRsSummary(nil); got != "(No existing ADRs)" {
		t.Errorf("empty summary = %q", got)
	}

	adrs := []*domain.ADR{
		{ID: "ADR-001", Title: "Use YAML", Status: domain.ADRStatusAccepted, Context: strings.Repeat("c", 150)},
	}
	got := buildHarvestADRsSummary(adrs)
	if !strings.Contains(got, "- **ADR-001**: Use YAML (accepted)") {
		t.Errorf("missing ADR line, got:\n%s", got)
	}
	if !strings.Contains(got, strings.Repeat("c", 100)+"...") {
		t.Errorf("expected context truncated to 100 chars with ellipsis, got:\n%s", got)
	}
}

func TestBuildHarvestConceptsSummary(t *testing.T) {
	if got := buildHarvestConceptsSummary(nil); got != "(No existing Concepts)" {
		t.Errorf("empty summary = %q", got)
	}

	concepts := []*domain.ConceptDoc{
		{ID: "yaml-tradeoffs", Title: "YAML Trade-offs", Status: domain.ConceptStatusDraft, RelatedADRs: []string{"ADR-001", "ADR-002"}},
	}
	got := buildHarvestConceptsSummary(concepts)
	if !strings.Contains(got, "- **yaml-tradeoffs**: YAML Trade-offs (draft)") {
		t.Errorf("missing concept line, got:\n%s", got)
	}
	if !strings.Contains(got, "Related ADRs: ADR-001, ADR-002") {
		t.Errorf("missing related ADRs line, got:\n%s", got)
	}
}

func TestBuildHarvestDomainDocsSummary(t *testing.T) {
	if got := buildHarvestDomainDocsSummary(nil); got != "(No existing Domain Docs)" {
		t.Errorf("empty summary = %q", got)
	}

	docs := []*domain.DomainDoc{
		{
			ID:       "conversations",
			Title:    "Conversation Lifecycle",
			Terms:    []domain.DomainTerm{{Term: "unprocessed"}, {Term: "system-truth"}},
			Entities: []domain.DomainEntity{{Name: "Conversation"}},
		},
	}
	got := buildHarvestDomainDocsSummary(docs)
	if !strings.Contains(got, "- **conversations**: Conversation Lifecycle") {
		t.Errorf("missing doc line, got:\n%s", got)
	}
	if !strings.Contains(got, "Terms: unprocessed, system-truth") {
		t.Errorf("missing terms line, got:\n%s", got)
	}
	if !strings.Contains(got, "Entities: Conversation") {
		t.Errorf("missing entities line, got:\n%s", got)
	}
}

// The run-sourced domain rule tells the harvest to treat a run's different word
// for a documented term as an alias rather than a new term. It can only do that
// if the aliases already recorded are in front of it.
func TestBuildHarvestDomainDocsSummary_ShowsRecordedAliases(t *testing.T) {
	docs := []*domain.DomainDoc{
		{
			ID:    "conversations",
			Title: "Conversation Lifecycle",
			Terms: []domain.DomainTerm{
				{Term: "execution run", Aliases: []string{"run transcript", "ralph run"}},
				{Term: "unprocessed"},
			},
		},
	}

	got := buildHarvestDomainDocsSummary(docs)
	if !strings.Contains(got, "execution run (aliases: run transcript, ralph run)") {
		t.Errorf("aliases not surfaced for the term that has them, got:\n%s", got)
	}
	if strings.Contains(got, "unprocessed (aliases") {
		t.Errorf("term with no aliases should render bare, got:\n%s", got)
	}
}

func TestPluralize(t *testing.T) {
	if got := pluralize(1); got != "" {
		t.Errorf("pluralize(1) = %q, want empty", got)
	}
	if got := pluralize(0); got != "s" {
		t.Errorf("pluralize(0) = %q, want \"s\"", got)
	}
	if got := pluralize(2); got != "s" {
		t.Errorf("pluralize(2) = %q, want \"s\"", got)
	}
}

func TestFormatSignalCount(t *testing.T) {
	if got := formatSignalCount(3); got != "3" {
		t.Errorf("formatSignalCount(3) = %q, want \"3\"", got)
	}
}

func TestHarvestSystemPromptComposes(t *testing.T) {
	// The prompt template has exactly eleven %s verbs; a stray verb (or an
	// unescaped % in the payload) would surface as a MISSING/EXTRA marker.
	prompt := composeTestPrompt()
	if strings.Contains(prompt, "%!") || strings.Contains(prompt, "(MISSING)") || strings.Contains(prompt, "(EXTRA") {
		t.Errorf("system prompt formatting produced error markers:\n%s", snippetAround(prompt, "%!"))
	}
	for _, want := range []string{
		"CONVS", "ADRS", "CONCEPTS", "DOMAINDOCS", "READMESIGNALS", "REWRITES",
		"/adrs-dir", "/concepts-dir", "/domain-dir",
		"/conversations-dir", "/runs-dir", "ADR-042",
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("composed prompt missing injected value %q", want)
		}
	}
}

// The ADR qualification test is one test, applied to runs as well as
// conversations, and the prompt is where that instruction lives: it has to say
// how the test reads against a decision made while building, what a run-sourced
// candidate is cross-referenced to, and where a reviewed run gets marked
// processed.
func TestHarvestSystemPrompt_CoversRunSourcedADRs(t *testing.T) {
	prompt := composeTestPrompt()

	for _, want := range []string{
		"Applying this test to implementation decisions in execution runs",
		"The Category Test and\nReversal Cost Test decide",
		"Cross-reference every candidate surfaced from a run to BOTH its CR and its\n  originating conversation",
		"source_runs",
		"Read each run file from /runs-dir/{run-file}",
		`Set status to "processed"`,
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("system prompt missing run-sourced ADR instruction %q", want)
		}
	}
}

// Runs are scanned for canonical terms, not only for decisions, and the same
// ambiguity-without-definition test applies to a term coined at the code. The
// prompt has to say so, guard the one test a run-coined term passes for free,
// and give the term somewhere to record the run it came from.
func TestHarvestSystemPrompt_CoversRunSourcedDomainTerms(t *testing.T) {
	prompt := composeTestPrompt()

	for _, want := range []string{
		"Scan them for Domain candidates too",
		"Applying this test to terms introduced in execution runs",
		"The ambiguity litmus test decides; where the term was coined\ndoes not",
		"passes the Code Alignment Test by construction",
		"has introduced an\n  alias, not a new term",
		"required when the term was coined in an execution run",
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("system prompt missing run-sourced domain instruction %q", want)
		}
	}
}

// Code alignment is what the Domain rubric reads as HIGH confidence, and a
// run-coined term satisfies it by construction - so the rubric would hand every
// run term HIGH regardless of the ambiguity test. The prompt has to break that
// tie in favour of the ambiguity test.
func TestHarvestSystemPrompt_RunCoinedTermConfidenceFollowsAmbiguityTest(t *testing.T) {
	prompt := composeTestPrompt()

	if !strings.Contains(prompt, `code presence is a given and carries no
      confidence signal`) {
		t.Errorf("confidence rubric lets code presence stand in for a run term's ambiguity test:\n%s", snippetAround(prompt, "  - For Domain:"))
	}
	if !strings.Contains(prompt, "borderline is MEDIUM, not HIGH") {
		t.Error("prompt does not cap a borderline run-coined term below HIGH")
	}
}

// A term found in a run is traceable only through that run, so the candidate
// table has to carry its origin - the same rule the ADR table states.
func TestHarvestSystemPrompt_DomainCandidatesReportRunOrigin(t *testing.T) {
	prompt := composeTestPrompt()

	domainTable := prompt[strings.Index(prompt, "### Domain Candidates (Qualified)"):]
	if !strings.Contains(domainTable, "| Origin (CR / Conversation) |") {
		t.Errorf("domain candidate table missing origin column:\n%s", snippetAround(prompt, "### Domain Candidates (Qualified)"))
	}
	if !strings.Contains(prompt, `required for every term whose source
type is execution`) {
		t.Error("prompt does not require an origin for execution-sourced terms")
	}
}

// Marking a source processed is a file write, and the harvest session inherits
// whatever working directory utopia was invoked from - not necessarily the
// project --project resolved to. Both source directories are therefore injected
// rather than assumed, or the write lands outside the project the sources came
// from and the run silently stays unprocessed forever.
func TestHarvestSystemPrompt_MarksSourcesProcessedAtResolvedPaths(t *testing.T) {
	prompt := composeTestPrompt()

	for _, want := range []string{
		"Read each conversation file from /conversations-dir/{id}.yaml",
		"Read each run file from /runs-dir/{run-file}",
		"do NOT rebuild a path from a working directory",
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("system prompt missing resolved-path instruction %q", want)
		}
	}
	if strings.Contains(prompt, ".utopia/conversations/") || strings.Contains(prompt, ".utopia/runs/") {
		t.Errorf("source paths must come from the store, not hardcoded .utopia literals:\n%s", snippetAround(prompt, ".utopia/"))
	}
}

// Runs are marked processed at the same point in the session as conversations -
// after the user completes or exits, never as each run is reviewed - so a run
// abandoned mid-harvest is still there next time.
func TestHarvestSystemPrompt_DefersMarkingRunsUntilTheSessionEnds(t *testing.T) {
	prompt := composeTestPrompt()

	for _, want := range []string{
		"### PHASE 5: MARK PROCESSED\nAfter the user completes or exits the harvest:",
		"Mark ALL reviewed sources as processed - conversations and execution runs",
		"ONLY mark conversations and execution runs processed after user completes or exits",
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("system prompt missing deferred-marking instruction %q", want)
		}
	}
}

// Without --runs the session never sees a run, so every instruction that would
// send it into the runs directory has to be gone from the prompt too.
func TestHarvestSystemPrompt_OmitsRunMarkingWhenRunsExcluded(t *testing.T) {
	prompt := composeTestPromptWithoutRuns()

	if strings.Contains(prompt, "/runs-dir") {
		t.Errorf("runs excluded should not name the runs directory:\n%s", snippetAround(prompt, "/runs-dir"))
	}
	for _, unwanted := range []string{
		"Mark ALL reviewed sources as processed - conversations and execution runs",
		"ONLY mark conversations and execution runs processed",
	} {
		if strings.Contains(prompt, unwanted) {
			t.Errorf("runs excluded but prompt still instructs marking them: %q", unwanted)
		}
	}
	if !strings.Contains(prompt, "ONLY mark conversations processed after user completes or exits") {
		t.Errorf("conversations must still be marked processed:\n%s", snippetAround(prompt, "ONLY mark"))
	}
}

// composeTestPrompt builds the prompt the way Run does for a harvest that opted
// execution runs in; composeTestPromptWithoutRuns is the conversations-only
// counterpart.
func composeTestPrompt() string { return composeTestPromptIncludingRuns(true) }

func composeTestPromptWithoutRuns() string { return composeTestPromptIncludingRuns(false) }

func composeTestPromptIncludingRuns(includeRuns bool) string {
	return fmt.Sprintf(harvestSystemPrompt,
		"CONVS", "ADRS", "CONCEPTS", "DOMAINDOCS", "READMESIGNALS", "REWRITES",
		markedSourcesPhrase(includeRuns),
		"/adrs-dir", "/concepts-dir", "/domain-dir",
		buildMarkProcessedInstructions("/conversations-dir", "/runs-dir", includeRuns),
		"ADR-042", markedSourcesPhrase(includeRuns))
}

func snippetAround(s, marker string) string {
	idx := strings.Index(s, marker)
	if idx == -1 {
		return ""
	}
	start := max(0, idx-80)
	end := min(len(s), idx+80)
	return s[start:end]
}
