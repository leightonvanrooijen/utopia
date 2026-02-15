package domain

import (
	"fmt"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
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

// CRType represents the type of change request which determines behavior and constraints
type CRType string

const (
	CRTypeFeature     CRType = "feature"
	CRTypeEnhancement CRType = "enhancement"
	CRTypeRefactor    CRType = "refactor"
	CRTypeRemoval     CRType = "removal"
	CRTypeInitiative  CRType = "initiative"
	CRTypeBugfix      CRType = "bugfix"
)

// IsValidCRType checks if a string is a valid CR type
func IsValidCRType(t string) bool {
	switch CRType(t) {
	case CRTypeFeature, CRTypeEnhancement, CRTypeRefactor, CRTypeRemoval, CRTypeInitiative, CRTypeBugfix:
		return true
	}
	return false
}

// PhaseStatus represents the lifecycle state of a phase within an initiative
type PhaseStatus string

const (
	PhaseStatusPending    PhaseStatus = "pending"
	PhaseStatusInProgress PhaseStatus = "in-progress"
	PhaseStatusComplete   PhaseStatus = "complete"
)

// Phase represents an ordered phase within an initiative CR
type Phase struct {
	Type    CRType      `yaml:"type"`              // Type of this phase (feature, refactor, etc.)
	Status  PhaseStatus `yaml:"status,omitempty"`  // Phase execution status (defaults to pending)
	Changes []Change    `yaml:"changes,omitempty"` // For feature/enhancement/removal phases
	Tasks   []Task      `yaml:"tasks,omitempty"`   // For refactor phases
}

// Task represents a single task within a refactor or bugfix CR or phase.
// For bugfix tasks, Spec and FeatureID reference the spec/feature that defines correct behavior.
type Task struct {
	ID                 string   `yaml:"id"`
	Description        string   `yaml:"description"`
	AcceptanceCriteria []string `yaml:"acceptance_criteria"`
	// Spec references the target spec (required for bugfix tasks, not used for refactors)
	Spec string `yaml:"spec,omitempty"`
	// FeatureID references the feature that defines correct behavior (required for bugfix tasks)
	FeatureID string `yaml:"feature_id,omitempty"`
}

// ChangeRequest represents a set of changes to apply to specs
type ChangeRequest struct {
	ID           string              `yaml:"id"`
	Type         CRType              `yaml:"type"` // Required: feature, enhancement, refactor, removal, initiative
	ParentSpec   string              `yaml:"parent_spec,omitempty"`
	Title        string              `yaml:"title"`
	Status       ChangeRequestStatus `yaml:"status"`
	Changes      []Change            `yaml:"changes,omitempty"`      // For feature/enhancement/removal types
	Tasks        []Task              `yaml:"tasks,omitempty"`        // For refactor type
	Phases       []Phase             `yaml:"phases,omitempty"`       // For initiative type
	CurrentPhase int                 `yaml:"current_phase,omitempty"` // For initiative: 0-indexed current phase (0 = first phase)
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
	Operation       string   `yaml:"operation"` // "add", "modify", "remove", "delete-spec"
	Spec            string   `yaml:"spec,omitempty"` // Target spec ID (required for feature/enhancement/removal/delete-spec)
	Feature         *Feature `yaml:"feature,omitempty"`
	DomainKnowledge []string `yaml:"domain_knowledge,omitempty"`
	// For modify/remove operations
	FeatureID          string                 `yaml:"feature_id,omitempty"`
	Description        string                 `yaml:"description,omitempty"`
	Criteria           *CriteriaModify        `yaml:"criteria,omitempty"`
	DomainKnowledgeMod *DomainKnowledgeModify `yaml:"domain_knowledge_mod,omitempty"`
	Reason             string                 `yaml:"reason,omitempty"` // For remove and delete-spec operations
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

// GetCurrentPhase returns the current phase index for an initiative CR.
// Returns -1 if not an initiative or all phases are complete.
func (cr *ChangeRequest) GetCurrentPhase() int {
	if cr.Type != CRTypeInitiative || len(cr.Phases) == 0 {
		return -1
	}
	return cr.CurrentPhase
}

// IsPhaseComplete returns true if the specified phase is complete.
func (cr *ChangeRequest) IsPhaseComplete(phaseIndex int) bool {
	if phaseIndex < 0 || phaseIndex >= len(cr.Phases) {
		return false
	}
	return cr.Phases[phaseIndex].Status == PhaseStatusComplete
}

// AllPhasesComplete returns true if all phases in an initiative are complete.
func (cr *ChangeRequest) AllPhasesComplete() bool {
	if cr.Type != CRTypeInitiative {
		return false
	}
	for _, phase := range cr.Phases {
		if phase.Status != PhaseStatusComplete {
			return false
		}
	}
	return true
}

// ApplyChanges applies all changes from the change request to the given spec.
// Changes are applied in order. If any change fails, the error is returned
// and the spec may be in a partially modified state.
// Note: delete-spec operations are skipped here - they are handled at the merge level.
func (cr *ChangeRequest) ApplyChanges(spec *Spec) error {
	for i, change := range cr.Changes {
		var err error
		switch change.Operation {
		case "add":
			err = spec.ApplyAddChange(change)
		case "modify":
			err = spec.ApplyModifyChange(change)
		case "remove":
			err = spec.ApplyRemoveChange(change)
		case "delete-spec":
			// delete-spec is handled at the merge level, not here
			continue
		default:
			err = fmt.Errorf("unknown operation: %s", change.Operation)
		}
		if err != nil {
			return fmt.Errorf("failed to apply change %d: %w", i, err)
		}
	}
	return nil
}

// ValidateChangeRequest checks that a change request has all required fields
// and type-specific structure populated correctly.
// Returns nil if valid, or an error describing what's missing/invalid.
func ValidateChangeRequest(cr *ChangeRequest) error {
	var errors []string

	// Required fields for all CRs
	if cr.ID == "" {
		errors = append(errors, "missing required field: id")
	}
	if cr.Title == "" {
		errors = append(errors, "missing required field: title")
	}
	if cr.Status == "" {
		errors = append(errors, "missing required field: status")
	}

	// Type is required and must be valid
	if cr.Type == "" {
		errors = append(errors, "missing required field: type")
	} else if !IsValidCRType(string(cr.Type)) {
		errors = append(errors, fmt.Sprintf("invalid type %q: must be one of feature, enhancement, refactor, removal, initiative, bugfix", cr.Type))
	} else {
		// Type-specific validation
		switch cr.Type {
		case CRTypeFeature, CRTypeEnhancement, CRTypeRemoval:
			if len(cr.Changes) == 0 {
				errors = append(errors, fmt.Sprintf("type %q requires changes array", cr.Type))
			}
			if len(cr.Tasks) > 0 {
				errors = append(errors, fmt.Sprintf("type %q should not have tasks array (use changes instead)", cr.Type))
			}
			if len(cr.Phases) > 0 {
				errors = append(errors, fmt.Sprintf("type %q should not have phases array", cr.Type))
			}
			// Validate each change has a spec field
			for i, change := range cr.Changes {
				if change.Spec == "" {
					errors = append(errors, fmt.Sprintf("changes[%d]: missing required field: spec", i))
				}
			}

		case CRTypeRefactor:
			if len(cr.Tasks) == 0 {
				errors = append(errors, "type refactor requires tasks array")
			}
			if len(cr.Changes) > 0 {
				errors = append(errors, "type refactor should not have changes array (use tasks instead)")
			}
			if len(cr.Phases) > 0 {
				errors = append(errors, "type refactor should not have phases array")
			}
			// Validate each task
			for i, task := range cr.Tasks {
				taskPrefix := fmt.Sprintf("tasks[%d]", i)
				if task.ID == "" {
					errors = append(errors, taskPrefix+": missing required field: id")
				}
				if task.Description == "" {
					errors = append(errors, taskPrefix+": missing required field: description")
				}
				if len(task.AcceptanceCriteria) == 0 {
					errors = append(errors, taskPrefix+": missing required field: acceptance_criteria")
				}
			}

		case CRTypeBugfix:
			if len(cr.Tasks) == 0 {
				errors = append(errors, "type bugfix requires tasks array")
			}
			if len(cr.Changes) > 0 {
				errors = append(errors, "type bugfix should not have changes array (use tasks instead)")
			}
			if len(cr.Phases) > 0 {
				errors = append(errors, "type bugfix should not have phases array")
			}
			// Validate each task - bugfix tasks require spec and feature_id references
			for i, task := range cr.Tasks {
				taskPrefix := fmt.Sprintf("tasks[%d]", i)
				if task.ID == "" {
					errors = append(errors, taskPrefix+": missing required field: id")
				}
				if task.Description == "" {
					errors = append(errors, taskPrefix+": missing required field: description")
				}
				if len(task.AcceptanceCriteria) == 0 {
					errors = append(errors, taskPrefix+": missing required field: acceptance_criteria")
				}
				if task.Spec == "" {
					errors = append(errors, taskPrefix+": missing required field: spec (bugfix tasks must reference target spec)")
				}
				if task.FeatureID == "" {
					errors = append(errors, taskPrefix+": missing required field: feature_id (bugfix tasks must reference feature that defines correct behavior)")
				}
			}

		case CRTypeInitiative:
			if len(cr.Phases) == 0 {
				errors = append(errors, "type initiative requires phases array")
			}
			if len(cr.Changes) > 0 {
				errors = append(errors, "type initiative should not have changes array (use phases instead)")
			}
			if len(cr.Tasks) > 0 {
				errors = append(errors, "type initiative should not have tasks array (use phases instead)")
			}
			// Validate each phase has appropriate content for its type
			for i, phase := range cr.Phases {
				phasePrefix := fmt.Sprintf("phases[%d]", i)
				if phase.Type == "" {
					errors = append(errors, phasePrefix+": missing required field: type")
				} else if phase.Type == CRTypeInitiative {
					errors = append(errors, phasePrefix+": phase type cannot be initiative (no nesting)")
				} else if phase.Type == CRTypeRefactor || phase.Type == CRTypeBugfix {
					if len(phase.Tasks) == 0 {
						errors = append(errors, fmt.Sprintf("%s: %s phase requires tasks", phasePrefix, phase.Type))
					}
					// Validate bugfix phase tasks require spec and feature_id
					if phase.Type == CRTypeBugfix {
						for j, task := range phase.Tasks {
							taskPrefix := fmt.Sprintf("%s.tasks[%d]", phasePrefix, j)
							if task.Spec == "" {
								errors = append(errors, taskPrefix+": missing required field: spec (bugfix tasks must reference target spec)")
							}
							if task.FeatureID == "" {
								errors = append(errors, taskPrefix+": missing required field: feature_id (bugfix tasks must reference feature that defines correct behavior)")
							}
						}
					}
				} else {
					if len(phase.Changes) == 0 {
						errors = append(errors, phasePrefix+": phase requires changes")
					}
					// Validate each change in non-refactor/non-bugfix phases has a spec field
					for j, change := range phase.Changes {
						if change.Spec == "" {
							errors = append(errors, fmt.Sprintf("%s.changes[%d]: missing required field: spec", phasePrefix, j))
						}
					}
				}
			}
		}
	}

	if len(errors) > 0 {
		return &CRValidationError{Errors: errors}
	}
	return nil
}

// CRValidationError holds multiple validation errors for a change request
type CRValidationError struct {
	Errors []string
}

func (e *CRValidationError) Error() string {
	return "change request validation failed:\n  - " + strings.Join(e.Errors, "\n  - ")
}

// ConversationStatus represents the processing state of a conversation
type ConversationStatus string

const (
	ConversationUnprocessed ConversationStatus = "unprocessed"
	ConversationProcessed   ConversationStatus = "processed"
)

// CRCommit represents a CR that was created and committed during a session
type CRCommit struct {
	CRID      string `yaml:"cr_id"`
	CommitSHA string `yaml:"commit_sha"`
}

// Conversation represents a captured session transcript with metadata
type Conversation struct {
	ID        string             `yaml:"id"`
	Timestamp time.Time          `yaml:"timestamp"`
	Branch    string             `yaml:"branch"`
	Status    ConversationStatus `yaml:"status"`

	// CRs created during this session (with their commit SHAs)
	CRsCreated []CRCommit `yaml:"crs_created,omitempty"`

	// All commits made during this session
	Commits []string `yaml:"commits,omitempty"`

	// The full transcript content
	Transcript string `yaml:"transcript"`
}

// ADRStatus represents the lifecycle state of an ADR
type ADRStatus string

const (
	ADRStatusDraft      ADRStatus = "draft"
	ADRStatusProposed   ADRStatus = "proposed"
	ADRStatusAccepted   ADRStatus = "accepted"
	ADRStatusDeprecated ADRStatus = "deprecated"
	ADRStatusSuperseded ADRStatus = "superseded"
)

// ADRStatusChange records a status transition with timestamp
type ADRStatusChange struct {
	From      ADRStatus `yaml:"from"`
	To        ADRStatus `yaml:"to"`
	Timestamp time.Time `yaml:"timestamp"`
	Reason    string    `yaml:"reason,omitempty"`
}

// ADROption represents an alternative that was considered
type ADROption struct {
	Option string   `yaml:"option"`
	Pros   []string `yaml:"pros,omitempty"`
	Cons   []string `yaml:"cons,omitempty"`
}

// ADRConsequences captures the outcomes of a decision
type ADRConsequences struct {
	Positive []string `yaml:"positive,omitempty"`
	Negative []string `yaml:"negative,omitempty"`
	Neutral  []string `yaml:"neutral,omitempty"`
}

// ADR represents an Architecture Decision Record
type ADR struct {
	ID                  string          `yaml:"id"`
	Title               string          `yaml:"title"`
	Status              ADRStatus       `yaml:"status"`
	Date                string          `yaml:"date"`
	Context             string          `yaml:"context"`
	Decision            string          `yaml:"decision"`
	OptionsConsidered   []ADROption     `yaml:"options_considered,omitempty"`
	Consequences        ADRConsequences `yaml:"consequences,omitempty"`
	Advice              []string        `yaml:"advice,omitempty"`
	Principles          []string        `yaml:"principles,omitempty"`
	SourceConversations []string        `yaml:"source_conversations,omitempty"`

	// Status transition tracking
	DeprecationReason string            `yaml:"deprecation_reason,omitempty"`
	SupersededBy      string            `yaml:"superseded_by,omitempty"`
	StatusHistory     []ADRStatusChange `yaml:"status_history,omitempty"`
}

// TransitionToProposed moves an ADR from draft to proposed status.
// Returns an error if the current status is not draft.
func (a *ADR) TransitionToProposed() error {
	if a.Status != ADRStatusDraft {
		return fmt.Errorf("cannot transition to proposed: ADR is in %q status (must be draft)", a.Status)
	}

	a.recordStatusChange(ADRStatusProposed, "")
	a.Status = ADRStatusProposed
	return nil
}

// TransitionToAccepted moves an ADR from proposed to accepted status.
// Returns an error if the current status is not proposed.
func (a *ADR) TransitionToAccepted() error {
	if a.Status != ADRStatusProposed {
		return fmt.Errorf("cannot transition to accepted: ADR is in %q status (must be proposed)", a.Status)
	}

	a.recordStatusChange(ADRStatusAccepted, "")
	a.Status = ADRStatusAccepted
	return nil
}

// MarkDeprecated marks an ADR as deprecated with a required reason.
// Can only be applied to accepted ADRs.
// Returns an error if the current status is not accepted or if reason is empty.
func (a *ADR) MarkDeprecated(reason string) error {
	if a.Status != ADRStatusAccepted {
		return fmt.Errorf("cannot deprecate: ADR is in %q status (must be accepted)", a.Status)
	}
	if reason == "" {
		return fmt.Errorf("deprecation reason is required")
	}

	a.recordStatusChange(ADRStatusDeprecated, reason)
	a.Status = ADRStatusDeprecated
	a.DeprecationReason = reason
	return nil
}

// MarkSuperseded marks an ADR as superseded by another ADR.
// Can only be applied to accepted ADRs.
// Returns an error if the current status is not accepted or if replacementADRID is empty.
func (a *ADR) MarkSuperseded(replacementADRID string) error {
	if a.Status != ADRStatusAccepted {
		return fmt.Errorf("cannot supersede: ADR is in %q status (must be accepted)", a.Status)
	}
	if replacementADRID == "" {
		return fmt.Errorf("replacement ADR ID is required")
	}

	reason := fmt.Sprintf("Superseded by %s", replacementADRID)
	a.recordStatusChange(ADRStatusSuperseded, reason)
	a.Status = ADRStatusSuperseded
	a.SupersededBy = replacementADRID
	return nil
}

// recordStatusChange appends a status change to the history
func (a *ADR) recordStatusChange(newStatus ADRStatus, reason string) {
	change := ADRStatusChange{
		From:      a.Status,
		To:        newStatus,
		Timestamp: time.Now(),
		Reason:    reason,
	}
	a.StatusHistory = append(a.StatusHistory, change)
}
