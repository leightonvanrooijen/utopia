package readme

import (
	"github.com/leightonvanrooijen/utopia/internal/domain"
)

// READMEDocumented captures what's already documented in the README
type READMEDocumented struct {
	// Commands documented in README (e.g., "utopia cr", "utopia execute")
	Commands []string
	// ArtifactTypes documented (e.g., "ADRs", "Concepts", "Domain")
	ArtifactTypes []string
	// Directories documented in the project structure section
	Directories []string
	// WorkflowSteps documented in "The Loop" section
	WorkflowSteps []string
}

// SignalCandidate represents a potential README documentation signal
type SignalCandidate struct {
	SpecID           string
	FeatureID        string
	Title            string
	Category         string // "command", "artifact", "workflow", "directory"
	Confidence       domain.SignalConfidence
	SuggestedSection string
}

// Strategy defines the interface for detecting README documentation signals.
// Different strategies detect signals in different ways:
// - comparison: compares README content against spec features (default)
// - (future) git-diff: detects changes based on git diff of README
type Strategy interface {
	// Detect scans specs and README to find documentation signals.
	// It receives the parsed README structure and current specs,
	// and returns a list of signals with confidence and suggested sections.
	Detect(specs []*domain.Spec, documented *READMEDocumented) []SignalCandidate

	// Name returns the strategy identifier (e.g., "comparison")
	Name() string

	// Description returns a human-readable description for CLI help
	Description() string
}

// Registry holds available README detection strategies
type Registry struct {
	strategies map[string]Strategy
}

// NewRegistry creates an empty README strategy registry
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
