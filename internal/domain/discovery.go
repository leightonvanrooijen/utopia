package domain

import (
	"fmt"
	"strings"
	"time"
)

// =============================================================================
// Project and Config Types
// =============================================================================

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

// DefaultConfig returns sensible defaults
func DefaultConfig() *Config {
	return &Config{}
}

// =============================================================================
// Error Types
// =============================================================================

// NotFoundError indicates a resource could not be found.
type NotFoundError struct {
	Resource string
	ID       string
}

func (e *NotFoundError) Error() string {
	return fmt.Sprintf("%s not found: %s", e.Resource, e.ID)
}

// Is allows errors.Is to match any NotFoundError regardless of resource/id.
func (e *NotFoundError) Is(target error) bool {
	_, ok := target.(*NotFoundError)
	return ok
}

// =============================================================================
// Spec Qualification Types
// =============================================================================

// SpecQualificationCriteria defines what makes a candidate spec-worthy.
// Core principle: Specs answer "what can I do?" not "how is it built?"
//
// Qualification criteria are applied by the Qualifier Agent during discovery
// to filter candidates before they become draft specifications.
type SpecQualificationCriteria struct{}

// QualificationResult represents whether a candidate passes qualification
// and why.
type QualificationResult struct {
	Qualified bool
	Reason    string
}

// Qualifications returns the criteria that must ALL be satisfied for a
// candidate to become a draft spec.
//
// A spec-worthy candidate:
//  1. Describes a user-observable capability (command, output, interaction, behavior)
//  2. Can be verified by using the system (testable from outside)
//  3. Represents a coherent, bounded feature users care about
//  4. Description focuses on WHAT users can achieve, not HOW it's implemented
func (c SpecQualificationCriteria) Qualifications() []string {
	return []string{
		"Capability is user-observable (command, output, interaction, behavior)",
		"Capability can be verified by using the system (testable from outside)",
		"Capability represents a coherent bounded feature users care about",
		"Description focuses on WHAT users can achieve, not HOW it's implemented",
	}
}

// AcceptanceCriteriaRequirements returns the constraints on acceptance criteria
// for qualified specs.
func (c SpecQualificationCriteria) AcceptanceCriteriaRequirements() []string {
	return []string{
		"Acceptance criteria describe observable outcomes, not internal state",
	}
}

// Disqualifications returns criteria where ANY match disqualifies a candidate.
// These are implementation details that should not become specs.
func (c SpecQualificationCriteria) Disqualifications() []string {
	return []string{
		"Implementation details (data structures, algorithms, patterns used)",
		"Internal code organization (services, handlers, repositories, utils)",
		"Technical plumbing users don't interact with (middleware, adapters)",
		"Standard practices covered by language/framework conventions",
		"Infrastructure concerns (logging, monitoring, deployment)",
		"Code quality practices (error handling patterns, validation approaches)",
		"Architectural decisions (those belong in ADRs)",
		"Domain vocabulary definitions (those belong in Domain docs)",
	}
}

// LitmusTest returns the core question to determine spec-worthiness.
// If the answer is YES, the candidate is spec-worthy.
func (c SpecQualificationCriteria) LitmusTest() string {
	return "Could a user verify this by using the system?"
}

// FormatForAgent returns the qualification criteria formatted for inclusion
// in agent prompts. This ensures consistency between the domain definition
// and the agent's instructions.
func (c SpecQualificationCriteria) FormatForAgent() string {
	var sb strings.Builder

	sb.WriteString("## Qualification Criteria (ALL must be true)\n")
	for i, q := range c.Qualifications() {
		sb.WriteString(fmt.Sprintf("%d. %s\n", i+1, q))
	}
	for i, r := range c.AcceptanceCriteriaRequirements() {
		sb.WriteString(fmt.Sprintf("%d. %s\n", len(c.Qualifications())+i+1, r))
	}

	sb.WriteString("\n## Litmus Test\n")
	sb.WriteString(fmt.Sprintf("Ask: %q\n", c.LitmusTest()))
	sb.WriteString("YES = Spec worthy\n")
	sb.WriteString("NO = Implementation detail, disqualify\n")

	sb.WriteString("\n## Disqualification Criteria (ANY disqualifies)\n")
	for _, d := range c.Disqualifications() {
		sb.WriteString(fmt.Sprintf("- %s\n", d))
	}

	return strings.TrimSuffix(sb.String(), "\n")
}

// =============================================================================
// Draft Spec Types
// =============================================================================

// DraftConfidence indicates how confident we are in a draft spec
type DraftConfidence string

const (
	// DraftConfidenceHigh indicates strong evidence: tests exist, clear boundaries, documentation
	DraftConfidenceHigh DraftConfidence = "high"
	// DraftConfidenceMedium indicates partial evidence: some tests or docs, but gaps exist
	DraftConfidenceMedium DraftConfidence = "medium"
	// DraftConfidenceLow indicates weak evidence: inferred from code patterns only
	DraftConfidenceLow DraftConfidence = "low"
)

// DraftSpec represents a proposed specification discovered from codebase analysis.
// Draft specs live in .utopia/drafts/specs/ and require validation before promotion to specs.
type DraftSpec struct {
	ID          string          `yaml:"id"`
	Title       string          `yaml:"title"`
	Created     time.Time       `yaml:"created"`
	Description string          `yaml:"description"`
	Confidence  DraftConfidence `yaml:"confidence"`

	// DiscoveredFrom lists the source files that were analyzed to create this draft.
	// This provides traceability back to the codebase locations that informed the spec.
	DiscoveredFrom []string `yaml:"discovered_from,omitempty"`

	// UncertaintyNotes explains what's unclear about this draft (especially for low confidence)
	UncertaintyNotes []string `yaml:"uncertainty_notes,omitempty"`

	// Evidence captures what sources informed this draft
	Evidence DraftEvidence `yaml:"evidence"`

	// Features with their acceptance criteria (proposed)
	Features []Feature `yaml:"features"`

	// DomainKnowledge captured during discovery
	DomainKnowledge []string `yaml:"domain_knowledge,omitempty"`
}

// DraftEvidence tracks what sources informed the draft spec
type DraftEvidence struct {
	// CodeFiles lists source files that define this behavior
	CodeFiles []string `yaml:"code_files,omitempty"`
	// TestFiles lists test files that verify this behavior
	TestFiles []string `yaml:"test_files,omitempty"`
	// DocFiles lists documentation files that describe this behavior
	DocFiles []string `yaml:"doc_files,omitempty"`
	// Comments captures relevant code comments that describe intent
	Comments []string `yaml:"comments,omitempty"`
}

// AddFeature adds a proposed feature to the draft spec
func (d *DraftSpec) AddFeature(f Feature) {
	d.Features = append(d.Features, f)
}

// AddUncertaintyNote adds a note explaining what's unclear about this draft
func (d *DraftSpec) AddUncertaintyNote(note string) {
	d.UncertaintyNotes = append(d.UncertaintyNotes, note)
}

// AddCodeEvidence adds a code file to the evidence
func (d *DraftSpec) AddCodeEvidence(file string) {
	d.Evidence.CodeFiles = append(d.Evidence.CodeFiles, file)
}

// AddTestEvidence adds a test file to the evidence
func (d *DraftSpec) AddTestEvidence(file string) {
	d.Evidence.TestFiles = append(d.Evidence.TestFiles, file)
}

// AddDocEvidence adds a documentation file to the evidence
func (d *DraftSpec) AddDocEvidence(file string) {
	d.Evidence.DocFiles = append(d.Evidence.DocFiles, file)
}

// AddCommentEvidence adds a comment to the evidence
func (d *DraftSpec) AddCommentEvidence(comment string) {
	d.Evidence.Comments = append(d.Evidence.Comments, comment)
}

// AddDiscoveredFrom adds a source file to the discovered_from list
func (d *DraftSpec) AddDiscoveredFrom(file string) {
	d.DiscoveredFrom = append(d.DiscoveredFrom, file)
}

// HasTests returns true if the draft has test file evidence
func (d *DraftSpec) HasTests() bool {
	return len(d.Evidence.TestFiles) > 0
}

// HasDocs returns true if the draft has documentation evidence
func (d *DraftSpec) HasDocs() bool {
	return len(d.Evidence.DocFiles) > 0
}

// CalculateConfidence determines confidence based on evidence quality
func (d *DraftSpec) CalculateConfidence() DraftConfidence {
	hasTests := d.HasTests()
	hasDocs := d.HasDocs()
	hasCode := len(d.Evidence.CodeFiles) > 0

	// High: tests exist AND (docs exist OR clear code boundaries)
	if hasTests && (hasDocs || hasCode) {
		return DraftConfidenceHigh
	}

	// Medium: tests exist OR docs exist
	if hasTests || hasDocs {
		return DraftConfidenceMedium
	}

	// Low: only code inference
	return DraftConfidenceLow
}

// =============================================================================
// Discovery State Types
// =============================================================================

// DiscoveryState tracks the state of codebase discovery for incremental runs.
// Stored in .utopia/drafts/specs/.discovery-state to enable re-running discover
// and only analyzing new or modified files.
type DiscoveryState struct {
	// LastRun is the timestamp of the last discovery run
	LastRun time.Time `yaml:"last_run"`
	// FilesAnalyzed tracks files processed in the last run with their mod times
	FilesAnalyzed map[string]time.Time `yaml:"files_analyzed,omitempty"`
	// Scope records any restrictions applied during discovery for context
	Scope *DiscoveryScope `yaml:"scope,omitempty"`
}

// DiscoveryScope records path and pattern restrictions applied during discovery.
// This provides context about what portion of the codebase was analyzed.
type DiscoveryScope struct {
	// Paths lists directories that discovery was limited to (empty = entire codebase)
	Paths []string `yaml:"paths,omitempty"`
	// ExcludePatterns lists glob patterns that were excluded from discovery
	ExcludePatterns []string `yaml:"exclude_patterns,omitempty"`
}

// DomainDiscoveryState tracks the state of domain vocabulary discovery for incremental runs.
// Stored in .utopia/drafts/domain/.discovery-state to enable re-running discover domain
// and only analyzing new or modified files.
type DomainDiscoveryState struct {
	// LastRun is the timestamp of the last discovery run
	LastRun time.Time `yaml:"last_run"`
	// FilesAnalyzed tracks files processed in the last run with their mod times
	FilesAnalyzed map[string]time.Time `yaml:"files_analyzed,omitempty"`
	// Scope records any restrictions applied during discovery for context
	Scope *DiscoveryScope `yaml:"scope,omitempty"`
}
