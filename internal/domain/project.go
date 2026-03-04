package domain

import "path/filepath"

// Project represents a Utopia-managed project
type Project struct {
	// Root directory of the project
	RootDir string

	// Configuration
	Config *Config
}

// Config holds project-level configuration
type Config struct {
	ProjectContext string             `yaml:"project_context,omitempty"`
	Strategies     StrategyConfig     `yaml:"strategies"`
	Verification   VerificationConfig `yaml:"verification"`
}

// VerificationConfig holds verification command settings
type VerificationConfig struct {
	// Command to run for verification (e.g., "npm test --onlyFailures")
	// User is responsible for configuring command to output failures only
	Command string `yaml:"command"`
	// MaxIterations limits retry attempts per work item (0 = unlimited)
	MaxIterations int `yaml:"max_iterations,omitempty"`
}

// StrategyConfig specifies which strategies to use
type StrategyConfig struct {
	Spec    string `yaml:"spec"`    // e.g., "guided", "minimal", "template"
	Chunk   string `yaml:"chunk"`   // e.g., "simple", "llm", "atomic"
	Execute string `yaml:"execute"` // e.g., "sequential", "parallel", "supervised"
}

// DefaultConfig returns sensible defaults
func DefaultConfig() *Config {
	return &Config{
		Strategies: StrategyConfig{
			Spec:    "guided",
			Chunk:   "ralph-sequential",
			Execute: "sequential",
		},
	}
}

// UtopiaDir returns the .utopia directory path
func (p *Project) UtopiaDir() string {
	return filepath.Join(p.RootDir, ".utopia")
}

// SpecsDir returns the specs directory path
func (p *Project) SpecsDir() string {
	return filepath.Join(p.UtopiaDir(), "specs")
}

// WorkItemsDir returns the work-items directory path
func (p *Project) WorkItemsDir() string {
	return filepath.Join(p.UtopiaDir(), "work-items")
}

// ConfigPath returns the config file path
func (p *Project) ConfigPath() string {
	return filepath.Join(p.UtopiaDir(), "config.yaml")
}

// RefactorsDir returns the refactors directory path
func (p *Project) RefactorsDir() string {
	return filepath.Join(p.UtopiaDir(), "refactors")
}

// ChangeRequestsDir returns the change-requests directory path
func (p *Project) ChangeRequestsDir() string {
	return filepath.Join(p.UtopiaDir(), "change-requests")
}

// ConversationsDir returns the conversations directory path
func (p *Project) ConversationsDir() string {
	return filepath.Join(p.UtopiaDir(), "conversations")
}
