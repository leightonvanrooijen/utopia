---
description: Scan conversations for domain terminology and entity discussions, guiding domain doc creation or updates
allowed-tools: Read, Write, Edit, Glob, Grep, Bash(utopia *), Bash(git *)
---

# Domain Discovery

You are helping review persisted conversations for domain terminology and entity discussions to create or update domain documentation.

## Run the Command

Execute the utopia domain command to start an interactive session:

```bash
utopia domain
```

This will:
1. Scan `.utopia/conversations/` for unprocessed conversations
2. Analyze each for domain signals (term definitions, entity discussions, relationship clarifications)
3. Check existing domain docs in `.utopia/domain/`
4. Guide you through creating or updating domain docs
5. Mark conversations as processed after review

## Manual Alternative

If the CLI isn't available, you can manually review conversations:

1. List unprocessed conversations: `ls .utopia/conversations/`
2. Read each conversation file looking for domain terminology discussions
3. Check existing domain docs: `ls .utopia/domain/`
4. Create or update domain docs following the format below

## Domain File Format

Save to `.utopia/domain/{bounded-context-id}.yaml`:

```yaml
id: bounded-context-id
title: "Human Readable Title"
description: |
  Brief description of this bounded context and its purpose
  in the system.

terms:
  - term: TermName
    definition: Clear, concise definition of what this term means in this context
    aliases:
      - alternative name
      - another alias

entities:
  - name: EntityName
    relationships:
      - type: relationship-verb
        target: TargetEntity
```

## Signal Detection

Only surface CLEAR domain signals:
- "X means Y" or "X is defined as Y" (term definitions)
- "X is a type of Y" or "X extends Y" (entity relationships)
- "X relates to Y by Z" or "X depends on Y" (relationship clarifications)
- "We call this X" or "The term for this is X" (naming conventions)
- "This belongs to the X bounded context" (context boundaries)
- Explicit discussions about ubiquitous language

Do NOT surface:
- General implementation discussion
- Code variable naming without domain significance
- Casual use of common terms
- Trade-off discussions (use /concept instead)
- Architectural decisions (use /adr instead)

## Before Creating New Domain Docs

Always check existing docs first:

```bash
ls .utopia/domain/
```

If a related bounded context exists:
- **Suggest updating it** instead of creating a new one
- Add new terms to the existing `terms` array
- Add new entities to the existing `entities` array
- Maintain consistency with existing definitions

## Term Format Guidelines

- **term**: Use PascalCase for formal terms (e.g., `WorkItem`, `ChangeRequest`)
- **definition**: Write a clear, single-sentence definition
- **aliases**: List common variations that should map to this term (e.g., `work item`, `WI`)

## Entity Relationship Types

Common relationship verbs:
- `contains` / `belongs-to` (composition)
- `references` (association)
- `derived-from` (transformation)
- `depends-on` (dependency)
- `produces` / `consumes` (data flow)
- `executes` / `verified-by` (process)
- `supersedes` (versioning)

## Conversational Guidance

When helping the user:
1. Present discovered domain signals with context
2. Ask which bounded context they belong to
3. Propose term definitions and get confirmation
4. Check for conflicts with existing terms
5. Update or create the domain file
6. Mark the conversation as processed

## Marking Conversations Processed

After reviewing a conversation, update its status:

```yaml
status: processed
```

This prevents the same conversation from appearing in future scans.
