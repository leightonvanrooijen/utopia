package cli

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/leightonvanrooijen/utopia/internal/infra/formatter"
	"github.com/spf13/cobra"
)

var formatCheck bool

var formatCmd = &cobra.Command{
	Use:   "format",
	Short: "Format YAML files in .utopia directory",
	Long: `Format all YAML files in the .utopia directory using consistent styling.

By default, formats all .yaml files in .utopia/ recursively, excluding config.yaml.

Use --check to verify files are formatted without making changes (useful for CI).`,
	RunE: runFormat,
}

func init() {
	rootCmd.AddCommand(formatCmd)
	formatCmd.Flags().BoolVar(&formatCheck, "check", false, "check if files are formatted (exit non-zero if changes needed)")
}

func runFormat(cmd *cobra.Command, args []string) error {
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

	// Find all YAML files
	var files []string
	err = filepath.WalkDir(utopiaDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".yaml") {
			return nil
		}
		// Exclude config.yaml
		if filepath.Base(path) == "config.yaml" {
			return nil
		}
		files = append(files, path)
		return nil
	})
	if err != nil {
		return fmt.Errorf("failed to find YAML files: %w", err)
	}

	if len(files) == 0 {
		fmt.Println("No YAML files to format")
		return nil
	}

	formattedCount := 0
	wouldChangeCount := 0

	for _, file := range files {
		content, err := os.ReadFile(file)
		if err != nil {
			return fmt.Errorf("failed to read %s: %w", file, err)
		}

		formatted, err := formatter.Format(content)
		if err != nil {
			return fmt.Errorf("failed to format %s: %w", file, err)
		}

		if bytes.Equal(content, formatted) {
			continue
		}

		if formatCheck {
			wouldChangeCount++
			relPath, _ := filepath.Rel(absPath, file)
			fmt.Printf("Would reformat: %s\n", relPath)
		} else {
			if err := os.WriteFile(file, formatted, 0644); err != nil {
				return fmt.Errorf("failed to write %s: %w", file, err)
			}
			formattedCount++
			relPath, _ := filepath.Rel(absPath, file)
			fmt.Printf("Formatted: %s\n", relPath)
		}
	}

	if formatCheck {
		if wouldChangeCount > 0 {
			fmt.Printf("\n%d file(s) would be reformatted\n", wouldChangeCount)
			os.Exit(1)
		}
		fmt.Printf("%d file(s) already formatted\n", len(files))
		return nil
	}

	fmt.Printf("\nFormatted %d file(s)\n", formattedCount)
	return nil
}
