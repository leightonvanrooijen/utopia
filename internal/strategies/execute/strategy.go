package execute

import (
	"context"

	"github.com/leightonvanrooijen/utopia/internal/domain"
	"github.com/leightonvanrooijen/utopia/internal/infra/storage"
)

// Result represents the outcome of executing work items
type Result struct {
	// Completed is the count of successfully completed work items
	Completed int
	// Total is the total number of work items attempted
	Total int
	// StoppedAt is the ID of the work item where execution stopped (if not all completed)
	StoppedAt string
	// Reason explains why execution stopped (if not all completed)
	Reason string
}

// Strategy defines the interface for executing work items.
// Different strategies execute work items in different ways:
// - sequential: one at a time, in order
// - (future) parallel: concurrent execution where dependencies allow
// - (future) supervised: human approval between items
type Strategy interface {
	// Execute runs work items for the given spec.
	// The strategy is responsible for:
	// - Loading work items from storage
	// - Executing them according to its policy
	// - Updating work item status
	// - Handling verification and retries
	Execute(ctx context.Context, specID string, store *storage.YAMLStore, config *domain.Config, projectDir string) (*Result, error)

	// Name returns the strategy identifier (e.g., "sequential")
	Name() string

	// Description returns a human-readable description for CLI help
	Description() string
}

// Registry holds available execute strategies
type Registry struct {
	strategies map[string]Strategy
}

// NewRegistry creates an empty execute strategy registry
func NewRegistry() *Registry {
	return &Registry{
		strategies: make(map[string]Strategy),
	}
}

// Register adds a strategy to the registry
func (r *Registry) Register(s Strategy) {
	r.strategies[s.Name()] = s
}

// Get retrieves a strategy by name
func (r *Registry) Get(name string) (Strategy, bool) {
	s, ok := r.strategies[name]
	return s, ok
}

// List returns all registered strategy names
func (r *Registry) List() []string {
	names := make([]string, 0, len(r.strategies))
	for name := range r.strategies {
		names = append(names, name)
	}
	return names
}
