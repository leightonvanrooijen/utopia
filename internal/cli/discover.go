package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/leightonvanrooijen/utopia/internal/discover"
	"github.com/leightonvanrooijen/utopia/internal/domain"
	"github.com/spf13/cobra"
)

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
	discoverModelFlag    string
)

func init() {
	rootCmd.AddCommand(discoverCmd)
	discoverCmd.Flags().StringSliceVar(&discoverPathFlags, "path", nil, "Limit discovery to specific directory (can be specified multiple times)")
	discoverCmd.Flags().StringSliceVar(&discoverExcludeFlags, "exclude", nil, "Exclude files matching glob pattern (can be specified multiple times)")
	discoverCmd.Flags().BoolVarP(&discoverVerboseFlag, "verbose", "v", false, "Enable detailed file-by-file progress output")
	discoverCmd.Flags().StringVar(&discoverModelFlag, "model", "", "model to use (haiku, sonnet, opus)")
}

func runDiscover(cmd *cobra.Command, args []string) error {
	// Validate model flag early before any work
	modelID, err := ResolveModelFlag(cmd)
	if err != nil {
		return err
	}

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

	scope := discover.Scope{Paths: discoverPathFlags, ExcludePatterns: discoverExcludeFlags}

	fmt.Println("Starting codebase discovery...")
	fmt.Printf("Project: %s\n", absPath)
	fmt.Printf("Existing specs: %d\n", len(existingSpecs))
	fmt.Printf("Existing drafts: %d\n", len(existingDrafts))
	if len(scope.Paths) > 0 {
		fmt.Printf("Scope: %s\n", strings.Join(scope.Paths, ", "))
	}
	if len(scope.ExcludePatterns) > 0 {
		fmt.Printf("Excluding: %s\n", strings.Join(scope.ExcludePatterns, ", "))
	}
	fmt.Println()

	startTime := time.Now()
	result, err := discover.Specs(context.Background(), store, discover.SpecsOptions{
		ProjectDir:    absPath,
		Scope:         scope,
		Verbose:       discoverVerboseFlag,
		Model:         modelID,
		ExistingSpecs: existingSpecs,
	})
	if err != nil {
		return fmt.Errorf("spec discovery failed: %w", err)
	}

	switch {
	case len(result.FilesAnalyzed) == 0:
		fmt.Println("No files to analyze.")
	case result.Qualified == 0:
		fmt.Println("\nNo candidates passed qualification criteria.")
	case len(result.Drafts) == 0:
		fmt.Println("\nNo draft specifications produced after refinement.")
	default:
		printDiscoverySummary(result.Drafts, draftsDir)
	}
	fmt.Printf("\nTotal elapsed time: %.1fs\n", time.Since(startTime).Seconds())
	return nil
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
	discoverDomainModelFlag    string
)

func init() {
	discoverCmd.AddCommand(discoverDomainCmd)
	discoverDomainCmd.Flags().BoolVar(&discoverDomainFullFlag, "full", false, "Force complete re-discovery of entire codebase")
	discoverDomainCmd.Flags().StringSliceVar(&discoverDomainPathFlags, "path", nil, "Limit discovery to specific directory (can be specified multiple times)")
	discoverDomainCmd.Flags().StringSliceVar(&discoverDomainExcludeFlags, "exclude", nil, "Exclude files matching glob pattern (can be specified multiple times)")
	discoverDomainCmd.Flags().BoolVarP(&discoverDomainVerboseFlag, "verbose", "v", false, "Enable detailed file-by-file progress output")
	discoverDomainCmd.Flags().StringVar(&discoverDomainModelFlag, "model", "", "model to use (haiku, sonnet, opus)")
}

func runDiscoverDomain(cmd *cobra.Command, args []string) error {
	// Validate model flag early before any work
	modelID, err := ResolveModelFlag(cmd)
	if err != nil {
		return err
	}

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

	scope := discover.Scope{Paths: discoverDomainPathFlags, ExcludePatterns: discoverDomainExcludeFlags}

	fmt.Println("Starting domain vocabulary discovery...")
	fmt.Printf("Project: %s\n", absPath)
	fmt.Printf("Existing domain docs: %d\n", len(existingDomainDocs))
	fmt.Printf("Existing draft domain docs: %d\n", len(existingDrafts))
	if isIncremental {
		fmt.Printf("Mode: incremental (since %s)\n", lastRunTime.Format("2006-01-02 15:04:05"))
	} else {
		fmt.Println("Mode: full discovery")
	}
	if len(scope.Paths) > 0 {
		fmt.Printf("Scope: %s\n", strings.Join(scope.Paths, ", "))
	}
	if len(scope.ExcludePatterns) > 0 {
		fmt.Printf("Excluding: %s\n", strings.Join(scope.ExcludePatterns, ", "))
	}
	fmt.Println()

	startTime := time.Now()
	result, err := discover.Domain(context.Background(), store, discover.DomainOptions{
		ProjectDir:   absPath,
		Scope:        scope,
		Verbose:      discoverDomainVerboseFlag,
		Model:        modelID,
		Incremental:  isIncremental,
		LastRun:      lastRunTime,
		ExistingDocs: existingDomainDocs,
	})
	if err != nil {
		return fmt.Errorf("domain discovery failed: %w", err)
	}

	switch {
	case len(result.FilesAnalyzed) == 0:
		fmt.Println("No new or modified files to analyze.")
		fmt.Println("Use --full to force complete re-discovery.")
	case len(result.Drafts) == 0:
		fmt.Println("No new draft domain documents discovered.")
	default:
		printDomainDiscoverySummary(result.Drafts, draftsDir)
	}
	fmt.Printf("\nTotal elapsed time: %.1fs\n", time.Since(startTime).Seconds())
	return nil
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
