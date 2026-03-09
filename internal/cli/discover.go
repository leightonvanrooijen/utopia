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

	"github.com/leightonvanrooijen/utopia/internal"
	"github.com/leightonvanrooijen/utopia/internal/analysis/types"
	"github.com/leightonvanrooijen/utopia/internal/domain"
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
	return &discoverProgress{startTime: now, phaseStart: now, totalPhases: totalPhases, verbose: verbose}
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
	fmt.Printf("\nTotal elapsed time: %.1fs\n", time.Since(p.startTime).Seconds())
}

func (p *discoverProgress) verbosePrintf(format string, args ...interface{}) {
	if p.verbose {
		fmt.Printf(format, args...)
	}
}

// discoverScope holds scope restrictions for discovery
type discoverScope struct {
	paths           []string
	excludePatterns []string
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

Scoping discovery:
  --path <dir>       Limit discovery to a specific directory
  --exclude <glob>   Exclude files matching glob pattern

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

## Output
`+"```yaml"+`
draft:
  id: %s
  title: "What User Can Do"
  description: |
    User-focused description with SPECIFIC details.
  confidence: high|medium|low
  discovered_from: ["source/file.go"]
  uncertainty_notes: ["What's unclear"]
  evidence:
    code_files: ["impl.go"]
    test_files: ["test.go"]
    doc_files: ["docs.md"]
    comments: ["Relevant code comments"]
  features:
    - id: feature-id
      description: "Specific capability with exact details"
      acceptance_criteria:
        - "Given [precondition], when [action], then [outcome]"
  domain_knowledge: ["Specific term: exact meaning"]
`+"```"+`
FIRST: Use your tools to read the source files and explore related code.
THEN: Output ONLY the YAML block with your refined specification.`,
		iteration, maxIterations, candidate.ID, candidate.Title, candidate.Description, sourceFilesStr, candidate.QualificationReason, candidate.ID)
}

func runDiscover(cmd *cobra.Command, args []string) error {
	absPath, utopiaDir, store, err := ResolveProject(cmd)
	if err != nil {
		return err
	}
	existingSpecs, _ := store.ListSpecs()
	existingDrafts, _ := store.ListDrafts()
	draftsDir := filepath.Join(utopiaDir, "drafts", "specs")
	if err := os.MkdirAll(draftsDir, 0755); err != nil {
		return fmt.Errorf("failed to create drafts/specs directory: %w", err)
	}

	scope := discoverScope{paths: discoverPathFlags, excludePatterns: discoverExcludeFlags}

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

	progress := newDiscoverProgress(4, discoverVerboseFlag)

	progress.startPhase(1, "Scanning files")
	codebaseContext, filesAnalyzed, err := collectCodebaseContext(absPath, scope, progress)
	if err != nil {
		return fmt.Errorf("failed to collect codebase context: %w", err)
	}
	if len(filesAnalyzed) == 0 {
		fmt.Printf(" done (no files found)\n")
		fmt.Println("No files to analyze.")
		progress.printTotalElapsed()
		return nil
	}
	progress.endPhase(fmt.Sprintf("%d files found", len(filesAnalyzed)))

	specsSummary := buildExistingSpecsSummary(existingSpecs)
	ctx := context.Background()
	cli := internal.NewCLI().WithVerbose(discoverVerboseFlag)

	progress.startPhase(2, "Stage 1: Identifying and qualifying candidates")
	stage1Prompt := buildIdentifyQualifyPrompt(codebaseContext, specsSummary)
	qualifiedOutput, err := cli.Prompt(ctx, stage1Prompt)
	if err != nil {
		return fmt.Errorf("identify and qualify failed: %w", err)
	}
	qualifiedCount := countYAMLItems(qualifiedOutput, "qualified")
	disqualifiedCount := countYAMLItems(qualifiedOutput, "disqualified")
	progress.endPhase(fmt.Sprintf("%d qualified, %d disqualified", qualifiedCount, disqualifiedCount))

	disqualified := parseDisqualifiedCandidates(qualifiedOutput)
	logDisqualifiedCandidates(disqualified, discoverVerboseFlag)
	qualified := parseQualifiedCandidates(qualifiedOutput)

	if len(qualified) == 0 {
		fmt.Println("\nNo candidates passed qualification criteria.")
		progress.printTotalElapsed()
		return nil
	}

	progress.startPhase(3, fmt.Sprintf("Stage 2: Refining %d candidates in parallel", len(qualified)))
	drafts, refinementErrors := runParallelRefinement(ctx, qualified, defaultRefinementIterations, discoverVerboseFlag)
	progress.endPhase(fmt.Sprintf("%d drafts refined", len(drafts)))

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

	progress.startPhase(4, "Saving drafts")
	for _, draft := range drafts {
		progress.verbosePrintf("\n  Saving %s.yaml", draft.ID)
		if err := store.SaveDraft(draft); err != nil {
			return fmt.Errorf("failed to save draft %s: %w", draft.ID, err)
		}
	}
	progress.endPhase(fmt.Sprintf("%d drafts saved", len(drafts)))

	printDiscoverySummary(drafts, draftsDir)
	progress.printTotalElapsed()
	return nil
}

func runParallelRefinement(ctx context.Context, candidates []qualifiedCandidate, iterations int, verbose bool) ([]*domain.DraftSpec, []error) {
	var (
		drafts []*domain.DraftSpec
		errors []error
		mu     sync.Mutex
		wg     sync.WaitGroup
	)

	refinementCLI := internal.NewCLI().WithVerbose(verbose).WithAllowedTools([]string{"Read", "Grep", "Glob"})

	for _, candidate := range candidates {
		wg.Add(1)
		go func(c qualifiedCandidate) {
			defer wg.Done()
			if verbose {
				fmt.Printf("\n  Starting refinement for: %s\n", c.ID)
			}
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
	wg.Wait()
	return drafts, errors
}

// YAML parsing types for spec discovery
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

func parseSingleDraftFromOutput(output string) (*domain.DraftSpec, error) {
	yamlContent := extractYAMLBlock(output)
	if yamlContent == "" {
		return nil, fmt.Errorf("no YAML block found in output")
	}
	var singleDraft struct {
		Draft draftOutput `yaml:"draft"`
	}
	if err := yaml.Unmarshal([]byte(yamlContent), &singleDraft); err == nil && singleDraft.Draft.ID != "" {
		return convertDraftOutput(singleDraft.Draft), nil
	}
	var draftsOut draftsOutput
	if err := yaml.Unmarshal([]byte(yamlContent), &draftsOut); err != nil {
		return nil, fmt.Errorf("failed to parse YAML: %w", err)
	}
	if len(draftsOut.Drafts) == 0 {
		return nil, fmt.Errorf("no drafts found in output")
	}
	return convertDraftOutput(draftsOut.Drafts[0]), nil
}

func convertDraftOutput(d draftOutput) *domain.DraftSpec {
	confidence := domain.DraftConfidenceMedium
	switch strings.ToLower(d.Confidence) {
	case "high":
		confidence = domain.DraftConfidenceHigh
	case "low":
		confidence = domain.DraftConfidenceLow
	}
	draft := &domain.DraftSpec{
		ID: d.ID, Title: d.Title, Created: time.Now(), Description: d.Description,
		Confidence: confidence, DiscoveredFrom: d.DiscoveredFrom, UncertaintyNotes: d.UncertaintyNotes,
		Evidence:        domain.DraftEvidence{CodeFiles: d.Evidence.CodeFiles, TestFiles: d.Evidence.TestFiles, DocFiles: d.Evidence.DocFiles, Comments: d.Evidence.Comments},
		DomainKnowledge: d.DomainKnowledge,
	}
	for _, f := range d.Features {
		draft.Features = append(draft.Features, domain.Feature{ID: f.ID, Description: f.Description, AcceptanceCriteria: f.AcceptanceCriteria})
	}
	return draft
}

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

func logDisqualifiedCandidates(disqualified []disqualifiedCandidate, verbose bool) {
	if len(disqualified) == 0 || !verbose {
		return
	}
	fmt.Println("\n  Disqualified candidates:")
	for _, d := range disqualified {
		fmt.Printf("    ✗ %s: %s\n", d.ID, d.Reason)
	}
}

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

// File collection types
type collectedFile struct {
	path    string
	content string
	modTime time.Time
}
type skippedFile struct {
	path   string
	reason string
}

func collectCodebaseContext(projectDir string, scope discoverScope, progress *discoverProgress) (string, []string, error) {
	var sb strings.Builder
	var filesAnalyzed []string

	searchRoots := scope.paths
	if len(searchRoots) == 0 {
		searchRoots = []string{projectDir}
	} else {
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

	var allFiles []collectedFile
	const maxTotalSize int64 = 200000
	for _, root := range searchRoots {
		files, skipped, err := collectAllTextFiles(root, projectDir, maxTotalSize, scope.excludePatterns, progress)
		if err != nil {
			continue
		}
		allFiles = append(allFiles, files...)
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

func collectAllTextFiles(root, projectDir string, maxTotalSize int64, excludePatterns []string, progress *discoverProgress) ([]collectedFile, []skippedFile, error) {
	var files []collectedFile
	var skipped []skippedFile
	var totalSize int64

	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			name := info.Name()
			if name == ".git" || name == "vendor" || name == "node_modules" || name == ".utopia" {
				return filepath.SkipDir
			}
			return nil
		}
		relPath, err := filepath.Rel(projectDir, path)
		if err != nil {
			return nil
		}
		if matchesAnyPattern(relPath, excludePatterns) {
			if progress.verbose {
				skipped = append(skipped, skippedFile{path: relPath, reason: "excluded by pattern"})
			}
			return nil
		}
		if totalSize+info.Size() > maxTotalSize {
			if progress.verbose {
				skipped = append(skipped, skippedFile{path: relPath, reason: "size limit exceeded"})
			}
			return nil
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		if !isTextFile(content) {
			if progress.verbose {
				skipped = append(skipped, skippedFile{path: relPath, reason: "binary file"})
			}
			return nil
		}
		files = append(files, collectedFile{path: relPath, content: truncateContent(string(content), 5000)})
		totalSize += info.Size()
		return nil
	})
	return files, skipped, err
}

func matchesAnyPattern(path string, patterns []string) bool {
	for _, pattern := range patterns {
		if matchGlob(path, pattern) {
			return true
		}
		if matched, _ := filepath.Match(pattern, filepath.Base(path)); matched {
			return true
		}
	}
	return false
}

func matchGlob(path, pattern string) bool {
	if strings.HasPrefix(pattern, "**/") {
		suffix := pattern[3:]
		if strings.HasSuffix(suffix, "/**") {
			dirPart := strings.TrimSuffix(suffix, "/**")
			return strings.HasPrefix(path, dirPart+"/") || path == dirPart
		}
		matched, _ := filepath.Match(suffix, filepath.Base(path))
		if matched {
			return true
		}
		if strings.Contains(suffix, "/") {
			return strings.Contains(path, strings.TrimPrefix(suffix, "**/"))
		}
	}
	return false
}

func isTextFile(content []byte) bool {
	if len(content) == 0 {
		return true
	}
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

func truncateContent(content string, maxLen int) string {
	if len(content) <= maxLen {
		return content
	}
	return content[:maxLen] + "\n... [truncated]"
}

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

func extractYAMLBlock(text string) string {
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

func printDiscoverySummary(drafts []*domain.DraftSpec, draftsDir string) {
	sort.Slice(drafts, func(i, j int) bool {
		confidenceOrder := map[domain.DraftConfidence]int{domain.DraftConfidenceHigh: 0, domain.DraftConfidenceMedium: 1, domain.DraftConfidenceLow: 2}
		return confidenceOrder[drafts[i].Confidence] < confidenceOrder[drafts[j].Confidence]
	})
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

// ============================================================================
// DOMAIN DISCOVERY - Subcommand for domain vocabulary discovery
// ============================================================================

var discoverDomainCmd = &cobra.Command{
	Use:   "domain",
	Short: "Scan codebase to discover domain vocabulary and bounded contexts",
	Long: `Analyze the codebase to discover domain terminology, bounded contexts, and propose draft domain documents.

The command will:
  1. Scan type definitions, package structure, and schemas
  2. Use Claude to analyze the codebase and identify bounded contexts
  3. Identify canonical terms and their relationships
  4. Generate draft domain docs with confidence levels
  5. Save drafts to .utopia/drafts/domain/ for review

Scoping discovery:
  --path <dir>       Limit discovery to a specific directory
  --exclude <glob>   Exclude files matching glob pattern
  --full             Force complete re-discovery (ignore incremental state)`,
	RunE: runDiscoverDomain,
}

var (
	discoverDomainFullFlag     bool
	discoverDomainPathFlags    []string
	discoverDomainExcludeFlags []string
	discoverDomainVerboseFlag  bool
)

func init() {
	discoverCmd.AddCommand(discoverDomainCmd)
	discoverDomainCmd.Flags().BoolVar(&discoverDomainFullFlag, "full", false, "Force complete re-discovery of entire codebase")
	discoverDomainCmd.Flags().StringSliceVar(&discoverDomainPathFlags, "path", nil, "Limit discovery to specific directory (can be specified multiple times)")
	discoverDomainCmd.Flags().StringSliceVar(&discoverDomainExcludeFlags, "exclude", nil, "Exclude files matching glob pattern (can be specified multiple times)")
	discoverDomainCmd.Flags().BoolVarP(&discoverDomainVerboseFlag, "verbose", "v", false, "Enable detailed file-by-file progress output")
}

const discoverDomainSystemPrompt = `You are a Domain Discovery Claude - an AI assistant that analyzes codebases to identify domain vocabulary, bounded contexts, and canonical terminology.

## Your Role
Analyze the provided codebase context and identify bounded contexts with their domain vocabulary.

**IMPORTANT**: Create a SEPARATE draft domain document for EACH bounded context identified.

## Codebase Context
%s

## Existing Domain Documents
%s

## Output Format
Generate draft domain documents in this EXACT YAML format:

` + "```yaml" + `
drafts:
  - id: bounded-context-name
    title: "Human Readable Context Title"
    bounded_context: bounded-context-name
    description: |
      Clear description of what this bounded context owns.
    confidence: high|medium|low
    discovered_from:
      - "path/to/type_definition.go"
    uncertainty_notes:
      - "Note about what's unclear (only for low confidence)"
    evidence:
      type_files:
        - "path/to/types.go"
      package_files:
        - "path/to/package/main.go"
      schema_files:
        - "path/to/schema.yaml"
      comments:
        - "Relevant code comment"
    terms:
      - term: CanonicalTermName
        canonical: true
        code_usage: "path/to/file.go - StructName"
        definition: "Clear definition of what this term means"
        aliases:
          - "AlternativeName"
        cross_context_note: "Optional note about how this term differs in other contexts"
        evidence:
          files:
            - "path/to/file.go"
          lines:
            - "path/to/file.go:42"
    entities:
      - name: EntityName
        description: "What this entity represents"
        relationships:
          - type: contains
            target: OtherEntity
` + "```" + `

Now analyze the codebase and generate draft domain documents.`

func runDiscoverDomain(cmd *cobra.Command, args []string) error {
	absPath, utopiaDir, store, err := ResolveProject(cmd)
	if err != nil {
		return err
	}
	existingDomainDocs, _ := store.ListDomainDocs()
	existingDrafts, _ := store.ListDraftDomainDocs()

	var lastRunTime time.Time
	previousState, err := store.LoadDomainDiscoveryState()
	if err != nil {
		return fmt.Errorf("failed to load domain discovery state: %w", err)
	}
	isIncremental := !discoverDomainFullFlag && previousState != nil
	if isIncremental {
		lastRunTime = previousState.LastRun
	}

	draftsDir := filepath.Join(utopiaDir, "drafts", "domain")
	if err := os.MkdirAll(draftsDir, 0755); err != nil {
		return fmt.Errorf("failed to create drafts/domain directory: %w", err)
	}

	scope := discoverScope{paths: discoverDomainPathFlags, excludePatterns: discoverDomainExcludeFlags}

	fmt.Println("Starting domain vocabulary discovery...")
	fmt.Printf("Project: %s\n", absPath)
	fmt.Printf("Existing domain docs: %d\n", len(existingDomainDocs))
	fmt.Printf("Existing draft domain docs: %d\n", len(existingDrafts))
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

	progress := newDiscoverProgress(4, discoverDomainVerboseFlag)

	progress.startPhase(1, "Scanning files")
	codebaseContext, filesAnalyzed, err := collectDomainContextIncremental(absPath, lastRunTime, isIncremental, scope, progress)
	if err != nil {
		return fmt.Errorf("failed to collect codebase context: %w", err)
	}
	if len(filesAnalyzed) == 0 {
		fmt.Printf(" done (no new files)\n")
		fmt.Println("No new or modified files to analyze.")
		fmt.Println("Use --full to force complete re-discovery.")
		progress.printTotalElapsed()
		return nil
	}
	progress.endPhase(fmt.Sprintf("%d files found", len(filesAnalyzed)))

	domainDocsSummary := buildExistingDomainDocsSummary(existingDomainDocs)
	systemPrompt := fmt.Sprintf(discoverDomainSystemPrompt, codebaseContext, domainDocsSummary)

	progress.startPhase(2, "Analyzing codebase with Claude")
	ctx := context.Background()
	cli := internal.NewCLI().WithVerbose(true)
	output, err := cli.Prompt(ctx, systemPrompt)
	if err != nil {
		return fmt.Errorf("claude analysis failed: %w", err)
	}
	progress.endPhase("")

	progress.startPhase(3, "Parsing draft domain documents")
	drafts, err := parseDomainDraftsFromOutput(output)
	if err != nil {
		return fmt.Errorf("failed to parse drafts: %w", err)
	}
	if len(drafts) == 0 {
		fmt.Printf(" done (no drafts found)\n")
		fmt.Println("No new draft domain documents discovered.")
		progress.printTotalElapsed()
		return nil
	}
	progress.endPhase(fmt.Sprintf("%d drafts parsed", len(drafts)))

	progress.startPhase(4, "Saving drafts")
	for _, draft := range drafts {
		progress.verbosePrintf("\n  Saving %s.yaml", draft.ID)
		if err := store.SaveDraftDomainDoc(draft); err != nil {
			return fmt.Errorf("failed to save draft %s: %w", draft.ID, err)
		}
	}
	progress.endPhase(fmt.Sprintf("%d drafts saved", len(drafts)))

	newState := &domain.DomainDiscoveryState{LastRun: time.Now(), FilesAnalyzed: filesAnalyzed}
	if len(scope.paths) > 0 || len(scope.excludePatterns) > 0 {
		newState.Scope = &domain.DiscoveryScope{Paths: scope.paths, ExcludePatterns: scope.excludePatterns}
	}
	if err := store.SaveDomainDiscoveryState(newState); err != nil {
		return fmt.Errorf("failed to save domain discovery state: %w", err)
	}

	printDomainDiscoverySummary(drafts, draftsDir)
	progress.printTotalElapsed()
	return nil
}

func collectDomainContextIncremental(projectDir string, lastRun time.Time, incrementalMode bool, scope discoverScope, progress *discoverProgress) (string, map[string]time.Time, error) {
	var sb strings.Builder
	filesAnalyzed := make(map[string]time.Time)
	typeAnalyzer := types.NewAnalyzer()
	var allDiscoveredTypes []*types.DiscoveredType

	patterns := []struct {
		name    string
		glob    string
		maxSize int64
	}{
		{"Go Type Definitions", "**/*.go", 50000},
		{"YAML Schemas/Config", "**/*.yaml", 15000},
		{"JSON Schemas", "**/*.json", 15000},
		{"Protocol Buffers", "**/*.proto", 20000},
		{"GraphQL Schemas", "**/*.graphql", 15000},
		{"TypeScript Types", "**/*.ts", 30000},
	}

	searchRoots := scope.paths
	if len(searchRoots) == 0 {
		searchRoots = []string{projectDir}
	} else {
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
			files, skipped, err := collectDomainFilesIncremental(root, projectDir, p.glob, p.maxSize, lastRun, incrementalMode, scope.excludePatterns, progress)
			if err != nil {
				continue
			}
			allFiles = append(allFiles, files...)
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
				if strings.HasSuffix(f.path, ".go") {
					discoveredTypes := typeAnalyzer.AnalyzeGoFile(f.path, f.content)
					allDiscoveredTypes = append(allDiscoveredTypes, discoveredTypes...)
				} else if strings.HasSuffix(f.path, ".ts") {
					discoveredTypes := typeAnalyzer.AnalyzeTypeScriptFile(f.path, f.content)
					allDiscoveredTypes = append(allDiscoveredTypes, discoveredTypes...)
				}
			}
		}
	}

	if len(allDiscoveredTypes) > 0 {
		bcAnalyzer := types.NewBoundedContextAnalyzer()
		contextTerms := bcAnalyzer.GroupTermsByContext(allDiscoveredTypes)
		crossContextTerms := bcAnalyzer.FindCrossContextTerms(contextTerms)

		sb.WriteString("\n### Pre-Analyzed Domain Terms by Bounded Context\n\n")
		sb.WriteString("Terms are grouped by their inferred bounded context based on package structure.\n\n")

		var contextNames []string
		for ctx := range contextTerms {
			contextNames = append(contextNames, ctx)
		}
		sort.Strings(contextNames)

		for _, ctx := range contextNames {
			terms := contextTerms[ctx]
			if len(terms) == 0 {
				continue
			}
			sb.WriteString(fmt.Sprintf("#### Bounded Context: %s\n\n", ctx))
			for _, conf := range []types.TermConfidence{types.TermConfidenceHigh, types.TermConfidenceMedium, types.TermConfidenceLow} {
				var termsAtLevel []*types.ContextualTerm
				for _, term := range terms {
					if term.Confidence == conf {
						termsAtLevel = append(termsAtLevel, term)
					}
				}
				if len(termsAtLevel) > 0 {
					sb.WriteString(fmt.Sprintf("**%s Confidence:**\n", titleCase(string(conf))))
					for _, term := range termsAtLevel {
						typeKind := ""
						if len(term.Types) > 0 {
							typeKind = term.Types[0].Kind
						}
						sb.WriteString(fmt.Sprintf("- **%s**", term.Term))
						if typeKind != "" {
							sb.WriteString(fmt.Sprintf(" (%s)", typeKind))
						}
						sb.WriteString(fmt.Sprintf(" - found in %d file(s)\n", len(term.Files)))
						if otherContexts, exists := crossContextTerms[term.Term]; exists {
							var others []string
							for _, other := range otherContexts {
								if other != ctx {
									others = append(others, other)
								}
							}
							if len(others) > 0 {
								sb.WriteString(fmt.Sprintf("  - ⚠ Also appears in: %s\n", strings.Join(others, ", ")))
							}
						}
						evidenceLimit := 3
						for i, line := range term.Lines {
							if i >= evidenceLimit {
								sb.WriteString(fmt.Sprintf("  - ... and %d more locations\n", len(term.Lines)-evidenceLimit))
								break
							}
							sb.WriteString(fmt.Sprintf("  - %s\n", line))
						}
					}
					sb.WriteString("\n")
				}
			}
		}

		if len(crossContextTerms) > 0 {
			sb.WriteString("### Cross-Context Terms\n\n")
			for term, contexts := range crossContextTerms {
				sb.WriteString(fmt.Sprintf("- **%s**: appears in %s\n", term, strings.Join(contexts, ", ")))
			}
			sb.WriteString("\n")
		}
	}
	return sb.String(), filesAnalyzed, nil
}

func collectDomainFilesIncremental(root, projectDir, pattern string, maxTotalSize int64, lastRun time.Time, incrementalMode bool, excludePatterns []string, progress *discoverProgress) ([]collectedFile, []skippedFile, error) {
	var files []collectedFile
	var skipped []skippedFile
	var totalSize int64

	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			name := info.Name()
			if name == ".git" || name == "vendor" || name == "node_modules" || name == ".utopia" {
				return filepath.SkipDir
			}
			return nil
		}
		relPath, err := filepath.Rel(projectDir, path)
		if err != nil {
			return nil
		}
		if strings.HasSuffix(relPath, "_test.go") {
			if progress.verbose {
				skipped = append(skipped, skippedFile{path: relPath, reason: "test file"})
			}
			return nil
		}
		if strings.Contains(filepath.Base(relPath), "mock") {
			if progress.verbose {
				skipped = append(skipped, skippedFile{path: relPath, reason: "mock file"})
			}
			return nil
		}
		if strings.Contains(relPath, "generated") || strings.HasSuffix(relPath, ".gen.go") {
			if progress.verbose {
				skipped = append(skipped, skippedFile{path: relPath, reason: "generated file"})
			}
			return nil
		}
		if matchesAnyPattern(relPath, excludePatterns) {
			if progress.verbose {
				skipped = append(skipped, skippedFile{path: relPath, reason: "excluded by pattern"})
			}
			return nil
		}
		matched, err := filepath.Match(pattern, filepath.Base(path))
		if err != nil || !matched {
			if !matchGlob(relPath, pattern) {
				return nil
			}
		}
		if incrementalMode && !info.ModTime().After(lastRun) {
			if progress.verbose {
				skipped = append(skipped, skippedFile{path: relPath, reason: "not modified since last run"})
			}
			return nil
		}
		if totalSize+info.Size() > maxTotalSize {
			if progress.verbose {
				skipped = append(skipped, skippedFile{path: relPath, reason: "size limit exceeded"})
			}
			return nil
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		if !isTextFile(content) {
			if progress.verbose {
				skipped = append(skipped, skippedFile{path: relPath, reason: "binary file"})
			}
			return nil
		}
		files = append(files, collectedFile{path: relPath, content: truncateContent(string(content), 5000), modTime: info.ModTime()})
		totalSize += info.Size()
		return nil
	})
	return files, skipped, err
}

func buildExistingDomainDocsSummary(docs []*domain.DomainDoc) string {
	if len(docs) == 0 {
		return "(No existing domain documents)"
	}
	var sb strings.Builder
	for _, doc := range docs {
		sb.WriteString(fmt.Sprintf("- **%s** (%s): %s\n", doc.Title, doc.BoundedContext, truncateContent(doc.Description, 100)))
		for _, term := range doc.Terms {
			sb.WriteString(fmt.Sprintf("  - %s: %s\n", term.Term, truncateContent(term.Definition, 80)))
		}
	}
	return sb.String()
}

// YAML parsing types for domain discovery
type domainDraftsOutput struct {
	Drafts []domainDraftOutput `yaml:"drafts"`
}
type domainDraftOutput struct {
	ID               string               `yaml:"id"`
	Title            string               `yaml:"title"`
	BoundedContext   string               `yaml:"bounded_context"`
	Description      string               `yaml:"description"`
	Confidence       string               `yaml:"confidence"`
	DiscoveredFrom   []string             `yaml:"discovered_from,omitempty"`
	UncertaintyNotes []string             `yaml:"uncertainty_notes,omitempty"`
	Evidence         domainEvidenceOutput `yaml:"evidence"`
	Terms            []domainTermOutput   `yaml:"terms,omitempty"`
	Entities         []domainEntityOutput `yaml:"entities,omitempty"`
}
type domainEvidenceOutput struct {
	TypeFiles    []string `yaml:"type_files,omitempty"`
	PackageFiles []string `yaml:"package_files,omitempty"`
	SchemaFiles  []string `yaml:"schema_files,omitempty"`
	Comments     []string `yaml:"comments,omitempty"`
}
type domainTermOutput struct {
	Term             string                    `yaml:"term"`
	Canonical        bool                      `yaml:"canonical"`
	CodeUsage        string                    `yaml:"code_usage"`
	Definition       string                    `yaml:"definition"`
	Aliases          []string                  `yaml:"aliases,omitempty"`
	CrossContextNote string                    `yaml:"cross_context_note,omitempty"`
	Evidence         *domainTermEvidenceOutput `yaml:"evidence,omitempty"`
}
type domainTermEvidenceOutput struct {
	Files []string `yaml:"files,omitempty"`
	Lines []string `yaml:"lines,omitempty"`
}
type domainEntityOutput struct {
	Name          string                     `yaml:"name"`
	Description   string                     `yaml:"description,omitempty"`
	Relationships []domainRelationshipOutput `yaml:"relationships,omitempty"`
}
type domainRelationshipOutput struct {
	Type   string `yaml:"type"`
	Target string `yaml:"target"`
}

func parseDomainDraftsFromOutput(output string) ([]*domain.DraftDomainDoc, error) {
	yamlContent := extractYAMLBlock(output)
	if yamlContent == "" {
		return nil, fmt.Errorf("no YAML block found in output")
	}
	var draftsOut domainDraftsOutput
	if err := yaml.Unmarshal([]byte(yamlContent), &draftsOut); err != nil {
		return nil, fmt.Errorf("failed to parse YAML: %w", err)
	}

	var drafts []*domain.DraftDomainDoc
	now := time.Now()
	for _, d := range draftsOut.Drafts {
		confidence := domain.DraftDomainConfidenceMedium
		switch strings.ToLower(d.Confidence) {
		case "high":
			confidence = domain.DraftDomainConfidenceHigh
		case "low":
			confidence = domain.DraftDomainConfidenceLow
		}
		draft := &domain.DraftDomainDoc{
			ID: d.ID, Title: d.Title, BoundedContext: d.BoundedContext, Description: d.Description,
			Confidence: confidence, Created: now, DiscoveredFrom: d.DiscoveredFrom, UncertaintyNotes: d.UncertaintyNotes,
			Evidence: domain.DraftDomainEvidence{TypeFiles: d.Evidence.TypeFiles, PackageFiles: d.Evidence.PackageFiles, SchemaFiles: d.Evidence.SchemaFiles, Comments: d.Evidence.Comments},
		}
		for _, t := range d.Terms {
			term := domain.DomainTerm{Term: t.Term, Canonical: t.Canonical, CodeUsage: t.CodeUsage, Definition: t.Definition, Aliases: t.Aliases, CrossContextNote: t.CrossContextNote}
			if t.Evidence != nil && (len(t.Evidence.Files) > 0 || len(t.Evidence.Lines) > 0) {
				term.Evidence = &domain.TermEvidence{Files: t.Evidence.Files, Lines: t.Evidence.Lines}
			}
			draft.Terms = append(draft.Terms, term)
		}
		for _, e := range d.Entities {
			entity := domain.DomainEntity{Name: e.Name, Description: e.Description}
			for _, r := range e.Relationships {
				entity.Relationships = append(entity.Relationships, domain.EntityRelationship{Type: r.Type, Target: r.Target})
			}
			draft.Entities = append(draft.Entities, entity)
		}
		drafts = append(drafts, draft)
	}
	return drafts, nil
}

func titleCase(s string) string {
	if len(s) == 0 {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

func printDomainDiscoverySummary(drafts []*domain.DraftDomainDoc, draftsDir string) {
	sort.Slice(drafts, func(i, j int) bool {
		confidenceOrder := map[domain.DraftDomainConfidence]int{domain.DraftDomainConfidenceHigh: 0, domain.DraftDomainConfidenceMedium: 1, domain.DraftDomainConfidenceLow: 2}
		return confidenceOrder[drafts[i].Confidence] < confidenceOrder[drafts[j].Confidence]
	})
	counts := map[domain.DraftDomainConfidence]int{}
	for _, d := range drafts {
		counts[d.Confidence]++
	}

	fmt.Println()
	fmt.Println("═══════════════════════════════════════════════════════════════")
	fmt.Println("                DOMAIN DISCOVERY COMPLETE")
	fmt.Println("═══════════════════════════════════════════════════════════════")
	fmt.Println()
	fmt.Printf("Created %d draft domain documents:\n", len(drafts))
	fmt.Printf("  • HIGH confidence:   %d\n", counts[domain.DraftDomainConfidenceHigh])
	fmt.Printf("  • MEDIUM confidence: %d\n", counts[domain.DraftDomainConfidenceMedium])
	fmt.Printf("  • LOW confidence:    %d\n", counts[domain.DraftDomainConfidenceLow])
	fmt.Println()
	fmt.Println("Drafts saved to:", draftsDir)
	fmt.Println()
	fmt.Println("Draft Domain Documents:")
	fmt.Println("───────────────────────────────────────────────────────────────")
	for _, d := range drafts {
		confidenceIcon := "○"
		switch d.Confidence {
		case domain.DraftDomainConfidenceHigh:
			confidenceIcon = "●"
		case domain.DraftDomainConfidenceMedium:
			confidenceIcon = "◐"
		}
		fmt.Printf("\n%s [%s] %s\n", confidenceIcon, strings.ToUpper(string(d.Confidence)), d.Title)
		fmt.Printf("  Bounded Context: %s\n", d.BoundedContext)
		fmt.Printf("  Terms: %d\n", len(d.Terms))
		fmt.Printf("  Entities: %d\n", len(d.Entities))
		if d.HasTypeDefinitions() {
			fmt.Printf("  Type files: %d\n", len(d.Evidence.TypeFiles))
		}
		if d.HasSchemas() {
			fmt.Printf("  Schema files: %d\n", len(d.Evidence.SchemaFiles))
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
	fmt.Println("  2. Validate terminology with domain experts")
	fmt.Println("  3. Promote validated drafts to official domain docs")
	fmt.Println()
}
