# Pattern Discovery Learnings

This document captures learnings from pattern discovery sessions to improve future detection.

---

## What IS a Pattern?

A pattern is a **template for BUILDING new things** that:

| Characteristic | Pattern | Not a Pattern |
|---------------|---------|---------------|
| **Created repeatedly** | Yes - you'll build this multiple times | No - it's fixed or one-time |
| **Has a shape to follow** | Multi-step structure across files | Single action or data format |
| **Coordinates across locations** | Create file + implement interface + register | Put file here, fill in fields |
| **Subtle to get wrong** | Compiles but fails at runtime | Validation/linting catches it |
| **Not discoverable by browsing** | Need to know the full workflow | See existing files and copy |

### Example: Strategy Pattern (IS a pattern)
- You'll create new strategies over time
- Shape: interface + implementation + registry + main.go registration
- Wrong approach compiles but strategy isn't available
- Need to know all four steps

### Example: CR Type System (NOT a pattern)
- CR types are fixed, you don't create new ones often
- It's a data format - fill in fields
- Validation tells you what's wrong
- Copy existing CR and modify

---

## What Else Exists (Not Patterns)

| Concept | What It Is | Example |
|---------|-----------|---------|
| **Schema** | Shape of data | CR type field requirements |
| **Convention** | Where things go | Work items in `.utopia/work-items/{id}/` |
| **Principle** | Guiding rule | "Only verification determines truth" |
| **Decision** | One-time choice | "We use YAML not JSON" |

These may need documentation, but not as patterns.

---

## Rejection Categories

### 1. TOO OBVIOUS
Discoverable by browsing existing files or following imports.

### 2. INDUSTRY STANDARD
Any experienced developer would know this from general knowledge.

### 3. NOT A PATTERN
Actually a schema, convention, or one-time decision.

### 4. LINTER/FORMATTER TERRITORY
Code style enforced by tooling.

---

## Detection Heuristics

1. **"Would they build this multiple times?"** - If no, it's not a pattern.

2. **"Is there a multi-step shape to follow?"** - If it's just "put X here" or "fill in Y", it's a convention or schema.

3. **"Would they find it in 5 minutes of browsing?"** - If yes, TOO OBVIOUS.

4. **"Would a senior dev from another project know this?"** - If yes, INDUSTRY STANDARD.

5. **"Does validation catch mistakes?"** - If yes, docs may be redundant.

6. **"Does it coordinate across multiple files/locations?"** - If yes, likely a pattern.

---

## Rejected Candidates

### Work Item Organization
- **Category:** TOO OBVIOUS
- **Why not:** Directory structure is self-documenting

### Change Request Type System
- **Category:** NOT A PATTERN (it's a schema)
- **Why not:** Data format, not something you build
- **Better as:** Template files, validation error messages

### Infrastructure Layer Boundaries
- **Category:** TOO OBVIOUS
- **Why not:** Following imports reveals the structure

### Cobra CLI Framework
- **Category:** INDUSTRY STANDARD
- **Why not:** Any Go dev knows this

### YAML Serialization
- **Category:** INDUSTRY STANDARD
- **Why not:** Standard Go practice

### Context for Cancellation
- **Category:** INDUSTRY STANDARD
- **Why not:** Standard Go concurrency

### Domain Types with Methods
- **Category:** INDUSTRY STANDARD
- **Why not:** Standard OOP-style Go

### Error Wrapping
- **Category:** INDUSTRY STANDARD
- **Why not:** Standard Go 1.13+ practice
