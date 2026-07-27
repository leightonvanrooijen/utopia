package internal

import (
	"fmt"
	"os"
	"path/filepath"
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
    discover: haiku
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
