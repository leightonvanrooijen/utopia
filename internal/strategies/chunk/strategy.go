package chunk

import (
	"github.com/leightonvanrooijen/utopia/internal/domain"
)

// Strategy defines the interface for chunking change requests into work items.
// Different strategies transform CRs into work items in different ways:
// - ralph-sequential: one WorkItem per feature/task, executed in CR order
// - (future) parallel: work items that can execute concurrently
// - (future) atomic: smaller, more granular work items
type Strategy interface {
	// Chunk transforms a change request into a slice of work items.
	// The strategy is responsible for:
	// - Validating the CR is chunkable
	// - Creating work items with appropriate prompts
	// - Setting execution order and constraints
	Chunk(cr *domain.ChangeRequest) ([]*domain.WorkItem, error)

	// ChunkPhase transforms a single phase of an initiative CR into work items.
	// The crID and phaseIndex are used to generate unique work item IDs.
	ChunkPhase(crID string, phaseIndex int, phase *domain.Phase) ([]*domain.WorkItem, error)

	// Name returns the strategy identifier (e.g., "ralph-sequential")
	Name() string

	// Description returns a human-readable description for CLI help
	Description() string
}

// Registry holds available chunk strategies
type Registry struct {
	strategies map[string]Strategy
}

// NewRegistry creates an empty chunk strategy registry
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
