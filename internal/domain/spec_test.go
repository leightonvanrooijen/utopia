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

func TestApplyRemoveChange_FeatureNotFound(t *testing.T) {
	spec := NewSpec("test-spec", "Test Spec")
	spec.AddFeature(Feature{
		ID:          "existing-feature",
		Description: "This exists",
	})

	change := Change{
		Operation: "remove",
		FeatureID: "nonexistent-feature",
	}

	err := spec.ApplyRemoveChange(change)
	if err == nil {
		t.Fatal("expected error for nonexistent feature, got nil")
	}

	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error should mention 'not found', got: %v", err)
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

func TestApplyRemoveChange_DomainKnowledgeNotFound(t *testing.T) {
	spec := NewSpec("test-spec", "Test Spec")
	spec.AddDomainKnowledge("Existing knowledge")

	change := Change{
		Operation:       "remove",
		DomainKnowledge: []string{"Nonexistent knowledge"},
	}

	err := spec.ApplyRemoveChange(change)
	if err == nil {
		t.Fatal("expected error for nonexistent domain knowledge, got nil")
	}

	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error should mention 'not found', got: %v", err)
	}
}

func TestApplyRemoveChange_DomainKnowledgeExactMatch(t *testing.T) {
	spec := NewSpec("test-spec", "Test Spec")
	spec.AddDomainKnowledge("Exact string to match")

	// Try with slightly different string (extra space)
	change := Change{
		Operation:       "remove",
		DomainKnowledge: []string{"Exact string to match "},
	}

	err := spec.ApplyRemoveChange(change)
	if err == nil {
		t.Fatal("expected error for non-exact match, got nil")
	}

	// Verify original still exists
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
	spec.Status = StatusApproved

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

	// Status should be preserved
	if spec.Status != StatusApproved {
		t.Errorf("expected status 'approved', got %q", spec.Status)
	}
}

func TestApplyChanges_MultipleOperations(t *testing.T) {
	spec := NewSpec("test-spec", "Test Spec")
	spec.Status = StatusReview
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

	// Status should be preserved
	if spec.Status != StatusReview {
		t.Errorf("expected status 'review', got %q", spec.Status)
	}
}

func TestApplyChanges_AllOperationTypes(t *testing.T) {
	spec := NewSpec("test-spec", "Test Spec")
	spec.Status = StatusDraft
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

	// Status preserved
	if spec.Status != StatusDraft {
		t.Errorf("expected status 'draft', got %q", spec.Status)
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
				Operation: "remove",
				FeatureID: "nonexistent-feature",
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
	spec.Status = StatusApproved

	cr := &ChangeRequest{
		ID:         "cr-1",
		ParentSpec: "test-spec",
		Changes:    []Change{},
	}

	err := cr.ApplyChanges(spec)
	if err != nil {
		t.Fatalf("ApplyChanges with empty changes failed: %v", err)
	}

	// Status should still be preserved
	if spec.Status != StatusApproved {
		t.Errorf("expected status 'approved', got %q", spec.Status)
	}
}

func TestApplyChanges_PreservesStatusThroughMultipleUpdates(t *testing.T) {
	spec := NewSpec("test-spec", "Test Spec")
	spec.Status = StatusApproved

	cr := &ChangeRequest{
		ID:         "cr-1",
		ParentSpec: "test-spec",
		Changes: []Change{
			{
				Operation: "add",
				Feature: &Feature{
					ID:          "feature-1",
					Description: "First",
				},
			},
			{
				Operation: "add",
				Feature: &Feature{
					ID:          "feature-2",
					Description: "Second",
				},
			},
			{
				Operation: "add",
				DomainKnowledge: []string{"Knowledge 1", "Knowledge 2"},
			},
		},
	}

	err := cr.ApplyChanges(spec)
	if err != nil {
		t.Fatalf("ApplyChanges failed: %v", err)
	}

	// Status must be preserved despite multiple updates
	if spec.Status != StatusApproved {
		t.Errorf("expected status 'approved', got %q", spec.Status)
	}
}

// Tests for ChangeRequest.ToSpec

func TestToSpec_SetsIDAndTitle(t *testing.T) {
	cr := &ChangeRequest{
		ID:         "my-change-request",
		Title:      "My Change Request Title",
		ParentSpec: "parent-spec",
		Changes:    []Change{},
	}

	spec := cr.ToSpec()

	if spec.ID != "my-change-request" {
		t.Errorf("expected ID 'my-change-request', got %q", spec.ID)
	}

	if spec.Title != "My Change Request Title" {
		t.Errorf("expected title 'My Change Request Title', got %q", spec.Title)
	}

	if !strings.Contains(spec.Description, "Generated from change request") {
		t.Error("description should mention it was generated from a change request")
	}
}

func TestToSpec_AddFeatureOperation(t *testing.T) {
	cr := &ChangeRequest{
		ID:         "cr-add-feature",
		Title:      "Add Feature CR",
		ParentSpec: "parent-spec",
		Changes: []Change{
			{
				Operation: "add",
				Feature: &Feature{
					ID:                 "new-feature",
					Description:        "A brand new feature",
					AcceptanceCriteria: []string{"Criterion 1", "Criterion 2"},
				},
			},
		},
	}

	spec := cr.ToSpec()

	if len(spec.Features) != 1 {
		t.Fatalf("expected 1 feature, got %d", len(spec.Features))
	}

	f := spec.Features[0]
	if f.ID != "new-feature" {
		t.Errorf("expected feature ID 'new-feature', got %q", f.ID)
	}
	if f.Description != "A brand new feature" {
		t.Errorf("expected description 'A brand new feature', got %q", f.Description)
	}
	if len(f.AcceptanceCriteria) != 2 {
		t.Errorf("expected 2 acceptance criteria, got %d", len(f.AcceptanceCriteria))
	}
}

func TestToSpec_AddDomainKnowledgeOnly_Ignored(t *testing.T) {
	cr := &ChangeRequest{
		ID:         "cr-add-dk",
		Title:      "Add Domain Knowledge CR",
		ParentSpec: "parent-spec",
		Changes: []Change{
			{
				Operation:       "add",
				DomainKnowledge: []string{"Some knowledge", "More knowledge"},
			},
		},
	}

	spec := cr.ToSpec()

	// Add operations with only domain knowledge should be ignored
	if len(spec.Features) != 0 {
		t.Errorf("expected 0 features (domain knowledge only should be ignored), got %d", len(spec.Features))
	}
}

func TestToSpec_RemoveOperation(t *testing.T) {
	cr := &ChangeRequest{
		ID:         "cr-remove",
		Title:      "Remove Feature CR",
		ParentSpec: "parent-spec",
		Changes: []Change{
			{
				Operation: "remove",
				FeatureID: "deprecated-feature",
				Reason:    "No longer needed after refactoring",
			},
		},
	}

	spec := cr.ToSpec()

	if len(spec.Features) != 1 {
		t.Fatalf("expected 1 feature, got %d", len(spec.Features))
	}

	f := spec.Features[0]
	if f.ID != "remove-deprecated-feature" {
		t.Errorf("expected feature ID 'remove-deprecated-feature', got %q", f.ID)
	}
	if !strings.Contains(f.Description, "Remove") {
		t.Error("description should mention removal")
	}
	if !strings.Contains(f.Description, "deprecated-feature") {
		t.Error("description should mention the feature being removed")
	}

	// Should have acceptance criteria about removal
	if len(f.AcceptanceCriteria) < 3 {
		t.Errorf("expected at least 3 acceptance criteria, got %d", len(f.AcceptanceCriteria))
	}

	// Should include the reason
	foundReason := false
	for _, c := range f.AcceptanceCriteria {
		if strings.Contains(c, "No longer needed after refactoring") {
			foundReason = true
			break
		}
	}
	if !foundReason {
		t.Error("acceptance criteria should include the removal reason")
	}
}

func TestToSpec_RemoveOperationWithoutReason(t *testing.T) {
	cr := &ChangeRequest{
		ID:         "cr-remove-no-reason",
		Title:      "Remove Without Reason",
		ParentSpec: "parent-spec",
		Changes: []Change{
			{
				Operation: "remove",
				FeatureID: "old-feature",
			},
		},
	}

	spec := cr.ToSpec()

	if len(spec.Features) != 1 {
		t.Fatalf("expected 1 feature, got %d", len(spec.Features))
	}

	// Should still have base criteria even without reason
	if len(spec.Features[0].AcceptanceCriteria) < 3 {
		t.Errorf("expected at least 3 acceptance criteria, got %d", len(spec.Features[0].AcceptanceCriteria))
	}
}

func TestToSpec_ModifyOperation_AddCriteria(t *testing.T) {
	cr := &ChangeRequest{
		ID:         "cr-modify",
		Title:      "Modify Feature CR",
		ParentSpec: "parent-spec",
		Changes: []Change{
			{
				Operation: "modify",
				FeatureID: "existing-feature",
				Criteria: &CriteriaModify{
					Add: []string{"New criterion 1", "New criterion 2"},
				},
			},
		},
	}

	spec := cr.ToSpec()

	if len(spec.Features) != 1 {
		t.Fatalf("expected 1 feature, got %d", len(spec.Features))
	}

	f := spec.Features[0]
	if f.ID != "modify-existing-feature" {
		t.Errorf("expected feature ID 'modify-existing-feature', got %q", f.ID)
	}

	// The added criteria should be in the acceptance criteria
	if len(f.AcceptanceCriteria) != 2 {
		t.Errorf("expected 2 acceptance criteria, got %d", len(f.AcceptanceCriteria))
	}
	if f.AcceptanceCriteria[0] != "New criterion 1" {
		t.Errorf("expected 'New criterion 1', got %q", f.AcceptanceCriteria[0])
	}
}

func TestToSpec_ModifyOperation_RemoveCriteria(t *testing.T) {
	cr := &ChangeRequest{
		ID:         "cr-modify-remove",
		Title:      "Modify Feature - Remove Criteria",
		ParentSpec: "parent-spec",
		Changes: []Change{
			{
				Operation: "modify",
				FeatureID: "existing-feature",
				Criteria: &CriteriaModify{
					Remove: []string{"Old criterion to remove"},
				},
			},
		},
	}

	spec := cr.ToSpec()

	if len(spec.Features) != 1 {
		t.Fatalf("expected 1 feature, got %d", len(spec.Features))
	}

	// Should have a criterion about removal
	found := false
	for _, c := range spec.Features[0].AcceptanceCriteria {
		if strings.Contains(c, "Remove/undo") && strings.Contains(c, "Old criterion to remove") {
			found = true
			break
		}
	}
	if !found {
		t.Error("acceptance criteria should describe the criterion to remove")
	}
}

func TestToSpec_ModifyOperation_EditCriteria(t *testing.T) {
	cr := &ChangeRequest{
		ID:         "cr-modify-edit",
		Title:      "Modify Feature - Edit Criteria",
		ParentSpec: "parent-spec",
		Changes: []Change{
			{
				Operation: "modify",
				FeatureID: "existing-feature",
				Criteria: &CriteriaModify{
					Edit: []EditPair{
						{Old: "Old text", New: "New text"},
					},
				},
			},
		},
	}

	spec := cr.ToSpec()

	if len(spec.Features) != 1 {
		t.Fatalf("expected 1 feature, got %d", len(spec.Features))
	}

	// Should have a criterion about the edit
	found := false
	for _, c := range spec.Features[0].AcceptanceCriteria {
		if strings.Contains(c, "Old text") && strings.Contains(c, "New text") {
			found = true
			break
		}
	}
	if !found {
		t.Error("acceptance criteria should describe the edit operation")
	}
}

func TestToSpec_ModifyOperation_WithDescription(t *testing.T) {
	cr := &ChangeRequest{
		ID:         "cr-modify-desc",
		Title:      "Modify Feature - With Description",
		ParentSpec: "parent-spec",
		Changes: []Change{
			{
				Operation:   "modify",
				FeatureID:   "existing-feature",
				Description: "Updated to support new requirements",
				Criteria: &CriteriaModify{
					Add: []string{"Supports new format"},
				},
			},
		},
	}

	spec := cr.ToSpec()

	if len(spec.Features) != 1 {
		t.Fatalf("expected 1 feature, got %d", len(spec.Features))
	}

	if !strings.Contains(spec.Features[0].Description, "Updated to support new requirements") {
		t.Errorf("description should include the provided description, got %q", spec.Features[0].Description)
	}
}

func TestToSpec_ModifyOperation_NoCriteria(t *testing.T) {
	cr := &ChangeRequest{
		ID:         "cr-modify-no-criteria",
		Title:      "Modify Feature - No Criteria",
		ParentSpec: "parent-spec",
		Changes: []Change{
			{
				Operation:   "modify",
				FeatureID:   "existing-feature",
				Description: "Just updating description",
			},
		},
	}

	spec := cr.ToSpec()

	if len(spec.Features) != 1 {
		t.Fatalf("expected 1 feature, got %d", len(spec.Features))
	}

	// Should have at least one default criterion
	if len(spec.Features[0].AcceptanceCriteria) < 1 {
		t.Error("should have at least one acceptance criterion even without explicit criteria changes")
	}
}

func TestToSpec_MixedOperations(t *testing.T) {
	cr := &ChangeRequest{
		ID:         "cr-mixed",
		Title:      "Mixed Operations CR",
		ParentSpec: "parent-spec",
		Changes: []Change{
			{
				Operation: "add",
				Feature: &Feature{
					ID:                 "new-feature",
					Description:        "Brand new",
					AcceptanceCriteria: []string{"Works"},
				},
			},
			{
				Operation:       "add",
				DomainKnowledge: []string{"Some knowledge"}, // Should be ignored
			},
			{
				Operation: "modify",
				FeatureID: "existing-feature",
				Criteria: &CriteriaModify{
					Add: []string{"New criterion"},
				},
			},
			{
				Operation: "remove",
				FeatureID: "old-feature",
				Reason:    "Deprecated",
			},
		},
	}

	spec := cr.ToSpec()

	// Should have 3 features: 1 add, 1 modify, 1 remove
	// The add with only domain knowledge is ignored
	if len(spec.Features) != 3 {
		t.Fatalf("expected 3 features, got %d", len(spec.Features))
	}

	// Verify we have the expected feature IDs
	ids := make(map[string]bool)
	for _, f := range spec.Features {
		ids[f.ID] = true
	}

	if !ids["new-feature"] {
		t.Error("missing 'new-feature'")
	}
	if !ids["modify-existing-feature"] {
		t.Error("missing 'modify-existing-feature'")
	}
	if !ids["remove-old-feature"] {
		t.Error("missing 'remove-old-feature'")
	}
}

func TestToSpec_EmptyChanges(t *testing.T) {
	cr := &ChangeRequest{
		ID:         "cr-empty",
		Title:      "Empty CR",
		ParentSpec: "parent-spec",
		Changes:    []Change{},
	}

	spec := cr.ToSpec()

	if len(spec.Features) != 0 {
		t.Errorf("expected 0 features for empty change request, got %d", len(spec.Features))
	}
}

func TestToSpec_RemoveWithoutFeatureID_Ignored(t *testing.T) {
	cr := &ChangeRequest{
		ID:         "cr-remove-no-id",
		Title:      "Remove Without Feature ID",
		ParentSpec: "parent-spec",
		Changes: []Change{
			{
				Operation: "remove",
				// No FeatureID - only domain knowledge
				DomainKnowledge: []string{"Knowledge to remove"},
			},
		},
	}

	spec := cr.ToSpec()

	// Remove operation without feature_id should be ignored
	if len(spec.Features) != 0 {
		t.Errorf("expected 0 features (remove without feature_id should be ignored), got %d", len(spec.Features))
	}
}

func TestToSpec_ModifyWithoutFeatureID_Ignored(t *testing.T) {
	cr := &ChangeRequest{
		ID:         "cr-modify-no-id",
		Title:      "Modify Without Feature ID",
		ParentSpec: "parent-spec",
		Changes: []Change{
			{
				Operation: "modify",
				// No FeatureID
				DomainKnowledgeMod: &DomainKnowledgeModify{
					Add: []string{"New knowledge"},
				},
			},
		},
	}

	spec := cr.ToSpec()

	// Modify operation without feature_id should be ignored
	if len(spec.Features) != 0 {
		t.Errorf("expected 0 features (modify without feature_id should be ignored), got %d", len(spec.Features))
	}
}

func TestToSpec_RefactorCR_SetsIsRefactor(t *testing.T) {
	cr := &ChangeRequest{
		ID:    "refactor-auth",
		Type:  CRTypeRefactor,
		Title: "Refactor authentication module",
		Tasks: []Task{
			{
				ID:                 "extract-helper",
				Description:        "Extract auth helper functions",
				AcceptanceCriteria: []string{"Helper functions are extracted"},
			},
			{
				ID:                 "rename-vars",
				Description:        "Rename variables for clarity",
				AcceptanceCriteria: []string{"Variables are renamed"},
			},
		},
	}

	spec := cr.ToSpec()

	// Verify IsRefactor flag is set
	if !spec.IsRefactor {
		t.Error("ToSpec() should set IsRefactor=true for refactor CRs")
	}

	// Verify tasks are converted to features
	if len(spec.Features) != 2 {
		t.Fatalf("expected 2 features, got %d", len(spec.Features))
	}

	// Verify task data is preserved
	if spec.Features[0].ID != "extract-helper" {
		t.Errorf("expected feature ID 'extract-helper', got %q", spec.Features[0].ID)
	}
	if spec.Features[0].Description != "Extract auth helper functions" {
		t.Errorf("expected description 'Extract auth helper functions', got %q", spec.Features[0].Description)
	}
}

func TestToSpec_NonRefactorCR_IsRefactorFalse(t *testing.T) {
	cr := &ChangeRequest{
		ID:    "feature-add",
		Type:  CRTypeFeature,
		Title: "Add new feature",
		Changes: []Change{
			{
				Operation: "add",
				Feature: &Feature{
					ID:                 "new-feature",
					Description:        "A new feature",
					AcceptanceCriteria: []string{"Feature works"},
				},
			},
		},
	}

	spec := cr.ToSpec()

	// Verify IsRefactor flag is NOT set for feature CRs
	if spec.IsRefactor {
		t.Error("ToSpec() should NOT set IsRefactor=true for non-refactor CRs")
	}
}

// Tests for delete-spec operation

func TestApplyChanges_SkipsDeleteSpecOperation(t *testing.T) {
	spec := NewSpec("test-spec", "Test Spec")
	spec.AddFeature(Feature{
		ID:          "existing-feature",
		Description: "This should remain",
	})

	cr := &ChangeRequest{
		ID:         "cr-with-delete-spec",
		ParentSpec: "test-spec",
		Changes: []Change{
			{
				Operation: "delete-spec",
				Spec:      "other-spec",
				Reason:    "No longer needed",
			},
		},
	}

	// ApplyChanges should skip delete-spec operations (they are handled at merge level)
	err := cr.ApplyChanges(spec)
	if err != nil {
		t.Fatalf("ApplyChanges should not fail for delete-spec: %v", err)
	}

	// The spec should be unchanged
	if len(spec.Features) != 1 {
		t.Errorf("expected 1 feature (unchanged), got %d", len(spec.Features))
	}
}

func TestToSpec_DeleteSpecOperation(t *testing.T) {
	cr := &ChangeRequest{
		ID:         "cr-delete-spec",
		Title:      "Delete Spec CR",
		ParentSpec: "parent-spec",
		Changes: []Change{
			{
				Operation: "delete-spec",
				Spec:      "obsolete-spec",
				Reason:    "Feature has been deprecated",
			},
		},
	}

	spec := cr.ToSpec()

	if len(spec.Features) != 1 {
		t.Fatalf("expected 1 feature for delete-spec, got %d", len(spec.Features))
	}

	f := spec.Features[0]
	if f.ID != "delete-spec-obsolete-spec" {
		t.Errorf("expected feature ID 'delete-spec-obsolete-spec', got %q", f.ID)
	}
	if !strings.Contains(f.Description, "Delete") {
		t.Error("description should mention deletion")
	}
	if !strings.Contains(f.Description, "obsolete-spec") {
		t.Error("description should mention the spec being deleted")
	}

	// Should have acceptance criteria
	if len(f.AcceptanceCriteria) < 3 {
		t.Errorf("expected at least 3 acceptance criteria, got %d", len(f.AcceptanceCriteria))
	}

	// Should include the reason
	foundReason := false
	for _, c := range f.AcceptanceCriteria {
		if strings.Contains(c, "Feature has been deprecated") {
			foundReason = true
			break
		}
	}
	if !foundReason {
		t.Error("acceptance criteria should include the deletion reason")
	}
}

func TestToSpec_DeleteSpecOperationWithoutReason(t *testing.T) {
	cr := &ChangeRequest{
		ID:         "cr-delete-no-reason",
		Title:      "Delete Without Reason",
		ParentSpec: "parent-spec",
		Changes: []Change{
			{
				Operation: "delete-spec",
				Spec:      "old-spec",
			},
		},
	}

	spec := cr.ToSpec()

	if len(spec.Features) != 1 {
		t.Fatalf("expected 1 feature, got %d", len(spec.Features))
	}

	// Should still have base criteria even without reason
	if len(spec.Features[0].AcceptanceCriteria) < 3 {
		t.Errorf("expected at least 3 acceptance criteria, got %d", len(spec.Features[0].AcceptanceCriteria))
	}
}

func TestToSpec_DeleteSpecOperationWithoutSpec_Ignored(t *testing.T) {
	cr := &ChangeRequest{
		ID:         "cr-delete-no-spec",
		Title:      "Delete Without Spec",
		ParentSpec: "parent-spec",
		Changes: []Change{
			{
				Operation: "delete-spec",
				// No Spec field
				Reason: "Some reason",
			},
		},
	}

	spec := cr.ToSpec()

	// delete-spec operation without spec field should be ignored
	if len(spec.Features) != 0 {
		t.Errorf("expected 0 features (delete-spec without spec should be ignored), got %d", len(spec.Features))
	}
}
