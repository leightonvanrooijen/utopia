package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/leightonvanrooijen/utopia/internal/cli/ui"
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
	out := ui.NewPrinter(cmd.OutOrStdout(), cmd.ErrOrStderr())

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

	out.Progressf("Starting codebase discovery...\n")
	out.Progressf("Project: %s\n", absPath)
	out.Progressf("Existing specs: %d\n", len(existingSpecs))
	out.Progressf("Existing drafts: %d\n", len(existingDrafts))
	if len(scope.Paths) > 0 {
		out.Progressf("Scope: %s\n", strings.Join(scope.Paths, ", "))
	}
	if len(scope.ExcludePatterns) > 0 {
		out.Progressf("Excluding: %s\n", strings.Join(scope.ExcludePatterns, ", "))
	}
	out.Progressf("\n")

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
		out.Printf("No files to analyze.\n")
	case result.Qualified == 0:
		out.Printf("\nNo candidates passed qualification criteria.\n")
	case len(result.Drafts) == 0:
		out.Printf("\nNo draft specifications produced after refinement.\n")
	default:
		printDiscoverySummary(out, result.Drafts, draftsDir)
	}
	out.Progressf("\nTotal elapsed time: %.1fs\n", time.Since(startTime).Seconds())
	return nil
}

func printDiscoverySummary(out *ui.Printer, drafts []*domain.DraftSpec, draftsDir string) {
	items := make([]ui.SummaryItem, 0, len(drafts))
	for _, d := range drafts {
		details := []string{
			fmt.Sprintf("ID: %s", d.ID),
			fmt.Sprintf("Features: %d", len(d.Features)),
		}
		if d.HasTests() {
			details = append(details, fmt.Sprintf("Tests: %d files", len(d.Evidence.TestFiles)))
		}
		if d.HasDocs() {
			details = append(details, fmt.Sprintf("Docs: %d files", len(d.Evidence.DocFiles)))
		}
		items = append(items, ui.SummaryItem{
			Confidence:    string(d.Confidence),
			Title:         d.Title,
			Details:       details,
			Uncertainties: d.UncertaintyNotes,
		})
	}
	out.Summary(ui.Summary{
		BannerTitle:  "DISCOVERY COMPLETE",
		CreatedNoun:  "draft specifications",
		SectionTitle: "Draft Specifications:",
		DraftsDir:    draftsDir,
		Items:        items,
		NextSteps: []string{
			fmt.Sprintf("1. Review drafts in %s", draftsDir),
			"2. Run 'utopia shape' to validate and refine drafts",
			"3. Promote validated drafts to specifications",
		},
	})
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
	out := ui.NewPrinter(cmd.OutOrStdout(), cmd.ErrOrStderr())

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

	out.Progressf("Starting domain vocabulary discovery...\n")
	out.Progressf("Project: %s\n", absPath)
	out.Progressf("Existing domain docs: %d\n", len(existingDomainDocs))
	out.Progressf("Existing draft domain docs: %d\n", len(existingDrafts))
	if isIncremental {
		out.Progressf("Mode: incremental (since %s)\n", lastRunTime.Format("2006-01-02 15:04:05"))
	} else {
		out.Progressf("Mode: full discovery\n")
	}
	if len(scope.Paths) > 0 {
		out.Progressf("Scope: %s\n", strings.Join(scope.Paths, ", "))
	}
	if len(scope.ExcludePatterns) > 0 {
		out.Progressf("Excluding: %s\n", strings.Join(scope.ExcludePatterns, ", "))
	}
	out.Progressf("\n")

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
		out.Printf("No new or modified files to analyze.\n")
		out.Printf("Use --full to force complete re-discovery.\n")
	case len(result.Drafts) == 0:
		out.Printf("No new draft domain documents discovered.\n")
	default:
		printDomainDiscoverySummary(out, result.Drafts, draftsDir)
	}
	out.Progressf("\nTotal elapsed time: %.1fs\n", time.Since(startTime).Seconds())
	return nil
}

func printDomainDiscoverySummary(out *ui.Printer, drafts []*domain.DraftDomainDoc, draftsDir string) {
	items := make([]ui.SummaryItem, 0, len(drafts))
	for _, d := range drafts {
		details := []string{
			fmt.Sprintf("Bounded Context: %s", d.BoundedContext),
			fmt.Sprintf("Terms: %d", len(d.Terms)),
			fmt.Sprintf("Entities: %d", len(d.Entities)),
		}
		if d.HasTypeDefinitions() {
			details = append(details, fmt.Sprintf("Type files: %d", len(d.Evidence.TypeFiles)))
		}
		if d.HasSchemas() {
			details = append(details, fmt.Sprintf("Schema files: %d", len(d.Evidence.SchemaFiles)))
		}
		items = append(items, ui.SummaryItem{
			Confidence:    string(d.Confidence),
			Title:         d.Title,
			Details:       details,
			Uncertainties: d.UncertaintyNotes,
		})
	}
	out.Summary(ui.Summary{
		BannerTitle:  "DOMAIN DISCOVERY COMPLETE",
		CreatedNoun:  "draft domain documents",
		SectionTitle: "Draft Domain Documents:",
		DraftsDir:    draftsDir,
		Items:        items,
		NextSteps: []string{
			fmt.Sprintf("1. Review drafts in %s", draftsDir),
			"2. Validate terminology with domain experts",
			"3. Promote validated drafts to official domain docs",
		},
	})
}
