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

SUCCESS: List nothing, and emit a <VERDICT> block with verdict "pass".
         The bare token <PASSED> is still accepted for compatibility.

FAILURE: List each violation with actionable details - file path, line
         number, and specific issue. The list is injected verbatim into the
         next attempt, so it is the feedback the executor acts on.
         Then emit a <VERDICT> block with verdict "fail", which is what
         Utopia routes on: a mechanical failure is retried on the same
         model, a comprehension failure escalates to a stronger one.

Emit exactly ONE <VERDICT> block. Two blocks, a malformed block, or no
block at all is treated as a comprehension failure, because a validator
that cannot state its verdict is not evidence the work was merely sloppy.
========================================================================== -->

If ALL standards are met, list nothing and output:

<VERDICT>
{"verdict": "pass"}
</VERDICT>

Otherwise, list each violation with file and line number:
- file.go:42 - Missing documentation for exported function Foo
- internal/api/handler.go:156 - Error message lacks context: "failed" should include operation name

Then classify the failure:

<VERDICT>
{
  "verdict": "fail",
  "failure_class": "mechanical",
  "diagnosis": "Two exported functions ship without doc comments; the standard is followed everywhere else in the package.",
  "confidence": "high"
}
</VERDICT>

<!-- =========================================================================
THE VERDICT FIELDS
============================================================================
verdict (required)
  "pass" or "fail". Nothing else is valid.

failure_class (required on fail, omit on pass)
  "mechanical" or "comprehension". The distinction is about INTENT, not
  severity or effort:

  - mechanical:    the intent was RIGHT and the execution slipped. The code
                   was trying to do the correct thing and got a detail
                   wrong - a missing doc comment, an unwrapped error, a
                   wrong signature. Retrying the same executor can fix it.

  - comprehension: the intent was WRONG. The work item or standard was
                   misread, so the code is doing the wrong thing correctly.
                   Trying harder cannot fix it, because the executor would
                   re-derive the same misreading.

  Ask: "if I told the author to try again with no extra explanation, would
  they get it right?" Yes means mechanical. No means comprehension.

diagnosis (required on fail)
  Why the check failed, in a sentence or two. Max 400 characters; longer
  text is truncated. This is the reason for the CLASS, not a repeat of the
  violation list above.

corrected_intent (required when failure_class is "comprehension", omit
otherwise)
  What the executor should have understood the work item to mean, as a
  short paragraph. Max 1500 characters. On a comprehension failure this is
  the only thing that breaks the loop, so state the correct intent plainly
  rather than describing the mistake. It is dropped on mechanical failures,
  where the intent was already right.

confidence (required on fail)
  "high" or "low".
  - high: the failure_class above is trusted as reported.
  - low:  you could not tell the two classes apart. A low-confidence
          failure is resolved as comprehension whatever class you reported.

  Report "low" rather than guessing. Guessing mechanical when the intent
  was actually wrong sends the same executor back to re-derive the same
  misreading; being wrong the other way costs one iteration on a stronger
  model. When the distinction is unclear, "low" is the correct answer, not
  a hedge.

spec_defect_suspected (optional, default false)
  Set true when the SPECIFICATION itself looks wrong rather than the
  execution - the code faithfully implements what was asked, and what was
  asked is the problem. This escalates the scoping instead of the executor.

Example of a comprehension failure:

<VERDICT>
{
  "verdict": "fail",
  "failure_class": "comprehension",
  "diagnosis": "Validation was added to the HTTP handler; the standard puts it in the domain type so every caller is covered.",
  "corrected_intent": "Enforce the constraint inside the domain constructor so it holds for every entry point, and let the handler surface the typed error the constructor returns.",
  "confidence": "high"
}
</VERDICT>

Example of a failure you cannot classify:

<VERDICT>
{
  "verdict": "fail",
  "failure_class": "comprehension",
  "diagnosis": "The retry budget is unbounded. Cannot tell whether the cap was overlooked or the requirement was read as best-effort.",
  "corrected_intent": "Bound the retry loop with the configured max_iterations; exhausting the budget must surface as a failure, not a silent give-up.",
  "confidence": "low"
}
</VERDICT>
========================================================================== -->
