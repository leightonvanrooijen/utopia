package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/leightonvanrooijen/utopia/internal/domain"
	"github.com/leightonvanrooijen/utopia/internal/infra/claude"
	"github.com/leightonvanrooijen/utopia/internal/infra/storage"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

// Default number of refinement iterations per candidate
const defaultRefinementIterations = 3

// discoverProgress tracks timing and progress for discovery phases
type discoverProgress struct {
	startTime   time.Time
	phaseStart  time.Time
	totalPhases int
	verbose     bool
}

func newDiscoverProgress(totalPhases int, verbose bool) *discoverProgress {
	now := time.Now()
	return &discoverProgress{
		startTime:   now,
		phaseStart:  now,
		totalPhases: totalPhases,
		verbose:     verbose,
	}
}

func (p *discoverProgress) startPhase(phaseNum int, name string) {
	p.phaseStart = time.Now()
	fmt.Printf("[%d/%d] %s...", phaseNum, p.totalPhases, name)
}

func (p *discoverProgress) endPhase(detail string) {
	elapsed := time.Since(p.phaseStart)
	if detail != "" {
		fmt.Printf(" done (%.1fs, %s)\n", elapsed.Seconds(), detail)
	} else {
		fmt.Printf(" done (%.1fs)\n", elapsed.Seconds())
	}
}

func (p *discoverProgress) printTotalElapsed() {
	elapsed := time.Since(p.startTime)
	fmt.Printf("\nTotal elapsed time: %.1fs\n", elapsed.Seconds())
}

func (p *discoverProgress) verbosePrintf(format string, args ...interface{}) {
	if p.verbose {
		fmt.Printf(format, args...)
	}
}

var discoverCmd = &cobra.Command{
	Use:   "discover",
	Short: "Scan codebase and propose draft specifications",
	Long: `Analyze the codebase to discover user-observable capabilities and propose draft specifications.

Discovery uses a 2-stage parallel agent pipeline:

  Stage 1 - Identify & Qualify:
    Scans codebase to find potential user-observable capabilities.
    Applies qualification criteria to filter candidates ruthlessly.
    Only specs that describe what users can DO pass through.

  Stage 2 - Parallel Refinement:
    Spawns one Claude Code agent per qualified candidate.
    Each agent has tool access (Read, Grep, Glob) to explore the codebase.
    Agents refine specs iteratively, discovering details from source files.
    Runs for a fixed number of iterations (default: 3) per candidate.

Qualification criteria (specs must satisfy ALL):
  - Describes a user-observable capability
  - Can be verified by using the system
  - Represents a coherent, bounded feature
  - Answers "what can I do?" not "how is it built?"

Disqualification criteria (ANY disqualifies):
  - Implementation details (data structures, algorithms)
  - Internal code organization (services, handlers)
  - Technical plumbing users don't interact with
  - Standard practices covered by language/framework conventions

Scoping discovery:
  --path <dir>       Limit discovery to a specific directory
  --exclude <glob>   Exclude files matching glob pattern

  Examples:
    utopia discover --path internal/api --path internal/domain
    utopia discover --exclude "**/*_test.go" --exclude "**/mock_*.go"

After discovery, use 'utopia shape' to validate and refine drafts.`,
	RunE: runDiscover,
}

var (
	discoverPathFlags    []string
	discoverExcludeFlags []string
	discoverVerboseFlag  bool
)

func init() {
	rootCmd.AddCommand(discoverCmd)
	discoverCmd.Flags().StringSliceVar(&discoverPathFlags, "path", nil, "Limit discovery to specific directory (can be specified multiple times)")
	discoverCmd.Flags().StringSliceVar(&discoverExcludeFlags, "exclude", nil, "Exclude files matching glob pattern (can be specified multiple times)")
	discoverCmd.Flags().BoolVarP(&discoverVerboseFlag, "verbose", "v", false, "Enable detailed file-by-file progress output")
}

// Stage 1: Identify & Qualify Agent
// Scans codebase to identify potential user-observable capabilities AND
// applies qualification criteria to filter candidates in a single pass.
// This merged stage reduces context loss between identification and qualification.
func buildIdentifyQualifyPrompt(codebaseContext, specsSummary string) string {
	criteria := domain.SpecQualificationCriteria{}
	return fmt.Sprintf(`Find and qualify user-observable capabilities in this system.

## Codebase
%s

## Already Documented (skip these)
%s

%s

## Your Mission
1. IDENTIFY: Find capabilities users can DO or SEE
2. QUALIFY: Apply the litmus test - "Could a user verify this by using the system?"
3. OUTPUT: Only qualified candidates that pass ALL criteria

## Good vs Bad Candidates
QUALIFIES: "Users can initialize a project" - user runs command, sees result
QUALIFIES: "Users can export data to CSV" - user triggers action, gets output
DISQUALIFY: "YAML parser validates schemas" - internal implementation
DISQUALIFY: "Repository uses file storage" - technical plumbing
DISQUALIFY: "Service validates input" - internal behavior

## Output
`+"```yaml"+`
qualified:
  - id: kebab-case-id
    title: "What User Can Do"
    description: "User-facing capability"
    source_files: ["path/to/file.go"]
    evidence_type: code|test|doc
    qualification_reason: "How user verifies this"
disqualified:
  - id: candidate-id
    reason: "Why not user-observable"
`+"```"+`
Be RUTHLESS. When in doubt, disqualify.
Output ONLY the YAML.`, codebaseContext, specsSummary, criteria.FormatForAgent())
}

// Stage 2: Refinement Agent (runs per-candidate with tool access)
// Deep analysis to extract precise, implementation-complete specifications.
// This agent has access to Read, Grep, Glob tools to explore the codebase.
func buildRefinementAgentPrompt(candidate qualifiedCandidate, iteration, maxIterations int) string {
	sourceFilesStr := strings.Join(candidate.SourceFiles, ", ")
	return fmt.Sprintf(`ULTRATHINK: Study the code deeply. Extract a precise specification.

## Your Mission
Refine this candidate into a high-quality draft specification.
This is iteration %d of %d - explore thoroughly using your tools.

## Candidate to Refine
- ID: %s
- Title: %s
- Description: %s
- Source Files: %s
- Qualification Reason: %s

## Tools Available
You have access to Read, Grep, and Glob tools. USE THEM to:
1. Read the source files listed above
2. Search for related code, tests, and documentation
3. Find exact values, error messages, and boundaries
4. Trace execution paths and discover edge cases

## Quality Bar
Could someone implement EQUIVALENT BEHAVIOR from this spec alone?
If no: the spec is too vague. Use your tools to dig deeper.

## What to Extract

### 1. Specific Values (never categories)
BAD:  "Supports multiple output formats"
GOOD: "Supports exactly 3 output formats: json, yaml, csv"

### 2. Exact Rules (not vague behaviors)
BAD:  "Validates input appropriately"
GOOD: "Rejects input if: empty string, longer than 255 chars, contains characters outside [a-zA-Z0-9_-]"

### 3. Explicit Boundaries
BAD:  "Works with large files"
GOOD: "Maximum file size: 10MB. Files larger than 10MB return error 'file exceeds 10MB limit'"

### 4. Negative Constraints (what DOESN'T happen)
BAD:  (missing)
GOOD: "Does NOT retry on 4xx errors (only 5xx). Does NOT follow redirects."

### 5. Format Strings and Identifiers
BAD:  "Outputs a formatted message"
GOOD: "Output format: '[{level}] {timestamp}: {message}' where level is INFO|WARN|ERROR"

## Forbidden Language
NEVER use these vague terms:
- "etc." - list EVERY item explicitly
- "various" - name each one
- "appropriately" - state the exact behavior
- "handles errors" - state WHICH errors and HOW
- "supports" - describe exact mechanism
- "multiple" - give the exact count or list

## Confidence Assessment
- HIGH: Tests + docs exist AND spec has concrete values
- MEDIUM: Tests OR docs (not both) AND spec mostly concrete
- LOW: Code only OR spec contains vague language

## Output
`+"```yaml"+`
draft:
  id: %s
  title: "What User Can Do"
  description: |
    User-focused description with SPECIFIC details.
  confidence: high|medium|low
  discovered_from: ["source/file.go"]
  uncertainty_notes: ["What's unclear - be specific about what you couldn't determine"]
  evidence:
    code_files: ["impl.go"]
    test_files: ["test.go"]
    doc_files: ["docs.md"]
    comments: ["Relevant code comments showing intent"]
  features:
    - id: feature-id
      description: "Specific capability with exact details"
      acceptance_criteria:
        - "Given [specific precondition], when [exact action], then [measurable outcome with specific values]"
        - "Does NOT [explicit negative constraint]"
  domain_knowledge: ["Specific term: exact meaning in this codebase"]
`+"```"+`
FIRST: Use your tools to read the source files and explore related code.
THEN: Output ONLY the YAML block with your refined specification.
If you can't determine a specific value, note it in uncertainty_notes.`,
		iteration, maxIterations,
		candidate.ID, candidate.Title, candidate.Description, sourceFilesStr, candidate.QualificationReason,
		candidate.ID)
}

func runDiscover(cmd *cobra.Command, args []string) error {
	projectDir := GetProjectDir(cmd)

	absPath, err := filepath.Abs(projectDir)
	if err != nil {
		return fmt.Errorf("failed to resolve project path: %w", err)
	}

	utopiaDir := filepath.Join(absPath, ".utopia")

	// Check if initialized
	if _, err := os.Stat(utopiaDir); os.IsNotExist(err) {
		return fmt.Errorf("not a Utopia project (run 'utopia init' first)")
	}

	store := storage.NewYAMLStore(utopiaDir)

	// Load existing specs to avoid duplicates
	existingSpecs, err := store.ListSpecs()
	if err != nil {
		existingSpecs = []*domain.Spec{}
	}

	// Load existing drafts to show status
	existingDrafts, err := store.ListDrafts()
	if err != nil {
		existingDrafts = []*domain.DraftSpec{}
	}

	// Ensure drafts/specs directory exists
	draftsDir := filepath.Join(utopiaDir, "drafts", "specs")
	if err := os.MkdirAll(draftsDir, 0755); err != nil {
		return fmt.Errorf("failed to create drafts/specs directory: %w", err)
	}

	// Build scope from flags
	scope := discoverScope{
		paths:           discoverPathFlags,
		excludePatterns: discoverExcludeFlags,
	}

	fmt.Println("Starting codebase discovery...")
	fmt.Printf("Project: %s\n", absPath)
	fmt.Printf("Existing specs: %d\n", len(existingSpecs))
	fmt.Printf("Existing drafts: %d\n", len(existingDrafts))
	if len(scope.paths) > 0 {
		fmt.Printf("Scope: %s\n", strings.Join(scope.paths, ", "))
	}
	if len(scope.excludePatterns) > 0 {
		fmt.Printf("Excluding: %s\n", strings.Join(scope.excludePatterns, ", "))
	}
	fmt.Println()

	// Initialize progress tracker with 4 phases: scan, identify+qualify, parallel refine, save
	progress := newDiscoverProgress(4, discoverVerboseFlag)

	// Phase 1: Collect codebase context
	progress.startPhase(1, "Scanning files")
	codebaseContext, filesAnalyzed, err := collectCodebaseContext(absPath, scope, progress)
	if err != nil {
		return fmt.Errorf("failed to collect codebase context: %w", err)
	}

	// Check if there are any files to analyze
	if len(filesAnalyzed) == 0 {
		fmt.Printf(" done (no files found)\n")
		fmt.Println("No files to analyze.")
		progress.printTotalElapsed()
		return nil
	}

	progress.endPhase(fmt.Sprintf("%d files found", len(filesAnalyzed)))

	// Build existing specs summary
	specsSummary := buildExistingSpecsSummary(existingSpecs)

	ctx := context.Background()
	cli := claude.NewCLI().WithVerbose(discoverVerboseFlag)

	// Phase 2: Stage 1 - Identify & Qualify (merged stage)
	progress.startPhase(2, "Stage 1: Identifying and qualifying candidates")
	stage1Prompt := buildIdentifyQualifyPrompt(codebaseContext, specsSummary)
	qualifiedOutput, err := cli.Prompt(ctx, stage1Prompt)
	if err != nil {
		return fmt.Errorf("identify and qualify failed: %w", err)
	}
	qualifiedCount := countYAMLItems(qualifiedOutput, "qualified")
	disqualifiedCount := countYAMLItems(qualifiedOutput, "disqualified")
	progress.endPhase(fmt.Sprintf("%d qualified, %d disqualified", qualifiedCount, disqualifiedCount))

	// Log disqualified candidates with reasons for transparency
	disqualified := parseDisqualifiedCandidates(qualifiedOutput)
	logDisqualifiedCandidates(disqualified, discoverVerboseFlag)

	// Parse qualified candidates for parallel refinement
	qualified := parseQualifiedCandidates(qualifiedOutput)

	// Check if any candidates qualified
	if len(qualified) == 0 {
		fmt.Println("\nNo candidates passed qualification criteria.")
		fmt.Println("All identified items were implementation details, not user-observable capabilities.")
		progress.printTotalElapsed()
		return nil
	}

	// Phase 3: Stage 2 - Parallel Refinement
	progress.startPhase(3, fmt.Sprintf("Stage 2: Refining %d candidates in parallel", len(qualified)))
	drafts, refinementErrors := runParallelRefinement(ctx, qualified, defaultRefinementIterations, discoverVerboseFlag)
	progress.endPhase(fmt.Sprintf("%d drafts refined", len(drafts)))

	// Log any refinement errors
	if len(refinementErrors) > 0 && discoverVerboseFlag {
		fmt.Println("\n  Refinement errors:")
		for _, err := range refinementErrors {
			fmt.Printf("    ✗ %v\n", err)
		}
	}

	if len(drafts) == 0 {
		fmt.Println("\nNo draft specifications produced after refinement.")
		progress.printTotalElapsed()
		return nil
	}

	// Phase 4: Save drafts
	progress.startPhase(4, "Saving drafts")
	for _, draft := range drafts {
		progress.verbosePrintf("\n  Saving %s.yaml", draft.ID)
		if err := store.SaveDraft(draft); err != nil {
			return fmt.Errorf("failed to save draft %s: %w", draft.ID, err)
		}
	}
	progress.endPhase(fmt.Sprintf("%d drafts saved", len(drafts)))

	// Print summary
	printDiscoverySummary(drafts, draftsDir)

	// Print total elapsed time
	progress.printTotalElapsed()

	return nil
}

// runParallelRefinement spawns one refinement agent per qualified candidate.
// Each agent runs for a fixed number of iterations and has access to Read, Grep, Glob tools.
// Returns refined drafts and any errors that occurred.
func runParallelRefinement(ctx context.Context, candidates []qualifiedCandidate, iterations int, verbose bool) ([]*domain.DraftSpec, []error) {
	var (
		drafts   []*domain.DraftSpec
		errors   []error
		mu       sync.Mutex
		wg       sync.WaitGroup
	)

	// Create a CLI instance with tool access for refinement agents
	// Agents get Read, Grep, Glob tools to explore codebase deeply
	refinementCLI := claude.NewCLI().
		WithVerbose(verbose).
		WithAllowedTools([]string{"Read", "Grep", "Glob"})

	// Spawn one agent per candidate
	for _, candidate := range candidates {
		wg.Add(1)
		go func(c qualifiedCandidate) {
			defer wg.Done()

			if verbose {
				fmt.Printf("\n  Starting refinement for: %s\n", c.ID)
			}

			// Run refinement iterations for this candidate
			var lastOutput string
			var lastErr error
			for i := 1; i <= iterations; i++ {
				prompt := buildRefinementAgentPrompt(c, i, iterations)
				output, err := refinementCLI.Prompt(ctx, prompt)
				if err != nil {
					lastErr = fmt.Errorf("refinement failed for %s (iteration %d): %w", c.ID, i, err)
					continue
				}
				lastOutput = output
				lastErr = nil

				if verbose {
					fmt.Printf("  ✓ %s iteration %d/%d complete\n", c.ID, i, iterations)
				}
			}

			// Parse the final output into a draft
			if lastErr != nil {
				mu.Lock()
				errors = append(errors, lastErr)
				mu.Unlock()
				return
			}

			draft, err := parseSingleDraftFromOutput(lastOutput)
			if err != nil {
				mu.Lock()
				errors = append(errors, fmt.Errorf("failed to parse draft for %s: %w", c.ID, err))
				mu.Unlock()
				return
			}

			mu.Lock()
			drafts = append(drafts, draft)
			mu.Unlock()

			if verbose {
				fmt.Printf("  ✓ %s refinement complete (confidence: %s)\n", c.ID, draft.Confidence)
			}
		}(candidate)
	}

	// Wait for all agents to complete
	wg.Wait()

	return drafts, errors
}

// parseQualifiedCandidates extracts qualified candidates from Stage 1 output
func parseQualifiedCandidates(output string) []qualifiedCandidate {
	yamlContent := extractYAMLBlock(output)
	if yamlContent == "" {
		return nil
	}

	var qOutput qualificationOutput
	if err := yaml.Unmarshal([]byte(yamlContent), &qOutput); err != nil {
		return nil
	}

	return qOutput.Qualified
}

// parseSingleDraftFromOutput extracts a single draft from refinement agent output
func parseSingleDraftFromOutput(output string) (*domain.DraftSpec, error) {
	yamlContent := extractYAMLBlock(output)
	if yamlContent == "" {
		return nil, fmt.Errorf("no YAML block found in output")
	}

	// Try parsing as single draft first (new format)
	var singleDraft struct {
		Draft draftOutput `yaml:"draft"`
	}
	if err := yaml.Unmarshal([]byte(yamlContent), &singleDraft); err == nil && singleDraft.Draft.ID != "" {
		return convertDraftOutput(singleDraft.Draft), nil
	}

	// Fallback: try parsing as drafts array (old format compatibility)
	var draftsOut draftsOutput
	if err := yaml.Unmarshal([]byte(yamlContent), &draftsOut); err != nil {
		return nil, fmt.Errorf("failed to parse YAML: %w", err)
	}

	if len(draftsOut.Drafts) == 0 {
		return nil, fmt.Errorf("no drafts found in output")
	}

	return convertDraftOutput(draftsOut.Drafts[0]), nil
}

// convertDraftOutput converts a draftOutput to a domain.DraftSpec
func convertDraftOutput(d draftOutput) *domain.DraftSpec {
	confidence := domain.DraftConfidenceMedium
	switch strings.ToLower(d.Confidence) {
	case "high":
		confidence = domain.DraftConfidenceHigh
	case "low":
		confidence = domain.DraftConfidenceLow
	}

	draft := &domain.DraftSpec{
		ID:               d.ID,
		Title:            d.Title,
		Created:          time.Now(),
		Description:      d.Description,
		Confidence:       confidence,
		DiscoveredFrom:   d.DiscoveredFrom,
		UncertaintyNotes: d.UncertaintyNotes,
		Evidence: domain.DraftEvidence{
			CodeFiles: d.Evidence.CodeFiles,
			TestFiles: d.Evidence.TestFiles,
			DocFiles:  d.Evidence.DocFiles,
			Comments:  d.Evidence.Comments,
		},
		DomainKnowledge: d.DomainKnowledge,
	}

	for _, f := range d.Features {
		draft.Features = append(draft.Features, domain.Feature{
			ID:                 f.ID,
			Description:        f.Description,
			AcceptanceCriteria: f.AcceptanceCriteria,
		})
	}

	return draft
}

// collectCodebaseContext gathers all text files for Claude to analyze.
// Returns the context string and a list of analyzed file paths.
func collectCodebaseContext(projectDir string, scope discoverScope, progress *discoverProgress) (string, []string, error) {
	var sb strings.Builder
	var filesAnalyzed []string

	// Determine search roots - use scoped paths or entire project
	searchRoots := scope.paths
	if len(searchRoots) == 0 {
		searchRoots = []string{projectDir}
	} else {
		// Convert relative paths to absolute
		absoluteRoots := make([]string, 0, len(searchRoots))
		for _, p := range searchRoots {
			if filepath.IsAbs(p) {
				absoluteRoots = append(absoluteRoots, p)
			} else {
				absoluteRoots = append(absoluteRoots, filepath.Join(projectDir, p))
			}
		}
		searchRoots = absoluteRoots
	}

	// Collect all text files - let Claude determine what's relevant
	var allFiles []collectedFile
	const maxTotalSize int64 = 200000 // 200KB total limit for context

	for _, root := range searchRoots {
		files, skipped, err := collectAllTextFiles(root, projectDir, maxTotalSize, scope.excludePatterns, progress)
		if err != nil {
			continue // Skip on error, don't fail entire discovery
		}
		allFiles = append(allFiles, files...)

		// Log skipped files in verbose mode
		for _, skip := range skipped {
			progress.verbosePrintf("\n  Skipped: %s (%s)", skip.path, skip.reason)
		}
	}

	if len(allFiles) > 0 {
		sb.WriteString("\n### Source Files\n\n")
		for _, f := range allFiles {
			progress.verbosePrintf("\n  Collected: %s", f.path)
			sb.WriteString(fmt.Sprintf("**File: %s**\n```\n%s\n```\n\n", f.path, f.content))
			filesAnalyzed = append(filesAnalyzed, f.path)
		}
	}

	return sb.String(), filesAnalyzed, nil
}

type collectedFile struct {
	path    string
	content string
	modTime time.Time
}

type skippedFile struct {
	path   string
	reason string
}

// discoverScope holds the scope restrictions for discovery
type discoverScope struct {
	paths           []string // directories to limit discovery to
	excludePatterns []string // glob patterns to exclude
}

// collectAllTextFiles gathers all text files regardless of extension,
// letting Claude determine what's relevant for capability discovery.
// Returns collected files, skipped files (for verbose logging), and any error.
func collectAllTextFiles(root, projectDir string, maxTotalSize int64, excludePatterns []string, progress *discoverProgress) ([]collectedFile, []skippedFile, error) {
	var files []collectedFile
	var skipped []skippedFile
	var totalSize int64

	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // Skip errors
		}

		// Skip directories we don't care about
		if info.IsDir() {
			name := info.Name()
			if name == ".git" || name == "vendor" || name == "node_modules" || name == ".utopia" {
				return filepath.SkipDir
			}
			return nil
		}

		// Compute path relative to project root for consistent reporting
		relPath, err := filepath.Rel(projectDir, path)
		if err != nil {
			return nil
		}

		// Check if file matches any exclude pattern
		if matchesAnyPattern(relPath, excludePatterns) {
			if progress.verbose {
				skipped = append(skipped, skippedFile{path: relPath, reason: "excluded by pattern"})
			}
			return nil
		}

		// Check size limit
		if totalSize+info.Size() > maxTotalSize {
			if progress.verbose {
				skipped = append(skipped, skippedFile{path: relPath, reason: "size limit exceeded"})
			}
			return nil
		}

		// Read file
		content, err := os.ReadFile(path)
		if err != nil {
			return nil
		}

		// Skip binary files
		if !isTextFile(content) {
			if progress.verbose {
				skipped = append(skipped, skippedFile{path: relPath, reason: "binary file"})
			}
			return nil
		}

		files = append(files, collectedFile{
			path:    relPath,
			content: truncateContent(string(content), 5000),
		})
		totalSize += info.Size()

		return nil
	})

	return files, skipped, err
}

// matchesAnyPattern returns true if path matches any of the given glob patterns
func matchesAnyPattern(path string, patterns []string) bool {
	for _, pattern := range patterns {
		if matchGlob(path, pattern) {
			return true
		}
		// Also try direct filepath.Match for simple patterns
		if matched, _ := filepath.Match(pattern, filepath.Base(path)); matched {
			return true
		}
	}
	return false
}

// matchGlob does simple glob matching for patterns with ** and * wildcards
func matchGlob(path, pattern string) bool {
	// Handle **/ prefix (match any directory depth)
	if strings.HasPrefix(pattern, "**/") {
		suffix := pattern[3:]

		// Handle trailing /** (matches everything under a directory)
		if strings.HasSuffix(suffix, "/**") {
			dirPart := strings.TrimSuffix(suffix, "/**")
			// Check if path is inside this directory
			return strings.HasPrefix(path, dirPart+"/") || path == dirPart
		}

		// Try matching the suffix against just the filename
		matched, _ := filepath.Match(suffix, filepath.Base(path))
		if matched {
			return true
		}
		// Also try matching against the full path for patterns like **/vendor/**
		if strings.Contains(suffix, "/") {
			// For patterns like **/foo/bar, check if path contains the suffix
			return strings.Contains(path, strings.TrimPrefix(suffix, "**/"))
		}
		return false
	}
	return false
}

// isTextFile checks if content appears to be text
func isTextFile(content []byte) bool {
	if len(content) == 0 {
		return true
	}
	// Check first 512 bytes for non-text characters
	checkLen := 512
	if len(content) < checkLen {
		checkLen = len(content)
	}
	for _, b := range content[:checkLen] {
		if b == 0 {
			return false
		}
	}
	return true
}

// truncateContent limits content size while keeping meaningful context
func truncateContent(content string, maxLen int) string {
	if len(content) <= maxLen {
		return content
	}
	return content[:maxLen] + "\n... [truncated]"
}

// buildExistingSpecsSummary creates a summary of existing specs for Claude
func buildExistingSpecsSummary(specs []*domain.Spec) string {
	if len(specs) == 0 {
		return "(No existing specifications)"
	}

	var sb strings.Builder
	for _, spec := range specs {
		sb.WriteString(fmt.Sprintf("- **%s** (%s): %s\n", spec.Title, spec.ID, truncateContent(spec.Description, 100)))
		for _, f := range spec.Features {
			sb.WriteString(fmt.Sprintf("  - %s: %s\n", f.ID, truncateContent(f.Description, 80)))
		}
	}
	return sb.String()
}

// draftsOutput represents the YAML structure Claude outputs
type draftsOutput struct {
	Drafts []draftOutput `yaml:"drafts"`
}

type draftOutput struct {
	ID               string          `yaml:"id"`
	Title            string          `yaml:"title"`
	Description      string          `yaml:"description"`
	Confidence       string          `yaml:"confidence"`
	DiscoveredFrom   []string        `yaml:"discovered_from,omitempty"`
	UncertaintyNotes []string        `yaml:"uncertainty_notes,omitempty"`
	Evidence         evidenceOutput  `yaml:"evidence"`
	Features         []featureOutput `yaml:"features"`
	DomainKnowledge  []string        `yaml:"domain_knowledge,omitempty"`
}

type evidenceOutput struct {
	CodeFiles []string `yaml:"code_files,omitempty"`
	TestFiles []string `yaml:"test_files,omitempty"`
	DocFiles  []string `yaml:"doc_files,omitempty"`
	Comments  []string `yaml:"comments,omitempty"`
}

type featureOutput struct {
	ID                 string   `yaml:"id"`
	Description        string   `yaml:"description"`
	AcceptanceCriteria []string `yaml:"acceptance_criteria"`
}

// qualificationOutput represents the Stage 2 qualification agent output
type qualificationOutput struct {
	Qualified    []qualifiedCandidate    `yaml:"qualified"`
	Disqualified []disqualifiedCandidate `yaml:"disqualified"`
}

type qualifiedCandidate struct {
	ID                  string   `yaml:"id"`
	Title               string   `yaml:"title"`
	Description         string   `yaml:"description"`
	SourceFiles         []string `yaml:"source_files,omitempty"`
	QualificationReason string   `yaml:"qualification_reason"`
}

type disqualifiedCandidate struct {
	ID     string `yaml:"id"`
	Reason string `yaml:"reason"`
}

// parseDisqualifiedCandidates extracts disqualified candidates from Stage 2 output
func parseDisqualifiedCandidates(output string) []disqualifiedCandidate {
	yamlContent := extractYAMLBlock(output)
	if yamlContent == "" {
		return nil
	}

	var qOutput qualificationOutput
	if err := yaml.Unmarshal([]byte(yamlContent), &qOutput); err != nil {
		return nil
	}

	return qOutput.Disqualified
}

// logDisqualifiedCandidates logs disqualified candidates with their reasons
// for transparency. This helps users understand why certain candidates
// were filtered out during discovery.
func logDisqualifiedCandidates(disqualified []disqualifiedCandidate, verbose bool) {
	if len(disqualified) == 0 {
		return
	}

	if verbose {
		fmt.Println("\n  Disqualified candidates:")
		for _, d := range disqualified {
			fmt.Printf("    ✗ %s: %s\n", d.ID, d.Reason)
		}
	}
}


// countYAMLItems counts items in a YAML list by key name
// Used for progress reporting between pipeline stages
func countYAMLItems(yamlOutput, key string) int {
	yamlContent := extractYAMLBlock(yamlOutput)
	if yamlContent == "" {
		return 0
	}

	var data map[string]interface{}
	if err := yaml.Unmarshal([]byte(yamlContent), &data); err != nil {
		return 0
	}

	if items, ok := data[key]; ok {
		if list, ok := items.([]interface{}); ok {
			return len(list)
		}
	}
	return 0
}

// extractYAMLBlock finds and extracts a YAML code block from text
func extractYAMLBlock(text string) string {
	// Look for ```yaml ... ``` block
	startMarkers := []string{"```yaml", "```yml"}
	endMarker := "```"

	for _, start := range startMarkers {
		startIdx := strings.Index(text, start)
		if startIdx == -1 {
			continue
		}

		contentStart := startIdx + len(start)
		remaining := text[contentStart:]
		endIdx := strings.Index(remaining, endMarker)
		if endIdx == -1 {
			continue
		}

		return strings.TrimSpace(remaining[:endIdx])
	}

	// Fallback: try to parse the entire output as YAML
	// (in case Claude didn't use code blocks)
	if strings.Contains(text, "drafts:") {
		lines := strings.Split(text, "\n")
		var yamlLines []string
		inYAML := false
		for _, line := range lines {
			if strings.HasPrefix(strings.TrimSpace(line), "drafts:") {
				inYAML = true
			}
			if inYAML {
				yamlLines = append(yamlLines, line)
			}
		}
		if len(yamlLines) > 0 {
			return strings.Join(yamlLines, "\n")
		}
	}

	return ""
}

// printDiscoverySummary displays the results of discovery
func printDiscoverySummary(drafts []*domain.DraftSpec, draftsDir string) {
	// Sort by confidence (high first)
	sort.Slice(drafts, func(i, j int) bool {
		confidenceOrder := map[domain.DraftConfidence]int{
			domain.DraftConfidenceHigh:   0,
			domain.DraftConfidenceMedium: 1,
			domain.DraftConfidenceLow:    2,
		}
		return confidenceOrder[drafts[i].Confidence] < confidenceOrder[drafts[j].Confidence]
	})

	// Count by confidence
	counts := map[domain.DraftConfidence]int{}
	for _, d := range drafts {
		counts[d.Confidence]++
	}

	fmt.Println()
	fmt.Println("═══════════════════════════════════════════════════════════════")
	fmt.Println("                    DISCOVERY COMPLETE")
	fmt.Println("═══════════════════════════════════════════════════════════════")
	fmt.Println()
	fmt.Printf("Created %d draft specifications:\n", len(drafts))
	fmt.Printf("  • HIGH confidence:   %d\n", counts[domain.DraftConfidenceHigh])
	fmt.Printf("  • MEDIUM confidence: %d\n", counts[domain.DraftConfidenceMedium])
	fmt.Printf("  • LOW confidence:    %d\n", counts[domain.DraftConfidenceLow])
	fmt.Println()
	fmt.Println("Drafts saved to:", draftsDir)
	fmt.Println()

	// List drafts with details
	fmt.Println("Draft Specifications:")
	fmt.Println("───────────────────────────────────────────────────────────────")
	for _, d := range drafts {
		confidenceIcon := "○"
		switch d.Confidence {
		case domain.DraftConfidenceHigh:
			confidenceIcon = "●"
		case domain.DraftConfidenceMedium:
			confidenceIcon = "◐"
		}

		fmt.Printf("\n%s [%s] %s\n", confidenceIcon, strings.ToUpper(string(d.Confidence)), d.Title)
		fmt.Printf("  ID: %s\n", d.ID)
		fmt.Printf("  Features: %d\n", len(d.Features))

		if d.HasTests() {
			fmt.Printf("  Tests: %d files\n", len(d.Evidence.TestFiles))
		}
		if d.HasDocs() {
			fmt.Printf("  Docs: %d files\n", len(d.Evidence.DocFiles))
		}

		if len(d.UncertaintyNotes) > 0 {
			fmt.Println("  Uncertainties:")
			for _, note := range d.UncertaintyNotes {
				fmt.Printf("    ⚠ %s\n", note)
			}
		}
	}

	fmt.Println()
	fmt.Println("───────────────────────────────────────────────────────────────")
	fmt.Println("Next steps:")
	fmt.Println("  1. Review drafts in", draftsDir)
	fmt.Println("  2. Run 'utopia shape' to validate and refine drafts")
	fmt.Println("  3. Promote validated drafts to specifications")
	fmt.Println()
}
