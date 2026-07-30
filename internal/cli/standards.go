package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/leightonvanrooijen/utopia/internal"
	"github.com/leightonvanrooijen/utopia/internal/cli/ui"
	"github.com/spf13/cobra"
)

var standardsCmd = &cobra.Command{
	Use:   "standards",
	Short: "Manage coding standards documentation",
	Long: `Manage coding standards documentation for your project.

Standards define what "good code" looks like in specific areas:
  - Styling and theming patterns
  - Data fetching approaches
  - State management conventions
  - Testing strategies
  - And more...

Use 'utopia standards generate' to create a new standards document.`,
}

func init() {
	rootCmd.AddCommand(standardsCmd)
}

// ============================================================================
// GENERATE - Generate a new standards document
// ============================================================================

var (
	standardsGenerateModelFlag  string
	standardsGenerateEffortFlag string
	standardsGenerateAuthFlag   string
)

var standardsGenerateCmd = &cobra.Command{
	Use:   "generate",
	Short: "Generate a new standards document via guided conversation",
	Long: `Start a conversation with Claude to generate a standards document.

Claude will guide you through defining standards for a specific area of your
codebase (e.g., styling, data fetching, state management) and generate a
focused markdown document.

The resulting document includes:
  - YAML frontmatter with id, title, description, and tags
  - The Stack: Technology choices
  - File Structure: Organization patterns
  - Patterns: Good approaches with code examples
  - Anti-Patterns: What to avoid with severity levels
  - Rationale: Why these decisions
  - Examples: Complete reference implementations

Standards are saved to .utopia/standards/`,
	RunE: runStandardsGenerate,
}

func init() {
	standardsCmd.AddCommand(standardsGenerateCmd)
	standardsGenerateCmd.Flags().StringVar(&standardsGenerateModelFlag, "model", "", "model alias (haiku, sonnet, opus, fable) or a full model identifier")
	standardsGenerateCmd.Flags().StringVar(&standardsGenerateEffortFlag, "effort", "", effortFlagUsage)
	standardsGenerateCmd.Flags().StringVar(&standardsGenerateAuthFlag, "auth", "", "credential to use (api-key, subscription), overriding config.auth.mode")
}

// standardsSystemPrompt guides Claude through standards document creation
const standardsSystemPrompt = `You are a Standards Claude - an AI assistant that helps users define and document coding standards for their codebase.

## Your Role
Guide users through a natural conversation to create a focused standards document for a specific area of their codebase. You help define what "good code" looks like by exploring:
- Technology choices and why
- File organization patterns
- Code patterns to follow
- Anti-patterns to avoid
- Concrete examples

## The Journey

### PHASE 1: DISCOVER
Start by understanding what area the user wants to define standards for:
- "What area of your codebase would you like to define standards for?"
- Common areas: styling/theming, data fetching, state management, testing, error handling, API design, component structure, etc.
- Ask ONE question at a time
- If they're unsure, suggest common areas based on their tech stack

### PHASE 2: EXPLORE
Once you know the area, explore their preferences through conversation:
- What technologies/libraries do they use for this area?
- What patterns have worked well for them?
- What problems have they encountered?
- Are there existing conventions in the codebase to follow?
- What should new team members know?

Use probing questions to draw out implicit knowledge:
- "How do you typically structure [X] files?"
- "What mistakes do you see developers make with [X]?"
- "Is there a particular file or component that exemplifies the right approach?"

### PHASE 3: SYNTHESIZE
Summarize what you've learned and get confirmation:
- "Based on our conversation, here's what I understand about your [X] standards..."
- Confirm technology choices
- Confirm key patterns and anti-patterns
- Ask if anything is missing

### PHASE 4: GENERATE
Create the standards document with this exact structure:

%s

### PHASE 5: SAVE
Save the document to: %s/{standard-id}.md

Use a kebab-case ID based on the area (e.g., "styling", "data-fetching", "state-management").

## Critical Guidelines
- Ask ONE question at a time - keep the conversation focused
- Draw out specific examples from their codebase when possible
- Anti-patterns should have severity levels: Error (must fix) or Warning (should improve)
- Code examples should be realistic and specific to their stack
- The document should be immediately useful to a new team member
- ALWAYS use the Write tool to save the final document
- Standard IDs should be kebab-case and descriptive

Start by warmly greeting the user and asking what area of their codebase they'd like to define standards for.`

// standardsDocumentFormat defines the expected output structure
const standardsDocumentFormat = `
` + "```" + `markdown
---
id: {kebab-case-id}
title: "{Human-Readable Title}"
description: "{One-sentence description of what this covers}"
tags:
  - {relevant}
  - {tags}
---

## The Stack

| Purpose | Technology | Version | Notes |
|---------|------------|---------|-------|
| {purpose} | {tech} | {version or "N/A"} | {brief note} |

## File Structure

` + "```" + `
{folder structure showing where these files live}
` + "```" + `

### Naming Conventions
- {Convention 1}
- {Convention 2}

## Patterns

### {Pattern Name}

{Brief explanation of when and why to use this pattern}

✅ **Good**
` + "```" + `{language}
{code example}
` + "```" + `

❌ **Bad**
` + "```" + `{language}
{code example}
` + "```" + `

{Repeat for each key pattern}

## Anti-Patterns

| Don't | Do Instead | Severity |
|-------|------------|----------|
| {anti-pattern} | {correct approach} | Error |
| {anti-pattern} | {correct approach} | Warning |

## Rationale

### Why {Decision 1}
{Explanation of why this approach was chosen}

### Why {Decision 2}
{Explanation}

## Examples

### {Example Name}

{Description of what this example demonstrates}

` + "```" + `{language}
{complete working example}
` + "```" + `
` + "```" + ``

func runStandardsGenerate(cmd *cobra.Command, args []string) error {
	out := ui.NewPrinter(cmd.OutOrStdout(), cmd.ErrOrStderr())

	// Validate model and effort flags early before any work
	modelID, err := ResolveModelFlag(cmd)
	if err != nil {
		return err
	}

	effort, err := ResolveEffortFlag(cmd)
	if err != nil {
		return err
	}

	// Resolve and report the credential this invocation runs with, before any
	// work starts
	authMode, err := ResolveAuth(cmd)
	if err != nil {
		return err
	}

	_, utopiaDir, _, err := ResolveProject(cmd)
	if err != nil {
		return err
	}

	// Create standards directory if it doesn't exist
	standardsDir := filepath.Join(utopiaDir, "standards")
	if err := os.MkdirAll(standardsDir, 0755); err != nil {
		return fmt.Errorf("failed to create standards directory: %w", err)
	}

	// Build the system prompt with format and path
	systemPrompt := fmt.Sprintf(standardsSystemPrompt, standardsDocumentFormat, standardsDir)

	out.Progressf("Starting standards generation session...\n\n")
	out.Printf("Standards will be saved to: %s\n\n", standardsDir)

	// Run interactive Claude session
	ctx := context.Background()
	cli := internal.NewCLI().WithAuth(authMode, utopiaDir)
	if modelID != "" {
		cli = cli.WithModel(modelID)
	}
	if effort != "" {
		cli = cli.WithEffort(effort)
	}

	_, sessionErr := cli.SessionWithCapture(ctx, systemPrompt)

	if sessionErr != nil {
		return fmt.Errorf("claude session failed: %w", sessionErr)
	}

	out.Progressf("\nSession ended.\n")

	return nil
}
