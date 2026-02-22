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

var domainCmd = &cobra.Command{
	Use:   "domain",
	Short: "Review conversations and create or update domain documentation",
	Long: `Scan persisted conversations for domain terminology and entity discussions,
guiding domain doc creation or updates.

The command will:
  1. Find unprocessed conversations from .utopia/conversations/
  2. Analyze each for domain signals (term definitions, entity discussions, relationships)
  3. Check existing domain docs in .utopia/domain/ for related bounded contexts
  4. Suggest updating existing docs or creating new ones
  5. Guide you through domain doc creation/update conversationally
  6. Mark conversations as processed after review

Domain docs are stored in .utopia/domain/ with one file per bounded context.`,
	RunE: runDomain,
}

func init() {
	rootCmd.AddCommand(domainCmd)
}

// domainSystemPrompt guides Claude through domain discovery and documentation
// Use fmt.Sprintf to inject: conversationsSummary, existingDomainDocsSummary, domainDir
const domainSystemPrompt = `You are a Domain Discovery Claude - an AI assistant that helps identify and document domain terminology from conversation history.

## Your Role
Review persisted conversations to identify domain knowledge that should be documented. Guide the user through creating or updating domain docs for terminology that:
- Defines specific terms used in the project's domain
- Describes entities and their relationships
- Clarifies bounded context boundaries
- Establishes ubiquitous language for the team

## STRICT Signal Detection
ONLY surface clear domain knowledge signals. Look for EXPLICIT patterns like:
- "X means..." or "X is defined as..."
- "An X is a type of..." or "X represents..."
- "X relates to Y by..." or "X contains/produces/references Y"
- "In this context, X refers to..."
- "We call this X because..."
- Technical terminology being explained or clarified
- Entity definitions with clear properties or relationships

DO NOT surface:
- General discussion about features or implementation
- Architectural decisions (those belong in ADRs)
- Code-level concerns without domain meaning
- Ambiguous references that might be domain terms
- Implementation details without domain significance

If you're uncertain whether something is a domain signal, err on the side of NOT surfacing it.

## Unprocessed Conversations
These conversations haven't been reviewed for domain knowledge yet:

%s

## Existing Domain Docs
**CRITICAL: ALWAYS check existing domain docs before suggesting new ones. If a signal relates to an existing bounded context, suggest UPDATING that doc instead of creating a new one.**

%s

## The Journey

### PHASE 1: ANALYZE
For each unprocessed conversation:
- Look for CLEAR domain signals (be strict - only surface definitive domain knowledge)
- Identify terms being defined or explained
- Note entity discussions with properties or relationships
- Detect bounded context boundaries

Present your findings: "I found [N] clear domain signals in the conversation from [DATE]. Here's what I identified..."

If no clear signals are found, state: "No clear domain signals found in this conversation. It can be marked as processed."

### PHASE 2: CHECK EXISTING
Before suggesting a new domain doc:
- **Scan existing domain docs above** for related bounded contexts
- If a related context exists, suggest **UPDATING it** instead of creating new
- Show: "This relates to existing domain doc '{id}' ({title}). Suggest: UPDATE existing at .utopia/domain/{id}.yaml vs CREATE new?"
- Ask ONE question at a time about whether to proceed

### PHASE 3: CREATE OR UPDATE
For each domain signal the user wants to document:
- Gather the term/entity definition
- Capture any aliases or alternative names
- Document relationships to other entities
- Assign to appropriate bounded context

### PHASE 4: SAVE
Write or update the domain doc file using the format below.
After saving, mark the conversation as processed.

## Domain Doc Format

Save domain docs to: %s/{bounded-context-id}.yaml

` + "```yaml" + `
id: bounded-context-id
title: "Human Readable Context Title"
description: |
  Brief description of this bounded context and what domain
  concepts it encompasses.

terms:
  - term: "Term Name"
    definition: |
      Clear definition of what this term means in this context.
    aliases:
      - "alternate name"
      - "another alias"

entities:
  - name: "EntityName"
    description: |
      What this entity represents in the domain.
    relationships:
      - type: contains
        target: OtherEntity
      - type: produces
        target: AnotherEntity

source_conversations:
  - "conversation-id-1"
  - "conversation-id-2"
` + "```" + `

## Relationship Types
Common relationship types to use:
- contains: Entity A contains/owns Entity B
- produces: Entity A creates/generates Entity B
- references: Entity A refers to Entity B
- extends: Entity A is a specialization of Entity B
- implements: Entity A implements/fulfills Entity B

## Marking Conversations Processed

After reviewing a conversation (whether or not it produced domain docs), update its status:
1. Read the conversation file from .utopia/conversations/{id}.yaml
2. Change the status field from "unprocessed" to "processed"
3. Write the updated file back

## Critical Guidelines
- Ask ONE question at a time - keep the conversation focused
- Be STRICT about signal detection - only surface CLEAR domain knowledge
- **ALWAYS scan existing domain docs BEFORE suggesting new ones - prefer UPDATE over CREATE**
- When suggesting an update, show: "UPDATE existing {id}" with the file path .utopia/domain/{id}.yaml
- It's okay if a conversation has no domain signals - not every conversation contains domain knowledge
- ALWAYS mark conversations as processed after review, even if no domain docs were created/updated
- Include the source conversation ID in the domain doc for traceability

Start by presenting a summary of the unprocessed conversations you found, highlighting any CLEAR domain signals you detected. Be strict - if there are no clear signals, say so.`

func runDomain(cmd *cobra.Command, args []string) error {
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

	// Create domain directory if it doesn't exist
	domainDir := filepath.Join(utopiaDir, "domain")
	if err := os.MkdirAll(domainDir, 0755); err != nil {
		return fmt.Errorf("failed to create domain directory: %w", err)
	}

	// Load existing domain docs
	existingDocs, err := store.ListDomainDocs()
	if err != nil {
		// Non-fatal - continue with empty list
		existingDocs = []*domain.DomainDoc{}
	}

	// Build summaries for Claude
	convsSummary := buildDomainConversationsSummary(unprocessedConvs)
	docsSummary := buildDomainDocsSummary(existingDocs)

	// Inject summaries and paths into the system prompt
	systemPrompt := fmt.Sprintf(domainSystemPrompt, convsSummary, docsSummary, domainDir)

	fmt.Println("Starting domain discovery session...")
	fmt.Printf("Found %d unprocessed conversations\n", len(unprocessedConvs))
	fmt.Printf("Found %d existing domain docs\n", len(existingDocs))
	fmt.Println()
	fmt.Println("Domain docs will be saved to:", domainDir)
	fmt.Println()

	// Run interactive Claude session
	ctx := context.Background()
	cli := claude.NewCLI()

	_, sessionErr := cli.SessionWithCapture(ctx, systemPrompt)

	fmt.Println()
	fmt.Println("Session ended.")

	// Note: We don't save this conversation to avoid infinite loops
	// (domain sessions reviewing domain sessions)

	if sessionErr != nil {
		return fmt.Errorf("claude session failed: %w", sessionErr)
	}

	return nil
}

// buildDomainConversationsSummary creates a readable summary of unprocessed conversations for Claude
func buildDomainConversationsSummary(convs []*domain.Conversation) string {
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
			// Show abbreviated commit SHAs
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

		// Include transcript preview (first 500 chars)
		transcript := strings.TrimSpace(conv.Transcript)
		if len(transcript) > 500 {
			transcript = transcript[:500] + "..."
		}
		if transcript != "" {
			sb.WriteString(fmt.Sprintf("**Transcript Preview:**\n```\n%s\n```\n", transcript))
		} else {
			sb.WriteString("**Transcript:** (empty)\n")
		}
		sb.WriteString("\n")
	}

	return sb.String()
}

// buildDomainDocsSummary creates a readable summary of existing domain docs for Claude
func buildDomainDocsSummary(docs []*domain.DomainDoc) string {
	if len(docs) == 0 {
		return "(No existing domain docs found)"
	}

	var sb strings.Builder
	for _, doc := range docs {
		sb.WriteString(fmt.Sprintf("### %s\n", doc.ID))
		sb.WriteString(fmt.Sprintf("**Title:** %s\n", doc.Title))

		// Truncate description if too long
		desc := strings.TrimSpace(doc.Description)
		if len(desc) > 200 {
			desc = desc[:200] + "..."
		}
		sb.WriteString(fmt.Sprintf("**Description:** %s\n", desc))

		// List terms
		if len(doc.Terms) > 0 {
			termNames := make([]string, len(doc.Terms))
			for i, t := range doc.Terms {
				termNames[i] = t.Term
			}
			sb.WriteString(fmt.Sprintf("**Terms:** %s\n", strings.Join(termNames, ", ")))
		}

		// List entities
		if len(doc.Entities) > 0 {
			entityNames := make([]string, len(doc.Entities))
			for i, e := range doc.Entities {
				entityNames[i] = e.Name
			}
			sb.WriteString(fmt.Sprintf("**Entities:** %s\n", strings.Join(entityNames, ", ")))
		}

		sb.WriteString("\n")
	}

	return sb.String()
}
