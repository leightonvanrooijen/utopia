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

var adrCmd = &cobra.Command{
	Use:   "adr",
	Short: "Review conversations and create Architecture Decision Records",
	Long: `Scan persisted conversations for architectural decisions and guide
through ADR creation conversationally.

The command will:
  1. Find unprocessed conversations from .utopia/conversations/
  2. Analyze each for architectural decision signals
  3. Cross-reference with commits and spec changes for context
  4. Guide you through creating ADRs for significant decisions
  5. Mark conversations as processed after review

ADRs are stored in .utopia/adrs/ with sequential IDs (ADR-001, ADR-002, etc.)`,
	RunE: runADR,
}

func init() {
	rootCmd.AddCommand(adrCmd)
}

// adrSystemPrompt guides Claude through ADR discovery and creation
// Use fmt.Sprintf to inject: conversationsSummary, existingADRsSummary, adrsDir, nextADRID
const adrSystemPrompt = `You are an ADR Discovery Claude - an AI assistant that helps identify and document Architecture Decision Records from conversation history.

## Your Role
Review persisted conversations to identify significant architectural decisions that should be recorded as ADRs. Guide the user through creating ADRs for decisions that:
- Affect system structure or behavior significantly
- Have long-term implications
- Involve trade-offs between alternatives
- Would be valuable for future developers to understand

## Unprocessed Conversations
These conversations haven't been reviewed for ADRs yet:

%s

## Existing ADRs
These ADRs already exist (avoid duplicates):

%s

## The Journey

### PHASE 1: ANALYZE
For each unprocessed conversation:
- Look for architectural decision signals:
  - "We decided to..."
  - "We chose X over Y because..."
  - Technology or approach selections
  - API design decisions
  - Data model choices
  - Integration patterns
  - Performance trade-offs
- Cross-reference with any commits or spec changes mentioned
- Identify the context and forces that drove the decision

Present your findings: "I found [N] potential architectural decisions in the conversation from [DATE]. Here's what I identified..."

### PHASE 2: PRIORITIZE
Help the user decide which decisions warrant formal ADRs:
- Not every decision needs an ADR
- Focus on decisions that future developers would need to understand
- Ask ONE question at a time about whether to proceed

### PHASE 3: CREATE
For each decision the user wants to document:
- Gather the context (forces at play)
- Capture the decision in active voice ("We will...")
- Document alternatives that were considered
- List consequences (positive and negative)
- Note any advice or principles that applied

### PHASE 4: SAVE
Write the ADR file using the format below.
After saving, mark the conversation as processed.

## ADR Format

Save ADRs to: %s/{adr-id}.yaml

` + "```yaml" + `
id: ADR-NNN
title: "Use [Technology/Approach] for [Problem]"
status: draft
date: YYYY-MM-DD
context: |
  [Describe the context and forces at play - technical, political, social.
  What is the issue that motivates this decision?]
decision: |
  [The decision in active voice. Start with "We will..."]
options_considered:
  - option: "[First alternative]"
    pros:
      - Pro 1
    cons:
      - Con 1
  - option: "[Second alternative]"
    pros:
      - Pro 1
    cons:
      - Con 1
consequences:
  positive:
    - "[Positive outcome]"
  negative:
    - "[Negative outcome or trade-off]"
  neutral:
    - "[Neutral implication]"
advice:
  - "[Who was consulted, what expertise was brought in]"
principles:
  - "[Architectural principles that apply or conflict]"
source_conversations:
  - "[conversation-id from which this decision was extracted]"
` + "```" + `

## Marking Conversations Processed

After reviewing a conversation (whether or not it produced ADRs), update its status:
1. Read the conversation file from .utopia/conversations/{id}.yaml
2. Change the status field from "unprocessed" to "processed"
3. Write the updated file back

## Critical Guidelines
- Ask ONE question at a time - keep the conversation focused
- The next ADR ID is: %s
- Only create ADRs for genuinely significant architectural decisions
- It's okay if a conversation has no ADRs - not every conversation contains architectural decisions
- ALWAYS mark conversations as processed after review, even if no ADRs were created
- Include the source conversation ID in the ADR for traceability

Start by presenting a summary of the unprocessed conversations you found, highlighting any architectural decision signals you detected.`

func runADR(cmd *cobra.Command, args []string) error {
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

	// Create ADRs directory if it doesn't exist
	adrsDir := filepath.Join(utopiaDir, "adrs")
	if err := os.MkdirAll(adrsDir, 0755); err != nil {
		return fmt.Errorf("failed to create ADRs directory: %w", err)
	}

	// Load existing ADRs
	existingADRs, err := store.ListADRs()
	if err != nil {
		// Non-fatal - continue with empty ADR list
		existingADRs = []*domain.ADR{}
	}

	// Build summaries for Claude
	convsSummary := buildConversationsSummary(unprocessedConvs)
	adrsSummary := buildADRsSummary(existingADRs)
	nextADRID := getNextADRID(existingADRs)

	// Inject summaries and paths into the system prompt
	systemPrompt := fmt.Sprintf(adrSystemPrompt, convsSummary, adrsSummary, adrsDir, nextADRID)

	fmt.Println("Starting ADR discovery session...")
	fmt.Printf("Found %d unprocessed conversations\n", len(unprocessedConvs))
	fmt.Printf("Found %d existing ADRs\n", len(existingADRs))
	fmt.Println()
	fmt.Println("ADRs will be saved to:", adrsDir)
	fmt.Println()

	// Run interactive Claude session
	ctx := context.Background()
	cli := claude.NewCLI()

	_, sessionErr := cli.SessionWithCapture(ctx, systemPrompt)

	fmt.Println()
	fmt.Println("Session ended.")

	// Note: We don't save this conversation to avoid infinite loops
	// (adr sessions reviewing adr sessions)

	if sessionErr != nil {
		return fmt.Errorf("claude session failed: %w", sessionErr)
	}

	return nil
}

// buildConversationsSummary creates a readable summary of unprocessed conversations for Claude
func buildConversationsSummary(convs []*domain.Conversation) string {
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
		sb.WriteString(fmt.Sprintf("**Transcript Preview:**\n```\n%s\n```\n", transcript))
		sb.WriteString("\n")
	}

	return sb.String()
}

// buildADRsSummary creates a readable summary of existing ADRs for Claude
func buildADRsSummary(adrs []*domain.ADR) string {
	if len(adrs) == 0 {
		return "(No existing ADRs found)"
	}

	var sb strings.Builder
	for _, adr := range adrs {
		sb.WriteString(fmt.Sprintf("### %s\n", adr.ID))
		sb.WriteString(fmt.Sprintf("**Title:** %s\n", adr.Title))
		sb.WriteString(fmt.Sprintf("**Status:** %s\n", adr.Status))
		sb.WriteString(fmt.Sprintf("**Date:** %s\n", adr.Date))

		// Truncate context if too long
		ctx := strings.TrimSpace(adr.Context)
		if len(ctx) > 200 {
			ctx = ctx[:200] + "..."
		}
		sb.WriteString(fmt.Sprintf("**Context:** %s\n", ctx))
		sb.WriteString("\n")
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
