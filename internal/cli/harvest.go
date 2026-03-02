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

## Conversation Types

Conversations are classified by type based on whether they produced executed Change Requests:

**System-Truth Conversations** (has CR + execution completed):
- These represent ACTUAL system state - decisions were implemented and verified
- PRIORITIZE these for ADR signals (higher confidence - the decision was actually made)
- ADR signals from system-truth conversations should generally be HIGH confidence
- These conversations show what was actually built, not just discussed

**Exploratory Conversations** (no CR):
- These are informational/research discussions without implementation
- Still valuable for Concept signals (trade-off discussions, "why we chose X")
- Still valuable for Domain signals (terminology definitions)
- ADR signals should be MEDIUM or LOW confidence (decision discussed but not implemented)
- May represent rejected approaches or future considerations

## Unprocessed Conversations
%s

## Existing Documentation

**CRITICAL: ALWAYS check existing docs before suggesting new ones. If a signal relates to an existing doc, suggest UPDATING that doc instead of creating a new one.**

### ADRs (check for related decisions)
%s

### Concepts (check for related topics)
%s

### Domain Docs (check for related bounded contexts)
%s

## The Journey

### PHASE 1: UNIFIED SIGNAL DETECTION
Analyze ALL unprocessed conversations for signals across all types. Be STRICT - only surface clear signals.

**ADR Qualification** (architectural decisions):
Instead of looking for decision phrases, apply a QUALIFICATION TEST to determine if something is truly architectural:

1. **Category Test** - Does the decision fall into one of these AWS architectural categories?
   - **structure**: Architectural patterns, layers, component organization (e.g., microservices, monolith, event-driven)
   - **nfr**: Non-functional requirements affecting architecture (e.g., security, high availability, fault tolerance, performance)
   - **dependencies**: Component coupling and external service choices (e.g., database selection, third-party integrations)
   - **interfaces**: APIs, published contracts, integration points (e.g., REST vs GraphQL, event schemas)
   - **construction**: Libraries, frameworks, tools, build processes (e.g., framework choice, CI/CD approach)

2. **Reversal Cost Test** - Is this decision costly to reverse?
   - Would changing this require significant rework?
   - Does it affect multiple components or teams?
   - Are there data migrations, API contracts, or external dependencies at stake?

3. **Disqualification Checks** - Reject if ANY of these apply:
   - **Temporary workarounds or experiments** - "For now we'll..." / "As a workaround..."
   - **Implementation details** - HOW something is coded, not WHAT is chosen
   - **Already documented** - Covered by existing ADRs, standards, or policies
   - **Localized decisions** - Only affects a single developer or component
   - **Configuration/deployment specifics** - Unless they constrain architecture

**Only suggest ADR creation when:**
- The decision fits at least one AWS category, AND
- The decision is costly to reverse, AND
- None of the disqualification criteria apply

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

For EACH signal found, capture:
- **ID**: Unique identifier (adr-1, concept-1, domain-1, etc.)
- **Type**: adr, concept, or domain
- **Title**: Brief description (1 line)
- **Confidence**: high, medium, or low
  - For ADRs:
    - HIGH: Passes all qualification tests AND from system-truth conversation (decision was implemented)
    - MEDIUM: Passes qualification tests but from exploratory conversation OR reversal cost is moderate
    - LOW: Borderline qualification (may need user confirmation)
  - For Concepts/Domain:
    - HIGH: Explicit signal language ("the trade-off is", "X is defined as")
    - MEDIUM: Implied signal (discussion of alternatives, clarification of terms)
    - LOW: Weak signal (might be relevant, needs confirmation)
- **For ADRs only**:
  - **Category**: Which AWS category (structure, nfr, dependencies, interfaces, construction)
  - **Reversal Cost**: Brief explanation of why this is costly to reverse
- **Conversation Type**: system-truth or exploratory (shown in source)
- **Location**: Source conversation ID + message range (e.g., "lines 15-30", "early", "mid", "late")
- **Related Signals**: IDs of related signals (e.g., adr-1 may link to concept-1)
- **Potential Duplicate / Update**: If similar to existing doc, note which one AND whether this should UPDATE that doc instead of creating new

### PHASE 2: PRESENT FINDINGS
Present a STRUCTURED SUMMARY of all signals found, grouped by type.

**Required format:**
` + "```" + `
## Harvest Results

**Summary: X qualified ADR candidates, Y Concept signals, Z Domain signals**
**Conversations: N system-truth, M exploratory**

### ADR Candidates (Qualified)
| ID | Confidence | Title | Category | Reversal Cost | Source | Conv Type |
|----|------------|-------|----------|---------------|--------|-----------|
| adr-1 | HIGH | Use YAML for storage | dependencies | Data migration, tooling changes | cr-session-20260217 | system-truth |
| adr-2 | MEDIUM | Use Cobra for CLI | construction | All command handlers depend on it | cr-session-20260216 | exploratory |

**Note:** Only decisions that pass all qualification tests appear here. Disqualified items are not shown.

### Concept Signals
| ID | Confidence | Title | Source | Conv Type | Message Range | Related |
|----|------------|-------|--------|-----------|---------------|---------|
| concept-1 | HIGH | YAML vs JSON trade-offs | cr-session-20260217 | system-truth | lines 50-75 | adr-1 |

### Domain Signals
| ID | Confidence | Title | Source | Conv Type | Message Range | Related |
|----|------------|-------|--------|-----------|---------------|---------|
| domain-1 | HIGH | "Conversation" entity definition | cr-session-20260217 | system-truth | early | - |
| domain-2 | HIGH | "unprocessed" status meaning | cr-session-20260217 | system-truth | lines 20-25 | domain-1 |
| domain-3 | MEDIUM | "Bounded context" term | cr-session-20260216 | exploratory | late | - |

### Cross-References
- adr-1 ↔ concept-1: The ADR records the YAML decision; the Concept explains the trade-off reasoning
- domain-1 ↔ domain-2: The Conversation entity has an "unprocessed" status

### Potential Duplicates / Updates to Existing Docs
- adr-2: **UPDATE existing ADR-003** (CLI framework choice) - new context extends existing decision
- concept-1: **UPDATE existing yaml-vs-markdown-concepts** - additional trade-off discussion
- domain-2: **UPDATE existing adrs.yaml** - add new term "unprocessed" to existing bounded context
` + "```" + `

**Message Range Guidelines:**
- Use line numbers when transcript has clear structure: "lines 15-30"
- Use approximate positions otherwise: "early" (first third), "mid" (middle third), "late" (final third)
- Be as specific as possible - this helps users find the source discussion

**Cross-Reference Guidelines:**
- Link signals that discuss the same topic from different angles
- An ADR decision often has a related Concept explaining WHY
- Domain signals may cluster around a single entity/bounded context
- Note the relationship type in the Cross-References section

**Update vs Create Guidelines:**
- ALWAYS scan existing docs section above BEFORE suggesting a new document
- If a signal adds context to an existing ADR → suggest "UPDATE existing ADR-XXX"
- If a signal extends an existing concept → suggest "UPDATE existing concept-id"
- If a signal adds terms/entities to an existing bounded context → suggest "UPDATE existing domain-id"
- Only suggest "CREATE new" when no related existing doc covers the topic
- In "Potential Duplicates / Updates" section, explicitly show: "UPDATE existing {doc-id}" with the file path

If no signals found: "No documentation signals found. Conversations can be marked as processed."

### PHASE 3: USER SELECTION
Ask the user which documents they want to create or update:
- "all" - Create/update all identified documents
- "ADR 1, Concept 1" - Create/update specific numbered items
- "skip" - Mark conversations as processed without creating docs
- Individual selection one at a time

**For updates**: Clearly state "This will UPDATE {existing-doc-id} at {file-path}"
**For creates**: Clearly state "This will CREATE new {doc-type} at {file-path}"

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
- **ALWAYS scan existing docs BEFORE suggesting new ones - prefer UPDATE over CREATE**
- When suggesting an update, show: "UPDATE existing {id}" with the file path
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

	// Count conversations by type
	var systemTruthCount, exploratoryCount int
	for _, conv := range unprocessedConvs {
		if conv.IsSystemTruth() {
			systemTruthCount++
		} else {
			exploratoryCount++
		}
	}

	// Display harvest summary
	fmt.Println("Starting unified harvest session...")
	fmt.Printf("Found %d unprocessed conversations:\n", len(unprocessedConvs))
	fmt.Printf("  - %d system-truth (has CR + executed)\n", systemTruthCount)
	fmt.Printf("  - %d exploratory (no CR)\n", exploratoryCount)
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
// Includes full transcript for comprehensive signal detection across all types.
// System-truth conversations (with executed CRs) are listed first as they represent actual state.
func buildHarvestConversationsSummary(convs []*domain.Conversation) string {
	if len(convs) == 0 {
		return "(No unprocessed conversations found)"
	}

	// Separate by type: system-truth first, then exploratory
	var systemTruth, exploratory []*domain.Conversation
	for _, conv := range convs {
		if conv.IsSystemTruth() {
			systemTruth = append(systemTruth, conv)
		} else {
			exploratory = append(exploratory, conv)
		}
	}

	// Sort each group by timestamp (newest first)
	sort.Slice(systemTruth, func(i, j int) bool {
		return systemTruth[i].Timestamp.After(systemTruth[j].Timestamp)
	})
	sort.Slice(exploratory, func(i, j int) bool {
		return exploratory[i].Timestamp.After(exploratory[j].Timestamp)
	})

	var sb strings.Builder

	// Summary line showing counts by type
	sb.WriteString(fmt.Sprintf("**Total: %d conversations (%d system-truth, %d exploratory)**\n\n",
		len(convs), len(systemTruth), len(exploratory)))

	// System-truth conversations first (prioritized for ADR signals)
	if len(systemTruth) > 0 {
		sb.WriteString("## System-Truth Conversations (has CR + executed)\n")
		sb.WriteString("*These represent actual system state - prioritize for ADR signals.*\n\n")
		for _, conv := range systemTruth {
			writeConversationSummary(&sb, conv)
		}
	}

	// Exploratory conversations second (still valuable for concepts/domain)
	if len(exploratory) > 0 {
		sb.WriteString("## Exploratory Conversations (no CR)\n")
		sb.WriteString("*Informational only - still valuable for concept and domain signals.*\n\n")
		for _, conv := range exploratory {
			writeConversationSummary(&sb, conv)
		}
	}

	return sb.String()
}

// writeConversationSummary writes a single conversation's details to the builder
func writeConversationSummary(sb *strings.Builder, conv *domain.Conversation) {
	sb.WriteString(fmt.Sprintf("### %s\n", conv.ID))
	sb.WriteString(fmt.Sprintf("**Type:** %s\n", conv.Type()))
	sb.WriteString(fmt.Sprintf("**Date:** %s\n", conv.Timestamp.Format("2006-01-02 15:04")))
	sb.WriteString(fmt.Sprintf("**Branch:** %s\n", conv.Branch))

	if len(conv.CRsCreated) > 0 {
		crIDs := make([]string, len(conv.CRsCreated))
		for i, cr := range conv.CRsCreated {
			crIDs[i] = cr.CRID
		}
		sb.WriteString(fmt.Sprintf("**CRs Created:** %s\n", strings.Join(crIDs, ", ")))
	}

	if len(conv.ExecutionLog) > 0 {
		sb.WriteString(fmt.Sprintf("**Executed WorkItems:** %d\n", len(conv.ExecutionLog)))
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

// getNextADRID determines the next sequential ADR ID
func getNextADRID(existingADRs []*domain.ADR) string {
	maxNum := 0
	for _, adr := range existingADRs {
		// Parse ADR-NNN format
		if strings.HasPrefix(adr.ID, "ADR-") {
			var num int
			_, err := fmt.Sscanf(adr.ID, "ADR-%d", &num)
			if err == nil && num > maxNum {
				maxNum = num
			}
		}
	}
	return fmt.Sprintf("ADR-%03d", maxNum+1)
}
