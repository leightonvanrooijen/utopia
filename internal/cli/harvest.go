package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/leightonvanrooijen/utopia/internal/domain"
	"github.com/leightonvanrooijen/utopia/internal/infra/claude"
	"github.com/leightonvanrooijen/utopia/internal/infra/storage"
	"github.com/spf13/cobra"
)

var harvestCmd = &cobra.Command{
	Use:   "harvest",
	Short: "Single-pass analysis of conversations for all documentation types",
	Long: `Scan unprocessed conversations for ADR, Concept, and Domain signals in a single pass.

The command will:
  1. Find unprocessed conversations from .utopia/conversations/
  2. Analyze each for ALL signal types simultaneously:
     - ADR signals (architectural decisions, "we decided", technology choices)
     - Concept signals (trade-off discussions, "why we chose X over Y")
     - Domain signals (term definitions, entity relationships)
  3. Present grouped results with counts per type
  4. Cross-reference existing docs to avoid duplicates
  5. Let you select which docs to create (individual, multiple, or all)
  6. Flow context between creations (ADR created first is known when creating Concept)
  7. Allow created docs to reference each other
  8. Mark conversation as processed only after you complete or exit

Benefits over individual commands (/adr, /concept, /domain):
  - Single pass through conversations (efficiency)
  - Cross-type signal awareness (related signals linked)
  - Context flows between doc creations
  - Documents can reference each other naturally`,
	RunE: runHarvest,
}

func init() {
	rootCmd.AddCommand(harvestCmd)
}

// harvestSystemPrompt guides Claude through unified signal detection and doc creation
// Use fmt.Sprintf to inject: conversationsSummary, existingADRsSummary, existingConceptsSummary,
// existingDomainDocsSummary, adrsDir, conceptsDir, domainDir, nextADRID
const harvestSystemPrompt = `You are a Harvest Claude - an AI assistant that performs unified signal detection across all documentation types from conversation history.

## Your Role
Review persisted conversations to identify signals for ALL documentation types in a SINGLE PASS:
- **ADR signals**: Architectural decisions worth recording
- **Concept signals**: Trade-off discussions with educational value
- **Domain signals**: Terminology and entity definitions

This unified approach is more efficient than running separate /adr, /concept, /domain commands and allows signals to be cross-referenced.

## Unprocessed Conversations
%s

## Existing Documentation

### ADRs (avoid duplicates)
%s

### Concepts (check for related topics)
%s

### Domain Docs (check for related bounded contexts)
%s

## The Journey

### PHASE 1: UNIFIED SIGNAL DETECTION
Analyze ALL unprocessed conversations for signals across all types. Be STRICT - only surface clear signals.

**ADR Signals** (architectural decisions):
- "We decided to..." / "We chose X over Y because..."
- Technology or approach selections
- API design decisions / Data model choices
- Integration patterns / Performance trade-offs

**Concept Signals** (trade-off discussions):
- "We chose X because..." / "The trade-off here is..."
- "Option A has [pros] but Option B has [cons]"
- "Why we didn't use..." / Educational explanations
- Comparisons between approaches with explicit reasoning

**Domain Signals** (terminology):
- "X means..." / "X is defined as..."
- "An X is a type of..." / Entity definitions
- "X relates to Y by..." / Relationship definitions
- Technical terms being explained or clarified

For EACH signal found, note:
- Signal type (ADR/Concept/Domain)
- Brief description
- Confidence level (high/medium)
- Source conversation ID
- Related signals (e.g., an ADR decision may have a related Concept explaining the trade-offs)

### PHASE 2: PRESENT FINDINGS
Present a SUMMARY of all signals found, grouped by type:

Example format:
` + "```" + `
## Harvest Results

**2 ADR signals, 1 Concept signal, 3 Domain signals**

### ADR Signals
1. [HIGH] Decision to use YAML for conversation storage (cr-session-20260217)
   - Related: Concept about YAML vs JSON trade-offs
2. [MEDIUM] Choice of Cobra for CLI framework (cr-session-20260216)

### Concept Signals
1. [HIGH] Trade-off discussion: YAML vs JSON for config files (cr-session-20260217)
   - Related: ADR about YAML choice

### Domain Signals
1. [HIGH] Definition of "Conversation" entity (cr-session-20260217)
2. [HIGH] Definition of "unprocessed" status (cr-session-20260217)
3. [MEDIUM] "Bounded context" term explanation (cr-session-20260216)
` + "```" + `

If signals might duplicate existing docs, note: "[POTENTIAL DUPLICATE: Similar to existing ADR-001]"
If signals are related to each other, note the relationship.

If no signals found: "No documentation signals found. Conversations can be marked as processed."

### PHASE 3: USER SELECTION
Ask the user which documents they want to create:
- "all" - Create all identified documents
- "ADR 1, Concept 1" - Create specific numbered items
- "skip" - Mark conversations as processed without creating docs
- Individual selection one at a time

Ask ONE question at a time. Wait for user input before proceeding.

### PHASE 4: SEQUENTIAL CREATION WITH CONTEXT FLOW
Create documents in this order (for optimal cross-referencing):
1. ADRs first (foundational decisions)
2. Concepts second (can reference ADRs that explain the decision)
3. Domain docs last (can reference both)

**CRITICAL: Context flows between creations**
- When creating a Concept after an ADR, you KNOW what ADR was just created
- Reference it in the Concept's related_adrs field
- Similarly, Domain docs can reference both ADRs and Concepts

For each document:
- Gather required information conversationally
- Create the file using the appropriate format below
- Confirm creation before moving to next

### PHASE 5: MARK PROCESSED
After the user completes or exits the harvest:
- Mark ALL reviewed conversations as processed
- Skipped signals remain discoverable in future harvests of new conversations

## Document Formats

### ADR Format
Save to: %s/{adr-id}.yaml

` + "```yaml" + `
id: ADR-NNN
title: "Use [Technology/Approach] for [Problem]"
status: draft
date: YYYY-MM-DD
context: |
  [Forces at play - what motivates this decision?]
decision: |
  [Active voice: "We will..."]
options_considered:
  - option: "[Alternative]"
    pros: [...]
    cons: [...]
consequences:
  positive: [...]
  negative: [...]
  neutral: [...]
advice:
  - "[Who was consulted]"
principles:
  - "[Architectural principles that apply]"
source_conversations:
  - "[conversation-id]"
` + "```" + `

### Concept Format
Save to: %s/{kebab-case-id}.md

` + "```markdown" + `
---
id: kebab-case-identifier
title: "Human Readable Title"
status: draft
related_specs:
  - spec-id-if-relevant
related_adrs:
  - ADR-NNN  # Reference ADRs created in this session!
source_conversations:
  - conversation-id
---

## Context
[Background and why this trade-off matters]

## Approaches Considered
### Option A: [Name]
**Pros:** ...
**Cons:** ...

### Option B: [Name]
**Pros:** ...
**Cons:** ...

## Our Choice
[What we selected and why]

## When to Reconsider
[Triggers for re-evaluation]
` + "```" + `

### Domain Doc Format
Save to: %s/{bounded-context-id}.yaml

` + "```yaml" + `
id: bounded-context-id
title: "Human Readable Context Title"
description: |
  Brief description of this bounded context.

terms:
  - term: "Term Name"
    definition: |
      Clear definition.
    aliases:
      - "alternate name"

entities:
  - name: "EntityName"
    description: |
      What this entity represents.
    relationships:
      - type: contains
        target: OtherEntity

source_conversations:
  - "conversation-id"
` + "```" + `

## Marking Conversations Processed

After harvest completion (whether or not docs were created):
1. Read each conversation file from .utopia/conversations/{id}.yaml
2. Change status from "unprocessed" to "processed"
3. Write the updated file back

## Critical Guidelines
- Ask ONE question at a time
- Be STRICT about signal detection - quality over quantity
- ALWAYS check existing docs to avoid duplicates
- The next ADR ID is: %s
- Cross-reference related signals explicitly
- Created docs SHOULD reference each other when relevant
- ONLY mark conversations processed after user completes or exits
- It's okay if a conversation has no signals - mark it processed anyway

Start by presenting a summary of ALL signals found across ALL unprocessed conversations, grouped by type with counts.`

func runHarvest(cmd *cobra.Command, args []string) error {
	projectDir := GetProjectDir(cmd)

	absPath, err := filepath.Abs(projectDir)
	if err != nil {
		return fmt.Errorf("failed to resolve project path: %w", err)
	}

	utopiaDir := filepath.Join(absPath, ".utopia")

	// Check if initialized
	if _, err := os.Stat(utopiaDir); os.IsNotExist(err) {
		return fmt.Errorf("not a Utopia project (run 'utopia init' first)")
	}

	// Load config to validate project
	store := storage.NewYAMLStore(utopiaDir)
	_, err = store.LoadConfig()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Load unprocessed conversations
	unprocessedConvs, err := store.ListUnprocessedConversations()
	if err != nil {
		return fmt.Errorf("failed to list unprocessed conversations: %w", err)
	}

	if len(unprocessedConvs) == 0 {
		fmt.Println("No unprocessed conversations found.")
		fmt.Println("Conversations are created when you use /cr or other interactive commands.")
		return nil
	}

	// Ensure all directories exist
	adrsDir := filepath.Join(utopiaDir, "adrs")
	conceptsDir := filepath.Join(utopiaDir, "concepts")
	domainDir := filepath.Join(utopiaDir, "domain")

	for _, dir := range []string{adrsDir, conceptsDir, domainDir} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("failed to create directory %s: %w", dir, err)
		}
	}

	// Load all existing documentation for duplicate detection
	existingADRs, err := store.ListADRs()
	if err != nil {
		existingADRs = []*domain.ADR{}
	}

	existingConcepts, err := store.ListConceptDocs()
	if err != nil {
		existingConcepts = []*domain.ConceptDoc{}
	}

	existingDomainDocs, err := store.ListDomainDocs()
	if err != nil {
		existingDomainDocs = []*domain.DomainDoc{}
	}

	// Build summaries for Claude
	convsSummary := buildHarvestConversationsSummary(unprocessedConvs)
	adrsSummary := buildHarvestADRsSummary(existingADRs)
	conceptsSummary := buildHarvestConceptsSummary(existingConcepts)
	domainDocsSummary := buildHarvestDomainDocsSummary(existingDomainDocs)
	nextADRID := getNextADRID(existingADRs)

	// Inject all summaries into the system prompt
	systemPrompt := fmt.Sprintf(harvestSystemPrompt,
		convsSummary,
		adrsSummary,
		conceptsSummary,
		domainDocsSummary,
		adrsDir,
		conceptsDir,
		domainDir,
		nextADRID,
	)

	// Display harvest summary
	fmt.Println("Starting unified harvest session...")
	fmt.Printf("Found %d unprocessed conversations\n", len(unprocessedConvs))
	fmt.Println()
	fmt.Println("Existing documentation:")
	fmt.Printf("  - %d ADRs\n", len(existingADRs))
	fmt.Printf("  - %d Concepts\n", len(existingConcepts))
	fmt.Printf("  - %d Domain Docs\n", len(existingDomainDocs))
	fmt.Println()
	fmt.Println("Documents will be saved to:")
	fmt.Printf("  - ADRs: %s\n", adrsDir)
	fmt.Printf("  - Concepts: %s\n", conceptsDir)
	fmt.Printf("  - Domain: %s\n", domainDir)
	fmt.Println()

	// Run interactive Claude session
	ctx := context.Background()
	cli := claude.NewCLI()

	_, sessionErr := cli.SessionWithCapture(ctx, systemPrompt)

	fmt.Println()
	fmt.Println("Harvest session ended.")

	// Note: We don't save this conversation to avoid infinite loops
	// (harvest sessions reviewing harvest sessions)

	if sessionErr != nil {
		return fmt.Errorf("claude session failed: %w", sessionErr)
	}

	return nil
}

// buildHarvestConversationsSummary creates a detailed summary of unprocessed conversations
// Includes full transcript for comprehensive signal detection across all types
func buildHarvestConversationsSummary(convs []*domain.Conversation) string {
	if len(convs) == 0 {
		return "(No unprocessed conversations found)"
	}

	// Sort by timestamp (newest first)
	sort.Slice(convs, func(i, j int) bool {
		return convs[i].Timestamp.After(convs[j].Timestamp)
	})

	var sb strings.Builder
	for _, conv := range convs {
		sb.WriteString(fmt.Sprintf("### %s\n", conv.ID))
		sb.WriteString(fmt.Sprintf("**Date:** %s\n", conv.Timestamp.Format("2006-01-02 15:04")))
		sb.WriteString(fmt.Sprintf("**Branch:** %s\n", conv.Branch))

		if len(conv.CRsCreated) > 0 {
			crIDs := make([]string, len(conv.CRsCreated))
			for i, cr := range conv.CRsCreated {
				crIDs[i] = cr.CRID
			}
			sb.WriteString(fmt.Sprintf("**CRs Created:** %s\n", strings.Join(crIDs, ", ")))
		}

		if len(conv.Commits) > 0 {
			abbrevCommits := make([]string, len(conv.Commits))
			for i, sha := range conv.Commits {
				if len(sha) >= 8 {
					abbrevCommits[i] = sha[:8]
				} else {
					abbrevCommits[i] = sha
				}
			}
			sb.WriteString(fmt.Sprintf("**Commits:** %s\n", strings.Join(abbrevCommits, ", ")))
		}

		// For harvest, include more of the transcript for comprehensive analysis
		// But cap at reasonable size to avoid context overflow
		transcript := strings.TrimSpace(conv.Transcript)
		if len(transcript) > 2000 {
			transcript = transcript[:2000] + "\n... [transcript truncated for length]"
		}
		if transcript != "" {
			sb.WriteString(fmt.Sprintf("**Transcript:**\n```\n%s\n```\n", transcript))
		} else {
			sb.WriteString("**Transcript:** (empty)\n")
		}
		sb.WriteString("\n")
	}

	return sb.String()
}

// buildHarvestADRsSummary creates a summary of existing ADRs for duplicate detection
func buildHarvestADRsSummary(adrs []*domain.ADR) string {
	if len(adrs) == 0 {
		return "(No existing ADRs)"
	}

	var sb strings.Builder
	for _, adr := range adrs {
		sb.WriteString(fmt.Sprintf("- **%s**: %s (%s)\n", adr.ID, adr.Title, adr.Status))
		// Include brief context for better duplicate detection
		ctx := strings.TrimSpace(adr.Context)
		if len(ctx) > 100 {
			ctx = ctx[:100] + "..."
		}
		if ctx != "" {
			sb.WriteString(fmt.Sprintf("  Context: %s\n", ctx))
		}
	}

	return sb.String()
}

// buildHarvestConceptsSummary creates a summary of existing concepts for duplicate detection
func buildHarvestConceptsSummary(concepts []*domain.ConceptDoc) string {
	if len(concepts) == 0 {
		return "(No existing Concepts)"
	}

	var sb strings.Builder
	for _, concept := range concepts {
		sb.WriteString(fmt.Sprintf("- **%s**: %s (%s)\n", concept.ID, concept.Title, concept.Status))
		if len(concept.RelatedADRs) > 0 {
			sb.WriteString(fmt.Sprintf("  Related ADRs: %s\n", strings.Join(concept.RelatedADRs, ", ")))
		}
	}

	return sb.String()
}

// buildHarvestDomainDocsSummary creates a summary of existing domain docs for duplicate detection
func buildHarvestDomainDocsSummary(docs []*domain.DomainDoc) string {
	if len(docs) == 0 {
		return "(No existing Domain Docs)"
	}

	var sb strings.Builder
	for _, doc := range docs {
		sb.WriteString(fmt.Sprintf("- **%s**: %s\n", doc.ID, doc.Title))
		if len(doc.Terms) > 0 {
			termNames := make([]string, len(doc.Terms))
			for i, t := range doc.Terms {
				termNames[i] = t.Term
			}
			sb.WriteString(fmt.Sprintf("  Terms: %s\n", strings.Join(termNames, ", ")))
		}
		if len(doc.Entities) > 0 {
			entityNames := make([]string, len(doc.Entities))
			for i, e := range doc.Entities {
				entityNames[i] = e.Name
			}
			sb.WriteString(fmt.Sprintf("  Entities: %s\n", strings.Join(entityNames, ", ")))
		}
	}

	return sb.String()
}
