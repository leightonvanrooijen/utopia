---
id: yaml-vs-markdown-concepts
title: "YAML vs Markdown for Concept Documents"
status: draft
related_specs:
  - concepts
related_adrs: []
source_conversations: []
---

## Context

When designing the concept documentation system, we needed to choose between pure YAML (like ADRs and domain docs) and Markdown with YAML frontmatter. The key requirement was optimizing for educational content that would be shared externally.

## Approaches Considered

### Option A: Pure YAML (like existing ADRs)

**Pros:**
- Consistent with other .utopia documentation formats
- Easier to parse programmatically
- Structured fields prevent formatting drift

**Cons:**
- YAML multiline strings are awkward for long prose
- Not reader-friendly when viewed raw on GitHub
- Harder to include code examples, lists, and rich formatting

### Option B: Markdown with YAML Frontmatter

**Pros:**
- Natural format for educational prose content
- Renders beautifully on GitHub and documentation sites
- Easy to include code blocks, headers, and formatting
- Familiar to developers from Jekyll, Hugo, Docusaurus, etc.

**Cons:**
- Parsing requires splitting frontmatter from body
- Less structured than pure YAML

## Our Choice

We chose **Markdown with YAML frontmatter** because concepts are fundamentally educational documents meant for human consumption. The prose-friendly nature of Markdown makes it ideal for explaining trade-offs, including examples, and sharing externally. The YAML frontmatter provides just enough structure for metadata (id, title, status, relationships) without constraining the content.

## When to Reconsider

Reconsider this decision if:
- Concepts need to be machine-processed for automated analysis
- Integration with external systems requires structured data
- The freeform nature leads to inconsistent quality
