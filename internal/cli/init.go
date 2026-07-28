package cli

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/leightonvanrooijen/utopia/internal"
	"github.com/leightonvanrooijen/utopia/internal/cli/ui"
	"github.com/leightonvanrooijen/utopia/internal/domain"
	"github.com/spf13/cobra"
)

// validatorTemplate is a reference template for creating validators.
// The underscore prefix indicates it's not active (must be copied and renamed).
const validatorTemplate = `---
# ============================================================================
# VALIDATOR TEMPLATE
# ============================================================================
# Validators enforce project standards by reviewing changes after verification
# passes. Copy this file (remove the underscore prefix) and customize it.
#
# Example: cp _template.md code-standards.md
# Then add to .utopia/config.yaml:
#   validators:
#     - validators/code-standards.md
# ============================================================================

# Validator ID (required)
# A unique identifier for this validator. Used in feedback messages to help
# identify which validator flagged an issue.
# Examples: "code-standards", "security-review", "api-consistency"
id: my-validator

# Description (optional, recommended)
# A short line stating what this validator checks and when it applies. The
# relevance router reads this - like a Claude skill description - to decide
# whether the validator is worth running for a given change, without loading
# the full prompt below. Leave it empty only if the validator should run on
# every change: an empty description gives the router no signal, so it treats
# the validator as always applicable rather than silently skipping it.
description: Checks [what this validator enforces] when [which files/changes it applies to]

# When to run (after-workitem, after-phase, or on-demand) is configured per
# validator in .utopia/config.yaml, NOT here. A "run" field in this frontmatter
# is deprecated and warns on load.

# Tools the validator can use (optional, default: [Read, Glob, Grep])
# By default, validators are read-only for safety. Available tools:
#
#   Read-only (default): [Read, Glob, Grep]
#   - Read:  Read file contents
#   - Glob:  Find files matching patterns (e.g., "**/*.go")
#   - Grep:  Search for patterns in files
#
#   Write tools (for auto-fixing validators):
#   - Write: Create or overwrite files
#   - Edit:  Make targeted edits to existing files
#   - Bash:  Execute shell commands
#
# Example auto-fixing validator:
#   allowed_tools: [Read, Glob, Grep, Edit]
#
allowed_tools: [Read, Glob, Grep]
---

<!-- =========================================================================
VALIDATOR PROMPT SECTION
============================================================================
Everything below the frontmatter (---) is the prompt sent to Claude.
Write clear instructions for what to check and how to report issues.
========================================================================== -->

Review the following changes for [YOUR STANDARDS HERE]:

<!-- The {{changed_files}} placeholder is replaced with the git diff of changes.
     This shows exactly what code was added, modified, or deleted.
     Format: unified diff with file paths, line numbers, and context. -->
{{changed_files}}

Check for:
- [Standard 1: e.g., All exported functions have documentation]
- [Standard 2: e.g., Error messages include context]
- [Standard 3: e.g., No TODO comments without ticket references]

<!-- =========================================================================
OUTPUT FORMAT (IMPORTANT)
============================================================================
Your validator MUST follow this output format:

SUCCESS: Output ONLY the token <PASSED> with nothing else.
         This signals all standards are met.

FAILURE: List each violation with actionable details.
         Include file path, line number, and specific issue.
         The feedback is injected into the LLM's next attempt.
========================================================================== -->

If ALL standards are met, output ONLY: <PASSED>

Otherwise, list each violation with file and line number:
- file.go:42 - Missing documentation for exported function Foo
- internal/api/handler.go:156 - Error message lacks context: "failed" should include operation name
`

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize a new Utopia project",
	Long: `Initialize a new Utopia project in the current directory.

This is the first step in the Utopia workflow. It creates a .utopia directory with:
  - config.yaml       Project configuration (verification command, strategies)
  - specs/            Living specifications (your system's source of truth)
  - work-items/       Auto-chunked work items for Ralph execution
  - validators/       Custom validators for enforcing project standards

You'll be prompted to configure:
  - Verification command (e.g., "npm test", "go test ./...")
  - Max iterations per work item (or unlimited)

After init, run 'utopia cr' to create your first change request.`,
	RunE: runInit,
}

func init() {
	rootCmd.AddCommand(initCmd)
}

func runInit(cmd *cobra.Command, args []string) error {
	out := ui.NewPrinter(cmd.OutOrStdout(), cmd.ErrOrStderr())
	projectDir, err := ResolveProjectDir(cmd)
	if err != nil {
		return err
	}

	utopiaDir := filepath.Join(projectDir, ".utopia")
	store := internal.NewYAMLStore(utopiaDir)

	// Check if config already exists
	existingConfig, _ := store.LoadConfig()
	isReInit := existingConfig != nil

	// Create directory structure (idempotent)
	dirs := []string{
		utopiaDir,
		store.SpecsDir(),
		filepath.Join(utopiaDir, "work-items"),
		filepath.Join(utopiaDir, "validators"),
	}

	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("failed to create directory %s: %w", dir, err)
		}
	}

	// Create validator template file (idempotent - only if it doesn't exist)
	templatePath := filepath.Join(utopiaDir, "validators", "_template.md")
	if _, err := os.Stat(templatePath); os.IsNotExist(err) {
		if err := os.WriteFile(templatePath, []byte(validatorTemplate), 0644); err != nil {
			return fmt.Errorf("failed to create validator template: %w", err)
		}
	}

	// Start with existing config or defaults
	config := existingConfig
	if config == nil {
		config = domain.DefaultConfig()
	}

	reader := bufio.NewReader(os.Stdin)
	var added, skipped []string

	// Prompt for verification command if missing
	if config.Verification.Command == "" {
		out.Printf("What command verifies your code? (e.g., npm test): ")
		verifyCmd, err := reader.ReadString('\n')
		if err != nil {
			return fmt.Errorf("failed to read verification command: %w", err)
		}
		config.Verification.Command = strings.TrimSpace(verifyCmd)
		added = append(added, "verification.command")
	} else {
		skipped = append(skipped, "verification.command")
	}

	// Prompt for max iterations if missing (0 means unlimited, so only prompt if never set)
	// We use a sentinel approach: if re-init and field was explicitly set (even to 0), skip
	if !isReInit || (isReInit && config.Verification.MaxIterations == 0 && config.Verification.Command == "") {
		// Only prompt on fresh init, not re-init (max_iterations has a valid zero value)
		if !isReInit {
			out.Printf("Max iterations per work item? (leave blank for unlimited): ")
			maxIterStr, err := reader.ReadString('\n')
			if err != nil {
				return fmt.Errorf("failed to read max iterations: %w", err)
			}
			maxIterStr = strings.TrimSpace(maxIterStr)

			if maxIterStr != "" {
				maxIterations, err := strconv.Atoi(maxIterStr)
				if err != nil {
					return fmt.Errorf("invalid max iterations value: %w", err)
				}
				config.Verification.MaxIterations = maxIterations
			}
			added = append(added, "verification.max_iterations")
		} else {
			skipped = append(skipped, "verification.max_iterations")
		}
	} else {
		skipped = append(skipped, "verification.max_iterations")
	}

	// Prompt for project context if missing
	if config.ProjectContext == "" {
		out.Printf("Project context (orient an AI to this project's workflow): ")
		projectContext, err := reader.ReadString('\n')
		if err != nil {
			return fmt.Errorf("failed to read project context: %w", err)
		}
		config.ProjectContext = strings.TrimSpace(projectContext)
		added = append(added, "project_context")
	} else {
		skipped = append(skipped, "project_context")
	}

	if err := store.SaveConfig(config); err != nil {
		return fmt.Errorf("failed to write config: %w", err)
	}

	// Print appropriate output based on fresh init vs re-init
	if isReInit {
		out.Printf("Updated Utopia project at %s\n\n", utopiaDir)
		if len(added) > 0 {
			out.Printf("Added:\n")
			for _, field := range added {
				out.Printf("  %s\n", field)
			}
		}
		if len(skipped) > 0 {
			out.Printf("Skipped (already configured):\n")
			for _, field := range skipped {
				out.Printf("  %s\n", field)
			}
		}
	} else {
		out.Printf("Initialized Utopia project at %s\n\n", utopiaDir)
		out.Printf("Created:\n")
		out.Printf("  .utopia/config.yaml              - Project configuration\n")
		out.Printf("  .utopia/specs/                   - Living specifications\n")
		out.Printf("  .utopia/work-items/              - Work items for Ralph\n")
		out.Printf("  .utopia/validators/_template.md  - Validator template (copy to create validators)\n")
		out.Printf("\nNext steps:\n")
		out.Printf("  utopia cr              - Create a change request\n")
		out.Printf("  utopia status          - View project status\n")
	}

	return nil
}
