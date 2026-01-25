package domain

import (
	"fmt"
	"time"
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

// Change represents a single operation in a change request
type Change struct {
	Operation       string   `yaml:"operation"` // "add", "modify", "remove"
	Feature         *Feature `yaml:"feature,omitempty"`
	DomainKnowledge []string `yaml:"domain_knowledge,omitempty"`
	// For modify/remove operations (to be implemented later)
	FeatureID string `yaml:"feature_id,omitempty"`
	Reason    string `yaml:"reason,omitempty"`
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
