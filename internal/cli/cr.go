package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/leightonvanrooijen/utopia/internal"
	"github.com/leightonvanrooijen/utopia/internal/ui"
	"github.com/leightonvanrooijen/utopia/internal/domain"
	"github.com/leightonvanrooijen/utopia/internal/git"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

var (
	crModelFlag  string
	crEffortFlag string
	crAuthFlag   string
)

var crCmd = &cobra.Command{
	Use:   "cr",
	Short: "Create a change request via guided conversation",
	Long: `Start a conversation with Claude to create a change request.

Change requests define modifications to existing specifications:
  - feature:     Add new capability to an existing spec
  - enhancement: Modify how an existing feature works
  - refactor:    Code improvement without behavior change
  - bugfix:      Correct behavior to match spec
  - removal:     Delete an existing capability
  - initiative:  Multi-phase changes with ordered execution

Claude will guide you through defining the change by:
  1. Understanding what you want to change
  2. Determining the appropriate CR type
  3. Capturing specific changes with acceptance criteria

The resulting change request is saved to .utopia/change-requests/

Tip: Run a file watcher to see updates in real-time:
  watch -n 1 'ls -la .utopia/change-requests/'`,
	RunE: runCR,
}

func init() {
	rootCmd.AddCommand(crCmd)
	crCmd.Flags().StringVar(&crModelFlag, "model", "", "model alias (haiku, sonnet, opus, fable) or a full model identifier")
	crCmd.Flags().StringVar(&crEffortFlag, "effort", "", effortFlagUsage)
	crCmd.Flags().StringVar(&crAuthFlag, "auth", "", "credential to use (api-key, subscription), overriding config.auth.mode")
}

// ============================================================================
// VALIDATE - Validate change request files
// ============================================================================

var crValidateCmd = &cobra.Command{
	Use:   "validate [file]",
	Short: "Validate change request files",
	Long: `Validate change request YAML files for syntax and required fields.

If no file is specified, validates all CRs in .utopia/change-requests/.
If a file path is specified, validates only that single CR file.

Returns exit code 0 on success, non-zero on validation failure.

Examples:
  utopia cr validate                                          # Validate all CRs
  utopia cr validate .utopia/change-requests/my-feature.yaml  # Validate single file`,
	Args: cobra.MaximumNArgs(1),
	RunE: runCRValidate,
}

func init() {
	crCmd.AddCommand(crValidateCmd)
}

func runCRValidate(cmd *cobra.Command, args []string) error {
	out := ui.NewPrinter(cmd.OutOrStdout(), cmd.ErrOrStderr())
	_, utopiaDir, store, err := ResolveProject(cmd)
	if err != nil {
		return err
	}

	if len(args) == 1 {
		// Validate a single file
		return validateSingleCR(out, args[0], utopiaDir)
	}

	// Validate all CRs
	if err := validateChangeRequests(store); err != nil {
		return err
	}

	crs, _ := store.ListChangeRequests()
	out.Successf("All %d change request(s) are valid", len(crs))
	return nil
}

// validateSingleCR validates a single CR file by path.
func validateSingleCR(out *ui.Printer, filePath, utopiaDir string) error {
	// Handle both absolute and relative paths
	absPath := filePath
	if !filepath.IsAbs(filePath) {
		cwd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("failed to get working directory: %w", err)
		}
		absPath = filepath.Join(cwd, filePath)
	}

	// Read and parse the file
	bytes, err := os.ReadFile(absPath)
	if err != nil {
		return fmt.Errorf("failed to read %s: %w", absPath, err)
	}

	var cr domain.ChangeRequest
	if err := yaml.Unmarshal(bytes, &cr); err != nil {
		return fmt.Errorf("invalid YAML syntax in %s: %w", filepath.Base(absPath), err)
	}

	// Validate the CR
	if validationErr := domain.ValidateChangeRequest(&cr); validationErr != nil {
		return fmt.Errorf("%s: %w", filepath.Base(absPath), validationErr)
	}

	out.Successf("%s is valid", filepath.Base(absPath))
	return nil
}

// crSystemPrompt guides Claude through change request creation
// Use fmt.Sprintf to inject: projectContext, specsSummary, existingCRsSummary, changeRequestsDir
const crSystemPrompt = `You are a Change Request Claude - an AI assistant that helps users create structured change requests.

## Project Context
%s

## Your Role
Guide users through a natural conversation to create change requests. CRs can target existing specs OR define new specs (which get created when the CR is merged).

## Existing Specifications
These are the existing specs (CRs can also target new specs not listed here):

%s

## Existing Change Requests
These change requests already exist (avoid duplicates):

%s

## The Journey

### PHASE 1: UNDERSTAND
Start by understanding what the user wants to change:
- What are you trying to modify or improve?
- Which spec does this relate to? (can be existing or a new spec)
- Ask ONE question at a time

### PHASE 2: CLASSIFY
Determine the CR type by asking: "Does this change observable behavior?"

| Intent Signal | CR Type | Key Question |
|--------------|---------|--------------|
| Behavior unchanged (code cleanup, restructure) | refactor | "Will the system behave exactly the same afterward?" |
| New capability that doesn't exist | feature | "Is this something users can't do today?" |
| Modifying how existing capability works | enhancement | "Are we changing how an existing feature behaves?" |
| Behavior doesn't match spec (bug) | bugfix | "Should this already work according to the spec?" |
| Removing existing capability | removal | "Are we deleting something that currently exists?" |
| Multiple ordered changes with dependencies | initiative | "Does this require phased execution?" |

**When intent is ambiguous, ask clarifying questions:**
- "To help me classify this correctly: will users notice any difference in behavior, or is this purely a code improvement?"
- "Is this adding something new, or changing how something existing works?"
- "Does this need to happen in phases, or can all changes be applied together?"

Tell the user your assessment: "This sounds like a [TYPE] change request to [SPEC]. Does that match your understanding?"

### PHASE 3: SPECIFY
Capture the specific changes with testable acceptance criteria:
- For features: What's the new capability? What are the acceptance criteria?
- For enhancements: What existing feature? How should it change?
- For refactors: What code improvement? How do we verify behavior is preserved?
- For bugfixes: What spec/feature is broken? How should it behave per the spec?
- For removals: What's being removed? Why?
- For initiatives: What are the phases? What's the execution order?

**Optional: Implementation Hints**
If the user provides specific technical guidance (file paths, patterns to follow, libraries to use), capture these as hints:
- Hints are ephemeral - they guide implementation but are NOT saved to specs after merge
- Add hints when users mention specific files, functions, patterns, or approaches
- Keep hints concise and actionable
- Only add hints when the user provides explicit implementation guidance

### PHASE 4: SAVE
Write the change request file using the appropriate format below.

### PHASE 5: VALIDATE
After writing the CR file, you MUST validate it:
1. Run: ` + "`utopia cr validate <file-path>`" + ` (e.g., ` + "`utopia cr validate .utopia/change-requests/my-feature.yaml`" + `)
2. If validation fails, fix the errors in the CR file
3. Re-run validation until it passes
4. Do NOT end the session until validation succeeds

## Output Formats

Save to: %s/{cr-id}.yaml

### Feature CR (new capability)
` + "```yaml" + `
id: spec-id-add-feature-name
type: feature
title: Add new capability
status: draft
changes:
  - operation: add
    spec: target-spec-id  # REQUIRED: Which spec to add to (can be new or existing)
    # Include spec_metadata ONLY when targeting a spec that doesn't exist yet:
    spec_metadata:
      title: Human-readable spec title
      description: What this spec is about
    feature:
      id: new-feature-id
      description: What this feature does
      acceptance_criteria:
        - Specific testable condition
      hints:  # Optional: implementation guidance (not persisted to spec)
        - Look at existing-file.go for patterns to follow
        - Use the FooService abstraction
` + "```" + `

### Enhancement CR (modify existing capability)
` + "```yaml" + `
id: spec-id-enhance-feature-name
type: enhancement
title: Enhance existing feature
status: draft
changes:
  - operation: modify
    spec: target-spec-id  # REQUIRED: Which spec to modify
    feature_id: existing-feature-id
    description: Updated description  # Optional
    criteria:
      add: ["New criterion"]
      remove: ["Exact text to remove"]
      edit:
        - old: "Exact old text"
          new: "Replacement text"
` + "```" + `

### Refactor CR (behavior unchanged)
` + "```yaml" + `
id: spec-id-refactor-description
type: refactor
title: Refactor without behavior change
status: draft
tasks:  # Note: tasks, not changes (refactors don't modify specs)
  - id: task-id
    description: What needs to be refactored
    acceptance_criteria:
      - Existing behavior is preserved
      - Code improvement is achieved
    hints:  # Optional: implementation guidance (not persisted to spec)
      - Start with internal/foo/bar.go
      - Follow the pattern in internal/baz/
` + "```" + `

### Bugfix CR (correct behavior to match spec)
` + "```yaml" + `
id: spec-id-fix-feature-name
type: bugfix
title: Fix behavior to match spec
status: draft
tasks:  # Note: tasks, not changes (bugfixes don't modify specs)
  - id: task-id
    spec: target-spec-id  # REQUIRED: Which spec defines correct behavior
    feature_id: feature-to-fix  # REQUIRED: Which feature defines correct behavior
    description: |
      Fix [feature] to match spec [spec-id].
      Current behavior: [what it does wrong]
      Expected behavior: [what the spec says it should do]
    acceptance_criteria:
      - Behavior matches spec [spec-id] feature [feature-id]
      - [Specific testable condition from spec]
    hints:  # Optional: implementation guidance (not persisted to spec)
      - The bug is in handler.go around line 150
      - Check the error handling path
` + "```" + `

### Removal CR (delete capability)
` + "```yaml" + `
id: spec-id-remove-feature-name
type: removal
title: Remove deprecated feature
status: draft
changes:
  - operation: remove
    spec: target-spec-id  # REQUIRED: Which spec to remove from
    feature_id: feature-to-remove
    reason: Why this is being removed
` + "```" + `

### Initiative CR (multi-phase)
` + "```yaml" + `
id: initiative-name
type: initiative
title: Multi-phase change
status: draft
phases:
  - type: refactor  # First phase: prepare
    tasks:
      - id: task-id
        description: Preparation task
        acceptance_criteria:
          - Criterion
        hints:  # Optional: implementation guidance
          - Refactor internal/foo first
  - type: feature  # Second phase: add capability
    changes:
      - operation: add
        spec: target-spec-id  # REQUIRED for non-refactor phases
        feature:
          id: feature-id
          description: Feature description
          acceptance_criteria:
            - Criterion
          hints:  # Optional: implementation guidance
            - Follow the pattern established in phase 1
` + "```" + `

## Capturing Notes
During conversations, users may mention ideas, future improvements, or thoughts that aren't part of the current change request. Save these to .utopia/notes/ as markdown files:
- Use descriptive kebab-case filenames (e.g., .utopia/notes/future-auth-improvements.md)
- Format is intentionally loose - just dump the thought for later
- Create the notes folder if it doesn't exist
- Tell the user when you've saved a note

## Critical Guidelines
- Ask ONE question at a time - keep the conversation focused
- CRs CAN target specs that don't exist yet - new specs are created when the CR is merged, not during CR creation
- For modify/remove operations, text must match EXACTLY (no fuzzy matching)
- Acceptance criteria must be testable (not vague)
- ALWAYS use the Write tool with the path: %s/{cr-id}.yaml
- CR IDs should be kebab-case and descriptive
- ALWAYS validate after saving: run ` + "`utopia cr validate <file>`" + ` and fix any errors before ending the session

## IMPORTANT: Spec Files Are Read-Only During CR Creation
**NEVER write to .utopia/specs/ directly.** This is critical for workflow integrity.

The correct flow is:
1. CR Creation (this session): Define WHAT should change → saves to .utopia/change-requests/
2. Execution (utopia execute): Claude implements CODE changes based on CR
3. Merge (automatic): Spec files are updated automatically when CR completes

If you edit specs directly:
- The merge will fail (trying to apply changes already made)
- The CR workflow loses its source-of-truth property
- Changes become untraceable

Your role is ONLY to create the CR file. Let the execution and merge phases handle the rest.

Start by warmly greeting the user and asking what change they'd like to make.`

func runCR(cmd *cobra.Command, args []string) error {
	out := ui.NewPrinter(cmd.OutOrStdout(), cmd.ErrOrStderr())

	// Validate model and effort flags early before any work
	modelID, err := ResolveModelFlag(cmd)
	if err != nil {
		return err
	}

	effort, err := ResolveEffortFlag(cmd)
	if err != nil {
		return err
	}

	// Resolve and report the credential this invocation runs with, before any
	// work starts
	authMode, err := ResolveAuth(cmd)
	if err != nil {
		return err
	}

	absPath, utopiaDir, store, err := ResolveProject(cmd)
	if err != nil {
		return err
	}

	// Load config to get project context
	config, err := store.LoadConfig()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Warn if project_context is missing
	if config.ProjectContext == "" {
		out.Warnf("Warning: project_context is empty in config.yaml")
		out.Progressf("  Run 'utopia init' to add project context for better CR guidance\n\n")
	}

	// Create change requests directory if it doesn't exist
	changeRequestsDir := filepath.Join(utopiaDir, "change-requests")
	if err := os.MkdirAll(changeRequestsDir, 0755); err != nil {
		return fmt.Errorf("failed to create change requests directory: %w", err)
	}

	// Load existing specs for context
	existingSpecs, err := store.ListSpecs()
	if err != nil {
		// Non-fatal - continue with empty spec list
		existingSpecs = []*domain.Spec{}
	}

	// Load existing change requests to avoid duplicates
	existingCRs, err := store.ListChangeRequests()
	if err != nil {
		// Non-fatal - continue with empty CR list
		existingCRs = []*domain.ChangeRequest{}
	}

	// Build summaries for Claude
	specsSummary := buildSpecsSummary(existingSpecs)
	crsSummary := buildCRsSummary(existingCRs)

	// Prepare project context for injection
	projectContext := config.ProjectContext
	if projectContext == "" {
		projectContext = "(No project context configured)"
	}

	// Inject project context, summaries, and path into the system prompt
	systemPrompt := fmt.Sprintf(crSystemPrompt, projectContext, specsSummary, crsSummary, changeRequestsDir, changeRequestsDir)

	out.Progressf("Starting change request creation session...\n")
	out.Progressf("Found %d existing specs\n", len(existingSpecs))
	out.Progressf("Found %d existing change requests\n\n", len(existingCRs))
	out.Printf("Change requests will be saved to: %s\n\n", changeRequestsDir)

	// Run interactive Claude session with transcript capture
	ctx := context.Background()
	cli := internal.NewCLI().WithAuth(authMode, utopiaDir)
	if modelID != "" {
		cli = cli.WithModel(modelID)
	}
	if effort != "" {
		cli = cli.WithEffort(effort)
	}

	// Get git branch for metadata before session
	branch := git.CurrentBranch(absPath)
	sessionStart := time.Now()

	// Capture transcript - this persists even on Ctrl+C due to defer in SessionWithCapture
	transcript, sessionErr := cli.SessionWithCapture(ctx, systemPrompt)

	out.Progressf("\nSession ended. Validating change requests...\n")

	// Track CRs and commits for conversation metadata
	var crsCreated []domain.CRCommit
	var commits []string

	// Validate all change requests
	crValidationErr := validateChangeRequests(store)
	if crValidationErr != nil {
		out.Progressf("\n%s Change request validation failed:\n%s\n\n", ui.Failure, crValidationErr)
		out.Progressf("Starting Claude session to fix validation errors...\n\n")

		fixPrompt := fmt.Sprintf(crFixSystemPrompt, utopiaDir, crValidationErr)
		fixTranscript, fixErr := cli.SessionWithCapture(ctx, fixPrompt)
		transcript += "\n\n--- Fix Session ---\n\n" + fixTranscript
		if fixErr != nil {
			// Save conversation before returning error
			saveConversation(out, store, sessionStart, branch, transcript, crsCreated, commits)
			return fmt.Errorf("claude fix session failed: %w", fixErr)
		}

		// Re-validate after fix session
		crValidationErr = validateChangeRequests(store)
		if crValidationErr != nil {
			out.Progressf("\n%s Change request validation still failed:\n%s\n", ui.Failure, crValidationErr)
			// Save conversation before returning error
			saveConversation(out, store, sessionStart, branch, transcript, crsCreated, commits)
			return fmt.Errorf("change request validation failed after fix attempt")
		}
	}

	out.Successf("All change requests are valid")

	// Auto-commit valid CRs and track commits
	crs, err := store.ListChangeRequests()
	if err != nil {
		saveConversation(out, store, sessionStart, branch, transcript, crsCreated, commits)
		return fmt.Errorf("failed to list change requests for commit: %w", err)
	}

	for _, cr := range crs {
		sha, commitErr := GitCommitCR(absPath, store, cr.ID)
		if commitErr != nil {
			out.Warnf("Failed to commit CR %s: %v", cr.ID, commitErr)
			continue
		}
		if sha != "" {
			out.Successf("Committed CR: %s (%s)", cr.ID, sha[:8])
			crsCreated = append(crsCreated, domain.CRCommit{CRID: cr.ID, CommitSHA: sha})
			commits = append(commits, sha)
		}
	}

	// Save the conversation transcript with metadata (skips empty conversations)
	convID := saveConversation(out, store, sessionStart, branch, transcript, crsCreated, commits)
	if convID != "" {
		out.Successf("Conversation saved: %s", convID)
	}

	// Return session error if there was one (transcript still saved above)
	if sessionErr != nil {
		return fmt.Errorf("claude session failed: %w", sessionErr)
	}

	return nil
}

// buildCRsSummary creates a readable summary of existing change requests for Claude
func buildCRsSummary(crs []*domain.ChangeRequest) string {
	if len(crs) == 0 {
		return "(No existing change requests found)"
	}

	var sb strings.Builder
	for _, cr := range crs {
		sb.WriteString(fmt.Sprintf("### %s\n", cr.ID))
		sb.WriteString(fmt.Sprintf("**Type:** %s\n", cr.Type))
		sb.WriteString(fmt.Sprintf("**Title:** %s\n", cr.Title))
		sb.WriteString(fmt.Sprintf("**Status:** %s\n", cr.Status))

		// Show target specs for non-refactor types
		if cr.Type != domain.CRTypeRefactor && cr.Type != domain.CRTypeInitiative {
			targetSpecs := make(map[string]bool)
			for _, change := range cr.Changes {
				if change.Spec != "" {
					targetSpecs[change.Spec] = true
				}
			}
			if len(targetSpecs) > 0 {
				specs := make([]string, 0, len(targetSpecs))
				for spec := range targetSpecs {
					specs = append(specs, spec)
				}
				sb.WriteString(fmt.Sprintf("**Target Specs:** %s\n", strings.Join(specs, ", ")))
			}
		}
		sb.WriteString("\n")
	}

	return sb.String()
}

// buildSpecsSummary creates a readable summary of existing specs for Claude
func buildSpecsSummary(specs []*domain.Spec) string {
	if len(specs) == 0 {
		return "(No existing specs found)"
	}

	var sb strings.Builder
	for _, spec := range specs {
		sb.WriteString(fmt.Sprintf("### %s\n", spec.ID))
		sb.WriteString(fmt.Sprintf("**Title:** %s\n", spec.Title))

		// Truncate description if too long
		desc := strings.TrimSpace(spec.Description)
		if len(desc) > 200 {
			desc = desc[:200] + "..."
		}
		sb.WriteString(fmt.Sprintf("**Description:** %s\n", desc))

		// List feature IDs
		if len(spec.Features) > 0 {
			sb.WriteString("**Features:** ")
			featureIDs := make([]string, len(spec.Features))
			for i, f := range spec.Features {
				featureIDs[i] = f.ID
			}
			sb.WriteString(strings.Join(featureIDs, ", "))
			sb.WriteString("\n")
		}
		sb.WriteString("\n")
	}

	return sb.String()
}

// validateChangeRequests validates all change request files for YAML syntax and required fields.
// Returns nil if all CRs are valid, or an error describing validation failures.
func validateChangeRequests(store *internal.YAMLStore) error {
	crs, err := store.ListChangeRequests()
	if err != nil {
		// ListChangeRequests returns parse errors for invalid YAML
		return fmt.Errorf("failed to list change requests: %w", err)
	}

	// Validate each CR has required fields based on type
	var allErrors []string
	for _, cr := range crs {
		if validationErr := domain.ValidateChangeRequest(cr); validationErr != nil {
			allErrors = append(allErrors, fmt.Sprintf("%s: %v", cr.ID, validationErr))
		}
	}

	if len(allErrors) > 0 {
		return fmt.Errorf("change request validation errors:\n%s", strings.Join(allErrors, "\n"))
	}

	return nil
}

// crFixSystemPrompt is used when change requests fail validation
const crFixSystemPrompt = `You are a CR Fix Claude - an AI assistant that helps fix Change Request YAML validation errors.

## Your Task
Fix the validation errors in the change request files. The CRs are located in: %s/change-requests/

## Validation Error
%s

## CR Structure Requirements by Type

### feature/enhancement/removal types require:
- id: unique identifier (kebab-case)
- type: "feature", "enhancement", or "removal"
- title: human-readable title
- status: "draft", "in-progress", or "complete"
- changes: array of changes (REQUIRED, cannot be empty)
  - Each change MUST have a "spec" field specifying the target spec ID
  - Each change has an "operation" field: "add", "modify", or "remove"

### refactor type requires:
- id: unique identifier (kebab-case)
- type: "refactor"
- title: human-readable title
- status: "draft", "in-progress", or "complete"
- tasks: array of tasks (REQUIRED, cannot be empty)
  - Each task needs: id, description, acceptance_criteria
  - Tasks do NOT have a "spec" field (refactors don't modify specs)

### bugfix type requires:
- id: unique identifier (kebab-case)
- type: "bugfix"
- title: human-readable title
- status: "draft", "in-progress", or "complete"
- tasks: array of tasks (REQUIRED, cannot be empty)
  - Each task needs: id, description, acceptance_criteria, spec, feature_id
  - spec: target spec that defines correct behavior (REQUIRED)
  - feature_id: feature that defines correct behavior (REQUIRED)
  - Tasks reference spec/feature but don't modify the spec

### initiative type requires:
- id: unique identifier (kebab-case)
- type: "initiative"
- title: human-readable title
- status: "draft", "in-progress", or "complete"
- phases: array of phases (REQUIRED, cannot be empty)
  - Each phase has a "type" field (feature, enhancement, removal, refactor, or bugfix)
  - Refactor and bugfix phases use "tasks" array
  - Other phase types use "changes" array (each change needs "spec" field)

## Guidelines
- Read the problematic file mentioned in the error
- Fix validation issues (missing fields, wrong structure for type)
- Fix YAML syntax issues (unquoted colons/braces, indentation)
- Save the fixed file

Start by reading the file mentioned in the error and fixing it.`

// saveConversation persists a conversation transcript with metadata
// Returns the conversation ID, or empty string if conversation has no meaningful content
func saveConversation(out *ui.Printer, store *internal.YAMLStore, sessionStart time.Time, branch, transcript string, crsCreated []domain.CRCommit, commits []string) string {
	// Skip persisting empty conversations (no transcript, no CRs, no commits)
	if strings.TrimSpace(transcript) == "" && len(crsCreated) == 0 && len(commits) == 0 {
		return ""
	}

	// Generate ID from timestamp: cr-session-YYYYMMDD-HHMMSS
	convID := fmt.Sprintf("cr-session-%s", sessionStart.Format("20060102-150405"))

	// Conversations with CRs start as pending-execution (wait for execution before harvest)
	// Conversations without CRs are immediately harvestable as unprocessed
	status := domain.ConversationUnprocessed
	if len(crsCreated) > 0 {
		status = domain.ConversationPendingExecution
	}

	conv := &domain.Conversation{
		ID:         convID,
		Timestamp:  sessionStart,
		Branch:     branch,
		Status:     status,
		CRsCreated: crsCreated,
		Commits:    commits,
		Transcript: transcript,
	}

	if err := store.SaveConversation(conv); err != nil {
		out.Warnf("Failed to save conversation: %v", err)
		return ""
	}

	return convID
}
