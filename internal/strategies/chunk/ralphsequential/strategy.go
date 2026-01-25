package ralphsequential

import (
	"fmt"
	"strings"

	"github.com/leightonvanrooijen/utopia/internal/domain"
)

// DefaultConstraints are applied to all work items unless overridden.
var DefaultConstraints = []string{
	"Do not introduce new abstractions, interfaces, or packages",
	"Do not refactor unrelated code",
	"Architecture is already correct",
}

// VagueTerms are phrases that indicate non-verifiable acceptance criteria.
var VagueTerms = []string{
	"should be good",
	"works well",
	"is nice",
	"looks good",
	"feels right",
	"is clean",
	"is better",
	"is optimal",
	"is fast enough",
	"is reasonable",
}

// Strategy implements the ralph-sequential chunking approach.
// It creates one WorkItem per feature, executed in spec order.
type Strategy struct{}

// New creates a new ralph-sequential strategy.
func New() *Strategy {
	return &Strategy{}
}

// Name returns the strategy identifier.
func (s *Strategy) Name() string {
	return "ralph-sequential"
}

// Description returns a human-readable description for CLI help.
func (s *Strategy) Description() string {
	return "One WorkItem per feature, executed sequentially in spec order"
}

// Chunk transforms a spec into work items.
func (s *Strategy) Chunk(spec *domain.Spec) ([]*domain.WorkItem, error) {
	// Validate before generating any work items
	if err := s.validate(spec); err != nil {
		return nil, err
	}

	workItems := make([]*domain.WorkItem, 0, len(spec.Features))

	for i, feature := range spec.Features {
		workItem := domain.NewWorkItem(
			fmt.Sprintf("%s-%s", spec.ID, feature.ID),
			spec.ID,
			feature.ID,
			feature,
			i, // Order is the position in the spec
		)

		// Build the prompt with task + criteria baked in
		workItem.Prompt = BuildPrompt(feature, nil)

		// Apply constraints (spec-level + defaults, deduplicated)
		workItem.Constraints = s.mergeConstraints(spec)

		workItems = append(workItems, workItem)
	}

	return workItems, nil
}

// validate checks that the spec is suitable for chunking.
func (s *Strategy) validate(spec *domain.Spec) error {
	var errors []string

	for _, feature := range spec.Features {
		// Check for empty acceptance criteria
		if len(feature.AcceptanceCriteria) == 0 {
			errors = append(errors, fmt.Sprintf(
				"feature %q has no acceptance criteria",
				feature.ID,
			))
			continue
		}

		// Check for vague terms in criteria
		for _, criterion := range feature.AcceptanceCriteria {
			for _, vague := range VagueTerms {
				if containsVagueTerm(criterion, vague) {
					errors = append(errors, fmt.Sprintf(
						"feature %q has vague criterion: %q (contains %q)",
						feature.ID, criterion, vague,
					))
				}
			}
		}
	}

	if len(errors) > 0 {
		return &ValidationError{Errors: errors}
	}

	return nil
}

// mergeConstraints combines default constraints with spec-level constraints,
// deduplicating any that appear in both.
func (s *Strategy) mergeConstraints(spec *domain.Spec) []string {
	seen := make(map[string]bool)
	var result []string

	// Add defaults first
	for _, c := range DefaultConstraints {
		normalized := strings.TrimSpace(strings.ToLower(c))
		if !seen[normalized] {
			seen[normalized] = true
			result = append(result, c)
		}
	}

	// Add spec-level constraints (from domain_knowledge that look like constraints)
	// In the future, we could add a dedicated spec.Constraints field
	for _, knowledge := range spec.DomainKnowledge {
		normalized := strings.TrimSpace(strings.ToLower(knowledge))
		if !seen[normalized] {
			seen[normalized] = true
			// Only include if it reads like a constraint (imperative/prohibition)
			if looksLikeConstraint(knowledge) {
				result = append(result, knowledge)
			}
		}
	}

	return result
}

// looksLikeConstraint heuristically checks if a string is a constraint.
func looksLikeConstraint(s string) bool {
	lower := strings.ToLower(s)
	prefixes := []string{
		"do not", "don't", "never", "avoid", "must not",
		"only", "always", "must", "should not",
	}
	for _, prefix := range prefixes {
		if strings.HasPrefix(lower, prefix) {
			return true
		}
	}
	return false
}

// containsVagueTerm checks if a criterion contains a vague term, but ignores
// terms that appear within quotes (as examples in the criterion description).
func containsVagueTerm(criterion, vagueTerm string) bool {
	lower := strings.ToLower(criterion)
	vagueLower := strings.ToLower(vagueTerm)

	// Check if the vague term appears at all
	idx := strings.Index(lower, vagueLower)
	if idx == -1 {
		return false
	}

	// Check if it's within quotes (either single or double)
	// by counting quotes before the match
	beforeMatch := criterion[:idx]
	doubleQuotes := strings.Count(beforeMatch, "\"")
	singleQuotes := strings.Count(beforeMatch, "'")

	// If odd number of quotes, we're inside a quoted string (likely an example)
	if doubleQuotes%2 == 1 || singleQuotes%2 == 1 {
		return false
	}

	return true
}

// ValidationError contains validation failures from spec checking.
type ValidationError struct {
	Errors []string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("spec validation failed:\n  - %s", strings.Join(e.Errors, "\n  - "))
}
