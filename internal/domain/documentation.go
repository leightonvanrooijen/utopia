package domain

import (
	"fmt"
	"strings"
	"time"
)

// =============================================================================
// Conversation Types
// =============================================================================

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

// SourceType classifies where a harvest signal came from, and therefore how
// much weight it carries. Exploratory sources are informational only;
// system-truth sources describe changes that were actually made to the system.
type SourceType string

const (
	// SourceExploratory indicates a conversation with no CR - informational only
	SourceExploratory SourceType = "exploratory"
	// SourceSystemTruth indicates a conversation with an executed CR - represents actual state
	SourceSystemTruth SourceType = "system-truth"
	// SourceExecution indicates an execution run - the record of what was
	// actually built while a work item ran. Distinct from SourceSystemTruth
	// (an executed conversation records what was agreed before building), but
	// system-truth all the same, and the closest source there is to the code.
	SourceExecution SourceType = "execution"
)

// IsSystemTruth reports whether this source describes actual system state
// rather than discussion. Both executed conversations and execution runs
// qualify: one records the decision that was implemented, the other records
// the implementing.
func (t SourceType) IsSystemTruth() bool {
	return t == SourceSystemTruth || t == SourceExecution
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

// Type returns the SourceType based on CR presence and execution status.
// System-truth: has CR AND execution completed (represents actual system state).
// Exploratory: no CR (informational only, but still valuable for concepts/domain knowledge).
func (c *Conversation) Type() SourceType {
	if c.HasCR() && c.ExecutionCompleted() {
		return SourceSystemTruth
	}
	return SourceExploratory
}

// IsSystemTruth returns true if this conversation represents actual system state
// (has CR and execution completed).
func (c *Conversation) IsSystemTruth() bool {
	return c.Type().IsSystemTruth()
}

// =============================================================================
// Execution Run Types
// =============================================================================

// RunOutcome records how a work item's execution run ended.
type RunOutcome string

const (
	// RunCompleted means the work item passed verification and its gates.
	RunCompleted RunOutcome = "completed"
	// RunFailed means the loop gave up on the work item without completing it.
	RunFailed RunOutcome = "failed"
)

// ExecutionRun is the durable record of one work item's Ralph execution: the
// streamed Claude output the loop already collected for completion-token
// detection, plus the metadata needed to join the run back to where it came
// from. A conversation captures what was decided before building; a run
// captures what was decided while building, which would otherwise be lost the
// moment the loop moved on to the next work item.
//
// CRID and WorkItemID are the join keys: CRID names the change request, and
// that CR's conversations carry an ExecutionLogEntry for the same WorkItemID,
// so a run can be traced to the conversation that originated it.
type ExecutionRun struct {
	WorkItemID string `yaml:"workitem_id"`
	CRID       string `yaml:"cr_id"`
	SpecRef    string `yaml:"spec_ref"` // e.g., "spec-id.feature-id"

	// Iterations is how many Claude invocations the run took to reach Outcome.
	Iterations  int        `yaml:"iterations"`
	CompletedAt time.Time  `yaml:"completed_at"`
	Outcome     RunOutcome `yaml:"outcome"`

	// Status is the harvest processing state. Runs share the conversation
	// vocabulary (unprocessed/processed) rather than growing a parallel one,
	// because harvest treats both the same way: reviewed once, then marked
	// processed so the next harvest only sees new material.
	Status ConversationStatus `yaml:"status,omitempty"`

	// Transcript is the streamed Claude output accumulated across every
	// iteration of the run.
	Transcript string `yaml:"transcript"`

	// Routing records how the run was routed between executors: which model and
	// effort each attempt used, how many failures of each class it took, and which
	// escalation paths were spent. It rides on the run record rather than in a
	// parallel file so a change request's routing history is the same directory
	// read as its transcripts, and the same git history is the time series.
	//
	// It is omitted only on runs written before routing was recorded.
	Routing *RoutingRecord `yaml:"routing,omitempty"`
}

// RoutingOutcome records how a work item's routing ended. It is a separate
// vocabulary from RunOutcome because it answers a different question: RunOutcome
// says whether the transcript is worth harvesting, RoutingOutcome says whether
// the routing worked, and "needs_human" is a distinct answer to the second that
// the first flattens into "failed".
type RoutingOutcome string

const (
	// RoutingPassed means the work item completed verification and its gates.
	RoutingPassed RoutingOutcome = "passed"
	// RoutingNeedsHuman means every bounded escalation path was exhausted, so no
	// further attempt could have changed the outcome.
	RoutingNeedsHuman RoutingOutcome = "needs_human"
	// RoutingAbandoned means the loop stopped for a reason other than an exhausted
	// escalation cap - max iterations, or an error that ended the item.
	RoutingAbandoned RoutingOutcome = "abandoned"
)

// CostNotCapturedNote is recorded verbatim on every routing record so a reader
// does not mistake the absence of token counts and dollar figures for zero. See
// ADR-005: Utopia drives the claude CLI, which reports neither, so cost is
// approximated from attempt counts and the model each attempt ran on.
const CostNotCapturedNote = "Token counts, cache hits and monetary cost are not captured - not zero, not observable. " +
	"Utopia drives the claude CLI, which reports none of them (ADR-005). " +
	"Cost is approximated from attempts, sonnet_attempts and opus_execution_attempts together with the model each attempt ran on."

// RoutingRecord is the persisted evidence for how one work item was routed. It
// exists so the escalation caps, and the premise behind them, can be argued from
// records rather than intuition - specifically the premise that repeated
// comprehension failure indicts the change request rather than the executor (see
// ADR-006 for what would overturn it).
//
// Every field here is either counted or measured by the loop. Nothing is
// estimated, and nothing that would have to be estimated is present.
type RoutingRecord struct {
	// CRType is the change request's type - for an initiative, the type of the
	// phase this work item came from. It is on the record because escalation rate
	// per cr_type is one of the two aggregations the record exists to support.
	CRType CRType `yaml:"cr_type"`

	// Attempts is every execution attempt the item made, in order, with the model
	// and effort it ran on. It is the per-attempt evidence behind the counts below,
	// and the evidence that no path raised the default executor's effort.
	Attempts []ExecutorAttempt `yaml:"attempts,omitempty"`

	// SonnetAttempts and OpusExecutionAttempts count attempts on the default and
	// escalated executors. They are named for the roles' usual models because that
	// is the vocabulary the caps are configured in, and they are the cost
	// approximation: attempts at a tier, not tokens.
	SonnetAttempts        int `yaml:"sonnet_attempts"`
	OpusExecutionAttempts int `yaml:"opus_execution_attempts"`

	// MechanicalFailures and ComprehensionFailures count failures by the class the
	// validators reported, before any routing reclassification, so their ratio
	// measures what the validators saw rather than what the caps did with it.
	// ReclassifiedFailures is how many of the mechanical ones the mechanical cap
	// then routed as comprehension, which is what reconciles these counts with the
	// routing counters on the work item.
	MechanicalFailures    int `yaml:"mechanical_failures"`
	ComprehensionFailures int `yaml:"comprehension_failures"`
	ReclassifiedFailures  int `yaml:"reclassified_failures"`

	// ScopingEscalations is how many times the change request was sent for rewrite;
	// SpecRewritten is whether a rewrite ever produced a change request execution
	// actually resumed against. They differ when a rewrite produced nothing usable.
	ScopingEscalations int  `yaml:"scoping_escalations"`
	SpecRewritten      bool `yaml:"spec_rewritten"`

	Outcome RoutingOutcome `yaml:"outcome"`

	// DurationSeconds is the item's wall clock, measured from the first attempt to
	// the write of this record. Duration is the same figure rendered for a reader;
	// queries use the seconds.
	DurationSeconds float64 `yaml:"duration_seconds"`
	Duration        string  `yaml:"duration"`

	// CostNote is CostNotCapturedNote, written into every record rather than left
	// to documentation, because the reader who needs it is reading the record.
	CostNote string `yaml:"cost_note"`
}

// Escalated reports whether this item left the default executor at all - either
// onto the escalated executor or into a change-request rewrite. It is the
// numerator of the escalation rate.
func (r *RoutingRecord) Escalated() bool {
	return r.OpusExecutionAttempts > 0 || r.ScopingEscalations > 0
}

// DefaultExecutorEffort returns the effort every attempt on the default executor
// ran at, and whether that effort was the same on all of them. A false second
// return is a bug in the loop, not a configuration choice: no path may raise the
// default executor's effort in response to a failure (ADR-004), and these
// records are where that rule is checked after the fact rather than only in a
// test.
func (r *RoutingRecord) DefaultExecutorEffort() (string, bool) {
	effort := ""
	seen := false
	for _, a := range r.Attempts {
		if a.Role != ExecutorRoleDefault {
			continue
		}
		if !seen {
			effort, seen = a.Effort, true
			continue
		}
		if a.Effort != effort {
			return effort, false
		}
	}
	return effort, true
}

// RoutingSummary aggregates routing records over some grouping - a spec, a CR
// type - so escalation rate and the mechanical-to-comprehension ratio are read
// off the persisted records without re-reading a single transcript.
type RoutingSummary struct {
	// Records is how many routing records the group contains, the denominator of
	// every rate below.
	Records int
	// Escalated is how many of them left the default executor.
	Escalated int
	// Passed, NeedsHuman and Abandoned partition Records by outcome.
	Passed     int
	NeedsHuman int
	Abandoned  int
	// MechanicalFailures and ComprehensionFailures are the group's totals by
	// reported class.
	MechanicalFailures    int
	ComprehensionFailures int
	// SpecRewrites is how many records ended up executing against a rewritten
	// change request.
	SpecRewrites int
}

// EscalationRate is the share of records in the group that left the default
// executor, or 0 for an empty group.
func (s RoutingSummary) EscalationRate() float64 {
	if s.Records == 0 {
		return 0
	}
	return float64(s.Escalated) / float64(s.Records)
}

// MechanicalToComprehensionRatio is mechanical failures per comprehension
// failure. It returns 0 when there were no failures of either class, and -1 when
// there were mechanical failures and no comprehension failures at all - a ratio
// with no finite value, reported as one rather than as a division by zero.
func (s RoutingSummary) MechanicalToComprehensionRatio() float64 {
	if s.ComprehensionFailures == 0 {
		if s.MechanicalFailures == 0 {
			return 0
		}
		return -1
	}
	return float64(s.MechanicalFailures) / float64(s.ComprehensionFailures)
}

// SummariseRoutingBySpec groups routing records by the spec their work item
// implemented - the spec-id half of spec_ref, not the feature.
//
// This is the aggregation the record exists for. A spec whose change requests
// escalate repeatedly has a boundary problem rather than a model problem, and
// that is a finding about the codebase, not about routing.
func SummariseRoutingBySpec(runs []*ExecutionRun) map[string]RoutingSummary {
	return summariseRouting(runs, func(run *ExecutionRun) string {
		return specIDOf(run.SpecRef)
	})
}

// SummariseRoutingByCRType groups routing records by change request type, so
// "features escalate more than enhancements" is a query rather than a hunch.
func SummariseRoutingByCRType(runs []*ExecutionRun) map[string]RoutingSummary {
	return summariseRouting(runs, func(run *ExecutionRun) string {
		return string(run.Routing.CRType)
	})
}

// summariseRouting folds every run that carries a routing record into a summary
// per key. Runs written before routing was recorded carry none and are skipped
// rather than counted as non-escalating, which would understate every rate.
func summariseRouting(runs []*ExecutionRun, key func(*ExecutionRun) string) map[string]RoutingSummary {
	summaries := map[string]RoutingSummary{}
	for _, run := range runs {
		if run == nil || run.Routing == nil {
			continue
		}
		k := key(run)
		s := summaries[k]
		s.Records++
		if run.Routing.Escalated() {
			s.Escalated++
		}
		switch run.Routing.Outcome {
		case RoutingPassed:
			s.Passed++
		case RoutingNeedsHuman:
			s.NeedsHuman++
		case RoutingAbandoned:
			s.Abandoned++
		}
		s.MechanicalFailures += run.Routing.MechanicalFailures
		s.ComprehensionFailures += run.Routing.ComprehensionFailures
		if run.Routing.SpecRewritten {
			s.SpecRewrites++
		}
		summaries[k] = s
	}
	return summaries
}

// specIDOf takes the spec half of a "spec-id.feature-id" reference. A reference
// with no feature suffix is already a spec id.
func specIDOf(specRef string) string {
	if spec, _, found := strings.Cut(specRef, "."); found {
		return spec
	}
	return specRef
}

// Type returns SourceExecution: a run is always its own source type, whatever
// its outcome. A failed run still records real decisions taken against the
// real codebase.
func (r *ExecutionRun) Type() SourceType {
	return SourceExecution
}

// IsSystemTruth returns true - an execution run is the record of what was
// actually built, so it is system-truth by construction.
func (r *ExecutionRun) IsSystemTruth() bool {
	return r.Type().IsSystemTruth()
}

// IsUnprocessed reports whether this run is still waiting to be harvested.
// An empty status counts as unprocessed: runs persisted before harvest tracked
// run status carry no status field, and those are exactly the runs whose
// decisions have never been reviewed.
func (r *ExecutionRun) IsUnprocessed() bool {
	return r.Status == "" || r.Status == ConversationUnprocessed
}

// =============================================================================
// Harvest Signal Types
// =============================================================================

// SignalType represents the type of documentation signal detected
type SignalType string

const (
	SignalTypeADR     SignalType = "adr"
	SignalTypeConcept SignalType = "concept"
	SignalTypeDomain  SignalType = "domain"
	SignalTypeREADME  SignalType = "readme"
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
	READMESignals  []HarvestSignal `yaml:"readme_signals,omitempty"`
}

// =============================================================================
// ADR (Architecture Decision Record) Types
// =============================================================================

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

	// SourceRuns records the execution runs a decision was surfaced from, each
	// as "<cr-id>/<workitem-id>" - the path the run is stored under. A decision
	// found while building carries both: the run for what was actually built,
	// and SourceConversations for the conversation that decided why.
	SourceRuns []string `yaml:"source_runs,omitempty"`

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

// =============================================================================
// Domain Documentation Types
// =============================================================================

// TermEvidence tracks where a specific term was found in code
type TermEvidence struct {
	// Files lists source files where this term was found
	Files []string `yaml:"files,omitempty"`
	// Lines captures specific code snippets or line references showing term usage
	Lines []string `yaml:"lines,omitempty"`
}

// DomainTerm represents a term within a bounded context
type DomainTerm struct {
	Term             string        `yaml:"term"`
	Definition       string        `yaml:"definition"`
	Canonical        bool          `yaml:"canonical"`                    // Indicates this is THE name to use in code and communication
	CodeUsage        string        `yaml:"code_usage"`                   // Where this term appears in code (or should)
	Aliases          []string      `yaml:"aliases,omitempty"`            // Alternative names that map to this canonical term
	CrossContextNote string        `yaml:"cross_context_note,omitempty"` // Notes about how this term differs in other contexts
	Evidence         *TermEvidence `yaml:"evidence,omitempty"`           // Where this term was found in code (for drafts)
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

	// SourceRuns records the execution runs a term was surfaced from, each as
	// "<cr-id>/<workitem-id>" - the path the run is stored under. A term coined
	// while building often has no conversation behind it: the name was chosen at
	// the code, and the run is the only record of where it came from.
	SourceRuns []string `yaml:"source_runs,omitempty"`
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

// HasTypeDefinitions returns true if the draft has type file evidence
func (d *DraftDomainDoc) HasTypeDefinitions() bool {
	return len(d.Evidence.TypeFiles) > 0
}

// HasSchemas returns true if the draft has schema file evidence
func (d *DraftDomainDoc) HasSchemas() bool {
	return len(d.Evidence.SchemaFiles) > 0
}

// =============================================================================
// Concept Documentation Types
// =============================================================================

// ConceptStatus represents the lifecycle state of a concept document
type ConceptStatus string

const (
	ConceptStatusDraft     ConceptStatus = "draft"
	ConceptStatusPublished ConceptStatus = "published"
)

// StandardsDocMeta holds the frontmatter metadata of a standards document
// in .utopia/standards/. Only lightweight metadata is carried - executors
// read the full doc content on demand from Path.
type StandardsDocMeta struct {
	ID          string   `yaml:"id"`
	Title       string   `yaml:"title"`
	Description string   `yaml:"description"`
	Tags        []string `yaml:"tags,omitempty"`
	// Path is the project-root-relative path to the doc (not stored in frontmatter)
	Path string `yaml:"-"`
}

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
