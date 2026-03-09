package cli

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/leightonvanrooijen/utopia/internal"
	"github.com/spf13/cobra"
)

// Flag for format command (package-level for Cobra compatibility)
var formatCheckFlag bool

func init() {
	rootCmd.AddCommand(newFormatCmd())
}

// newFormatCmd creates the format command with flag bindings.
func newFormatCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "format",
		Short: "Format YAML files in .utopia directory",
		Long: `Format all YAML files in the .utopia directory using consistent styling.

By default, formats all .yaml files in .utopia/ recursively, excluding config.yaml.

Use --check to verify files are formatted without making changes (useful for CI).`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runFormat(cmd, args, formatCheckFlag)
		},
	}

	cmd.Flags().BoolVar(&formatCheckFlag, "check", false, "check if files are formatted (exit non-zero if changes needed)")

	return cmd
}

func runFormat(cmd *cobra.Command, args []string, checkOnly bool) error {
	absPath, utopiaDir, _, err := ResolveProject(cmd)
	if err != nil {
		return err
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

		formatted, err := internal.FormatYAML(content)
		if err != nil {
			return fmt.Errorf("failed to format %s: %w", file, err)
		}

		if bytes.Equal(content, formatted) {
			continue
		}

		if checkOnly {
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

	if checkOnly {
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
