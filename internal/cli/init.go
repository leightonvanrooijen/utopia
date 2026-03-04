package cli

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/leightonvanrooijen/utopia/internal/domain"
	"github.com/leightonvanrooijen/utopia/internal/infra/storage"
	"github.com/spf13/cobra"
)

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize a new Utopia project",
	Long: `Initialize a new Utopia project in the current directory.

This is the first step in the Utopia workflow. It creates a .utopia directory with:
  - config.yaml       Project configuration (verification command, strategies)
  - specs/            Living specifications (your system's source of truth)
  - work-items/       Auto-chunked work items for Ralph execution

You'll be prompted to configure:
  - Verification command (e.g., "npm test", "go test ./...")
  - Max iterations per work item (or unlimited)

After init, run 'utopia cr' to create your first change request.`,
	RunE: runInit,
}

func init() {
	rootCmd.AddCommand(initCmd)
}

func runInit(cmd *cobra.Command, args []string) error {
	projectDir := GetProjectDir(cmd)

	// Resolve to absolute path
	absPath, err := filepath.Abs(projectDir)
	if err != nil {
		return fmt.Errorf("failed to resolve project path: %w", err)
	}

	utopiaDir := filepath.Join(absPath, ".utopia")
	store := storage.NewYAMLStore(utopiaDir)

	// Check if config already exists
	existingConfig, _ := store.LoadConfig()
	isReInit := existingConfig != nil

	// Create directory structure (idempotent)
	dirs := []string{
		utopiaDir,
		filepath.Join(utopiaDir, "specs"),
		filepath.Join(utopiaDir, "work-items"),
	}

	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("failed to create directory %s: %w", dir, err)
		}
	}

	// Start with existing config or defaults
	config := existingConfig
	if config == nil {
		config = domain.DefaultConfig()
	}

	reader := bufio.NewReader(os.Stdin)
	var added, skipped []string

	// Prompt for verification command if missing
	if config.Verification.Command == "" {
		fmt.Print("What command verifies your code? (e.g., npm test): ")
		verifyCmd, err := reader.ReadString('\n')
		if err != nil {
			return fmt.Errorf("failed to read verification command: %w", err)
		}
		config.Verification.Command = strings.TrimSpace(verifyCmd)
		added = append(added, "verification.command")
	} else {
		skipped = append(skipped, "verification.command")
	}

	// Prompt for max iterations if missing (0 means unlimited, so only prompt if never set)
	// We use a sentinel approach: if re-init and field was explicitly set (even to 0), skip
	if !isReInit || (isReInit && config.Verification.MaxIterations == 0 && config.Verification.Command == "") {
		// Only prompt on fresh init, not re-init (max_iterations has a valid zero value)
		if !isReInit {
			fmt.Print("Max iterations per work item? (leave blank for unlimited): ")
			maxIterStr, err := reader.ReadString('\n')
			if err != nil {
				return fmt.Errorf("failed to read max iterations: %w", err)
			}
			maxIterStr = strings.TrimSpace(maxIterStr)

			if maxIterStr != "" {
				maxIterations, err := strconv.Atoi(maxIterStr)
				if err != nil {
					return fmt.Errorf("invalid max iterations value: %w", err)
				}
				config.Verification.MaxIterations = maxIterations
			}
			added = append(added, "verification.max_iterations")
		} else {
			skipped = append(skipped, "verification.max_iterations")
		}
	} else {
		skipped = append(skipped, "verification.max_iterations")
	}

	// Prompt for project context if missing
	if config.ProjectContext == "" {
		fmt.Print("Project context (orient an AI to this project's workflow): ")
		projectContext, err := reader.ReadString('\n')
		if err != nil {
			return fmt.Errorf("failed to read project context: %w", err)
		}
		config.ProjectContext = strings.TrimSpace(projectContext)
		added = append(added, "project_context")
	} else {
		skipped = append(skipped, "project_context")
	}

	if err := store.SaveConfig(config); err != nil {
		return fmt.Errorf("failed to write config: %w", err)
	}

	// Print appropriate output based on fresh init vs re-init
	if isReInit {
		fmt.Printf("Updated Utopia project at %s\n", utopiaDir)
		fmt.Println()
		if len(added) > 0 {
			fmt.Println("Added:")
			for _, field := range added {
				fmt.Printf("  %s\n", field)
			}
		}
		if len(skipped) > 0 {
			fmt.Println("Skipped (already configured):")
			for _, field := range skipped {
				fmt.Printf("  %s\n", field)
			}
		}
	} else {
		fmt.Printf("Initialized Utopia project at %s\n", utopiaDir)
		fmt.Println()
		fmt.Println("Created:")
		fmt.Println("  .utopia/config.yaml    - Project configuration")
		fmt.Println("  .utopia/specs/         - Living specifications")
		fmt.Println("  .utopia/work-items/    - Work items for Ralph")
		fmt.Println()
		fmt.Println("Next steps:")
		fmt.Println("  utopia cr              - Create a change request")
		fmt.Println("  utopia status          - View project status")
	}

	return nil
}
