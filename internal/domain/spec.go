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

// ConversationStatus represents the processing state of a conversation
type ConversationStatus string

const (
	ConversationUnprocessed      ConversationStatus = "unprocessed"
	ConversationProcessed        ConversationStatus = "processed"
	ConversationPendingExecution ConversationStatus = "pending-execution"
)

// CRCommit represents a CR that was created and committed during a session
type CRCommit struct {
	CRID      string `yaml:"cr_id"`
	CommitSHA string `yaml:"commit_sha"`
}

// ExecutionLogEntry records a WorkItem execution result for a conversation.
// Links the conversation to specific spec changes that resulted from execution.
type ExecutionLogEntry struct {
	WorkItemID  string    `yaml:"workitem_id"`
	SpecRef     string    `yaml:"spec_ref"`  // e.g., "spec-id.feature-id"
	Operation   string    `yaml:"operation"` // add, modify, remove, refactor
	CompletedAt time.Time `yaml:"completed_at"`
}

// ConversationType distinguishes exploratory conversations from system-truth conversations.
// Exploratory conversations have no CR and are informational only.
// System-truth conversations have an executed CR and represent actual system state.
type ConversationType string

const (
	// ConversationExploratory indicates a conversation with no CR - informational only
	ConversationExploratory ConversationType = "exploratory"
	// ConversationSystemTruth indicates a conversation with an executed CR - represents actual state
	ConversationSystemTruth ConversationType = "system-truth"
)

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

	// ExecutionLog tracks WorkItems executed against this conversation's CRs
	ExecutionLog []ExecutionLogEntry `yaml:"execution_log,omitempty"`

	// The full transcript content
	Transcript string `yaml:"transcript"`
}

// HasCR returns true if this conversation created any Change Requests.
func (c *Conversation) HasCR() bool {
	return len(c.CRsCreated) > 0
}

// ExecutionCompleted returns true if any WorkItems have been executed for this conversation.
func (c *Conversation) ExecutionCompleted() bool {
	return len(c.ExecutionLog) > 0
}

// Type returns the ConversationType based on CR presence and execution status.
// System-truth: has CR AND execution completed (represents actual system state).
// Exploratory: no CR (informational only, but still valuable for concepts/domain knowledge).
func (c *Conversation) Type() ConversationType {
	if c.HasCR() && c.ExecutionCompleted() {
		return ConversationSystemTruth
	}
	return ConversationExploratory
}

// IsSystemTruth returns true if this conversation represents actual system state
// (has CR and execution completed).
func (c *Conversation) IsSystemTruth() bool {
	return c.Type() == ConversationSystemTruth
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

// SignalType represents the type of documentation signal detected
type SignalType string

const (
	SignalTypeADR     SignalType = "adr"
	SignalTypeConcept SignalType = "concept"
	SignalTypeDomain  SignalType = "domain"
)

// SignalConfidence represents the confidence level of a detected signal
type SignalConfidence string

const (
	SignalConfidenceHigh   SignalConfidence = "high"
	SignalConfidenceMedium SignalConfidence = "medium"
	SignalConfidenceLow    SignalConfidence = "low"
)

// SignalLocation tracks where a signal was found in a conversation
type SignalLocation struct {
	ConversationID string `yaml:"conversation_id"`
	// MessageRange indicates the approximate location within the transcript.
	// Format: "start-end" where start/end are approximate line numbers or message indices.
	// Examples: "15-25", "early", "mid", "late" for less precise locations.
	MessageRange string `yaml:"message_range,omitempty"`
}

// HarvestSignal represents a documentation opportunity detected in a conversation
type HarvestSignal struct {
	// ID is a unique identifier for referencing this signal (e.g., "adr-1", "concept-2")
	ID string `yaml:"id"`
	// Type indicates what kind of documentation this signal suggests
	Type SignalType `yaml:"type"`
	// Title is a brief description of the signal
	Title string `yaml:"title"`
	// Description provides more detail about what was detected
	Description string `yaml:"description,omitempty"`
	// Confidence indicates how certain we are this is a valid signal
	Confidence SignalConfidence `yaml:"confidence"`
	// Location tracks where in the conversation this was found
	Location SignalLocation `yaml:"location"`
	// RelatedSignals lists IDs of signals that are related to this one
	// (e.g., an ADR decision may have a related Concept explaining trade-offs)
	RelatedSignals []string `yaml:"related_signals,omitempty"`
	// PotentialDuplicate indicates this may overlap with existing documentation
	PotentialDuplicate string `yaml:"potential_duplicate,omitempty"`
}

// HarvestResult aggregates all signals detected across conversations
type HarvestResult struct {
	// Signals contains all detected signals, grouped by type
	ADRSignals     []HarvestSignal `yaml:"adr_signals,omitempty"`
	ConceptSignals []HarvestSignal `yaml:"concept_signals,omitempty"`
	DomainSignals  []HarvestSignal `yaml:"domain_signals,omitempty"`
}

// TotalSignals returns the total count of all signals
func (h *HarvestResult) TotalSignals() int {
	return len(h.ADRSignals) + len(h.ConceptSignals) + len(h.DomainSignals)
}

// AllSignals returns all signals as a flat slice
func (h *HarvestResult) AllSignals() []HarvestSignal {
	all := make([]HarvestSignal, 0, h.TotalSignals())
	all = append(all, h.ADRSignals...)
	all = append(all, h.ConceptSignals...)
	all = append(all, h.DomainSignals...)
	return all
}
