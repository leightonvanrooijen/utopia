package domain

import (
	"fmt"
	"strings"
)

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
	Changes      []Change            `yaml:"changes,omitempty"`       // For feature/enhancement/removal types
	Tasks        []Task              `yaml:"tasks,omitempty"`         // For refactor type
	Phases       []Phase             `yaml:"phases,omitempty"`        // For initiative type
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
	Operation       string   `yaml:"operation"`      // "add", "modify", "remove", "delete-spec"
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
