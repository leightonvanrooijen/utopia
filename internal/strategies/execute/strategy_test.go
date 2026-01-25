package execute

import (
	"context"
	"testing"

	"github.com/leightonvanrooijen/utopia/internal/domain"
	"github.com/leightonvanrooijen/utopia/internal/infra/storage"
)

// mockStrategy is a test double for Strategy interface
type mockStrategy struct {
	name        string
	description string
	executeFunc func(ctx context.Context, specID string, store *storage.YAMLStore, config *domain.Config, projectDir string) (*Result, error)
}

func (m *mockStrategy) Name() string {
	return m.name
}

func (m *mockStrategy) Description() string {
	return m.description
}

func (m *mockStrategy) Execute(ctx context.Context, specID string, store *storage.YAMLStore, config *domain.Config, projectDir string) (*Result, error) {
	if m.executeFunc != nil {
		return m.executeFunc(ctx, specID, store, config, projectDir)
	}
	return &Result{}, nil
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

func TestResult_Fields(t *testing.T) {
	result := &Result{
		Completed: 5,
		Total:     10,
		StoppedAt: "work-item-6",
		Reason:    "max iterations reached",
	}

	if result.Completed != 5 {
		t.Errorf("Completed = %d, want %d", result.Completed, 5)
	}

	if result.Total != 10 {
		t.Errorf("Total = %d, want %d", result.Total, 10)
	}

	if result.StoppedAt != "work-item-6" {
		t.Errorf("StoppedAt = %q, want %q", result.StoppedAt, "work-item-6")
	}

	if result.Reason != "max iterations reached" {
		t.Errorf("Reason = %q, want %q", result.Reason, "max iterations reached")
	}
}
