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

Opens a conversation with Claude to define what you want to build. The conversation guides you through:

1. Understanding what you want to change
2. Classifying the change type (feature, enhancement, refactor, bugfix, removal, initiative)
3. Defining acceptance criteria

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
Captures architectural decisions — technology choices, patterns adopted, approaches rejected. Signals: "we decided", "we chose X over Y", "the trade-off is". ADRs from executed CRs have higher confidence since the decision was actually implemented.

**Concepts**
Captures trade-off discussions with educational value — why something works the way it does, design rationale, mental models. These help future-you (or teammates) understand the reasoning behind the system.

**Domain**
Captures terminology and entity definitions specific to your project — the ubiquitous language. When you define what a "workspace" or "verification loop" means in your system, that belongs here.

These artifacts are injected into future Claude sessions, so the AI understands your project's decisions, concepts, and vocabulary.

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
