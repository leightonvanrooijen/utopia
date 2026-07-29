package domain

import (
	"strings"
	"testing"
)

// A rewrite block is the provenance of a change request produced by a scoping
// escalation. It is optional, but an incomplete one is worse than none: the
// artefact exists precisely so a later reader can trace what it replaced and on
// what evidence.
func TestValidateChangeRequest_RewriteProvenance(t *testing.T) {
	valid := func() *ChangeRequest {
		return &ChangeRequest{
			ID:     "escalation-routing-rewrite-1",
			Type:   CRTypeFeature,
			Title:  "Rewrite",
			Status: ChangeRequestDraft,
			Changes: []Change{{
				Operation: "add",
				Spec:      "escalation-routing",
				Feature:   &Feature{ID: "scoping-escalation", Description: "d", AcceptanceCriteria: []string{"c"}},
			}},
			Rewrite: &ScopingRewrite{Supersedes: "escalation-routing", Diagnoses: []string{"spec-fidelity: term was ambiguous"}},
		}
	}

	tests := []struct {
		name    string
		mutate  func(cr *ChangeRequest)
		wantErr string
	}{
		{"complete provenance", func(*ChangeRequest) {}, ""},
		{"no rewrite block at all", func(cr *ChangeRequest) { cr.Rewrite = nil }, ""},
		{"missing supersedes", func(cr *ChangeRequest) { cr.Rewrite.Supersedes = "" }, "missing required field: supersedes"},
		{"missing diagnoses", func(cr *ChangeRequest) { cr.Rewrite.Diagnoses = nil }, "missing required field: diagnoses"},
		{"supersedes itself", func(cr *ChangeRequest) { cr.Rewrite.Supersedes = cr.ID }, "own id"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cr := valid()
			tt.mutate(cr)

			err := ValidateChangeRequest(cr)

			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("ValidateChangeRequest() = %v, want nil", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("ValidateChangeRequest() = %v, want an error mentioning %q", err, tt.wantErr)
			}
		})
	}
}

func TestIsRewrite(t *testing.T) {
	if (&ChangeRequest{}).IsRewrite() {
		t.Error("a change request with no rewrite block reported itself as a rewrite")
	}
	if !(&ChangeRequest{Rewrite: &ScopingRewrite{}}).IsRewrite() {
		t.Error("a change request carrying a rewrite block did not report itself as a rewrite")
	}
}
