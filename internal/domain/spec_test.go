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

// Tests for ADR status transitions

func TestADRTransitionToProposed_FromDraft(t *testing.T) {
	adr := &ADR{
		ID:     "ADR-001",
		Title:  "Test ADR",
		Status: ADRStatusDraft,
	}

	err := adr.TransitionToProposed()
	if err != nil {
		t.Fatalf("TransitionToProposed failed: %v", err)
	}

	if adr.Status != ADRStatusProposed {
		t.Errorf("expected status 'proposed', got %q", adr.Status)
	}

	if len(adr.StatusHistory) != 1 {
		t.Fatalf("expected 1 status change, got %d", len(adr.StatusHistory))
	}

	change := adr.StatusHistory[0]
	if change.From != ADRStatusDraft {
		t.Errorf("expected from 'draft', got %q", change.From)
	}
	if change.To != ADRStatusProposed {
		t.Errorf("expected to 'proposed', got %q", change.To)
	}
	if change.Timestamp.IsZero() {
		t.Error("timestamp should not be zero")
	}
}

func TestADRTransitionToProposed_FromNonDraft(t *testing.T) {
	tests := []ADRStatus{
		ADRStatusProposed,
		ADRStatusAccepted,
		ADRStatusDeprecated,
		ADRStatusSuperseded,
	}

	for _, status := range tests {
		t.Run(string(status), func(t *testing.T) {
			adr := &ADR{
				ID:     "ADR-001",
				Title:  "Test ADR",
				Status: status,
			}

			err := adr.TransitionToProposed()
			if err == nil {
				t.Fatal("expected error for non-draft status, got nil")
			}

			if !strings.Contains(err.Error(), "must be draft") {
				t.Errorf("error should mention 'must be draft', got: %v", err)
			}
		})
	}
}

func TestADRTransitionToAccepted_FromProposed(t *testing.T) {
	adr := &ADR{
		ID:     "ADR-001",
		Title:  "Test ADR",
		Status: ADRStatusProposed,
	}

	err := adr.TransitionToAccepted()
	if err != nil {
		t.Fatalf("TransitionToAccepted failed: %v", err)
	}

	if adr.Status != ADRStatusAccepted {
		t.Errorf("expected status 'accepted', got %q", adr.Status)
	}

	if len(adr.StatusHistory) != 1 {
		t.Fatalf("expected 1 status change, got %d", len(adr.StatusHistory))
	}

	change := adr.StatusHistory[0]
	if change.From != ADRStatusProposed {
		t.Errorf("expected from 'proposed', got %q", change.From)
	}
	if change.To != ADRStatusAccepted {
		t.Errorf("expected to 'accepted', got %q", change.To)
	}
}

func TestADRTransitionToAccepted_FromNonProposed(t *testing.T) {
	tests := []ADRStatus{
		ADRStatusDraft,
		ADRStatusAccepted,
		ADRStatusDeprecated,
		ADRStatusSuperseded,
	}

	for _, status := range tests {
		t.Run(string(status), func(t *testing.T) {
			adr := &ADR{
				ID:     "ADR-001",
				Title:  "Test ADR",
				Status: status,
			}

			err := adr.TransitionToAccepted()
			if err == nil {
				t.Fatal("expected error for non-proposed status, got nil")
			}

			if !strings.Contains(err.Error(), "must be proposed") {
				t.Errorf("error should mention 'must be proposed', got: %v", err)
			}
		})
	}
}

func TestADRMarkDeprecated_FromAccepted(t *testing.T) {
	adr := &ADR{
		ID:     "ADR-001",
		Title:  "Test ADR",
		Status: ADRStatusAccepted,
	}

	reason := "No longer relevant due to architecture changes"
	err := adr.MarkDeprecated(reason)
	if err != nil {
		t.Fatalf("MarkDeprecated failed: %v", err)
	}

	if adr.Status != ADRStatusDeprecated {
		t.Errorf("expected status 'deprecated', got %q", adr.Status)
	}

	if adr.DeprecationReason != reason {
		t.Errorf("expected deprecation reason %q, got %q", reason, adr.DeprecationReason)
	}

	if len(adr.StatusHistory) != 1 {
		t.Fatalf("expected 1 status change, got %d", len(adr.StatusHistory))
	}

	change := adr.StatusHistory[0]
	if change.From != ADRStatusAccepted {
		t.Errorf("expected from 'accepted', got %q", change.From)
	}
	if change.To != ADRStatusDeprecated {
		t.Errorf("expected to 'deprecated', got %q", change.To)
	}
	if change.Reason != reason {
		t.Errorf("expected reason %q, got %q", reason, change.Reason)
	}
}

func TestADRMarkDeprecated_FromNonAccepted(t *testing.T) {
	tests := []ADRStatus{
		ADRStatusDraft,
		ADRStatusProposed,
		ADRStatusDeprecated,
		ADRStatusSuperseded,
	}

	for _, status := range tests {
		t.Run(string(status), func(t *testing.T) {
			adr := &ADR{
				ID:     "ADR-001",
				Title:  "Test ADR",
				Status: status,
			}

			err := adr.MarkDeprecated("Some reason")
			if err == nil {
				t.Fatal("expected error for non-accepted status, got nil")
			}

			if !strings.Contains(err.Error(), "must be accepted") {
				t.Errorf("error should mention 'must be accepted', got: %v", err)
			}
		})
	}
}

func TestADRMarkDeprecated_EmptyReason(t *testing.T) {
	adr := &ADR{
		ID:     "ADR-001",
		Title:  "Test ADR",
		Status: ADRStatusAccepted,
	}

	err := adr.MarkDeprecated("")
	if err == nil {
		t.Fatal("expected error for empty reason, got nil")
	}

	if !strings.Contains(err.Error(), "reason is required") {
		t.Errorf("error should mention 'reason is required', got: %v", err)
	}

	// Status should not have changed
	if adr.Status != ADRStatusAccepted {
		t.Errorf("status should remain 'accepted', got %q", adr.Status)
	}
}

func TestADRMarkSuperseded_FromAccepted(t *testing.T) {
	adr := &ADR{
		ID:     "ADR-001",
		Title:  "Test ADR",
		Status: ADRStatusAccepted,
	}

	replacementID := "ADR-002"
	err := adr.MarkSuperseded(replacementID)
	if err != nil {
		t.Fatalf("MarkSuperseded failed: %v", err)
	}

	if adr.Status != ADRStatusSuperseded {
		t.Errorf("expected status 'superseded', got %q", adr.Status)
	}

	if adr.SupersededBy != replacementID {
		t.Errorf("expected superseded_by %q, got %q", replacementID, adr.SupersededBy)
	}

	if len(adr.StatusHistory) != 1 {
		t.Fatalf("expected 1 status change, got %d", len(adr.StatusHistory))
	}

	change := adr.StatusHistory[0]
	if change.From != ADRStatusAccepted {
		t.Errorf("expected from 'accepted', got %q", change.From)
	}
	if change.To != ADRStatusSuperseded {
		t.Errorf("expected to 'superseded', got %q", change.To)
	}
	if !strings.Contains(change.Reason, replacementID) {
		t.Errorf("reason should contain replacement ID, got %q", change.Reason)
	}
}

func TestADRMarkSuperseded_FromNonAccepted(t *testing.T) {
	tests := []ADRStatus{
		ADRStatusDraft,
		ADRStatusProposed,
		ADRStatusDeprecated,
		ADRStatusSuperseded,
	}

	for _, status := range tests {
		t.Run(string(status), func(t *testing.T) {
			adr := &ADR{
				ID:     "ADR-001",
				Title:  "Test ADR",
				Status: status,
			}

			err := adr.MarkSuperseded("ADR-002")
			if err == nil {
				t.Fatal("expected error for non-accepted status, got nil")
			}

			if !strings.Contains(err.Error(), "must be accepted") {
				t.Errorf("error should mention 'must be accepted', got: %v", err)
			}
		})
	}
}

func TestADRMarkSuperseded_EmptyReplacementID(t *testing.T) {
	adr := &ADR{
		ID:     "ADR-001",
		Title:  "Test ADR",
		Status: ADRStatusAccepted,
	}

	err := adr.MarkSuperseded("")
	if err == nil {
		t.Fatal("expected error for empty replacement ID, got nil")
	}

	if !strings.Contains(err.Error(), "replacement ADR ID is required") {
		t.Errorf("error should mention 'replacement ADR ID is required', got: %v", err)
	}

	// Status should not have changed
	if adr.Status != ADRStatusAccepted {
		t.Errorf("status should remain 'accepted', got %q", adr.Status)
	}
}

func TestADRFullLifecycle_DraftToAccepted(t *testing.T) {
	adr := &ADR{
		ID:     "ADR-001",
		Title:  "Test ADR",
		Status: ADRStatusDraft,
		Date:   "2025-02-16",
	}

	// Draft -> Proposed
	if err := adr.TransitionToProposed(); err != nil {
		t.Fatalf("TransitionToProposed failed: %v", err)
	}

	// Proposed -> Accepted
	if err := adr.TransitionToAccepted(); err != nil {
		t.Fatalf("TransitionToAccepted failed: %v", err)
	}

	if adr.Status != ADRStatusAccepted {
		t.Errorf("expected final status 'accepted', got %q", adr.Status)
	}

	// Should have 2 status changes recorded
	if len(adr.StatusHistory) != 2 {
		t.Fatalf("expected 2 status changes, got %d", len(adr.StatusHistory))
	}

	// Verify order: draft->proposed, proposed->accepted
	if adr.StatusHistory[0].From != ADRStatusDraft || adr.StatusHistory[0].To != ADRStatusProposed {
		t.Errorf("first change should be draft->proposed, got %s->%s",
			adr.StatusHistory[0].From, adr.StatusHistory[0].To)
	}

	if adr.StatusHistory[1].From != ADRStatusProposed || adr.StatusHistory[1].To != ADRStatusAccepted {
		t.Errorf("second change should be proposed->accepted, got %s->%s",
			adr.StatusHistory[1].From, adr.StatusHistory[1].To)
	}
}

func TestADRFullLifecycle_ToDeprecated(t *testing.T) {
	adr := &ADR{
		ID:     "ADR-001",
		Title:  "Test ADR",
		Status: ADRStatusDraft,
	}

	// Full lifecycle: draft -> proposed -> accepted -> deprecated
	if err := adr.TransitionToProposed(); err != nil {
		t.Fatalf("TransitionToProposed failed: %v", err)
	}
	if err := adr.TransitionToAccepted(); err != nil {
		t.Fatalf("TransitionToAccepted failed: %v", err)
	}
	if err := adr.MarkDeprecated("Technology no longer supported"); err != nil {
		t.Fatalf("MarkDeprecated failed: %v", err)
	}

	if adr.Status != ADRStatusDeprecated {
		t.Errorf("expected final status 'deprecated', got %q", adr.Status)
	}

	if len(adr.StatusHistory) != 3 {
		t.Fatalf("expected 3 status changes, got %d", len(adr.StatusHistory))
	}
}

func TestADRFullLifecycle_ToSuperseded(t *testing.T) {
	adr := &ADR{
		ID:     "ADR-001",
		Title:  "Test ADR",
		Status: ADRStatusDraft,
	}

	// Full lifecycle: draft -> proposed -> accepted -> superseded
	if err := adr.TransitionToProposed(); err != nil {
		t.Fatalf("TransitionToProposed failed: %v", err)
	}
	if err := adr.TransitionToAccepted(); err != nil {
		t.Fatalf("TransitionToAccepted failed: %v", err)
	}
	if err := adr.MarkSuperseded("ADR-002"); err != nil {
		t.Fatalf("MarkSuperseded failed: %v", err)
	}

	if adr.Status != ADRStatusSuperseded {
		t.Errorf("expected final status 'superseded', got %q", adr.Status)
	}

	if adr.SupersededBy != "ADR-002" {
		t.Errorf("expected superseded_by 'ADR-002', got %q", adr.SupersededBy)
	}

	if len(adr.StatusHistory) != 3 {
		t.Fatalf("expected 3 status changes, got %d", len(adr.StatusHistory))
	}
}

func TestADRStatusConstants(t *testing.T) {
	tests := []struct {
		status   ADRStatus
		expected string
	}{
		{ADRStatusDraft, "draft"},
		{ADRStatusProposed, "proposed"},
		{ADRStatusAccepted, "accepted"},
		{ADRStatusDeprecated, "deprecated"},
		{ADRStatusSuperseded, "superseded"},
	}

	for _, tt := range tests {
		t.Run(string(tt.status), func(t *testing.T) {
			if string(tt.status) != tt.expected {
				t.Errorf("got %q, want %q", tt.status, tt.expected)
			}
		})
	}
}
