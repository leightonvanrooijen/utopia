// Package discover implements the multi-stage discovery pipelines behind
// `utopia discover`: scanning the codebase, qualifying candidate capabilities
// with Claude, refining them with parallel agents, and saving the resulting
// draft specifications or draft domain documents.
package discover

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/leightonvanrooijen/utopia/internal"
	"github.com/leightonvanrooijen/utopia/internal/domain"
)

// Default number of refinement iterations per candidate
const defaultRefinementIterations = 3

// Scope holds scope restrictions for discovery
type Scope struct {
	Paths           []string
	ExcludePatterns []string
}

// progress tracks timing and progress for discovery phases
type progress struct {
	phaseStart  time.Time
	totalPhases int
	verbose     bool
}

func newProgress(totalPhases int, verbose bool) *progress {
	return &progress{phaseStart: time.Now(), totalPhases: totalPhases, verbose: verbose}
}

func (p *progress) startPhase(phaseNum int, name string) {
	p.phaseStart = time.Now()
	fmt.Printf("[%d/%d] %s...", phaseNum, p.totalPhases, name)
}

func (p *progress) endPhase(detail string) {
	elapsed := time.Since(p.phaseStart)
	if detail != "" {
		fmt.Printf(" done (%.1fs, %s)\n", elapsed.Seconds(), detail)
	} else {
		fmt.Printf(" done (%.1fs)\n", elapsed.Seconds())
	}
}

func (p *progress) verbosePrintf(format string, args ...interface{}) {
	if p.verbose {
		fmt.Printf(format, args...)
	}
}

// SpecsOptions configures the spec discovery pipeline.
type SpecsOptions struct {
	// ProjectDir is the absolute path of the project to scan
	ProjectDir string
	// Scope restricts which files are scanned
	Scope Scope
	// Verbose enables detailed file-by-file progress output
	Verbose bool
	// Model is an optional Claude model override
	Model string
	// ExistingSpecs are summarized in the prompt so Claude skips documented capabilities
	ExistingSpecs []*domain.Spec
}

// SpecsResult represents the outcome of the spec discovery pipeline.
type SpecsResult struct {
	// FilesAnalyzed lists the files included in the codebase context
	FilesAnalyzed []string
	// Qualified is the number of candidates that passed qualification
	Qualified int
	// Drafts are the refined draft specifications that were saved
	Drafts []*domain.DraftSpec
}

// Specs runs the 2-stage spec discovery pipeline: scan files, identify and
// qualify candidates with Claude, refine each qualified candidate with a
// parallel agent, and save the resulting drafts via the store.
// It returns early (with a nil error) when a stage produces nothing to
// carry forward; callers inspect the result to frame the outcome.
func Specs(ctx context.Context, store *internal.YAMLStore, opts SpecsOptions) (*SpecsResult, error) {
	prog := newProgress(4, opts.Verbose)
	result := &SpecsResult{}

	prog.startPhase(1, "Scanning files")
	codebaseContext, filesAnalyzed, err := collectCodebaseContext(opts.ProjectDir, opts.Scope, prog)
	if err != nil {
		return nil, fmt.Errorf("failed to collect codebase context: %w", err)
	}
	result.FilesAnalyzed = filesAnalyzed
	if len(filesAnalyzed) == 0 {
		fmt.Printf(" done (no files found)\n")
		return result, nil
	}
	prog.endPhase(fmt.Sprintf("%d files found", len(filesAnalyzed)))

	specsSummary := buildExistingSpecsSummary(opts.ExistingSpecs)
	cli := internal.NewCLI().WithVerbose(opts.Verbose)
	if opts.Model != "" {
		cli = cli.WithModel(opts.Model)
	}

	prog.startPhase(2, "Stage 1: Identifying and qualifying candidates")
	stage1Prompt := buildIdentifyQualifyPrompt(codebaseContext, specsSummary)
	qualifiedResult, err := cli.Prompt(ctx, stage1Prompt)
	if err != nil {
		return nil, fmt.Errorf("identify and qualify failed: %w", err)
	}
	qualifiedCount := countYAMLItems(qualifiedResult.Stdout, "qualified")
	disqualifiedCount := countYAMLItems(qualifiedResult.Stdout, "disqualified")
	prog.endPhase(fmt.Sprintf("%d qualified, %d disqualified", qualifiedCount, disqualifiedCount))

	disqualified := parseDisqualifiedCandidates(qualifiedResult.Stdout)
	logDisqualifiedCandidates(disqualified, opts.Verbose)
	qualified := parseQualifiedCandidates(qualifiedResult.Stdout)
	result.Qualified = len(qualified)

	if len(qualified) == 0 {
		return result, nil
	}

	prog.startPhase(3, fmt.Sprintf("Stage 2: Refining %d candidates in parallel", len(qualified)))
	drafts, refinementErrors := runParallelRefinement(ctx, qualified, defaultRefinementIterations, opts.Verbose, opts.Model)
	prog.endPhase(fmt.Sprintf("%d drafts refined", len(drafts)))

	if len(refinementErrors) > 0 && opts.Verbose {
		fmt.Println("\n  Refinement errors:")
		for _, err := range refinementErrors {
			fmt.Printf("    ✗ %v\n", err)
		}
	}

	result.Drafts = drafts
	if len(drafts) == 0 {
		return result, nil
	}

	prog.startPhase(4, "Saving drafts")
	for _, draft := range drafts {
		prog.verbosePrintf("\n  Saving %s.yaml", draft.ID)
		if err := store.SaveDraft(draft); err != nil {
			return nil, fmt.Errorf("failed to save draft %s: %w", draft.ID, err)
		}
	}
	prog.endPhase(fmt.Sprintf("%d drafts saved", len(drafts)))

	return result, nil
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

func runParallelRefinement(ctx context.Context, candidates []qualifiedCandidate, iterations int, verbose bool, modelID string) ([]*domain.DraftSpec, []error) {
	var (
		drafts []*domain.DraftSpec
		errors []error
		mu     sync.Mutex
		wg     sync.WaitGroup
	)

	refinementCLI := internal.NewCLI().WithVerbose(verbose).WithAllowedTools([]string{"Read", "Grep", "Glob"})
	if modelID != "" {
		refinementCLI = refinementCLI.WithModel(modelID)
	}

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
				result, err := refinementCLI.Prompt(ctx, prompt)
				if err != nil {
					lastErr = fmt.Errorf("refinement failed for %s (iteration %d): %w", c.ID, i, err)
					continue
				}
				lastOutput = result.Stdout
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

func logDisqualifiedCandidates(disqualified []disqualifiedCandidate, verbose bool) {
	if len(disqualified) == 0 || !verbose {
		return
	}
	fmt.Println("\n  Disqualified candidates:")
	for _, d := range disqualified {
		fmt.Printf("    ✗ %s: %s\n", d.ID, d.Reason)
	}
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
