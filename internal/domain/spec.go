package domain

import (
	"fmt"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Status represents the lifecycle state of a spec
type Status string

const (
	StatusDraft    Status = "draft"
	StatusReview   Status = "review"
	StatusApproved Status = "approved"
)

// Spec represents a living specification document
type Spec struct {
	ID          string    `yaml:"id"`
	Title       string    `yaml:"title"`
	Status      Status    `yaml:"status"`
	Created     time.Time `yaml:"created"`
	Updated     time.Time `yaml:"updated"`
	Description string    `yaml:"description"`

	// Domain knowledge captured during exploration
	DomainKnowledge []string `yaml:"domain_knowledge,omitempty"`

	// Features with their acceptance criteria
	Features []Feature `yaml:"features"`
}

// Feature represents a capability of the system
type Feature struct {
	ID                 string   `yaml:"id"`
	Description        string   `yaml:"description"`
	AcceptanceCriteria []string `yaml:"acceptance_criteria"`
}

// MarshalYAML customizes YAML output for Feature to use block style
// for multi-line descriptions.
func (f Feature) MarshalYAML() (interface{}, error) {
	// Create a node structure manually to control formatting
	node := &yaml.Node{
		Kind: yaml.MappingNode,
	}

	// Add id
	node.Content = append(node.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Value: "id"},
		&yaml.Node{Kind: yaml.ScalarNode, Value: f.ID},
	)

	// Add description with block style if multi-line
	descNode := &yaml.Node{Kind: yaml.ScalarNode, Value: f.Description}
	if strings.Contains(f.Description, "\n") || len(f.Description) > 60 {
		descNode.Style = yaml.LiteralStyle // Forces | block style
	}
	node.Content = append(node.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Value: "description"},
		descNode,
	)

	// Add acceptance_criteria
	criteriaNode := &yaml.Node{Kind: yaml.SequenceNode}
	for _, c := range f.AcceptanceCriteria {
		criteriaNode.Content = append(criteriaNode.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Value: c},
		)
	}
	node.Content = append(node.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Value: "acceptance_criteria"},
		criteriaNode,
	)

	return node, nil
}

// NewSpec creates a new spec with sensible defaults
func NewSpec(id, title string) *Spec {
	now := time.Now()
	return &Spec{
		ID:       id,
		Title:    title,
		Status:   StatusDraft,
		Created:  now,
		Updated:  now,
		Features: []Feature{},
	}
}

// AddFeature adds a feature to the spec
func (s *Spec) AddFeature(f Feature) {
	s.Features = append(s.Features, f)
	s.Updated = time.Now()
}

// AddDomainKnowledge adds domain knowledge to the spec
func (s *Spec) AddDomainKnowledge(knowledge string) {
	s.DomainKnowledge = append(s.DomainKnowledge, knowledge)
	s.Updated = time.Now()
}

// ChangeRequestStatus represents the lifecycle state of a change request
type ChangeRequestStatus string

const (
	ChangeRequestDraft      ChangeRequestStatus = "draft"
	ChangeRequestApproved   ChangeRequestStatus = "approved"
	ChangeRequestInProgress ChangeRequestStatus = "in-progress"
	ChangeRequestComplete   ChangeRequestStatus = "complete"
)

// ChangeRequest represents a set of changes to apply to a parent spec
type ChangeRequest struct {
	ID         string              `yaml:"id"`
	Type       string              `yaml:"type"` // Always "change-request"
	ParentSpec string              `yaml:"parent_spec"`
	Title      string              `yaml:"title"`
	Status     ChangeRequestStatus `yaml:"status"`
	Changes    []Change            `yaml:"changes"`
}

// EditPair represents an old/new pair for edit operations
type EditPair struct {
	Old string `yaml:"old"`
	New string `yaml:"new"`
}

// CriteriaModify represents modifications to acceptance criteria
type CriteriaModify struct {
	Add    []string   `yaml:"add,omitempty"`
	Remove []string   `yaml:"remove,omitempty"`
	Edit   []EditPair `yaml:"edit,omitempty"`
}

// DomainKnowledgeModify represents modifications to domain knowledge
type DomainKnowledgeModify struct {
	Add    []string   `yaml:"add,omitempty"`
	Remove []string   `yaml:"remove,omitempty"`
	Edit   []EditPair `yaml:"edit,omitempty"`
}

// Change represents a single operation in a change request
type Change struct {
	Operation       string   `yaml:"operation"` // "add", "modify", "remove"
	Feature         *Feature `yaml:"feature,omitempty"`
	DomainKnowledge []string `yaml:"domain_knowledge,omitempty"`
	// For modify/remove operations
	FeatureID             string                 `yaml:"feature_id,omitempty"`
	Description           string                 `yaml:"description,omitempty"`
	Criteria              *CriteriaModify        `yaml:"criteria,omitempty"`
	DomainKnowledgeMod    *DomainKnowledgeModify `yaml:"domain_knowledge_mod,omitempty"`
	Reason                string                 `yaml:"reason,omitempty"`
}

// ApplyAddChange applies an "add" operation to the spec
// Returns an error if:
// - The operation is not "add"
// - A feature ID already exists in the spec
// - Domain knowledge already exists in the spec
func (s *Spec) ApplyAddChange(change Change) error {
	if change.Operation != "add" {
		return fmt.Errorf("expected 'add' operation, got '%s'", change.Operation)
	}

	// Add feature if present
	if change.Feature != nil {
		if err := s.addFeatureWithValidation(*change.Feature); err != nil {
			return err
		}
	}

	// Add domain knowledge if present
	for _, dk := range change.DomainKnowledge {
		if err := s.addDomainKnowledgeWithValidation(dk); err != nil {
			return err
		}
	}

	return nil
}

// addFeatureWithValidation adds a feature only if its ID is unique
func (s *Spec) addFeatureWithValidation(f Feature) error {
	for _, existing := range s.Features {
		if existing.ID == f.ID {
			return fmt.Errorf("feature with ID '%s' already exists in spec", f.ID)
		}
	}
	s.AddFeature(f)
	return nil
}

// addDomainKnowledgeWithValidation adds domain knowledge only if not duplicate
func (s *Spec) addDomainKnowledgeWithValidation(knowledge string) error {
	for _, existing := range s.DomainKnowledge {
		if existing == knowledge {
			return fmt.Errorf("domain knowledge already exists: %s", knowledge)
		}
	}
	s.AddDomainKnowledge(knowledge)
	return nil
}

// HasFeature checks if a feature with the given ID exists
func (s *Spec) HasFeature(id string) bool {
	for _, f := range s.Features {
		if f.ID == id {
			return true
		}
	}
	return false
}

// HasDomainKnowledge checks if the exact domain knowledge string exists
func (s *Spec) HasDomainKnowledge(knowledge string) bool {
	for _, dk := range s.DomainKnowledge {
		if dk == knowledge {
			return true
		}
	}
	return false
}

// ApplyModifyChange applies a "modify" operation to the spec
// Returns an error if:
// - The operation is not "modify"
// - For feature modifications: feature_id doesn't exist
// - For criteria.remove: criterion doesn't match exactly
// - For criteria.edit: old value doesn't match exactly
// - For domain_knowledge.remove: item doesn't match exactly
// - For domain_knowledge.edit: old value doesn't match exactly
func (s *Spec) ApplyModifyChange(change Change) error {
	if change.Operation != "modify" {
		return fmt.Errorf("expected 'modify' operation, got '%s'", change.Operation)
	}

	// Handle feature modifications
	if change.FeatureID != "" {
		if err := s.modifyFeature(change); err != nil {
			return err
		}
	}

	// Handle domain knowledge modifications
	if change.DomainKnowledgeMod != nil {
		if err := s.modifyDomainKnowledge(*change.DomainKnowledgeMod); err != nil {
			return err
		}
	}

	return nil
}

// modifyFeature applies modifications to an existing feature
func (s *Spec) modifyFeature(change Change) error {
	// Find the feature index
	featureIdx := -1
	for i, f := range s.Features {
		if f.ID == change.FeatureID {
			featureIdx = i
			break
		}
	}

	if featureIdx == -1 {
		return fmt.Errorf("feature with ID '%s' not found in spec", change.FeatureID)
	}

	// Update description if provided
	if change.Description != "" {
		s.Features[featureIdx].Description = change.Description
	}

	// Apply criteria modifications if provided
	if change.Criteria != nil {
		if err := s.modifyCriteria(featureIdx, *change.Criteria); err != nil {
			return err
		}
	}

	s.Updated = time.Now()
	return nil
}

// modifyCriteria applies add/remove/edit operations to feature acceptance criteria
func (s *Spec) modifyCriteria(featureIdx int, criteria CriteriaModify) error {
	// Process removals first (before adds to avoid removing newly added items)
	for _, toRemove := range criteria.Remove {
		found := false
		for i, existing := range s.Features[featureIdx].AcceptanceCriteria {
			if existing == toRemove {
				s.Features[featureIdx].AcceptanceCriteria = append(
					s.Features[featureIdx].AcceptanceCriteria[:i],
					s.Features[featureIdx].AcceptanceCriteria[i+1:]...,
				)
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("criterion not found for removal: %s", toRemove)
		}
	}

	// Process edits
	for _, edit := range criteria.Edit {
		found := false
		for i, existing := range s.Features[featureIdx].AcceptanceCriteria {
			if existing == edit.Old {
				s.Features[featureIdx].AcceptanceCriteria[i] = edit.New
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("criterion not found for edit: %s", edit.Old)
		}
	}

	// Process additions last
	s.Features[featureIdx].AcceptanceCriteria = append(
		s.Features[featureIdx].AcceptanceCriteria,
		criteria.Add...,
	)

	return nil
}

// modifyDomainKnowledge applies add/remove/edit operations to domain knowledge
func (s *Spec) modifyDomainKnowledge(mod DomainKnowledgeModify) error {
	// Process removals first
	for _, toRemove := range mod.Remove {
		found := false
		for i, existing := range s.DomainKnowledge {
			if existing == toRemove {
				s.DomainKnowledge = append(
					s.DomainKnowledge[:i],
					s.DomainKnowledge[i+1:]...,
				)
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("domain knowledge not found for removal: %s", toRemove)
		}
	}

	// Process edits
	for _, edit := range mod.Edit {
		found := false
		for i, existing := range s.DomainKnowledge {
			if existing == edit.Old {
				s.DomainKnowledge[i] = edit.New
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("domain knowledge not found for edit: %s", edit.Old)
		}
	}

	// Process additions last
	s.DomainKnowledge = append(s.DomainKnowledge, mod.Add...)

	s.Updated = time.Now()
	return nil
}

// ApplyRemoveChange applies a "remove" operation to the spec
// Returns an error if:
// - The operation is not "remove"
// - For features: feature_id doesn't exist in the spec
// - For domain_knowledge: any item doesn't match exactly
func (s *Spec) ApplyRemoveChange(change Change) error {
	if change.Operation != "remove" {
		return fmt.Errorf("expected 'remove' operation, got '%s'", change.Operation)
	}

	// Remove feature if feature_id is specified
	if change.FeatureID != "" {
		if err := s.removeFeature(change.FeatureID); err != nil {
			return err
		}
	}

	// Remove domain knowledge items if specified
	for _, dk := range change.DomainKnowledge {
		if err := s.removeDomainKnowledge(dk); err != nil {
			return err
		}
	}

	return nil
}

// removeFeature removes a feature by ID from the spec
func (s *Spec) removeFeature(featureID string) error {
	for i, f := range s.Features {
		if f.ID == featureID {
			s.Features = append(s.Features[:i], s.Features[i+1:]...)
			s.Updated = time.Now()
			return nil
		}
	}
	return fmt.Errorf("feature with ID '%s' not found in spec", featureID)
}

// removeDomainKnowledge removes an exact domain knowledge string from the spec
func (s *Spec) removeDomainKnowledge(knowledge string) error {
	for i, dk := range s.DomainKnowledge {
		if dk == knowledge {
			s.DomainKnowledge = append(s.DomainKnowledge[:i], s.DomainKnowledge[i+1:]...)
			s.Updated = time.Now()
			return nil
		}
	}
	return fmt.Errorf("domain knowledge not found: %s", knowledge)
}

// ToSpec converts a change request to a chunkable Spec by transforming
// all operations into features. This enables chunking change requests
// directly without needing a parent spec.
//
// Operation handling:
// - "add" with feature: included as-is
// - "add" with only domain knowledge: ignored (no feature to chunk)
// - "remove": becomes feature "remove-{feature_id}" with removal task
// - "modify": becomes feature "modify-{feature_id}" with delta criteria
func (cr *ChangeRequest) ToSpec() *Spec {
	spec := NewSpec(cr.ID, cr.Title)
	spec.Description = "Generated from change request: " + cr.Title

	for _, change := range cr.Changes {
		switch change.Operation {
		case "add":
			if change.Feature != nil {
				spec.Features = append(spec.Features, *change.Feature)
			}
			// Ignore add operations with only domain knowledge

		case "remove":
			if change.FeatureID != "" {
				feature := Feature{
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
				spec.Features = append(spec.Features, feature)
			}

		case "modify":
			if change.FeatureID != "" {
				feature := Feature{
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
				spec.Features = append(spec.Features, feature)
			}
		}
	}

	return spec
}

// ApplyChanges applies all changes from the change request to the given spec.
// Changes are applied in order. If any change fails, the error is returned
// and the spec may be in a partially modified state.
// Note: The spec's Status is preserved (not modified by this operation).
func (cr *ChangeRequest) ApplyChanges(spec *Spec) error {
	originalStatus := spec.Status
	for i, change := range cr.Changes {
		var err error
		switch change.Operation {
		case "add":
			err = spec.ApplyAddChange(change)
		case "modify":
			err = spec.ApplyModifyChange(change)
		case "remove":
			err = spec.ApplyRemoveChange(change)
		default:
			err = fmt.Errorf("unknown operation: %s", change.Operation)
		}
		if err != nil {
			return fmt.Errorf("failed to apply change %d: %w", i, err)
		}
	}
	// Restore original status - merge should not change spec status
	spec.Status = originalStatus
	return nil
}
