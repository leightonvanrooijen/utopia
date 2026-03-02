# Utopia

A conversation-first CLI for building software you actually understand.

> The friction AI removed wasn't waste. It was comprehension.
> Everything in Utopia exists to put that friction back in the right place — in the thinking, not the building.

## The Problem

AI can write code faster than you can read it. That's not a feature — it's a trap.

When you generate code you don't understand, you're not building software. You're accumulating debt you can't see, in a codebase you can't navigate, solving problems you haven't fully thought through.

## The Solution

Utopia creates a loop that forces comprehension before execution:

```
Converse → CR → Execute → Spec updates → Harvest
    ↑                                        │
    └────────────────────────────────────────┘
```

1. **Converse** — Talk through what you want to build. The conversation IS the thinking.
2. **CR** — Crystallize the conversation into a Change Request with clear acceptance criteria.
3. **Execute** — AI implements the CR autonomously, guided by tests and verification.
4. **Spec updates** — Working code gets merged, specs evolve to reflect reality.
5. **Harvest** — Extract ADRs, concepts, and domain knowledge from conversations.

The harvest feeds back into the next conversation. Your understanding compounds.

## Quick Start

### Prerequisites

- [Claude Code CLI](https://docs.anthropic.com/en/docs/claude-code) installed and authenticated
- A project with a test command (any language)

### Install

```bash
go install github.com/leightonvanrooijen/utopia/cmd/utopia@latest
```

### Initialize

```bash
cd your-project
utopia init
```

This creates a `.utopia/` directory with your project config.

### Configure Verification

Edit `.utopia/config.yaml` to set your test command:

```yaml
verification:
  command: "npm test" # or "pytest", "go test ./...", etc.
  max_iterations: 6
```

### The Loop

**1. Create a Change Request**

```bash
utopia cr
```

Opens a conversation with Claude. Discuss what you want to build. When you're ready, Utopia captures the conversation and creates a structured CR with acceptance criteria.

**2. Execute**

```bash
utopia execute
```

Select a CR. Utopia chunks it into work items and executes them autonomously — small, focused tasks with verification after each iteration.

**3. Harvest Knowledge**

```bash
utopia harvest
```

Scans your conversations for architectural decisions, concepts, and domain terminology. Creates documentation that feeds back into future conversations.

## Project Structure

```
.utopia/
├── config.yaml           # Project configuration
├── specs/                # Feature specifications
├── change-requests/      # Pending CRs
├── work-items/           # Chunked tasks for execution
├── conversations/        # Captured conversation transcripts
├── adrs/                 # Architecture Decision Records
├── concepts/             # Concept documentation
└── domain/               # Domain terminology
```

## Status

**Alpha** — Works, but expect rough edges. The core loop is functional. APIs may change.

## Philosophy

Utopia is opinionated:

- **WHAT not HOW** — Tell AI what to achieve, not how to implement it. It discovers patterns from your codebase.
- **Backpressure beats direction** — Tests and verification guide the AI, not detailed prompts.
- **Small, focused tasks** — Work items fit in one context window. Complex features get chunked.
- **Conversations are artifacts** — Your discussions contain decisions. Harvest them.

## License

GPL v3 — Free to use, modify, and distribute. Modifications must also be open source.
