package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/leightonvanrooijen/utopia/internal/infra/claude"
	"github.com/leightonvanrooijen/utopia/internal/infra/storage"
	"github.com/spf13/cobra"
)

var patternsCmd = &cobra.Command{
	Use:   "patterns",
	Short: "Manage codebase patterns and architecture documentation",
	Long: `Patterns capture codebase-specific architectural building blocks and flows.

Unlike generic design patterns, these document decisions unique to THIS codebase:
  - Structural patterns (how code is organized)
  - Boundaries (what can call what)
  - Naming conventions
  - How patterns compose together (flows)

Use 'utopia patterns discover' to analyze your codebase and generate draft patterns.`,
}

var discoverCmd = &cobra.Command{
	Use:   "discover",
	Short: "Discover patterns in the codebase via AI analysis",
	Long: `Triggers an AI-driven analysis that:
  1. Scans directory structure to identify organizational patterns
  2. Reads source files to identify recurring structural patterns
  3. Identifies naming conventions from file and function names
  4. Detects boundaries (what calls what, what doesn't)
  5. Generates draft pattern and flow files for human review

The discovery happens in a conversational session where:
  - AI proposes patterns based on codebase analysis
  - You provide feedback and guidance
  - AI refines the patterns
  - Final patterns are written as draft files

Output: Draft .md files in .utopia/patterns/ with status: draft in frontmatter.`,
	RunE: runDiscover,
}

func init() {
	rootCmd.AddCommand(patternsCmd)
	patternsCmd.AddCommand(discoverCmd)
}

// discoverSystemPrompt guides Claude through pattern discovery
// Use fmt.Sprintf to inject: projectDir, patternsDir, flowsDir
const discoverSystemPrompt = `You are a Pattern Discovery Claude - an AI assistant that analyzes codebases to identify and document architectural patterns.

## Your Role
Discover and document codebase-specific patterns through analysis and conversation. You identify building blocks unique to THIS codebase, not generic programming patterns.

## Project Information
Project directory: %s
Patterns will be saved to: %s
Flows will be saved to: %s

## The Discovery Process

### PHASE 1: ANALYZE
Start by analyzing the codebase structure:
1. Use Glob to explore the directory structure
2. Read key files to understand organizational patterns
3. Look for recurring structural patterns in the code
4. Identify naming conventions from file and function names
5. Detect boundaries (what calls what, what doesn't)

Focus areas:
- Directory structure and organization
- File naming patterns
- Class/struct/function naming patterns
- Module boundaries and dependencies
- Common abstractions and building blocks

### PHASE 2: PROPOSE
Present your findings to the user:
- List the patterns you've identified
- Explain why each is codebase-specific (not generic)
- Propose which patterns deserve documentation

Ask the user:
- "Do these patterns look correct?"
- "Should I add or remove any?"
- "Are the boundaries I identified accurate?"

### PHASE 3: REFINE
Based on user feedback:
- Adjust pattern definitions
- Correct any misunderstandings
- Add patterns the user suggests
- Remove patterns that don't fit

Keep iterating until the user is satisfied.

### PHASE 4: WRITE
Once approved, write the pattern and flow files.

## What to Document vs What to Ignore

**The test: Would a competent developer who doesn't know THIS codebase get it wrong?**

DOCUMENT - Building Blocks:
- Codebase-specific structural patterns (Fetcher, Transformer, Service, etc.)
- Boundaries unique to this project (read-only layers, no direct DB access, etc.)
- Custom naming conventions (transform{Resource}.ts, {Name}Service.go, etc.)
- How patterns compose together (flows)
- Decisions made for THIS project

IGNORE - Generic Patterns:
- Error handling (try/catch, Result types) - any senior dev knows this
- Standard language idioms
- SOLID, DRY, generic best practices
- Formatting (covered by linters)
- Patterns any senior developer would know

Heuristic: Is this a decision WE made, or an industry default?
- Decision we made -> Document
- Industry default -> Ignore

## Output Formats

### Pattern File Format
Location: %s/{pattern-id}.md

` + "```markdown" + `
---
id: kebab-case-identifier
status: draft
---

# Pattern Name

## Description
One sentence - what this building block is.

## Responsibility
What this pattern owns:
- Bullet point 1
- Bullet point 2

## Boundaries
What this pattern must NOT do:
- Cannot access X directly
- Must not contain business logic

## Naming
File and function naming convention:
- Template: {Name}Pattern.ext
- Example: UserService.go

## Examples
Real examples in this codebase:
- path/to/example1.ext
- path/to/example2.ext
` + "```" + `

### Flow File Format
Location: %s/{flow-name}.md

` + "```markdown" + `
---
id: kebab-case-identifier
status: draft
patterns:
  - pattern-id-1
  - pattern-id-2
---

# Flow Name

## Description
What this flow accomplishes.

## Diagram
` + "```" + `
[Pattern A] --> [Pattern B] --> [Pattern C]
      |              |
      v              v
  (validation)   (transform)
` + "```" + `

## Layer Rules
| Pattern | Can Call | Cannot Call |
|---------|----------|-------------|
| A       | B        | C           |
| B       | C        | A           |

## Sequence
1. Request arrives at Pattern A
2. Pattern A validates and calls Pattern B
3. Pattern B transforms and calls Pattern C
4. Response flows back up
` + "```" + `

## Critical Guidelines
- Start by exploring the codebase - use Glob and Read tools
- Focus on codebase-specific patterns, not generic ones
- Ask questions to verify your understanding
- Only write files after user approval
- All files must have status: draft in frontmatter
- Pattern IDs should be kebab-case
- Create the directories if they don't exist

Begin by introducing yourself and starting to analyze the codebase structure.`

func runDiscover(cmd *cobra.Command, args []string) error {
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

	// Validate project has config
	store := storage.NewYAMLStore(utopiaDir)
	_, err = store.LoadConfig()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Set up directories for patterns
	patternsDir := filepath.Join(utopiaDir, "patterns")
	flowsDir := filepath.Join(patternsDir, "flows")

	// Create directories if they don't exist
	if err := os.MkdirAll(flowsDir, 0755); err != nil {
		return fmt.Errorf("failed to create patterns directories: %w", err)
	}

	// Build the system prompt with injected paths
	systemPrompt := fmt.Sprintf(discoverSystemPrompt,
		absPath,     // Project directory
		patternsDir, // Patterns directory
		flowsDir,    // Flows directory
		patternsDir, // Pattern file format location
		flowsDir,    // Flow file format location
	)

	fmt.Println("Starting pattern discovery session...")
	fmt.Println()
	fmt.Println("This session will:")
	fmt.Println("  1. Analyze your codebase structure")
	fmt.Println("  2. Identify codebase-specific patterns")
	fmt.Println("  3. Generate draft pattern and flow documentation")
	fmt.Println()
	fmt.Println("Patterns will be saved to:", patternsDir)
	fmt.Println("Flows will be saved to:", flowsDir)
	fmt.Println()
	fmt.Println("Tip: In another terminal, watch for changes with:")
	fmt.Printf("  watch -n 1 'ls -la %s && echo && ls -la %s'\n", patternsDir, flowsDir)
	fmt.Println()

	// Run interactive Claude session
	ctx := context.Background()
	cli := claude.NewCLI()

	_, err = cli.Session(ctx, systemPrompt)
	if err != nil {
		return fmt.Errorf("claude session failed: %w", err)
	}

	fmt.Println()
	fmt.Println("Pattern discovery session ended.")
	fmt.Println()

	// List what was created
	patternFiles, _ := filepath.Glob(filepath.Join(patternsDir, "*.md"))
	flowFiles, _ := filepath.Glob(filepath.Join(flowsDir, "*.md"))

	if len(patternFiles) > 0 || len(flowFiles) > 0 {
		fmt.Println("Created files:")
		for _, f := range patternFiles {
			fmt.Printf("  ✓ %s\n", f)
		}
		for _, f := range flowFiles {
			fmt.Printf("  ✓ %s\n", f)
		}
		fmt.Println()
		fmt.Println("Review these draft files and update status to 'approved' when ready.")
	} else {
		fmt.Println("No pattern files were created in this session.")
	}

	return nil
}
