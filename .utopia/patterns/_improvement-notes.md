# Pattern System Improvement Notes

Notes from pattern discovery session (2025-02) to feed into a change request for improving the patterns system.

---

## Key Insight Discovered

The current spec has `documentation-criteria` about what to document vs ignore, but misses the deeper insight:

### What IS a Pattern?

**A pattern is a template for BUILDING new things that:**

| Characteristic | Pattern | Not a Pattern |
|---------------|---------|---------------|
| Created repeatedly | Yes - you'll build this multiple times | No - it's fixed or one-time |
| Has a shape to follow | Multi-step structure across files | Single action or data format |
| Coordinates across locations | Create file + implement interface + register | Put file here, fill in fields |
| Subtle to get wrong | Compiles but fails at runtime | Validation/linting catches it |
| Not discoverable by browsing | Need to know the full workflow | See existing files and copy |

This distinction is critical for filtering. The current "Would They Get It Wrong?" test is necessary but not sufficient.

---

## Gap 1: Missing Pattern Definition

**Current state:** Spec defines what to document (codebase-specific) vs ignore (generic), but doesn't define what a pattern fundamentally IS.

**Problem:** We rejected "CR Type System" even though it passed the "would they get it wrong" test - because it's a schema, not a pattern.

**Proposed addition to spec/prompt:**

```
A pattern is a template for BUILDING things. It must be:
- Something you create multiple times
- A multi-step process across files/locations
- Not discoverable just by browsing existing code

Things that are NOT patterns (need different documentation):
- Schemas: Shape of data (use templates, validation)
- Conventions: Where things go (self-documenting via filesystem)
- Principles: Guiding rules (separate principles doc)
- Decisions: One-time choices (ADRs or CLAUDE.md)
```

---

## Gap 2: No Learnings Capture

**Current state:** Discovery session produces patterns, but learnings about what works/doesn't work are lost.

**Problem:** Each discovery session starts from scratch. Rejection reasoning isn't captured.

**Proposed addition:**

Add `_discovery-learnings.md` as a standard output of discovery sessions:
- Captures "what IS a pattern" for this codebase
- Records rejection categories with examples
- Builds detection heuristics over time
- Persists learning across sessions

**Spec change:** Add feature for learnings file output.

---

## Gap 3: Pattern Format Missing "How to Create"

**Current state:** Pattern format has Description, Responsibility, Boundaries, Naming, Examples.

**Problem:** The Strategy pattern became much more useful when we added "How to Create" with numbered steps. Patterns that are templates for building need actionable instructions.

**Proposed addition to pattern format:**

```markdown
## How to Create (optional - for buildable patterns)

1. Step one with file path
2. Step two with code example
3. Step three (registration, wiring, etc.)
```

**Spec change:** Add optional "How to Create" section to pattern file format.

---

## Gap 4: Inline Flows vs Separate Flow Files

**Current state:** Spec defines separate flow files in `.utopia/patterns/flows/`.

**Observation:** We put flow diagrams directly in patterns (completion-token, verification-before-commit) and it worked well. Separate files feel like overkill for simple flows.

**Proposed change:**

- **Inline flows:** Simple flows that involve 1-2 patterns go directly in the pattern file under a `## Flow` section
- **Separate flow files:** Complex multi-pattern flows that need their own documentation

**Spec change:** Update flow-file-format to clarify when to use separate files vs inline.

---

## Gap 5: Rejection Categories

**Current state:** Prompt says REJECT "generic knowledge" but doesn't categorize rejections.

**Proposed categories (from session):**

1. **TOO OBVIOUS** - Discoverable by browsing existing files or following imports
2. **INDUSTRY STANDARD** - Any experienced developer would know this
3. **NOT A PATTERN** - Actually a schema, convention, or one-time decision
4. **LINTER/FORMATTER TERRITORY** - Code style enforced by tooling

**Spec change:** Add rejection categories to documentation-criteria feature.

---

## Gap 6: Detection Heuristics

**Current state:** Only has "Would they get it wrong?" test.

**Proposed heuristics (from session):**

1. "Would they build this multiple times?" - If no, not a pattern
2. "Is there a multi-step shape to follow?" - If just "put X here", it's a convention
3. "Would they find it in 5 minutes of browsing?" - If yes, TOO OBVIOUS
4. "Would a senior dev from another project know this?" - If yes, INDUSTRY STANDARD
5. "Does validation catch mistakes?" - If yes, docs may be redundant
6. "Does it coordinate across multiple files/locations?" - If yes, likely a pattern

**Spec change:** Add heuristics to documentation-criteria feature.

---

## Summary of Proposed Changes

### Spec Changes

| Feature | Change |
|---------|--------|
| `documentation-criteria` | Add pattern definition, rejection categories, detection heuristics |
| `pattern-file-format` | Add optional "How to Create" section, optional "Flow" section |
| `flow-file-format` | Clarify when to use separate files vs inline flows |
| NEW: `learnings-file-format` | Define `_discovery-learnings.md` as standard output |

### System Prompt Changes

| Section | Change |
|---------|--------|
| Filter section | Add pattern definition before the filter |
| Reject section | Add rejection categories |
| Propose section | Add detection heuristics |
| Output formats | Add "How to Create" and "Flow" to pattern format |
| NEW section | Instruct to update/create learnings file |

---

## Files Created This Session

| File | Purpose |
|------|---------|
| `strategy.md` | Updated with "How to Create" and "Flow" sections |
| `completion-token.md` | New pattern with inline flow |
| `verification-before-commit.md` | New pattern with inline flow |
| `_discovery-learnings.md` | Meta-doc capturing what IS a pattern |
| `_improvement-notes.md` | This file - CR input |
