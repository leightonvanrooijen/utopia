package storage

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/leightonvanrooijen/utopia/internal/domain"
	"gopkg.in/yaml.v3"
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
	return s.writeYAML(path, spec)
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
			return fmt.Errorf("spec not found: %s", id)
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

// featurePattern matches the start of a feature in YAML (indented "- id:")
var featurePattern = regexp.MustCompile(`(?m)^(\s+- id:)`)

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

// SaveADR writes an ADR to .utopia/adrs/{id}.yaml
func (s *YAMLStore) SaveADR(adr *domain.ADR) error {
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
