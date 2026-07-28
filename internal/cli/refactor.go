package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/leightonvanrooijen/utopia/internal"
	"github.com/leightonvanrooijen/utopia/internal/cli/ui"
	"github.com/leightonvanrooijen/utopia/internal/domain"
	"github.com/leightonvanrooijen/utopia/internal/git"
	"github.com/spf13/cobra"
)

var (
	refactorModelFlag string
	refactorAuthFlag  string
)

var refactorCmd = &cobra.Command{
	Use:   "refactor",
	Short: "Create a refactor change request via guided conversation",
	Long: `Start a conversation with Claude to create a refactor change request.

Refactor CRs are for code improvements that preserve existing behavior:
  - Code cleanup and reorganization
  - Performance optimization (same behavior, faster)
  - Improving code readability or maintainability
  - Extracting shared logic or reducing duplication
  - Renaming for clarity

The key principle: the system behaves exactly the same afterward.

Claude will guide you through:
  1. Understanding what you want to refactor
  2. Defining the scope and boundaries
  3. Capturing testable acceptance criteria (behavior preservation)

The resulting change request is saved to .utopia/change-requests/

Tip: Run a file watcher to see updates in real-time:
  watch -n 1 'ls -la .utopia/change-requests/'`,
	RunE: runRefactor,
}

func init() {
	rootCmd.AddCommand(refactorCmd)
	refactorCmd.Flags().StringVar(&refactorModelFlag, "model", "", "model to use (haiku, sonnet, opus)")
	refactorCmd.Flags().StringVar(&refactorAuthFlag, "auth", "", "credential to use (api-key, subscription), overriding config.auth.mode")
}

// refactorSystemPrompt guides Claude through refactor CR creation
// Use fmt.Sprintf to inject: changeRequestsDir (twice)
const refactorSystemPrompt = `You are a Refactor Claude - an AI assistant that helps users create refactor change requests.

## Your Role
Guide users through creating refactor CRs. Refactors are code improvements that preserve existing behavior - the system works exactly the same afterward, but the code is better.

## The Key Principle
**Behavior Preservation**: A refactor must not change what the system does, only how it does it. If a user describes changes that would alter behavior, help them distinguish:
- Pure refactor: same inputs → same outputs, same side effects
- Feature/enhancement: different behavior (needs a different CR type - suggest they use ` + "`utopia cr`" + ` instead)

## The Journey

### PHASE 1: UNDERSTAND
Start by understanding what the user wants to refactor:
- What code do you want to improve?
- What's the problem with the current code? (duplication, complexity, naming, structure)
- Ask ONE question at a time

### PHASE 2: SCOPE
Define the boundaries of the refactor:
- Which files/modules are affected?
- What code will NOT change?
- How do we know when we're done?

### PHASE 3: VERIFY BEHAVIOR PRESERVATION
The critical question: "Will the system behave exactly the same afterward?"
- If yes → proceed with refactor CR
- If no → this might be a feature/enhancement, suggest ` + "`utopia cr`" + ` instead

Help the user identify how to verify behavior is preserved:
- Existing tests that should still pass
- Manual verification steps
- Observable behaviors that must remain unchanged

### PHASE 4: SPECIFY
Capture the refactor with testable acceptance criteria:
- What improvement will be achieved?
- How do we verify behavior is preserved?
- What are the specific tasks?

**Optional: Implementation Hints**
If the user provides specific technical guidance (file paths, patterns to follow), capture these as hints:
- Hints are ephemeral - they guide implementation but are NOT saved to specs after merge
- Keep hints concise and actionable

### PHASE 5: SAVE
Write the refactor CR using the format below.

### PHASE 6: VALIDATE
After writing the CR file, you MUST validate it:
1. Run: ` + "`utopia cr validate <file-path>`" + ` (e.g., ` + "`utopia cr validate .utopia/change-requests/my-refactor.yaml`" + `)
2. If validation fails, fix the errors in the CR file
3. Re-run validation until it passes
4. Do NOT end the session until validation succeeds

## Output Format

Save to: %s/{cr-id}.yaml

### Refactor CR Structure
` + "```yaml" + `
id: descriptive-refactor-name
type: refactor
title: Refactor description
status: draft
tasks:
  - id: task-id
    description: What needs to be refactored
    acceptance_criteria:
      - Existing behavior is preserved (tests pass)
      - Code improvement is achieved (specific improvement)
    hints:  # Optional: implementation guidance
      - Start with internal/foo/bar.go
      - Follow the pattern in internal/baz/
` + "```" + `

### Example Refactor CRs

**Extract shared logic:**
` + "```yaml" + `
id: extract-validation-utils
type: refactor
title: Extract shared validation logic into utils package
status: draft
tasks:
  - id: extract-validation
    description: Extract duplicated validation logic from handlers into a shared utils package
    acceptance_criteria:
      - All existing tests pass unchanged
      - Validation behavior identical (same inputs produce same errors)
      - Duplicated code replaced with calls to shared utils
    hints:
      - Look at internal/api/handlers/*.go for validation patterns
      - Create internal/utils/validation.go
` + "```" + `

**Improve naming:**
` + "```yaml" + `
id: rename-user-service-methods
type: refactor
title: Rename UserService methods for clarity
status: draft
tasks:
  - id: rename-methods
    description: Rename ambiguous methods in UserService to be more descriptive
    acceptance_criteria:
      - All existing tests pass
      - API behavior unchanged
      - Method names clearly describe their purpose
    hints:
      - GetUser → GetUserByID
      - Find → FindUsersByEmail
` + "```" + `

## Critical Guidelines
- Ask ONE question at a time - keep the conversation focused
- Always verify: "Will behavior stay the same?"
- Acceptance criteria MUST include behavior preservation verification
- ALWAYS use the Write tool with the path: %s/{cr-id}.yaml
- CR IDs should be kebab-case and descriptive
- ALWAYS validate after saving: run ` + "`utopia cr validate <file>`" + ` and fix any errors before ending

## Scope Boundaries
This command is ONLY for refactors (behavior-preserving code improvements).
If the user wants to:
- Add new features → suggest ` + "`utopia cr`" + `
- Change how something works → suggest ` + "`utopia cr`" + `
- Fix a bug → suggest ` + "`utopia cr`" + `
- Remove functionality → suggest ` + "`utopia cr`" + `

Start by warmly greeting the user and asking what code they'd like to refactor.`

func runRefactor(cmd *cobra.Command, args []string) error {
	out := ui.NewPrinter(cmd.OutOrStdout(), cmd.ErrOrStderr())

	// Validate model flag early before any work
	modelID, err := ResolveModelFlag(cmd)
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

	// Create change requests directory if it doesn't exist
	changeRequestsDir := filepath.Join(utopiaDir, "change-requests")
	if err := os.MkdirAll(changeRequestsDir, 0755); err != nil {
		return fmt.Errorf("failed to create change requests directory: %w", err)
	}

	// Build the system prompt with the change requests directory path
	systemPrompt := fmt.Sprintf(refactorSystemPrompt, changeRequestsDir, changeRequestsDir)

	out.Progressf("Starting refactor change request session...\n\n")
	out.Printf("Change requests will be saved to: %s\n\n", changeRequestsDir)

	// Run interactive Claude session with transcript capture
	ctx := context.Background()
	cli := internal.NewCLI().WithAuth(authMode, utopiaDir)
	if modelID != "" {
		cli = cli.WithModel(modelID)
	}

	// Get git branch for metadata before session
	branch := git.CurrentBranch(absPath)
	sessionStart := time.Now()

	// Capture transcript - persists even on Ctrl+C due to defer in SessionWithCapture
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
			saveRefactorConversation(out, store, sessionStart, branch, transcript, crsCreated, commits)
			return fmt.Errorf("claude fix session failed: %w", fixErr)
		}

		// Re-validate after fix session
		crValidationErr = validateChangeRequests(store)
		if crValidationErr != nil {
			out.Progressf("\n%s Change request validation still failed:\n%s\n", ui.Failure, crValidationErr)
			// Save conversation before returning error
			saveRefactorConversation(out, store, sessionStart, branch, transcript, crsCreated, commits)
			return fmt.Errorf("change request validation failed after fix attempt")
		}
	}

	out.Successf("All change requests are valid")

	// Auto-commit valid CRs and track commits
	crs, err := store.ListChangeRequests()
	if err != nil {
		saveRefactorConversation(out, store, sessionStart, branch, transcript, crsCreated, commits)
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
	convID := saveRefactorConversation(out, store, sessionStart, branch, transcript, crsCreated, commits)
	if convID != "" {
		out.Successf("Conversation saved: %s", convID)
	}

	// Return session error if there was one (transcript still saved above)
	if sessionErr != nil {
		return fmt.Errorf("claude session failed: %w", sessionErr)
	}

	return nil
}

// saveRefactorConversation persists a refactor conversation transcript with metadata
// Returns the conversation ID, or empty string if conversation has no meaningful content
func saveRefactorConversation(out *ui.Printer, store *internal.YAMLStore, sessionStart time.Time, branch, transcript string, crsCreated []domain.CRCommit, commits []string) string {
	// Skip persisting empty conversations (no transcript, no CRs, no commits)
	if transcript == "" && len(crsCreated) == 0 && len(commits) == 0 {
		return ""
	}

	// Generate ID from timestamp: refactor-session-YYYYMMDD-HHMMSS
	convID := fmt.Sprintf("refactor-session-%s", sessionStart.Format("20060102-150405"))

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
