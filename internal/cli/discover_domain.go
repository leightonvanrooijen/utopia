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

var discoverDomainCmd = &cobra.Command{
	Use:   "domain",
	Short: "Scan codebase to discover domain vocabulary and bounded contexts",
	Long: `Analyze the codebase to discover domain terminology, bounded contexts, and propose draft domain documents.

The command will:
  1. Scan type definitions, package structure, and schemas
  2. Use Claude to analyze the codebase and identify bounded contexts
  3. Identify canonical terms and their relationships
  4. Generate draft domain docs with confidence levels based on evidence quality
  5. Save drafts to .utopia/drafts/domain/ for review

Scoping discovery:
  By default, discover domain analyzes the entire codebase. For large codebases or to
  focus on specific modules, use scoping flags:

  --path <dir>       Limit discovery to a specific directory
                     Can be specified multiple times for multiple directories
  --exclude <glob>   Exclude files matching a glob pattern
                     Can be specified multiple times for multiple patterns

  Examples:
    utopia discover domain --path internal/domain --path internal/api
    utopia discover domain --exclude "**/*_test.go" --exclude "**/mock_*.go"

Incremental discovery:
  Re-running discover domain after codebase changes only analyzes new or modified files.
  Use --full to force complete re-discovery of the entire codebase.

Confidence levels:
  - HIGH: Clear type definitions, consistent naming, documentation exists
  - MEDIUM: Type definitions exist but naming is inconsistent or undocumented
  - LOW: Inferred from code patterns only (includes uncertainty notes)

After discovery, review drafts and validate them before promoting to official domain docs.`,
	RunE: runDiscoverDomain,
}

var (
	discoverDomainFullFlag     bool
	discoverDomainPathFlags    []string
	discoverDomainExcludeFlags []string
)

func init() {
	discoverCmd.AddCommand(discoverDomainCmd)
	discoverDomainCmd.Flags().BoolVar(&discoverDomainFullFlag, "full", false, "Force complete re-discovery of entire codebase")
	discoverDomainCmd.Flags().StringSliceVar(&discoverDomainPathFlags, "path", nil, "Limit discovery to specific directory (can be specified multiple times)")
	discoverDomainCmd.Flags().StringSliceVar(&discoverDomainExcludeFlags, "exclude", nil, "Exclude files matching glob pattern (can be specified multiple times)")
}

// discoverDomainSystemPrompt guides Claude through codebase analysis for domain vocabulary discovery
const discoverDomainSystemPrompt = `You are a Domain Discovery Claude - an AI assistant that analyzes codebases to identify domain vocabulary, bounded contexts, and canonical terminology.

## Your Role
Analyze the provided codebase context (type definitions, package structure, schemas) and identify bounded contexts with their domain vocabulary.

## Codebase Context
%s

## Existing Domain Documents
%s

## Guidelines for Domain Discovery

### What to Look For
1. **Bounded Contexts**: Distinct areas of the system with their own vocabulary
2. **Type Definitions**: Structs, interfaces, enums that define domain concepts
3. **Canonical Terms**: The authoritative names used in code and should be used in communication
4. **Aliases**: Alternative names that map to canonical terms
5. **Entity Relationships**: How domain concepts relate to each other
6. **Package Boundaries**: How packages organize domain concepts

### Evidence Quality Assessment
For each discovered bounded context, assess the evidence:
- **Type Definitions**: Are there clear structs/interfaces defining domain concepts?
- **Consistent Naming**: Are terms used consistently across the codebase?
- **Documentation**: Are domain concepts documented in code or docs?
- **Code Usage**: Can you identify where terms appear in the codebase?

### Confidence Levels
Assign confidence based on evidence:
- **HIGH**: Clear type definitions AND consistent naming AND (docs OR strong code patterns)
- **MEDIUM**: Type definitions exist OR consistent naming (but not both, or inconsistencies present)
- **LOW**: Inferred from code patterns only, naming inconsistent

### Uncertainty Notes
For LOW confidence drafts, include notes explaining:
- What aspects are unclear
- Where naming is inconsistent
- What additional information would help
- Potential alternative interpretations

## Output Format

Generate draft domain documents in this EXACT YAML format. Output ONLY the YAML block, no additional text:

` + "```yaml" + `
drafts:
  - id: bounded-context-name
    title: "Human Readable Context Title"
    bounded_context: bounded-context-name
    description: |
      Clear description of what this bounded context owns.
      Include the scope and boundaries of this context.
    confidence: high|medium|low
    discovered_from:
      - "path/to/type_definition.go"
      - "path/to/another_source.go"
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
        - "Relevant code comment explaining domain concept"
    terms:
      - term: CanonicalTermName
        canonical: true
        code_usage: "path/to/file.go - StructName; used in functionX"
        definition: "Clear definition of what this term means in this context"
        aliases:
          - "AlternativeName"
          - "OtherName"
        cross_context_note: "Optional note about how this term differs in other contexts"
    entities:
      - name: EntityName
        description: "What this entity represents"
        relationships:
          - type: contains
            target: OtherEntity
          - type: produces
            target: AnotherEntity
` + "```" + `

## Important Rules
1. Create SEPARATE draft domain docs for distinct bounded contexts
2. Use kebab-case for IDs and bounded_context values
3. Use PascalCase for term names (matching Go naming conventions)
4. Focus on VOCABULARY and RELATIONSHIPS, not implementation details
5. Don't duplicate existing domain documents - check the "Existing Domain Documents" section
6. If a term appears in multiple contexts with different meanings, note this in cross_context_note
7. Canonical terms should match actual code identifiers where possible

Now analyze the codebase and generate draft domain documents.`

func runDiscoverDomain(cmd *cobra.Command, args []string) error {
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

	// Load existing domain docs to avoid duplicates
	existingDomainDocs, err := store.ListDomainDocs()
	if err != nil {
		existingDomainDocs = []*domain.DomainDoc{}
	}

	// Load existing draft domain docs to show status
	existingDrafts, err := store.ListDraftDomainDocs()
	if err != nil {
		existingDrafts = []*domain.DraftDomainDoc{}
	}

	// Load previous discovery state for incremental discovery
	var lastRunTime time.Time
	previousState, err := store.LoadDomainDiscoveryState()
	if err != nil {
		return fmt.Errorf("failed to load domain discovery state: %w", err)
	}

	isIncremental := !discoverDomainFullFlag && previousState != nil
	if isIncremental {
		lastRunTime = previousState.LastRun
	}

	// Ensure drafts/domain directory exists
	draftsDir := filepath.Join(utopiaDir, "drafts", "domain")
	if err := os.MkdirAll(draftsDir, 0755); err != nil {
		return fmt.Errorf("failed to create drafts/domain directory: %w", err)
	}

	// Build scope from flags
	scope := discoverScope{
		paths:           discoverDomainPathFlags,
		excludePatterns: discoverDomainExcludeFlags,
	}

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

	// Collect codebase context focused on type definitions and structure
	fmt.Println("Collecting codebase context (types, packages, schemas)...")
	codebaseContext, filesAnalyzed, err := collectDomainContextIncremental(absPath, lastRunTime, isIncremental, scope)
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

	// Build existing domain docs summary
	domainDocsSummary := buildExistingDomainDocsSummary(existingDomainDocs)

	// Build system prompt
	systemPrompt := fmt.Sprintf(discoverDomainSystemPrompt, codebaseContext, domainDocsSummary)

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
	drafts, err := parseDomainDraftsFromOutput(output)
	if err != nil {
		return fmt.Errorf("failed to parse drafts: %w", err)
	}

	if len(drafts) == 0 {
		fmt.Println("No new draft domain documents discovered.")
		return nil
	}

	// Save drafts
	for _, draft := range drafts {
		if err := store.SaveDraftDomainDoc(draft); err != nil {
			return fmt.Errorf("failed to save draft %s: %w", draft.ID, err)
		}
	}

	// Save discovery state for future incremental runs
	newState := &domain.DomainDiscoveryState{
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
	if err := store.SaveDomainDiscoveryState(newState); err != nil {
		return fmt.Errorf("failed to save domain discovery state: %w", err)
	}

	// Print summary
	printDomainDiscoverySummary(drafts, draftsDir)

	return nil
}

// collectDomainContextIncremental gathers relevant files for domain analysis,
// focusing on type definitions, package structure, and schemas.
func collectDomainContextIncremental(projectDir string, lastRun time.Time, incrementalMode bool, scope discoverScope) (string, map[string]time.Time, error) {
	var sb strings.Builder
	filesAnalyzed := make(map[string]time.Time)

	// Define file patterns focused on domain vocabulary discovery
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
			files, err := collectDomainFilesIncremental(root, projectDir, p.glob, p.maxSize, lastRun, incrementalMode, scope.excludePatterns)
			if err != nil {
				continue // Skip on error, don't fail entire discovery
			}
			allFiles = append(allFiles, files...)
		}

		if len(allFiles) > 0 {
			sb.WriteString(fmt.Sprintf("\n### %s\n\n", p.name))
			for _, f := range allFiles {
				sb.WriteString(fmt.Sprintf("**File: %s**\n```\n%s\n```\n\n", f.path, f.content))
				filesAnalyzed[f.path] = f.modTime
			}
		}
	}

	return sb.String(), filesAnalyzed, nil
}

// collectDomainFilesIncremental gathers files for domain analysis, prioritizing
// files with type definitions and filtering out test files and generated code.
func collectDomainFilesIncremental(root, projectDir, pattern string, maxTotalSize int64, lastRun time.Time, incrementalMode bool, excludePatterns []string) ([]collectedFile, error) {
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

		// Compute path relative to project root for consistent reporting
		relPath, err := filepath.Rel(projectDir, path)
		if err != nil {
			return nil
		}

		// Skip test files for domain discovery (we want core type definitions)
		if strings.HasSuffix(relPath, "_test.go") {
			return nil
		}

		// Skip mock files
		if strings.Contains(filepath.Base(relPath), "mock") {
			return nil
		}

		// Skip generated files
		if strings.Contains(relPath, "generated") || strings.HasSuffix(relPath, ".gen.go") {
			return nil
		}

		// Check if file matches any exclude pattern
		if matchesAnyPattern(relPath, excludePatterns) {
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

// buildExistingDomainDocsSummary creates a summary of existing domain docs for Claude
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

// domainDraftsOutput represents the YAML structure Claude outputs for domain discovery
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
	Term             string   `yaml:"term"`
	Canonical        bool     `yaml:"canonical"`
	CodeUsage        string   `yaml:"code_usage"`
	Definition       string   `yaml:"definition"`
	Aliases          []string `yaml:"aliases,omitempty"`
	CrossContextNote string   `yaml:"cross_context_note,omitempty"`
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

// parseDomainDraftsFromOutput extracts draft domain docs from Claude's YAML output
func parseDomainDraftsFromOutput(output string) ([]*domain.DraftDomainDoc, error) {
	// Find YAML block in output
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
			ID:               d.ID,
			Title:            d.Title,
			BoundedContext:   d.BoundedContext,
			Description:      d.Description,
			Confidence:       confidence,
			Created:          now,
			DiscoveredFrom:   d.DiscoveredFrom,
			UncertaintyNotes: d.UncertaintyNotes,
			Evidence: domain.DraftDomainEvidence{
				TypeFiles:    d.Evidence.TypeFiles,
				PackageFiles: d.Evidence.PackageFiles,
				SchemaFiles:  d.Evidence.SchemaFiles,
				Comments:     d.Evidence.Comments,
			},
		}

		// Convert terms
		for _, t := range d.Terms {
			draft.Terms = append(draft.Terms, domain.DomainTerm{
				Term:             t.Term,
				Canonical:        t.Canonical,
				CodeUsage:        t.CodeUsage,
				Definition:       t.Definition,
				Aliases:          t.Aliases,
				CrossContextNote: t.CrossContextNote,
			})
		}

		// Convert entities
		for _, e := range d.Entities {
			entity := domain.DomainEntity{
				Name:        e.Name,
				Description: e.Description,
			}
			for _, r := range e.Relationships {
				entity.Relationships = append(entity.Relationships, domain.EntityRelationship{
					Type:   r.Type,
					Target: r.Target,
				})
			}
			draft.Entities = append(draft.Entities, entity)
		}

		drafts = append(drafts, draft)
	}

	return drafts, nil
}

// printDomainDiscoverySummary displays the results of domain discovery
func printDomainDiscoverySummary(drafts []*domain.DraftDomainDoc, draftsDir string) {
	// Sort by confidence (high first)
	sort.Slice(drafts, func(i, j int) bool {
		confidenceOrder := map[domain.DraftDomainConfidence]int{
			domain.DraftDomainConfidenceHigh:   0,
			domain.DraftDomainConfidenceMedium: 1,
			domain.DraftDomainConfidenceLow:    2,
		}
		return confidenceOrder[drafts[i].Confidence] < confidenceOrder[drafts[j].Confidence]
	})

	// Count by confidence
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

	// List drafts with details
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
