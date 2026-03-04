package domain

import "time"

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

// TotalSignals returns the total count of all signals
func (h *HarvestResult) TotalSignals() int {
	return len(h.ADRSignals) + len(h.ConceptSignals) + len(h.DomainSignals) + len(h.READMESignals)
}

// AllSignals returns all signals as a flat slice
func (h *HarvestResult) AllSignals() []HarvestSignal {
	all := make([]HarvestSignal, 0, h.TotalSignals())
	all = append(all, h.ADRSignals...)
	all = append(all, h.ConceptSignals...)
	all = append(all, h.DomainSignals...)
	all = append(all, h.READMESignals...)
	return all
}
