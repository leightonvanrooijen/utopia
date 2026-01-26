package ralphsequential

import (
	"strings"
	"testing"

	"github.com/leightonvanrooijen/utopia/internal/domain"
)

func TestNew(t *testing.T) {
	s := New()

	if s == nil {
		t.Fatal("New() returned nil")
	}
}

func TestStrategy_Name(t *testing.T) {
	s := New()

	if got := s.Name(); got != "ralph-sequential" {
		t.Errorf("Name() = %q, want %q", got, "ralph-sequential")
	}
}

func TestStrategy_Description(t *testing.T) {
	s := New()

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
	s := New()

	spec := &domain.Spec{
		ID:    "test-spec",
		Title: "Test Spec",
		Features: []domain.Feature{
			{
				ID:                 "feature-1",
				Description:        "First feature",
				AcceptanceCriteria: []string{"Criterion A", "Criterion B"},
			},
		},
	}

	items, err := s.Chunk(spec)
	if err != nil {
		t.Fatalf("Chunk() error = %v", err)
	}

	if len(items) != 1 {
		t.Fatalf("Chunk() returned %d items, want 1", len(items))
	}

	item := items[0]

	// Verify ID format
	if item.ID != "test-spec-feature-1" {
		t.Errorf("ID = %q, want %q", item.ID, "test-spec-feature-1")
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
	s := New()

	spec := &domain.Spec{
		ID: "multi-spec",
		Features: []domain.Feature{
			{ID: "f1", Description: "Feature 1", AcceptanceCriteria: []string{"C1"}},
			{ID: "f2", Description: "Feature 2", AcceptanceCriteria: []string{"C2"}},
			{ID: "f3", Description: "Feature 3", AcceptanceCriteria: []string{"C3"}},
		},
	}

	items, err := s.Chunk(spec)
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

		expectedID := "multi-spec-f" + string(rune('1'+i))
		if item.ID != expectedID {
			t.Errorf("items[%d].ID = %q, want %q", i, item.ID, expectedID)
		}
	}
}

func TestStrategy_Chunk_NoFeatures(t *testing.T) {
	s := New()

	spec := &domain.Spec{
		ID:       "empty-spec",
		Features: []domain.Feature{},
	}

	items, err := s.Chunk(spec)
	if err != nil {
		t.Fatalf("Chunk() error = %v", err)
	}

	if len(items) != 0 {
		t.Errorf("Chunk() returned %d items for empty spec, want 0", len(items))
	}
}

func TestStrategy_Validate_NoAcceptanceCriteria(t *testing.T) {
	s := New()

	spec := &domain.Spec{
		ID: "invalid-spec",
		Features: []domain.Feature{
			{ID: "bad-feature", Description: "No criteria", AcceptanceCriteria: []string{}},
		},
	}

	_, err := s.Chunk(spec)
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

func TestStrategy_Validate_VagueTerms(t *testing.T) {
	tests := []struct {
		name      string
		criterion string
		wantError bool
	}{
		{"should be good", "The code should be good", true},
		{"works well", "It works well with other systems", true},
		{"is nice", "The API is nice", true},
		{"looks good", "The output looks good", true},
		{"feels right", "The UX feels right", true},
		{"is clean", "The code is clean", true},
		{"is better", "This approach is better", true},
		{"is optimal", "Performance is optimal", true},
		{"is fast enough", "Response time is fast enough", true},
		{"is reasonable", "Memory usage is reasonable", true},
		{"specific criterion", "Returns HTTP 200 on success", false},
		{"testable criterion", "Creates a file at /tmp/test.txt", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := New()
			spec := &domain.Spec{
				ID: "test",
				Features: []domain.Feature{
					{ID: "f1", Description: "Test", AcceptanceCriteria: []string{tt.criterion}},
				},
			}

			_, err := s.Chunk(spec)

			if tt.wantError && err == nil {
				t.Errorf("Chunk() should reject vague criterion %q", tt.criterion)
			}
			if !tt.wantError && err != nil {
				t.Errorf("Chunk() should accept criterion %q, got error: %v", tt.criterion, err)
			}
		})
	}
}

func TestStrategy_Validate_VagueTermInQuotes(t *testing.T) {
	s := New()

	// Vague terms in quotes should be allowed (they're examples)
	spec := &domain.Spec{
		ID: "quoted-spec",
		Features: []domain.Feature{
			{
				ID:          "f1",
				Description: "Error handling",
				AcceptanceCriteria: []string{
					`Error message should not contain "looks good" literally`,
					`Avoid responses like "is nice" in production`,
				},
			},
		},
	}

	_, err := s.Chunk(spec)
	if err != nil {
		t.Errorf("Chunk() should allow vague terms in quotes: %v", err)
	}
}

func TestStrategy_Validate_VagueTermInSingleQuotes(t *testing.T) {
	s := New()

	// Note: The quote detection algorithm counts all single quotes,
	// so apostrophes in contractions (like "Don't") can interfere.
	// Use examples without contractions for reliable detection.
	spec := &domain.Spec{
		ID: "single-quoted-spec",
		Features: []domain.Feature{
			{
				ID:          "f1",
				Description: "Validation",
				AcceptanceCriteria: []string{
					`Never return 'should be good' as a status`,
				},
			},
		},
	}

	_, err := s.Chunk(spec)
	if err != nil {
		t.Errorf("Chunk() should allow vague terms in single quotes: %v", err)
	}
}

func TestStrategy_Validate_MultipleErrors(t *testing.T) {
	s := New()

	spec := &domain.Spec{
		ID: "multi-error-spec",
		Features: []domain.Feature{
			{ID: "f1", Description: "No criteria", AcceptanceCriteria: []string{}},
			{ID: "f2", Description: "Vague", AcceptanceCriteria: []string{"It should be good"}},
			{ID: "f3", Description: "Also vague", AcceptanceCriteria: []string{"Performance is reasonable"}},
		},
	}

	_, err := s.Chunk(spec)
	if err == nil {
		t.Fatal("Chunk() should return error for multiple invalid features")
	}

	valErr, ok := err.(*ValidationError)
	if !ok {
		t.Fatalf("error should be *ValidationError, got %T", err)
	}

	// Should have at least 3 errors (1 missing criteria + 2 vague)
	if len(valErr.Errors) < 3 {
		t.Errorf("ValidationError should have at least 3 errors, got %d: %v", len(valErr.Errors), valErr.Errors)
	}
}

func TestStrategy_MergeConstraints_DefaultsOnly(t *testing.T) {
	s := New()

	spec := &domain.Spec{
		ID:              "no-knowledge",
		DomainKnowledge: []string{},
		Features: []domain.Feature{
			{ID: "f1", Description: "Test", AcceptanceCriteria: []string{"Works"}},
		},
	}

	items, err := s.Chunk(spec)
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

func TestStrategy_MergeConstraints_WithDomainKnowledge(t *testing.T) {
	s := New()

	spec := &domain.Spec{
		ID: "with-knowledge",
		DomainKnowledge: []string{
			"Do not use external APIs",       // Should be included (starts with "Do not")
			"Never store passwords in plain", // Should be included (starts with "Never")
			"This is just info",              // Should NOT be included (not a constraint)
			"Always validate input",          // Should be included (starts with "Always")
		},
		Features: []domain.Feature{
			{ID: "f1", Description: "Test", AcceptanceCriteria: []string{"Works"}},
		},
	}

	items, err := s.Chunk(spec)
	if err != nil {
		t.Fatalf("Chunk() error = %v", err)
	}

	constraints := items[0].Constraints

	// Should have defaults + 3 constraint-like knowledge items
	expectedMin := len(DefaultConstraints) + 3
	if len(constraints) < expectedMin {
		t.Errorf("got %d constraints, want at least %d", len(constraints), expectedMin)
	}

	// Verify constraint-like items are included
	mustContain := []string{
		"Do not use external APIs",
		"Never store passwords in plain",
		"Always validate input",
	}
	for _, expected := range mustContain {
		found := false
		for _, c := range constraints {
			if c == expected {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("missing expected constraint: %q", expected)
		}
	}

	// Verify non-constraint is NOT included
	for _, c := range constraints {
		if c == "This is just info" {
			t.Error("non-constraint domain knowledge should not be included")
		}
	}
}

func TestStrategy_MergeConstraints_Deduplication(t *testing.T) {
	s := New()

	spec := &domain.Spec{
		ID: "duplicate-spec",
		DomainKnowledge: []string{
			"Do not introduce new abstractions, interfaces, or packages", // Duplicate of default
			"do not introduce new abstractions, interfaces, or packages", // Case-different duplicate
		},
		Features: []domain.Feature{
			{ID: "f1", Description: "Test", AcceptanceCriteria: []string{"Works"}},
		},
	}

	items, err := s.Chunk(spec)
	if err != nil {
		t.Fatalf("Chunk() error = %v", err)
	}

	constraints := items[0].Constraints

	// Count occurrences of the constraint
	count := 0
	for _, c := range constraints {
		if strings.ToLower(c) == strings.ToLower("Do not introduce new abstractions, interfaces, or packages") {
			count++
		}
	}

	if count != 1 {
		t.Errorf("duplicate constraint appeared %d times, want 1", count)
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

func TestContainsVagueTerm(t *testing.T) {
	tests := []struct {
		name      string
		criterion string
		vagueTerm string
		expected  bool
	}{
		{"direct match", "Code should be good", "should be good", true},
		{"case insensitive", "Code SHOULD BE GOOD", "should be good", true},
		{"in double quotes", `Warn user with "should be good" message`, "should be good", false},
		{"in single quotes", `Avoid 'should be good' responses`, "should be good", false},
		{"not present", "Returns HTTP 200", "should be good", false},
		{"partial match at start", "Should be validated", "should be good", false},
		{"nested quotes double-single", `Say "it's good"`, "is good", false},
		// Known limitation: apostrophes count as single quotes
		{"apostrophe interference", `Don't say 'should be good'`, "should be good", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := containsVagueTerm(tt.criterion, tt.vagueTerm)
			if got != tt.expected {
				t.Errorf("containsVagueTerm(%q, %q) = %v, want %v",
					tt.criterion, tt.vagueTerm, got, tt.expected)
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

func TestVagueTerms_NotEmpty(t *testing.T) {
	if len(VagueTerms) == 0 {
		t.Error("VagueTerms should not be empty")
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

func TestStrategy_Chunk_RefactorSpec_InjectsConstraints(t *testing.T) {
	s := New()

	// Create a spec marked as a refactor
	spec := &domain.Spec{
		ID:         "refactor-test",
		Title:      "Test Refactor",
		IsRefactor: true,
		Features: []domain.Feature{
			{
				ID:                 "task-1",
				Description:        "Refactor the auth module",
				AcceptanceCriteria: []string{"Auth module is refactored"},
			},
		},
	}

	items, err := s.Chunk(spec)
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

func TestStrategy_Chunk_NonRefactorSpec_NoRefactorConstraints(t *testing.T) {
	s := New()

	// Create a regular (non-refactor) spec
	spec := &domain.Spec{
		ID:         "regular-spec",
		Title:      "Regular Spec",
		IsRefactor: false, // Not a refactor
		Features: []domain.Feature{
			{
				ID:                 "feature-1",
				Description:        "Add new feature",
				AcceptanceCriteria: []string{"Feature is added"},
			},
		},
	}

	items, err := s.Chunk(spec)
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

func TestStrategy_ChunkRefactor(t *testing.T) {
	s := New()

	refactor := &domain.Refactor{
		ID:    "test-refactor",
		Title: "Test Refactoring",
		Tasks: []domain.RefactorTask{
			{
				ID:                 "task-1",
				Description:        "Extract helper function",
				AcceptanceCriteria: []string{"Helper function is extracted"},
			},
			{
				ID:                 "task-2",
				Description:        "Rename variable",
				AcceptanceCriteria: []string{"Variable is renamed"},
			},
		},
	}

	items, err := s.ChunkRefactor(refactor)
	if err != nil {
		t.Fatalf("ChunkRefactor() error = %v", err)
	}

	if len(items) != 2 {
		t.Fatalf("ChunkRefactor() returned %d items, want 2", len(items))
	}

	// Verify all items have refactor constraints
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
				t.Errorf("items[%d] missing refactor system constraint: %q", i, rc)
			}
		}
	}
}

func TestStrategy_MergeConstraints_RefactorConstraintsFirst(t *testing.T) {
	s := New()

	spec := &domain.Spec{
		ID:         "refactor-order-test",
		IsRefactor: true,
		Features: []domain.Feature{
			{ID: "f1", Description: "Test", AcceptanceCriteria: []string{"Works"}},
		},
	}

	items, err := s.Chunk(spec)
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
