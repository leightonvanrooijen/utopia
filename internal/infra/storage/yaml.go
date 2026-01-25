package storage

import (
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

// SaveChangeRequest writes a change request to .utopia/specs/_changerequests/{id}.yaml
func (s *YAMLStore) SaveChangeRequest(cr *domain.ChangeRequest) error {
	dir := filepath.Join(s.baseDir, "specs", "_changerequests")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create change requests directory: %w", err)
	}

	path := filepath.Join(dir, cr.ID+".yaml")
	return s.writeYAML(path, cr)
}

// LoadChangeRequest reads a change request from .utopia/specs/_changerequests/{id}.yaml
func (s *YAMLStore) LoadChangeRequest(id string) (*domain.ChangeRequest, error) {
	path := filepath.Join(s.baseDir, "specs", "_changerequests", id+".yaml")

	var cr domain.ChangeRequest
	if err := s.readYAML(path, &cr); err != nil {
		return nil, err
	}

	return &cr, nil
}

// ListChangeRequests returns all change requests in the _changerequests directory
func (s *YAMLStore) ListChangeRequests() ([]*domain.ChangeRequest, error) {
	dir := filepath.Join(s.baseDir, "specs", "_changerequests")

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

	if err := os.WriteFile(path, bytes, 0644); err != nil {
		return fmt.Errorf("failed to write file %s: %w", path, err)
	}

	return nil
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
