package spec

import (
	"context"

	"github.com/leightonvanrooijen/utopia/internal/domain"
)

// Strategy defines the interface for spec creation workflows.
// Different strategies can guide users through spec creation in different ways:
// - guided: 5-stage conversational workflow
// - minimal: quick, few questions
// - template: start from predefined templates
// - import: extract specs from existing documentation
type Strategy interface {
	// Run executes the spec creation workflow and returns the resulting spec.
	// The strategy is responsible for:
	// - Interacting with the user (via Claude CLI)
	// - Gathering requirements through conversation
	// - Producing structured spec artifacts
	Run(ctx context.Context, project *domain.Project) (*domain.Spec, error)

	// Name returns the strategy identifier (e.g., "guided", "minimal")
	Name() string

	// Description returns a human-readable description for CLI help
	Description() string
}

// Registry holds available spec strategies
type Registry struct {
	strategies map[string]Strategy
}

// NewRegistry creates an empty strategy registry
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
