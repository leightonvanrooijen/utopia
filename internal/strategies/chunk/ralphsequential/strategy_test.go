package ralphsequential

import (
	"strings"
	"testing"

	"github.com/leightonvanrooijen/utopia/internal/domain"
	"github.com/leightonvanrooijen/utopia/internal/testutil"
)

func TestNew(t *testing.T) {
	s := New(nil)

	if s == nil {
		t.Fatal("New() returned nil")
	}
}

func TestStrategy_Name(t *testing.T) {
	s := New(nil)

	if got := s.Name(); got != "ralph-sequential" {
		t.Errorf("Name() = %q, want %q", got, "ralph-sequential")
	}
}

func TestStrategy_Description(t *testing.T) {
	s := New(nil)

	desc := s.Description()
	if desc == "" {
		t.Error("Description() returned empty string")
	}

	// Should mention key characteristics
	lower := strings.ToLower(desc)
	if !strings.Contains(lower, "sequential") && !strings.Contains(lower, "feature") {
		t.Error("Description should mention sequential or feature-based execution")
	}
}


func TestStrategy_Chunk_SingleFeature(t *testing.T) {
	s := New(nil)

	cr := testutil.CRWithFeatures("test-cr",
		domain.Feature{
			ID:                 "feature-1",
			Description:        "First feature",
			AcceptanceCriteria: []string{"Criterion A", "Criterion B"},
		},
	)

	items, err := s.Chunk(cr)
	if err != nil {
		t.Fatalf("Chunk() error = %v", err)
	}

	if len(items) != 1 {
		t.Fatalf("Chunk() returned %d items, want 1", len(items))
	}

	item := items[0]

	// Verify ID format
	if item.ID != "test-cr-feature-1" {
		t.Errorf("ID = %q, want %q", item.ID, "test-cr-feature-1")
	}

	// Verify order
	if item.Order != 0 {
		t.Errorf("Order = %d, want %d", item.Order, 0)
	}

	// Verify prompt contains task and criteria
	if !strings.Contains(item.Prompt, "First feature") {
		t.Error("Prompt should contain feature description")
	}
	if !strings.Contains(item.Prompt, "Criterion A") {
		t.Error("Prompt should contain acceptance criteria")
	}

	// Verify constraints include defaults
	if len(item.Constraints) < len(DefaultConstraints) {
		t.Errorf("Constraints count = %d, want at least %d", len(item.Constraints), len(DefaultConstraints))
	}
}

func TestStrategy_Chunk_MultipleFeatures(t *testing.T) {
	s := New(nil)

	cr := testutil.CRWithFeatures("multi-cr",
		domain.Feature{ID: "f1", Description: "Feature 1", AcceptanceCriteria: []string{"C1"}},
		domain.Feature{ID: "f2", Description: "Feature 2", AcceptanceCriteria: []string{"C2"}},
		domain.Feature{ID: "f3", Description: "Feature 3", AcceptanceCriteria: []string{"C3"}},
	)

	items, err := s.Chunk(cr)
	if err != nil {
		t.Fatalf("Chunk() error = %v", err)
	}

	if len(items) != 3 {
		t.Fatalf("Chunk() returned %d items, want 3", len(items))
	}

	// Verify sequential ordering
	for i, item := range items {
		if item.Order != i {
			t.Errorf("items[%d].Order = %d, want %d", i, item.Order, i)
		}

		expectedID := "multi-cr-f" + string(rune('1'+i))
		if item.ID != expectedID {
			t.Errorf("items[%d].ID = %q, want %q", i, item.ID, expectedID)
		}
	}
}

func TestStrategy_Chunk_NoFeatures(t *testing.T) {
	s := New(nil)

	cr := &domain.ChangeRequest{
		ID:      "empty-cr",
		Type:    domain.CRTypeFeature,
		Changes: []domain.Change{},
	}

	items, err := s.Chunk(cr)
	if err != nil {
		t.Fatalf("Chunk() error = %v", err)
	}

	if len(items) != 0 {
		t.Errorf("Chunk() returned %d items for empty CR, want 0", len(items))
	}
}

func TestStrategy_Validate_NoAcceptanceCriteria(t *testing.T) {
	s := New(nil)

	cr := testutil.CRWithFeatures("invalid-cr",
		domain.Feature{ID: "bad-feature", Description: "No criteria", AcceptanceCriteria: []string{}},
	)

	_, err := s.Chunk(cr)
	if err == nil {
		t.Fatal("Chunk() should return error for feature without acceptance criteria")
	}

	valErr, ok := err.(*ValidationError)
	if !ok {
		t.Fatalf("error should be *ValidationError, got %T", err)
	}

	if len(valErr.Errors) != 1 {
		t.Errorf("ValidationError should have 1 error, got %d", len(valErr.Errors))
	}

	if !strings.Contains(valErr.Errors[0], "no acceptance criteria") {
		t.Errorf("error message should mention missing criteria: %q", valErr.Errors[0])
	}
}

func TestStrategy_Validate_MultipleEmptyCriteria(t *testing.T) {
	s := New(nil)

	cr := testutil.CRWithFeatures("multi-error-cr",
		domain.Feature{ID: "f1", Description: "No criteria", AcceptanceCriteria: []string{}},
		domain.Feature{ID: "f2", Description: "Also empty", AcceptanceCriteria: []string{}},
	)

	_, err := s.Chunk(cr)
	if err == nil {
		t.Fatal("Chunk() should return error for features without acceptance criteria")
	}

	valErr, ok := err.(*ValidationError)
	if !ok {
		t.Fatalf("error should be *ValidationError, got %T", err)
	}

	if len(valErr.Errors) != 2 {
		t.Errorf("ValidationError should have 2 errors, got %d: %v", len(valErr.Errors), valErr.Errors)
	}
}

func TestStrategy_MergeConstraints_DefaultsOnly(t *testing.T) {
	s := New(nil)

	cr := testutil.CRWithFeatures("no-knowledge",
		domain.Feature{ID: "f1", Description: "Test", AcceptanceCriteria: []string{"Works"}},
	)

	items, err := s.Chunk(cr)
	if err != nil {
		t.Fatalf("Chunk() error = %v", err)
	}

	constraints := items[0].Constraints

	// Should have exactly the default constraints
	if len(constraints) != len(DefaultConstraints) {
		t.Errorf("got %d constraints, want %d", len(constraints), len(DefaultConstraints))
	}

	for _, dc := range DefaultConstraints {
		found := false
		for _, c := range constraints {
			if c == dc {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("missing default constraint: %q", dc)
		}
	}
}

func TestLooksLikeConstraint(t *testing.T) {
	tests := []struct {
		input    string
		expected bool
	}{
		{"Do not use external libraries", true},
		{"Don't modify the database", true},
		{"Never expose internal IDs", true},
		{"Avoid blocking operations", true},
		{"Must not exceed 100ms", true},
		{"Only use approved vendors", true},
		{"Always log errors", true},
		{"Must handle errors", true},
		{"Should not throw exceptions", true},
		{"The system uses PostgreSQL", false},
		{"Users can upload files", false},
		{"API returns JSON", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := looksLikeConstraint(tt.input)
			if got != tt.expected {
				t.Errorf("looksLikeConstraint(%q) = %v, want %v", tt.input, got, tt.expected)
			}
		})
	}
}

func TestValidationError_Error(t *testing.T) {
	err := &ValidationError{
		Errors: []string{"error 1", "error 2", "error 3"},
	}

	msg := err.Error()

	if !strings.Contains(msg, "spec validation failed") {
		t.Error("error message should contain 'spec validation failed'")
	}

	for _, e := range err.Errors {
		if !strings.Contains(msg, e) {
			t.Errorf("error message should contain %q", e)
		}
	}
}

func TestDefaultConstraints_NotEmpty(t *testing.T) {
	if len(DefaultConstraints) == 0 {
		t.Error("DefaultConstraints should not be empty")
	}
}

func TestRefactorSystemConstraints_NotEmpty(t *testing.T) {
	if len(RefactorSystemConstraints) == 0 {
		t.Error("RefactorSystemConstraints should not be empty")
	}
}

func TestRefactorSystemConstraints_RequiredText(t *testing.T) {
	// Verify required constraint text per acceptance criteria
	requiredPhrases := []string{
		"This is a refactor. Existing behavior MUST be preserved.",
		"All existing tests must pass without modification",
		"Do not introduce new abstractions, interfaces, or packages",
	}

	for _, phrase := range requiredPhrases {
		found := false
		for _, c := range RefactorSystemConstraints {
			if c == phrase {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("RefactorSystemConstraints missing required phrase: %q", phrase)
		}
	}
}

func TestStrategy_Chunk_RefactorCR_InjectsConstraints(t *testing.T) {
	s := New(nil)

	// Create a refactor change request with tasks
	cr := &domain.ChangeRequest{
		ID:    "refactor-test",
		Type:  domain.CRTypeRefactor,
		Title: "Test Refactor",
		Tasks: []domain.Task{
			{
				ID:                 "task-1",
				Description:        "Refactor the auth module",
				AcceptanceCriteria: []string{"Auth module is refactored"},
			},
		},
	}

	items, err := s.Chunk(cr)
	if err != nil {
		t.Fatalf("Chunk() error = %v", err)
	}

	if len(items) != 1 {
		t.Fatalf("Chunk() returned %d items, want 1", len(items))
	}

	item := items[0]

	// Verify refactor system constraints are included
	for _, rc := range RefactorSystemConstraints {
		found := false
		for _, c := range item.Constraints {
			if c == rc {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("work item missing refactor system constraint: %q", rc)
		}
	}

	// Verify constraints appear in the CONSTRAINTS section of the prompt
	if !strings.Contains(item.Prompt, "## CONSTRAINTS") {
		t.Error("Prompt should contain CONSTRAINTS section")
	}
	for _, rc := range RefactorSystemConstraints {
		if !strings.Contains(item.Prompt, rc) {
			t.Errorf("Prompt CONSTRAINTS section should contain: %q", rc)
		}
	}
}

func TestStrategy_Chunk_NonRefactorCR_NoRefactorConstraints(t *testing.T) {
	s := New(nil)

	// Create a feature change request (not a refactor)
	cr := testutil.CRWithFeatures("regular-cr",
		domain.Feature{
			ID:                 "feature-1",
			Description:        "Add new feature",
			AcceptanceCriteria: []string{"Feature is added"},
		},
	)

	items, err := s.Chunk(cr)
	if err != nil {
		t.Fatalf("Chunk() error = %v", err)
	}

	item := items[0]

	// Verify refactor system constraints are NOT included
	for _, rc := range RefactorSystemConstraints {
		for _, c := range item.Constraints {
			if c == rc {
				t.Errorf("non-refactor work item should NOT have refactor constraint: %q", rc)
			}
		}
	}
}

func TestStrategy_MergeConstraints_RefactorConstraintsFirst(t *testing.T) {
	s := New(nil)

	cr := &domain.ChangeRequest{
		ID:   "refactor-order-test",
		Type: domain.CRTypeRefactor,
		Tasks: []domain.Task{
			{ID: "f1", Description: "Test", AcceptanceCriteria: []string{"Works"}},
		},
	}

	items, err := s.Chunk(cr)
	if err != nil {
		t.Fatalf("Chunk() error = %v", err)
	}

	constraints := items[0].Constraints

	// Refactor constraints should come first
	if len(constraints) < len(RefactorSystemConstraints) {
		t.Fatalf("not enough constraints: got %d, want at least %d",
			len(constraints), len(RefactorSystemConstraints))
	}

	for i, rc := range RefactorSystemConstraints {
		if constraints[i] != rc {
			t.Errorf("constraint[%d] = %q, want refactor constraint %q", i, constraints[i], rc)
		}
	}
}

// TestStrategy_Chunk_RefactorCR_MultipleTasks verifies that change requests
// with type "refactor" receive behavior-preservation constraints on all work items.
func TestStrategy_Chunk_RefactorCR_MultipleTasks(t *testing.T) {
	s := New(nil)

	// Create a refactor change request with multiple tasks
	cr := &domain.ChangeRequest{
		ID:    "refactor-auth",
		Type:  domain.CRTypeRefactor,
		Title: "Refactor authentication module",
		Tasks: []domain.Task{
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

	items, err := s.Chunk(cr)
	if err != nil {
		t.Fatalf("Chunk() error = %v", err)
	}

	if len(items) != 2 {
		t.Fatalf("Chunk() returned %d items, want 2", len(items))
	}

	// Verify ALL work items receive behavior-preservation constraints
	for i, item := range items {
		for _, rc := range RefactorSystemConstraints {
			found := false
			for _, c := range item.Constraints {
				if c == rc {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("work item[%d] (%s) missing refactor constraint: %q", i, item.ID, rc)
			}
		}

		// Verify constraints appear in the prompt
		if !strings.Contains(item.Prompt, "This is a refactor") {
			t.Errorf("work item[%d] prompt should contain refactor constraint", i)
		}
	}
}

// TestStrategy_Chunk_FeatureCR_NoRefactorConstraints verifies that
// non-refactor CRs do NOT receive behavior-preservation constraints.
func TestStrategy_Chunk_FeatureCR_NoRefactorConstraints(t *testing.T) {
	s := New(nil)

	// Create a feature change request (not a refactor)
	cr := &domain.ChangeRequest{
		ID:    "feature-new-login",
		Type:  domain.CRTypeFeature,
		Title: "Add OAuth login",
		Changes: []domain.Change{
			{
				Operation: "add",
				Feature: &domain.Feature{
					ID:                 "oauth-login",
					Description:        "Add OAuth login support",
					AcceptanceCriteria: []string{"OAuth login works"},
				},
			},
		},
	}

	items, err := s.Chunk(cr)
	if err != nil {
		t.Fatalf("Chunk() error = %v", err)
	}

	// Verify work items do NOT have refactor constraints
	for i, item := range items {
		for _, rc := range RefactorSystemConstraints {
			for _, c := range item.Constraints {
				if c == rc {
					t.Errorf("work item[%d] should NOT have refactor constraint: %q", i, rc)
				}
			}
		}
	}
}

// TestStrategy_ChunkPhase verifies that ChunkPhase correctly handles initiative phases
func TestStrategy_ChunkPhase_SingleTask(t *testing.T) {
	s := New(nil)

	phase := &domain.Phase{
		Type: domain.CRTypeFeature,
		Tasks: []domain.Task{
			{
				ID:                 "task-1",
				Description:        "First task",
				AcceptanceCriteria: []string{"Task completed"},
			},
		},
	}

	items, err := s.ChunkPhase("initiative-1", 0, phase)
	if err != nil {
		t.Fatalf("ChunkPhase() error = %v", err)
	}

	if len(items) != 1 {
		t.Fatalf("ChunkPhase() returned %d items, want 1", len(items))
	}

	// Verify ID format includes phase info
	if !strings.Contains(items[0].ID, "initiative-1-phase-0") {
		t.Errorf("ID = %q, should contain 'initiative-1-phase-0'", items[0].ID)
	}
}

func TestStrategy_ChunkPhase_RefactorPhase(t *testing.T) {
	s := New(nil)

	phase := &domain.Phase{
		Type: domain.CRTypeRefactor,
		Tasks: []domain.Task{
			{
				ID:                 "refactor-task",
				Description:        "Refactor code",
				AcceptanceCriteria: []string{"Code refactored"},
			},
		},
	}

	items, err := s.ChunkPhase("initiative-1", 1, phase)
	if err != nil {
		t.Fatalf("ChunkPhase() error = %v", err)
	}

	// Verify refactor constraints are injected
	for _, rc := range RefactorSystemConstraints {
		found := false
		for _, c := range items[0].Constraints {
			if c == rc {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("work item missing refactor system constraint: %q", rc)
		}
	}
}

func TestStrategy_ChunkPhase_WithChanges(t *testing.T) {
	s := New(nil)

	phase := &domain.Phase{
		Type: domain.CRTypeFeature,
		Changes: []domain.Change{
			{
				Operation: "add",
				Feature: &domain.Feature{
					ID:                 "new-feature",
					Description:        "New feature",
					AcceptanceCriteria: []string{"Feature works"},
				},
			},
		},
	}

	items, err := s.ChunkPhase("initiative-1", 0, phase)
	if err != nil {
		t.Fatalf("ChunkPhase() error = %v", err)
	}

	if len(items) != 1 {
		t.Fatalf("ChunkPhase() returned %d items, want 1", len(items))
	}

	if !strings.Contains(items[0].ID, "new-feature") {
		t.Errorf("ID = %q, should contain 'new-feature'", items[0].ID)
	}
}

// Test extractFeatures with different operation types
func TestStrategy_ExtractFeatures_RemoveOperation(t *testing.T) {
	s := New(nil)

	cr := &domain.ChangeRequest{
		ID:   "remove-test",
		Type: domain.CRTypeRemoval,
		Changes: []domain.Change{
			{
				Operation: "remove",
				FeatureID: "old-feature",
				Reason:    "No longer needed",
			},
		},
	}

	items, err := s.Chunk(cr)
	if err != nil {
		t.Fatalf("Chunk() error = %v", err)
	}

	if len(items) != 1 {
		t.Fatalf("Chunk() returned %d items, want 1", len(items))
	}

	// Verify the generated work item ID includes the feature ID
	if !strings.Contains(items[0].ID, "remove-old-feature") {
		t.Errorf("ID = %q, should contain 'remove-old-feature'", items[0].ID)
	}

	// Verify the reason is in acceptance criteria
	if !strings.Contains(items[0].Prompt, "No longer needed") {
		t.Error("Prompt should contain removal reason")
	}
}

func TestStrategy_ExtractFeatures_ModifyOperation(t *testing.T) {
	s := New(nil)

	cr := &domain.ChangeRequest{
		ID:   "modify-test",
		Type: domain.CRTypeEnhancement,
		Changes: []domain.Change{
			{
				Operation:   "modify",
				FeatureID:   "existing-feature",
				Description: "Updated behavior",
				Criteria: &domain.CriteriaModify{
					Add: []string{"New criterion"},
				},
			},
		},
	}

	items, err := s.Chunk(cr)
	if err != nil {
		t.Fatalf("Chunk() error = %v", err)
	}

	if len(items) != 1 {
		t.Fatalf("Chunk() returned %d items, want 1", len(items))
	}

	// Verify the generated work item ID includes the feature ID
	if !strings.Contains(items[0].ID, "modify-existing-feature") {
		t.Errorf("ID = %q, should contain 'modify-existing-feature'", items[0].ID)
	}

	// Verify the description change
	if !strings.Contains(items[0].Prompt, "Updated behavior") {
		t.Error("Prompt should contain updated description")
	}
}

func TestStrategy_ExtractFeatures_DeleteSpecOperation(t *testing.T) {
	s := New(nil)

	cr := &domain.ChangeRequest{
		ID:   "delete-spec-test",
		Type: domain.CRTypeRemoval,
		Changes: []domain.Change{
			{
				Operation: "delete-spec",
				Spec:      "old-spec",
				Reason:    "Deprecated",
			},
		},
	}

	items, err := s.Chunk(cr)
	if err != nil {
		t.Fatalf("Chunk() error = %v", err)
	}

	if len(items) != 1 {
		t.Fatalf("Chunk() returned %d items, want 1", len(items))
	}

	// Verify the generated work item ID includes the feature ID
	if !strings.Contains(items[0].ID, "delete-spec-old-spec") {
		t.Errorf("ID = %q, should contain 'delete-spec-old-spec'", items[0].ID)
	}

	// Verify the deletion info is in prompt
	if !strings.Contains(items[0].Prompt, "old-spec") {
		t.Error("Prompt should contain spec name")
	}
}

// TestBugfixSystemConstraints_RequiredText verifies bugfix constraints include required text
func TestBugfixSystemConstraints_RequiredText(t *testing.T) {
	requiredPhrases := []string{
		"This is a bugfix. The implementation must be corrected to match the spec.",
		"The acceptance criteria below are the source of truth for correct behavior.",
	}

	for _, phrase := range requiredPhrases {
		found := false
		for _, c := range BugfixSystemConstraints {
			if c == phrase {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("BugfixSystemConstraints missing required phrase: %q", phrase)
		}
	}
}

// TestStrategy_Chunk_BugfixCR_InjectsConstraints verifies that bugfix CRs get bugfix constraints
func TestStrategy_Chunk_BugfixCR_InjectsConstraints(t *testing.T) {
	s := New(nil)

	// Create a bugfix change request
	cr := &domain.ChangeRequest{
		ID:    "bugfix-test",
		Type:  domain.CRTypeBugfix,
		Title: "Fix authentication bug",
		Tasks: []domain.Task{
			{
				ID:                 "fix-login",
				Description:        "Fix login validation",
				AcceptanceCriteria: []string{"Login works correctly"},
				// Note: No spec/feature_id since we're not testing the reference injection
			},
		},
	}

	items, err := s.Chunk(cr)
	if err != nil {
		t.Fatalf("Chunk() error = %v", err)
	}

	if len(items) != 1 {
		t.Fatalf("Chunk() returned %d items, want 1", len(items))
	}

	item := items[0]

	// Verify bugfix system constraints are included
	for _, bc := range BugfixSystemConstraints {
		found := false
		for _, c := range item.Constraints {
			if c == bc {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("work item missing bugfix system constraint: %q", bc)
		}
	}

	// Verify constraints appear in the prompt
	if !strings.Contains(item.Prompt, "This is a bugfix") {
		t.Error("Prompt should contain bugfix constraint")
	}
	if !strings.Contains(item.Prompt, "source of truth for correct behavior") {
		t.Error("Prompt should contain source of truth constraint")
	}
}

// TestStrategy_Chunk_BugfixCR_WithSpecReference verifies that bugfix CRs load and inject referenced features
func TestStrategy_Chunk_BugfixCR_WithSpecReference(t *testing.T) {
	// Create a mock spec loader that returns a test spec
	testSpec := &domain.Spec{
		ID:    "auth-spec",
		Title: "Authentication Spec",
		Features: []domain.Feature{
			{
				ID:          "login",
				Description: "User login functionality",
				AcceptanceCriteria: []string{
					"User can login with valid credentials",
					"Invalid credentials return 401",
				},
			},
		},
	}

	specLoader := func(specID string) (*domain.Spec, error) {
		if specID == "auth-spec" {
			return testSpec, nil
		}
		return nil, nil
	}

	s := New(specLoader)

	cr := &domain.ChangeRequest{
		ID:    "bugfix-login",
		Type:  domain.CRTypeBugfix,
		Title: "Fix login bug",
		Tasks: []domain.Task{
			{
				ID:                 "fix-validation",
				Description:        "Fix credential validation",
				AcceptanceCriteria: []string{"Validation matches spec"},
				Spec:               "auth-spec",
				FeatureID:          "login",
			},
		},
	}

	items, err := s.Chunk(cr)
	if err != nil {
		t.Fatalf("Chunk() error = %v", err)
	}

	item := items[0]

	// Verify REFERENCE section is in the prompt
	if !strings.Contains(item.Prompt, "## REFERENCE") {
		t.Error("Prompt should contain REFERENCE section")
	}

	// Verify reference content from spec feature is included
	if !strings.Contains(item.Prompt, "User login functionality") {
		t.Error("Prompt should contain referenced feature description")
	}
	if !strings.Contains(item.Prompt, "User can login with valid credentials") {
		t.Error("Prompt should contain referenced feature criteria")
	}
}

// TestStrategy_Chunk_BugfixCR_MissingSpec verifies chunking fails when spec not found
func TestStrategy_Chunk_BugfixCR_MissingSpec(t *testing.T) {
	specLoader := func(specID string) (*domain.Spec, error) {
		return nil, nil // Spec not found
	}

	s := New(specLoader)

	cr := &domain.ChangeRequest{
		ID:    "bugfix-missing",
		Type:  domain.CRTypeBugfix,
		Title: "Fix with missing spec",
		Tasks: []domain.Task{
			{
				ID:                 "task-1",
				Description:        "Fix something",
				AcceptanceCriteria: []string{"Fixed"},
				Spec:               "nonexistent-spec",
				FeatureID:          "feature-1",
			},
		},
	}

	_, err := s.Chunk(cr)
	if err == nil {
		t.Fatal("Chunk() should return error when referenced spec not found")
	}

	if !strings.Contains(err.Error(), "nonexistent-spec") {
		t.Errorf("error should mention missing spec: %v", err)
	}
}

// TestStrategy_Chunk_BugfixCR_MissingFeature verifies chunking fails when feature not in spec
func TestStrategy_Chunk_BugfixCR_MissingFeature(t *testing.T) {
	testSpec := &domain.Spec{
		ID:       "auth-spec",
		Features: []domain.Feature{},
	}

	specLoader := func(specID string) (*domain.Spec, error) {
		return testSpec, nil
	}

	s := New(specLoader)

	cr := &domain.ChangeRequest{
		ID:    "bugfix-missing-feature",
		Type:  domain.CRTypeBugfix,
		Title: "Fix with missing feature",
		Tasks: []domain.Task{
			{
				ID:                 "task-1",
				Description:        "Fix something",
				AcceptanceCriteria: []string{"Fixed"},
				Spec:               "auth-spec",
				FeatureID:          "nonexistent-feature",
			},
		},
	}

	_, err := s.Chunk(cr)
	if err == nil {
		t.Fatal("Chunk() should return error when referenced feature not found")
	}

	if !strings.Contains(err.Error(), "nonexistent-feature") {
		t.Errorf("error should mention missing feature: %v", err)
	}
}

// TestStrategy_Chunk_BugfixCR_NoSpecLoader verifies chunking fails when no spec loader configured
func TestStrategy_Chunk_BugfixCR_NoSpecLoader(t *testing.T) {
	s := New(nil) // No spec loader

	cr := &domain.ChangeRequest{
		ID:    "bugfix-no-loader",
		Type:  domain.CRTypeBugfix,
		Title: "Fix without loader",
		Tasks: []domain.Task{
			{
				ID:                 "task-1",
				Description:        "Fix something",
				AcceptanceCriteria: []string{"Fixed"},
				Spec:               "some-spec",
				FeatureID:          "some-feature",
			},
		},
	}

	_, err := s.Chunk(cr)
	if err == nil {
		t.Fatal("Chunk() should return error when spec loader not configured")
	}

	if !strings.Contains(err.Error(), "spec loader") {
		t.Errorf("error should mention missing spec loader: %v", err)
	}
}

// TestStrategy_ChunkPhase_BugfixPhase_WithSpecReference verifies bugfix phases work
func TestStrategy_ChunkPhase_BugfixPhase_WithSpecReference(t *testing.T) {
	testSpec := &domain.Spec{
		ID: "test-spec",
		Features: []domain.Feature{
			{
				ID:                 "target-feature",
				Description:        "Target feature from spec",
				AcceptanceCriteria: []string{"Criterion from spec"},
			},
		},
	}

	specLoader := func(specID string) (*domain.Spec, error) {
		return testSpec, nil
	}

	s := New(specLoader)

	phase := &domain.Phase{
		Type: domain.CRTypeBugfix,
		Tasks: []domain.Task{
			{
				ID:                 "bugfix-task",
				Description:        "Fix the bug",
				AcceptanceCriteria: []string{"Bug is fixed"},
				Spec:               "test-spec",
				FeatureID:          "target-feature",
			},
		},
	}

	items, err := s.ChunkPhase("initiative-1", 0, phase)
	if err != nil {
		t.Fatalf("ChunkPhase() error = %v", err)
	}

	// Verify REFERENCE section is included
	if !strings.Contains(items[0].Prompt, "## REFERENCE") {
		t.Error("Prompt should contain REFERENCE section for bugfix phase")
	}

	// Verify bugfix constraints are included
	for _, bc := range BugfixSystemConstraints {
		found := false
		for _, c := range items[0].Constraints {
			if c == bc {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("bugfix phase work item missing constraint: %q", bc)
		}
	}
}

// TestStrategy_SetSpecLoader verifies the SetSpecLoader method works
func TestStrategy_SetSpecLoader(t *testing.T) {
	s := New(nil) // Start with no loader

	// Initially should fail for bugfix with spec reference
	cr := &domain.ChangeRequest{
		ID:   "bugfix",
		Type: domain.CRTypeBugfix,
		Tasks: []domain.Task{
			{
				ID:                 "task-1",
				Description:        "Fix",
				AcceptanceCriteria: []string{"Fixed"},
				Spec:               "spec",
				FeatureID:          "feature",
			},
		},
	}

	_, err := s.Chunk(cr)
	if err == nil {
		t.Fatal("should fail without spec loader")
	}

	// Set the spec loader
	testSpec := &domain.Spec{
		ID: "spec",
		Features: []domain.Feature{
			{ID: "feature", Description: "Test", AcceptanceCriteria: []string{"AC"}},
		},
	}
	s.SetSpecLoader(func(specID string) (*domain.Spec, error) {
		return testSpec, nil
	})

	// Now should succeed
	items, err := s.Chunk(cr)
	if err != nil {
		t.Fatalf("Chunk() should succeed with spec loader: %v", err)
	}

	if !strings.Contains(items[0].Prompt, "## REFERENCE") {
		t.Error("should include REFERENCE section after setting spec loader")
	}
}
