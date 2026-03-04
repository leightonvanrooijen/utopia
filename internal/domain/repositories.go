package domain

// SpecRepository defines storage operations for Spec entities.
// Implementations handle persistence details (YAML files, databases, etc.).
type SpecRepository interface {
	LoadSpec(id string) (*Spec, error)
	SaveSpec(spec *Spec) error
	ListSpecs() ([]*Spec, error)
	DeleteSpec(id string) error
}

// ChangeRequestRepository defines storage operations for ChangeRequest entities.
// Implementations handle persistence details (YAML files, databases, etc.).
type ChangeRequestRepository interface {
	LoadChangeRequest(id string) (*ChangeRequest, error)
	SaveChangeRequest(cr *ChangeRequest) error
	ListChangeRequests() ([]*ChangeRequest, error)
	DeleteChangeRequest(id string) error
}

// WorkItemRepository defines storage operations for WorkItem entities.
// Work items are stored hierarchically under specs/CRs for organization.
type WorkItemRepository interface {
	LoadWorkItem(id string) (*WorkItem, error)
	SaveWorkItem(item *WorkItem) error
	ListWorkItems() ([]*WorkItem, error)
	// SaveWorkItemForSpec saves a work item under a specific spec/CR directory.
	SaveWorkItemForSpec(specID string, item *WorkItem) error
	// LoadWorkItemForSpec loads a work item from a specific spec/CR directory.
	LoadWorkItemForSpec(specID, id string) (*WorkItem, error)
	// ListWorkItemsForSpec returns all work items for a specific spec/CR.
	ListWorkItemsForSpec(specID string) ([]*WorkItem, error)
}

// ConversationRepository defines storage operations for Conversation entities.
// Includes query methods for filtering by status and type.
type ConversationRepository interface {
	LoadConversation(id string) (*Conversation, error)
	SaveConversation(conv *Conversation) error
	ListConversations() ([]*Conversation, error)
	// ListUnprocessedConversations returns conversations with status "unprocessed".
	ListUnprocessedConversations() ([]*Conversation, error)
	// ListUnprocessedConversationsByType filters unprocessed conversations by type.
	ListUnprocessedConversationsByType(convType ConversationType) ([]*Conversation, error)
	// MarkConversationsReadyForHarvest transitions conversations referencing the CR
	// from pending-execution to unprocessed status.
	MarkConversationsReadyForHarvest(crID string) error
	// LoadConversationsByCRID returns all conversations that reference the given CR.
	LoadConversationsByCRID(crID string) ([]*Conversation, error)
	// AppendExecutionLogEntry adds a log entry to conversations referencing the CR.
	AppendExecutionLogEntry(crID string, entry ExecutionLogEntry) error
}

// ConfigRepository defines storage operations for project configuration.
type ConfigRepository interface {
	LoadConfig() (*Config, error)
	SaveConfig(config *Config) error
}

// DraftRepository defines storage operations for DraftSpec entities.
// Drafts are stored in .utopia/drafts/specs/ and represent proposed specs
// discovered from codebase analysis that require validation.
type DraftRepository interface {
	LoadDraft(id string) (*DraftSpec, error)
	SaveDraft(draft *DraftSpec) error
	ListDrafts() ([]*DraftSpec, error)
	DeleteDraft(id string) error
}

// DiscoveryStateRepository defines storage operations for discovery state.
// State is stored in .utopia/drafts/specs/.discovery-state to track incremental discovery.
type DiscoveryStateRepository interface {
	LoadDiscoveryState() (*DiscoveryState, error)
	SaveDiscoveryState(state *DiscoveryState) error
}

// DraftDomainDocRepository defines storage operations for DraftDomainDoc entities.
// Draft domain docs are stored in .utopia/drafts/domain/ and represent proposed domain
// vocabulary discovered from codebase analysis that requires validation.
type DraftDomainDocRepository interface {
	LoadDraftDomainDoc(id string) (*DraftDomainDoc, error)
	SaveDraftDomainDoc(draft *DraftDomainDoc) error
	ListDraftDomainDocs() ([]*DraftDomainDoc, error)
	DeleteDraftDomainDoc(id string) error
}

// DomainDiscoveryStateRepository defines storage operations for domain discovery state.
// State is stored in .utopia/drafts/domain/.discovery-state to track incremental discovery.
type DomainDiscoveryStateRepository interface {
	LoadDomainDiscoveryState() (*DomainDiscoveryState, error)
	SaveDomainDiscoveryState(state *DomainDiscoveryState) error
}
