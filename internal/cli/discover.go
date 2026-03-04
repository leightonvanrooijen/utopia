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

var discoverCmd = &cobra.Command{
	Use:   "discover",
	Short: "Scan codebase and propose draft specifications",
	Long: `Analyze the codebase to discover existing system behavior and propose draft specifications.

The command will:
  1. Scan source code, tests, documentation, and comments
  2. Use Claude to analyze the codebase and identify features
  3. Generate draft specs with confidence levels based on evidence quality
  4. Save drafts to .utopia/drafts/ for review

Incremental discovery:
  Re-running discover after codebase changes only analyzes new or modified files.
  Use --full to force complete re-discovery of the entire codebase.

Confidence levels:
  - HIGH: Tests exist with clear boundaries and documentation
  - MEDIUM: Some tests or docs exist, but gaps remain
  - LOW: Inferred from code patterns only (includes uncertainty notes)

After discovery, use 'utopia shape' to validate and refine drafts before
promoting them to official specifications.`,
	RunE: runDiscover,
}

var discoverFullFlag bool

func init() {
	rootCmd.AddCommand(discoverCmd)
	discoverCmd.Flags().BoolVar(&discoverFullFlag, "full", false, "Force complete re-discovery of entire codebase")
}

// discoverSystemPrompt guides Claude through codebase analysis and draft spec generation
const discoverSystemPrompt = `You are a Discovery Claude - an AI assistant that analyzes codebases to identify existing system behavior and propose draft specifications.

## Your Role
Analyze the provided codebase context (source code, tests, documentation, comments) and identify distinct features or capabilities that should be documented as specifications.

## Codebase Context
%s

## Existing Specifications
%s

## Guidelines for Discovery

### What to Look For
1. **Distinct Features**: Self-contained capabilities with clear boundaries
2. **Behavioral Patterns**: How the system responds to different inputs
3. **Integration Points**: APIs, commands, data flows between components
4. **Domain Concepts**: Core entities and their relationships

### Evidence Quality Assessment
For each discovered feature, assess the evidence:
- **Tests**: Do tests exist that verify this behavior?
- **Documentation**: Is this behavior documented?
- **Clear Boundaries**: Are the feature boundaries well-defined?
- **Code Comments**: Do comments explain the intent?

### Confidence Levels
Assign confidence based on evidence:
- **HIGH**: Tests exist AND (docs exist OR very clear code boundaries)
- **MEDIUM**: Tests exist OR docs exist (but not both)
- **LOW**: Inferred from code patterns only

### Uncertainty Notes
For LOW confidence drafts, include notes explaining:
- What aspects are unclear
- What additional information would help
- Potential alternative interpretations

## Output Format

Generate draft specifications in this EXACT YAML format. Output ONLY the YAML block, no additional text:

` + "```yaml" + `
drafts:
  - id: feature-name-kebab-case
    title: "Human Readable Feature Title"
    description: |
      Clear description of what this feature does.
      Include the business value and main use cases.
    confidence: high|medium|low
    discovered_from:
      - "path/to/source_file_analyzed.go"
      - "path/to/another_source.go"
    uncertainty_notes:
      - "Note about what's unclear (only for low confidence)"
    evidence:
      code_files:
        - "path/to/implementation.go"
      test_files:
        - "path/to/implementation_test.go"
      doc_files:
        - "path/to/docs.md"
      comments:
        - "Relevant code comment that explains intent"
    features:
      - id: sub-feature-id
        description: "What this specific capability does"
        acceptance_criteria:
          - "Given X, when Y, then Z"
          - "Must handle error case A"
    domain_knowledge:
      - "Important domain concept relevant to this spec"
` + "```" + `

## Important Rules
1. Create SEPARATE draft specs for distinct features - don't combine unrelated functionality
2. Use kebab-case for all IDs
3. Focus on BEHAVIOR, not implementation details
4. Acceptance criteria should be testable
5. Don't duplicate existing specifications - check the "Existing Specifications" section
6. If a feature is partially covered by an existing spec, note this in uncertainty_notes

Now analyze the codebase and generate draft specifications.`

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

	// Ensure drafts directory exists
	draftsDir := filepath.Join(utopiaDir, "drafts")
	if err := os.MkdirAll(draftsDir, 0755); err != nil {
		return fmt.Errorf("failed to create drafts directory: %w", err)
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
	fmt.Println()

	// Collect codebase context (with optional time filter for incremental)
	fmt.Println("Collecting codebase context...")
	codebaseContext, filesAnalyzed, err := collectCodebaseContextIncremental(absPath, lastRunTime, isIncremental)
	if err != nil {
		return fmt.Errorf("failed to collect codebase context: %w", err)
	}

	// Check if there are any files to analyze
	if len(filesAnalyzed) == 0 {
		fmt.Println("No new or modified files to analyze.")
		fmt.Println("Use --full to force complete re-discovery.")
		return nil
	}

	fmt.Printf("Files to analyze: %d\n", len(filesAnalyzed))

	// Build existing specs summary
	specsSummary := buildExistingSpecsSummary(existingSpecs)

	// Build system prompt
	systemPrompt := fmt.Sprintf(discoverSystemPrompt, codebaseContext, specsSummary)

	fmt.Println("Analyzing codebase with Claude...")
	fmt.Println()

	// Run Claude analysis
	ctx := context.Background()
	cli := claude.NewCLI().WithVerbose(true)

	output, err := cli.Prompt(ctx, systemPrompt)
	if err != nil {
		return fmt.Errorf("claude analysis failed: %w", err)
	}

	// Parse drafts from Claude output
	drafts, err := parseDraftsFromOutput(output)
	if err != nil {
		return fmt.Errorf("failed to parse drafts: %w", err)
	}

	if len(drafts) == 0 {
		fmt.Println("No new draft specifications discovered.")
		return nil
	}

	// Save drafts
	for _, draft := range drafts {
		if err := store.SaveDraft(draft); err != nil {
			return fmt.Errorf("failed to save draft %s: %w", draft.ID, err)
		}
	}

	// Save discovery state for future incremental runs
	newState := &domain.DiscoveryState{
		LastRun:       time.Now(),
		FilesAnalyzed: filesAnalyzed,
	}
	if err := store.SaveDiscoveryState(newState); err != nil {
		return fmt.Errorf("failed to save discovery state: %w", err)
	}

	// Print summary
	printDiscoverySummary(drafts, draftsDir)

	return nil
}

// collectCodebaseContextIncremental gathers relevant files for Claude to analyze,
// optionally filtering to only include files modified since lastRun.
// Returns the context string and a map of analyzed files with their modification times.
func collectCodebaseContextIncremental(projectDir string, lastRun time.Time, incrementalMode bool) (string, map[string]time.Time, error) {
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

	for _, p := range patterns {
		files, err := collectFilesIncremental(projectDir, p.glob, p.maxSize, lastRun, incrementalMode)
		if err != nil {
			continue // Skip on error, don't fail entire discovery
		}

		if len(files) > 0 {
			sb.WriteString(fmt.Sprintf("\n### %s\n\n", p.name))
			for _, f := range files {
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

// collectFilesIncremental gathers files matching a pattern with size limit,
// optionally filtering to only include files modified since lastRun.
func collectFilesIncremental(root, pattern string, maxTotalSize int64, lastRun time.Time, incrementalMode bool) ([]collectedFile, error) {
	var files []collectedFile
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

		// Check if file matches pattern
		relPath, err := filepath.Rel(root, path)
		if err != nil {
			return nil
		}

		matched, err := filepath.Match(pattern, filepath.Base(path))
		if err != nil || !matched {
			// Try glob-style matching for **/ patterns
			if !matchGlob(relPath, pattern) {
				return nil
			}
		}

		// In incremental mode, skip files not modified since last run
		if incrementalMode && !info.ModTime().After(lastRun) {
			return nil
		}

		// Check size
		if totalSize+info.Size() > maxTotalSize {
			return nil // Skip if would exceed limit
		}

		// Read file
		content, err := os.ReadFile(path)
		if err != nil {
			return nil
		}

		// Skip binary files
		if !isTextFile(content) {
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

	return files, err
}

// matchGlob does simple glob matching for **/*.ext patterns
func matchGlob(path, pattern string) bool {
	// Handle **/*.ext pattern
	if strings.HasPrefix(pattern, "**/") {
		suffix := pattern[3:]
		if strings.HasPrefix(suffix, "*.") {
			ext := suffix[1:]
			return strings.HasSuffix(path, ext)
		}
		return strings.HasSuffix(path, suffix)
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
