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

// Tests for ApplyModifyChange

func TestApplyModifyChange_WrongOperation(t *testing.T) {
	spec := NewSpec("test-spec", "Test Spec")

	change := Change{
		Operation: "add",
		FeatureID: "some-feature",
	}

	err := spec.ApplyModifyChange(change)
	if err == nil {
		t.Fatal("expected error for wrong operation, got nil")
	}

	if !strings.Contains(err.Error(), "expected 'modify' operation") {
		t.Errorf("error should mention expected operation, got: %v", err)
	}
}

func TestApplyModifyChange_FeatureNotFound(t *testing.T) {
	spec := NewSpec("test-spec", "Test Spec")

	change := Change{
		Operation: "modify",
		FeatureID: "nonexistent-feature",
	}

	err := spec.ApplyModifyChange(change)
	if err == nil {
		t.Fatal("expected error for nonexistent feature, got nil")
	}

	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error should mention 'not found', got: %v", err)
	}
}

func TestApplyModifyChange_UpdateDescription(t *testing.T) {
	spec := NewSpec("test-spec", "Test Spec")
	spec.AddFeature(Feature{
		ID:          "my-feature",
		Description: "Original description",
	})

	change := Change{
		Operation:   "modify",
		FeatureID:   "my-feature",
		Description: "Updated description",
	}

	err := spec.ApplyModifyChange(change)
	if err != nil {
		t.Fatalf("ApplyModifyChange failed: %v", err)
	}

	if spec.Features[0].Description != "Updated description" {
		t.Errorf("expected description 'Updated description', got %q", spec.Features[0].Description)
	}
}

func TestApplyModifyChange_AddCriteria(t *testing.T) {
	spec := NewSpec("test-spec", "Test Spec")
	spec.AddFeature(Feature{
		ID:                 "my-feature",
		Description:        "Test feature",
		AcceptanceCriteria: []string{"Existing criterion"},
	})

	change := Change{
		Operation: "modify",
		FeatureID: "my-feature",
		Criteria: &CriteriaModify{
			Add: []string{"New criterion 1", "New criterion 2"},
		},
	}

	err := spec.ApplyModifyChange(change)
	if err != nil {
		t.Fatalf("ApplyModifyChange failed: %v", err)
	}

	if len(spec.Features[0].AcceptanceCriteria) != 3 {
		t.Errorf("expected 3 criteria, got %d", len(spec.Features[0].AcceptanceCriteria))
	}

	if spec.Features[0].AcceptanceCriteria[1] != "New criterion 1" {
		t.Errorf("expected 'New criterion 1', got %q", spec.Features[0].AcceptanceCriteria[1])
	}
}

func TestApplyModifyChange_RemoveCriteria(t *testing.T) {
	spec := NewSpec("test-spec", "Test Spec")
	spec.AddFeature(Feature{
		ID:                 "my-feature",
		Description:        "Test feature",
		AcceptanceCriteria: []string{"Keep this", "Remove this", "Also keep"},
	})

	change := Change{
		Operation: "modify",
		FeatureID: "my-feature",
		Criteria: &CriteriaModify{
			Remove: []string{"Remove this"},
		},
	}

	err := spec.ApplyModifyChange(change)
	if err != nil {
		t.Fatalf("ApplyModifyChange failed: %v", err)
	}

	if len(spec.Features[0].AcceptanceCriteria) != 2 {
		t.Errorf("expected 2 criteria, got %d", len(spec.Features[0].AcceptanceCriteria))
	}

	for _, c := range spec.Features[0].AcceptanceCriteria {
		if c == "Remove this" {
			t.Error("criterion 'Remove this' should have been removed")
		}
	}
}

func TestApplyModifyChange_RemoveCriteriaNotFound(t *testing.T) {
	spec := NewSpec("test-spec", "Test Spec")
	spec.AddFeature(Feature{
		ID:                 "my-feature",
		Description:        "Test feature",
		AcceptanceCriteria: []string{"Existing criterion"},
	})

	change := Change{
		Operation: "modify",
		FeatureID: "my-feature",
		Criteria: &CriteriaModify{
			Remove: []string{"Nonexistent criterion"},
		},
	}

	err := spec.ApplyModifyChange(change)
	if err == nil {
		t.Fatal("expected error for nonexistent criterion, got nil")
	}

	if !strings.Contains(err.Error(), "not found for removal") {
		t.Errorf("error should mention 'not found for removal', got: %v", err)
	}
}

func TestApplyModifyChange_EditCriteria(t *testing.T) {
	spec := NewSpec("test-spec", "Test Spec")
	spec.AddFeature(Feature{
		ID:                 "my-feature",
		Description:        "Test feature",
		AcceptanceCriteria: []string{"Old criterion text"},
	})

	change := Change{
		Operation: "modify",
		FeatureID: "my-feature",
		Criteria: &CriteriaModify{
			Edit: []EditPair{
				{Old: "Old criterion text", New: "New criterion text"},
			},
		},
	}

	err := spec.ApplyModifyChange(change)
	if err != nil {
		t.Fatalf("ApplyModifyChange failed: %v", err)
	}

	if spec.Features[0].AcceptanceCriteria[0] != "New criterion text" {
		t.Errorf("expected 'New criterion text', got %q", spec.Features[0].AcceptanceCriteria[0])
	}
}

func TestApplyModifyChange_EditCriteriaNotFound(t *testing.T) {
	spec := NewSpec("test-spec", "Test Spec")
	spec.AddFeature(Feature{
		ID:                 "my-feature",
		Description:        "Test feature",
		AcceptanceCriteria: []string{"Existing criterion"},
	})

	change := Change{
		Operation: "modify",
		FeatureID: "my-feature",
		Criteria: &CriteriaModify{
			Edit: []EditPair{
				{Old: "Nonexistent criterion", New: "New text"},
			},
		},
	}

	err := spec.ApplyModifyChange(change)
	if err == nil {
		t.Fatal("expected error for nonexistent criterion, got nil")
	}

	if !strings.Contains(err.Error(), "not found for edit") {
		t.Errorf("error should mention 'not found for edit', got: %v", err)
	}
}

func TestApplyModifyChange_CombinedCriteriaOperations(t *testing.T) {
	spec := NewSpec("test-spec", "Test Spec")
	spec.AddFeature(Feature{
		ID:                 "my-feature",
		Description:        "Test feature",
		AcceptanceCriteria: []string{"Keep", "Remove me", "Edit me"},
	})

	change := Change{
		Operation: "modify",
		FeatureID: "my-feature",
		Criteria: &CriteriaModify{
			Remove: []string{"Remove me"},
			Edit:   []EditPair{{Old: "Edit me", New: "Edited"}},
			Add:    []string{"New one"},
		},
	}

	err := spec.ApplyModifyChange(change)
	if err != nil {
		t.Fatalf("ApplyModifyChange failed: %v", err)
	}

	expected := []string{"Keep", "Edited", "New one"}
	if len(spec.Features[0].AcceptanceCriteria) != len(expected) {
		t.Fatalf("expected %d criteria, got %d", len(expected), len(spec.Features[0].AcceptanceCriteria))
	}

	for i, exp := range expected {
		if spec.Features[0].AcceptanceCriteria[i] != exp {
			t.Errorf("criterion[%d]: expected %q, got %q", i, exp, spec.Features[0].AcceptanceCriteria[i])
		}
	}
}

func TestApplyModifyChange_DomainKnowledgeAdd(t *testing.T) {
	spec := NewSpec("test-spec", "Test Spec")
	spec.AddDomainKnowledge("Existing knowledge")

	change := Change{
		Operation: "modify",
		DomainKnowledgeMod: &DomainKnowledgeModify{
			Add: []string{"New knowledge 1", "New knowledge 2"},
		},
	}

	err := spec.ApplyModifyChange(change)
	if err != nil {
		t.Fatalf("ApplyModifyChange failed: %v", err)
	}

	if len(spec.DomainKnowledge) != 3 {
		t.Errorf("expected 3 domain knowledge items, got %d", len(spec.DomainKnowledge))
	}
}

func TestApplyModifyChange_DomainKnowledgeRemove(t *testing.T) {
	spec := NewSpec("test-spec", "Test Spec")
	spec.AddDomainKnowledge("Keep this")
	spec.AddDomainKnowledge("Remove this")

	change := Change{
		Operation: "modify",
		DomainKnowledgeMod: &DomainKnowledgeModify{
			Remove: []string{"Remove this"},
		},
	}

	err := spec.ApplyModifyChange(change)
	if err != nil {
		t.Fatalf("ApplyModifyChange failed: %v", err)
	}

	if len(spec.DomainKnowledge) != 1 {
		t.Errorf("expected 1 domain knowledge item, got %d", len(spec.DomainKnowledge))
	}

	if spec.DomainKnowledge[0] != "Keep this" {
		t.Errorf("expected 'Keep this', got %q", spec.DomainKnowledge[0])
	}
}

func TestApplyModifyChange_DomainKnowledgeRemoveNotFound(t *testing.T) {
	spec := NewSpec("test-spec", "Test Spec")
	spec.AddDomainKnowledge("Existing knowledge")

	change := Change{
		Operation: "modify",
		DomainKnowledgeMod: &DomainKnowledgeModify{
			Remove: []string{"Nonexistent knowledge"},
		},
	}

	err := spec.ApplyModifyChange(change)
	if err == nil {
		t.Fatal("expected error for nonexistent domain knowledge, got nil")
	}

	if !strings.Contains(err.Error(), "not found for removal") {
		t.Errorf("error should mention 'not found for removal', got: %v", err)
	}
}

func TestApplyModifyChange_DomainKnowledgeEdit(t *testing.T) {
	spec := NewSpec("test-spec", "Test Spec")
	spec.AddDomainKnowledge("Old knowledge text")

	change := Change{
		Operation: "modify",
		DomainKnowledgeMod: &DomainKnowledgeModify{
			Edit: []EditPair{
				{Old: "Old knowledge text", New: "New knowledge text"},
			},
		},
	}

	err := spec.ApplyModifyChange(change)
	if err != nil {
		t.Fatalf("ApplyModifyChange failed: %v", err)
	}

	if spec.DomainKnowledge[0] != "New knowledge text" {
		t.Errorf("expected 'New knowledge text', got %q", spec.DomainKnowledge[0])
	}
}

func TestApplyModifyChange_DomainKnowledgeEditNotFound(t *testing.T) {
	spec := NewSpec("test-spec", "Test Spec")
	spec.AddDomainKnowledge("Existing knowledge")

	change := Change{
		Operation: "modify",
		DomainKnowledgeMod: &DomainKnowledgeModify{
			Edit: []EditPair{
				{Old: "Nonexistent knowledge", New: "New text"},
			},
		},
	}

	err := spec.ApplyModifyChange(change)
	if err == nil {
		t.Fatal("expected error for nonexistent domain knowledge, got nil")
	}

	if !strings.Contains(err.Error(), "not found for edit") {
		t.Errorf("error should mention 'not found for edit', got: %v", err)
	}
}

func TestApplyModifyChange_CombinedDomainKnowledgeOperations(t *testing.T) {
	spec := NewSpec("test-spec", "Test Spec")
	spec.AddDomainKnowledge("Keep")
	spec.AddDomainKnowledge("Remove me")
	spec.AddDomainKnowledge("Edit me")

	change := Change{
		Operation: "modify",
		DomainKnowledgeMod: &DomainKnowledgeModify{
			Remove: []string{"Remove me"},
			Edit:   []EditPair{{Old: "Edit me", New: "Edited"}},
			Add:    []string{"New one"},
		},
	}

	err := spec.ApplyModifyChange(change)
	if err != nil {
		t.Fatalf("ApplyModifyChange failed: %v", err)
	}

	expected := []string{"Keep", "Edited", "New one"}
	if len(spec.DomainKnowledge) != len(expected) {
		t.Fatalf("expected %d domain knowledge items, got %d", len(expected), len(spec.DomainKnowledge))
	}

	for i, exp := range expected {
		if spec.DomainKnowledge[i] != exp {
			t.Errorf("domain_knowledge[%d]: expected %q, got %q", i, exp, spec.DomainKnowledge[i])
		}
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

// Tests for ApplyRemoveChange

func TestApplyRemoveChange_WrongOperation(t *testing.T) {
	spec := NewSpec("test-spec", "Test Spec")

	change := Change{
		Operation: "add",
		FeatureID: "some-feature",
	}

	err := spec.ApplyRemoveChange(change)
	if err == nil {
		t.Fatal("expected error for wrong operation, got nil")
	}

	if !strings.Contains(err.Error(), "expected 'remove' operation") {
		t.Errorf("error should mention expected operation, got: %v", err)
	}
}

func TestApplyRemoveChange_Feature(t *testing.T) {
	spec := NewSpec("test-spec", "Test Spec")
	spec.AddFeature(Feature{
		ID:                 "feature-to-remove",
		Description:        "This will be removed",
		AcceptanceCriteria: []string{"Criterion 1"},
	})
	spec.AddFeature(Feature{
		ID:          "feature-to-keep",
		Description: "This stays",
	})

	change := Change{
		Operation: "remove",
		FeatureID: "feature-to-remove",
	}

	err := spec.ApplyRemoveChange(change)
	if err != nil {
		t.Fatalf("ApplyRemoveChange failed: %v", err)
	}

	if len(spec.Features) != 1 {
		t.Errorf("expected 1 feature, got %d", len(spec.Features))
	}

	if spec.Features[0].ID != "feature-to-keep" {
		t.Errorf("wrong feature remained, got %q", spec.Features[0].ID)
	}
}

func TestApplyRemoveChange_FeatureNotFound_Idempotent(t *testing.T) {
	spec := NewSpec("test-spec", "Test Spec")
	spec.AddFeature(Feature{
		ID:          "existing-feature",
		Description: "This exists",
	})

	change := Change{
		Operation: "remove",
		FeatureID: "nonexistent-feature",
	}

	// Idempotent: removing a nonexistent feature should succeed
	err := spec.ApplyRemoveChange(change)
	if err != nil {
		t.Fatalf("expected idempotent success for nonexistent feature, got: %v", err)
	}

	// Existing feature should be untouched
	if len(spec.Features) != 1 {
		t.Errorf("expected 1 feature to remain, got %d", len(spec.Features))
	}
}

func TestApplyRemoveChange_FeatureWithReason(t *testing.T) {
	spec := NewSpec("test-spec", "Test Spec")
	spec.AddFeature(Feature{
		ID:          "deprecated-feature",
		Description: "Old feature",
	})

	change := Change{
		Operation: "remove",
		FeatureID: "deprecated-feature",
		Reason:    "Feature deprecated in favor of new-feature",
	}

	err := spec.ApplyRemoveChange(change)
	if err != nil {
		t.Fatalf("ApplyRemoveChange failed: %v", err)
	}

	if len(spec.Features) != 0 {
		t.Errorf("expected 0 features, got %d", len(spec.Features))
	}
}

func TestApplyRemoveChange_DomainKnowledge(t *testing.T) {
	spec := NewSpec("test-spec", "Test Spec")
	spec.AddDomainKnowledge("Knowledge to remove")
	spec.AddDomainKnowledge("Knowledge to keep")

	change := Change{
		Operation:       "remove",
		DomainKnowledge: []string{"Knowledge to remove"},
	}

	err := spec.ApplyRemoveChange(change)
	if err != nil {
		t.Fatalf("ApplyRemoveChange failed: %v", err)
	}

	if len(spec.DomainKnowledge) != 1 {
		t.Errorf("expected 1 domain knowledge item, got %d", len(spec.DomainKnowledge))
	}

	if spec.DomainKnowledge[0] != "Knowledge to keep" {
		t.Errorf("wrong knowledge remained, got %q", spec.DomainKnowledge[0])
	}
}

func TestApplyRemoveChange_DomainKnowledgeMultiple(t *testing.T) {
	spec := NewSpec("test-spec", "Test Spec")
	spec.AddDomainKnowledge("Remove first")
	spec.AddDomainKnowledge("Keep this")
	spec.AddDomainKnowledge("Remove second")

	change := Change{
		Operation:       "remove",
		DomainKnowledge: []string{"Remove first", "Remove second"},
	}

	err := spec.ApplyRemoveChange(change)
	if err != nil {
		t.Fatalf("ApplyRemoveChange failed: %v", err)
	}

	if len(spec.DomainKnowledge) != 1 {
		t.Errorf("expected 1 domain knowledge item, got %d", len(spec.DomainKnowledge))
	}

	if spec.DomainKnowledge[0] != "Keep this" {
		t.Errorf("wrong knowledge remained, got %q", spec.DomainKnowledge[0])
	}
}

func TestApplyRemoveChange_DomainKnowledgeNotFound_Idempotent(t *testing.T) {
	spec := NewSpec("test-spec", "Test Spec")
	spec.AddDomainKnowledge("Existing knowledge")

	change := Change{
		Operation:       "remove",
		DomainKnowledge: []string{"Nonexistent knowledge"},
	}

	// Idempotent: removing nonexistent knowledge should succeed
	err := spec.ApplyRemoveChange(change)
	if err != nil {
		t.Fatalf("expected idempotent success for nonexistent knowledge, got: %v", err)
	}

	// Existing knowledge should be untouched
	if len(spec.DomainKnowledge) != 1 {
		t.Errorf("expected 1 knowledge item to remain, got %d", len(spec.DomainKnowledge))
	}
}

func TestApplyRemoveChange_DomainKnowledgeExactMatch(t *testing.T) {
	spec := NewSpec("test-spec", "Test Spec")
	spec.AddDomainKnowledge("Exact string to match")

	// Try with slightly different string (extra space) - should succeed but not remove anything
	change := Change{
		Operation:       "remove",
		DomainKnowledge: []string{"Exact string to match "},
	}

	// Idempotent: non-matching string succeeds (nothing to remove)
	err := spec.ApplyRemoveChange(change)
	if err != nil {
		t.Fatalf("expected idempotent success, got: %v", err)
	}

	// Original should still exist (exact match required for actual removal)
	if len(spec.DomainKnowledge) != 1 {
		t.Errorf("original knowledge should still exist, got %d items", len(spec.DomainKnowledge))
	}
}

func TestApplyRemoveChange_FeatureAndDomainKnowledge(t *testing.T) {
	spec := NewSpec("test-spec", "Test Spec")
	spec.AddFeature(Feature{
		ID:          "feature-to-remove",
		Description: "Will be removed",
	})
	spec.AddFeature(Feature{
		ID:          "feature-to-keep",
		Description: "Will stay",
	})
	spec.AddDomainKnowledge("Knowledge to remove")
	spec.AddDomainKnowledge("Knowledge to keep")

	change := Change{
		Operation:       "remove",
		FeatureID:       "feature-to-remove",
		DomainKnowledge: []string{"Knowledge to remove"},
	}

	err := spec.ApplyRemoveChange(change)
	if err != nil {
		t.Fatalf("ApplyRemoveChange failed: %v", err)
	}

	if len(spec.Features) != 1 {
		t.Errorf("expected 1 feature, got %d", len(spec.Features))
	}

	if len(spec.DomainKnowledge) != 1 {
		t.Errorf("expected 1 domain knowledge item, got %d", len(spec.DomainKnowledge))
	}

	if spec.Features[0].ID != "feature-to-keep" {
		t.Errorf("wrong feature remained")
	}

	if spec.DomainKnowledge[0] != "Knowledge to keep" {
		t.Errorf("wrong knowledge remained")
	}
}

// Tests for ChangeRequest.ApplyChanges

func TestApplyChanges_SingleAddOperation(t *testing.T) {
	spec := NewSpec("test-spec", "Test Spec")

	cr := &ChangeRequest{
		ID:         "cr-1",
		ParentSpec: "test-spec",
		Changes: []Change{
			{
				Operation: "add",
				Feature: &Feature{
					ID:                 "new-feature",
					Description:        "A new feature",
					AcceptanceCriteria: []string{"It works"},
				},
			},
		},
	}

	err := cr.ApplyChanges(spec)
	if err != nil {
		t.Fatalf("ApplyChanges failed: %v", err)
	}

	if len(spec.Features) != 1 {
		t.Errorf("expected 1 feature, got %d", len(spec.Features))
	}

	if spec.Features[0].ID != "new-feature" {
		t.Errorf("expected feature ID 'new-feature', got %q", spec.Features[0].ID)
	}
}

func TestApplyChanges_MultipleOperations(t *testing.T) {
	spec := NewSpec("test-spec", "Test Spec")
	spec.AddFeature(Feature{
		ID:                 "existing-feature",
		Description:        "Original description",
		AcceptanceCriteria: []string{"Original criterion"},
	})
	spec.AddDomainKnowledge("Existing knowledge")

	cr := &ChangeRequest{
		ID:         "cr-1",
		ParentSpec: "test-spec",
		Changes: []Change{
			{
				Operation: "add",
				Feature: &Feature{
					ID:          "new-feature",
					Description: "Brand new",
				},
				DomainKnowledge: []string{"New knowledge"},
			},
			{
				Operation:   "modify",
				FeatureID:   "existing-feature",
				Description: "Updated description",
				Criteria: &CriteriaModify{
					Add: []string{"New criterion"},
				},
			},
		},
	}

	err := cr.ApplyChanges(spec)
	if err != nil {
		t.Fatalf("ApplyChanges failed: %v", err)
	}

	// Verify add
	if len(spec.Features) != 2 {
		t.Errorf("expected 2 features, got %d", len(spec.Features))
	}

	if len(spec.DomainKnowledge) != 2 {
		t.Errorf("expected 2 domain knowledge items, got %d", len(spec.DomainKnowledge))
	}

	// Verify modify
	var existingFeature *Feature
	for i := range spec.Features {
		if spec.Features[i].ID == "existing-feature" {
			existingFeature = &spec.Features[i]
			break
		}
	}

	if existingFeature == nil {
		t.Fatal("existing-feature not found")
	}

	if existingFeature.Description != "Updated description" {
		t.Errorf("expected 'Updated description', got %q", existingFeature.Description)
	}

	if len(existingFeature.AcceptanceCriteria) != 2 {
		t.Errorf("expected 2 criteria, got %d", len(existingFeature.AcceptanceCriteria))
	}
}

func TestApplyChanges_AllOperationTypes(t *testing.T) {
	spec := NewSpec("test-spec", "Test Spec")
	spec.AddFeature(Feature{
		ID:          "feature-to-modify",
		Description: "Original",
	})
	spec.AddFeature(Feature{
		ID:          "feature-to-remove",
		Description: "Will be gone",
	})
	spec.AddDomainKnowledge("Knowledge to remove")

	cr := &ChangeRequest{
		ID:         "cr-1",
		ParentSpec: "test-spec",
		Changes: []Change{
			{
				Operation: "add",
				Feature: &Feature{
					ID:          "added-feature",
					Description: "New feature",
				},
			},
			{
				Operation:   "modify",
				FeatureID:   "feature-to-modify",
				Description: "Modified",
			},
			{
				Operation: "remove",
				FeatureID: "feature-to-remove",
			},
			{
				Operation:       "remove",
				DomainKnowledge: []string{"Knowledge to remove"},
			},
		},
	}

	err := cr.ApplyChanges(spec)
	if err != nil {
		t.Fatalf("ApplyChanges failed: %v", err)
	}

	// Should have 2 features: added-feature and feature-to-modify
	if len(spec.Features) != 2 {
		t.Errorf("expected 2 features, got %d", len(spec.Features))
	}

	// Verify the right features exist
	if !spec.HasFeature("added-feature") {
		t.Error("added-feature should exist")
	}
	if !spec.HasFeature("feature-to-modify") {
		t.Error("feature-to-modify should exist")
	}
	if spec.HasFeature("feature-to-remove") {
		t.Error("feature-to-remove should not exist")
	}

	// Domain knowledge should be empty
	if len(spec.DomainKnowledge) != 0 {
		t.Errorf("expected 0 domain knowledge items, got %d", len(spec.DomainKnowledge))
	}

	// Verify modification
	for _, f := range spec.Features {
		if f.ID == "feature-to-modify" && f.Description != "Modified" {
			t.Errorf("expected description 'Modified', got %q", f.Description)
		}
	}
}

func TestApplyChanges_FailsOnInvalidOperation(t *testing.T) {
	spec := NewSpec("test-spec", "Test Spec")

	cr := &ChangeRequest{
		ID:         "cr-1",
		ParentSpec: "test-spec",
		Changes: []Change{
			{
				Operation: "invalid",
			},
		},
	}

	err := cr.ApplyChanges(spec)
	if err == nil {
		t.Fatal("expected error for invalid operation, got nil")
	}

	if !strings.Contains(err.Error(), "unknown operation") {
		t.Errorf("error should mention 'unknown operation', got: %v", err)
	}
}

func TestApplyChanges_FailsOnSecondChange(t *testing.T) {
	spec := NewSpec("test-spec", "Test Spec")

	cr := &ChangeRequest{
		ID:         "cr-1",
		ParentSpec: "test-spec",
		Changes: []Change{
			{
				Operation: "add",
				Feature: &Feature{
					ID:          "good-feature",
					Description: "This works",
				},
			},
			{
				// Modify on nonexistent feature should fail (modify is not idempotent)
				Operation:   "modify",
				FeatureID:   "nonexistent-feature",
				Description: "Updated description",
			},
		},
	}

	err := cr.ApplyChanges(spec)
	if err == nil {
		t.Fatal("expected error for nonexistent feature, got nil")
	}

	if !strings.Contains(err.Error(), "failed to apply change 1") {
		t.Errorf("error should mention change index, got: %v", err)
	}

	// First change should have been applied (partial state)
	if len(spec.Features) != 1 {
		t.Errorf("first change should have been applied, got %d features", len(spec.Features))
	}
}

func TestApplyChanges_EmptyChanges(t *testing.T) {
	spec := NewSpec("test-spec", "Test Spec")

	cr := &ChangeRequest{
		ID:         "cr-1",
		ParentSpec: "test-spec",
		Changes:    []Change{},
	}

	err := cr.ApplyChanges(spec)
	if err != nil {
		t.Fatalf("ApplyChanges with empty changes failed: %v", err)
	}
}
