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

var conceptCmd = &cobra.Command{
	Use:   "concept",
	Short: "Review conversations and create or update concept documentation",
	Long: `Scan persisted conversations for educational trade-off discussions,
guiding concept creation or updates.

The command will:
  1. Find unprocessed conversations from .utopia/conversations/
  2. Analyze each for trade-off signals (comparisons, "why we chose", reasoning)
  3. Check existing concepts in .utopia/concepts/ for related topics
  4. Suggest updating existing concepts or creating new ones
  5. Guide you through concept creation/update conversationally
  6. Mark conversations as processed after review

Concepts are stored in .utopia/concepts/ as Markdown files with YAML frontmatter.`,
	RunE: runConcept,
}

func init() {
	rootCmd.AddCommand(conceptCmd)
}

// conceptSystemPrompt guides Claude through concept discovery and documentation
// Use fmt.Sprintf to inject: conversationsSummary, existingConceptsSummary, conceptsDir
const conceptSystemPrompt = `You are a Concept Discovery Claude - an AI assistant that helps identify and document educational trade-off discussions from conversation history.

## Your Role
Review persisted conversations to identify trade-off discussions and reasoning that should be documented as concepts. Guide the user through creating or updating concept docs for discussions that:
- Compare different approaches with trade-offs
- Explain "why we chose X over Y"
- Document reasoning behind design decisions
- Capture educational moments that benefit future developers

## STRICT Signal Detection
ONLY surface clear trade-off signals. Look for EXPLICIT patterns like:
- "We chose X because..." or "We went with X over Y because..."
- "The trade-off here is..." or "The downside of this approach..."
- "Option A has [pros] but Option B has [cons]"
- "Why we didn't use..." or "We considered X but..."
- "The reason for this design is..."
- Comparisons between approaches with explicit reasoning
- Educational explanations of non-obvious decisions

DO NOT surface:
- General implementation discussion without explicit reasoning
- Simple "how to" explanations without trade-offs
- Code changes without design rationale
- Discussions that mention alternatives without explaining why one was chosen
- Domain terminology (that belongs in /domain command)
- Architectural decisions without educational context (those belong in ADRs)

If you're uncertain whether something is a trade-off signal, err on the side of NOT surfacing it.

## Unprocessed Conversations
These conversations haven't been reviewed for concept documentation yet:

%s

## Existing Concepts
**CRITICAL: ALWAYS check existing concepts before suggesting new ones. If a signal relates to an existing concept, suggest UPDATING that concept instead of creating a new one.**

%s

## The Journey

### PHASE 1: ANALYZE
For each unprocessed conversation:
- Look for CLEAR trade-off signals (be strict - only surface definitive educational moments)
- Identify reasoning about design choices
- Note comparisons between approaches
- Detect "why we chose" explanations

Present your findings: "I found [N] clear trade-off discussions in the conversation from [DATE]. Here's what I identified..."

If no clear signals are found, state: "No clear trade-off discussions found in this conversation. It can be marked as processed."

### PHASE 2: CHECK EXISTING
Before suggesting a new concept:
- **Scan existing concepts above** for related topics
- If a related concept exists, suggest **UPDATING it** instead of creating new
- Show: "This relates to existing concept '{id}' ({title}). Suggest: UPDATE existing at .utopia/concepts/{id}.md vs CREATE new?"
- Ask ONE question at a time about whether to proceed

### PHASE 3: CREATE OR UPDATE
For each trade-off the user wants to document:
- Gather the context and background
- Capture the approaches considered
- Document the choice made and reasoning
- Note when to reconsider this decision

### PHASE 4: SAVE
Write or update the concept file using the format below.
After saving, mark the conversation as processed.

## Concept File Format

Save concepts to: %s/{kebab-case-id}.md

` + "```markdown" + `
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
[Background and why this trade-off matters. What problem were we solving?]

## Approaches Considered
[Different approaches that were considered and their trade-offs]

### Option A: [Name]
**Pros:**
- ...

**Cons:**
- ...

### Option B: [Name]
**Pros:**
- ...

**Cons:**
- ...

## Our Choice
[The approach we selected and the reasoning behind it]

## When to Reconsider
[Conditions or signals that should trigger re-evaluation of this decision]
` + "```" + `

## Marking Conversations Processed

After reviewing a conversation (whether or not it produced concepts), update its status:
1. Read the conversation file from .utopia/conversations/{id}.yaml
2. Change the status field from "unprocessed" to "processed"
3. Write the updated file back

## Critical Guidelines
- Ask ONE question at a time - keep the conversation focused
- Be STRICT about signal detection - only surface CLEAR trade-off discussions
- **ALWAYS scan existing concepts BEFORE suggesting new ones - prefer UPDATE over CREATE**
- When suggesting an update, show: "UPDATE existing {id}" with the file path .utopia/concepts/{id}.md
- It's okay if a conversation has no trade-off signals - not every conversation contains educational content
- ALWAYS mark conversations as processed after review, even if no concepts were created/updated
- Include the source conversation ID in the concept for traceability
- Concepts start in "draft" status - they can be promoted to "published" later

Start by presenting a summary of the unprocessed conversations you found, highlighting any CLEAR trade-off discussions you detected. Be strict - if there are no clear signals, say so.`

func runConcept(cmd *cobra.Command, args []string) error {
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

	// Create concepts directory if it doesn't exist
	conceptsDir := filepath.Join(utopiaDir, "concepts")
	if err := os.MkdirAll(conceptsDir, 0755); err != nil {
		return fmt.Errorf("failed to create concepts directory: %w", err)
	}

	// Load existing concepts
	existingConcepts, err := store.ListConceptDocs()
	if err != nil {
		// Non-fatal - continue with empty list
		existingConcepts = []*domain.ConceptDoc{}
	}

	// Build summaries for Claude
	convsSummary := buildConceptConversationsSummary(unprocessedConvs)
	conceptsSummary := buildConceptsSummary(existingConcepts)

	// Inject summaries and paths into the system prompt
	systemPrompt := fmt.Sprintf(conceptSystemPrompt, convsSummary, conceptsSummary, conceptsDir)

	fmt.Println("Starting concept discovery session...")
	fmt.Printf("Found %d unprocessed conversations\n", len(unprocessedConvs))
	fmt.Printf("Found %d existing concepts\n", len(existingConcepts))
	fmt.Println()
	fmt.Println("Concepts will be saved to:", conceptsDir)
	fmt.Println()

	// Run interactive Claude session
	ctx := context.Background()
	cli := claude.NewCLI()

	_, sessionErr := cli.SessionWithCapture(ctx, systemPrompt)

	fmt.Println()
	fmt.Println("Session ended.")

	// Note: We don't save this conversation to avoid infinite loops
	// (concept sessions reviewing concept sessions)

	if sessionErr != nil {
		return fmt.Errorf("claude session failed: %w", sessionErr)
	}

	return nil
}

// buildConceptConversationsSummary creates a readable summary of unprocessed conversations for Claude
func buildConceptConversationsSummary(convs []*domain.Conversation) string {
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

// buildConceptsSummary creates a readable summary of existing concepts for Claude
func buildConceptsSummary(concepts []*domain.ConceptDoc) string {
	if len(concepts) == 0 {
		return "(No existing concepts found)"
	}

	var sb strings.Builder
	for _, concept := range concepts {
		sb.WriteString(fmt.Sprintf("### %s\n", concept.ID))
		sb.WriteString(fmt.Sprintf("**Title:** %s\n", concept.Title))
		sb.WriteString(fmt.Sprintf("**Status:** %s\n", concept.Status))

		if len(concept.RelatedSpecs) > 0 {
			sb.WriteString(fmt.Sprintf("**Related Specs:** %s\n", strings.Join(concept.RelatedSpecs, ", ")))
		}

		if len(concept.RelatedADRs) > 0 {
			sb.WriteString(fmt.Sprintf("**Related ADRs:** %s\n", strings.Join(concept.RelatedADRs, ", ")))
		}

		// Show content preview (first 200 chars)
		content := strings.TrimSpace(concept.Content)
		if len(content) > 200 {
			content = content[:200] + "..."
		}
		if content != "" {
			sb.WriteString(fmt.Sprintf("**Preview:** %s\n", content))
		}

		sb.WriteString("\n")
	}

	return sb.String()
}
