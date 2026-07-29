---
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
