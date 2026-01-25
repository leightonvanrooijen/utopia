package domain

import (
	"strings"
	"testing"
)

func TestApplyAddChange_Feature(t *testing.T) {
	spec := NewSpec("test-spec", "Test Spec")

	change := Change{
		Operation: "add",
		Feature: &Feature{
			ID:                 "new-feature",
			Description:        "A new feature",
			AcceptanceCriteria: []string{"It works", "It's tested"},
		},
	}

	err := spec.ApplyAddChange(change)
	if err != nil {
		t.Fatalf("ApplyAddChange failed: %v", err)
	}

	if len(spec.Features) != 1 {
		t.Errorf("expected 1 feature, got %d", len(spec.Features))
	}

	if spec.Features[0].ID != "new-feature" {
		t.Errorf("expected feature ID 'new-feature', got %q", spec.Features[0].ID)
	}
}

func TestApplyAddChange_DuplicateFeatureID(t *testing.T) {
	spec := NewSpec("test-spec", "Test Spec")
	spec.AddFeature(Feature{
		ID:          "existing-feature",
		Description: "Already here",
	})

	change := Change{
		Operation: "add",
		Feature: &Feature{
			ID:          "existing-feature",
			Description: "Trying to add duplicate",
		},
	}

	err := spec.ApplyAddChange(change)
	if err == nil {
		t.Fatal("expected error for duplicate feature ID, got nil")
	}

	if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("error should mention 'already exists', got: %v", err)
	}
}

func TestApplyAddChange_DomainKnowledge(t *testing.T) {
	spec := NewSpec("test-spec", "Test Spec")

	change := Change{
		Operation: "add",
		DomainKnowledge: []string{
			"First domain knowledge",
			"Second domain knowledge",
		},
	}

	err := spec.ApplyAddChange(change)
	if err != nil {
		t.Fatalf("ApplyAddChange failed: %v", err)
	}

	if len(spec.DomainKnowledge) != 2 {
		t.Errorf("expected 2 domain knowledge items, got %d", len(spec.DomainKnowledge))
	}
}

func TestApplyAddChange_DuplicateDomainKnowledge(t *testing.T) {
	spec := NewSpec("test-spec", "Test Spec")
	spec.AddDomainKnowledge("Existing knowledge")

	change := Change{
		Operation:       "add",
		DomainKnowledge: []string{"Existing knowledge"},
	}

	err := spec.ApplyAddChange(change)
	if err == nil {
		t.Fatal("expected error for duplicate domain knowledge, got nil")
	}

	if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("error should mention 'already exists', got: %v", err)
	}
}

func TestApplyAddChange_WrongOperation(t *testing.T) {
	spec := NewSpec("test-spec", "Test Spec")

	change := Change{
		Operation: "modify",
		Feature: &Feature{
			ID:          "some-feature",
			Description: "Some description",
		},
	}

	err := spec.ApplyAddChange(change)
	if err == nil {
		t.Fatal("expected error for wrong operation, got nil")
	}

	if !strings.Contains(err.Error(), "expected 'add' operation") {
		t.Errorf("error should mention expected operation, got: %v", err)
	}
}

func TestApplyAddChange_FeatureAndDomainKnowledge(t *testing.T) {
	spec := NewSpec("test-spec", "Test Spec")

	change := Change{
		Operation: "add",
		Feature: &Feature{
			ID:                 "combo-feature",
			Description:        "Added with domain knowledge",
			AcceptanceCriteria: []string{"Works together"},
		},
		DomainKnowledge: []string{"Related knowledge"},
	}

	err := spec.ApplyAddChange(change)
	if err != nil {
		t.Fatalf("ApplyAddChange failed: %v", err)
	}

	if len(spec.Features) != 1 {
		t.Errorf("expected 1 feature, got %d", len(spec.Features))
	}

	if len(spec.DomainKnowledge) != 1 {
		t.Errorf("expected 1 domain knowledge item, got %d", len(spec.DomainKnowledge))
	}
}

func TestHasFeature(t *testing.T) {
	spec := NewSpec("test-spec", "Test Spec")
	spec.AddFeature(Feature{ID: "existing-feature", Description: "Test"})

	if !spec.HasFeature("existing-feature") {
		t.Error("HasFeature should return true for existing feature")
	}

	if spec.HasFeature("nonexistent-feature") {
		t.Error("HasFeature should return false for nonexistent feature")
	}
}

func TestHasDomainKnowledge(t *testing.T) {
	spec := NewSpec("test-spec", "Test Spec")
	spec.AddDomainKnowledge("Existing knowledge")

	if !spec.HasDomainKnowledge("Existing knowledge") {
		t.Error("HasDomainKnowledge should return true for existing knowledge")
	}

	if spec.HasDomainKnowledge("Nonexistent knowledge") {
		t.Error("HasDomainKnowledge should return false for nonexistent knowledge")
	}
}

func TestChangeRequestStatus_Constants(t *testing.T) {
	tests := []struct {
		status   ChangeRequestStatus
		expected string
	}{
		{ChangeRequestDraft, "draft"},
		{ChangeRequestApproved, "approved"},
		{ChangeRequestInProgress, "in-progress"},
		{ChangeRequestComplete, "complete"},
	}

	for _, tt := range tests {
		t.Run(string(tt.status), func(t *testing.T) {
			if string(tt.status) != tt.expected {
				t.Errorf("got %q, want %q", tt.status, tt.expected)
			}
		})
	}
}
