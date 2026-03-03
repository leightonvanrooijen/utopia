package domain

import (
	"fmt"
	"time"
)

// Spec represents a living specification document
type Spec struct {
	ID          string    `yaml:"id"`
	Title       string    `yaml:"title"`
	Created     time.Time `yaml:"created"`
	Updated     time.Time `yaml:"updated"`
	Description string    `yaml:"description"`

	// Domain knowledge captured during exploration
	DomainKnowledge []string `yaml:"domain_knowledge,omitempty"`

	// Features with their acceptance criteria
	Features []Feature `yaml:"features"`

	// IsRefactor indicates this spec was converted from a Refactor.
	// When true, system-level refactor constraints are automatically
	// injected during chunking. This field is not persisted to YAML.
	IsRefactor bool `yaml:"-"`
}

// Feature represents a capability of the system
type Feature struct {
	ID                 string   `yaml:"id"`
	Description        string   `yaml:"description"`
	AcceptanceCriteria []string `yaml:"acceptance_criteria"`
}

// NewSpec creates a new spec with sensible defaults
func NewSpec(id, title string) *Spec {
	now := time.Now()
	return &Spec{
		ID:       id,
		Title:    title,
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
			// Feature already exists - already in desired state
			return nil
		}
	}
	s.AddFeature(f)
	return nil
}

// addDomainKnowledgeWithValidation adds domain knowledge only if not duplicate.
// Idempotent: if knowledge already exists, succeed silently (already in desired state).
func (s *Spec) addDomainKnowledgeWithValidation(knowledge string) error {
	for _, existing := range s.DomainKnowledge {
		if existing == knowledge {
			// Knowledge already exists - already in desired state
			return nil
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

// modifyCriteria applies add/remove/edit operations to feature acceptance criteria.
// Idempotent: removals succeed if item already gone, edits succeed if new value already present.
func (s *Spec) modifyCriteria(featureIdx int, criteria CriteriaModify) error {
	// Process removals first (before adds to avoid removing newly added items)
	// Idempotent: if item doesn't exist, it's already in desired state
	for _, toRemove := range criteria.Remove {
		for i, existing := range s.Features[featureIdx].AcceptanceCriteria {
			if existing == toRemove {
				s.Features[featureIdx].AcceptanceCriteria = append(
					s.Features[featureIdx].AcceptanceCriteria[:i],
					s.Features[featureIdx].AcceptanceCriteria[i+1:]...,
				)
				break
			}
		}
		// If not found, already in desired state - continue
	}

	// Process edits
	// Idempotent: if OLD not found but NEW exists, already in desired state
	for _, edit := range criteria.Edit {
		foundOld := false
		foundNew := false
		for i, existing := range s.Features[featureIdx].AcceptanceCriteria {
			if existing == edit.Old {
				s.Features[featureIdx].AcceptanceCriteria[i] = edit.New
				foundOld = true
				break
			}
			if existing == edit.New {
				foundNew = true
			}
		}
		if !foundOld && !foundNew {
			// Neither old nor new found - this is an error
			return fmt.Errorf("criterion not found for edit (old: %q, new: %q not present)", edit.Old, edit.New)
		}
		// If foundNew but not foundOld, already in desired state - continue
	}

	// Process additions last
	s.Features[featureIdx].AcceptanceCriteria = append(
		s.Features[featureIdx].AcceptanceCriteria,
		criteria.Add...,
	)

	return nil
}

// modifyDomainKnowledge applies add/remove/edit operations to domain knowledge.
// Idempotent: removals succeed if item already gone, edits succeed if new value already present.
func (s *Spec) modifyDomainKnowledge(mod DomainKnowledgeModify) error {
	// Process removals first
	// Idempotent: if item doesn't exist, it's already in desired state
	for _, toRemove := range mod.Remove {
		for i, existing := range s.DomainKnowledge {
			if existing == toRemove {
				s.DomainKnowledge = append(
					s.DomainKnowledge[:i],
					s.DomainKnowledge[i+1:]...,
				)
				break
			}
		}
		// If not found, already in desired state - continue
	}

	// Process edits
	// Idempotent: if OLD not found but NEW exists, already in desired state
	for _, edit := range mod.Edit {
		foundOld := false
		foundNew := false
		for i, existing := range s.DomainKnowledge {
			if existing == edit.Old {
				s.DomainKnowledge[i] = edit.New
				foundOld = true
				break
			}
			if existing == edit.New {
				foundNew = true
			}
		}
		if !foundOld && !foundNew {
			// Neither old nor new found - this is an error
			return fmt.Errorf("domain knowledge not found for edit (old: %q, new: %q not present)", edit.Old, edit.New)
		}
		// If foundNew but not foundOld, already in desired state - continue
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

// removeFeature removes a feature by ID from the spec.
// Idempotent: returns nil if the feature doesn't exist (already removed).
func (s *Spec) removeFeature(featureID string) error {
	for i, f := range s.Features {
		if f.ID == featureID {
			s.Features = append(s.Features[:i], s.Features[i+1:]...)
			s.Updated = time.Now()
			return nil
		}
	}
	// Feature not found - already in desired state, succeed silently
	return nil
}

// removeDomainKnowledge removes an exact domain knowledge string from the spec.
// Idempotent: returns nil if the knowledge doesn't exist (already removed).
func (s *Spec) removeDomainKnowledge(knowledge string) error {
	for i, dk := range s.DomainKnowledge {
		if dk == knowledge {
			s.DomainKnowledge = append(s.DomainKnowledge[:i], s.DomainKnowledge[i+1:]...)
			s.Updated = time.Now()
			return nil
		}
	}
	// Knowledge not found - already in desired state, succeed silently
	return nil
}
