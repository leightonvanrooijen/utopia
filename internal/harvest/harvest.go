// Package harvest implements the unified harvest session behind
// `utopia harvest`: loading unprocessed conversations, detecting README
// documentation signals, composing the qualification-based system prompt,
// and running the interactive Claude session that proposes documentation
// candidates (ADRs, Concepts, Domain docs).
package harvest

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/leightonvanrooijen/utopia/internal"
	"github.com/leightonvanrooijen/utopia/internal/cli/ui"
	"github.com/leightonvanrooijen/utopia/internal/domain"
)

// Options configures a harvest session.
type Options struct {
	// ProjectDir is the absolute path of the project (used to read README.md)
	ProjectDir string
	// UtopiaDir is the .utopia directory where documents are saved
	UtopiaDir string
	// Model is an optional Claude model override
	Model string
	// Effort is an optional reasoning effort override for the session. The empty
	// string leaves the claude CLI on its own default.
	Effort string
	// Auth selects the credential the harvest session authenticates with.
	// The empty mode inherits the ambient environment.
	Auth domain.AuthMode
}

// Result represents the outcome of a harvest session.
type Result struct {
	// UnprocessedConversations is the number of unprocessed conversations found.
	UnprocessedConversations int
	// UnprocessedRuns is the number of unprocessed execution runs found.
	// A session runs when either source has something unprocessed; both being
	// zero means no session was run, and callers inspect that to frame the
	// outcome.
	UnprocessedRuns int
	// RewrittenChangeRequests is the number of change requests produced by a
	// scoping escalation that the session was shown. They are context for the
	// session rather than a source it processes, so they do not on their own
	// cause a session to run.
	RewrittenChangeRequests int
}

// harvestSystemPrompt guides Claude through unified signal detection and doc creation
// Use fmt.Sprintf to inject: conversationsSummary, existingADRsSummary, existingConceptsSummary,
// existingDomainDocsSummary, readmeSignalsSummary, rewritesSummary, adrsDir, conceptsDir,
// domainDir, conversationsDir, runsDir, nextADRID
const harvestSystemPrompt = `You are a Harvest Claude - an AI assistant that applies qualification tests to identify documentation candidates from conversation history.

## Your Role
Review persisted harvest sources - execution runs and conversations - and apply QUALIFICATION TESTS to identify candidates for ALL documentation types in a SINGLE PASS:
- **ADR candidates**: Architectural decisions that pass category + reversal cost tests
- **Concept candidates**: Educational content that passes orientation + independence tests
- **Domain candidates**: Terms that pass domain specificity + precision + consistency tests
- **README signals**: Spec features that qualify for README documentation (pre-computed and shown below)

This unified approach is more efficient than running separate /adr, /concept, /domain commands and allows candidates to be cross-referenced.

**Note on README signals:** Unlike other candidates which are detected from conversations, README signals are pre-computed by scanning specs against the current README. You simply include them in your results if they appear in the "README Documentation Signals" section below.

## Source Types

Harvest sources are classified by how close they sit to actual system state:

**Execution Runs** (source type: execution) - SYSTEM-TRUTH:
- These are transcripts of a work item actually being built, captured while the loop ran
- They record decisions made AT the code, which appear nowhere else once the loop moves on
- PRIORITIZE these for ADR candidates at least as highly as system-truth conversations
- ADR candidates from execution runs should generally be HIGH confidence
- A failed run is still system-truth: it records what was really attempted and why it did not hold
- Scan them for Domain candidates too - a run is where vocabulary gets coined. The name a
  run picks for a new type, status, or stored artifact is the name in the codebase from
  that commit on, whether or not anyone ever wrote down what it means
- Each run below lists its CR, its work item, and its **Originating Conversation** -
  the conversation whose CR put the work item there. Those are the cross-references
  any candidate surfaced from the run must carry
- Each run also lists its **Run File**: the path, relative to the runs directory named
  under "Marking Sources Processed" below, of the file to mark processed once the
  harvest is done

**System-Truth Conversations** (has CR + execution completed):
- These represent ACTUAL system state - decisions were implemented and verified
- PRIORITIZE these for ADR candidates (higher confidence - the decision was actually made)
- ADR candidates from system-truth conversations should generally be HIGH confidence
- These conversations show what was decided before building; the matching run shows what was built

**Exploratory Conversations** (no CR):
- These are informational/research discussions without implementation
- Still valuable for Concept candidates (educational content worth documenting)
- Still valuable for Domain candidates (terms that need canonical definition)
- ADR candidates should be MEDIUM or LOW confidence (decision discussed but not implemented)
- May represent rejected approaches or future considerations

When an execution run and a conversation cover the same decision, prefer the run's
account of WHAT was built and the conversation's account of WHY, and cross-reference
them via the run's CR ID and work item ID.

## Unprocessed Sources
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

### Rewritten Change Requests (scoping escalations)

A rewritten change request is what a scoping escalation produced: an executor
misread a specification repeatedly, and the scoper rewrote the specification
rather than the code. Each one below records the change request it supersedes and
the validator diagnoses that motivated the rewrite.

Treat these as HIGH-priority signals, and read the diagnoses as the signal rather
than the rewrite itself:

- A diagnosis saying a term was read to mean something other than what was meant
  is a **Domain candidate** - the term has no canonical definition, which is why
  two readers took it two ways
- A diagnosis saying an implied constraint was not honoured is usually an **ADR
  candidate** - the constraint is a decision nobody wrote down
- A rewrite that mostly adds explanation rather than changing what is being asked
  for is a **Concept candidate** - the reasoning was load-bearing and undocumented

Cross-reference any candidate surfaced here to the superseded change request id
and the work item named in the rewrite.

%s

## The Journey

### PHASE 1: QUALIFICATION-BASED DETECTION
Analyze ALL unprocessed sources - execution runs and conversations alike - and apply qualification tests. Be STRICT - only surface candidates that pass all tests.

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

**Applying this test to implementation decisions in execution runs:**
Runs are where implementation decisions get made - decisions taken at the code, with
the real constraints visible. Apply the SAME test, unchanged. The Category Test and
Reversal Cost Test decide; where the decision was made does not.
- "Implementation detail" disqualifies HOW something was coded, not WHAT was chosen
  while coding. A run that settles a storage layout, a directory structure, a
  published signature, or a new dependency has made an architectural decision;
  a run that picks a variable name or a loop shape has not
- A decision the run tried and abandoned is a temporary experiment - disqualified,
  unless the run abandoned it in favour of something it then settled on (in which
  case the settled choice is the candidate and the abandoned one is its context)
- A decision the run made that CONTRADICTS its CR or conversation is a strong
  candidate: what was built beat what was planned, and only the run records it
- Cross-reference every candidate surfaced from a run to BOTH its CR and its
  originating conversation, and record both when creating the document

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

**Applying this test to terms introduced in execution runs:**
Runs are where vocabulary gets coined - the name is chosen at the code, often with no
conversation behind it, and it is load-bearing the moment it is committed. Apply the
SAME test, unchanged. The ambiguity litmus test decides; where the term was coined
does not.
- A run-coined term passes the Code Alignment Test by construction - it is already in
  the code. That is one test of five, not a free pass: the term must still be specific
  to our domain and ambiguous without a definition. Do NOT promote every identifier a
  run introduces
- "Implementation detail" disqualifies internal naming that stands for no domain concept -
  a helper function, a loop variable, a test fixture. A run that names a new type, a
  status value, a persisted artifact, or the directory it lives in has named a concept
- A run that uses a DIFFERENT word for an already-documented term has introduced an
  alias, not a new term - suggest UPDATE of the existing bounded context to record the
  alias, and name the term that drifted. The Domain Docs section above lists each term's
  recorded aliases: a word already listed there is documented drift, not a candidate
- A term the run coined and then abandoned is a working title - disqualified
- Cross-reference every term surfaced from a run to its CR and its originating
  conversation, and record the run when creating or updating the document

For EACH qualified candidate, capture:
- **ID**: Unique identifier (adr-1, concept-1, domain-1, etc.)
- **Type**: adr, concept, or domain
- **Title**: Brief description (1 line)
- **Confidence**: high, medium, or low
  - For ADRs:
    - HIGH: Passes all qualification tests AND from a system-truth source - an execution run or an executed conversation (the decision was actually implemented)
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
    - For a term coined in an execution run, code presence is a given and carries no
      confidence signal - rate it on how strongly the ambiguity test reads. A run-coined
      term whose ambiguity test is borderline is MEDIUM, not HIGH
- **For ADRs only**:
  - **Category**: Which AWS category (structure, nfr, dependencies, interfaces, construction)
  - **Reversal Cost**: Brief explanation of why this is costly to reverse
- **Source Type**: execution, system-truth, or exploratory (shown in source)
- **Location**: Source conversation or run ID + message range (e.g., "lines 15-30", "early", "mid", "late")
- **Origin** (candidates from execution runs only): the run's CR ID and its originating
  conversation ID, taken from the run's **CR** and **Originating Conversation** lines
- **Related Candidates**: IDs of related candidates (e.g., adr-1 may link to concept-1)
- **Potential Duplicate / Update**: If similar to existing doc, note which one AND whether this should UPDATE that doc instead of creating new

### PHASE 2: PRESENT FINDINGS
Present a STRUCTURED SUMMARY of all qualified candidates, grouped by type.

**Required format:**
` + "```" + `
## Harvest Results

**Summary: X ADR candidates, Y Concept candidates, Z Domain candidates, W README signals**
**Sources: R execution runs, N system-truth conversations, M exploratory conversations**

### ADR Candidates (Qualified)
| ID | Confidence | Title | Category | Reversal Cost | Source | Source Type | Origin (CR / Conversation) |
|----|------------|-------|----------|---------------|--------|-------------|----------------------------|
| adr-1 | HIGH | Group runs by CR on disk | structure | Path layout is baked into harvest and tooling | cr-003-phase-0-persist-runs | execution | cr-003 / cr-session-20260217 |
| adr-2 | HIGH | Use YAML for storage | dependencies | Data migration, tooling changes | cr-session-20260217 | system-truth | - |
| adr-3 | MEDIUM | Use Cobra for CLI | construction | All command handlers depend on it | cr-session-20260216 | exploratory | - |

**Origin** is required for every candidate whose source type is execution, and is "-" otherwise.

**Note:** Only decisions that pass all qualification tests appear here. Disqualified items are not shown.

### Concept Candidates (Qualified)
| ID | Confidence | Title | Litmus Test | Source | Source Type | Related |
|----|------------|-------|-------------|--------|-------------|---------|
| concept-1 | HIGH | YAML vs JSON trade-offs | ✓ Useful to others | cr-session-20260217 | system-truth | adr-2 |

**Note:** Only content that passes all qualification tests (Orientation, Educational Value, Independence) and the litmus test appears here. Disqualified items are not shown.

### Domain Candidates (Qualified)
| ID | Confidence | Term | Code Usage | Ambiguity Test | Source | Source Type | Origin (CR / Conversation) | Related |
|----|------------|------|------------|----------------|--------|-------------|----------------------------|---------|
| domain-1 | HIGH | "WorkItem" | WorkItem struct, workitem.go | ✓ Would confuse without def | cr-session-20260217 | system-truth | - | - |
| domain-2 | HIGH | "unprocessed" | ConversationUnprocessed const | ✓ Status vs adjective | cr-session-20260217 | system-truth | - | domain-1 |
| domain-3 | HIGH | "execution run" | ExecutionRun struct, runs/ | ✓ Run vs conversation vs iteration | cr-003-phase-0-persist-runs | execution | cr-003 / cr-session-20260217 | domain-1 |
| domain-4 | MEDIUM | "bounded context" | Not in code yet (should be) | ✓ DDD term with local meaning | cr-session-20260216 | exploratory | - | - |

**Note:** Only terms that pass all qualification tests (Domain Specificity, Precision, Consistency, Code Alignment) and the ambiguity litmus test appear here. Disqualified items are not shown.

**Origin** follows the same rule as the ADR table: required for every term whose source
type is execution, "-" otherwise. A term coined at the code is traceable only through
its run.

### README Documentation Signals
These are spec features that qualify for README documentation but aren't yet documented.
See the "README Documentation Signals" section in Existing Documentation above for pre-computed candidates.

| ID | Confidence | Title | Category | Suggested Section | Spec | Feature |
|----|------------|-------|----------|-------------------|------|---------|
| readme-1 | HIGH | utopia cleanup command | command | Quick Start / The Loop | adoption | cleanup-command |

**Note:** README signals are detected by scanning specs against the current README. Only features that qualify (new command, new artifact type, workflow change, new directory) AND are not already documented appear here.

### Cross-References
- adr-2 ↔ concept-1: The ADR records the YAML decision; the Concept explains the trade-off reasoning
- adr-1 ↔ cr-session-20260217: The run shows the layout that was built; the conversation that created its CR shows why
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

If no qualified candidates found: "No qualified documentation candidates found. All content either failed qualification tests or is already documented. Sources can be marked as processed."

### PHASE 3: USER SELECTION
Ask the user which documents they want to create or update:
- "all" - Create/update all identified documents
- "ADR 1, Concept 1" - Create/update specific numbered items
- "skip" - Mark all reviewed sources as processed without creating docs
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
- Mark ALL reviewed sources as processed - execution runs and conversations alike
- Skipped candidates remain discoverable in future harvests of new sources

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
source_runs:
  - "[cr-id]/[workitem-id]"   # required when the decision came from an execution run
` + "```" + `

For a decision surfaced from an execution run, fill in BOTH: source_runs with the run
that built it, and source_conversations with the run's originating conversation (omit
source_conversations only when the run has none). The run records WHAT was built; the
conversation records WHY it was asked for, and the ADR needs both to stay traceable.

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
source_runs:
  - "[cr-id]/[workitem-id]"   # required when the term was coined in an execution run
` + "```" + `

For a term surfaced from an execution run, fill in source_runs with the run that coined
it, and source_conversations with the run's originating conversation. Unlike an ADR, a
term often has no conversation behind it at all - the name was chosen at the code and
never discussed. In that case record source_runs alone rather than reaching for a
conversation that does not define the term.

## Marking Sources Processed

After harvest completion (whether or not docs were created), every source reviewed in
this session is marked processed, so the next harvest only sees new material.

The directories below are the ones this harvest actually read its sources from. Use
them as given - do NOT rebuild a path from a working directory, which may not be the
project the sources came from.

Conversations:
1. Read each conversation file from %s/{id}.yaml
2. Change status from "unprocessed" to "processed"
3. Write the updated file back

Execution runs - the same rule, at the run's own path:
1. Read each run file from %s/{run-file}, where {run-file} is the **Run File** line
   shown with the run above (cr-id/workitem-id.yaml)
2. Set status to "processed" (a run with no status field yet is unprocessed - add it)
3. Write the updated file back, leaving every other field untouched

## Critical Guidelines
- Ask ONE question at a time
- Be STRICT about qualification - only present candidates that pass ALL tests
- **ALWAYS scan existing docs BEFORE suggesting new ones - prefer UPDATE over CREATE**
- When suggesting an update, show: "UPDATE existing {id}" with the file path
- The next ADR ID is: %s
- Cross-reference related candidates explicitly
- A candidate from an execution run is cross-referenced to its CR AND its originating conversation
- Created docs SHOULD reference each other when relevant
- ONLY mark conversations and execution runs processed after user completes or exits
- It's okay if a source has no qualified candidates - mark it processed anyway

Start by presenting a summary of ALL qualified candidates found across ALL unprocessed sources, grouped by type with counts.`

// Run executes the harvest session: it loads unprocessed conversations and
// execution runs alongside existing documentation, detects README signals,
// composes the system prompt, prints the session summary, and runs the
// interactive Claude session.
// It returns early (with a nil error) when neither source has anything
// unprocessed; callers inspect the result to frame that outcome.
func Run(ctx context.Context, store *internal.YAMLStore, opts Options) (*Result, error) {
	// Load unprocessed conversations
	unprocessedConvs, err := store.ListUnprocessedConversations()
	if err != nil {
		return nil, fmt.Errorf("failed to list unprocessed conversations: %w", err)
	}

	// Execution runs are the other half of the harvest input: what was actually
	// built, as opposed to what was discussed. A missing runs directory is not
	// an error - projects that predate run persistence simply have none.
	// Unprocessed runs are worth a session on their own: a work item can be
	// executed long after its conversation was harvested.
	runs, err := store.ListUnprocessedExecutionRuns()
	if err != nil {
		return nil, fmt.Errorf("failed to list unprocessed execution runs: %w", err)
	}

	result := &Result{UnprocessedConversations: len(unprocessedConvs), UnprocessedRuns: len(runs)}
	if len(unprocessedConvs) == 0 && len(runs) == 0 {
		return result, nil
	}

	// Every conversation is indexed, not only the unprocessed ones: the
	// conversation a run originated from has usually been harvested already,
	// and it is still the record of why the work was asked for.
	allConvs, err := store.ListConversations()
	if err != nil {
		return nil, fmt.Errorf("failed to list conversations: %w", err)
	}
	runOrigins := indexRunOrigins(allConvs)

	// Ensure all directories exist
	adrsDir := store.ADRsDir()
	conceptsDir := store.ConceptsDir()
	domainDir := store.DomainDir()

	for _, dir := range []string{adrsDir, conceptsDir, domainDir} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return nil, fmt.Errorf("failed to create directory %s: %w", dir, err)
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
	convsSummary := buildHarvestSourcesSummary(unprocessedConvs, runs, runOrigins)
	adrsSummary := buildHarvestADRsSummary(existingADRs)
	conceptsSummary := buildHarvestConceptsSummary(existingConcepts)
	domainDocsSummary := buildHarvestDomainDocsSummary(existingDomainDocs)
	nextADRID := getNextADRID(existingADRs)

	// Build README signals summary
	readmeSignalsSummary := buildREADMESignalsSummary(opts.ProjectDir, store)

	// Rewritten change requests are the durable output of runs that produced no
	// working code. They are context rather than a source to be marked processed:
	// a rewrite lives until its change request merges, and the run that produced
	// it is already tracked as a harvest source.
	rewrites, err := store.ListRewrittenChangeRequests()
	if err != nil {
		return nil, fmt.Errorf("failed to list rewritten change requests: %w", err)
	}
	rewritesSummary := buildRewrittenCRsSummary(rewrites)
	result.RewrittenChangeRequests = len(rewrites)

	// Inject all summaries into the system prompt. The source directories are
	// injected for the same reason the artifact directories are: the harvest
	// session inherits whatever working directory utopia was invoked from, which
	// is not necessarily the project --project resolved to, so the paths it marks
	// processed have to be the resolved ones.
	systemPrompt := fmt.Sprintf(harvestSystemPrompt,
		convsSummary,
		adrsSummary,
		conceptsSummary,
		domainDocsSummary,
		readmeSignalsSummary,
		rewritesSummary,
		adrsDir,
		conceptsDir,
		domainDir,
		store.ConversationsDir(),
		store.RunsDir(),
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
	readmeSignalCount := countREADMESignals(opts.ProjectDir, store)

	// Display harvest summary
	fmt.Println("Starting unified harvest session...")
	fmt.Printf("Found %d unprocessed conversations:\n", len(unprocessedConvs))
	fmt.Printf("  "+ui.Bullet+" %d system-truth (has CR + executed)\n", systemTruthCount)
	fmt.Printf("  "+ui.Bullet+" %d exploratory (no CR)\n", exploratoryCount)
	fmt.Printf("Found %d unprocessed execution runs (system-truth - what was built)\n", len(runs))
	fmt.Println()
	fmt.Println("Existing documentation:")
	fmt.Printf("  "+ui.Bullet+" %d ADRs\n", len(existingADRs))
	fmt.Printf("  "+ui.Bullet+" %d Concepts\n", len(existingConcepts))
	fmt.Printf("  "+ui.Bullet+" %d Domain Docs\n", len(existingDomainDocs))
	fmt.Println()
	fmt.Println("Documentation signals:")
	fmt.Printf("  "+ui.Bullet+" %s README signal%s\n", formatSignalCount(readmeSignalCount), pluralize(readmeSignalCount))
	fmt.Printf("  "+ui.Bullet+" %d rewritten change request%s (scoping escalations)\n", len(rewrites), pluralize(len(rewrites)))
	fmt.Println()
	fmt.Println("Documents will be saved to:")
	fmt.Printf("  "+ui.Bullet+" ADRs: %s\n", adrsDir)
	fmt.Printf("  "+ui.Bullet+" Concepts: %s\n", conceptsDir)
	fmt.Printf("  "+ui.Bullet+" Domain: %s\n", domainDir)
	fmt.Println()

	// Run interactive Claude session
	cli := internal.NewCLI().WithAuth(opts.Auth, opts.UtopiaDir)
	if opts.Model != "" {
		cli = cli.WithModel(opts.Model)
	}
	if opts.Effort != "" {
		cli = cli.WithEffort(opts.Effort)
	}

	_, sessionErr := cli.SessionWithCapture(ctx, systemPrompt)

	fmt.Println()
	fmt.Println("Harvest session ended.")

	// Note: We don't save this conversation to avoid infinite loops
	// (harvest sessions reviewing harvest sessions)

	if sessionErr != nil {
		return nil, fmt.Errorf("claude session failed: %w", sessionErr)
	}

	return result, nil
}

// buildHarvestSourcesSummary creates a detailed summary of the unprocessed
// harvest sources: execution runs and conversations.
// Includes full transcript for comprehensive signal detection across all types.
//
// Sources are grouped by source type and ordered by how close each type sits to
// actual system state, because position in the prompt is itself a priority
// signal. Execution runs lead: they record what was actually built. System-truth
// conversations follow, then exploratory ones.
// runOrigins is passed through so each run can be shown next to the
// conversation it came from; see indexRunOrigins.
func buildHarvestSourcesSummary(convs []*domain.Conversation, runs []*domain.ExecutionRun, runOrigins map[string][]string) string {
	if len(convs) == 0 && len(runs) == 0 {
		return "(No unprocessed sources found)"
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
	sortedRuns := append([]*domain.ExecutionRun(nil), runs...)
	sort.Slice(sortedRuns, func(i, j int) bool {
		return sortedRuns[i].CompletedAt.After(sortedRuns[j].CompletedAt)
	})
	sort.Slice(systemTruth, func(i, j int) bool {
		return systemTruth[i].Timestamp.After(systemTruth[j].Timestamp)
	})
	sort.Slice(exploratory, func(i, j int) bool {
		return exploratory[i].Timestamp.After(exploratory[j].Timestamp)
	})

	var sb strings.Builder

	// Summary line showing counts by type
	sb.WriteString(fmt.Sprintf("**Total: %d sources (%d execution runs, %d system-truth conversations, %d exploratory conversations)**\n\n",
		len(convs)+len(runs), len(sortedRuns), len(systemTruth), len(exploratory)))

	// Execution runs first (system-truth - the record of what was built)
	if len(sortedRuns) > 0 {
		sb.WriteString("## Execution Runs (system-truth - what was built)\n")
		sb.WriteString("*These record decisions made while building - prioritize for ADR signals.*\n\n")
		for _, run := range sortedRuns {
			writeExecutionRunSummary(&sb, run, runOrigins[run.CRID])
		}
	}

	// System-truth conversations next (prioritized for ADR signals)
	if len(systemTruth) > 0 {
		sb.WriteString("## System-Truth Conversations (has CR + executed)\n")
		sb.WriteString("*These represent actual system state - prioritize for ADR signals.*\n\n")
		for _, conv := range systemTruth {
			writeConversationSummary(&sb, conv)
		}
	}

	// Exploratory conversations last (still valuable for concepts/domain)
	if len(exploratory) > 0 {
		sb.WriteString("## Exploratory Conversations (no CR)\n")
		sb.WriteString("*Informational only - still valuable for concept and domain signals.*\n\n")
		for _, conv := range exploratory {
			writeConversationSummary(&sb, conv)
		}
	}

	return sb.String()
}

// indexRunOrigins maps a CR ID to the IDs of the conversations that created it.
// A run knows only its CR and work item, but the decision that put the work
// there was made in a conversation; joining through CRsCreated is what lets a
// decision found in a run be attributed to both. Conversations already
// processed are indexed too - a run's origin is usually one of them.
func indexRunOrigins(convs []*domain.Conversation) map[string][]string {
	origins := make(map[string][]string)
	for _, conv := range convs {
		for _, cr := range conv.CRsCreated {
			origins[cr.CRID] = append(origins[cr.CRID], conv.ID)
		}
	}
	return origins
}

// writeExecutionRunSummary writes a single execution run's details to the
// builder. CR ID, work item ID, and the originating conversations are always
// written: they are what a decision surfaced from this run gets cross-
// referenced to.
func writeExecutionRunSummary(sb *strings.Builder, run *domain.ExecutionRun, originConvIDs []string) {
	sb.WriteString(fmt.Sprintf("### %s\n", run.WorkItemID))
	sb.WriteString(fmt.Sprintf("**Type:** %s\n", run.Type()))
	sb.WriteString(fmt.Sprintf("**CR:** %s\n", run.CRID))
	sb.WriteString(fmt.Sprintf("**Spec Ref:** %s\n", run.SpecRef))
	sb.WriteString(fmt.Sprintf("**Outcome:** %s (%d iteration%s)\n", run.Outcome, run.Iterations, pluralize(run.Iterations)))
	if !run.CompletedAt.IsZero() {
		sb.WriteString(fmt.Sprintf("**Completed:** %s\n", run.CompletedAt.Format("2006-01-02 15:04")))
	}
	sb.WriteString(fmt.Sprintf("**Originating Conversation:** %s\n", formatRunOrigins(originConvIDs)))
	// The run's own file, relative to the runs directory the prompt names. It is
	// written for every run because this is the file the harvest marks processed
	// when the session ends, and a run's file name is its work item ID nested
	// under its CR - not something derivable from the heading alone.
	sb.WriteString(fmt.Sprintf("**Run File:** %s\n", filepath.Join(run.CRID, run.WorkItemID+".yaml")))

	writeTranscript(sb, run.Transcript)
}

// formatRunOrigins renders the conversations a run traces back to. Saying so
// explicitly when there are none stops the model inventing a conversation ID
// to fill the cross-reference: a run whose CR was created outside a captured
// session genuinely has no originating conversation.
func formatRunOrigins(convIDs []string) string {
	if len(convIDs) == 0 {
		return "(none found - this run's CR has no captured conversation)"
	}
	return strings.Join(convIDs, ", ")
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

	writeTranscript(sb, conv.Transcript)
}

// writeTranscript appends a source's transcript block.
// For harvest, include more of the transcript for comprehensive analysis
// But cap at reasonable size to avoid context overflow
func writeTranscript(sb *strings.Builder, raw string) {
	transcript := strings.TrimSpace(raw)
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

// buildRewrittenCRsSummary lists the change requests produced by a scoping
// escalation, each with what it supersedes and the diagnoses behind it.
//
// The diagnoses are quoted in full rather than truncated: they are the signal.
// A diagnosis is capped at a couple of sentences where it is written, and it is
// the sentence naming the misread term that a Domain or ADR candidate comes from.
func buildRewrittenCRsSummary(rewrites []*domain.ChangeRequest) string {
	if len(rewrites) == 0 {
		return "(No rewritten change requests - no run has escalated its scoping)"
	}

	var sb strings.Builder
	for _, cr := range rewrites {
		sb.WriteString(fmt.Sprintf("- **%s**: %s\n", cr.ID, cr.Title))
		sb.WriteString(fmt.Sprintf("  Supersedes: %s\n", cr.Rewrite.Supersedes))
		if cr.Rewrite.WorkItem != "" {
			sb.WriteString(fmt.Sprintf("  Work item: %s\n", cr.Rewrite.WorkItem))
		}
		for _, d := range cr.Rewrite.Diagnoses {
			sb.WriteString(fmt.Sprintf("  Diagnosis: %s\n", strings.TrimSpace(d)))
		}
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
				// Recorded aliases are shown because a term coined in a run is
				// often just a different word for something already documented.
				// Without the aliases here, that drift reads as a new term and
				// gets proposed all over again.
				if len(t.Aliases) > 0 {
					termNames[i] += fmt.Sprintf(" (aliases: %s)", strings.Join(t.Aliases, ", "))
				}
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

// formatSignalCount returns a formatted count string (e.g., "1", "3", or "0")
func formatSignalCount(count int) string {
	return fmt.Sprintf("%d", count)
}

// pluralize returns "s" if count != 1, empty string otherwise
func pluralize(count int) string {
	if count == 1 {
		return ""
	}
	return "s"
}
