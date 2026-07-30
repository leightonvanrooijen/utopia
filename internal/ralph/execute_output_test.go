package ralph

import (
	"bytes"
	"context"
	"path/filepath"
	"testing"

	"github.com/leightonvanrooijen/utopia/internal"
	"github.com/leightonvanrooijen/utopia/internal/domain"
	"github.com/leightonvanrooijen/utopia/internal/ui"
)

// The loop's status lines are the only trace an operator has of a long run, and
// until now they went straight to the process stdout: unredirectable and
// unassertable. Execute takes a printer on its overrides, so a caller can hand in
// its own buffers and read back exactly what the run reported - and nothing
// reaches the real stdout on the way.
//
// Two items that need no Claude invocation exercise both of Execute's own report
// lines: one already completed, one halted for a human on an earlier run.
func TestExecute_ReportsThroughTheInjectedPrinter(t *testing.T) {
	utopiaDir := filepath.Join(t.TempDir(), ".utopia")
	store := internal.NewYAMLStore(utopiaDir)
	const specID = "cr-001"

	for _, item := range []*domain.WorkItem{
		{ID: "wi-1", Order: 1, Status: domain.WorkItemCompleted},
		{ID: "wi-2", Order: 2, Status: domain.WorkItemNeedsHuman},
	} {
		if err := store.SaveWorkItemForSpec(specID, item); err != nil {
			t.Fatalf("SaveWorkItemForSpec(%s) = %v", item.ID, err)
		}
	}

	var stdout, stderr bytes.Buffer
	var result *Result
	var err error
	leaked := captureStdout(t, func() {
		result, err = Execute(context.Background(), specID, store, &domain.Config{}, t.TempDir(), "",
			Overrides{Out: ui.NewPrinter(&stdout, &stderr)})
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	want := "[1/2] wi-1 - already completed\n" +
		"[2/2] wi-2 - halted, needs human attention (skipped)\n"
	if got := stdout.String(); got != want {
		t.Errorf("captured stdout = %q, want %q", got, want)
	}
	if stderr.Len() != 0 {
		t.Errorf("captured stderr = %q, want empty", stderr.String())
	}
	// A line that still reached the process stdout is a line no caller can
	// capture, which is the whole failure this test exists to catch.
	if leaked != "" {
		t.Errorf("Execute() wrote %q to the process stdout, want nothing", leaked)
	}

	if result.Completed != 1 || result.Total != 2 {
		t.Errorf("Execute() = %+v, want 1 of 2 completed", result)
	}
	if len(result.NeedsHuman) != 1 || result.NeedsHuman[0] != "wi-2" {
		t.Errorf("NeedsHuman = %v, want [wi-2]", result.NeedsHuman)
	}
}
