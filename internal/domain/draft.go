package domain

import (
	"time"
)

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

// NewDraftSpec creates a new draft spec with sensible defaults
func NewDraftSpec(id, title string, confidence DraftConfidence) *DraftSpec {
	return &DraftSpec{
		ID:         id,
		Title:      title,
		Created:    time.Now(),
		Confidence: confidence,
		Evidence:   DraftEvidence{},
		Features:   []Feature{},
	}
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

// DiscoveryState tracks the state of codebase discovery for incremental runs.
// Stored in .utopia/drafts/.discovery-state to enable re-running discover
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
