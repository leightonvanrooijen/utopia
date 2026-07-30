package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/leightonvanrooijen/utopia/internal/ui"
	"github.com/leightonvanrooijen/utopia/internal/domain"
	"github.com/leightonvanrooijen/utopia/internal/ralph"
)

// A run with halted items did not fail, but the phase is incomplete: reporting
// true is what stops the caller merging it as a success.
func TestReportNeedsHuman(t *testing.T) {
	var out, errOut bytes.Buffer
	printer := ui.NewPrinter(&out, &errOut)

	if reportNeedsHuman(printer, &ralph.Result{Completed: 2, Total: 2}) {
		t.Error("reported halted items for a run that had none")
	}
	if errOut.Len() != 0 || out.Len() != 0 {
		t.Errorf("printed %q / %q for a run with no halted items", out.String(), errOut.String())
	}

	if !reportNeedsHuman(printer, &ralph.Result{Completed: 1, Total: 3, NeedsHuman: []string{"item-b", "item-c"}}) {
		t.Fatal("did not report the halted items")
	}
	report := errOut.String()
	for _, want := range []string{"2 work item(s) halted", "item-b", "item-c", "Re-scope"} {
		if !strings.Contains(report, want) {
			t.Errorf("report %q missing %q", report, want)
		}
	}
}

func TestReadyChangeRequests(t *testing.T) {
	crs := []*domain.ChangeRequest{
		{ID: "a-draft", Status: domain.ChangeRequestDraft},
		{ID: "b-approved", Status: domain.ChangeRequestApproved},
		{ID: "c-in-progress", Status: domain.ChangeRequestInProgress},
		{ID: "d-complete", Status: domain.ChangeRequestComplete},
		{ID: "e-approved", Status: domain.ChangeRequestApproved},
	}

	ready := readyChangeRequests(crs)

	if len(ready) != 2 {
		t.Fatalf("expected 2 ready CRs, got %d", len(ready))
	}
	if ready[0].ID != "b-approved" || ready[1].ID != "e-approved" {
		t.Errorf("expected [b-approved e-approved] preserving input order, got [%s %s]", ready[0].ID, ready[1].ID)
	}
}

func TestReadyChangeRequestsNoneReady(t *testing.T) {
	crs := []*domain.ChangeRequest{
		{ID: "a-draft", Status: domain.ChangeRequestDraft},
		{ID: "b-in-progress", Status: domain.ChangeRequestInProgress},
	}

	if ready := readyChangeRequests(crs); len(ready) != 0 {
		t.Errorf("expected no ready CRs, got %d", len(ready))
	}
}

func TestReadyChangeRequestsEmpty(t *testing.T) {
	if ready := readyChangeRequests(nil); len(ready) != 0 {
		t.Errorf("expected no ready CRs from nil input, got %d", len(ready))
	}
}
