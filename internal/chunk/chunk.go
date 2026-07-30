package chunk

import (
	"bytes"
	"fmt"
	"regexp"
	"strings"
	"text/template"

	"github.com/leightonvanrooijen/utopia/internal/domain"
)

// DefaultConstraints are applied to all work items unless overridden.
var DefaultConstraints = []string{
	"Prefer existing abstractions over creating new ones unless clearly required",
	"Do not refactor unrelated code",
	"Architecture is already correct",
	"Do not modify .utopia/specs/ files - specs are updated automatically when the CR merges",
}

// RefactorSystemConstraints are automatically injected for refactor WorkItems.
// These ensure refactors preserve existing behavior.
var RefactorSystemConstraints = []string{
	"This is a refactor. Existing behavior MUST be preserved.",
	"All existing tests must pass without modification",
	"Do not introduce new abstractions, interfaces, or packages",
}

// BugfixSystemConstraints are automatically injected for bugfix WorkItems.
// These ensure bugfixes correct behavior to match the spec.
var BugfixSystemConstraints = []string{
	"This is a bugfix. The implementation must be corrected to match the spec.",
	"The acceptance criteria below are the source of truth for correct behavior.",
	"Fix only the behavior that deviates from the spec",
}

// SpecLoader is a function that loads a spec by ID.
// This allows the chunking logic to load referenced specs for bugfix tasks
// without being coupled to a specific storage implementation.
type SpecLoader func(specID string) (*domain.Spec, error)

// bugfixFeature wraps a feature extracted from a bugfix task with its spec reference.
// This allows the chunking logic to load the referenced spec feature.
type bugfixFeature struct {
	feature   domain.Feature
	specRef   string // The spec ID to load
	featureID string // The feature ID within that spec
}

// FeatureSplits records how the sizer divided features that exceed the turn
// budget. Keys are feature IDs; each value is the ordered list of slices that
// feature becomes, in the order the sizer returned them. A feature absent from
// the map - or mapped to fewer than two slices - produces exactly one work
// item, unchanged. Pass nil when no sizing ran.
type FeatureSplits map[string][]domain.Feature

// workUnit is one work item's worth of a feature: the feature itself when it fit
// the turn budget, or one slice of a feature the sizer split.
type workUnit struct {
	feature        domain.Feature        // description and criteria this work item covers
	sourceFeature  string                // ID of the feature the unit was derived from
	criteriaOrigin domain.CriteriaOrigin // where a split unit's criteria came from; empty when unsplit
}

// Chunk transforms a change request into work items.
// For bugfix CRs that reference specs, specLoader must be provided.
// The standards index (frontmatter metadata of .utopia/standards/ docs) is
// injected into every work item prompt; pass nil when no standards exist.
// splits carries the sizer's verdicts; pass nil for one work item per feature.
func Chunk(cr *domain.ChangeRequest, specLoader SpecLoader, standards []domain.StandardsDocMeta, splits FeatureSplits) ([]*domain.WorkItem, error) {
	// Extract features from the CR
	features, bugfixRefs := extractFeatures(cr)

	// Determine CR type for constraint injection
	isRefactor := cr.Type == domain.CRTypeRefactor
	isBugfix := cr.Type == domain.CRTypeBugfix

	// For bugfix CRs, validate and load referenced spec features
	var refFeatures map[string]*domain.Feature
	if isBugfix && len(bugfixRefs) > 0 {
		var err error
		refFeatures, err = loadReferencedFeatures(bugfixRefs, specLoader)
		if err != nil {
			return nil, err
		}
	}

	// Validate before generating any work items
	if err := validateFeatures(features); err != nil {
		return nil, err
	}

	return generateWorkItems(expandFeatures(features, splits), cr.ID, isRefactor, isBugfix, refFeatures, standards), nil
}

// ChunkPhase transforms a single phase of an initiative CR into work items.
// For bugfix phases that reference specs, specLoader must be provided.
// The standards index is injected into every work item prompt; pass nil
// when no standards exist.
// splits carries the sizer's verdicts; pass nil for one work item per feature.
func ChunkPhase(crID string, phaseIndex int, phase *domain.Phase, specLoader SpecLoader, standards []domain.StandardsDocMeta, splits FeatureSplits) ([]*domain.WorkItem, error) {
	// Extract features from the phase
	features, bugfixRefs := extractFeaturesFromPhase(phase)

	// Determine phase type for constraint injection
	isRefactor := phase.Type == domain.CRTypeRefactor
	isBugfix := phase.Type == domain.CRTypeBugfix

	// For bugfix phases, validate and load referenced spec features
	var refFeatures map[string]*domain.Feature
	if isBugfix && len(bugfixRefs) > 0 {
		var err error
		refFeatures, err = loadReferencedFeatures(bugfixRefs, specLoader)
		if err != nil {
			return nil, err
		}
	}

	// Validate before generating any work items
	if err := validateFeatures(features); err != nil {
		return nil, err
	}

	phaseWorkItemPrefix := fmt.Sprintf("%s-phase-%d", crID, phaseIndex)

	return generateWorkItems(expandFeatures(features, splits), phaseWorkItemPrefix, isRefactor, isBugfix, refFeatures, standards), nil
}

// expandFeatures flattens features into the units work items are generated from.
// Features are walked in spec order, and a split feature's units are emitted
// adjacently in the order the sizer returned them, so a running position over
// the result stays contiguous and sequential while preserving spec order.
//
// Unit identity is owned here rather than taken from the sizer: a split unit is
// named <featureID>-<n>, 1-based, which keeps work item IDs unique and every
// unit traceable back to the feature it came from.
func expandFeatures(features []domain.Feature, splits FeatureSplits) []workUnit {
	units := make([]workUnit, 0, len(features))

	for _, feature := range features {
		slices := splits[feature.ID]
		if len(slices) < 2 {
			// Not split: the feature is one unit, unchanged.
			units = append(units, workUnit{feature: feature, sourceFeature: feature.ID})
			continue
		}

		// Criteria attribution is derived from the slices rather than declared
		// alongside them, so the label and the criteria it describes cannot
		// disagree: a split whose slices account for every criterion of the feature
		// exactly once is a partition, and anything else is the sizer's own writing.
		origin := domain.CriteriaOriginAuthored
		if isPartitionOf(feature.AcceptanceCriteria, slices) {
			origin = domain.CriteriaOriginPartitioned
		}

		for n, slice := range slices {
			slice.ID = fmt.Sprintf("%s-%d", feature.ID, n+1)
			if len(slice.Hints) == 0 {
				slice.Hints = feature.Hints
			}
			units = append(units, workUnit{feature: slice, sourceFeature: feature.ID, criteriaOrigin: origin})
		}
	}

	return units
}

// generateWorkItems builds one work item per unit, assigning Order from the
// unit's position so order is contiguous and sequential across split features.
// idPrefix prefixes work item IDs and is the SpecRef the items are keyed to.
func generateWorkItems(units []workUnit, idPrefix string, isRefactor, isBugfix bool, refFeatures map[string]*domain.Feature, standards []domain.StandardsDocMeta) []*domain.WorkItem {
	workItems := make([]*domain.WorkItem, 0, len(units))

	for i, unit := range units {
		workItem := domain.NewWorkItem(
			fmt.Sprintf("%s-%s", idPrefix, unit.feature.ID),
			idPrefix,
			unit.feature.ID,
			unit.feature,
			i, // Order is the position in the expanded unit list
		)

		// A split unit's ID is the slice's, so the source feature has to be restated
		// here; for an unsplit unit the two are the same and this is a no-op.
		workItem.SourceFeatureID = unit.sourceFeature
		workItem.CriteriaOrigin = unit.criteriaOrigin

		// Apply constraints (defaults + type-specific constraints)
		workItem.Constraints = mergeConstraintsForCRType(isRefactor, isBugfix)

		// Build the prompt with task + criteria + constraints baked in.
		// For bugfix items, include the referenced feature for the REFERENCE
		// section - keyed to the source feature, which a split unit shares.
		var refFeature *domain.Feature
		if isBugfix && refFeatures != nil {
			refFeature = refFeatures[unit.sourceFeature]
		}
		workItem.Prompt = BuildPromptWithConstraints(unit.feature, workItem.Constraints, nil, refFeature, unit.feature.Hints, standards)

		workItems = append(workItems, workItem)
	}

	return workItems
}

// extractFeatures converts CR tasks and changes into a flat list of features for chunking.
// For bugfix CRs, the returned bugfixRefs map contains spec/feature references keyed by task ID.
func extractFeatures(cr *domain.ChangeRequest) ([]domain.Feature, map[string]bugfixFeature) {
	var features []domain.Feature
	bugfixRefs := make(map[string]bugfixFeature)

	// Convert tasks to features (supported on any CR type)
	for _, task := range cr.Tasks {
		feature, ref := convertTaskToFeature(task)
		features = append(features, feature)
		if ref != nil {
			bugfixRefs[task.ID] = *ref
		}
	}

	// Convert changes to features
	for _, change := range cr.Changes {
		if feature := convertChangeToFeature(change); feature != nil {
			features = append(features, *feature)
		}
	}

	return features, bugfixRefs
}

// extractFeaturesFromPhase converts phase tasks and changes into a flat list of features.
// For bugfix phases, the returned bugfixRefs map contains spec/feature references keyed by task ID.
func extractFeaturesFromPhase(phase *domain.Phase) ([]domain.Feature, map[string]bugfixFeature) {
	var features []domain.Feature
	bugfixRefs := make(map[string]bugfixFeature)

	// Convert tasks to features (supported on any phase type)
	for _, task := range phase.Tasks {
		feature, ref := convertTaskToFeature(task)
		features = append(features, feature)
		if ref != nil {
			bugfixRefs[task.ID] = *ref
		}
	}

	// Convert changes to features
	for _, change := range phase.Changes {
		if feature := convertChangeToFeature(change); feature != nil {
			features = append(features, *feature)
		}
	}

	return features, bugfixRefs
}

// SpecForFeature returns the id of the spec the change or task behind featureID
// targets, or "" when nothing in changes or tasks produced that feature.
//
// It exists because a work item's SpecRef is keyed to the change request, not to
// a spec - the feature id a work item carries is derived here, and only the
// change that produced it knows which spec it lands in. A caller that needs the
// real spec (scoping escalation, which quotes it to the scoper) asks here rather
// than re-deriving the feature-id rules and drifting from them.
func SpecForFeature(changes []domain.Change, tasks []domain.Task, featureID string) string {
	for _, task := range tasks {
		if task.ID == featureID {
			return task.Spec
		}
	}
	for _, change := range changes {
		if feature := convertChangeToFeature(change); feature != nil && feature.ID == featureID {
			return change.Spec
		}
	}
	return ""
}

// convertTaskToFeature converts a Task to a Feature and optionally tracks bugfix references.
// If the task has Spec and FeatureID set, it returns a bugfixFeature for reference loading.
// Task hints are preserved in the returned Feature for injection into work item prompts.
func convertTaskToFeature(task domain.Task) (domain.Feature, *bugfixFeature) {
	feature := domain.Feature{
		ID:                 task.ID,
		Description:        task.Description,
		AcceptanceCriteria: task.AcceptanceCriteria,
		Hints:              task.Hints,
	}

	// For bugfix tasks, track the spec/feature reference
	if task.Spec != "" && task.FeatureID != "" {
		return feature, &bugfixFeature{
			feature:   feature,
			specRef:   task.Spec,
			featureID: task.FeatureID,
		}
	}

	return feature, nil
}

// convertChangeToFeature converts a Change to a Feature based on its operation type.
// Returns nil if the change doesn't produce a feature (e.g., add without feature).
func convertChangeToFeature(change domain.Change) *domain.Feature {
	switch change.Operation {
	case "add":
		if change.Feature != nil {
			return change.Feature
		}
		// Ignore add operations with only domain knowledge
		return nil

	case "remove":
		if change.FeatureID == "" {
			return nil
		}
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
		return &feature

	case "modify":
		if change.FeatureID == "" {
			return nil
		}
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
		return &feature

	case "delete-spec":
		if change.Spec == "" {
			return nil
		}
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
		return &feature
	}

	return nil
}

// validateFeatures checks that the features extracted from a CR are suitable for chunking.
func validateFeatures(features []domain.Feature) error {
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
func loadReferencedFeatures(bugfixRefs map[string]bugfixFeature, specLoader SpecLoader) (map[string]*domain.Feature, error) {
	if specLoader == nil {
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
			spec, err = specLoader(ref.specRef)
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
func mergeConstraintsForCRType(isRefactor, isBugfix bool) []string {
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

// ValidationError contains validation failures from spec checking.
type ValidationError struct {
	Errors []string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("spec validation failed:\n  - %s", strings.Join(e.Errors, "\n  - "))
}

// PromptTemplate is the minimal template for Ralph execution.
// It uses {{handlebars}} style syntax.
const PromptTemplate = `## TASK

{{.Task}}
{{if .Reference}}

## REFERENCE

The following spec feature defines the correct behavior:

{{.Reference}}
{{end}}
{{if .Hints}}

## HINTS

{{range .Hints}}- {{.}}
{{end}}
{{end}}
{{if .Standards}}
## STANDARDS

{{.Standards}}

{{end}}
## CONSTRAINTS

{{range .Constraints}}- {{.}}
{{end}}
{{if .PreviousFailures}}
## PREVIOUS FAILURES

The previous attempt failed with the following output:

{{.PreviousFailures}}

Please address these failures in your implementation.
{{end}}
---

When complete, commit your changes and output: <COMPLETE>`

// PromptData holds the data for rendering the prompt template.
type PromptData struct {
	Task             string
	Reference        string // Optional: for bugfix items, the referenced spec feature content
	Hints            []string
	Standards        string // Optional: index of standards docs the executor reads on demand
	Constraints      []string
	PreviousFailures string
}

// BuildPromptWithConstraints creates a prompt with custom constraints.
// For bugfix items, refFeature contains the spec feature that defines correct behavior.
// Hints provide ephemeral implementation guidance and appear in a HINTS section after TASK.
// Standards metadata is rendered as a STANDARDS index section; the executor
// reads the full docs it judges relevant before implementing.
func BuildPromptWithConstraints(feature domain.Feature, constraints []string, failures []string, refFeature *domain.Feature, hints []string, standards []domain.StandardsDocMeta) string {
	task := buildTaskWithCriteria(feature)

	data := PromptData{
		Task:        task,
		Hints:       hints,
		Constraints: constraints,
	}

	if len(standards) > 0 {
		data.Standards = buildStandardsSection(standards)
	}

	// For bugfix items, include the referenced feature content
	if refFeature != nil {
		data.Reference = buildReferenceSection(refFeature)
	}

	if len(failures) > 0 {
		data.PreviousFailures = strings.Join(failures, "\n\n")
	}

	return renderTemplate(data)
}

// buildReferenceSection formats a spec feature for the REFERENCE section.
func buildReferenceSection(feature *domain.Feature) string {
	var sb strings.Builder

	sb.WriteString(feature.Description)
	sb.WriteString("\n\n")

	sb.WriteString("Acceptance criteria:\n")
	for _, criterion := range feature.AcceptanceCriteria {
		sb.WriteString("- ")
		sb.WriteString(criterion)
		sb.WriteString("\n")
	}

	return strings.TrimSpace(sb.String())
}

// buildStandardsSection formats the standards index for the STANDARDS section.
// Only frontmatter metadata is included - never full doc content. The executor
// reads the docs it judges relevant on demand.
func buildStandardsSection(standards []domain.StandardsDocMeta) string {
	var sb strings.Builder

	sb.WriteString("This project defines coding standards in .utopia/standards/. ")
	sb.WriteString("Before implementing, read the standards docs relevant to the files this task touches:\n\n")

	for _, doc := range standards {
		sb.WriteString(fmt.Sprintf("- id: %s\n", doc.ID))
		sb.WriteString(fmt.Sprintf("  description: %s\n", doc.Description))
		if len(doc.Tags) > 0 {
			sb.WriteString(fmt.Sprintf("  tags: %s\n", strings.Join(doc.Tags, ", ")))
		}
		sb.WriteString(fmt.Sprintf("  path: %s\n", doc.Path))
	}

	return strings.TrimSpace(sb.String())
}

// buildTaskWithCriteria merges feature description with acceptance criteria
// into a single TASK block.
func buildTaskWithCriteria(feature domain.Feature) string {
	var sb strings.Builder

	// Feature description becomes the task headline
	sb.WriteString(feature.Description)
	sb.WriteString("\n\n")

	// Acceptance criteria are listed as bullet points
	sb.WriteString("Acceptance criteria:\n")
	for _, criterion := range feature.AcceptanceCriteria {
		sb.WriteString("- ")
		sb.WriteString(criterion)
		sb.WriteString("\n")
	}

	return strings.TrimSpace(sb.String())
}

// renderTemplate executes the prompt template with the given data.
func renderTemplate(data PromptData) string {
	// Escape any template syntax in user content
	data.Task = escapeTemplateContent(data.Task)
	data.Reference = escapeTemplateContent(data.Reference)
	data.Standards = escapeTemplateContent(data.Standards)
	data.PreviousFailures = escapeTemplateContent(data.PreviousFailures)
	for i, hint := range data.Hints {
		data.Hints[i] = escapeTemplateContent(hint)
	}

	tmpl, err := template.New("prompt").Parse(PromptTemplate)
	if err != nil {
		// This should never happen with a valid template
		panic("invalid prompt template: " + err.Error())
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		// This should never happen with valid data
		panic("failed to execute template: " + err.Error())
	}

	return buf.String()
}

// escapeTemplateContent escapes Go template syntax in user-provided content.
// This prevents user content from being interpreted as template directives.
func escapeTemplateContent(s string) string {
	if s == "" {
		return s
	}
	// Escape {{ and }} to prevent template injection
	re := regexp.MustCompile(`\{\{|\}\}`)
	return re.ReplaceAllStringFunc(s, func(match string) string {
		if match == "{{" {
			return "{ {"
		}
		return "} }"
	})
}
