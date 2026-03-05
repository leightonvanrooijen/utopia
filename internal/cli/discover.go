package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/leightonvanrooijen/utopia/internal/domain"
	"github.com/leightonvanrooijen/utopia/internal/infra/claude"
	"github.com/leightonvanrooijen/utopia/internal/infra/storage"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

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

Discovery uses a sequential multi-agent pipeline optimized for spec quality:

  Stage 1 - Candidate Identification:
    Scans codebase to find potential user-observable capabilities.
    Casts a wide net to identify anything users might interact with.

  Stage 2 - Qualification:
    Applies strict criteria to filter candidates ruthlessly.
    Only specs that describe what users can DO pass through.

  Stage 3 - Refinement:
    Sharpens descriptions and ensures acceptance criteria are testable.
    Produces polished draft specifications ready for review.

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

Incremental discovery:
  Re-running discover only analyzes new or modified files.
  Use --full to force complete re-discovery.

After discovery, use 'utopia shape' to validate and refine drafts.`,
	RunE: runDiscover,
}

var (
	discoverFullFlag     bool
	discoverPathFlags    []string
	discoverExcludeFlags []string
	discoverVerboseFlag  bool
)

func init() {
	rootCmd.AddCommand(discoverCmd)
	discoverCmd.Flags().BoolVar(&discoverFullFlag, "full", false, "Force complete re-discovery of entire codebase")
	discoverCmd.Flags().StringSliceVar(&discoverPathFlags, "path", nil, "Limit discovery to specific directory (can be specified multiple times)")
	discoverCmd.Flags().StringSliceVar(&discoverExcludeFlags, "exclude", nil, "Exclude files matching glob pattern (can be specified multiple times)")
	discoverCmd.Flags().BoolVarP(&discoverVerboseFlag, "verbose", "v", false, "Enable detailed file-by-file progress output")
}

// Stage 1: Candidate Identification Agent
// Scans codebase to identify potential user-observable capabilities
const candidateAgentPrompt = `You are a Candidate Identification Agent. Scan the codebase to find potential user-observable capabilities.

## Codebase Context
%s

## Existing Specifications (avoid duplicates)
%s

## Core Principle
Specs document USER-OBSERVABLE capabilities. They answer "what can I do?" not "how is it built?"

## What to Identify
- Commands users can run
- APIs users can call
- Features users can interact with
- Behaviors users can observe

## Output Format
Output a YAML list of candidates. Be inclusive at this stage - filtering happens next.

` + "```yaml" + `
candidates:
  - id: candidate-id-kebab-case
    title: "Brief Title"
    description: "What the user can do or observe"
    source_files:
      - "path/to/file.go"
    evidence_type: code|test|doc
` + "```" + `

Output ONLY the YAML block.`

// Stage 2: Qualification Agent
// Applies strict criteria to filter candidates ruthlessly.
// Criteria are defined in domain.SpecQualificationCriteria.
func buildQualifierAgentPrompt(candidatesYAML string) string {
	criteria := domain.SpecQualificationCriteria{}
	return fmt.Sprintf(`You are a Qualification Agent. Apply strict criteria to filter spec candidates.

## Candidates from Stage 1
%s

%s

## Output Format
Output qualified candidates with qualification reasoning.

`+"```yaml"+`
qualified:
  - id: candidate-id
    title: "Title"
    description: "What the user can do"
    source_files:
      - "path/to/file.go"
    qualification_reason: "Why this qualifies as user-observable"
disqualified:
  - id: candidate-id
    reason: "Why this was disqualified"
`+"```"+`

Be RUTHLESS. When in doubt, disqualify. Quality over quantity.
Output ONLY the YAML block.`, candidatesYAML, criteria.FormatForAgent())
}

// Stage 3: Refinement Agent
// Sharpens descriptions and ensures acceptance criteria are testable
const refinementAgentPrompt = `You are a Refinement Agent. Sharpen qualified specs for clarity and testability.

## Qualified Candidates from Stage 2
%s

## Your Task
For each qualified candidate:
1. Sharpen the description to focus on user value
2. Break into specific features with testable acceptance criteria
3. Assess confidence based on evidence quality
4. Note any uncertainties

## Confidence Levels
- HIGH: Tests exist AND (docs exist OR very clear boundaries)
- MEDIUM: Tests OR docs exist (not both)
- LOW: Inferred from code only

## Output Format
Output refined draft specifications.

` + "```yaml" + `
drafts:
  - id: spec-id-kebab-case
    title: "Human Readable Title"
    description: |
      Clear description of what the user can do.
      Focus on user value, not implementation.
    confidence: high|medium|low
    discovered_from:
      - "path/to/file.go"
    uncertainty_notes:
      - "What remains unclear (for low confidence)"
    evidence:
      code_files:
        - "path/to/impl.go"
      test_files:
        - "path/to/test.go"
      doc_files:
        - "path/to/docs.md"
      comments:
        - "Relevant comment explaining intent"
    features:
      - id: feature-id
        description: "Specific capability"
        acceptance_criteria:
          - "Given X, when Y, then Z"
    domain_knowledge:
      - "Important domain concept"
` + "```" + `

Ensure EVERY acceptance criterion is testable by using the system.
Output ONLY the YAML block.`

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

	// Load previous discovery state for incremental discovery
	var lastRunTime time.Time
	previousState, err := store.LoadDiscoveryState()
	if err != nil {
		return fmt.Errorf("failed to load discovery state: %w", err)
	}

	isIncremental := !discoverFullFlag && previousState != nil
	if isIncremental {
		lastRunTime = previousState.LastRun
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
	if isIncremental {
		fmt.Printf("Mode: incremental (since %s)\n", lastRunTime.Format("2006-01-02 15:04:05"))
	} else {
		fmt.Println("Mode: full discovery")
	}
	if len(scope.paths) > 0 {
		fmt.Printf("Scope: %s\n", strings.Join(scope.paths, ", "))
	}
	if len(scope.excludePatterns) > 0 {
		fmt.Printf("Excluding: %s\n", strings.Join(scope.excludePatterns, ", "))
	}
	fmt.Println()

	// Initialize progress tracker with 5 phases: scan, 3 agent stages, save
	progress := newDiscoverProgress(5, discoverVerboseFlag)

	// Phase 1: Collect codebase context (with optional time filter for incremental)
	progress.startPhase(1, "Scanning files")
	codebaseContext, filesAnalyzed, err := collectCodebaseContextIncremental(absPath, lastRunTime, isIncremental, scope, progress)
	if err != nil {
		return fmt.Errorf("failed to collect codebase context: %w", err)
	}

	// Check if there are any files to analyze
	if len(filesAnalyzed) == 0 {
		fmt.Printf(" done (no new files)\n")
		fmt.Println("No new or modified files to analyze.")
		fmt.Println("Use --full to force complete re-discovery.")
		progress.printTotalElapsed()
		return nil
	}

	progress.endPhase(fmt.Sprintf("%d files found", len(filesAnalyzed)))

	// Build existing specs summary
	specsSummary := buildExistingSpecsSummary(existingSpecs)

	ctx := context.Background()
	cli := claude.NewCLI().WithVerbose(discoverVerboseFlag)

	// Phase 2: Stage 1 - Candidate Identification
	progress.startPhase(2, "Stage 1: Identifying candidates")
	stage1Prompt := fmt.Sprintf(candidateAgentPrompt, codebaseContext, specsSummary)
	candidatesOutput, err := cli.Prompt(ctx, stage1Prompt)
	if err != nil {
		return fmt.Errorf("candidate identification failed: %w", err)
	}
	candidateCount := countYAMLItems(candidatesOutput, "candidates")
	progress.endPhase(fmt.Sprintf("%d candidates found", candidateCount))

	// Phase 3: Stage 2 - Qualification
	progress.startPhase(3, "Stage 2: Qualifying candidates")
	stage2Prompt := buildQualifierAgentPrompt(candidatesOutput)
	qualifiedOutput, err := cli.Prompt(ctx, stage2Prompt)
	if err != nil {
		return fmt.Errorf("qualification failed: %w", err)
	}
	qualifiedCount := countYAMLItems(qualifiedOutput, "qualified")
	disqualifiedCount := countYAMLItems(qualifiedOutput, "disqualified")
	progress.endPhase(fmt.Sprintf("%d qualified, %d disqualified", qualifiedCount, disqualifiedCount))

	// Check if any candidates qualified
	if qualifiedCount == 0 {
		fmt.Println("\nNo candidates passed qualification criteria.")
		fmt.Println("All identified items were implementation details, not user-observable capabilities.")
		progress.printTotalElapsed()
		return nil
	}

	// Phase 4: Stage 3 - Refinement
	progress.startPhase(4, "Stage 3: Refining specifications")
	stage3Prompt := fmt.Sprintf(refinementAgentPrompt, qualifiedOutput)
	refinedOutput, err := cli.Prompt(ctx, stage3Prompt)
	if err != nil {
		return fmt.Errorf("refinement failed: %w", err)
	}
	progress.endPhase("")

	// Parse final drafts from refined output
	drafts, err := parseDraftsFromOutput(refinedOutput)
	if err != nil {
		return fmt.Errorf("failed to parse drafts: %w", err)
	}

	if len(drafts) == 0 {
		fmt.Println("\nNo draft specifications produced after refinement.")
		progress.printTotalElapsed()
		return nil
	}

	// Phase 5: Save drafts
	progress.startPhase(5, "Saving drafts")
	for _, draft := range drafts {
		progress.verbosePrintf("\n  Saving %s.yaml", draft.ID)
		if err := store.SaveDraft(draft); err != nil {
			return fmt.Errorf("failed to save draft %s: %w", draft.ID, err)
		}
	}
	progress.endPhase(fmt.Sprintf("%d drafts saved", len(drafts)))

	// Save discovery state for future incremental runs
	newState := &domain.DiscoveryState{
		LastRun:       time.Now(),
		FilesAnalyzed: filesAnalyzed,
	}
	// Record scope restrictions if any were applied
	if len(scope.paths) > 0 || len(scope.excludePatterns) > 0 {
		newState.Scope = &domain.DiscoveryScope{
			Paths:           scope.paths,
			ExcludePatterns: scope.excludePatterns,
		}
	}
	if err := store.SaveDiscoveryState(newState); err != nil {
		return fmt.Errorf("failed to save discovery state: %w", err)
	}

	// Print summary
	printDiscoverySummary(drafts, draftsDir)

	// Print total elapsed time
	progress.printTotalElapsed()

	return nil
}

// collectCodebaseContextIncremental gathers relevant files for Claude to analyze,
// optionally filtering to only include files modified since lastRun.
// Returns the context string and a map of analyzed files with their modification times.
func collectCodebaseContextIncremental(projectDir string, lastRun time.Time, incrementalMode bool, scope discoverScope, progress *discoverProgress) (string, map[string]time.Time, error) {
	var sb strings.Builder
	filesAnalyzed := make(map[string]time.Time)

	// Define file patterns to collect
	patterns := []struct {
		name    string
		glob    string
		maxSize int64
	}{
		{"Go Source Files", "**/*.go", 50000},
		{"Test Files", "**/*_test.go", 30000},
		{"Documentation", "**/*.md", 20000},
		{"YAML Config", "**/*.yaml", 10000},
	}

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

	for _, p := range patterns {
		var allFiles []collectedFile
		for _, root := range searchRoots {
			files, skipped, err := collectFilesIncremental(root, projectDir, p.glob, p.maxSize, lastRun, incrementalMode, scope.excludePatterns, progress)
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
			sb.WriteString(fmt.Sprintf("\n### %s\n\n", p.name))
			for _, f := range allFiles {
				progress.verbosePrintf("\n  Collected: %s", f.path)
				sb.WriteString(fmt.Sprintf("**File: %s**\n```\n%s\n```\n\n", f.path, f.content))
				filesAnalyzed[f.path] = f.modTime
			}
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

// collectFilesIncremental gathers files matching a pattern with size limit,
// optionally filtering to only include files modified since lastRun.
// The projectDir is used to compute paths relative to the project root.
// excludePatterns contains glob patterns to skip files matching those patterns.
// Returns collected files, skipped files (for verbose logging), and any error.
func collectFilesIncremental(root, projectDir, pattern string, maxTotalSize int64, lastRun time.Time, incrementalMode bool, excludePatterns []string, progress *discoverProgress) ([]collectedFile, []skippedFile, error) {
	var files []collectedFile
	var skipped []skippedFile
	var totalSize int64

	// Walk directory and find matching files
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

		// Check if file matches the include pattern
		matched, err := filepath.Match(pattern, filepath.Base(path))
		if err != nil || !matched {
			// Try glob-style matching for **/ patterns
			if !matchGlob(relPath, pattern) {
				return nil
			}
		}

		// In incremental mode, skip files not modified since last run
		if incrementalMode && !info.ModTime().After(lastRun) {
			if progress.verbose {
				skipped = append(skipped, skippedFile{path: relPath, reason: "not modified since last run"})
			}
			return nil
		}

		// Check size
		if totalSize+info.Size() > maxTotalSize {
			if progress.verbose {
				skipped = append(skipped, skippedFile{path: relPath, reason: "size limit exceeded"})
			}
			return nil // Skip if would exceed limit
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
			modTime: info.ModTime(),
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

// parseDraftsFromOutput extracts draft specs from Claude's YAML output
func parseDraftsFromOutput(output string) ([]*domain.DraftSpec, error) {
	// Find YAML block in output
	yamlContent := extractYAMLBlock(output)
	if yamlContent == "" {
		return nil, fmt.Errorf("no YAML block found in output")
	}

	var draftsOut draftsOutput
	if err := yaml.Unmarshal([]byte(yamlContent), &draftsOut); err != nil {
		return nil, fmt.Errorf("failed to parse YAML: %w", err)
	}

	var drafts []*domain.DraftSpec
	now := time.Now()

	for _, d := range draftsOut.Drafts {
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
			Created:          now,
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

		drafts = append(drafts, draft)
	}

	return drafts, nil
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
