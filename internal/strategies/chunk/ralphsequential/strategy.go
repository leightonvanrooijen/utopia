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
	"The acceptance criteria below are the source of truth for correct behavior.",
	"Fix only the behavior that deviates from the spec",
}

// SpecLoader is a function that loads a spec by ID.
// This allows the chunking strategy to load referenced specs for bugfix tasks
// without being coupled to a specific storage implementation.
type SpecLoader func(specID string) (*domain.Spec, error)

// bugfixFeature wraps a feature extracted from a bugfix task with its spec reference.
// This allows the chunking strategy to load the referenced spec feature.
type bugfixFeature struct {
	feature   domain.Feature
	specRef   string // The spec ID to load
	featureID string // The feature ID within that spec
}

// Strategy implements the ralph-sequential chunking approach.
// It creates one WorkItem per feature, executed in spec order.
type Strategy struct {
	specLoader    SpecLoader
	domainContext string // Ubiquitous language guidance from domain docs
}

// New creates a new ralph-sequential strategy.
// The specLoader is optional - if nil, use SetSpecLoader before chunking bugfix CRs.
func New(specLoader SpecLoader) *Strategy {
	return &Strategy{specLoader: specLoader}
}

// SetSpecLoader sets the spec loader for loading referenced specs during bugfix chunking.
// This allows the spec loader to be set after strategy creation when the storage becomes available.
// Note: Uses anonymous function type to match SpecLoaderConfigurable interface in execute.go.
func (s *Strategy) SetSpecLoader(loader func(specID string) (*domain.Spec, error)) {
	s.specLoader = loader
}

// SetDomainContext sets the ubiquitous language guidance from domain docs.
// This context is injected into work item prompts to ensure Claude uses consistent terminology.
func (s *Strategy) SetDomainContext(context string) {
	s.domainContext = context
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
	features, bugfixRefs := s.extractFeatures(cr)

	// Determine CR type for constraint injection
	isRefactor := cr.Type == domain.CRTypeRefactor
	isBugfix := cr.Type == domain.CRTypeBugfix

	// For bugfix CRs, validate and load referenced spec features
	var refFeatures map[string]*domain.Feature
	if isBugfix && len(bugfixRefs) > 0 {
		var err error
		refFeatures, err = s.loadReferencedFeatures(bugfixRefs)
		if err != nil {
			return nil, err
		}
	}

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
		// For bugfix items, include the referenced feature for the REFERENCE section
		var refFeature *domain.Feature
		if isBugfix && refFeatures != nil {
			refFeature = refFeatures[feature.ID]
		}
		workItem.Prompt = BuildPromptWithConstraints(feature, workItem.Constraints, nil, refFeature, s.domainContext)

		workItems = append(workItems, workItem)
	}

	return workItems, nil
}

// extractFeatures converts CR tasks and changes into a flat list of features for chunking.
// For bugfix CRs, the returned bugfixRefs map contains spec/feature references keyed by task ID.
func (s *Strategy) extractFeatures(cr *domain.ChangeRequest) ([]domain.Feature, map[string]bugfixFeature) {
	var features []domain.Feature
	bugfixRefs := make(map[string]bugfixFeature)

	// Convert tasks to features (supported on any CR type)
	for _, task := range cr.Tasks {
		feature := domain.Feature{
			ID:                 task.ID,
			Description:        task.Description,
			AcceptanceCriteria: task.AcceptanceCriteria,
		}
		features = append(features, feature)

		// For bugfix tasks, track the spec/feature reference
		if task.Spec != "" && task.FeatureID != "" {
			bugfixRefs[task.ID] = bugfixFeature{
				feature:   feature,
				specRef:   task.Spec,
				featureID: task.FeatureID,
			}
		}
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
					criteria = append(criteria, change.Criteria.Add...)
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

	return features, bugfixRefs
}

// ChunkPhase transforms a single phase of an initiative CR into work items.
func (s *Strategy) ChunkPhase(crID string, phaseIndex int, phase *domain.Phase) ([]*domain.WorkItem, error) {
	// Extract features from the phase
	features, bugfixRefs := s.extractFeaturesFromPhase(phase)

	// Determine phase type for constraint injection
	isRefactor := phase.Type == domain.CRTypeRefactor
	isBugfix := phase.Type == domain.CRTypeBugfix

	// For bugfix phases, validate and load referenced spec features
	var refFeatures map[string]*domain.Feature
	if isBugfix && len(bugfixRefs) > 0 {
		var err error
		refFeatures, err = s.loadReferencedFeatures(bugfixRefs)
		if err != nil {
			return nil, err
		}
	}

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
		// For bugfix items, include the referenced feature for the REFERENCE section
		var refFeature *domain.Feature
		if isBugfix && refFeatures != nil {
			refFeature = refFeatures[feature.ID]
		}
		workItem.Prompt = BuildPromptWithConstraints(feature, workItem.Constraints, nil, refFeature, s.domainContext)

		workItems = append(workItems, workItem)
	}

	return workItems, nil
}

// extractFeaturesFromPhase converts phase tasks and changes into a flat list of features.
// For bugfix phases, the returned bugfixRefs map contains spec/feature references keyed by task ID.
func (s *Strategy) extractFeaturesFromPhase(phase *domain.Phase) ([]domain.Feature, map[string]bugfixFeature) {
	var features []domain.Feature
	bugfixRefs := make(map[string]bugfixFeature)

	// Convert tasks to features (supported on any phase type)
	for _, task := range phase.Tasks {
		feature := domain.Feature{
			ID:                 task.ID,
			Description:        task.Description,
			AcceptanceCriteria: task.AcceptanceCriteria,
		}
		features = append(features, feature)

		// For bugfix tasks, track the spec/feature reference
		if task.Spec != "" && task.FeatureID != "" {
			bugfixRefs[task.ID] = bugfixFeature{
				feature:   feature,
				specRef:   task.Spec,
				featureID: task.FeatureID,
			}
		}
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
					criteria = append(criteria, change.Criteria.Add...)
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

	return features, bugfixRefs
}

// validateFeatures checks that the features extracted from a CR are suitable for chunking.
func (s *Strategy) validateFeatures(features []domain.Feature) error {
	var errors []string

	for _, feature := range features {
		if len(feature.AcceptanceCriteria) == 0 {
			errors = append(errors, fmt.Sprintf(
				"feature %q has no acceptance criteria",
				feature.ID,
			))
		}
	}

	if len(errors) > 0 {
		return &ValidationError{Errors: errors}
	}

	return nil
}

// loadReferencedFeatures loads the spec features referenced by bugfix tasks.
// Returns a map from task ID to the referenced feature from the spec.
// Fails with a clear error if the spec or feature is not found.
func (s *Strategy) loadReferencedFeatures(bugfixRefs map[string]bugfixFeature) (map[string]*domain.Feature, error) {
	if s.specLoader == nil {
		return nil, fmt.Errorf("bugfix CR references specs but no spec loader is configured")
	}

	result := make(map[string]*domain.Feature)
	// Cache loaded specs to avoid reloading the same spec multiple times
	specCache := make(map[string]*domain.Spec)

	for taskID, ref := range bugfixRefs {
		// Load spec (with caching)
		spec, ok := specCache[ref.specRef]
		if !ok {
			var err error
			spec, err = s.specLoader(ref.specRef)
			if err != nil {
				return nil, fmt.Errorf("bugfix task %q references spec %q which could not be loaded: %w", taskID, ref.specRef, err)
			}
			if spec == nil {
				return nil, fmt.Errorf("bugfix task %q references spec %q which was not found", taskID, ref.specRef)
			}
			specCache[ref.specRef] = spec
		}

		// Find the feature in the spec
		var foundFeature *domain.Feature
		for i := range spec.Features {
			if spec.Features[i].ID == ref.featureID {
				foundFeature = &spec.Features[i]
				break
			}
		}

		if foundFeature == nil {
			return nil, fmt.Errorf("bugfix task %q references feature %q in spec %q but feature not found", taskID, ref.featureID, ref.specRef)
		}

		result[taskID] = foundFeature
	}

	return result, nil
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

// ValidationError contains validation failures from spec checking.
type ValidationError struct {
	Errors []string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("spec validation failed:\n  - %s", strings.Join(e.Errors, "\n  - "))
}
