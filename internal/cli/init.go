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

This creates a .utopia directory with:
  - config.yaml     Configuration for strategies
  - specs/          Living specifications
  - work-items/     Auto-chunked work items for Ralph`,
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

	// Check if already initialized
	if _, err := os.Stat(utopiaDir); err == nil {
		return fmt.Errorf("project already initialized: %s exists", utopiaDir)
	}

	// Create directory structure
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

	// Prompt for verification configuration
	reader := bufio.NewReader(os.Stdin)

	fmt.Print("What command verifies your code? (e.g., npm test): ")
	verifyCmd, err := reader.ReadString('\n')
	if err != nil {
		return fmt.Errorf("failed to read verification command: %w", err)
	}
	verifyCmd = strings.TrimSpace(verifyCmd)

	fmt.Print("Max iterations per work item? (leave blank for unlimited): ")
	maxIterStr, err := reader.ReadString('\n')
	if err != nil {
		return fmt.Errorf("failed to read max iterations: %w", err)
	}
	maxIterStr = strings.TrimSpace(maxIterStr)

	var maxIterations int
	if maxIterStr != "" {
		maxIterations, err = strconv.Atoi(maxIterStr)
		if err != nil {
			return fmt.Errorf("invalid max iterations value: %w", err)
		}
	}

	// Write config with verification settings
	store := storage.NewYAMLStore(utopiaDir)
	config := domain.DefaultConfig()
	config.Verification.Command = verifyCmd
	config.Verification.MaxIterations = maxIterations

	if err := store.SaveConfig(config); err != nil {
		return fmt.Errorf("failed to write config: %w", err)
	}

	fmt.Printf("Initialized Utopia project at %s\n", utopiaDir)
	fmt.Println()
	fmt.Println("Created:")
	fmt.Println("  .utopia/config.yaml    - Project configuration")
	fmt.Println("  .utopia/specs/         - Living specifications")
	fmt.Println("  .utopia/work-items/    - Work items for Ralph")
	fmt.Println()
	fmt.Println("Next steps:")
	fmt.Println("  utopia spec            - Start creating a specification")
	fmt.Println("  utopia status          - View project status")

	return nil
}
