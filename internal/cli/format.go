package cli

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/leightonvanrooijen/utopia/internal"
	"github.com/leightonvanrooijen/utopia/internal/cli/ui"
	"github.com/spf13/cobra"
)

// ErrCheckFailed signals that format --check found files needing reformatting.
// It carries exit-status semantics (like gofmt -l), not an error to dump:
// the handler silences cobra's usage/error output and Execute() supplies exit code 1.
var ErrCheckFailed = errors.New("files would be reformatted")

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
	out := ui.NewPrinter(cmd.OutOrStdout(), cmd.ErrOrStderr())
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
		out.Printf("No YAML files to format\n")
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
			out.Printf("Would reformat: %s\n", relPath)
		} else {
			if err := os.WriteFile(file, formatted, 0644); err != nil {
				return fmt.Errorf("failed to write %s: %w", file, err)
			}
			formattedCount++
			relPath, _ := filepath.Rel(absPath, file)
			out.Printf("Formatted: %s\n", relPath)
		}
	}

	if checkOnly {
		if wouldChangeCount > 0 {
			out.Printf("\n%d file(s) would be reformatted\n", wouldChangeCount)
			cmd.SilenceUsage = true
			cmd.SilenceErrors = true
			return ErrCheckFailed
		}
		out.Printf("%d file(s) already formatted\n", len(files))
		return nil
	}

	out.Printf("\nFormatted %d file(s)\n", formattedCount)
	return nil
}
