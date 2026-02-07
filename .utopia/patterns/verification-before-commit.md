---
id: verification-before-commit
status: approved
---

# Verification Before Commit

## Description

Git commits only happen after verification passes, ensuring git history contains only verified-passing states.

## Responsibility

This pattern ensures:

- Each commit represents a "known good" checkpoint
- Rolling back to any commit is safe
- Failed attempts never pollute git history

## Boundaries

- MUST NOT commit before verification runs
- MUST NOT commit if verification fails
- MUST inject failure output and retry on verification failure

## Naming

- Verification runner: `internal/verification/runner.go`
- Commit function: `gitCommitWorkItem()` in `internal/strategies/execute/sequential/strategy.go`

## Examples

- internal/strategies/execute/sequential/strategy.go

## Flow

```
Claude outputs <COMPLETE>
        ↓
Run verification command
        ↓
    Pass? ──YES──→ git commit → next item
        ↓
       NO
        ↓
Inject failure into prompt
        ↓
    Retry (no commit)
```
