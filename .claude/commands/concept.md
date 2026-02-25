---
description: Scan conversations for trade-off discussions and create concept documentation
allowed-tools: Read, Write, Edit, Glob, Grep, Bash(utopia *), Bash(git *)
---

# Concept Discovery

You are helping review persisted conversations for educational trade-off discussions to create or update concept documentation.

## Run the Command

Execute the utopia concept command to start an interactive session:

```bash
utopia concept
```

This will:
1. Scan `.utopia/conversations/` for unprocessed conversations
2. Analyze each for trade-off signals (comparisons, "why we chose", reasoning)
3. Check existing concepts in `.utopia/concepts/`
4. Guide you through creating or updating concepts
5. Mark conversations as processed after review

## Manual Alternative

If the CLI isn't available, you can manually review conversations:

1. List unprocessed conversations: `ls .utopia/conversations/`
2. Read each conversation file looking for trade-off discussions
3. Check existing concepts: `ls .utopia/concepts/`
4. Create new concepts following the format below

## Concept File Format

Save to `.utopia/concepts/{kebab-case-id}.md`:

```markdown
---
id: kebab-case-identifier
title: "Human Readable Title"
status: draft
related_specs:
  - spec-id-if-relevant
related_adrs:
  - adr-id-if-relevant
source_conversations:
  - conversation-id
---

## Context
[Background and why this trade-off matters]

## Approaches Considered
[Different approaches and their trade-offs]

## Our Choice
[The approach selected and reasoning]

## When to Reconsider
[Conditions that should trigger re-evaluation]
```

## Signal Detection

Only surface CLEAR trade-off signals:
- "We chose X because..."
- "The trade-off here is..."
- "Option A vs Option B"
- "Why we didn't use..."
- Explicit reasoning about design choices

Do NOT surface:
- General implementation discussion
- Simple "how to" explanations
- Code changes without rationale
- Domain terminology (use /domain instead)
