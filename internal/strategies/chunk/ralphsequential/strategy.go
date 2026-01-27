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

// RefactorSystemConstraints are automatically injected for refactor WorkItems.
// These ensure refactors preserve existing behavior.
var RefactorSystemConstraints = []string{
	"This is a refactor. Existing behavior MUST be preserved.",
	"All existing tests must pass without modification",
}

// BugfixSystemConstraints are automatically injected for bugfix WorkItems.
// These ensure bugfixes correct behavior to match the spec.
var BugfixSystemConstraints = []string{
	"This is a bugfix. The implementation must be corrected to match the spec.",
	"Fix only the behavior that deviates from the spec",
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

// Chunk transforms a change request into work items.
func (s *Strategy) Chunk(cr *domain.ChangeRequest) ([]*domain.WorkItem, error) {
	// Extract features from the CR
	features := s.extractFeatures(cr)

	// Determine CR type for constraint injection
	isRefactor := cr.Type == domain.CRTypeRefactor
	isBugfix := cr.Type == domain.CRTypeBugfix

	// Validate before generating any work items
	if err := s.validateFeatures(features); err != nil {
		return nil, err
	}

	workItems := make([]*domain.WorkItem, 0, len(features))

	for i, feature := range features {
		workItem := domain.NewWorkItem(
			fmt.Sprintf("%s-%s", cr.ID, feature.ID),
			cr.ID,
			feature.ID,
			feature,
			i, // Order is the position in the CR
		)

		// Apply constraints (defaults + type-specific constraints)
		workItem.Constraints = s.mergeConstraintsForCRType(isRefactor, isBugfix)

		// Build the prompt with task + criteria + constraints baked in
		workItem.Prompt = BuildPromptWithConstraints(feature, workItem.Constraints, nil)

		workItems = append(workItems, workItem)
	}

	return workItems, nil
}

// extractFeatures converts CR tasks and changes into a flat list of features for chunking.
func (s *Strategy) extractFeatures(cr *domain.ChangeRequest) []domain.Feature {
	var features []domain.Feature

	// Convert tasks to features (supported on any CR type)
	for _, task := range cr.Tasks {
		feature := domain.Feature{
			ID:                 task.ID,
			Description:        task.Description,
			AcceptanceCriteria: task.AcceptanceCriteria,
		}
		features = append(features, feature)
	}

	// Convert changes to features
	for _, change := range cr.Changes {
		switch change.Operation {
		case "add":
			if change.Feature != nil {
				features = append(features, *change.Feature)
			}
			// Ignore add operations with only domain knowledge

		case "remove":
			if change.FeatureID != "" {
				feature := domain.Feature{
					ID:          "remove-" + change.FeatureID,
					Description: fmt.Sprintf("Remove the %s feature from the codebase", change.FeatureID),
					AcceptanceCriteria: []string{
						fmt.Sprintf("All code related to feature %q is removed", change.FeatureID),
						fmt.Sprintf("All tests for feature %q are removed", change.FeatureID),
						"No references to the removed feature remain in the codebase",
					},
				}
				if change.Reason != "" {
					feature.AcceptanceCriteria = append(feature.AcceptanceCriteria,
						fmt.Sprintf("Removal reason: %s", change.Reason))
				}
				features = append(features, feature)
			}

		case "modify":
			if change.FeatureID != "" {
				feature := domain.Feature{
					ID:          "modify-" + change.FeatureID,
					Description: fmt.Sprintf("Modify the %s feature", change.FeatureID),
				}

				// Add description change if provided
				if change.Description != "" {
					feature.Description = fmt.Sprintf("Modify the %s feature: %s", change.FeatureID, change.Description)
				}

				// Build acceptance criteria from the deltas
				var criteria []string

				if change.Criteria != nil {
					for _, add := range change.Criteria.Add {
						criteria = append(criteria, add)
					}
					for _, remove := range change.Criteria.Remove {
						criteria = append(criteria, fmt.Sprintf("Remove/undo: %s", remove))
					}
					for _, edit := range change.Criteria.Edit {
						criteria = append(criteria, fmt.Sprintf("Change from %q to: %s", edit.Old, edit.New))
					}
				}

				// Ensure at least one criterion exists
				if len(criteria) == 0 {
					criteria = append(criteria, fmt.Sprintf("Feature %q is updated as specified", change.FeatureID))
				}

				feature.AcceptanceCriteria = criteria
				features = append(features, feature)
			}

		case "delete-spec":
			if change.Spec != "" {
				feature := domain.Feature{
					ID:          "delete-spec-" + change.Spec,
					Description: fmt.Sprintf("Delete the entire %s spec file", change.Spec),
					AcceptanceCriteria: []string{
						fmt.Sprintf("All code implementing features from spec %q is removed", change.Spec),
						fmt.Sprintf("All tests for features from spec %q are removed", change.Spec),
						fmt.Sprintf("The spec file .utopia/specs/%s.yaml is deleted", change.Spec),
					},
				}
				if change.Reason != "" {
					feature.AcceptanceCriteria = append(feature.AcceptanceCriteria,
						fmt.Sprintf("Deletion reason: %s", change.Reason))
				}
				features = append(features, feature)
			}
		}
	}

	return features
}

// ChunkPhase transforms a single phase of an initiative CR into work items.
func (s *Strategy) ChunkPhase(crID string, phaseIndex int, phase *domain.Phase) ([]*domain.WorkItem, error) {
	// Extract features from the phase
	features := s.extractFeaturesFromPhase(phase)

	// Determine phase type for constraint injection
	isRefactor := phase.Type == domain.CRTypeRefactor
	isBugfix := phase.Type == domain.CRTypeBugfix

	// Validate before generating any work items
	if err := s.validateFeatures(features); err != nil {
		return nil, err
	}

	workItems := make([]*domain.WorkItem, 0, len(features))
	phaseWorkItemPrefix := fmt.Sprintf("%s-phase-%d", crID, phaseIndex)

	for i, feature := range features {
		workItem := domain.NewWorkItem(
			fmt.Sprintf("%s-%s", phaseWorkItemPrefix, feature.ID),
			phaseWorkItemPrefix,
			feature.ID,
			feature,
			i, // Order is the position in the phase
		)

		// Apply constraints (defaults + type-specific constraints)
		workItem.Constraints = s.mergeConstraintsForCRType(isRefactor, isBugfix)

		// Build the prompt with task + criteria + constraints baked in
		workItem.Prompt = BuildPromptWithConstraints(feature, workItem.Constraints, nil)

		workItems = append(workItems, workItem)
	}

	return workItems, nil
}

// extractFeaturesFromPhase converts phase tasks and changes into a flat list of features.
func (s *Strategy) extractFeaturesFromPhase(phase *domain.Phase) []domain.Feature {
	var features []domain.Feature

	// Convert tasks to features (supported on any phase type)
	for _, task := range phase.Tasks {
		feature := domain.Feature{
			ID:                 task.ID,
			Description:        task.Description,
			AcceptanceCriteria: task.AcceptanceCriteria,
		}
		features = append(features, feature)
	}

	// Convert changes to features
	for _, change := range phase.Changes {
		switch change.Operation {
		case "add":
			if change.Feature != nil {
				features = append(features, *change.Feature)
			}

		case "remove":
			if change.FeatureID != "" {
				feature := domain.Feature{
					ID:          "remove-" + change.FeatureID,
					Description: fmt.Sprintf("Remove the %s feature from the codebase", change.FeatureID),
					AcceptanceCriteria: []string{
						fmt.Sprintf("All code related to feature %q is removed", change.FeatureID),
						fmt.Sprintf("All tests for feature %q are removed", change.FeatureID),
						"No references to the removed feature remain in the codebase",
					},
				}
				if change.Reason != "" {
					feature.AcceptanceCriteria = append(feature.AcceptanceCriteria,
						fmt.Sprintf("Removal reason: %s", change.Reason))
				}
				features = append(features, feature)
			}

		case "modify":
			if change.FeatureID != "" {
				feature := domain.Feature{
					ID:          "modify-" + change.FeatureID,
					Description: fmt.Sprintf("Modify the %s feature", change.FeatureID),
				}

				if change.Description != "" {
					feature.Description = fmt.Sprintf("Modify the %s feature: %s", change.FeatureID, change.Description)
				}

				var criteria []string
				if change.Criteria != nil {
					for _, add := range change.Criteria.Add {
						criteria = append(criteria, add)
					}
					for _, remove := range change.Criteria.Remove {
						criteria = append(criteria, fmt.Sprintf("Remove/undo: %s", remove))
					}
					for _, edit := range change.Criteria.Edit {
						criteria = append(criteria, fmt.Sprintf("Change from %q to: %s", edit.Old, edit.New))
					}
				}

				if len(criteria) == 0 {
					criteria = append(criteria, fmt.Sprintf("Feature %q is updated as specified", change.FeatureID))
				}

				feature.AcceptanceCriteria = criteria
				features = append(features, feature)
			}

		case "delete-spec":
			if change.Spec != "" {
				feature := domain.Feature{
					ID:          "delete-spec-" + change.Spec,
					Description: fmt.Sprintf("Delete the entire %s spec file", change.Spec),
					AcceptanceCriteria: []string{
						fmt.Sprintf("All code implementing features from spec %q is removed", change.Spec),
						fmt.Sprintf("All tests for features from spec %q are removed", change.Spec),
						fmt.Sprintf("The spec file .utopia/specs/%s.yaml is deleted", change.Spec),
					},
				}
				if change.Reason != "" {
					feature.AcceptanceCriteria = append(feature.AcceptanceCriteria,
						fmt.Sprintf("Deletion reason: %s", change.Reason))
				}
				features = append(features, feature)
			}
		}
	}

	return features
}

// validateFeatures checks that the features extracted from a CR are suitable for chunking.
func (s *Strategy) validateFeatures(features []domain.Feature) error {
	var errors []string

	for _, feature := range features {
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

// mergeConstraintsForCRType combines default constraints, adding type-specific
// system constraints for refactor or bugfix types.
func (s *Strategy) mergeConstraintsForCRType(isRefactor, isBugfix bool) []string {
	seen := make(map[string]bool)
	var result []string

	// Add refactor system constraints first (if applicable)
	if isRefactor {
		for _, c := range RefactorSystemConstraints {
			normalized := strings.TrimSpace(strings.ToLower(c))
			if !seen[normalized] {
				seen[normalized] = true
				result = append(result, c)
			}
		}
	}

	// Add bugfix system constraints (if applicable)
	if isBugfix {
		for _, c := range BugfixSystemConstraints {
			normalized := strings.TrimSpace(strings.ToLower(c))
			if !seen[normalized] {
				seen[normalized] = true
				result = append(result, c)
			}
		}
	}

	// Add defaults
	for _, c := range DefaultConstraints {
		normalized := strings.TrimSpace(strings.ToLower(c))
		if !seen[normalized] {
			seen[normalized] = true
			result = append(result, c)
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
