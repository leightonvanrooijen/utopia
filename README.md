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

Edit `.utopia/config.yaml` to set your verification command:

```yaml
verification:
  command: "npm test" # or "pytest", "go test ./...", etc.
  max_iterations: 6
```

Your verification command is the backpressure that guides AI execution. A good verification command:

- **Runs your test suite** — The primary signal that code works
- **Runs fast enough to iterate** — Executed after every work item
- **Exits non-zero on failure** — So Utopia knows to retry or stop
- **Optionally includes linting/type-checking** — Catches more issues early

Example for a TypeScript project:

```bash
#!/bin/bash
npm run typecheck && npm run lint && npm test
```

The `max_iterations` setting limits how many times the AI can retry a failing work item before giving up.

### The Loop

**1. Create a Change Request**

```bash
utopia cr
```

Opens a conversation with Claude to define what you want to build. The conversation guides you through:

1. Understanding what you want to change
2. Classifying the change type
3. Defining acceptance criteria

**CR Types** — The key question is: "Does this change observable behavior?"

| Type | When to use |
|------|-------------|
| **feature** | New capability that doesn't exist today |
| **enhancement** | Modifying how an existing feature works |
| **refactor** | Code improvement without behavior change |
| **bugfix** | Correcting behavior to match the spec |
| **removal** | Deleting an existing capability |
| **initiative** | Multi-phase changes with ordered execution |

When Claude has captured your intent, it writes the CR to `.utopia/change-requests/`. Press `Ctrl+C` to exit the conversation — Utopia validates the CR format. If validation fails, the errors are fed into a new Claude session that automatically fixes the issues.

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

### Knowledge Artifacts

Harvest extracts three types of documentation from your conversations:

**ADRs (Architecture Decision Records)**
Captures architectural decisions — technology choices, patterns adopted, approaches rejected. Signals: "we decided", "we chose X over Y", "the trade-off is".

**Concepts**
Captures trade-off discussions with educational value — why something works the way it does, design rationale, mental models. These help future-you (or teammates) understand the reasoning behind the system.

**Domain**
Captures terminology and entity definitions specific to your project — the ubiquitous language. When you define what a "workspace" or "verification loop" means in your system, that belongs here.

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
