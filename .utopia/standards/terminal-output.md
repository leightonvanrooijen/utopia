---
id: terminal-output
title: "Terminal Output & CLI UX"
description: "One shared output vocabulary (glyphs, banners, progress) routed through cobra's injectable writers so command output is consistent and testable."
tags:
  - go
  - cli
  - ux
  - output
---

## The Stack

| Purpose | Technology | Version | Notes |
|---------|------------|---------|-------|
| Output streams | cobra `cmd.OutOrStdout()` / `cmd.ErrOrStderr()` | v1.10.2 | The ONLY way to write output - never package-level `fmt.Print*` |
| Shared renderer | `internal/cli/ui` (to be created) | N/A | Banners, glyphs, progress, summaries live here |
| Testing | `cmd.SetOut(&buf)` + std testing | stdlib | Output becomes assertable once routed through the writer |

## File Structure

```
internal/cli/
  ui/
    ui.go          # Printer type wrapping an io.Writer; glyph + banner vocabulary
    progress.go    # phase progress ([n/N] ... done (X.Xs)) - promoted from discover.go
    summary.go     # shared "COMPLETE" summary renderer (replaces the two
                   #   near-duplicate printers in discover.go)
  <command>.go     # commands hold NO formatting logic beyond one-line status prints
```

### Naming Conventions
- The shared package is `ui`; commands construct `ui.NewPrinter(cmd.OutOrStdout(), cmd.ErrOrStderr())` at the top of `RunE`.
- Glyph constants are named for *meaning*, not shape: `ui.Success` (✓), `ui.Failure` (✗), `ui.Warning` (⚠) - so the shape can change in one place.
- Rules/banners are computed with `strings.Repeat`, never frozen string literals.

## Patterns

### Route all output through cobra's writers

Package-level `fmt.Println`/`fmt.Printf` writes to the real process stdout - unredirectable and untestable. There are currently ~199 such calls and zero tests in `internal/cli`; every new or touched line of output must go through `cmd.OutOrStdout()` / `cmd.ErrOrStderr()` (usually via a `ui.Printer`).

✅ **Good**
```go
func runStatus(cmd *cobra.Command, args []string) error {
	out := ui.NewPrinter(cmd.OutOrStdout(), cmd.ErrOrStderr())
	// ...
	out.Successf("%s is valid", filepath.Base(absPath))
	return nil
}
```

❌ **Bad** (the current norm - `cr.go:128`)
```go
fmt.Printf("✓ %s is valid\n", filepath.Base(absPath))
```

### Results to stdout, diagnostics to stderr

Primary output (the thing a user might pipe or grep) goes to stdout. Progress, warnings, and errors go to stderr. Today only one line in the whole package writes to stderr (`root.go:51`); even `✗ Invalid YAML syntax` goes to stdout.

✅ **Good**
```go
out.Warnf("failed to commit CR: %v (continuing)", err)   // -> stderr
out.Printf("Created %d draft specifications\n", n)        // -> stdout
```

❌ **Bad** (`cr.go:491` - a warning polluting pipeable stdout)
```go
fmt.Printf("⚠ Failed to commit CR: %s\n", err)
```

### One glyph vocabulary

The codebase already converged on a de-facto vocabulary - codify it as constants in `ui` and stop reinventing per file:

| Glyph | Meaning | Constant |
|-------|---------|----------|
| `✓` | success / done / created | `ui.Success` |
| `✗` | failure / invalid | `ui.Failure` |
| `⚠` | warning / uncertainty | `ui.Warning` |
| `●` / `◐` / `○` | confidence high / medium / low | `ui.ConfidenceGlyph(c)` |
| `•` | bullet in summaries | `ui.Bullet` |
| `[n/N] ... done (X.Xs)` | phase progress | `ui.Progress` |

✅ **Good**
```go
out.Printf("  %s %s: %s\n", ui.Success, task.ID, task.Description)
```

❌ **Bad** (today: `discover.go` uses `•` bullets, `harvest.go` uses `-`; the confidence icon switch is copy-pasted between `printDiscoverySummary` and `printDomainDiscoverySummary`)
```go
confidenceIcon := "○"
switch d.Confidence {
case domain.DraftConfidenceHigh:
	confidenceIcon = "●"
case domain.DraftConfidenceMedium:
	confidenceIcon = "◐"
}
```

### Banners and rules are shared and computed

`═══` "COMPLETE" banners appear in `discover.go`, `promote.go`, and `shape.go` as hardcoded 63-character string literals. One renderer, computed width:

✅ **Good**
```go
// internal/cli/ui/ui.go
const ruleWidth = 63

func (p *Printer) Banner(title string) {
	rule := strings.Repeat("═", ruleWidth)
	pad := (ruleWidth - len(title)) / 2
	fmt.Fprintf(p.out, "\n%s\n%s%s\n%s\n\n", rule, strings.Repeat(" ", pad), title, rule)
}

// caller:
out.Banner("DISCOVERY COMPLETE")
```

❌ **Bad** (`discover.go:704-707`, duplicated in `promote.go`, `shape.go`)
```go
fmt.Println("═══════════════════════════════════════════════════════════════")
fmt.Println("                    DISCOVERY COMPLETE")
fmt.Println("═══════════════════════════════════════════════════════════════")
```

### Progress and verbosity live in ui, not per command

`discoverProgress` (`discover.go:36-58`) is the right idea in the wrong place - promote it to `ui`, and make `--verbose` behave the same in every command that offers it.

✅ **Good**
```go
prog := ui.NewProgress(cmd.ErrOrStderr(), totalPhases, verbose)
prog.StartPhase(2, "Stage 1: Identifying and qualifying candidates")
// ...
prog.EndPhase(fmt.Sprintf("%d qualified, %d disqualified", q, d))
prog.Verbosef("  Collected: %s", f.path)   // no-op unless --verbose
```

❌ **Bad** - a fourth command defining its own `xProgress` struct and `verbosePrintf` clone.

### Exemption: LLM prompt templates

Glyphs, arrows, and markdown tables inside prompt-template strings (e.g. `harvest.go`'s example tables) are payload for the model, not terminal output. This standard does not apply to them - only to what humans see in the terminal.

## Anti-Patterns

| Don't | Do Instead | Severity |
|-------|------------|----------|
| Package-level `fmt.Print*` for command output | `ui.Printer` over `cmd.OutOrStdout()` / `cmd.ErrOrStderr()` | Error |
| Print errors/warnings to stdout | Errors: return from `RunE` (root prints to stderr); warnings: `out.Warnf` -> stderr | Error |
| Hardcode `═══`/`───` rule literals | `out.Banner(...)` / `out.Rule()` with computed width | Warning |
| Copy-paste a summary/confidence renderer into a new command | Extend `ui/summary.go` | Warning |
| Raw glyph literals (`"✓ ..."`) in command files | `ui.Success` / `ui.Failure` / `ui.Warning` constants | Warning |
| A new per-command progress struct or `verbosePrintf` clone | `ui.NewProgress(...)` | Warning |
| Mixed bullets (`•` vs `-`) across commands | `ui.Bullet` everywhere | Warning |

## Rationale

### Why cobra's writers instead of fmt
`cmd.OutOrStdout()` defaults to stdout, so behavior is identical in production - but tests can call `cmd.SetOut(&buf)` and assert on output. Today `internal/cli` has zero test files partly *because* there is no seam: you cannot capture what a command prints. This one change makes ~7,400 lines of the codebase's largest package testable.

### Why stdout/stderr separation
Utopia's output includes counts and file paths users will pipe (`utopia status | grep ...`). Progress spinners and `⚠` warnings interleaved on stdout corrupt that. The Unix convention (results on stdout, chatter on stderr) also means `--verbose` output never changes what a script consuming stdout sees.

### Why a ui package instead of "just be consistent"
Consistency by discipline has already failed here: three commands grew banners, three grew inline glyphs, two summary printers are near-verbatim duplicates, and bullet characters diverged. Constants and shared renderers make the consistent thing the *easy* thing - and give AI agents working in this repo one obvious place to look.

## Examples

### The ui package core

```go
// internal/cli/ui/ui.go
package ui

import (
	"fmt"
	"io"
	"strings"
)

const (
	Success = "✓"
	Failure = "✗"
	Warning = "⚠"
	Bullet  = "•"

	ruleWidth = 63
)

type Printer struct {
	out io.Writer // results (stdout)
	err io.Writer // diagnostics (stderr)
}

func NewPrinter(out, err io.Writer) *Printer {
	return &Printer{out: out, err: err}
}

func (p *Printer) Printf(format string, a ...any) { fmt.Fprintf(p.out, format, a...) }

func (p *Printer) Successf(format string, a ...any) {
	fmt.Fprintf(p.out, "%s %s\n", Success, fmt.Sprintf(format, a...))
}

func (p *Printer) Warnf(format string, a ...any) {
	fmt.Fprintf(p.err, "%s %s\n", Warning, fmt.Sprintf(format, a...))
}

func (p *Printer) Banner(title string) {
	rule := strings.Repeat("═", ruleWidth)
	pad := max(0, (ruleWidth-len(title))/2)
	fmt.Fprintf(p.out, "\n%s\n%s%s\n%s\n\n", rule, strings.Repeat(" ", pad), title, rule)
}

func (p *Printer) Rule() {
	fmt.Fprintln(p.out, strings.Repeat("─", ruleWidth))
}
```

### A command using it, with the output test it enables

```go
// internal/cli/validate.go (inside RunE)
out := ui.NewPrinter(cmd.OutOrStdout(), cmd.ErrOrStderr())
if err := domain.ValidateChangeRequest(&cr); err != nil {
	return fmt.Errorf("%s: %w", filepath.Base(absPath), err)
}
out.Successf("%s is valid", filepath.Base(absPath))
return nil
```

```go
// internal/cli/validate_test.go
func TestValidateOutput(t *testing.T) {
	cmd := NewValidateCmd()
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{"testdata/valid-cr.yaml"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got, want := stdout.String(), "✓ valid-cr.yaml is valid\n"; got != want {
		t.Errorf("stdout = %q, want %q", got, want)
	}
	if stderr.Len() != 0 {
		t.Errorf("expected empty stderr, got %q", stderr.String())
	}
}
```
