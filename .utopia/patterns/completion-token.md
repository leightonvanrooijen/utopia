---
id: completion-token
status: approved
---

# Completion Token Protocol

## Description

Claude signals task completion by outputting `<COMPLETE>`, which triggers verification to run.

## Responsibility

The completion token protocol defines the contract between Utopia and Claude:

- Claude outputs `<COMPLETE>` when it believes the task is done
- Utopia only runs verification after detecting this token
- Without the token, the execution loop retries (assumes Claude hit a limit or got stuck)

## Boundaries

- Verification MUST NOT run without the completion token
- Prompts MUST include the completion instruction
- The token is checked via simple string containment, not parsed

## Naming

- Token constant: `CompletionToken` in `internal/strategies/execute/sequential/strategy.go`
- Prompt instruction: `"When complete, commit your changes and output: <COMPLETE>"`

## Examples

- internal/strategies/execute/sequential/strategy.go (token detection)
- internal/strategies/chunk/ralphsequential/prompt.go (prompt template with instruction)

## Flow

```
Claude returns output
        ↓
Contains <COMPLETE>?
        ↓
   NO → Retry (don't run verification)
        ↓
  YES → Run verification command
        ↓
    Pass? → Commit and continue
    Fail? → Inject failure output, retry
```
