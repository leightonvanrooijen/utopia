package discover

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/leightonvanrooijen/utopia/internal"
	"github.com/leightonvanrooijen/utopia/internal/domain"
	"github.com/leightonvanrooijen/utopia/internal/layout"
	"github.com/leightonvanrooijen/utopia/internal/ui"
)

const domainSystemPrompt = `You are a Domain Discovery Claude - an AI assistant that analyzes codebases to identify domain vocabulary, bounded contexts, and canonical terminology.

## Your Role
Analyze the provided codebase context and identify bounded contexts with their domain vocabulary.

**IMPORTANT**: Create a SEPARATE draft domain document for EACH bounded context identified.

## Codebase Context
%s

## Existing Domain Documents
%s

%s
A candidate that is a spec-local constraint or assumption, rather than a term, definition,
or entity relationship, does not belong in a Domain document - it belongs in that spec's
domain_knowledge instead. Do NOT silently drop this candidate: leave it out of every draft's
terms/entities, and instead record it under top-level excluded_candidates with the spec you
believe it belongs to, so it can be routed there via a change request.

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
excluded_candidates:
  - name: CandidateName
    description: "Why this looked domain-shaped"
    likely_spec: spec-id-it-likely-belongs-to
` + "```" + `

Now analyze the codebase and generate draft domain documents.`

// DomainOptions configures the domain vocabulary discovery pipeline.
type DomainOptions struct {
	// ProjectDir is the absolute path of the project to scan
	ProjectDir string
	// Scope restricts which files are scanned
	Scope Scope
	// Model is an optional Claude model override
	Model string
	// Effort is an optional reasoning effort override for the analysis. The empty
	// string leaves the claude CLI on its own default.
	Effort string
	// Auth selects the credential the analysis authenticates with. The empty
	// mode inherits the ambient environment.
	Auth domain.AuthMode
	// Incremental restricts scanning to files modified after LastRun
	Incremental bool
	// LastRun is the previous discovery run time (used when Incremental is true)
	LastRun time.Time
	// ExistingDocs are summarized in the prompt so Claude builds on prior discovery
	ExistingDocs []*domain.DomainDoc
	// Out receives the pipeline's user-facing terminal output. The CLI hands in
	// the printer it built over cobra's writers so a discovery run is capturable;
	// nil falls back to the process's own streams.
	Out *ui.Printer
}

// DomainResult represents the outcome of the domain discovery pipeline.
type DomainResult struct {
	// FilesAnalyzed maps each analyzed file to its modification time
	FilesAnalyzed map[string]time.Time
	// Drafts are the draft domain documents that were saved
	Drafts []*domain.DraftDomainDoc
	// ExcludedCandidates are domain-shaped candidates left out of every draft
	// because they are spec-local implementation invariants (per
	// domain.DomainKnowledgeBoundary), each tagged with the spec they likely
	// belong to so they can be routed there instead of silently dropped.
	ExcludedCandidates []*domain.ExcludedInvariantCandidate
}

// Domain runs the domain vocabulary discovery pipeline: scan type definitions
// and schemas, analyze them with Claude, parse the draft domain documents,
// save them via the store, and record the discovery state for incremental
// runs. It returns early (with a nil error) when a stage produces nothing to
// carry forward; callers inspect the result to frame the outcome.
func Domain(ctx context.Context, store *internal.YAMLStore, opts DomainOptions) (*DomainResult, error) {
	out := ui.OrDefault(opts.Out)
	prog := newProgress(out, 4)

	prog.StartPhase(1, "Scanning files")
	codebaseContext, filesAnalyzed, err := collectDomainContextIncremental(opts.ProjectDir, opts.LastRun, opts.Incremental, opts.Scope, prog)
	if err != nil {
		return nil, fmt.Errorf("failed to collect codebase context: %w", err)
	}
	result := &DomainResult{FilesAnalyzed: filesAnalyzed}
	if len(filesAnalyzed) == 0 {
		// See Specs: an early return completes its phase line through the renderer,
		// so the tail cannot outlive the level that suppressed the line.
		prog.EndPhase("no new files")
		return result, nil
	}
	prog.EndPhase(fmt.Sprintf("%d files found", len(filesAnalyzed)))

	domainDocsSummary := buildExistingDomainDocsSummary(opts.ExistingDocs)
	systemPrompt := fmt.Sprintf(domainSystemPrompt, codebaseContext, domainDocsSummary, domain.DomainKnowledgeBoundary{}.FormatForAgent())

	prog.StartPhase(2, "Analyzing codebase with Claude")
	cli := internal.NewCLI().WithAuth(opts.Auth, layout.Root(opts.ProjectDir)).WithPrinter(out)
	if opts.Model != "" {
		cli = cli.WithModel(opts.Model)
	}
	if opts.Effort != "" {
		cli = cli.WithEffort(opts.Effort)
	}
	promptResult, err := cli.Prompt(ctx, systemPrompt)
	if err != nil {
		return nil, fmt.Errorf("claude analysis failed: %w", err)
	}
	prog.EndPhase("")

	prog.StartPhase(3, "Parsing draft domain documents")
	drafts, err := parseDomainDraftsFromOutput(promptResult.Stdout)
	if err != nil {
		return nil, fmt.Errorf("failed to parse drafts: %w", err)
	}
	result.Drafts = drafts

	excluded := parseDomainExcludedCandidates(promptResult.Stdout)
	result.ExcludedCandidates = convertExcludedCandidates(excluded)
	for _, c := range result.ExcludedCandidates {
		out.Progressf("  "+ui.Bullet+" excluded %q as a spec-local invariant - likely belongs to spec %q: %s\n", c.Name, c.LikelySpec, c.Description)
	}

	if len(drafts) == 0 {
		prog.EndPhase("no drafts found")
		return result, nil
	}
	prog.EndPhase(fmt.Sprintf("%d drafts parsed", len(drafts)))

	prog.StartPhase(4, "Saving drafts")
	for _, draft := range drafts {
		prog.Verbosef("\n  Saving %s.yaml", draft.ID)
		if err := store.SaveDraftDomainDoc(draft); err != nil {
			return nil, fmt.Errorf("failed to save draft %s: %w", draft.ID, err)
		}
	}
	prog.EndPhase(fmt.Sprintf("%d drafts saved", len(drafts)))

	newState := &domain.DomainDiscoveryState{LastRun: time.Now(), FilesAnalyzed: filesAnalyzed}
	if len(opts.Scope.Paths) > 0 || len(opts.Scope.ExcludePatterns) > 0 {
		newState.Scope = &domain.DiscoveryScope{Paths: opts.Scope.Paths, ExcludePatterns: opts.Scope.ExcludePatterns}
	}
	if err := store.SaveDomainDiscoveryState(newState); err != nil {
		return nil, fmt.Errorf("failed to save domain discovery state: %w", err)
	}

	return result, nil
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

func titleCase(s string) string {
	if len(s) == 0 {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}
