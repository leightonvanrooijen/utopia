package storage

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/leightonvanrooijen/utopia/internal/domain"
	"gopkg.in/yaml.v3"
)

// Compile-time interface assertions.
// These ensure YAMLStore implements all repository interfaces from the domain package.
var (
	_ domain.SpecRepository          = (*YAMLStore)(nil)
	_ domain.ChangeRequestRepository = (*YAMLStore)(nil)
	_ domain.WorkItemRepository      = (*YAMLStore)(nil)
	_ domain.ConversationRepository  = (*YAMLStore)(nil)
	_ domain.ConfigRepository        = (*YAMLStore)(nil)
	_ domain.DraftRepository         = (*YAMLStore)(nil)
)

// YAMLStore handles reading and writing YAML files
type YAMLStore struct {
	baseDir string
}

// NewYAMLStore creates a store rooted at the given directory
func NewYAMLStore(baseDir string) *YAMLStore {
	return &YAMLStore{baseDir: baseDir}
}

// SaveSpec writes a spec to .utopia/specs/{id}.yaml
func (s *YAMLStore) SaveSpec(spec *domain.Spec) error {
	dir := filepath.Join(s.baseDir, "specs")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create specs directory: %w", err)
	}

	path := filepath.Join(dir, spec.ID+".yaml")
	return s.writeSpecYAML(path, spec)
}

// writeSpecYAML marshals a spec using custom marshaling for features
func (s *YAMLStore) writeSpecYAML(path string, spec *domain.Spec) error {
	marshaler := newSpecMarshaler(spec)
	bytes, err := yaml.Marshal(marshaler)
	if err != nil {
		return fmt.Errorf("failed to marshal YAML: %w", err)
	}

	// Post-process to add blank lines between features for readability
	content := addFeatureSpacing(string(bytes))

	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return fmt.Errorf("failed to write file %s: %w", path, err)
	}

	return nil
}

// LoadSpec reads a spec from .utopia/specs/{id}.yaml
func (s *YAMLStore) LoadSpec(id string) (*domain.Spec, error) {
	path := filepath.Join(s.baseDir, "specs", id+".yaml")

	var spec domain.Spec
	if err := s.readYAML(path, &spec); err != nil {
		return nil, err
	}

	return &spec, nil
}

// DeleteSpec removes a spec file from .utopia/specs/{id}.yaml
func (s *YAMLStore) DeleteSpec(id string) error {
	path := filepath.Join(s.baseDir, "specs", id+".yaml")
	if err := os.Remove(path); err != nil {
		if os.IsNotExist(err) {
			return &domain.NotFoundError{Resource: "spec", ID: id}
		}
		return fmt.Errorf("failed to delete spec %s: %w", id, err)
	}
	return nil
}

// ListSpecs returns all specs in the specs directory
func (s *YAMLStore) ListSpecs() ([]*domain.Spec, error) {
	dir := filepath.Join(s.baseDir, "specs")

	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return []*domain.Spec{}, nil
		}
		return nil, fmt.Errorf("failed to read specs directory: %w", err)
	}

	var specs []*domain.Spec
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".yaml") {
			continue
		}

		id := strings.TrimSuffix(entry.Name(), ".yaml")
		spec, err := s.LoadSpec(id)
		if err != nil {
			return nil, fmt.Errorf("failed to load spec %s: %w", id, err)
		}
		specs = append(specs, spec)
	}

	return specs, nil
}

// SaveWorkItem writes a work item to .utopia/work-items/{id}.yaml
func (s *YAMLStore) SaveWorkItem(item *domain.WorkItem) error {
	dir := filepath.Join(s.baseDir, "work-items")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create work-items directory: %w", err)
	}

	path := filepath.Join(dir, item.ID+".yaml")
	return s.writeYAML(path, item)
}

// SaveWorkItemForSpec writes a work item to .utopia/work-items/{specID}/{id}.yaml
func (s *YAMLStore) SaveWorkItemForSpec(specID string, item *domain.WorkItem) error {
	dir := filepath.Join(s.baseDir, "work-items", specID)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create work-items directory for spec %s: %w", specID, err)
	}

	path := filepath.Join(dir, item.ID+".yaml")
	return s.writeYAML(path, item)
}

// ListWorkItemsForSpec returns all work items for a specific spec
func (s *YAMLStore) ListWorkItemsForSpec(specID string) ([]*domain.WorkItem, error) {
	dir := filepath.Join(s.baseDir, "work-items", specID)

	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return []*domain.WorkItem{}, nil
		}
		return nil, fmt.Errorf("failed to read work-items directory for spec %s: %w", specID, err)
	}

	var items []*domain.WorkItem
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".yaml") {
			continue
		}

		id := strings.TrimSuffix(entry.Name(), ".yaml")
		item, err := s.LoadWorkItemForSpec(specID, id)
		if err != nil {
			return nil, fmt.Errorf("failed to load work item %s: %w", id, err)
		}
		items = append(items, item)
	}

	return items, nil
}

// LoadWorkItemForSpec reads a work item from .utopia/work-items/{specID}/{id}.yaml
func (s *YAMLStore) LoadWorkItemForSpec(specID, id string) (*domain.WorkItem, error) {
	path := filepath.Join(s.baseDir, "work-items", specID, id+".yaml")

	var item domain.WorkItem
	if err := s.readYAML(path, &item); err != nil {
		return nil, err
	}

	return &item, nil
}

// LoadWorkItem reads a work item from .utopia/work-items/{id}.yaml
func (s *YAMLStore) LoadWorkItem(id string) (*domain.WorkItem, error) {
	path := filepath.Join(s.baseDir, "work-items", id+".yaml")

	var item domain.WorkItem
	if err := s.readYAML(path, &item); err != nil {
		return nil, err
	}

	return &item, nil
}

// ListWorkItems returns all work items from both flat and nested structures.
// It searches .utopia/work-items/*.yaml (legacy) and .utopia/work-items/<spec-id>/*.yaml
func (s *YAMLStore) ListWorkItems() ([]*domain.WorkItem, error) {
	dir := filepath.Join(s.baseDir, "work-items")

	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return []*domain.WorkItem{}, nil
		}
		return nil, fmt.Errorf("failed to read work-items directory: %w", err)
	}

	var items []*domain.WorkItem
	for _, entry := range entries {
		if entry.IsDir() {
			// Check for nested work items (new format: .utopia/work-items/<spec-id>/)
			specItems, err := s.ListWorkItemsForSpec(entry.Name())
			if err != nil {
				return nil, err
			}
			items = append(items, specItems...)
			continue
		}

		if !strings.HasSuffix(entry.Name(), ".yaml") {
			continue
		}

		// Legacy flat format
		id := strings.TrimSuffix(entry.Name(), ".yaml")
		item, err := s.LoadWorkItem(id)
		if err != nil {
			return nil, fmt.Errorf("failed to load work item %s: %w", id, err)
		}
		items = append(items, item)
	}

	return items, nil
}

// SaveConfig writes the project configuration
func (s *YAMLStore) SaveConfig(config *domain.Config) error {
	path := filepath.Join(s.baseDir, "config.yaml")
	return s.writeYAML(path, config)
}

// LoadConfig reads the project configuration
func (s *YAMLStore) LoadConfig() (*domain.Config, error) {
	path := filepath.Join(s.baseDir, "config.yaml")

	var config domain.Config
	if err := s.readYAML(path, &config); err != nil {
		return nil, err
	}

	return &config, nil
}

// SaveChangeRequest writes a change request to .utopia/change-requests/{id}.yaml
func (s *YAMLStore) SaveChangeRequest(cr *domain.ChangeRequest) error {
	dir := filepath.Join(s.baseDir, "change-requests")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create change requests directory: %w", err)
	}

	path := filepath.Join(dir, cr.ID+".yaml")
	return s.writeYAML(path, cr)
}

// LoadChangeRequest reads a change request from .utopia/change-requests/{id}.yaml
func (s *YAMLStore) LoadChangeRequest(id string) (*domain.ChangeRequest, error) {
	path := filepath.Join(s.baseDir, "change-requests", id+".yaml")

	var cr domain.ChangeRequest
	if err := s.readYAML(path, &cr); err != nil {
		return nil, err
	}

	return &cr, nil
}

// DeleteChangeRequest removes a change request file from .utopia/change-requests/{id}.yaml
func (s *YAMLStore) DeleteChangeRequest(id string) error {
	path := filepath.Join(s.baseDir, "change-requests", id+".yaml")
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("failed to delete change request %s: %w", id, err)
	}
	return nil
}

// ListChangeRequests returns all change requests in the change-requests directory
func (s *YAMLStore) ListChangeRequests() ([]*domain.ChangeRequest, error) {
	dir := filepath.Join(s.baseDir, "change-requests")

	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return []*domain.ChangeRequest{}, nil
		}
		return nil, fmt.Errorf("failed to read change requests directory: %w", err)
	}

	var crs []*domain.ChangeRequest
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".yaml") {
			continue
		}
		// Skip template file
		if entry.Name() == "_template.yaml" {
			continue
		}

		id := strings.TrimSuffix(entry.Name(), ".yaml")
		cr, err := s.LoadChangeRequest(id)
		if err != nil {
			return nil, fmt.Errorf("failed to load change request %s: %w", id, err)
		}
		crs = append(crs, cr)
	}

	return crs, nil
}

// writeYAML marshals and writes data to a file
func (s *YAMLStore) writeYAML(path string, data interface{}) error {
	bytes, err := yaml.Marshal(data)
	if err != nil {
		return fmt.Errorf("failed to marshal YAML: %w", err)
	}

	// Post-process to add blank lines between features for readability
	content := addFeatureSpacing(string(bytes))

	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return fmt.Errorf("failed to write file %s: %w", path, err)
	}

	return nil
}

// featureMarshaler wraps domain.Feature to provide custom YAML marshaling.
// This keeps YAML-specific formatting logic in the storage layer rather than
// polluting domain types with serialization concerns.
type featureMarshaler struct {
	Feature domain.Feature
}

// MarshalYAML customizes YAML output for Feature to use block style
// for multi-line descriptions.
func (f featureMarshaler) MarshalYAML() (interface{}, error) {
	// Create a node structure manually to control formatting
	node := &yaml.Node{
		Kind: yaml.MappingNode,
	}

	// Add id
	node.Content = append(node.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Value: "id"},
		&yaml.Node{Kind: yaml.ScalarNode, Value: f.Feature.ID},
	)

	// Add description with block style if multi-line
	descNode := &yaml.Node{Kind: yaml.ScalarNode, Value: f.Feature.Description}
	if strings.Contains(f.Feature.Description, "\n") || len(f.Feature.Description) > 60 {
		descNode.Style = yaml.LiteralStyle // Forces | block style
	}
	node.Content = append(node.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Value: "description"},
		descNode,
	)

	// Add acceptance_criteria
	criteriaNode := &yaml.Node{Kind: yaml.SequenceNode}
	for _, c := range f.Feature.AcceptanceCriteria {
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

// specMarshaler wraps domain.Spec for custom YAML marshaling that applies
// featureMarshaler to all features while preserving the standard marshaling
// for other fields.
type specMarshaler struct {
	ID              string             `yaml:"id"`
	Title           string             `yaml:"title"`
	Created         string             `yaml:"created"`
	Updated         string             `yaml:"updated"`
	Description     string             `yaml:"description"`
	DomainKnowledge []string           `yaml:"domain_knowledge,omitempty"`
	Features        []featureMarshaler `yaml:"features"`
}

// newSpecMarshaler creates a specMarshaler from a domain.Spec
func newSpecMarshaler(spec *domain.Spec) specMarshaler {
	features := make([]featureMarshaler, len(spec.Features))
	for i, f := range spec.Features {
		features[i] = featureMarshaler{Feature: f}
	}

	return specMarshaler{
		ID:              spec.ID,
		Title:           spec.Title,
		Created:         spec.Created.Format("2006-01-02T15:04:05.999999999-07:00"),
		Updated:         spec.Updated.Format("2006-01-02T15:04:05.999999999-07:00"),
		Description:     spec.Description,
		DomainKnowledge: spec.DomainKnowledge,
		Features:        features,
	}
}

// addFeatureSpacing inserts blank lines between features in YAML output
// This makes the output more readable by separating feature blocks
func addFeatureSpacing(content string) string {
	// Split into lines
	lines := strings.Split(content, "\n")
	var result []string
	inFeatures := false
	firstFeature := true

	for _, line := range lines {
		// Detect when we enter the features section
		if strings.HasPrefix(line, "features:") {
			inFeatures = true
			result = append(result, line)
			firstFeature = true
			continue
		}

		// Detect when we leave the features section (non-indented line after features)
		if inFeatures && len(line) > 0 && line[0] != ' ' && !strings.HasPrefix(line, "features:") {
			inFeatures = false
		}

		// Add blank line before each feature (except the first one)
		// Match "    - id:" pattern (4-space indent typical of yaml.Marshal)
		trimmed := strings.TrimLeft(line, " ")
		if inFeatures && strings.HasPrefix(trimmed, "- id:") {
			if !firstFeature {
				// Check if previous line isn't already blank
				if len(result) > 0 && strings.TrimSpace(result[len(result)-1]) != "" {
					result = append(result, "")
				}
			}
			firstFeature = false
		}

		result = append(result, line)
	}

	return strings.Join(result, "\n")
}

// readYAML reads and unmarshals a file
func (s *YAMLStore) readYAML(path string, dest interface{}) error {
	bytes, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("failed to read file %s: %w", path, err)
	}

	if err := yaml.Unmarshal(bytes, dest); err != nil {
		return fmt.Errorf("failed to unmarshal YAML from %s: %w", path, err)
	}

	return nil
}

// SaveConversation writes a conversation to .utopia/conversations/{id}.yaml
func (s *YAMLStore) SaveConversation(conv *domain.Conversation) error {
	dir := filepath.Join(s.baseDir, "conversations")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create conversations directory: %w", err)
	}

	path := filepath.Join(dir, conv.ID+".yaml")
	return s.writeYAML(path, conv)
}

// LoadConversation reads a conversation from .utopia/conversations/{id}.yaml
func (s *YAMLStore) LoadConversation(id string) (*domain.Conversation, error) {
	path := filepath.Join(s.baseDir, "conversations", id+".yaml")

	var conv domain.Conversation
	if err := s.readYAML(path, &conv); err != nil {
		return nil, err
	}

	return &conv, nil
}

// ListConversations returns all conversations in the conversations directory
func (s *YAMLStore) ListConversations() ([]*domain.Conversation, error) {
	dir := filepath.Join(s.baseDir, "conversations")

	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return []*domain.Conversation{}, nil
		}
		return nil, fmt.Errorf("failed to read conversations directory: %w", err)
	}

	var convs []*domain.Conversation
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".yaml") {
			continue
		}

		id := strings.TrimSuffix(entry.Name(), ".yaml")
		conv, err := s.LoadConversation(id)
		if err != nil {
			return nil, fmt.Errorf("failed to load conversation %s: %w", id, err)
		}
		convs = append(convs, conv)
	}

	return convs, nil
}

// ListUnprocessedConversations returns conversations with status "unprocessed"
func (s *YAMLStore) ListUnprocessedConversations() ([]*domain.Conversation, error) {
	all, err := s.ListConversations()
	if err != nil {
		return nil, err
	}

	var unprocessed []*domain.Conversation
	for _, conv := range all {
		if conv.Status == domain.ConversationUnprocessed {
			unprocessed = append(unprocessed, conv)
		}
	}

	return unprocessed, nil
}

// ListUnprocessedConversationsByType returns unprocessed conversations filtered by type.
// Use domain.ConversationSystemTruth for conversations with executed CRs.
// Use domain.ConversationExploratory for conversations without CRs.
func (s *YAMLStore) ListUnprocessedConversationsByType(convType domain.ConversationType) ([]*domain.Conversation, error) {
	unprocessed, err := s.ListUnprocessedConversations()
	if err != nil {
		return nil, err
	}

	var filtered []*domain.Conversation
	for _, conv := range unprocessed {
		if conv.Type() == convType {
			filtered = append(filtered, conv)
		}
	}

	return filtered, nil
}

// MarkConversationsReadyForHarvest transitions conversations that reference the given CR
// from pending-execution to unprocessed status, making them eligible for harvest.
func (s *YAMLStore) MarkConversationsReadyForHarvest(crID string) error {
	all, err := s.ListConversations()
	if err != nil {
		return err
	}

	for _, conv := range all {
		if conv.Status != domain.ConversationPendingExecution {
			continue
		}

		// Check if this conversation references the executed CR
		for _, crCommit := range conv.CRsCreated {
			if crCommit.CRID == crID {
				conv.Status = domain.ConversationUnprocessed
				if err := s.SaveConversation(conv); err != nil {
					return fmt.Errorf("failed to update conversation %s: %w", conv.ID, err)
				}
				break
			}
		}
	}

	return nil
}

// LoadConversationsByCRID returns all conversations that reference the given CR ID.
// This is used during execution to append log entries to conversations.
func (s *YAMLStore) LoadConversationsByCRID(crID string) ([]*domain.Conversation, error) {
	all, err := s.ListConversations()
	if err != nil {
		return nil, err
	}

	var matching []*domain.Conversation
	for _, conv := range all {
		for _, crCommit := range conv.CRsCreated {
			if crCommit.CRID == crID {
				matching = append(matching, conv)
				break
			}
		}
	}

	return matching, nil
}

// AppendExecutionLogEntry adds a log entry to all conversations that reference the given CR.
// Also updates conversation status from pending-execution to unprocessed.
func (s *YAMLStore) AppendExecutionLogEntry(crID string, entry domain.ExecutionLogEntry) error {
	convs, err := s.LoadConversationsByCRID(crID)
	if err != nil {
		return err
	}

	for _, conv := range convs {
		conv.ExecutionLog = append(conv.ExecutionLog, entry)
		// Update status from pending-execution to unprocessed
		if conv.Status == domain.ConversationPendingExecution {
			conv.Status = domain.ConversationUnprocessed
		}
		if err := s.SaveConversation(conv); err != nil {
			return fmt.Errorf("failed to update conversation %s: %w", conv.ID, err)
		}
	}

	return nil
}

// SaveADR writes an ADR to .utopia/adrs/{id}.yaml
// Returns an error if the ADR fails validation (e.g., invalid category).
func (s *YAMLStore) SaveADR(adr *domain.ADR) error {
	if err := adr.Validate(); err != nil {
		return fmt.Errorf("ADR validation failed: %w", err)
	}

	dir := filepath.Join(s.baseDir, "adrs")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create adrs directory: %w", err)
	}

	path := filepath.Join(dir, adr.ID+".yaml")
	return s.writeYAML(path, adr)
}

// LoadADR reads an ADR from .utopia/adrs/{id}.yaml
func (s *YAMLStore) LoadADR(id string) (*domain.ADR, error) {
	path := filepath.Join(s.baseDir, "adrs", id+".yaml")

	var adr domain.ADR
	if err := s.readYAML(path, &adr); err != nil {
		return nil, err
	}

	return &adr, nil
}

// ListADRs returns all ADRs in the adrs directory
func (s *YAMLStore) ListADRs() ([]*domain.ADR, error) {
	dir := filepath.Join(s.baseDir, "adrs")

	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return []*domain.ADR{}, nil
		}
		return nil, fmt.Errorf("failed to read adrs directory: %w", err)
	}

	var adrs []*domain.ADR
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".yaml") {
			continue
		}

		id := strings.TrimSuffix(entry.Name(), ".yaml")
		adr, err := s.LoadADR(id)
		if err != nil {
			return nil, fmt.Errorf("failed to load ADR %s: %w", id, err)
		}
		adrs = append(adrs, adr)
	}

	return adrs, nil
}

// SaveDomainDoc writes a domain doc to .utopia/domain/{id}.yaml
func (s *YAMLStore) SaveDomainDoc(doc *domain.DomainDoc) error {
	dir := filepath.Join(s.baseDir, "domain")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create domain directory: %w", err)
	}

	path := filepath.Join(dir, doc.ID+".yaml")
	return s.writeYAML(path, doc)
}

// LoadDomainDoc reads a domain doc from .utopia/domain/{id}.yaml
func (s *YAMLStore) LoadDomainDoc(id string) (*domain.DomainDoc, error) {
	path := filepath.Join(s.baseDir, "domain", id+".yaml")

	var doc domain.DomainDoc
	if err := s.readYAML(path, &doc); err != nil {
		return nil, err
	}

	return &doc, nil
}

// ListDomainDocs returns all domain docs in the domain directory
func (s *YAMLStore) ListDomainDocs() ([]*domain.DomainDoc, error) {
	dir := filepath.Join(s.baseDir, "domain")

	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return []*domain.DomainDoc{}, nil
		}
		return nil, fmt.Errorf("failed to read domain directory: %w", err)
	}

	var docs []*domain.DomainDoc
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".yaml") {
			continue
		}

		id := strings.TrimSuffix(entry.Name(), ".yaml")
		doc, err := s.LoadDomainDoc(id)
		if err != nil {
			return nil, fmt.Errorf("failed to load domain doc %s: %w", id, err)
		}
		docs = append(docs, doc)
	}

	return docs, nil
}

// SaveConceptDoc writes a concept doc to .utopia/concepts/{id}.md
// Concepts use Markdown with YAML frontmatter for readability and sharing.
func (s *YAMLStore) SaveConceptDoc(doc *domain.ConceptDoc) error {
	dir := filepath.Join(s.baseDir, "concepts")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create concepts directory: %w", err)
	}

	// Build YAML frontmatter
	frontmatter, err := yaml.Marshal(doc)
	if err != nil {
		return fmt.Errorf("failed to marshal concept frontmatter: %w", err)
	}

	// Combine frontmatter and content
	content := fmt.Sprintf("---\n%s---\n\n%s", string(frontmatter), doc.Content)

	path := filepath.Join(dir, doc.ID+".md")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return fmt.Errorf("failed to write concept file %s: %w", path, err)
	}

	return nil
}

// LoadConceptDoc reads a concept doc from .utopia/concepts/{id}.md
func (s *YAMLStore) LoadConceptDoc(id string) (*domain.ConceptDoc, error) {
	path := filepath.Join(s.baseDir, "concepts", id+".md")

	bytes, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read concept file %s: %w", path, err)
	}

	content := string(bytes)

	// Parse frontmatter (between --- markers)
	if !strings.HasPrefix(content, "---\n") {
		return nil, fmt.Errorf("concept file %s missing YAML frontmatter", path)
	}

	// Find the closing ---
	endMarker := strings.Index(content[4:], "\n---")
	if endMarker == -1 {
		return nil, fmt.Errorf("concept file %s has unclosed YAML frontmatter", path)
	}

	frontmatterStr := content[4 : 4+endMarker]
	bodyStart := 4 + endMarker + 4 // Skip past "\n---"

	var doc domain.ConceptDoc
	if err := yaml.Unmarshal([]byte(frontmatterStr), &doc); err != nil {
		return nil, fmt.Errorf("failed to unmarshal concept frontmatter from %s: %w", path, err)
	}

	// Extract body content (skip leading newlines)
	if bodyStart < len(content) {
		doc.Content = strings.TrimPrefix(content[bodyStart:], "\n")
	}

	return &doc, nil
}

// ListConceptDocs returns all concept docs in the concepts directory
func (s *YAMLStore) ListConceptDocs() ([]*domain.ConceptDoc, error) {
	dir := filepath.Join(s.baseDir, "concepts")

	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return []*domain.ConceptDoc{}, nil
		}
		return nil, fmt.Errorf("failed to read concepts directory: %w", err)
	}

	var docs []*domain.ConceptDoc
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}

		id := strings.TrimSuffix(entry.Name(), ".md")
		doc, err := s.LoadConceptDoc(id)
		if err != nil {
			return nil, fmt.Errorf("failed to load concept doc %s: %w", id, err)
		}
		docs = append(docs, doc)
	}

	return docs, nil
}

// SaveDraft writes a draft spec to .utopia/drafts/{id}.yaml
func (s *YAMLStore) SaveDraft(draft *domain.DraftSpec) error {
	dir := filepath.Join(s.baseDir, "drafts")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create drafts directory: %w", err)
	}

	path := filepath.Join(dir, draft.ID+".yaml")
	return s.writeYAML(path, draft)
}

// LoadDraft reads a draft spec from .utopia/drafts/{id}.yaml
func (s *YAMLStore) LoadDraft(id string) (*domain.DraftSpec, error) {
	path := filepath.Join(s.baseDir, "drafts", id+".yaml")

	var draft domain.DraftSpec
	if err := s.readYAML(path, &draft); err != nil {
		if os.IsNotExist(err) {
			return nil, &domain.NotFoundError{Resource: "draft", ID: id}
		}
		return nil, err
	}

	return &draft, nil
}

// ListDrafts returns all draft specs in the drafts directory
func (s *YAMLStore) ListDrafts() ([]*domain.DraftSpec, error) {
	dir := filepath.Join(s.baseDir, "drafts")

	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return []*domain.DraftSpec{}, nil
		}
		return nil, fmt.Errorf("failed to read drafts directory: %w", err)
	}

	var drafts []*domain.DraftSpec
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".yaml") {
			continue
		}

		id := strings.TrimSuffix(entry.Name(), ".yaml")
		draft, err := s.LoadDraft(id)
		if err != nil {
			return nil, fmt.Errorf("failed to load draft %s: %w", id, err)
		}
		drafts = append(drafts, draft)
	}

	return drafts, nil
}

// DeleteDraft removes a draft spec file from .utopia/drafts/{id}.yaml
func (s *YAMLStore) DeleteDraft(id string) error {
	path := filepath.Join(s.baseDir, "drafts", id+".yaml")
	if err := os.Remove(path); err != nil {
		if os.IsNotExist(err) {
			return &domain.NotFoundError{Resource: "draft", ID: id}
		}
		return fmt.Errorf("failed to delete draft %s: %w", id, err)
	}
	return nil
}
