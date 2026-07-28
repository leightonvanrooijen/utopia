---
id: error-handling
title: "Error Handling & Process Exit"
description: "How errors are created, wrapped, matched, and surfaced: typed domain errors for branching, %w wrapping for infrastructure, one exit point in root.go."
tags:
  - go
  - errors
  - cli
---

## The Stack

| Purpose | Technology | Version | Notes |
|---------|------------|---------|-------|
| Error creation/wrapping | fmt.Errorf + %w | stdlib | ~169 wrap sites; the dominant norm |
| Error matching | errors.Is / errors.As | stdlib | Domain types implement `Is` for this purpose |
| Typed errors | internal/domain | N/A | `NotFoundError`, `InvalidModelError`, `CRValidationError` |
| Exit point | internal/cli/root.go `Execute()` | N/A | The ONLY place `os.Exit` is allowed |

## File Structure

```
internal/
  domain/
    discovery.go        # NotFoundError (typed, implements Is)
    model.go            # InvalidModelError, InvalidModelConfigError
    changerequest.go    # CRValidationError (multi-error aggregator)
  cli/
    root.go             # Execute(): the single print-to-stderr + os.Exit(1) chokepoint
  store.go              # boundary: typed NotFoundError vs %w-wrapped infra errors
```

### Naming Conventions
- Typed errors: `<Thing>Error` structs in `internal/domain`, each implementing `Error()` and `Is(target error) bool`.
- Error messages: lowercase, no trailing punctuation, context first: `"failed to load config: %w"`.
- Identifiers in messages use `%q` or `'%s'`: `"CR %q failed: %w"`.

## Patterns

### One exit point

Commands return errors from `RunE`. Only `Execute()` in `root.go` prints to stderr and exits.

✅ **Good** (`root.go:49-54` - the chokepoint)
```go
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
```

❌ **Bad** (`cr.go:110-120` - exits mid-handler, prints the error to *stdout*, and skips the central path)
```go
bytes, err := os.ReadFile(absPath)
if err != nil {
	fmt.Printf("✗ Failed to read file: %s\n", err)
	os.Exit(1)
}
```
Return the error instead:
```go
bytes, err := os.ReadFile(absPath)
if err != nil {
	return fmt.Errorf("failed to read %s: %w", absPath, err)
}
```

**The check-mode exception.** `format --check` needs a nonzero exit as a *status code* (like `gofmt -l`), not an error dump. Even then, don't call `os.Exit` in the handler - return a sentinel and let the command translate it:
```go
var ErrCheckFailed = errors.New("files would be reformatted")

// in RunE:
if checkOnly && wouldChangeCount > 0 {
	fmt.Fprintf(cmd.OutOrStdout(), "%d file(s) would be reformatted\n", wouldChangeCount)
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	return ErrCheckFailed
}
```
The exit still happens in `Execute()`; the handler stays testable.

### Wrap with %w, add context the caller doesn't have

Every error that crosses a function boundary gains the context that function knows: which resource, which ID, which iteration.

✅ **Good**
```go
// store.go:96 - resource + id
return fmt.Errorf("failed to delete %s %s: %w", resourceType, id, err)

// execute.go:261 - quoted identifier
return fmt.Errorf("CR %q failed: %w", cr.ID, err)

// discover.go:321 - id + iteration
lastErr = fmt.Errorf("refinement failed for %s (iteration %d): %w", c.ID, i, err)
```

❌ **Bad**
```go
return err                                    // no context added
return fmt.Errorf("error: %v", err)           // %v breaks errors.Is/As chains
return fmt.Errorf("Failed to delete!: %w", e) // capitalized, punctuated
```

### Typed errors for branching, wrapped errors for plumbing

The store boundary sets the convention: return a **typed** `*domain.NotFoundError` when callers need to branch on the case, and a **%w-wrapped** error for infrastructure failures they can only report.

✅ **Good** (`store.go:94`)
```go
if os.IsNotExist(err) {
	return &domain.NotFoundError{Resource: resourceType, ID: id}
}
return fmt.Errorf("failed to delete %s %s: %w", resourceType, id, err)
```

Typed errors implement `Is` so they match through wrapping:
```go
// domain/discovery.go:172-186
type NotFoundError struct {
	Resource string
	ID       string
}

func (e *NotFoundError) Error() string {
	return fmt.Sprintf("%s not found: %s", e.Resource, e.ID)
}

// Is allows errors.Is to match any NotFoundError regardless of resource/id.
func (e *NotFoundError) Is(target error) bool {
	_, ok := target.(*NotFoundError)
	return ok
}
```

### Match with errors.Is / errors.As, never type assertions

Type assertions break the moment an error is wrapped anywhere in the chain.

✅ **Good**
```go
var nfe *domain.NotFoundError
if errors.As(err, &nfe) {
	return fmt.Errorf("draft %q not found (use --list to see available drafts)", draftID)
}
return fmt.Errorf("failed to load draft: %w", err)

// stdlib sentinels the same way (store.go:680):
if err != nil && errors.Is(err, os.ErrNotExist) {
	return nil, nil // no previous state exists
}
```

❌ **Bad** (`promote.go:55` - fails silently if the error is ever wrapped)
```go
if _, ok := err.(*domain.NotFoundError); ok {
	return fmt.Errorf("draft '%s' not found", draftID)
}
```

### Panic only for unreachable invariants

`panic` is reserved for programmer errors that cannot occur at runtime with valid code - never for user input, I/O, or anything a caller could plausibly trigger.

✅ **Acceptable** (`chunk/chunk.go:550-560` - a compile-time-constant template failing to parse is a broken build, not a runtime condition)
```go
tmpl, err := template.New("prompt").Parse(PromptTemplate)
if err != nil {
	// This should never happen with a valid template
	panic("invalid prompt template: " + err.Error())
}
```

❌ **Bad**
```go
cr, err := store.LoadChangeRequest(id)
if err != nil {
	panic(err) // user-triggerable: bad id, missing file, corrupt YAML
}
```

## Anti-Patterns

| Don't | Do Instead | Severity |
|-------|------------|----------|
| `os.Exit` anywhere except `root.go Execute()` | Return the error from `RunE`; for check-modes, return a sentinel with `SilenceUsage/SilenceErrors` | Error |
| Type-assert on error types: `err.(*domain.NotFoundError)` | `errors.As(err, &nfe)` | Error |
| `panic` on user-triggerable conditions (input, I/O, parsing) | Return a wrapped error | Error |
| Wrap with `%v` instead of `%w` | `%w` so `errors.Is/As` see through the chain | Error |
| Print an error to stdout then continue/exit | Return it; `Execute()` routes to stderr | Error |
| `return err` bare across a package boundary | Add context: `fmt.Errorf("loading spec %s: %w", id, err)` | Warning |
| Capitalized or punctuated error messages | Lowercase phrase, no trailing punctuation | Warning |
| New sentinel-style `var Err...` for domain cases with data | Typed `<Thing>Error` struct with an `Is` method in `internal/domain` | Warning |

## Rationale

### Why one exit point
`os.Exit` skips deferred cleanup, makes handlers untestable (a test that hits the path kills the test binary), and here it also misroutes errors to stdout. With `RunE` + the `Execute()` chokepoint, every command fails uniformly: message on stderr, exit code 1, defers run.

### Why typed errors over sentinels
Cases like "not found" carry data (`Resource`, `ID`) that a plain `var ErrNotFound` can't. The struct + `Is` method pattern gives both the data and `errors.Is` compatibility. The `Is` method matches on *type* deliberately, so `errors.Is(err, &domain.NotFoundError{})` works without knowing which resource was missing.

### Why %w everywhere
169 of the codebase's wrap sites already use `%w`. It's what makes the typed-error strategy work: `errors.As` can find a `*NotFoundError` that the store returned even after three layers of CLI code added context on top.

### Why error values and console messages are different things
`"failed to read %s: %w"` (lowercase, composable) is an error *value* - it will be embedded in other messages. `"✗ Failed to read file"` (capitalized, glyph-prefixed) is a console *message* - terminal UI, covered by the terminal-output standard. Don't let the styles bleed into each other: error values never contain glyphs or newlines (`CRValidationError`'s bullet list is the one deliberate exception, for aggregated validation output).

## Examples

### A store-to-CLI error path, end to end

```go
// internal/store.go - boundary decides typed vs wrapped
func (s *YAMLStore) LoadDraft(id string) (*domain.Draft, error) {
	data, err := os.ReadFile(s.draftPath(id))
	if os.IsNotExist(err) {
		return nil, &domain.NotFoundError{Resource: "draft", ID: id}
	}
	if err != nil {
		return nil, fmt.Errorf("failed to read draft %s: %w", id, err)
	}
	var d domain.Draft
	if err := yaml.Unmarshal(data, &d); err != nil {
		return nil, fmt.Errorf("failed to unmarshal draft %s: %w", id, err)
	}
	return &d, nil
}

// internal/cli/promote.go - handler branches with errors.As, adds user guidance
func runPromote(cmd *cobra.Command, args []string) error {
	_, _, store, err := ResolveProject(cmd)
	if err != nil {
		return err
	}
	draft, err := store.LoadDraft(args[0])
	if err != nil {
		var nfe *domain.NotFoundError
		if errors.As(err, &nfe) {
			return fmt.Errorf("draft %q not found (use --list to see available drafts)", args[0])
		}
		return fmt.Errorf("failed to load draft: %w", err)
	}
	// ... promote draft ...
	return nil
}
// Failure output (stderr, exit 1, via root.go Execute()):
//   draft "d-042" not found (use --list to see available drafts)
```
