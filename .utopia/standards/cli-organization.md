---
id: cli-organization
title: "CLI Package Organization"
description:
  "How to structure cobra commands in internal/cli: thin handlers that own the
  CLI frame and delegate the algorithm to domain packages."
tags:
  - go
  - cli
  - cobra
  - architecture
---

## The Stack

| Purpose       | Technology             | Version | Notes                                                        |
| ------------- | ---------------------- | ------- | ------------------------------------------------------------ |
| CLI framework | github.com/spf13/cobra | v1.10.2 | All commands live in `internal/cli`                          |
| Language      | Go                     | 1.25.0  | Single-language project                                      |
| Persistence   | internal.YAMLStore     | N/A     | Constructed via `ResolveProject`, never directly in handlers |

## File Structure

```
cmd/utopia/main.go          # 13-line entrypoint -> cli.Execute()
internal/
  cli/                      # ONE file per command (or command family)
    root.go                 # rootCmd, Execute(), shared resolvers
    status.go               # smallest example of the standard shape
    execute.go              # exemplar: delegates algorithm to internal/ralph
    ...
  domain/                   # core types: Spec, ChangeRequest, WorkItem, Config
  chunk/  ralph/  git/      # algorithm packages - cli imports these, never the reverse
  validators/  verification/
  claude.go store.go format.go   # package internal: shared low-level primitives
```

### Naming Conventions

- One file per command (or parent command + its subcommands): `discover.go`
  holds `discoverCmd` and `discoverDomainCmd`.
- Command variable: `<name>Cmd`. Handler function: `run<Name>` with signature
  `func(cmd *cobra.Command, args []string) error`.
- Flags backing a command are package-level vars prefixed with the command name:
  `discoverVerboseFlag`.
- New commands prefer the constructor form `New<Name>Cmd()` (see `execute.go`)
  over a bare package var - it is testable without global state.

## Patterns

### Thin handler: CLI owns the frame, a domain package owns the algorithm

The `RunE` handler is responsible for CLI concerns only: flag validation,
project resolution, signal handling, context/timeout setup, and user-facing
framing of results. The actual algorithm lives in a package under `internal/`
and is called as one function.

✅ **Good** (`execute.go` → `internal/ralph`)

```go
func runExecute(cmd *cobra.Command, args []string) error {
 _, _, store, err := ResolveProject(cmd)
 if err != nil {
  return err
 }
 modelID, err := ResolveModelFlag(cmd)
 if err != nil {
  return err
 }
 ctx, cancel := signal.NotifyContext(cmd.Context(), os.Interrupt)
 defer cancel()

 // The algorithm is one call into a domain package.
 result, err := ralph.Execute(ctx, crID, store, config, absPath, modelID)
 if err != nil {
  return fmt.Errorf("execute %s: %w", crID, err)
 }
 // ...print result summary...
 return nil
}
```

❌ **Bad** (`discover.go:190-293` - a ~103-line RunE that inlines orchestration,
LLM prompting, stdout parsing, and multi-phase progress printing)

```go
func runDiscover(cmd *cobra.Command, args []string) error {
 // ...50 lines of setup...
 progress.startPhase(2, "Stage 1: Identifying and qualifying candidates")
 stage1Prompt := buildIdentifyQualifyPrompt(codebaseContext, specsSummary)
 qualifiedResult, err := cli.Prompt(ctx, stage1Prompt)
 if err != nil {
  return fmt.Errorf("identify and qualify failed: %w", err)
 }
 qualifiedCount := countYAMLItems(qualifiedResult.Stdout, "qualified")
 disqualified := parseDisqualifiedCandidates(qualifiedResult.Stdout)
 logDisqualifiedCandidates(disqualified, discoverVerboseFlag)
 // ...pipeline stages 3, 4, 5 continue inline for another 50 lines...
}
```

The discovery pipeline (qualify → refine → draft) belongs in its own package
(e.g. `internal/discover`), invoked as one call, exactly as `ralph.Execute` is.

### Command wiring

Every command file declares its command and registers it in a file-local
`init()`. Subcommands attach to their parent, not to root.

✅ **Good**

```go
var statusCmd = &cobra.Command{
 Use:   "status",
 Short: "Show project status",
 RunE:  runStatus,
}

func init() {
 rootCmd.AddCommand(statusCmd)
}

// Subcommands attach to their parent:
func init() { discoverCmd.AddCommand(discoverDomainCmd) }
```

❌ **Bad**

```go
// Registering everything centrally in root.go, or attaching a
// subcommand to rootCmd so it appears as a top-level command:
func init() { rootCmd.AddCommand(discoverDomainCmd) }
```

### Always open with the shared resolvers

`root.go` provides the common project-resolution seam. Handlers never hand-roll
path resolution or construct `YAMLStore` directly.

✅ **Good**

```go
func runStatus(cmd *cobra.Command, args []string) error {
 _, _, store, err := ResolveProject(cmd)
 if err != nil {
  return err
 }
 // ...
}
```

❌ **Bad**

```go
func runStatus(cmd *cobra.Command, args []string) error {
 dir, _ := cmd.Flags().GetString("project")
 abs, _ := filepath.Abs(dir)
 store := internal.NewYAMLStore(filepath.Join(abs, ".utopia"))
 // silently works even when .utopia doesn't exist
}
```

Resolver reference:

- `ResolveProject(cmd)` - the default: abs path + `.utopia` existence check +
  constructed store.
- `ResolveProjectDir(cmd)` - path only, no `.utopia` check (only for
  `utopia init`).
- `ResolveModelFlag(cmd)` - validates `--model` via `domain.ResolveModel`, `""`
  if unset.
- `ResolveAuthFlag(cmd)` - validates `--auth` via `domain.ValidateAuthMode`, `""`
  if unset (meaning `config.auth.mode` applies; combine with
  `domain.ResolveAuthMode`).

### One-way imports

`internal/cli` imports downward into `internal`, `internal/domain`,
`internal/chunk`, `internal/ralph`, `internal/git`, `internal/validators`,
`internal/analysis/types`. No package ever imports `internal/cli`.

## Anti-Patterns

| Don't                                                              | Do Instead                                                    | Severity |
| ------------------------------------------------------------------ | ------------------------------------------------------------- | -------- |
| Import `internal/cli` from any other package                       | Move the shared code down into `domain` or a new package      | Error    |
| Construct `internal.NewYAMLStore` / resolve paths inside a handler | Open with `ResolveProject(cmd)`                               | Error    |
| Put a multi-stage algorithm inline in `RunE`                       | Extract to a package under `internal/`, call as one function  | Warning  |
| Let a command file grow past ~400 lines                            | Split: extract the algorithm package and/or per-command files | Warning  |
| Attach subcommands to `rootCmd`                                    | Attach to their parent command in that file's `init()`        | Warning  |

## Rationale

### Why thin handlers

The `cli` package is already the largest area of the codebase (~7,400 lines),
with a bimodal split: clean small commands (79-352 lines) and
orchestration-heavy outliers (`discover.go` 1348, `harvest.go` 1289). Logic
buried in `RunE` handlers can only be tested through cobra and can't be reused
(e.g. by `ralph` or future daemon modes). `execute.go`/`ralph` proves the
division works: the handler kept signals, flags, and framing; the loop became an
independently testable package.

### Why the shared resolvers

`ResolveProject` centralizes the "not a Utopia project (run 'utopia init'
first)" error and store construction, so every command fails the same way on an
uninitialized directory. Bypassing it produces commands that half-work outside a
project.

### Why constructor form for new commands

`NewExecuteCmd()` builds the command and wires flags without package-level
mutable state, so tests can construct a fresh command per case. Existing
package-var commands don't need migrating, but new commands should use the
constructor.

## Examples

### A new command, done right

A hypothetical `utopia audit` command that runs an audit algorithm living in
`internal/audit`:

```go
// internal/cli/audit.go
package cli

import (
 "fmt"

 "github.com/spf13/cobra"

 "utopia/internal/audit"
)

func NewAuditCmd() *cobra.Command {
 var verbose bool
 cmd := &cobra.Command{
  Use:   "audit",
  Short: "Audit specs against the codebase",
  RunE: func(cmd *cobra.Command, args []string) error {
   _, utopiaDir, store, err := ResolveProject(cmd)
   if err != nil {
    return err
   }
   result, err := audit.Run(cmd.Context(), store, utopiaDir)
   if err != nil {
    return fmt.Errorf("audit: %w", err)
   }
   fmt.Fprintf(cmd.OutOrStdout(), "✓ %d specs audited, %d drifted\n",
    result.Total, result.Drifted)
   return nil
  },
 }
 cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "verbose output")
 return cmd
}

func init() {
 rootCmd.AddCommand(NewAuditCmd())
}
```

The handler is ~15 lines: resolve, delegate, frame the result. Everything about
_what an audit is_ lives in `internal/audit`, where it has its own table-driven
tests.
