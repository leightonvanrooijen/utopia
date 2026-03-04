package readme

import (
	"testing"

	"github.com/leightonvanrooijen/utopia/internal/domain"
)

// mockStrategy is a test double for Strategy interface
type mockStrategy struct {
	name        string
	description string
	detectFunc  func(specs []*domain.Spec, documented *READMEDocumented) []SignalCandidate
}

func (m *mockStrategy) Name() string {
	return m.name
}

func (m *mockStrategy) Description() string {
	return m.description
}

func (m *mockStrategy) Detect(specs []*domain.Spec, documented *READMEDocumented) []SignalCandidate {
	if m.detectFunc != nil {
		return m.detectFunc(specs, documented)
	}
	return nil
}

func TestNewRegistry(t *testing.T) {
	r := NewRegistry()

	if r == nil {
		t.Fatal("NewRegistry returned nil")
	}

	if r.strategies == nil {
		t.Error("strategies map is nil")
	}

	// Should be empty initially
	if len(r.List()) != 0 {
		t.Errorf("new registry should be empty, got %d strategies", len(r.List()))
	}
}

func TestRegistry_Register(t *testing.T) {
	r := NewRegistry()

	strategy := &mockStrategy{
		name:        "test-strategy",
		description: "A test strategy",
	}

	r.Register(strategy)

	// Should be retrievable
	got, ok := r.Get("test-strategy")
	if !ok {
		t.Error("registered strategy not found")
	}

	if got.Name() != "test-strategy" {
		t.Errorf("got name %q, want %q", got.Name(), "test-strategy")
	}
}

func TestRegistry_Get_NotFound(t *testing.T) {
	r := NewRegistry()

	_, ok := r.Get("nonexistent")
	if ok {
		t.Error("Get returned true for nonexistent strategy")
	}
}

func TestRegistry_Register_Overwrites(t *testing.T) {
	r := NewRegistry()

	// Register first version
	r.Register(&mockStrategy{
		name:        "my-strategy",
		description: "Version 1",
	})

	// Register second version with same name
	r.Register(&mockStrategy{
		name:        "my-strategy",
		description: "Version 2",
	})

	// Should get the second version
	got, ok := r.Get("my-strategy")
	if !ok {
		t.Fatal("strategy not found")
	}

	if got.Description() != "Version 2" {
		t.Errorf("got description %q, want %q", got.Description(), "Version 2")
	}

	// Should still only have one strategy
	if len(r.List()) != 1 {
		t.Errorf("expected 1 strategy, got %d", len(r.List()))
	}
}

func TestRegistry_List(t *testing.T) {
	r := NewRegistry()

	// Register multiple strategies
	r.Register(&mockStrategy{name: "alpha"})
	r.Register(&mockStrategy{name: "beta"})
	r.Register(&mockStrategy{name: "gamma"})

	names := r.List()

	if len(names) != 3 {
		t.Errorf("expected 3 strategies, got %d", len(names))
	}

	// Check all names are present (order may vary)
	nameSet := make(map[string]bool)
	for _, name := range names {
		nameSet[name] = true
	}

	for _, expected := range []string{"alpha", "beta", "gamma"} {
		if !nameSet[expected] {
			t.Errorf("missing strategy %q in list", expected)
		}
	}
}

func TestSignalCandidate_Fields(t *testing.T) {
	candidate := SignalCandidate{
		SpecID:           "my-spec",
		FeatureID:        "my-feature",
		Title:            "utopia new command",
		Category:         "command",
		Confidence:       domain.SignalConfidenceHigh,
		SuggestedSection: "Quick Start",
	}

	if candidate.SpecID != "my-spec" {
		t.Errorf("SpecID = %q, want %q", candidate.SpecID, "my-spec")
	}

	if candidate.FeatureID != "my-feature" {
		t.Errorf("FeatureID = %q, want %q", candidate.FeatureID, "my-feature")
	}

	if candidate.Title != "utopia new command" {
		t.Errorf("Title = %q, want %q", candidate.Title, "utopia new command")
	}

	if candidate.Category != "command" {
		t.Errorf("Category = %q, want %q", candidate.Category, "command")
	}

	if candidate.Confidence != domain.SignalConfidenceHigh {
		t.Errorf("Confidence = %q, want %q", candidate.Confidence, domain.SignalConfidenceHigh)
	}

	if candidate.SuggestedSection != "Quick Start" {
		t.Errorf("SuggestedSection = %q, want %q", candidate.SuggestedSection, "Quick Start")
	}
}

func TestREADMEDocumented_Fields(t *testing.T) {
	doc := &READMEDocumented{
		Commands:      []string{"cr", "execute", "harvest"},
		ArtifactTypes: []string{"ADR", "Concept", "Domain"},
		Directories:   []string{"specs", "workitems", "adrs"},
		WorkflowSteps: []string{"converse", "execute", "harvest"},
	}

	if len(doc.Commands) != 3 {
		t.Errorf("Commands length = %d, want %d", len(doc.Commands), 3)
	}

	if len(doc.ArtifactTypes) != 3 {
		t.Errorf("ArtifactTypes length = %d, want %d", len(doc.ArtifactTypes), 3)
	}

	if len(doc.Directories) != 3 {
		t.Errorf("Directories length = %d, want %d", len(doc.Directories), 3)
	}

	if len(doc.WorkflowSteps) != 3 {
		t.Errorf("WorkflowSteps length = %d, want %d", len(doc.WorkflowSteps), 3)
	}
}

func TestMockStrategy_Detect(t *testing.T) {
	expectedCandidates := []SignalCandidate{
		{
			SpecID:    "spec-1",
			FeatureID: "feature-1",
			Title:     "Test Signal",
			Category:  "command",
		},
	}

	strategy := &mockStrategy{
		name:        "mock",
		description: "Mock strategy for testing",
		detectFunc: func(specs []*domain.Spec, documented *READMEDocumented) []SignalCandidate {
			return expectedCandidates
		},
	}

	specs := []*domain.Spec{{ID: "test"}}
	documented := &READMEDocumented{}

	candidates := strategy.Detect(specs, documented)

	if len(candidates) != 1 {
		t.Fatalf("expected 1 candidate, got %d", len(candidates))
	}

	if candidates[0].Title != "Test Signal" {
		t.Errorf("Title = %q, want %q", candidates[0].Title, "Test Signal")
	}
}
