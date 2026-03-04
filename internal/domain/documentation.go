package domain

import (
	"fmt"
	"time"
)

// ADRStatus represents the lifecycle state of an ADR
type ADRStatus string

const (
	ADRStatusDraft      ADRStatus = "draft"
	ADRStatusProposed   ADRStatus = "proposed"
	ADRStatusAccepted   ADRStatus = "accepted"
	ADRStatusDeprecated ADRStatus = "deprecated"
	ADRStatusSuperseded ADRStatus = "superseded"
)

// ADRCategory classifies architectural decisions using AWS architectural decision categories.
// Each category represents a distinct type of architectural concern.
type ADRCategory string

const (
	// ADRCategoryStructure covers architectural patterns, layers, and component organization.
	// Examples: microservices vs monolith, event-driven architecture, hexagonal architecture.
	ADRCategoryStructure ADRCategory = "structure"

	// ADRCategoryNFR covers non-functional requirements that affect architecture.
	// Examples: security approach, high availability strategy, fault tolerance, performance targets.
	ADRCategoryNFR ADRCategory = "nfr"

	// ADRCategoryDependencies covers component coupling and external service choices.
	// Examples: database selection, third-party integrations, internal service dependencies.
	ADRCategoryDependencies ADRCategory = "dependencies"

	// ADRCategoryInterfaces covers APIs, published contracts, and integration points.
	// Examples: REST vs GraphQL, event schemas, internal API contracts, protocol choices.
	ADRCategoryInterfaces ADRCategory = "interfaces"

	// ADRCategoryConstruction covers libraries, frameworks, tools, and build processes.
	// Examples: framework choice, CI/CD approach, testing strategy, deployment tooling.
	ADRCategoryConstruction ADRCategory = "construction"
)

// IsValid returns true if the category is one of the five valid AWS architectural categories.
// A decision that doesn't fit any category is not architecturally significant.
func (c ADRCategory) IsValid() bool {
	switch c {
	case ADRCategoryStructure, ADRCategoryNFR, ADRCategoryDependencies,
		ADRCategoryInterfaces, ADRCategoryConstruction:
		return true
	default:
		return false
	}
}

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
	Category            ADRCategory     `yaml:"category"`
	Significance        string          `yaml:"significance"`
	ReversalCost        string          `yaml:"reversal_cost"`
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

// Validate checks that the ADR has all required fields and valid values.
// Returns an error if validation fails.
func (a *ADR) Validate() error {
	if !a.Category.IsValid() {
		return fmt.Errorf("invalid ADR category %q: must be one of structure, nfr, dependencies, interfaces, or construction", a.Category)
	}
	return nil
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

// DomainTerm represents a term within a bounded context
type DomainTerm struct {
	Term             string   `yaml:"term"`
	Definition       string   `yaml:"definition"`
	Canonical        bool     `yaml:"canonical"`                    // Indicates this is THE name to use in code and communication
	CodeUsage        string   `yaml:"code_usage"`                   // Where this term appears in code (or should)
	Aliases          []string `yaml:"aliases,omitempty"`            // Alternative names that map to this canonical term
	CrossContextNote string   `yaml:"cross_context_note,omitempty"` // Notes about how this term differs in other contexts
}

// DomainEntity represents an entity within a bounded context
type DomainEntity struct {
	Name          string               `yaml:"name"`
	Description   string               `yaml:"description,omitempty"`
	Relationships []EntityRelationship `yaml:"relationships,omitempty"`
}

// EntityRelationship represents a relationship between entities
type EntityRelationship struct {
	Type   string `yaml:"type"`   // e.g., "contains", "produces", "references"
	Target string `yaml:"target"` // The related entity name
}

// DomainDoc represents domain terminology documentation for a bounded context
type DomainDoc struct {
	ID                  string         `yaml:"id"`
	Title               string         `yaml:"title"`
	BoundedContext      string         `yaml:"bounded_context"` // Which context owns this vocabulary - context boundaries should be explicit and intentional
	Description         string         `yaml:"description"`
	Terms               []DomainTerm   `yaml:"terms,omitempty"`
	Entities            []DomainEntity `yaml:"entities,omitempty"`
	SourceConversations []string       `yaml:"source_conversations,omitempty"`
}

// DraftDomainConfidence indicates how confident we are in a discovered domain document
type DraftDomainConfidence string

const (
	// DraftDomainConfidenceHigh indicates strong evidence: clear type definitions, consistent naming, documentation
	DraftDomainConfidenceHigh DraftDomainConfidence = "high"
	// DraftDomainConfidenceMedium indicates partial evidence: some type definitions, naming patterns visible
	DraftDomainConfidenceMedium DraftDomainConfidence = "medium"
	// DraftDomainConfidenceLow indicates weak evidence: inferred from code patterns, inconsistent naming
	DraftDomainConfidenceLow DraftDomainConfidence = "low"
)

// DraftDomainDoc represents a proposed domain document discovered from codebase analysis.
// Draft domain docs live in .utopia/drafts/domain/ and require validation before promotion.
type DraftDomainDoc struct {
	ID             string                `yaml:"id"`
	Title          string                `yaml:"title"`
	BoundedContext string                `yaml:"bounded_context"`
	Description    string                `yaml:"description"`
	Confidence     DraftDomainConfidence `yaml:"confidence"`
	Created        time.Time             `yaml:"created"`

	// DiscoveredFrom lists the source files that were analyzed to create this draft.
	DiscoveredFrom []string `yaml:"discovered_from,omitempty"`

	// UncertaintyNotes explains what's unclear about this draft (especially for low confidence)
	UncertaintyNotes []string `yaml:"uncertainty_notes,omitempty"`

	// Evidence captures what sources informed this draft
	Evidence DraftDomainEvidence `yaml:"evidence"`

	// Proposed terms for this bounded context
	Terms []DomainTerm `yaml:"terms,omitempty"`

	// Proposed entities for this bounded context
	Entities []DomainEntity `yaml:"entities,omitempty"`
}

// DraftDomainEvidence tracks what sources informed the draft domain doc
type DraftDomainEvidence struct {
	// TypeFiles lists source files containing type definitions
	TypeFiles []string `yaml:"type_files,omitempty"`
	// PackageFiles lists files showing package structure
	PackageFiles []string `yaml:"package_files,omitempty"`
	// SchemaFiles lists files containing schemas (yaml, json, protobuf, etc.)
	SchemaFiles []string `yaml:"schema_files,omitempty"`
	// Comments captures relevant code comments explaining domain concepts
	Comments []string `yaml:"comments,omitempty"`
}

// NewDraftDomainDoc creates a new draft domain doc with sensible defaults
func NewDraftDomainDoc(id, title, boundedContext string, confidence DraftDomainConfidence) *DraftDomainDoc {
	return &DraftDomainDoc{
		ID:             id,
		Title:          title,
		BoundedContext: boundedContext,
		Confidence:     confidence,
		Created:        time.Now(),
		Evidence:       DraftDomainEvidence{},
		Terms:          []DomainTerm{},
		Entities:       []DomainEntity{},
	}
}

// HasTypeDefinitions returns true if the draft has type file evidence
func (d *DraftDomainDoc) HasTypeDefinitions() bool {
	return len(d.Evidence.TypeFiles) > 0
}

// HasSchemas returns true if the draft has schema file evidence
func (d *DraftDomainDoc) HasSchemas() bool {
	return len(d.Evidence.SchemaFiles) > 0
}

// ConceptStatus represents the lifecycle state of a concept document
type ConceptStatus string

const (
	ConceptStatusDraft     ConceptStatus = "draft"
	ConceptStatusPublished ConceptStatus = "published"
)

// ConceptDoc represents an educational trade-off explanation document.
// Unlike other docs, concepts are stored as Markdown with YAML frontmatter
// for readability and external sharing.
type ConceptDoc struct {
	ID                  string        `yaml:"id"`
	Title               string        `yaml:"title"`
	Status              ConceptStatus `yaml:"status"`
	RelatedSpecs        []string      `yaml:"related_specs,omitempty"`
	RelatedADRs         []string      `yaml:"related_adrs,omitempty"`
	SourceConversations []string      `yaml:"source_conversations,omitempty"`
	// Content is the markdown body (not stored in frontmatter)
	Content string `yaml:"-"`
}
