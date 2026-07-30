package ralph

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/leightonvanrooijen/utopia/internal"
	"github.com/leightonvanrooijen/utopia/internal/domain"
	"github.com/leightonvanrooijen/utopia/internal/ui"
)

// The step durations are an operator's answer to "where did the hour go", but
// they are still chatter about the run rather than the run's result, so every
// one of them - the per-step timings appended to the iteration status lines and
// the breakdown emitted when the item completes - goes to the diagnostic
// channel. An operator piping `utopia execute` gets what Claude produced and
// nothing about how long it took.
//
// This drives the whole loop against a stand-in claude on PATH rather than
// asserting on the rendered summary alone: the summary's shape is covered by
// TestStepTimings_SummaryReportsTotalAndPerCategoryBreakdown, and what is at
// stake here is which stream the loop hands it to.
func TestExecute_TimingLinesLandOnTheDiagnosticChannel(t *testing.T) {
	projectDir := t.TempDir()
	store := internal.NewYAMLStore(filepath.Join(projectDir, ".utopia"))
	const specID = "cr-timing/phase-1"

	item := &domain.WorkItem{ID: "wi-1", Order: 1, Status: domain.WorkItemPending, Prompt: "do the thing"}
	if err := store.SaveWorkItemForSpec(specID, item); err != nil {
		t.Fatalf("SaveWorkItemForSpec() = %v", err)
	}
	completingClaudeOnPath(t)

	var stdout, stderr bytes.Buffer
	var result *Result
	var err error
	leaked := captureStdout(t, func() {
		result, err = Execute(context.Background(), specID, store, &domain.Config{}, projectDir, "",
			Overrides{Out: ui.NewPrinter(&stdout, &stderr)})
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if result.Completed != 1 {
		t.Fatalf("Execute() completed %d of %d, want 1 - the item must reach the completion line that carries the timings", result.Completed, result.Total)
	}

	diagnostics := stderr.String()
	// A measured duration, not a fixed one: the assertion is that the loop timed
	// the Claude call as a black box and that the clock plausibly ran, the fake
	// claude having slept before it answered.
	wantClaude := regexp.MustCompile(`token found, running verification\.\.\. \(claude 0\.[1-9]s\)`)
	if !wantClaude.MatchString(diagnostics) {
		t.Errorf("the status line must carry the Claude invocation's elapsed time, got:\n%s", diagnostics)
	}
	wantSummary := regexp.MustCompile(`timing: total \d+\.\ds \(claude 0\.[1-9]s, verification 0\.0s, validators 0\.\ds\)`)
	if !wantSummary.MatchString(diagnostics) {
		t.Errorf("completion must be followed by the per-category breakdown, got:\n%s", diagnostics)
	}

	// The result stream carries what Claude produced and nothing about how long
	// the loop took to get it.
	if got := stdout.String(); !strings.Contains(got, CompletionToken) {
		t.Errorf("captured stdout = %q, want Claude's own output", got)
	}
	for _, banned := range []string{"timing:", "claude 0.", "Iteration 1:"} {
		if strings.Contains(stdout.String(), banned) {
			t.Errorf("captured stdout = %q, must not carry %q", stdout.String(), banned)
		}
	}
	if leaked != "" {
		t.Errorf("Execute() wrote %q to the process stdout, want nothing", leaked)
	}
}

// completingClaudeOnPath installs a stand-in claude that answers every prompt
// with a completion token, in the stream-json shape the execution loop asks for.
// It sleeps first so the loop's start-marker-to-elapsed bracket measures a
// duration a test can tell apart from a zeroed clock.
//
// It goes on PATH rather than being injected because Execute builds its own
// *internal.CLI: only a real spawn exercises the bracket the loop puts around it.
func completingClaudeOnPath(t *testing.T) {
	t.Helper()

	dir := t.TempDir()
	script := "#!/bin/sh\n" +
		"sleep 0.2\n" +
		`echo '{"type":"system","subtype":"init","model":"claude-test"}'` + "\n" +
		`echo '{"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"text_delta","text":"done <COMPLETE>"}}}'` + "\n" +
		`echo '{"type":"result","subtype":"success","result":"done <COMPLETE>"}'` + "\n"
	if err := os.WriteFile(filepath.Join(dir, "claude"), []byte(script), 0o755); err != nil {
		t.Fatalf("failed to write fake claude: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}
