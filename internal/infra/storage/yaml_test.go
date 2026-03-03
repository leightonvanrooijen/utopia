package storage

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/leightonvanrooijen/utopia/internal/domain"
	"github.com/leightonvanrooijen/utopia/internal/testutil"
)

// Tests for merge workflow

func TestMergeWorkflow_AddFeature(t *testing.T) {
	store, cleanup := SetupTestStore(t)
	defer cleanup()

	// Create parent spec
	parentSpec := domain.NewSpec("parent-spec", "Parent Spec")
	parentSpec.Features = []domain.Feature{
		testutil.NewTestFeature("existing-feature", "Already here", "Works"),
	}
	testutil.AssertNoError(t, store.SaveSpec(parentSpec))

	// Create change request that adds a feature
	newFeature := testutil.NewTestFeature("new-feature", "Brand new", "It works")
	cr := &domain.ChangeRequest{
		ID:         "add-feature-cr",
		Title:      "Add New Feature",
		ParentSpec: "parent-spec",
		Changes: []domain.Change{
			{
				Operation: "add",
				Feature:   &newFeature,
			},
		},
	}
	testutil.AssertNoError(t, store.SaveChangeRequest(cr))

	// Apply changes (simulating merge)
	testutil.AssertNoError(t, cr.ApplyChanges(parentSpec))

	// Save updated spec
	testutil.AssertNoError(t, store.SaveSpec(parentSpec))

	// Reload and verify
	reloaded, err := store.LoadSpec("parent-spec")
	testutil.AssertNoError(t, err)

	if len(reloaded.Features) != 2 {
		t.Errorf("expected 2 features after merge, got %d", len(reloaded.Features))
	}

	if !reloaded.HasFeature("new-feature") {
		t.Error("new-feature should exist after merge")
	}
	if !reloaded.HasFeature("existing-feature") {
		t.Error("existing-feature should still exist after merge")
	}
}

func TestMergeWorkflow_ModifyFeature(t *testing.T) {
	store, cleanup := SetupTestStore(t)
	defer cleanup()

	// Create parent spec
	parentSpec := domain.NewSpec("parent-spec", "Parent Spec")
	parentSpec.Features = []domain.Feature{
		testutil.NewTestFeature("my-feature", "Original", "Old criterion"),
	}
	testutil.AssertNoError(t, store.SaveSpec(parentSpec))

	// Create change request that modifies the feature
	cr := &domain.ChangeRequest{
		ID:         "modify-feature-cr",
		Title:      "Modify Feature",
		ParentSpec: "parent-spec",
		Changes: []domain.Change{
			{
				Operation:   "modify",
				FeatureID:   "my-feature",
				Description: "Updated description",
				Criteria: &domain.CriteriaModify{
					Add: []string{"New criterion"},
				},
			},
		},
	}
	testutil.AssertNoError(t, store.SaveChangeRequest(cr))

	// Apply changes
	testutil.AssertNoError(t, cr.ApplyChanges(parentSpec))

	// Save and reload
	testutil.AssertNoError(t, store.SaveSpec(parentSpec))

	reloaded, err := store.LoadSpec("parent-spec")
	testutil.AssertNoError(t, err)

	if reloaded.Features[0].Description != "Updated description" {
		t.Errorf("expected updated description, got %q", reloaded.Features[0].Description)
	}

	if len(reloaded.Features[0].AcceptanceCriteria) != 2 {
		t.Errorf("expected 2 criteria after merge, got %d", len(reloaded.Features[0].AcceptanceCriteria))
	}
}

func TestMergeWorkflow_RemoveFeature(t *testing.T) {
	store, cleanup := SetupTestStore(t)
	defer cleanup()

	// Create parent spec with two features
	parentSpec := domain.NewSpec("parent-spec", "Parent Spec")
	parentSpec.Features = []domain.Feature{
		testutil.NewTestFeature("keep-feature", "Keep this", "Works"),
		testutil.NewTestFeature("remove-feature", "Remove this", "Old"),
	}
	testutil.AssertNoError(t, store.SaveSpec(parentSpec))

	// Create change request that removes a feature
	cr := &domain.ChangeRequest{
		ID:         "remove-feature-cr",
		Title:      "Remove Feature",
		ParentSpec: "parent-spec",
		Changes: []domain.Change{
			{
				Operation: "remove",
				FeatureID: "remove-feature",
				Reason:    "No longer needed",
			},
		},
	}
	testutil.AssertNoError(t, store.SaveChangeRequest(cr))

	// Apply changes
	testutil.AssertNoError(t, cr.ApplyChanges(parentSpec))

	// Save and reload
	testutil.AssertNoError(t, store.SaveSpec(parentSpec))

	reloaded, err := store.LoadSpec("parent-spec")
	testutil.AssertNoError(t, err)

	if len(reloaded.Features) != 1 {
		t.Errorf("expected 1 feature after merge, got %d", len(reloaded.Features))
	}

	if reloaded.HasFeature("remove-feature") {
		t.Error("remove-feature should not exist after merge")
	}
	if !reloaded.HasFeature("keep-feature") {
		t.Error("keep-feature should still exist after merge")
	}
}

func TestMergeWorkflow_DeleteChangeRequestAfterMerge(t *testing.T) {
	store, cleanup := SetupTestStore(t)
	defer cleanup()

	// Create parent spec
	parentSpec := domain.NewSpec("parent-spec", "Parent Spec")
	testutil.AssertNoError(t, store.SaveSpec(parentSpec))

	// Create change request
	newFeature := testutil.NewTestFeature("new-feature", "New", "Works")
	cr := &domain.ChangeRequest{
		ID:         "to-delete-cr",
		Title:      "Will Be Deleted",
		ParentSpec: "parent-spec",
		Changes: []domain.Change{
			{
				Operation: "add",
				Feature:   &newFeature,
			},
		},
	}
	testutil.AssertNoError(t, store.SaveChangeRequest(cr))

	// Verify it exists
	_, err := store.LoadChangeRequest("to-delete-cr")
	if err != nil {
		t.Fatalf("change request should exist before merge: %v", err)
	}

	// Apply changes and delete
	testutil.AssertNoError(t, cr.ApplyChanges(parentSpec))
	testutil.AssertNoError(t, store.SaveSpec(parentSpec))
	testutil.AssertNoError(t, store.DeleteChangeRequest("to-delete-cr"))

	// Verify it's gone
	_, err = store.LoadChangeRequest("to-delete-cr")
	if err == nil {
		t.Error("change request should not exist after deletion")
	}
}

func TestYAMLFormatting_FeatureSpacing(t *testing.T) {
	dir, cleanup := testutil.SetupTestProject(t)
	defer cleanup()

	store := NewYAMLStore(dir)

	// Create a spec with multiple features
	spec := domain.NewSpec("test-spec", "Test Spec")
	spec.Features = []domain.Feature{
		{
			ID:                 "feature-one",
			Description:        "First feature with a longer description\nthat spans multiple lines",
			AcceptanceCriteria: []string{"Criterion A", "Criterion B"},
		},
		{
			ID:                 "feature-two",
			Description:        "Second feature",
			AcceptanceCriteria: []string{"Criterion C"},
		},
		{
			ID:                 "feature-three",
			Description:        "Third feature",
			AcceptanceCriteria: []string{"Criterion D"},
		},
	}

	testutil.AssertNoError(t, store.SaveSpec(spec))

	// Read the raw file to check formatting
	content, err := os.ReadFile(filepath.Join(dir, "specs", "test-spec.yaml"))
	testutil.AssertNoError(t, err)

	contentStr := string(content)

	// Verify blank lines between features (4-space indent from yaml.Marshal)
	if !strings.Contains(contentStr, "\n\n    - id: feature-two") {
		t.Errorf("expected blank line before feature-two, got:\n%s", contentStr)
	}
	if !strings.Contains(contentStr, "\n\n    - id: feature-three") {
		t.Errorf("expected blank line before feature-three, got:\n%s", contentStr)
	}

	// Verify block style for multi-line description
	if !strings.Contains(contentStr, "description: |") {
		t.Error("expected block style (|) for multi-line description")
	}
}

func TestYAMLFormatting_BlockStyleDescription(t *testing.T) {
	dir, cleanup := testutil.SetupTestProject(t)
	defer cleanup()

	store := NewYAMLStore(dir)

	// Create a spec with a multi-line description
	spec := domain.NewSpec("block-test", "Block Style Test")
	spec.Features = []domain.Feature{
		testutil.NewTestFeature("multiline-feature", "This is a longer description\nthat should use block style", "Works"),
	}

	testutil.AssertNoError(t, store.SaveSpec(spec))

	// Read and verify
	content, err := os.ReadFile(filepath.Join(dir, "specs", "block-test.yaml"))
	testutil.AssertNoError(t, err)

	if !strings.Contains(string(content), "description: |") {
		t.Errorf("expected block style for multi-line description, got:\n%s", string(content))
	}
}

func TestDeleteSpec_Success(t *testing.T) {
	store, cleanup := SetupTestStore(t)
	defer cleanup()

	// Create a spec
	spec := domain.NewSpec("to-delete", "Spec To Delete")
	spec.Features = []domain.Feature{
		testutil.NewTestFeature("feature-1", "A feature", "Works"),
	}
	testutil.AssertNoError(t, store.SaveSpec(spec))

	// Verify it exists
	_, err := store.LoadSpec("to-delete")
	if err != nil {
		t.Fatalf("spec should exist before deletion: %v", err)
	}

	// Delete it
	testutil.AssertNoError(t, store.DeleteSpec("to-delete"))

	// Verify it's gone
	_, err = store.LoadSpec("to-delete")
	if err == nil {
		t.Error("spec should not exist after deletion")
	}
}

func TestDeleteSpec_NotFound(t *testing.T) {
	store, cleanup := SetupTestStore(t)
	defer cleanup()

	// Try to delete a non-existent spec
	err := store.DeleteSpec("nonexistent")
	if err == nil {
		t.Fatal("expected error when deleting nonexistent spec, got nil")
	}

	if !strings.Contains(err.Error(), "spec not found") {
		t.Errorf("error should mention 'spec not found', got: %v", err)
	}
}

func TestMergeWorkflow_FullScenario(t *testing.T) {
	store, cleanup := SetupTestStore(t)
	defer cleanup()

	// Create parent spec with existing content
	parentSpec := domain.NewSpec("execution-ralph", "Ralph Execution Loop")
	parentSpec.Description = "The Ralph execution loop"
	parentSpec.DomainKnowledge = []string{"Existing knowledge"}
	parentSpec.Features = []domain.Feature{
		testutil.NewTestFeature("ralph-loop", "Core loop", "Loops correctly"),
		testutil.NewTestFeature("old-feature", "To be removed", "Old"),
	}
	testutil.AssertNoError(t, store.SaveSpec(parentSpec))

	// Create change request with all operation types
	timeoutFeature := testutil.NewTestFeature("timeout-flag", "Add --timeout flag", "Flag accepts minutes", "Flag is optional")
	cr := &domain.ChangeRequest{
		ID:         "execution-ralph-add-timeout",
		Title:      "Add timeout feature",
		ParentSpec: "execution-ralph",
		Changes: []domain.Change{
			{
				Operation:       "add",
				DomainKnowledge: []string{"Timeout is session-level"},
			},
			{
				Operation: "add",
				Feature:   &timeoutFeature,
			},
			{
				Operation: "modify",
				FeatureID: "ralph-loop",
				Criteria: &domain.CriteriaModify{
					Add: []string{"Respects timeout setting"},
				},
			},
			{
				Operation: "remove",
				FeatureID: "old-feature",
				Reason:    "Replaced by new approach",
			},
		},
	}
	testutil.AssertNoError(t, store.SaveChangeRequest(cr))

	// Simulate full merge workflow
	testutil.AssertNoError(t, cr.ApplyChanges(parentSpec))
	testutil.AssertNoError(t, store.SaveSpec(parentSpec))
	testutil.AssertNoError(t, store.DeleteChangeRequest("execution-ralph-add-timeout"))

	// Reload and verify final state
	final, err := store.LoadSpec("execution-ralph")
	testutil.AssertNoError(t, err)

	// Check domain knowledge
	if len(final.DomainKnowledge) != 2 {
		t.Errorf("expected 2 domain knowledge items, got %d", len(final.DomainKnowledge))
	}

	// Check features
	if len(final.Features) != 2 {
		t.Errorf("expected 2 features (ralph-loop + timeout-flag), got %d", len(final.Features))
	}

	if !final.HasFeature("ralph-loop") {
		t.Error("ralph-loop should exist")
	}
	if !final.HasFeature("timeout-flag") {
		t.Error("timeout-flag should exist (added)")
	}
	if final.HasFeature("old-feature") {
		t.Error("old-feature should not exist (removed)")
	}

	// Check modification was applied
	for _, f := range final.Features {
		if f.ID == "ralph-loop" {
			found := false
			for _, c := range f.AcceptanceCriteria {
				if c == "Respects timeout setting" {
					found = true
					break
				}
			}
			if !found {
				t.Error("ralph-loop should have the new criterion added")
			}
		}
	}
}

// Tests for ADR storage validation

func TestSaveADR_ValidCategory(t *testing.T) {
	store, cleanup := SetupTestStore(t)
	defer cleanup()

	adr := &domain.ADR{
		ID:       "ADR-001",
		Title:    "Test Decision",
		Status:   domain.ADRStatusDraft,
		Category: domain.ADRCategoryStructure,
		Context:  "Test context",
		Decision: "We decided to test",
	}

	err := store.SaveADR(adr)
	if err != nil {
		t.Errorf("SaveADR should succeed with valid category, got: %v", err)
	}

	// Verify it was saved
	loaded, err := store.LoadADR("ADR-001")
	if err != nil {
		t.Fatalf("failed to load saved ADR: %v", err)
	}

	if loaded.Category != domain.ADRCategoryStructure {
		t.Errorf("loaded ADR should have structure category, got %q", loaded.Category)
	}
}

func TestSaveADR_InvalidCategory_Rejected(t *testing.T) {
	store, cleanup := SetupTestStore(t)
	defer cleanup()

	adr := &domain.ADR{
		ID:       "ADR-002",
		Title:    "Test Decision",
		Status:   domain.ADRStatusDraft,
		Category: domain.ADRCategory("invalid-category"),
		Context:  "Test context",
		Decision: "We decided something",
	}

	err := store.SaveADR(adr)
	if err == nil {
		t.Fatal("SaveADR should reject ADR with invalid category")
	}

	if !strings.Contains(err.Error(), "ADR validation failed") {
		t.Errorf("error should mention 'ADR validation failed', got: %v", err)
	}

	if !strings.Contains(err.Error(), "invalid ADR category") {
		t.Errorf("error should mention 'invalid ADR category', got: %v", err)
	}
}

func TestSaveADR_EmptyCategory_Rejected(t *testing.T) {
	store, cleanup := SetupTestStore(t)
	defer cleanup()

	adr := &domain.ADR{
		ID:       "ADR-003",
		Title:    "Test Decision",
		Status:   domain.ADRStatusDraft,
		Category: "",
		Context:  "Test context",
		Decision: "We decided something",
	}

	err := store.SaveADR(adr)
	if err == nil {
		t.Fatal("SaveADR should reject ADR with empty category")
	}

	if !strings.Contains(err.Error(), "ADR validation failed") {
		t.Errorf("error should mention 'ADR validation failed', got: %v", err)
	}
}

func TestSaveADR_AllValidCategories(t *testing.T) {
	store, cleanup := SetupTestStore(t)
	defer cleanup()

	categories := []domain.ADRCategory{
		domain.ADRCategoryStructure,
		domain.ADRCategoryNFR,
		domain.ADRCategoryDependencies,
		domain.ADRCategoryInterfaces,
		domain.ADRCategoryConstruction,
	}

	for i, cat := range categories {
		t.Run(string(cat), func(t *testing.T) {
			adr := &domain.ADR{
				ID:       "ADR-" + string(rune('A'+i)),
				Title:    "Test Decision for " + string(cat),
				Status:   domain.ADRStatusDraft,
				Category: cat,
				Context:  "Test context",
				Decision: "We decided to use " + string(cat),
			}

			err := store.SaveADR(adr)
			if err != nil {
				t.Errorf("SaveADR should succeed with %q category, got: %v", cat, err)
			}
		})
	}
}
