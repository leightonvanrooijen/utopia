package storage

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/leightonvanrooijen/utopia/internal/domain"
)

func setupTestDir(t *testing.T) (string, func()) {
	t.Helper()
	dir, err := os.MkdirTemp("", "utopia-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}

	// Create necessary subdirectories
	if err := os.MkdirAll(filepath.Join(dir, "specs"), 0755); err != nil {
		t.Fatalf("failed to create specs subdir: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "change-requests"), 0755); err != nil {
		t.Fatalf("failed to create change-requests subdir: %v", err)
	}

	cleanup := func() {
		os.RemoveAll(dir)
	}

	return dir, cleanup
}

func TestLoadSpecOrChangeRequest_LoadsSpec(t *testing.T) {
	dir, cleanup := setupTestDir(t)
	defer cleanup()

	store := NewYAMLStore(dir)

	// Create a spec
	spec := domain.NewSpec("test-spec", "Test Spec")
	spec.Description = "A test spec"
	spec.Features = []domain.Feature{
		{ID: "f1", Description: "Feature 1", AcceptanceCriteria: []string{"Works"}},
	}
	if err := store.SaveSpec(spec); err != nil {
		t.Fatalf("failed to save spec: %v", err)
	}

	// Load it via LoadSpecOrChangeRequest
	loaded, isChangeRequest, err := store.LoadSpecOrChangeRequest("test-spec")
	if err != nil {
		t.Fatalf("LoadSpecOrChangeRequest failed: %v", err)
	}

	if isChangeRequest {
		t.Error("expected isChangeRequest to be false for a regular spec")
	}

	if loaded.ID != "test-spec" {
		t.Errorf("expected ID 'test-spec', got %q", loaded.ID)
	}

	if len(loaded.Features) != 1 {
		t.Errorf("expected 1 feature, got %d", len(loaded.Features))
	}
}

func TestLoadSpecOrChangeRequest_FallsBackToChangeRequest(t *testing.T) {
	dir, cleanup := setupTestDir(t)
	defer cleanup()

	store := NewYAMLStore(dir)

	// Create a change request (not a spec)
	cr := &domain.ChangeRequest{
		ID:         "my-change",
		Type:       "change-request",
		ParentSpec: "parent-spec",
		Title:      "My Change",
		Status:     domain.ChangeRequestDraft,
		Changes: []domain.Change{
			{
				Operation: "add",
				Feature: &domain.Feature{
					ID:                 "new-feature",
					Description:        "A new feature",
					AcceptanceCriteria: []string{"It works"},
				},
			},
		},
	}
	if err := store.SaveChangeRequest(cr); err != nil {
		t.Fatalf("failed to save change request: %v", err)
	}

	// Load it via LoadSpecOrChangeRequest
	loaded, isChangeRequest, err := store.LoadSpecOrChangeRequest("my-change")
	if err != nil {
		t.Fatalf("LoadSpecOrChangeRequest failed: %v", err)
	}

	if !isChangeRequest {
		t.Error("expected isChangeRequest to be true for a change request")
	}

	// The change request should be converted to a spec
	if loaded.ID != "my-change" {
		t.Errorf("expected ID 'my-change', got %q", loaded.ID)
	}

	if len(loaded.Features) != 1 {
		t.Errorf("expected 1 feature (from add operation), got %d", len(loaded.Features))
	}

	if loaded.Features[0].ID != "new-feature" {
		t.Errorf("expected feature ID 'new-feature', got %q", loaded.Features[0].ID)
	}
}

func TestLoadSpecOrChangeRequest_SpecTakesPrecedence(t *testing.T) {
	dir, cleanup := setupTestDir(t)
	defer cleanup()

	store := NewYAMLStore(dir)

	// Create both a spec and a change request with the same ID
	spec := domain.NewSpec("same-id", "Regular Spec")
	spec.Features = []domain.Feature{
		{ID: "spec-feature", Description: "From spec", AcceptanceCriteria: []string{"Spec"}},
	}
	if err := store.SaveSpec(spec); err != nil {
		t.Fatalf("failed to save spec: %v", err)
	}

	cr := &domain.ChangeRequest{
		ID:         "same-id",
		Title:      "Change Request",
		ParentSpec: "parent",
		Changes: []domain.Change{
			{
				Operation: "add",
				Feature: &domain.Feature{
					ID:                 "cr-feature",
					Description:        "From CR",
					AcceptanceCriteria: []string{"CR"}},
			},
		},
	}
	if err := store.SaveChangeRequest(cr); err != nil {
		t.Fatalf("failed to save change request: %v", err)
	}

	// Load via LoadSpecOrChangeRequest - spec should take precedence
	loaded, isChangeRequest, err := store.LoadSpecOrChangeRequest("same-id")
	if err != nil {
		t.Fatalf("LoadSpecOrChangeRequest failed: %v", err)
	}

	if isChangeRequest {
		t.Error("expected spec to take precedence, but got change request")
	}

	if loaded.Features[0].ID != "spec-feature" {
		t.Errorf("expected feature from spec, got %q", loaded.Features[0].ID)
	}
}

func TestLoadSpecOrChangeRequest_NeitherExists(t *testing.T) {
	dir, cleanup := setupTestDir(t)
	defer cleanup()

	store := NewYAMLStore(dir)

	// Try to load a non-existent ID
	_, _, err := store.LoadSpecOrChangeRequest("nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent spec/change request, got nil")
	}

	// Error should mention both locations
	if !strings.Contains(err.Error(), "specs") {
		t.Error("error should mention specs directory")
	}
	if !strings.Contains(err.Error(), "change-requests") {
		t.Error("error should mention change-requests directory")
	}
}

func TestLoadSpecOrChangeRequest_ChangeRequestWithAllOperations(t *testing.T) {
	dir, cleanup := setupTestDir(t)
	defer cleanup()

	store := NewYAMLStore(dir)

	// Create a change request with all operation types
	cr := &domain.ChangeRequest{
		ID:         "full-cr",
		Title:      "Full Change Request",
		ParentSpec: "parent",
		Changes: []domain.Change{
			{
				Operation: "add",
				Feature: &domain.Feature{
					ID:                 "added-feature",
					Description:        "New feature",
					AcceptanceCriteria: []string{"Works"},
				},
			},
			{
				Operation: "modify",
				FeatureID: "existing-feature",
				Criteria: &domain.CriteriaModify{
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
	if err := store.SaveChangeRequest(cr); err != nil {
		t.Fatalf("failed to save change request: %v", err)
	}

	loaded, isChangeRequest, err := store.LoadSpecOrChangeRequest("full-cr")
	if err != nil {
		t.Fatalf("LoadSpecOrChangeRequest failed: %v", err)
	}

	if !isChangeRequest {
		t.Error("expected isChangeRequest to be true")
	}

	// Should have 3 features: 1 add, 1 modify, 1 remove
	if len(loaded.Features) != 3 {
		t.Errorf("expected 3 features, got %d", len(loaded.Features))
	}

	// Verify feature IDs
	ids := make(map[string]bool)
	for _, f := range loaded.Features {
		ids[f.ID] = true
	}

	if !ids["added-feature"] {
		t.Error("missing 'added-feature'")
	}
	if !ids["modify-existing-feature"] {
		t.Error("missing 'modify-existing-feature'")
	}
	if !ids["remove-old-feature"] {
		t.Error("missing 'remove-old-feature'")
	}
}

// Tests for merge workflow

func TestMergeWorkflow_AddFeature(t *testing.T) {
	dir, cleanup := setupTestDir(t)
	defer cleanup()

	store := NewYAMLStore(dir)

	// Create parent spec
	parentSpec := domain.NewSpec("parent-spec", "Parent Spec")
	parentSpec.Features = []domain.Feature{
		{ID: "existing-feature", Description: "Already here", AcceptanceCriteria: []string{"Works"}},
	}
	if err := store.SaveSpec(parentSpec); err != nil {
		t.Fatalf("failed to save parent spec: %v", err)
	}

	// Create change request that adds a feature
	cr := &domain.ChangeRequest{
		ID:         "add-feature-cr",
		Title:      "Add New Feature",
		ParentSpec: "parent-spec",
		Changes: []domain.Change{
			{
				Operation: "add",
				Feature: &domain.Feature{
					ID:                 "new-feature",
					Description:        "Brand new",
					AcceptanceCriteria: []string{"It works"},
				},
			},
		},
	}
	if err := store.SaveChangeRequest(cr); err != nil {
		t.Fatalf("failed to save change request: %v", err)
	}

	// Apply changes (simulating merge)
	if err := cr.ApplyChanges(parentSpec); err != nil {
		t.Fatalf("ApplyChanges failed: %v", err)
	}

	// Save updated spec
	if err := store.SaveSpec(parentSpec); err != nil {
		t.Fatalf("failed to save updated spec: %v", err)
	}

	// Reload and verify
	reloaded, err := store.LoadSpec("parent-spec")
	if err != nil {
		t.Fatalf("failed to reload spec: %v", err)
	}

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
	dir, cleanup := setupTestDir(t)
	defer cleanup()

	store := NewYAMLStore(dir)

	// Create parent spec
	parentSpec := domain.NewSpec("parent-spec", "Parent Spec")
	parentSpec.Features = []domain.Feature{
		{ID: "my-feature", Description: "Original", AcceptanceCriteria: []string{"Old criterion"}},
	}
	if err := store.SaveSpec(parentSpec); err != nil {
		t.Fatalf("failed to save parent spec: %v", err)
	}

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
	if err := store.SaveChangeRequest(cr); err != nil {
		t.Fatalf("failed to save change request: %v", err)
	}

	// Apply changes
	if err := cr.ApplyChanges(parentSpec); err != nil {
		t.Fatalf("ApplyChanges failed: %v", err)
	}

	// Save and reload
	if err := store.SaveSpec(parentSpec); err != nil {
		t.Fatalf("failed to save updated spec: %v", err)
	}

	reloaded, err := store.LoadSpec("parent-spec")
	if err != nil {
		t.Fatalf("failed to reload spec: %v", err)
	}

	if reloaded.Features[0].Description != "Updated description" {
		t.Errorf("expected updated description, got %q", reloaded.Features[0].Description)
	}

	if len(reloaded.Features[0].AcceptanceCriteria) != 2 {
		t.Errorf("expected 2 criteria after merge, got %d", len(reloaded.Features[0].AcceptanceCriteria))
	}
}

func TestMergeWorkflow_RemoveFeature(t *testing.T) {
	dir, cleanup := setupTestDir(t)
	defer cleanup()

	store := NewYAMLStore(dir)

	// Create parent spec with two features
	parentSpec := domain.NewSpec("parent-spec", "Parent Spec")
	parentSpec.Features = []domain.Feature{
		{ID: "keep-feature", Description: "Keep this", AcceptanceCriteria: []string{"Works"}},
		{ID: "remove-feature", Description: "Remove this", AcceptanceCriteria: []string{"Old"}},
	}
	if err := store.SaveSpec(parentSpec); err != nil {
		t.Fatalf("failed to save parent spec: %v", err)
	}

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
	if err := store.SaveChangeRequest(cr); err != nil {
		t.Fatalf("failed to save change request: %v", err)
	}

	// Apply changes
	if err := cr.ApplyChanges(parentSpec); err != nil {
		t.Fatalf("ApplyChanges failed: %v", err)
	}

	// Save and reload
	if err := store.SaveSpec(parentSpec); err != nil {
		t.Fatalf("failed to save updated spec: %v", err)
	}

	reloaded, err := store.LoadSpec("parent-spec")
	if err != nil {
		t.Fatalf("failed to reload spec: %v", err)
	}

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
	dir, cleanup := setupTestDir(t)
	defer cleanup()

	store := NewYAMLStore(dir)

	// Create parent spec
	parentSpec := domain.NewSpec("parent-spec", "Parent Spec")
	if err := store.SaveSpec(parentSpec); err != nil {
		t.Fatalf("failed to save parent spec: %v", err)
	}

	// Create change request
	cr := &domain.ChangeRequest{
		ID:         "to-delete-cr",
		Title:      "Will Be Deleted",
		ParentSpec: "parent-spec",
		Changes: []domain.Change{
			{
				Operation: "add",
				Feature: &domain.Feature{
					ID:                 "new-feature",
					Description:        "New",
					AcceptanceCriteria: []string{"Works"},
				},
			},
		},
	}
	if err := store.SaveChangeRequest(cr); err != nil {
		t.Fatalf("failed to save change request: %v", err)
	}

	// Verify it exists
	_, err := store.LoadChangeRequest("to-delete-cr")
	if err != nil {
		t.Fatalf("change request should exist before merge: %v", err)
	}

	// Apply changes and delete
	if err := cr.ApplyChanges(parentSpec); err != nil {
		t.Fatalf("ApplyChanges failed: %v", err)
	}
	if err := store.SaveSpec(parentSpec); err != nil {
		t.Fatalf("failed to save updated spec: %v", err)
	}
	if err := store.DeleteChangeRequest("to-delete-cr"); err != nil {
		t.Fatalf("failed to delete change request: %v", err)
	}

	// Verify it's gone
	_, err = store.LoadChangeRequest("to-delete-cr")
	if err == nil {
		t.Error("change request should not exist after deletion")
	}
}

func TestYAMLFormatting_FeatureSpacing(t *testing.T) {
	dir, cleanup := setupTestDir(t)
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

	if err := store.SaveSpec(spec); err != nil {
		t.Fatalf("failed to save spec: %v", err)
	}

	// Read the raw file to check formatting
	content, err := os.ReadFile(filepath.Join(dir, "specs", "test-spec.yaml"))
	if err != nil {
		t.Fatalf("failed to read file: %v", err)
	}

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
	dir, cleanup := setupTestDir(t)
	defer cleanup()

	store := NewYAMLStore(dir)

	// Create a spec with a multi-line description
	spec := domain.NewSpec("block-test", "Block Style Test")
	spec.Features = []domain.Feature{
		{
			ID:                 "multiline-feature",
			Description:        "This is a longer description\nthat should use block style",
			AcceptanceCriteria: []string{"Works"},
		},
	}

	if err := store.SaveSpec(spec); err != nil {
		t.Fatalf("failed to save spec: %v", err)
	}

	// Read and verify
	content, err := os.ReadFile(filepath.Join(dir, "specs", "block-test.yaml"))
	if err != nil {
		t.Fatalf("failed to read file: %v", err)
	}

	if !strings.Contains(string(content), "description: |") {
		t.Errorf("expected block style for multi-line description, got:\n%s", string(content))
	}
}

func TestMergeWorkflow_FullScenario(t *testing.T) {
	dir, cleanup := setupTestDir(t)
	defer cleanup()

	store := NewYAMLStore(dir)

	// Create parent spec with existing content
	parentSpec := domain.NewSpec("execution-ralph", "Ralph Execution Loop")
	parentSpec.Description = "The Ralph execution loop"
	parentSpec.DomainKnowledge = []string{"Existing knowledge"}
	parentSpec.Features = []domain.Feature{
		{ID: "ralph-loop", Description: "Core loop", AcceptanceCriteria: []string{"Loops correctly"}},
		{ID: "old-feature", Description: "To be removed", AcceptanceCriteria: []string{"Old"}},
	}
	if err := store.SaveSpec(parentSpec); err != nil {
		t.Fatalf("failed to save parent spec: %v", err)
	}

	// Create change request with all operation types
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
				Feature: &domain.Feature{
					ID:                 "timeout-flag",
					Description:        "Add --timeout flag",
					AcceptanceCriteria: []string{"Flag accepts minutes", "Flag is optional"},
				},
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
	if err := store.SaveChangeRequest(cr); err != nil {
		t.Fatalf("failed to save change request: %v", err)
	}

	// Simulate full merge workflow
	if err := cr.ApplyChanges(parentSpec); err != nil {
		t.Fatalf("ApplyChanges failed: %v", err)
	}
	if err := store.SaveSpec(parentSpec); err != nil {
		t.Fatalf("failed to save updated spec: %v", err)
	}
	if err := store.DeleteChangeRequest("execution-ralph-add-timeout"); err != nil {
		t.Fatalf("failed to delete change request: %v", err)
	}

	// Reload and verify final state
	final, err := store.LoadSpec("execution-ralph")
	if err != nil {
		t.Fatalf("failed to reload spec: %v", err)
	}

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
