package internal

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/leightonvanrooijen/utopia/internal/domain"
	"gopkg.in/yaml.v3"
)

// YAMLStore handles reading and writing YAML files
type YAMLStore struct {
	baseDir string
	// Absolute artifact directories, resolved from the optional paths section
	// in config.yaml. Default to specs/, adrs/, concepts/, and domain/ under baseDir.
	specsDir    string
	adrsDir     string
	conceptsDir string
	domainDir   string
}

// NewYAMLStore creates a store rooted at the given directory.
// Artifact directories (specs, adrs, concepts, domain) default to their
// standard locations under baseDir and can be overridden via the optional
// paths section in config.yaml.
func NewYAMLStore(baseDir string) *YAMLStore {
	s := &YAMLStore{
		baseDir:     baseDir,
		specsDir:    filepath.Join(baseDir, "specs"),
		adrsDir:     filepath.Join(baseDir, "adrs"),
		conceptsDir: filepath.Join(baseDir, "concepts"),
		domainDir:   filepath.Join(baseDir, "domain"),
	}
	s.applyConfiguredPaths()
	return s
}

// applyConfiguredPaths overrides artifact directories from the optional paths
// section in config.yaml. A missing or unreadable config keeps the defaults,
// so projects without a paths section behave exactly as before.
func (s *YAMLStore) applyConfiguredPaths() {
	config, err := Load[domain.Config](s, "config.yaml")
	if err != nil || config.Paths == nil {
		return
	}

	// baseDir is always <project root>/.utopia, so relative configured paths
	// resolve from the project root; absolute paths are used as-is.
	projectRoot := filepath.Dir(s.baseDir)
	resolve := func(configured string, dir *string) {
		if configured == "" {
			return
		}
		if filepath.IsAbs(configured) {
			*dir = configured
			return
		}
		*dir = filepath.Join(projectRoot, configured)
	}

	resolve(config.Paths.Specs, &s.specsDir)
	resolve(config.Paths.ADRs, &s.adrsDir)
	resolve(config.Paths.Concepts, &s.conceptsDir)
	resolve(config.Paths.Domain, &s.domainDir)
}

// SpecsDir returns the absolute directory where specs are stored.
func (s *YAMLStore) SpecsDir() string { return s.specsDir }

// ADRsDir returns the absolute directory where ADRs are stored.
func (s *YAMLStore) ADRsDir() string { return s.adrsDir }

// ConceptsDir returns the absolute directory where concept docs are stored.
func (s *YAMLStore) ConceptsDir() string { return s.conceptsDir }

// DomainDir returns the absolute directory where domain docs are stored.
func (s *YAMLStore) DomainDir() string { return s.domainDir }

// fullPath resolves a path against the store's base directory.
// Absolute paths (already-resolved artifact locations) are used as-is.
func (s *YAMLStore) fullPath(path string) string {
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(s.baseDir, path)
}

// Storable is implemented by types that can be persisted to the store.
// Types must have an ID field that determines the filename.
type Storable interface {
	GetID() string
}

// Load reads a YAML file and unmarshals it into T.
// The path should be relative to the store's base directory (e.g., "specs/my-spec.yaml").
func Load[T any](s *YAMLStore, path string) (*T, error) {
	fullPath := s.fullPath(path)

	var dest T
	if err := s.readYAML(fullPath, &dest); err != nil {
		return nil, err
	}

	return &dest, nil
}

// Save marshals data to YAML and writes it to the specified path.
// Creates parent directories if they don't exist.
// The path should be relative to the store's base directory.
func Save[T any](s *YAMLStore, path string, data *T) error {
	fullPath := s.fullPath(path)
	dir := filepath.Dir(fullPath)

	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory %s: %w", dir, err)
	}

	return s.writeYAML(fullPath, data)
}

// List reads all YAML files in a directory and returns them as a slice.
// Skips directories and non-YAML files.
// The dir should be relative to the store's base directory.
func List[T any](s *YAMLStore, dir string) ([]*T, error) {
	fullDir := s.fullPath(dir)

	entries, err := os.ReadDir(fullDir)
	if err != nil {
		if os.IsNotExist(err) {
			return []*T{}, nil
		}
		return nil, fmt.Errorf("failed to read directory %s: %w", dir, err)
	}

	var items []*T
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".yaml") {
			continue
		}

		path := filepath.Join(dir, entry.Name())
		item, err := Load[T](s, path)
		if err != nil {
			return nil, fmt.Errorf("failed to load %s: %w", entry.Name(), err)
		}
		items = append(items, item)
	}

	return items, nil
}

// Delete removes a file at the specified path.
// The path should be relative to the store's base directory.
func Delete(s *YAMLStore, path string, resourceType, id string) error {
	fullPath := s.fullPath(path)
	if err := os.Remove(fullPath); err != nil {
		if os.IsNotExist(err) {
			return &domain.NotFoundError{Resource: resourceType, ID: id}
		}
		return fmt.Errorf("failed to delete %s %s: %w", resourceType, id, err)
	}
	return nil
}

// SaveSpec writes a spec to {specsDir}/{id}.yaml (default .utopia/specs/).
// Uses custom marshaling to preserve feature spacing and block style.
func (s *YAMLStore) SaveSpec(spec *domain.Spec) error {
	if err := os.MkdirAll(s.specsDir, 0755); err != nil {
		return fmt.Errorf("failed to create specs directory: %w", err)
	}

	path := filepath.Join(s.specsDir, spec.ID+".yaml")
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

// LoadSpec reads a spec from {specsDir}/{id}.yaml (default .utopia/specs/)
func (s *YAMLStore) LoadSpec(id string) (*domain.Spec, error) {
	return Load[domain.Spec](s, filepath.Join(s.specsDir, id+".yaml"))
}

// DeleteSpec removes a spec file from {specsDir}/{id}.yaml (default .utopia/specs/)
func (s *YAMLStore) DeleteSpec(id string) error {
	return Delete(s, filepath.Join(s.specsDir, id+".yaml"), "spec", id)
}

// ListSpecs returns all specs in the specs directory
func (s *YAMLStore) ListSpecs() ([]*domain.Spec, error) {
	return List[domain.Spec](s, s.specsDir)
}

// SaveWorkItemForSpec writes a work item to .utopia/work-items/{specID}/{id}.yaml
func (s *YAMLStore) SaveWorkItemForSpec(specID string, item *domain.WorkItem) error {
	return Save(s, filepath.Join("work-items", specID, item.ID+".yaml"), item)
}

// ListWorkItemsForSpec returns all work items for a specific spec
func (s *YAMLStore) ListWorkItemsForSpec(specID string) ([]*domain.WorkItem, error) {
	return List[domain.WorkItem](s, filepath.Join("work-items", specID))
}

// LoadWorkItem reads a work item from .utopia/work-items/{id}.yaml
func (s *YAMLStore) LoadWorkItem(id string) (*domain.WorkItem, error) {
	return Load[domain.WorkItem](s, filepath.Join("work-items", id+".yaml"))
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
	return Save(s, "config.yaml", config)
}

// LoadConfig reads the project configuration.
// Returns an error if any configured model names are invalid.
func (s *YAMLStore) LoadConfig() (*domain.Config, error) {
	config, err := Load[domain.Config](s, "config.yaml")
	if err != nil {
		return nil, err
	}

	// Validate model names at load time
	if err := domain.ValidateModelConfig(config.Models); err != nil {
		return nil, fmt.Errorf("failed to load config: %w", err)
	}

	// Validate connector entries at load time
	if err := domain.ValidateConnectors(config.Connectors); err != nil {
		return nil, fmt.Errorf("failed to load config: %w", err)
	}

	return config, nil
}

// SaveChangeRequest writes a change request to .utopia/change-requests/{id}.yaml
func (s *YAMLStore) SaveChangeRequest(cr *domain.ChangeRequest) error {
	return Save(s, filepath.Join("change-requests", cr.ID+".yaml"), cr)
}

// LoadChangeRequest reads a change request from .utopia/change-requests/{id}.yaml
func (s *YAMLStore) LoadChangeRequest(id string) (*domain.ChangeRequest, error) {
	return Load[domain.ChangeRequest](s, filepath.Join("change-requests", id+".yaml"))
}

// DeleteChangeRequest removes a change request file from .utopia/change-requests/{id}.yaml
func (s *YAMLStore) DeleteChangeRequest(id string) error {
	return Delete(s, filepath.Join("change-requests", id+".yaml"), "change request", id)
}

// ListChangeRequests returns all change requests in the change-requests directory.
// Skips _template.yaml if present.
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
	return Save(s, filepath.Join("conversations", conv.ID+".yaml"), conv)
}

// ListConversations returns all conversations in the conversations directory
func (s *YAMLStore) ListConversations() ([]*domain.Conversation, error) {
	return List[domain.Conversation](s, "conversations")
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

// ListADRs returns all ADRs in the adrs directory
func (s *YAMLStore) ListADRs() ([]*domain.ADR, error) {
	return List[domain.ADR](s, s.adrsDir)
}

// SaveDomainDoc writes a domain doc to {domainDir}/{id}.yaml (default .utopia/domain/)
func (s *YAMLStore) SaveDomainDoc(doc *domain.DomainDoc) error {
	return Save(s, filepath.Join(s.domainDir, doc.ID+".yaml"), doc)
}

// LoadDomainDoc reads a domain doc from {domainDir}/{id}.yaml (default .utopia/domain/)
func (s *YAMLStore) LoadDomainDoc(id string) (*domain.DomainDoc, error) {
	return Load[domain.DomainDoc](s, filepath.Join(s.domainDir, id+".yaml"))
}

// ListDomainDocs returns all domain docs in the domain directory
func (s *YAMLStore) ListDomainDocs() ([]*domain.DomainDoc, error) {
	return List[domain.DomainDoc](s, s.domainDir)
}

// LoadConceptDoc reads a concept doc from {conceptsDir}/{id}.md (default .utopia/concepts/)
func (s *YAMLStore) LoadConceptDoc(id string) (*domain.ConceptDoc, error) {
	path := filepath.Join(s.conceptsDir, id+".md")

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
	dir := s.conceptsDir

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

// LoadStandardsIndex reads the frontmatter metadata of every standards doc
// in .utopia/standards/. Docs with missing or unparseable frontmatter are
// skipped so a single bad doc never fails chunking. Returns an empty index
// when the directory is missing or empty.
func (s *YAMLStore) LoadStandardsIndex() []domain.StandardsDocMeta {
	dir := filepath.Join(s.baseDir, "standards")

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}

	var docs []domain.StandardsDocMeta
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}

		bytes, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			continue
		}

		content := string(bytes)
		if !strings.HasPrefix(content, "---\n") {
			continue
		}
		endMarker := strings.Index(content[4:], "\n---")
		if endMarker == -1 {
			continue
		}

		var meta domain.StandardsDocMeta
		if err := yaml.Unmarshal([]byte(content[4:4+endMarker]), &meta); err != nil {
			continue
		}
		if meta.ID == "" {
			continue
		}

		meta.Path = filepath.ToSlash(filepath.Join(".utopia", "standards", entry.Name()))
		docs = append(docs, meta)
	}

	return docs
}

// SaveDraft writes a draft spec to .utopia/drafts/specs/{id}.yaml
func (s *YAMLStore) SaveDraft(draft *domain.DraftSpec) error {
	return Save(s, filepath.Join("drafts", "specs", draft.ID+".yaml"), draft)
}

// LoadDraft reads a draft spec from .utopia/drafts/specs/{id}.yaml
func (s *YAMLStore) LoadDraft(id string) (*domain.DraftSpec, error) {
	return Load[domain.DraftSpec](s, filepath.Join("drafts", "specs", id+".yaml"))
}

// ListDrafts returns all draft specs in the drafts/specs directory
func (s *YAMLStore) ListDrafts() ([]*domain.DraftSpec, error) {
	return List[domain.DraftSpec](s, filepath.Join("drafts", "specs"))
}

// DeleteDraft removes a draft spec file from .utopia/drafts/specs/{id}.yaml
func (s *YAMLStore) DeleteDraft(id string) error {
	return Delete(s, filepath.Join("drafts", "specs", id+".yaml"), "draft", id)
}

// SaveDraftDomainDoc writes a draft domain doc to .utopia/drafts/domain/{id}.yaml
func (s *YAMLStore) SaveDraftDomainDoc(draft *domain.DraftDomainDoc) error {
	return Save(s, filepath.Join("drafts", "domain", draft.ID+".yaml"), draft)
}

// LoadDraftDomainDoc reads a draft domain doc from .utopia/drafts/domain/{id}.yaml
func (s *YAMLStore) LoadDraftDomainDoc(id string) (*domain.DraftDomainDoc, error) {
	return Load[domain.DraftDomainDoc](s, filepath.Join("drafts", "domain", id+".yaml"))
}

// ListDraftDomainDocs returns all draft domain docs in the drafts/domain directory.
// Skips the .discovery-state file.
func (s *YAMLStore) ListDraftDomainDocs() ([]*domain.DraftDomainDoc, error) {
	dir := filepath.Join(s.baseDir, "drafts", "domain")

	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return []*domain.DraftDomainDoc{}, nil
		}
		return nil, fmt.Errorf("failed to read drafts/domain directory: %w", err)
	}

	var drafts []*domain.DraftDomainDoc
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".yaml") {
			continue
		}
		// Skip discovery state file
		if entry.Name() == ".discovery-state" {
			continue
		}

		id := strings.TrimSuffix(entry.Name(), ".yaml")
		draft, err := s.LoadDraftDomainDoc(id)
		if err != nil {
			return nil, fmt.Errorf("failed to load draft domain doc %s: %w", id, err)
		}
		drafts = append(drafts, draft)
	}

	return drafts, nil
}

// DeleteDraftDomainDoc removes a draft domain doc file from .utopia/drafts/domain/{id}.yaml
func (s *YAMLStore) DeleteDraftDomainDoc(id string) error {
	return Delete(s, filepath.Join("drafts", "domain", id+".yaml"), "draft domain doc", id)
}

// LoadDomainDiscoveryState reads domain discovery state from .utopia/drafts/domain/.discovery-state
// Returns nil (no error) if no previous state exists.
func (s *YAMLStore) LoadDomainDiscoveryState() (*domain.DomainDiscoveryState, error) {
	state, err := Load[domain.DomainDiscoveryState](s, filepath.Join("drafts", "domain", ".discovery-state"))
	if err != nil && errors.Is(err, os.ErrNotExist) {
		return nil, nil // No previous state exists
	}
	return state, err
}

// SaveDomainDiscoveryState writes domain discovery state to .utopia/drafts/domain/.discovery-state
func (s *YAMLStore) SaveDomainDiscoveryState(state *domain.DomainDiscoveryState) error {
	return Save(s, filepath.Join("drafts", "domain", ".discovery-state"), state)
}

// ValidateValidatorPaths checks that all configured validator file paths exist.
// Returns nil if all paths exist (or if validators slice is empty).
// Returns a clear error message listing any missing files.
func (s *YAMLStore) ValidateValidatorPaths(validators []domain.ValidatorConfig) error {
	var missing []string
	for _, vc := range validators {
		fullPath := filepath.Join(s.baseDir, vc.GetPath())
		if _, err := os.Stat(fullPath); os.IsNotExist(err) {
			missing = append(missing, vc.GetPath())
		}
	}

	if len(missing) > 0 {
		if len(missing) == 1 {
			return fmt.Errorf("validator file not found: %s", missing[0])
		}
		return fmt.Errorf("validator files not found: %s", strings.Join(missing, ", "))
	}

	return nil
}

// DeleteValidator removes a validator file from .utopia/validators/.
// The path should be relative to the store's base directory (e.g., "validators/my-validator.md").
func (s *YAMLStore) DeleteValidator(path string) error {
	return Delete(s, path, "validator", path)
}

// validatorFrontmatter is used for parsing validator file frontmatter.
// It includes the deprecated 'run' field to detect and warn about its usage.
type validatorFrontmatter struct {
	ID           string   `yaml:"id"`
	Description  string   `yaml:"description,omitempty"`
	AllowedTools []string `yaml:"allowed_tools,omitempty"`
	Run          string   `yaml:"run,omitempty"` // deprecated: configure in config.yaml instead
}

// LoadValidator reads a validator from a .md file with YAML frontmatter.
// The path should be relative to the store's base directory (e.g., "validators/component-standards.md").
// Returns the validator with frontmatter fields populated and Prompt containing the markdown body.
//
// Valid frontmatter fields: id, description, allowed_tools
// The "run" field is deprecated in validator files and should be configured in config.yaml.
// If "run" is found in the file, a warning is logged but the file continues to load.
func (s *YAMLStore) LoadValidator(path string) (*domain.Validator, error) {
	fullPath := filepath.Join(s.baseDir, path)

	bytes, err := os.ReadFile(fullPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read validator file %s: %w", path, err)
	}

	content := string(bytes)

	// Parse frontmatter (between --- markers)
	if !strings.HasPrefix(content, "---\n") {
		return nil, fmt.Errorf("validator file %s missing YAML frontmatter", path)
	}

	// Find the closing ---
	endMarker := strings.Index(content[4:], "\n---")
	if endMarker == -1 {
		return nil, fmt.Errorf("validator file %s has unclosed YAML frontmatter", path)
	}

	frontmatterStr := content[4 : 4+endMarker]
	bodyStart := 4 + endMarker + 4 // Skip past "\n---"

	var fm validatorFrontmatter
	if err := yaml.Unmarshal([]byte(frontmatterStr), &fm); err != nil {
		return nil, fmt.Errorf("failed to unmarshal validator frontmatter from %s: %w", path, err)
	}

	// Validate required fields
	if fm.ID == "" {
		return nil, fmt.Errorf("validator file %s missing required 'id' field in frontmatter", path)
	}

	// Warn if deprecated 'run' field is present
	if fm.Run != "" {
		fmt.Fprintf(os.Stderr, "Warning: validator file %s contains 'run' field which is deprecated; configure 'run' in config.yaml instead\n", path)
	}

	// Build validator (without deprecated run field)
	validator := &domain.Validator{
		ID:           fm.ID,
		Description:  fm.Description,
		AllowedTools: fm.AllowedTools,
	}

	// Extract body content (skip leading newlines)
	if bodyStart < len(content) {
		validator.Prompt = strings.TrimPrefix(content[bodyStart:], "\n")
	}

	return validator, nil
}
