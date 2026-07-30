package ralph

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"

	"github.com/leightonvanrooijen/utopia/internal"
	"github.com/leightonvanrooijen/utopia/internal/domain"
	"github.com/leightonvanrooijen/utopia/internal/ui"
)

// A run's diagnostics are only useful to whatever collects them if each record
// says which change request and work item it came from. The run attaches both as
// attributes once, so this asserts on the attributes of the record rather than on
// the text of a rendered line - reword the message and this test still holds.
func TestExecute_DiagnosticsCarryRunAttributes(t *testing.T) {
	utopiaDir := filepath.Join(t.TempDir(), ".utopia")
	store := internal.NewYAMLStore(utopiaDir)
	const specID = "06_observability-unified-output/phase-2"

	for _, item := range []*domain.WorkItem{
		{ID: "wi-1", Order: 1, Status: domain.WorkItemCompleted},
		{ID: "wi-2", Order: 2, Status: domain.WorkItemNeedsHuman},
	} {
		if err := store.SaveWorkItemForSpec(specID, item); err != nil {
			t.Fatalf("SaveWorkItemForSpec(%s) = %v", item.ID, err)
		}
	}

	// A JSON handler at debug level: the diagnostics arrive as records, not as
	// terminal lines, which is the point of routing them through slog.
	var diagnostics bytes.Buffer
	printer := ui.NewPrinter(&bytes.Buffer{}, &bytes.Buffer{}).
		WithHandler(slog.NewJSONHandler(&diagnostics, &slog.HandlerOptions{Level: slog.LevelDebug}))

	var err error
	captureStdout(t, func() {
		_, err = Execute(context.Background(), specID, store, &domain.Config{}, t.TempDir(), "",
			Overrides{Out: printer})
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	byWorkItem := map[string]map[string]any{}
	for _, line := range strings.Split(strings.TrimSpace(diagnostics.String()), "\n") {
		if line == "" {
			continue
		}
		var rec map[string]any
		if jsonErr := json.Unmarshal([]byte(line), &rec); jsonErr != nil {
			t.Fatalf("diagnostic %q is not a record: %v", line, jsonErr)
		}
		if rec["cr_id"] != "06_observability-unified-output" {
			t.Errorf("diagnostic %v cr_id = %v, want 06_observability-unified-output", rec["msg"], rec["cr_id"])
		}
		id, ok := rec["work_item_id"].(string)
		if !ok {
			t.Fatalf("diagnostic %v carries no work_item_id", rec["msg"])
		}
		// Every record above is checked for the run's attributes; the assertions
		// below are about the structured "work item reached" record, which is the
		// one carrying typed attributes rather than a rendered progress line.
		if rec["total"] != nil {
			byWorkItem[id] = rec
		}
	}

	for _, want := range []string{"wi-1", "wi-2"} {
		rec, ok := byWorkItem[want]
		if !ok {
			t.Fatalf("no diagnostic for %s, got %v", want, byWorkItem)
		}
		if rec["level"] != "DEBUG" {
			t.Errorf("%s level = %v, want DEBUG", want, rec["level"])
		}
		if rec["total"] != float64(2) {
			t.Errorf("%s total = %v, want 2", want, rec["total"])
		}
	}
	if got := byWorkItem["wi-2"]["status"]; got != string(domain.WorkItemNeedsHuman) {
		t.Errorf("wi-2 status attr = %v, want %s", got, domain.WorkItemNeedsHuman)
	}
}
