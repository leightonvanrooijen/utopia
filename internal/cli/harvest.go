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
	Short: "Qualification-based analysis of conversations for documentation candidates",
	Long: `Scan unprocessed conversations and apply qualification tests to identify documentation candidates.

The command will:
  1. Find unprocessed conversations from .utopia/conversations/
  2. Apply qualification tests to identify documentation candidates:
     - ADR candidates (architectural decisions that pass category + reversal cost tests)
     - Concept candidates (educational content that passes orientation + independence tests)
     - Domain candidates (terms that pass domain specificity + precision + consistency tests)
  3. Present qualified candidates grouped by type with confidence levels
  4. Cross-reference existing docs to avoid duplicates
  5. Let you select which docs to create (individual, multiple, or all)
  6. Flow context between creations (ADR created first is known when creating Concept)
  7. Allow created docs to reference each other
  8. Mark conversation as processed only after you complete or exit

Benefits over individual commands (/adr, /concept, /domain):
  - Single pass through conversations (efficiency)
  - Cross-type awareness (related candidates linked)
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
const harvestSystemPrompt = `You are a Harvest Claude - an AI assistant that applies qualification tests to identify documentation candidates from conversation history.

## Your Role
Review persisted conversations and apply QUALIFICATION TESTS to identify candidates for ALL documentation types in a SINGLE PASS:
- **ADR candidates**: Architectural decisions that pass category + reversal cost tests
- **Concept candidates**: Educational content that passes orientation + independence tests
- **Domain candidates**: Terms that pass domain specificity + precision + consistency tests
- **README signals**: Spec features that qualify for README documentation (pre-computed and shown below)

This unified approach is more efficient than running separate /adr, /concept, /domain commands and allows candidates to be cross-referenced.

**Note on README signals:** Unlike other candidates which are detected from conversations, README signals are pre-computed by scanning specs against the current README. You simply include them in your results if they appear in the "README Documentation Signals" section below.

## Conversation Types

Conversations are classified by type based on whether they produced executed Change Requests:

**System-Truth Conversations** (has CR + execution completed):
- These represent ACTUAL system state - decisions were implemented and verified
- PRIORITIZE these for ADR candidates (higher confidence - the decision was actually made)
- ADR candidates from system-truth conversations should generally be HIGH confidence
- These conversations show what was actually built, not just discussed

**Exploratory Conversations** (no CR):
- These are informational/research discussions without implementation
- Still valuable for Concept candidates (educational content worth documenting)
- Still valuable for Domain candidates (terms that need canonical definition)
- ADR candidates should be MEDIUM or LOW confidence (decision discussed but not implemented)
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

### README Documentation Signals
%s

## The Journey

### PHASE 1: QUALIFICATION-BASED DETECTION
Analyze ALL unprocessed conversations and apply qualification tests. Be STRICT - only surface candidates that pass all tests.

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

**Concept Qualification** (educational explanations):
Instead of looking for trade-off phrases, apply a QUALIFICATION TEST to determine if content is truly explanation-worthy:

1. **Orientation Test** - Is this understanding-oriented, not task-oriented?
   - Does it explain WHY rather than HOW?
   - Does it illuminate the problem space, not just solve a problem?
   - Would it help someone understand, not just complete a task?

2. **Educational Value Test** - Does this have lasting educational value?
   - Would this benefit someone onboarding or learning?
   - Will this still matter in 6 months?
   - Does it answer questions people repeatedly ask?

3. **Independence Test** - Is this independent of a specific binding decision?
   - Are multiple valid approaches being explained (not just our choice)?
   - Would this help someone at a DIFFERENT company facing a similar choice?
   - Does it explain the decision SPACE, not just record a decision?

4. **Disqualification Checks** - Reject if ANY of these apply:
   - **Records our binding decision** - "We decided to use X" (that's an ADR)
   - **Project-specific only** - Only useful in our specific context (lacks generalizable insight)
   - **Term/entity definition** - "X is defined as..." (that's Domain knowledge)
   - **Step-by-step instructions** - Tutorial or how-to content
   - **Technical reference** - API docs, config references
   - **Ephemeral discussion** - Won't matter in 6 months

**Litmus Test:** "Would this be useful to someone at a different company?"
- YES = Concept candidate
- NO = Probably an ADR or Domain doc

**Only suggest Concept creation when:**
- Content passes all three qualification tests (Orientation, Educational Value, Independence), AND
- None of the disqualification criteria apply, AND
- The litmus test passes (useful beyond this project)

**Domain Qualification** (Ubiquitous Language):
Instead of looking for definition phrases, apply a QUALIFICATION TEST based on Domain-Driven Design's Ubiquitous Language concept to determine if a term truly needs canonical documentation:

1. **Domain Specificity Test** - Is this term specific to our project's domain?
   - Does this term have meaning unique to THIS project, not general programming?
   - Would a developer from another project understand it without explanation?
   - Is this OUR vocabulary, not industry-standard terminology?

2. **Precision Test** - Could this term be misunderstood without definition?
   - Does the term have a precise meaning that differs from common usage?
   - Are there multiple interpretations that could cause confusion?
   - Would different team members interpret it differently?

3. **Consistency Test** - Should this term be used consistently?
   - Should this exact term appear in code (class names, methods, variables)?
   - Should this exact term be used in docs and communication?
   - Would domain experts recognize and validate this term?

4. **Code Alignment Test** - Does/should this term appear in code?
   - Is this term already used in class names, method names, or variables?
   - If not, SHOULD it be? (Indicates code/domain misalignment to fix)
   - Does the code use synonyms where this canonical term should appear?

5. **Disqualification Checks** - Reject if ANY of these apply:
   - **General programming term** - "function", "class", "API", "endpoint" (standard vocabulary)
   - **Standard industry term** - Used in its standard way without project-specific meaning
   - **Temporary/experimental** - Prototype names, working titles, placeholder terms
   - **Implementation detail** - Internal naming not representing a domain concept
   - **Already externally documented** - Term defined in external docs we reference
   - **One-off explanation** - Not canonical vocabulary, just explaining something once

**Litmus Test:** "Would ambiguity arise without this canonical definition?"
- YES = Document this term
- NO = Don't clutter the domain vocabulary

**Only suggest Domain doc creation when:**
- The term passes Domain Specificity (specific to our domain), AND
- The term passes Precision Test (could be misunderstood), AND
- The term passes Consistency Test (should be used consistently), AND
- The term passes Code Alignment Test (appears or should appear in code), AND
- None of the disqualification criteria apply, AND
- The litmus test passes (ambiguity would arise without definition)

For EACH qualified candidate, capture:
- **ID**: Unique identifier (adr-1, concept-1, domain-1, etc.)
- **Type**: adr, concept, or domain
- **Title**: Brief description (1 line)
- **Confidence**: high, medium, or low
  - For ADRs:
    - HIGH: Passes all qualification tests AND from system-truth conversation (decision was implemented)
    - MEDIUM: Passes qualification tests but from exploratory conversation OR reversal cost is moderate
    - LOW: Borderline qualification (may need user confirmation)
  - For Concepts:
    - HIGH: Passes all qualification tests AND litmus test clearly passes (useful to someone at different company)
    - MEDIUM: Passes qualification tests but litmus test is borderline (generalizable but narrowly)
    - LOW: Borderline qualification (may need user confirmation on educational value)
  - For Domain:
    - HIGH: Passes all qualification tests AND term already appears in code (strong code alignment)
    - MEDIUM: Passes qualification tests but term not yet in code (should be added) OR ambiguity test is borderline
    - LOW: Borderline qualification (may need user confirmation on domain specificity)
- **For ADRs only**:
  - **Category**: Which AWS category (structure, nfr, dependencies, interfaces, construction)
  - **Reversal Cost**: Brief explanation of why this is costly to reverse
- **Conversation Type**: system-truth or exploratory (shown in source)
- **Location**: Source conversation ID + message range (e.g., "lines 15-30", "early", "mid", "late")
- **Related Candidates**: IDs of related candidates (e.g., adr-1 may link to concept-1)
- **Potential Duplicate / Update**: If similar to existing doc, note which one AND whether this should UPDATE that doc instead of creating new

### PHASE 2: PRESENT FINDINGS
Present a STRUCTURED SUMMARY of all qualified candidates, grouped by type.

**Required format:**
` + "```" + `
## Harvest Results

**Summary: X ADR candidates, Y Concept candidates, Z Domain candidates, W README signals**
**Conversations: N system-truth, M exploratory**

### ADR Candidates (Qualified)
| ID | Confidence | Title | Category | Reversal Cost | Source | Conv Type |
|----|------------|-------|----------|---------------|--------|-----------|
| adr-1 | HIGH | Use YAML for storage | dependencies | Data migration, tooling changes | cr-session-20260217 | system-truth |
| adr-2 | MEDIUM | Use Cobra for CLI | construction | All command handlers depend on it | cr-session-20260216 | exploratory |

**Note:** Only decisions that pass all qualification tests appear here. Disqualified items are not shown.

### Concept Candidates (Qualified)
| ID | Confidence | Title | Litmus Test | Source | Conv Type | Related |
|----|------------|-------|-------------|--------|-----------|---------|
| concept-1 | HIGH | YAML vs JSON trade-offs | ✓ Useful to others | cr-session-20260217 | system-truth | adr-1 |

**Note:** Only content that passes all qualification tests (Orientation, Educational Value, Independence) and the litmus test appears here. Disqualified items are not shown.

### Domain Candidates (Qualified)
| ID | Confidence | Term | Code Usage | Ambiguity Test | Source | Conv Type | Related |
|----|------------|------|------------|----------------|--------|-----------|---------|
| domain-1 | HIGH | "WorkItem" | WorkItem struct, workitem.go | ✓ Would confuse without def | cr-session-20260217 | system-truth | - |
| domain-2 | HIGH | "unprocessed" | ConversationUnprocessed const | ✓ Status vs adjective | cr-session-20260217 | system-truth | domain-1 |
| domain-3 | MEDIUM | "bounded context" | Not in code yet (should be) | ✓ DDD term with local meaning | cr-session-20260216 | exploratory | - |

**Note:** Only terms that pass all qualification tests (Domain Specificity, Precision, Consistency, Code Alignment) and the ambiguity litmus test appear here. Disqualified items are not shown.

### README Documentation Signals
These are spec features that qualify for README documentation but aren't yet documented.
See the "README Documentation Signals" section in Existing Documentation above for pre-computed candidates.

| ID | Confidence | Title | Category | Suggested Section | Spec | Feature |
|----|------------|-------|----------|-------------------|------|---------|
| readme-1 | HIGH | utopia cleanup command | command | Quick Start / The Loop | adoption | cleanup-command |

**Note:** README signals are detected by scanning specs against the current README. Only features that qualify (new command, new artifact type, workflow change, new directory) AND are not already documented appear here.

### Cross-References
- adr-1 ↔ concept-1: The ADR records the YAML decision; the Concept explains the trade-off reasoning
- domain-1 ↔ domain-2: The Conversation entity has an "unprocessed" status

### Potential Duplicates / Updates to Existing Docs
- adr-2: **UPDATE existing ADR-003** (CLI framework choice) - new context extends existing decision
- concept-1: **UPDATE existing yaml-vs-markdown-concepts** - additional trade-off discussion
- domain-2: **UPDATE existing adrs.yaml** - add new term "unprocessed" to existing bounded context

### Disqualified Items (Not Shown)
Items that failed qualification tests are not included in the tables above. Examples of disqualified content:
- General programming terms (function, class, API) - disqualified as standard vocabulary
- Implementation details that don't represent domain concepts
- Temporary workarounds or experiments
- Content already documented elsewhere
` + "```" + `

**Message Range Guidelines:**
- Use line numbers when transcript has clear structure: "lines 15-30"
- Use approximate positions otherwise: "early" (first third), "mid" (middle third), "late" (final third)
- Be as specific as possible - this helps users find the source discussion

**Cross-Reference Guidelines:**
- Link candidates that discuss the same topic from different angles
- An ADR decision often has a related Concept explaining WHY
- Domain candidates may cluster around a single entity/bounded context
- Note the relationship type in the Cross-References section

**Update vs Create Guidelines:**
- ALWAYS scan existing docs section above BEFORE suggesting a new document
- If a candidate adds context to an existing ADR → suggest "UPDATE existing ADR-XXX"
- If a candidate extends an existing concept → suggest "UPDATE existing concept-id"
- If a candidate adds terms/entities to an existing bounded context → suggest "UPDATE existing domain-id"
- Only suggest "CREATE new" when no related existing doc covers the topic
- In "Potential Duplicates / Updates" section, explicitly show: "UPDATE existing {doc-id}" with the file path

If no qualified candidates found: "No qualified documentation candidates found. All content either failed qualification tests or is already documented. Conversations can be marked as processed."

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
- Skipped candidates remain discoverable in future harvests of new conversations

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
- Be STRICT about qualification - only present candidates that pass ALL tests
- **ALWAYS scan existing docs BEFORE suggesting new ones - prefer UPDATE over CREATE**
- When suggesting an update, show: "UPDATE existing {id}" with the file path
- The next ADR ID is: %s
- Cross-reference related candidates explicitly
- Created docs SHOULD reference each other when relevant
- ONLY mark conversations processed after user completes or exits
- It's okay if a conversation has no qualified candidates - mark it processed anyway

Start by presenting a summary of ALL qualified candidates found across ALL unprocessed conversations, grouped by type with counts.`

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

	// Build README signals summary
	readmeSignalsSummary := buildREADMESignalsSummary(absPath, store)

	// Inject all summaries into the system prompt
	systemPrompt := fmt.Sprintf(harvestSystemPrompt,
		convsSummary,
		adrsSummary,
		conceptsSummary,
		domainDocsSummary,
		readmeSignalsSummary,
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

	// Count README signal candidates
	readmeSignalCount := countREADMESignals(absPath, store)

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
	if readmeSignalCount > 0 {
		fmt.Printf("README documentation signals: %d (spec features needing README updates)\n", readmeSignalCount)
		fmt.Println()
	}
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

// buildREADMESignalsSummary scans specs against the README to find documentation candidates
func buildREADMESignalsSummary(projectDir string, store *storage.YAMLStore) string {
	// Try to read README.md from project root
	readmePath := filepath.Join(projectDir, "README.md")
	readmeContent, err := os.ReadFile(readmePath)
	if err != nil {
		return "(Could not read README.md - README signals skipped)"
	}

	// Parse what's documented in README
	documented := ParseREADMEDocumented(string(readmeContent))

	// Load all specs
	specs, err := store.ListSpecs()
	if err != nil {
		return "(Could not load specs - README signals skipped)"
	}

	// Scan specs for README candidates
	candidates := ScanSpecsForREADMECandidates(specs, documented)

	return BuildREADMESignalsSummary(candidates)
}

// countREADMESignals returns the count of README signal candidates
func countREADMESignals(projectDir string, store *storage.YAMLStore) int {
	readmePath := filepath.Join(projectDir, "README.md")
	readmeContent, err := os.ReadFile(readmePath)
	if err != nil {
		return 0
	}

	documented := ParseREADMEDocumented(string(readmeContent))
	specs, err := store.ListSpecs()
	if err != nil {
		return 0
	}

	candidates := ScanSpecsForREADMECandidates(specs, documented)
	return len(candidates)
}
