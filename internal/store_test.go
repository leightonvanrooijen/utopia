package internal

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/leightonvanrooijen/utopia/internal/domain"
)

// SetupTestStore creates a YAMLStore backed by a temporary directory with the
// necessary .utopia structure (specs/ and change-requests/ subdirectories).
// Returns the store and a cleanup function.
func SetupTestStore(t *testing.T) (*YAMLStore, func()) {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "specs"), 0755); err != nil {
		t.Fatalf("failed to create specs subdir: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "change-requests"), 0755); err != nil {
		t.Fatalf("failed to create change-requests subdir: %v", err)
	}
	return NewYAMLStore(dir), func() {}
}

// Tests for merge workflow

func TestMergeWorkflow_AddFeature(t *testing.T) {
	store, cleanup := SetupTestStore(t)
	defer cleanup()

	// Create parent spec
	parentSpec := domain.NewSpec("parent-spec", "Parent Spec")
	parentSpec.Features = []domain.Feature{
		{ID: "existing-feature", Description: "Already here", AcceptanceCriteria: []string{"Works"}},
	}
	if err := store.SaveSpec(parentSpec); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Create change request that adds a feature
	newFeature := domain.Feature{ID: "new-feature", Description: "Brand new", AcceptanceCriteria: []string{"It works"}}
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
	if err := store.SaveChangeRequest(cr); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Apply changes (simulating merge)
	if err := cr.ApplyChanges(parentSpec); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Save updated spec
	if err := store.SaveSpec(parentSpec); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Reload and verify
	reloaded, err := store.LoadSpec("parent-spec")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
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
	store, cleanup := SetupTestStore(t)
	defer cleanup()

	// Create parent spec
	parentSpec := domain.NewSpec("parent-spec", "Parent Spec")
	parentSpec.Features = []domain.Feature{
		{ID: "my-feature", Description: "Original", AcceptanceCriteria: []string{"Old criterion"}},
	}
	if err := store.SaveSpec(parentSpec); err != nil {
		t.Fatalf("unexpected error: %v", err)
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
		t.Fatalf("unexpected error: %v", err)
	}

	// Apply changes
	if err := cr.ApplyChanges(parentSpec); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Save and reload
	if err := store.SaveSpec(parentSpec); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	reloaded, err := store.LoadSpec("parent-spec")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

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
		{ID: "keep-feature", Description: "Keep this", AcceptanceCriteria: []string{"Works"}},
		{ID: "remove-feature", Description: "Remove this", AcceptanceCriteria: []string{"Old"}},
	}
	if err := store.SaveSpec(parentSpec); err != nil {
		t.Fatalf("unexpected error: %v", err)
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
		t.Fatalf("unexpected error: %v", err)
	}

	// Apply changes
	if err := cr.ApplyChanges(parentSpec); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Save and reload
	if err := store.SaveSpec(parentSpec); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	reloaded, err := store.LoadSpec("parent-spec")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
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
	store, cleanup := SetupTestStore(t)
	defer cleanup()

	// Create parent spec
	parentSpec := domain.NewSpec("parent-spec", "Parent Spec")
	if err := store.SaveSpec(parentSpec); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Create change request
	newFeature := domain.Feature{ID: "new-feature", Description: "New", AcceptanceCriteria: []string{"Works"}}
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
	if err := store.SaveChangeRequest(cr); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify it exists
	_, err := store.LoadChangeRequest("to-delete-cr")
	if err != nil {
		t.Fatalf("change request should exist before merge: %v", err)
	}

	// Apply changes and delete
	if err := cr.ApplyChanges(parentSpec); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := store.SaveSpec(parentSpec); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := store.DeleteChangeRequest("to-delete-cr"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify it's gone
	_, err = store.LoadChangeRequest("to-delete-cr")
	if err == nil {
		t.Error("change request should not exist after deletion")
	}
}

func TestYAMLFormatting_FeatureSpacing(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "specs"), 0755); err != nil {
		t.Fatalf("failed to create specs subdir: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "change-requests"), 0755); err != nil {
		t.Fatalf("failed to create change-requests subdir: %v", err)
	}

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
		t.Fatalf("unexpected error: %v", err)
	}

	// Read the raw file to check formatting
	content, err := os.ReadFile(filepath.Join(dir, "specs", "test-spec.yaml"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
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
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "specs"), 0755); err != nil {
		t.Fatalf("failed to create specs subdir: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "change-requests"), 0755); err != nil {
		t.Fatalf("failed to create change-requests subdir: %v", err)
	}

	store := NewYAMLStore(dir)

	// Create a spec with a multi-line description
	spec := domain.NewSpec("block-test", "Block Style Test")
	spec.Features = []domain.Feature{
		{ID: "multiline-feature", Description: "This is a longer description\nthat should use block style", AcceptanceCriteria: []string{"Works"}},
	}

	if err := store.SaveSpec(spec); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Read and verify
	content, err := os.ReadFile(filepath.Join(dir, "specs", "block-test.yaml"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

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
		{ID: "feature-1", Description: "A feature", AcceptanceCriteria: []string{"Works"}},
	}
	if err := store.SaveSpec(spec); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify it exists
	_, err := store.LoadSpec("to-delete")
	if err != nil {
		t.Fatalf("spec should exist before deletion: %v", err)
	}

	// Delete it
	if err := store.DeleteSpec("to-delete"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

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

func TestValidateValidatorPaths_AllExist(t *testing.T) {
	store, cleanup := SetupTestStore(t)
	defer cleanup()

	// Create validators directory and files
	validatorDir := filepath.Join(store.baseDir, "validators")
	if err := os.MkdirAll(validatorDir, 0755); err != nil {
		t.Fatalf("failed to create validators dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(validatorDir, "test1.md"), []byte("content"), 0644); err != nil {
		t.Fatalf("failed to create test1.md: %v", err)
	}
	if err := os.WriteFile(filepath.Join(validatorDir, "test2.md"), []byte("content"), 0644); err != nil {
		t.Fatalf("failed to create test2.md: %v", err)
	}

	validators := []domain.ValidatorConfig{
		{Path: "validators/test1.md"},
		{Path: "validators/test2.md"},
	}

	err := store.ValidateValidatorPaths(validators)
	if err != nil {
		t.Errorf("expected no error when all files exist, got: %v", err)
	}
}

func TestValidateValidatorPaths_EmptyList(t *testing.T) {
	store, cleanup := SetupTestStore(t)
	defer cleanup()

	// Empty validators list should succeed (validators are optional)
	err := store.ValidateValidatorPaths([]domain.ValidatorConfig{})
	if err != nil {
		t.Errorf("expected no error for empty validators list, got: %v", err)
	}

	// Nil validators list should also succeed
	err = store.ValidateValidatorPaths(nil)
	if err != nil {
		t.Errorf("expected no error for nil validators list, got: %v", err)
	}
}

func TestValidateValidatorPaths_SingleMissing(t *testing.T) {
	store, cleanup := SetupTestStore(t)
	defer cleanup()

	validators := []domain.ValidatorConfig{{Path: "validators/nonexistent.md"}}

	err := store.ValidateValidatorPaths(validators)
	if err == nil {
		t.Fatal("expected error when validator file is missing")
	}

	if !strings.Contains(err.Error(), "validator file not found") {
		t.Errorf("error should mention 'validator file not found', got: %v", err)
	}
	if !strings.Contains(err.Error(), "validators/nonexistent.md") {
		t.Errorf("error should include the missing file path, got: %v", err)
	}
}

func TestValidateValidatorPaths_MultipleMissing(t *testing.T) {
	store, cleanup := SetupTestStore(t)
	defer cleanup()

	// Create one file but reference multiple
	validatorDir := filepath.Join(store.baseDir, "validators")
	if err := os.MkdirAll(validatorDir, 0755); err != nil {
		t.Fatalf("failed to create validators dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(validatorDir, "exists.md"), []byte("content"), 0644); err != nil {
		t.Fatalf("failed to create exists.md: %v", err)
	}

	validators := []domain.ValidatorConfig{
		{Path: "validators/exists.md"},
		{Path: "validators/missing1.md"},
		{Path: "validators/missing2.md"},
	}

	err := store.ValidateValidatorPaths(validators)
	if err == nil {
		t.Fatal("expected error when validator files are missing")
	}

	if !strings.Contains(err.Error(), "validator files not found") {
		t.Errorf("error should mention 'validator files not found' (plural), got: %v", err)
	}
	if !strings.Contains(err.Error(), "missing1.md") {
		t.Errorf("error should include missing1.md, got: %v", err)
	}
	if !strings.Contains(err.Error(), "missing2.md") {
		t.Errorf("error should include missing2.md, got: %v", err)
	}
}

func TestConfigValidators_LoadFromYAML(t *testing.T) {
	store, cleanup := SetupTestStore(t)
	defer cleanup()

	// Write config with validators (string format - backward compatible)
	configContent := `project_context: Test context
verification:
    command: ./test.sh
validators:
    - validators/code-standards.md
    - validators/naming.md
`
	if err := os.WriteFile(filepath.Join(store.baseDir, "config.yaml"), []byte(configContent), 0644); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	config, err := store.LoadConfig()
	if err != nil {
		t.Fatalf("unexpected error loading config: %v", err)
	}

	if len(config.Validators) != 2 {
		t.Errorf("expected 2 validators, got %d", len(config.Validators))
	}
	if config.Validators[0].GetPath() != "validators/code-standards.md" {
		t.Errorf("expected first validator path 'validators/code-standards.md', got %q", config.Validators[0].GetPath())
	}
	if config.Validators[1].GetPath() != "validators/naming.md" {
		t.Errorf("expected second validator path 'validators/naming.md', got %q", config.Validators[1].GetPath())
	}
}

func TestConfigValidators_OmittedWhenEmpty(t *testing.T) {
	store, cleanup := SetupTestStore(t)
	defer cleanup()

	// Write config without validators (older config format)
	configContent := `project_context: Test context
verification:
    command: ./test.sh
`
	if err := os.WriteFile(filepath.Join(store.baseDir, "config.yaml"), []byte(configContent), 0644); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	config, err := store.LoadConfig()
	if err != nil {
		t.Fatalf("unexpected error loading config: %v", err)
	}

	// Validators should be nil/empty when not configured
	if len(config.Validators) != 0 {
		t.Errorf("expected empty validators when not configured, got %d", len(config.Validators))
	}
}

func TestConfigValidators_ObjectFormat(t *testing.T) {
	store, cleanup := SetupTestStore(t)
	defer cleanup()

	// Write config with validators in object format
	configContent := `project_context: Test context
verification:
    command: ./test.sh
validators:
    - path: validators/security.md
      model: opus
      run: after-phase
    - path: validators/naming.md
      model: haiku
`
	if err := os.WriteFile(filepath.Join(store.baseDir, "config.yaml"), []byte(configContent), 0644); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	config, err := store.LoadConfig()
	if err != nil {
		t.Fatalf("unexpected error loading config: %v", err)
	}

	if len(config.Validators) != 2 {
		t.Fatalf("expected 2 validators, got %d", len(config.Validators))
	}

	// Check first validator
	if config.Validators[0].GetPath() != "validators/security.md" {
		t.Errorf("expected first validator path 'validators/security.md', got %q", config.Validators[0].GetPath())
	}
	if config.Validators[0].GetModel() != "opus" {
		t.Errorf("expected first validator model 'opus', got %q", config.Validators[0].GetModel())
	}
	if config.Validators[0].GetRun() != "after-phase" {
		t.Errorf("expected first validator run 'after-phase', got %q", config.Validators[0].GetRun())
	}

	// Check second validator
	if config.Validators[1].GetPath() != "validators/naming.md" {
		t.Errorf("expected second validator path 'validators/naming.md', got %q", config.Validators[1].GetPath())
	}
	if config.Validators[1].GetModel() != "haiku" {
		t.Errorf("expected second validator model 'haiku', got %q", config.Validators[1].GetModel())
	}
	if config.Validators[1].GetRun() != "" {
		t.Errorf("expected second validator run to be empty, got %q", config.Validators[1].GetRun())
	}
}

func TestConfigValidators_MixedFormat(t *testing.T) {
	store, cleanup := SetupTestStore(t)
	defer cleanup()

	// Write config with validators in mixed format (string and object)
	configContent := `project_context: Test context
verification:
    command: ./test.sh
validators:
    - validators/code-standards.md
    - path: validators/security.md
      model: opus
    - validators/naming.md
`
	if err := os.WriteFile(filepath.Join(store.baseDir, "config.yaml"), []byte(configContent), 0644); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	config, err := store.LoadConfig()
	if err != nil {
		t.Fatalf("unexpected error loading config: %v", err)
	}

	if len(config.Validators) != 3 {
		t.Fatalf("expected 3 validators, got %d", len(config.Validators))
	}

	// First validator: string format
	if config.Validators[0].GetPath() != "validators/code-standards.md" {
		t.Errorf("expected first validator path 'validators/code-standards.md', got %q", config.Validators[0].GetPath())
	}
	if config.Validators[0].GetModel() != "" {
		t.Errorf("expected first validator model to be empty, got %q", config.Validators[0].GetModel())
	}

	// Second validator: object format with model
	if config.Validators[1].GetPath() != "validators/security.md" {
		t.Errorf("expected second validator path 'validators/security.md', got %q", config.Validators[1].GetPath())
	}
	if config.Validators[1].GetModel() != "opus" {
		t.Errorf("expected second validator model 'opus', got %q", config.Validators[1].GetModel())
	}

	// Third validator: string format
	if config.Validators[2].GetPath() != "validators/naming.md" {
		t.Errorf("expected third validator path 'validators/naming.md', got %q", config.Validators[2].GetPath())
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
		{ID: "ralph-loop", Description: "Core loop", AcceptanceCriteria: []string{"Loops correctly"}},
		{ID: "old-feature", Description: "To be removed", AcceptanceCriteria: []string{"Old"}},
	}
	if err := store.SaveSpec(parentSpec); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Create change request with all operation types
	timeoutFeature := domain.Feature{ID: "timeout-flag", Description: "Add --timeout flag", AcceptanceCriteria: []string{"Flag accepts minutes", "Flag is optional"}}
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
	if err := store.SaveChangeRequest(cr); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Simulate full merge workflow
	if err := cr.ApplyChanges(parentSpec); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := store.SaveSpec(parentSpec); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := store.DeleteChangeRequest("execution-ralph-add-timeout"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Reload and verify final state
	final, err := store.LoadSpec("execution-ralph")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
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

func TestLoadValidator_FullFormat(t *testing.T) {
	store, cleanup := SetupTestStore(t)
	defer cleanup()

	// Create validators directory and file with full frontmatter
	validatorDir := filepath.Join(store.baseDir, "validators")
	if err := os.MkdirAll(validatorDir, 0755); err != nil {
		t.Fatalf("failed to create validators dir: %v", err)
	}

	// Note: run field is deprecated in validator files but should still load without error
	content := `---
id: code-standards
description: Checks naming conventions; applies to Go source changes
allowed_tools: [Read, Glob, Grep, WebFetch]
---
Review the following changes for code standards:

{{changed_files}}

Check for naming conventions and patterns.
If all standards are met, output: <PASSED>
`
	if err := os.WriteFile(filepath.Join(validatorDir, "code-standards.md"), []byte(content), 0644); err != nil {
		t.Fatalf("failed to create validator file: %v", err)
	}

	validator, err := store.LoadValidator("validators/code-standards.md")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if validator.ID != "code-standards" {
		t.Errorf("expected ID 'code-standards', got %q", validator.ID)
	}
	if validator.Description != "Checks naming conventions; applies to Go source changes" {
		t.Errorf("expected description to be parsed from frontmatter, got %q", validator.Description)
	}
	// Run should default since it's not configured (run in file is deprecated/ignored)
	if validator.GetRun() != domain.RunAfterWorkitem {
		t.Errorf("expected Run default 'after-workitem', got %q", validator.GetRun())
	}
	if len(validator.AllowedTools) != 4 {
		t.Errorf("expected 4 allowed tools, got %d", len(validator.AllowedTools))
	}
	if validator.AllowedTools[0] != "Read" {
		t.Errorf("expected first tool 'Read', got %q", validator.AllowedTools[0])
	}
	if !strings.Contains(validator.Prompt, "Review the following changes") {
		t.Errorf("prompt should contain expected text, got: %s", validator.Prompt)
	}
	if !strings.Contains(validator.Prompt, "{{changed_files}}") {
		t.Errorf("prompt should contain placeholder, got: %s", validator.Prompt)
	}
}

func TestLoadValidator_MinimalFormat(t *testing.T) {
	store, cleanup := SetupTestStore(t)
	defer cleanup()

	// Create validator with only required field (id)
	validatorDir := filepath.Join(store.baseDir, "validators")
	if err := os.MkdirAll(validatorDir, 0755); err != nil {
		t.Fatalf("failed to create validators dir: %v", err)
	}

	content := `---
id: minimal-validator
---
Simple prompt with {{changed_files}} placeholder.
`
	if err := os.WriteFile(filepath.Join(validatorDir, "minimal.md"), []byte(content), 0644); err != nil {
		t.Fatalf("failed to create validator file: %v", err)
	}

	validator, err := store.LoadValidator("validators/minimal.md")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if validator.ID != "minimal-validator" {
		t.Errorf("expected ID 'minimal-validator', got %q", validator.ID)
	}

	// A validator with no description must still load; empty Description is the
	// router's signal to treat it as always applicable rather than skip it.
	if validator.Description != "" {
		t.Errorf("expected empty description when frontmatter omits it, got %q", validator.Description)
	}

	// Verify defaults are applied via getter methods
	if validator.GetRun() != domain.RunAfterWorkitem {
		t.Errorf("expected default run 'after-workitem', got %q", validator.GetRun())
	}
	expectedTools := domain.DefaultAllowedTools()
	gotTools := validator.GetAllowedTools()
	if len(gotTools) != len(expectedTools) {
		t.Errorf("expected %d default tools, got %d", len(expectedTools), len(gotTools))
	}
}

func TestLoadValidator_MissingID(t *testing.T) {
	store, cleanup := SetupTestStore(t)
	defer cleanup()

	validatorDir := filepath.Join(store.baseDir, "validators")
	if err := os.MkdirAll(validatorDir, 0755); err != nil {
		t.Fatalf("failed to create validators dir: %v", err)
	}

	// Missing id field should error
	content := `---
run: after-workitem
---
Prompt without id.
`
	if err := os.WriteFile(filepath.Join(validatorDir, "no-id.md"), []byte(content), 0644); err != nil {
		t.Fatalf("failed to create validator file: %v", err)
	}

	_, err := store.LoadValidator("validators/no-id.md")
	if err == nil {
		t.Fatal("expected error for validator missing id")
	}
	if !strings.Contains(err.Error(), "missing required 'id' field") {
		t.Errorf("error should mention missing id field, got: %v", err)
	}
}

func TestLoadValidator_MissingFrontmatter(t *testing.T) {
	store, cleanup := SetupTestStore(t)
	defer cleanup()

	validatorDir := filepath.Join(store.baseDir, "validators")
	if err := os.MkdirAll(validatorDir, 0755); err != nil {
		t.Fatalf("failed to create validators dir: %v", err)
	}

	// No frontmatter at all
	content := `Just a plain markdown file without frontmatter.`
	if err := os.WriteFile(filepath.Join(validatorDir, "plain.md"), []byte(content), 0644); err != nil {
		t.Fatalf("failed to create validator file: %v", err)
	}

	_, err := store.LoadValidator("validators/plain.md")
	if err == nil {
		t.Fatal("expected error for validator missing frontmatter")
	}
	if !strings.Contains(err.Error(), "missing YAML frontmatter") {
		t.Errorf("error should mention missing frontmatter, got: %v", err)
	}
}

func TestLoadValidator_UnclosedFrontmatter(t *testing.T) {
	store, cleanup := SetupTestStore(t)
	defer cleanup()

	validatorDir := filepath.Join(store.baseDir, "validators")
	if err := os.MkdirAll(validatorDir, 0755); err != nil {
		t.Fatalf("failed to create validators dir: %v", err)
	}

	// Frontmatter without closing ---
	content := `---
id: broken
Some content without closing delimiter.`
	if err := os.WriteFile(filepath.Join(validatorDir, "unclosed.md"), []byte(content), 0644); err != nil {
		t.Fatalf("failed to create validator file: %v", err)
	}

	_, err := store.LoadValidator("validators/unclosed.md")
	if err == nil {
		t.Fatal("expected error for unclosed frontmatter")
	}
	if !strings.Contains(err.Error(), "unclosed YAML frontmatter") {
		t.Errorf("error should mention unclosed frontmatter, got: %v", err)
	}
}

func TestLoadValidator_FileNotFound(t *testing.T) {
	store, cleanup := SetupTestStore(t)
	defer cleanup()

	_, err := store.LoadValidator("validators/nonexistent.md")
	if err == nil {
		t.Fatal("expected error for nonexistent file")
	}
	if !strings.Contains(err.Error(), "failed to read validator file") {
		t.Errorf("error should mention failed to read, got: %v", err)
	}
}

func TestLoadValidator_DeprecatedRunFieldIgnored(t *testing.T) {
	store, cleanup := SetupTestStore(t)
	defer cleanup()

	validatorDir := filepath.Join(store.baseDir, "validators")
	if err := os.MkdirAll(validatorDir, 0755); err != nil {
		t.Fatalf("failed to create validators dir: %v", err)
	}

	// Test that validators with deprecated 'run' field still load without error
	// but the run field is ignored (defaults to after-workitem)
	tests := []struct {
		name string
		run  string
	}{
		{"after-workitem", "after-workitem"},
		{"after-phase", "after-phase"},
		{"on-demand", "on-demand"},
	}

	for _, tc := range tests {
		content := fmt.Sprintf(`---
id: test-%s
run: %s
---
Test prompt.
`, tc.name, tc.run)
		filename := fmt.Sprintf("test-%s.md", tc.name)
		if err := os.WriteFile(filepath.Join(validatorDir, filename), []byte(content), 0644); err != nil {
			t.Fatalf("failed to create validator file: %v", err)
		}

		validator, err := store.LoadValidator("validators/" + filename)
		if err != nil {
			t.Fatalf("unexpected error for %s: %v", tc.name, err)
		}

		// Run field in file is deprecated and ignored - should always default
		if validator.GetRun() != domain.RunAfterWorkitem {
			t.Errorf("for %s: expected default run %q (run in file is deprecated), got %q",
				tc.name, domain.RunAfterWorkitem, validator.GetRun())
		}
	}
}

func TestConfigModels_LoadWithDefault(t *testing.T) {
	store, cleanup := SetupTestStore(t)
	defer cleanup()

	configContent := `project_context: Test context
verification:
    command: ./test.sh
models:
    default: opus
`
	if err := os.WriteFile(filepath.Join(store.baseDir, "config.yaml"), []byte(configContent), 0644); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	config, err := store.LoadConfig()
	if err != nil {
		t.Fatalf("unexpected error loading config: %v", err)
	}

	if config.Models == nil {
		t.Fatal("expected Models to be non-nil")
	}
	if config.Models.Default != "opus" {
		t.Errorf("expected default model 'opus', got %q", config.Models.Default)
	}
}

func TestConfigModels_LoadWithPerCommandOverrides(t *testing.T) {
	store, cleanup := SetupTestStore(t)
	defer cleanup()

	configContent := `project_context: Test context
verification:
    command: ./test.sh
models:
    default: sonnet
    cr: opus
    execute: haiku
    validators: opus
`
	if err := os.WriteFile(filepath.Join(store.baseDir, "config.yaml"), []byte(configContent), 0644); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	config, err := store.LoadConfig()
	if err != nil {
		t.Fatalf("unexpected error loading config: %v", err)
	}

	if config.Models == nil {
		t.Fatal("expected Models to be non-nil")
	}
	if config.Models.Default != "sonnet" {
		t.Errorf("expected default 'sonnet', got %q", config.Models.Default)
	}
	if config.Models.CR != "opus" {
		t.Errorf("expected cr 'opus', got %q", config.Models.CR)
	}
	if config.Models.Execute != "haiku" {
		t.Errorf("expected execute 'haiku', got %q", config.Models.Execute)
	}
	if config.Models.Validators != "opus" {
		t.Errorf("expected validators 'opus', got %q", config.Models.Validators)
	}
}

func TestConfigModels_OmittedWhenMissing(t *testing.T) {
	store, cleanup := SetupTestStore(t)
	defer cleanup()

	configContent := `project_context: Test context
verification:
    command: ./test.sh
`
	if err := os.WriteFile(filepath.Join(store.baseDir, "config.yaml"), []byte(configContent), 0644); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	config, err := store.LoadConfig()
	if err != nil {
		t.Fatalf("unexpected error loading config: %v", err)
	}

	// Models should be nil when not configured
	if config.Models != nil {
		t.Errorf("expected nil Models when not configured, got %+v", config.Models)
	}
}

func TestConfigModels_InvalidModelNameProducesError(t *testing.T) {
	store, cleanup := SetupTestStore(t)
	defer cleanup()

	configContent := `project_context: Test context
verification:
    command: ./test.sh
models:
    default: gpt4
`
	if err := os.WriteFile(filepath.Join(store.baseDir, "config.yaml"), []byte(configContent), 0644); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	_, err := store.LoadConfig()
	if err == nil {
		t.Fatal("expected error for invalid model name, got nil")
	}
	if !strings.Contains(err.Error(), "invalid model configuration") {
		t.Errorf("expected error to mention 'invalid model configuration', got: %v", err)
	}
	if !strings.Contains(err.Error(), "gpt4") {
		t.Errorf("expected error to mention invalid model name 'gpt4', got: %v", err)
	}
}

func TestConfigModels_MultipleInvalidModelNames(t *testing.T) {
	store, cleanup := SetupTestStore(t)
	defer cleanup()

	configContent := `project_context: Test context
verification:
    command: ./test.sh
models:
    default: gpt4
    cr: invalid-model
`
	if err := os.WriteFile(filepath.Join(store.baseDir, "config.yaml"), []byte(configContent), 0644); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	_, err := store.LoadConfig()
	if err == nil {
		t.Fatal("expected error for invalid model names, got nil")
	}
	if !strings.Contains(err.Error(), "gpt4") {
		t.Errorf("expected error to mention 'gpt4', got: %v", err)
	}
	if !strings.Contains(err.Error(), "invalid-model") {
		t.Errorf("expected error to mention 'invalid-model', got: %v", err)
	}
}

func TestConfigModels_AllValidModelNames(t *testing.T) {
	store, cleanup := SetupTestStore(t)
	defer cleanup()

	// Test all valid model names
	configContent := `project_context: Test context
verification:
    command: ./test.sh
models:
    default: sonnet
    cr: opus
    harvest: haiku
    execute: sonnet
    validators: opus
    validator_router: fable
    discover: claude-model-1-20260101
    standards: sonnet
    refactor: opus
    shape: haiku
    validator_create: sonnet
    validator_edit: opus
`
	if err := os.WriteFile(filepath.Join(store.baseDir, "config.yaml"), []byte(configContent), 0644); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	config, err := store.LoadConfig()
	if err != nil {
		t.Fatalf("unexpected error loading config with all valid models: %v", err)
	}

	if config.Models.CR != "opus" {
		t.Errorf("expected cr 'opus', got %q", config.Models.CR)
	}
	if config.Models.ValidatorCreate != "sonnet" {
		t.Errorf("expected validator_create 'sonnet', got %q", config.Models.ValidatorCreate)
	}
	if config.Models.ValidatorEdit != "opus" {
		t.Errorf("expected validator_edit 'opus', got %q", config.Models.ValidatorEdit)
	}
}

// setupPathsTestProject creates a project root with a .utopia directory and
// optional config.yaml content, returning the project root and utopia dir.
func setupPathsTestProject(t *testing.T, configContent string) (projectRoot, utopiaDir string) {
	t.Helper()
	projectRoot = t.TempDir()
	utopiaDir = filepath.Join(projectRoot, ".utopia")
	if err := os.MkdirAll(utopiaDir, 0755); err != nil {
		t.Fatalf("failed to create .utopia dir: %v", err)
	}
	if configContent != "" {
		if err := os.WriteFile(filepath.Join(utopiaDir, "config.yaml"), []byte(configContent), 0644); err != nil {
			t.Fatalf("failed to write config: %v", err)
		}
	}
	return projectRoot, utopiaDir
}

func TestConfigPaths_DefaultsWhenOmitted(t *testing.T) {
	_, utopiaDir := setupPathsTestProject(t, `verification:
    command: ./test.sh
`)

	store := NewYAMLStore(utopiaDir)

	expected := map[string]string{
		"specs":    store.SpecsDir(),
		"adrs":     store.ADRsDir(),
		"concepts": store.ConceptsDir(),
		"domain":   store.DomainDir(),
	}
	for name, got := range expected {
		want := filepath.Join(utopiaDir, name)
		if got != want {
			t.Errorf("expected %s dir %q, got %q", name, want, got)
		}
	}
}

func TestConfigPaths_DefaultsWhenNoConfigFile(t *testing.T) {
	_, utopiaDir := setupPathsTestProject(t, "")

	store := NewYAMLStore(utopiaDir)

	if store.SpecsDir() != filepath.Join(utopiaDir, "specs") {
		t.Errorf("expected default specs dir, got %q", store.SpecsDir())
	}
}

func TestConfigPaths_RelativeResolvedFromProjectRoot(t *testing.T) {
	projectRoot, utopiaDir := setupPathsTestProject(t, `verification:
    command: ./test.sh
paths:
    specs: docs/specs
    adrs: docs/adrs
    concepts: docs/concepts
    domain: docs/domain
`)

	store := NewYAMLStore(utopiaDir)

	expected := map[string]string{
		"specs":    store.SpecsDir(),
		"adrs":     store.ADRsDir(),
		"concepts": store.ConceptsDir(),
		"domain":   store.DomainDir(),
	}
	for name, got := range expected {
		want := filepath.Join(projectRoot, "docs", name)
		if got != want {
			t.Errorf("expected %s dir %q, got %q", name, want, got)
		}
	}
}

func TestConfigPaths_AbsoluteUsedAsIs(t *testing.T) {
	absDir := filepath.Join(t.TempDir(), "shared-specs")
	_, utopiaDir := setupPathsTestProject(t, `verification:
    command: ./test.sh
paths:
    specs: `+absDir+`
`)

	store := NewYAMLStore(utopiaDir)

	if store.SpecsDir() != absDir {
		t.Errorf("expected specs dir %q, got %q", absDir, store.SpecsDir())
	}
}

// Harvest sources are the tool's own artifacts, so their directories stay under
// .utopia even when the documentation directories are relocated. They are still
// resolved through the store rather than assumed relative to a working
// directory: harvest hands these paths to a session that marks sources
// processed, and that session may be running from anywhere.
func TestSourceDirs_AlwaysUnderBaseDir(t *testing.T) {
	projectRoot, utopiaDir := setupPathsTestProject(t, `paths:
    specs: docs/specs
    adrs: docs/adrs
`)

	store := NewYAMLStore(utopiaDir)

	if got, want := store.ConversationsDir(), filepath.Join(utopiaDir, "conversations"); got != want {
		t.Errorf("ConversationsDir() = %q, want %q", got, want)
	}
	if got, want := store.RunsDir(), filepath.Join(utopiaDir, "runs"); got != want {
		t.Errorf("RunsDir() = %q, want %q", got, want)
	}
	if !filepath.IsAbs(store.RunsDir()) {
		t.Errorf("RunsDir() must be absolute, got %q", store.RunsDir())
	}
	// A relocated adrs dir must not drag the source dirs out of .utopia.
	if store.ADRsDir() != filepath.Join(projectRoot, "docs", "adrs") {
		t.Fatalf("test setup did not relocate adrs, got %q", store.ADRsDir())
	}
}

func TestConfigPaths_PartialSectionKeepsDefaults(t *testing.T) {
	projectRoot, utopiaDir := setupPathsTestProject(t, `verification:
    command: ./test.sh
paths:
    specs: docs/specs
`)

	store := NewYAMLStore(utopiaDir)

	if store.SpecsDir() != filepath.Join(projectRoot, "docs", "specs") {
		t.Errorf("expected configured specs dir, got %q", store.SpecsDir())
	}
	if store.ADRsDir() != filepath.Join(utopiaDir, "adrs") {
		t.Errorf("expected default adrs dir, got %q", store.ADRsDir())
	}
	if store.ConceptsDir() != filepath.Join(utopiaDir, "concepts") {
		t.Errorf("expected default concepts dir, got %q", store.ConceptsDir())
	}
	if store.DomainDir() != filepath.Join(utopiaDir, "domain") {
		t.Errorf("expected default domain dir, got %q", store.DomainDir())
	}
}

func TestConfigPaths_SpecRoundTripInConfiguredDir(t *testing.T) {
	projectRoot, utopiaDir := setupPathsTestProject(t, `verification:
    command: ./test.sh
paths:
    specs: docs/specs
`)

	store := NewYAMLStore(utopiaDir)

	spec := &domain.Spec{ID: "test-spec", Title: "Test Spec"}
	if err := store.SaveSpec(spec); err != nil {
		t.Fatalf("failed to save spec: %v", err)
	}

	// Directory is created on first write, and the file lands there
	specPath := filepath.Join(projectRoot, "docs", "specs", "test-spec.yaml")
	if _, err := os.Stat(specPath); err != nil {
		t.Fatalf("expected spec file at %s: %v", specPath, err)
	}

	loaded, err := store.LoadSpec("test-spec")
	if err != nil {
		t.Fatalf("failed to load spec: %v", err)
	}
	if loaded.Title != "Test Spec" {
		t.Errorf("expected title 'Test Spec', got %q", loaded.Title)
	}

	specs, err := store.ListSpecs()
	if err != nil {
		t.Fatalf("failed to list specs: %v", err)
	}
	if len(specs) != 1 {
		t.Fatalf("expected 1 spec, got %d", len(specs))
	}

	if err := store.DeleteSpec("test-spec"); err != nil {
		t.Fatalf("failed to delete spec: %v", err)
	}
	if _, err := os.Stat(specPath); !os.IsNotExist(err) {
		t.Errorf("expected spec file to be deleted")
	}
}

func TestConfigPaths_DomainDocRoundTripInConfiguredDir(t *testing.T) {
	projectRoot, utopiaDir := setupPathsTestProject(t, `verification:
    command: ./test.sh
paths:
    domain: docs/domain
`)

	store := NewYAMLStore(utopiaDir)

	doc := &domain.DomainDoc{ID: "billing"}
	if err := store.SaveDomainDoc(doc); err != nil {
		t.Fatalf("failed to save domain doc: %v", err)
	}

	docPath := filepath.Join(projectRoot, "docs", "domain", "billing.yaml")
	if _, err := os.Stat(docPath); err != nil {
		t.Fatalf("expected domain doc at %s: %v", docPath, err)
	}

	loaded, err := store.LoadDomainDoc("billing")
	if err != nil {
		t.Fatalf("failed to load domain doc: %v", err)
	}
	if loaded.ID != "billing" {
		t.Errorf("expected doc ID 'billing', got %q", loaded.ID)
	}
}

func TestConfigPaths_LoadConfigParsesPathsSection(t *testing.T) {
	_, utopiaDir := setupPathsTestProject(t, `verification:
    command: ./test.sh
paths:
    specs: docs/specs
    adrs: docs/adrs
`)

	store := NewYAMLStore(utopiaDir)

	config, err := store.LoadConfig()
	if err != nil {
		t.Fatalf("unexpected error loading config: %v", err)
	}
	if config.Paths == nil {
		t.Fatal("expected Paths to be non-nil")
	}
	if config.Paths.Specs != "docs/specs" {
		t.Errorf("expected paths.specs 'docs/specs', got %q", config.Paths.Specs)
	}
	if config.Paths.ADRs != "docs/adrs" {
		t.Errorf("expected paths.adrs 'docs/adrs', got %q", config.Paths.ADRs)
	}
}

// Tests for the auth section

func TestConfigAuth_LoadWithAPIKeyMode(t *testing.T) {
	store, cleanup := SetupTestStore(t)
	defer cleanup()

	configContent := `verification:
    command: ./test.sh
auth:
    mode: api-key
`
	if err := os.WriteFile(filepath.Join(store.baseDir, "config.yaml"), []byte(configContent), 0644); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	config, err := store.LoadConfig()
	if err != nil {
		t.Fatalf("unexpected error loading config: %v", err)
	}
	if config.Auth.GetMode() != domain.AuthModeAPIKey {
		t.Errorf("expected auth mode %q, got %q", domain.AuthModeAPIKey, config.Auth.GetMode())
	}
}

func TestConfigAuth_LoadWithSubscriptionMode(t *testing.T) {
	store, cleanup := SetupTestStore(t)
	defer cleanup()

	configContent := `verification:
    command: ./test.sh
auth:
    mode: subscription
`
	if err := os.WriteFile(filepath.Join(store.baseDir, "config.yaml"), []byte(configContent), 0644); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	config, err := store.LoadConfig()
	if err != nil {
		t.Fatalf("unexpected error loading config: %v", err)
	}
	if config.Auth.GetMode() != domain.AuthModeSubscription {
		t.Errorf("expected auth mode %q, got %q", domain.AuthModeSubscription, config.Auth.GetMode())
	}
}

func TestConfigAuth_OmittedSectionLoadsCleanly(t *testing.T) {
	store, cleanup := SetupTestStore(t)
	defer cleanup()

	configContent := `project_context: Test context
verification:
    command: ./test.sh
`
	if err := os.WriteFile(filepath.Join(store.baseDir, "config.yaml"), []byte(configContent), 0644); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	config, err := store.LoadConfig()
	if err != nil {
		t.Fatalf("unexpected error loading config without auth section: %v", err)
	}
	if config.Auth != nil {
		t.Errorf("expected nil Auth when not configured, got %+v", config.Auth)
	}
	// The omitted section must resolve to "no selection", which is what keeps
	// the subprocess environment inherited unchanged.
	if config.Auth.GetMode() != "" {
		t.Errorf("expected empty auth mode when not configured, got %q", config.Auth.GetMode())
	}
}

func TestConfigAuth_InvalidModeProducesError(t *testing.T) {
	store, cleanup := SetupTestStore(t)
	defer cleanup()

	configContent := `verification:
    command: ./test.sh
auth:
    mode: oauth
`
	if err := os.WriteFile(filepath.Join(store.baseDir, "config.yaml"), []byte(configContent), 0644); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	_, err := store.LoadConfig()
	if err == nil {
		t.Fatal("expected error for invalid auth mode, got nil")
	}
	if !strings.Contains(err.Error(), "oauth") {
		t.Errorf("expected error to name the invalid value 'oauth', got: %v", err)
	}
	if !strings.Contains(err.Error(), "api-key") || !strings.Contains(err.Error(), "subscription") {
		t.Errorf("expected error to list the valid options, got: %v", err)
	}
	if !errors.Is(err, &domain.InvalidAuthModeError{}) {
		t.Errorf("expected error to match *domain.InvalidAuthModeError, got %T", err)
	}
}

// Tests for standards index loading

func TestLoadStandardsIndex_MissingDir(t *testing.T) {
	store, cleanup := SetupTestStore(t)
	defer cleanup()

	docs := store.LoadStandardsIndex()
	if len(docs) != 0 {
		t.Errorf("expected empty index for missing standards dir, got %d docs", len(docs))
	}
}

func TestLoadStandardsIndex_ParsesFrontmatter(t *testing.T) {
	dir := t.TempDir()
	standardsDir := filepath.Join(dir, "standards")
	if err := os.MkdirAll(standardsDir, 0755); err != nil {
		t.Fatalf("failed to create standards dir: %v", err)
	}

	doc := `---
id: cli-organization
title: "CLI Package Organization"
description: "How to structure cobra commands"
tags:
  - go
  - cli
---

Full body content that must never appear in the index.
`
	if err := os.WriteFile(filepath.Join(standardsDir, "cli-organization.md"), []byte(doc), 0644); err != nil {
		t.Fatalf("failed to write standards doc: %v", err)
	}

	store := NewYAMLStore(dir)
	docs := store.LoadStandardsIndex()

	if len(docs) != 1 {
		t.Fatalf("expected 1 doc in index, got %d", len(docs))
	}
	got := docs[0]
	if got.ID != "cli-organization" {
		t.Errorf("expected id 'cli-organization', got %q", got.ID)
	}
	if got.Title != "CLI Package Organization" {
		t.Errorf("expected title 'CLI Package Organization', got %q", got.Title)
	}
	if got.Description != "How to structure cobra commands" {
		t.Errorf("expected description 'How to structure cobra commands', got %q", got.Description)
	}
	if len(got.Tags) != 2 || got.Tags[0] != "go" || got.Tags[1] != "cli" {
		t.Errorf("expected tags [go cli], got %v", got.Tags)
	}
	if got.Path != ".utopia/standards/cli-organization.md" {
		t.Errorf("expected path '.utopia/standards/cli-organization.md', got %q", got.Path)
	}
}

func TestLoadStandardsIndex_SkipsUnparseableDocs(t *testing.T) {
	dir := t.TempDir()
	standardsDir := filepath.Join(dir, "standards")
	if err := os.MkdirAll(standardsDir, 0755); err != nil {
		t.Fatalf("failed to create standards dir: %v", err)
	}

	files := map[string]string{
		"valid.md":          "---\nid: valid\ndescription: A valid doc\n---\nBody.\n",
		"no-frontmatter.md": "Just a markdown file with no frontmatter.\n",
		"unclosed.md":       "---\nid: unclosed\ndescription: never closed\n",
		"bad-yaml.md":       "---\nid: [unbalanced\n---\nBody.\n",
		"no-id.md":          "---\ndescription: missing the id field\n---\nBody.\n",
		"not-markdown.txt":  "---\nid: ignored\n---\nBody.\n",
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(standardsDir, name), []byte(content), 0644); err != nil {
			t.Fatalf("failed to write %s: %v", name, err)
		}
	}

	store := NewYAMLStore(dir)
	docs := store.LoadStandardsIndex()

	if len(docs) != 1 {
		t.Fatalf("expected only the valid doc in index, got %d docs", len(docs))
	}
	if docs[0].ID != "valid" {
		t.Errorf("expected id 'valid', got %q", docs[0].ID)
	}
}

// writeRawCR writes a CR file at an arbitrary filename whose internal id may
// differ from the basename - which SaveChangeRequest cannot do, since it always
// derives the filename from cr.ID. This lets the resolution tests exercise the
// numeric-prefix decoupling (06_ai-chat.yaml with internal id ai-chat).
func writeRawCR(t *testing.T, store *YAMLStore, filename, id string) {
	t.Helper()
	content := fmt.Sprintf("id: %s\ntype: refactor\ntitle: %s\nstatus: approved\n", id, id)
	path := filepath.Join(store.baseDir, "change-requests", filename)
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write %s: %v", filename, err)
	}
}

func TestNumericFilenamePrefix(t *testing.T) {
	tests := []struct {
		base     string
		wantN    int
		wantRest string
		wantHave bool
	}{
		{"1_first", 1, "first", true},
		{"2_second", 2, "second", true},
		{"10_tenth", 10, "tenth", true},
		{"02_padded", 2, "padded", true}, // zero-padding collapses to the same value
		{"000_zero", 0, "zero", true},    // an explicit zero prefix is still a prefix
		{"01_reusable-core", 1, "reusable-core", true},
		{"cleanup-legacy", 0, "cleanup-legacy", false},
		{"2024-migration", 0, "2024-migration", false}, // digits not followed by "_" is not a prefix
		{"2a_mixed", 0, "2a_mixed", false},             // non-digit inside the run
		{"_leading", 0, "_leading", false},             // empty digit run
		{"nodigits", 0, "nodigits", false},
		{"+5_signed", 0, "+5_signed", false}, // a sign is not a digit run
		{"-5_negative", 0, "-5_negative", false},
		// A digit run too long to be a sequence number is left alone rather than
		// silently ordering as garbage.
		{"99999999999999999999_overflow", 0, "99999999999999999999_overflow", false},
	}
	for _, tt := range tests {
		gotN, gotRest, gotHave := NumericFilenamePrefix(tt.base)
		if gotN != tt.wantN || gotRest != tt.wantRest || gotHave != tt.wantHave {
			t.Errorf("NumericFilenamePrefix(%q) = (%d, %q, %t), want (%d, %q, %t)",
				tt.base, gotN, gotRest, gotHave, tt.wantN, tt.wantRest, tt.wantHave)
		}
	}
}

func TestStripNumericPrefix(t *testing.T) {
	tests := []struct{ base, want string }{
		{"01_reusable-core", "reusable-core"},
		{"06_ai-chat", "ai-chat"},
		{"2_second", "second"},
		{"000_zero", "zero"},                 // an explicit zero prefix is still a prefix
		{"reusable-core", "reusable-core"},   // no underscore, no prefix
		{"2024-migration", "2024-migration"}, // digits not followed by "_"
		{"2a_mixed", "2a_mixed"},             // non-digit inside the run
		{"_leading", "_leading"},             // empty digit run
	}
	for _, tt := range tests {
		if got := stripNumericPrefix(tt.base); got != tt.want {
			t.Errorf("stripNumericPrefix(%q) = %q, want %q", tt.base, got, tt.want)
		}
	}
}

// TestListChangeRequestFiles_SurfacesBasename guards the seam where the filename
// used to be lost: the listing computed each basename in order to load the file,
// then returned only the parsed documents. Nothing in a parsed CR records which
// file it came from, so callers that order by filename had no key left but cr.ID
// - which by convention carries no prefix.
func TestListChangeRequestFiles_SurfacesBasename(t *testing.T) {
	store, cleanup := SetupTestStore(t)
	defer cleanup()
	writeRawCR(t, store, "01_reusable-core.yaml", "reusable-core")
	writeRawCR(t, store, "plain.yaml", "plain")

	files, err := store.ListChangeRequestFiles()
	if err != nil {
		t.Fatalf("ListChangeRequestFiles failed: %v", err)
	}
	if len(files) != 2 {
		t.Fatalf("got %d CR file(s), want 2", len(files))
	}

	// os.ReadDir sorts by filename, so "01_reusable-core" comes first.
	if files[0].Basename != "01_reusable-core" {
		t.Errorf("basename = %q, want %q", files[0].Basename, "01_reusable-core")
	}
	if files[0].CR.ID != "reusable-core" {
		t.Errorf("id = %q, want %q - the prefix belongs to the filename only", files[0].CR.ID, "reusable-core")
	}
	if files[1].Basename != "plain" || files[1].CR.ID != "plain" {
		t.Errorf("second entry = (%q, %q), want (%q, %q)",
			files[1].Basename, files[1].CR.ID, "plain", "plain")
	}
}

// TestListChangeRequests_MatchesFileListing keeps the id-only convenience wrapper
// honest: it must return exactly the CRs from ListChangeRequestFiles, in order.
func TestListChangeRequests_MatchesFileListing(t *testing.T) {
	store, cleanup := SetupTestStore(t)
	defer cleanup()
	writeRawCR(t, store, "01_reusable-core.yaml", "reusable-core")
	writeRawCR(t, store, "plain.yaml", "plain")

	crs, err := store.ListChangeRequests()
	if err != nil {
		t.Fatalf("ListChangeRequests failed: %v", err)
	}
	want := []string{"reusable-core", "plain"}
	if len(crs) != len(want) {
		t.Fatalf("got %d CR(s), want %d", len(crs), len(want))
	}
	for i, cr := range crs {
		if cr.ID != want[i] {
			t.Errorf("position %d = %q, want %q", i, cr.ID, want[i])
		}
	}
}

func TestResolveChangeRequest_ExactFilename(t *testing.T) {
	store, cleanup := SetupTestStore(t)
	defer cleanup()
	writeRawCR(t, store, "plain.yaml", "plain")

	cr, err := store.ResolveChangeRequest("plain")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cr.ID != "plain" {
		t.Errorf("cr.ID = %q, want %q", cr.ID, "plain")
	}
}

func TestResolveChangeRequest_PrefixStrippedName(t *testing.T) {
	store, cleanup := SetupTestStore(t)
	defer cleanup()
	// The file carries a numeric prefix; its internal id does not.
	writeRawCR(t, store, "01_reusable-core.yaml", "reusable-core")

	cr, err := store.ResolveChangeRequest("reusable-core")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cr.ID != "reusable-core" {
		t.Errorf("cr.ID = %q, want %q", cr.ID, "reusable-core")
	}
}

func TestResolveChangeRequest_RunnableByBothNames(t *testing.T) {
	store, cleanup := SetupTestStore(t)
	defer cleanup()
	writeRawCR(t, store, "06_ai-chat.yaml", "ai-chat")

	// Both the full filename and the prefix-stripped name resolve the same CR.
	for _, name := range []string{"06_ai-chat", "ai-chat"} {
		cr, err := store.ResolveChangeRequest(name)
		if err != nil {
			t.Fatalf("ResolveChangeRequest(%q): unexpected error: %v", name, err)
		}
		if cr.ID != "ai-chat" {
			t.Errorf("ResolveChangeRequest(%q).ID = %q, want %q", name, cr.ID, "ai-chat")
		}
	}
}

func TestResolveChangeRequest_ExactMatchWinsOverPrefix(t *testing.T) {
	store, cleanup := SetupTestStore(t)
	defer cleanup()
	// A bare file and a prefixed file both strip to "ai-chat"; the exact
	// filename match must win without reporting ambiguity.
	writeRawCR(t, store, "ai-chat.yaml", "ai-chat-exact")
	writeRawCR(t, store, "06_ai-chat.yaml", "ai-chat-prefixed")

	cr, err := store.ResolveChangeRequest("ai-chat")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cr.ID != "ai-chat-exact" {
		t.Errorf("cr.ID = %q, want the exact-match file's id %q", cr.ID, "ai-chat-exact")
	}
}

func TestResolveChangeRequest_Ambiguous(t *testing.T) {
	store, cleanup := SetupTestStore(t)
	defer cleanup()
	// Two prefixed files strip to the same name and no exact match exists.
	writeRawCR(t, store, "06_ai-chat.yaml", "ai-chat")
	writeRawCR(t, store, "07_ai-chat.yaml", "ai-chat")

	_, err := store.ResolveChangeRequest("ai-chat")
	if err == nil {
		t.Fatal("expected ambiguity error, got nil")
	}
	var nfe *domain.NotFoundError
	if errors.As(err, &nfe) {
		t.Fatalf("ambiguous name should not surface as NotFoundError, got: %v", err)
	}
	for _, want := range []string{"06_ai-chat.yaml", "07_ai-chat.yaml"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("ambiguity error should list candidate %q, got: %v", want, err)
		}
	}
}

func TestResolveChangeRequest_NotFound(t *testing.T) {
	store, cleanup := SetupTestStore(t)
	defer cleanup()
	writeRawCR(t, store, "01_reusable-core.yaml", "reusable-core")

	_, err := store.ResolveChangeRequest("does-not-exist")
	if err == nil {
		t.Fatal("expected not-found error, got nil")
	}
	var nfe *domain.NotFoundError
	if !errors.As(err, &nfe) {
		t.Errorf("expected *domain.NotFoundError, got %T: %v", err, err)
	}
}

func TestChangeRequestPath_Canonical(t *testing.T) {
	store, cleanup := SetupTestStore(t)
	defer cleanup()
	writeRawCR(t, store, "plain.yaml", "plain")

	path, err := store.ChangeRequestPath("plain")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := filepath.Base(path); got != "plain.yaml" {
		t.Errorf("resolved basename = %q, want %q", got, "plain.yaml")
	}
}

func TestChangeRequestPath_PrefixStripped(t *testing.T) {
	store, cleanup := SetupTestStore(t)
	defer cleanup()
	// The file carries a numeric ordering prefix; its internal id does not.
	writeRawCR(t, store, "01_reusable-core.yaml", "reusable-core")

	path, err := store.ChangeRequestPath("reusable-core")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := filepath.Base(path); got != "01_reusable-core.yaml" {
		t.Errorf("resolved basename = %q, want the real on-disk file %q", got, "01_reusable-core.yaml")
	}
}

func TestChangeRequestPath_CanonicalWinsOverPrefix(t *testing.T) {
	store, cleanup := SetupTestStore(t)
	defer cleanup()
	// A bare file and a prefixed file both strip to "ai-chat"; the canonical
	// filename must win without reporting ambiguity.
	writeRawCR(t, store, "ai-chat.yaml", "ai-chat")
	writeRawCR(t, store, "06_ai-chat.yaml", "ai-chat")

	path, err := store.ChangeRequestPath("ai-chat")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := filepath.Base(path); got != "ai-chat.yaml" {
		t.Errorf("resolved basename = %q, want the canonical file %q", got, "ai-chat.yaml")
	}
}

func TestChangeRequestPath_Ambiguous(t *testing.T) {
	store, cleanup := SetupTestStore(t)
	defer cleanup()
	// Two prefixed files strip to the same id and no canonical file exists.
	writeRawCR(t, store, "06_ai-chat.yaml", "ai-chat")
	writeRawCR(t, store, "07_ai-chat.yaml", "ai-chat")

	_, err := store.ChangeRequestPath("ai-chat")
	if err == nil {
		t.Fatal("expected ambiguity error, got nil")
	}
	var nfe *domain.NotFoundError
	if errors.As(err, &nfe) {
		t.Fatalf("ambiguous id should not surface as NotFoundError, got: %v", err)
	}
	for _, want := range []string{"06_ai-chat.yaml", "07_ai-chat.yaml"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("ambiguity error should list candidate %q, got: %v", want, err)
		}
	}
}

func TestChangeRequestPath_NotFound(t *testing.T) {
	store, cleanup := SetupTestStore(t)
	defer cleanup()
	writeRawCR(t, store, "01_reusable-core.yaml", "reusable-core")

	_, err := store.ChangeRequestPath("does-not-exist")
	if err == nil {
		t.Fatal("expected not-found error, got nil")
	}
	var nfe *domain.NotFoundError
	if !errors.As(err, &nfe) {
		t.Errorf("expected *domain.NotFoundError, got %T: %v", err, err)
	}
}

func TestDeleteChangeRequest_NumericPrefix(t *testing.T) {
	store, cleanup := SetupTestStore(t)
	defer cleanup()
	// A CR saved under a prefixed filename must be deleted by its real path,
	// not a reconstructed reusable-core.yaml that does not exist.
	writeRawCR(t, store, "01_reusable-core.yaml", "reusable-core")

	if err := store.DeleteChangeRequest("reusable-core"); err != nil {
		t.Fatalf("unexpected error deleting prefixed CR: %v", err)
	}

	if _, err := os.Stat(filepath.Join(store.baseDir, "change-requests", "01_reusable-core.yaml")); !os.IsNotExist(err) {
		t.Errorf("prefixed CR file should be gone, stat err = %v", err)
	}
}

// crFilenames returns the basenames of every file in the store's
// change-requests directory (os.ReadDir already sorts them), so tests can
// assert that a save updated an existing file rather than adding one.
func crFilenames(t *testing.T, store *YAMLStore) []string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(store.baseDir, "change-requests"))
	if err != nil {
		t.Fatalf("failed to read change-requests dir: %v", err)
	}
	names := []string{}
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	return names
}

// Saving a CR that already exists under a numeric-prefixed filename must update
// that file, not fork an un-prefixed shadow with the same id. The shadow would
// then win resolution (ChangeRequestPath prefers a canonical filename), so every
// later read and the post-merge delete would act on it instead of the real file.
func TestSaveChangeRequest_UpdatesPrefixedFileInPlace(t *testing.T) {
	store, cleanup := SetupTestStore(t)
	defer cleanup()
	writeRawCR(t, store, "01_reusable-core.yaml", "reusable-core")

	cr, err := store.ResolveChangeRequest("reusable-core")
	if err != nil {
		t.Fatalf("failed to resolve CR: %v", err)
	}
	cr.Status = domain.ChangeRequestComplete
	if err := store.SaveChangeRequest(cr); err != nil {
		t.Fatalf("unexpected error saving CR: %v", err)
	}

	if got, want := crFilenames(t, store), []string{"01_reusable-core.yaml"}; !slices.Equal(got, want) {
		t.Errorf("change-requests contents = %v, want only the prefixed file %v", got, want)
	}

	// The prefixed file itself carries the new status.
	reloaded, err := store.LoadChangeRequest("01_reusable-core")
	if err != nil {
		t.Fatalf("failed to reload prefixed CR: %v", err)
	}
	if reloaded.Status != domain.ChangeRequestComplete {
		t.Errorf("prefixed CR status = %q, want %q", reloaded.Status, domain.ChangeRequestComplete)
	}
}

// A CR with no file on disk yet is genuinely new, so the canonical filename is
// correct - ChangeRequestPath's NotFoundError must not propagate as a failure.
func TestSaveChangeRequest_NewCRWritesCanonical(t *testing.T) {
	store, cleanup := SetupTestStore(t)
	defer cleanup()

	cr := &domain.ChangeRequest{
		ID:     "brand-new",
		Type:   domain.CRTypeRefactor,
		Title:  "Brand New",
		Status: domain.ChangeRequestDraft,
	}
	if err := store.SaveChangeRequest(cr); err != nil {
		t.Fatalf("unexpected error saving new CR: %v", err)
	}

	if got, want := crFilenames(t, store), []string{"brand-new.yaml"}; !slices.Equal(got, want) {
		t.Errorf("change-requests contents = %v, want %v", got, want)
	}
}

// Save reports an ambiguous id rather than silently picking one of the
// candidates, matching how ChangeRequestPath already reports it on read.
func TestSaveChangeRequest_Ambiguous(t *testing.T) {
	store, cleanup := SetupTestStore(t)
	defer cleanup()
	// Two prefixed files strip to the same id and no canonical file exists.
	writeRawCR(t, store, "06_ai-chat.yaml", "ai-chat")
	writeRawCR(t, store, "07_ai-chat.yaml", "ai-chat")

	cr, err := store.LoadChangeRequest("06_ai-chat")
	if err != nil {
		t.Fatalf("failed to load CR: %v", err)
	}
	cr.Status = domain.ChangeRequestInProgress

	err = store.SaveChangeRequest(cr)
	if err == nil {
		t.Fatal("expected ambiguity error, got nil")
	}
	var nfe *domain.NotFoundError
	if errors.As(err, &nfe) {
		t.Fatalf("ambiguous id should not surface as NotFoundError, got: %v", err)
	}
	for _, want := range []string{"06_ai-chat.yaml", "07_ai-chat.yaml"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("ambiguity error should list candidate %q, got: %v", want, err)
		}
	}
	// Nothing new was written.
	if got, want := crFilenames(t, store), []string{"06_ai-chat.yaml", "07_ai-chat.yaml"}; !slices.Equal(got, want) {
		t.Errorf("change-requests contents = %v, want %v", got, want)
	}
}

// Mirrors the chunk-time status transition (chunkCR in internal/cli/execute.go
// sets in-progress and saves before chunking). On a prefix-named CR this must
// update the prefixed file, not leak a shadow that later re-appears as an
// executable orphan.
func TestSaveChangeRequest_ChunkTimeStatusUpdateNoShadow(t *testing.T) {
	store, cleanup := SetupTestStore(t)
	defer cleanup()
	writeRawCR(t, store, "01_reusable-core.yaml", "reusable-core")

	cr, err := store.ResolveChangeRequest("reusable-core")
	if err != nil {
		t.Fatalf("failed to resolve CR: %v", err)
	}
	cr.Status = domain.ChangeRequestInProgress
	if err := store.SaveChangeRequest(cr); err != nil {
		t.Fatalf("unexpected error saving CR: %v", err)
	}

	if got, want := crFilenames(t, store), []string{"01_reusable-core.yaml"}; !slices.Equal(got, want) {
		t.Errorf("change-requests contents = %v, want no shadow copy alongside %v", got, want)
	}
	reloaded, err := store.LoadChangeRequest("01_reusable-core")
	if err != nil {
		t.Fatalf("failed to reload prefixed CR: %v", err)
	}
	if reloaded.Status != domain.ChangeRequestInProgress {
		t.Errorf("prefixed CR status = %q, want %q", reloaded.Status, domain.ChangeRequestInProgress)
	}
}

func TestSaveExecutionRun_GroupsRunsByChangeRequest(t *testing.T) {
	store, cleanup := SetupTestStore(t)
	defer cleanup()

	runs := []*domain.ExecutionRun{
		{WorkItemID: "item-a", CRID: "cr-1", SpecRef: "spec.a", Iterations: 1, Outcome: domain.RunCompleted, Transcript: "a"},
		{WorkItemID: "item-b", CRID: "cr-1", SpecRef: "spec.b", Iterations: 2, Outcome: domain.RunFailed, Transcript: "b"},
		{WorkItemID: "item-c", CRID: "cr-2", SpecRef: "spec.c", Iterations: 1, Outcome: domain.RunCompleted, Transcript: "c"},
	}
	for _, run := range runs {
		if err := store.SaveExecutionRun(run); err != nil {
			t.Fatalf("SaveExecutionRun(%s): %v", run.WorkItemID, err)
		}
	}

	// A CR's whole execution history is one directory, so a harvest can walk
	// runs/<cr-id>/ without consulting the work items.
	for _, run := range runs {
		path := filepath.Join("runs", run.CRID, run.WorkItemID+".yaml")
		loaded, err := Load[domain.ExecutionRun](store, path)
		if err != nil {
			t.Fatalf("expected run at %s: %v", path, err)
		}
		if loaded.Transcript != run.Transcript || loaded.Outcome != run.Outcome {
			t.Errorf("run %s round-tripped as %+v", run.WorkItemID, loaded)
		}
	}
}

func TestSaveExecutionRun_ReExecutionOverwritesTheSameFile(t *testing.T) {
	store, cleanup := SetupTestStore(t)
	defer cleanup()

	first := &domain.ExecutionRun{WorkItemID: "item-a", CRID: "cr-1", Iterations: 5, Outcome: domain.RunFailed, Transcript: "gave up"}
	if err := store.SaveExecutionRun(first); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	second := &domain.ExecutionRun{WorkItemID: "item-a", CRID: "cr-1", Iterations: 2, Outcome: domain.RunCompleted, Transcript: "done"}
	if err := store.SaveExecutionRun(second); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	entries, err := os.ReadDir(filepath.Join(store.baseDir, "runs", "cr-1"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) != 1 {
		t.Errorf("a re-executed work item must keep one run file, got %d", len(entries))
	}

	loaded, err := Load[domain.ExecutionRun](store, "runs/cr-1/item-a.yaml")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if loaded.Outcome != domain.RunCompleted || loaded.Transcript != "done" {
		t.Errorf("latest run should win, got %+v", loaded)
	}
}

func TestListUnprocessedExecutionRuns(t *testing.T) {
	store, cleanup := SetupTestStore(t)
	defer cleanup()

	saved := []*domain.ExecutionRun{
		{WorkItemID: "item-new", CRID: "cr-1", Status: domain.ConversationUnprocessed},
		{WorkItemID: "item-harvested", CRID: "cr-1", Status: domain.ConversationProcessed},
		// Written before harvest tracked run status: no status field at all,
		// and never reviewed, so it is still owed a harvest.
		{WorkItemID: "item-legacy", CRID: "cr-2"},
	}
	for _, run := range saved {
		if err := store.SaveExecutionRun(run); err != nil {
			t.Fatalf("SaveExecutionRun(%s): %v", run.WorkItemID, err)
		}
	}

	runs, err := store.ListUnprocessedExecutionRuns()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := map[string]bool{}
	for _, run := range runs {
		got[run.WorkItemID] = true
	}
	if len(runs) != 2 || !got["item-new"] || !got["item-legacy"] {
		t.Errorf("unprocessed runs = %v, want item-new and item-legacy", got)
	}
}

func TestListExecutionRuns_ReturnsRunsAcrossChangeRequests(t *testing.T) {
	store, cleanup := SetupTestStore(t)
	defer cleanup()

	// No runs directory yet: a project that predates run persistence has no
	// runs to harvest, which is not an error.
	runs, err := store.ListExecutionRuns()
	if err != nil {
		t.Fatalf("missing runs directory should not error: %v", err)
	}
	if len(runs) != 0 {
		t.Errorf("expected no runs, got %d", len(runs))
	}

	saved := []*domain.ExecutionRun{
		{WorkItemID: "item-a", CRID: "cr-1", Outcome: domain.RunCompleted},
		{WorkItemID: "item-b", CRID: "cr-1", Outcome: domain.RunFailed},
		{WorkItemID: "item-c", CRID: "cr-2", Outcome: domain.RunCompleted},
	}
	for _, run := range saved {
		if err := store.SaveExecutionRun(run); err != nil {
			t.Fatalf("SaveExecutionRun(%s): %v", run.WorkItemID, err)
		}
	}

	runs, err = store.ListExecutionRuns()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(runs) != len(saved) {
		t.Fatalf("expected %d runs across both CRs, got %d", len(saved), len(runs))
	}

	byID := map[string]*domain.ExecutionRun{}
	for _, run := range runs {
		byID[run.WorkItemID] = run
	}
	for _, want := range saved {
		got, ok := byID[want.WorkItemID]
		if !ok {
			t.Errorf("run %s missing from listing", want.WorkItemID)
			continue
		}
		if got.CRID != want.CRID || got.Outcome != want.Outcome {
			t.Errorf("run %s = %+v, want CR %s outcome %s", want.WorkItemID, got, want.CRID, want.Outcome)
		}
	}
}
