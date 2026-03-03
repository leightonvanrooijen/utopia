---
id: dead-code-detection-approaches
title: "Dead Code Detection Approaches in Go"
status: draft
related_specs: []
related_adrs: []
source_conversations:
  - cr-session-20260302-155808
---

## Context

Dead code accumulates silently in any codebase. In Go specifically, the compiler catches unused local variables, but **exported functions, types, and package-level code** can linger indefinitely. Different detection approaches have different capabilities, false positive rates, and performance characteristics.

This concept explores the trade-offs between detection approaches to help teams choose the right tools for their codebase health strategy.

## Detection Approaches

### Static Analysis

Tools like `deadcode` and `unused` analyze code without running it.

**How it works:** Builds a call graph from entry points (main, init, exported funcs) and identifies unreachable code.

**Pros:**
- Fast execution, no runtime needed
- Catches obviously unreachable code
- Can run in CI pipelines
- No test coverage requirements

**Cons:**
- Cannot detect dynamically-used code (reflection, plugins)
- May have false positives with interface implementations
- Misses code called only via `go:linkname` or assembly
- Entry point detection can be tricky for libraries

**Best for:** Application code with clear entry points, catching obvious dead code quickly.

### Coverage-Based Detection

Run tests and identify code that's never executed.

**How it works:** Instruments code, runs test suite, reports lines/functions with zero coverage.

**Pros:**
- Shows what's actually exercised in practice
- Catches code that's technically reachable but never used
- Integrates with existing test workflows

**Cons:**
- Requires comprehensive test coverage to be meaningful
- Slow execution (must run full test suite)
- May flag intentionally unused code (error handlers, fallbacks)
- Test coverage != production usage

**Best for:** Mature codebases with good test coverage, finding "probably dead" code.

### Dependency Graph Analysis

Analyze import relationships to find orphaned packages or files.

**How it works:** Builds package dependency graph, identifies packages with no importers.

**Pros:**
- Can find entire orphaned packages/files
- Fast for coarse-grained analysis
- No false positives at package level

**Cons:**
- Coarser granularity (package level, not function level)
- Misses dead code within actively-used packages
- Doesn't catch unused exported functions

**Best for:** Finding orphaned packages after refactoring, cleaning up module structure.

### Manual Review Triggers

Flag code based on heuristics like age, change frequency, or ownership.

**How it works:** Analyzes git history to find code that hasn't been modified or reviewed in a long time.

**Pros:**
- Catches "zombie" code that lingers without owners
- Surfaces code for human review
- Can identify abandoned features

**Cons:**
- Age doesn't equal dead (stable code is often old)
- Requires git history analysis tooling
- High false positive rate without human judgment

**Best for:** Supplementary signal, triggering periodic code review.

## Scope Considerations

When implementing dead code detection, consider what you're trying to find:

| Scope | Detection Difficulty | Tools |
|-------|---------------------|-------|
| **Unused functions/methods** | Medium | deadcode, unused |
| **Unused types** | Medium | unused, staticcheck |
| **Unused struct fields** | Hard | fieldalignment, manual |
| **Unused parameters** | Medium | unparam |
| **Unused packages** | Easy | dependency analysis |

## Recommended Strategy

For most Go projects, a layered approach works well:

1. **CI Gate:** Run `deadcode` and `unused` on every PR (fast, catches obvious issues)
2. **Periodic Deep Scan:** Coverage-based analysis monthly (thorough, finds subtle issues)
3. **Refactoring Trigger:** Dependency graph analysis after major changes
4. **Human Review:** Age-based flags for annual codebase cleanup

## When to Reconsider

- **High false positive rate:** If tools flag too much valid code, adjust configuration or switch approaches
- **Plugin/reflection heavy:** Static analysis struggles; lean more on coverage-based
- **Library code:** Entry point detection is harder; may need explicit "public API" markers
- **Rapid growth:** Invest in prevention (unused detection in CI) over periodic cleanup
