package cli

import (
	"testing"

	"github.com/leightonvanrooijen/utopia/internal/domain"
)

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
