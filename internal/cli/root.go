package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "utopia",
	Short: "AI-assisted development system",
	Long: `Utopia is a two-layer system where humans define intent through conversation,
and AI agents execute the work using the Ralph methodology.

The Human Layer (this tool) helps you:
  - Explore and define what you want to build
  - Create structured specifications through conversation
  - Generate work items ready for AI execution

The AI Layer (future) will:
  - Execute work items using Ralph loops
  - Iterate until completion promises are met
  - Produce working code that meets your specs`,
}

// Execute runs the CLI
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func init() {
	// Global flags can be added here
	rootCmd.PersistentFlags().StringP("project", "p", ".", "project directory")
}

// GetProjectDir returns the project directory from flags
func GetProjectDir(cmd *cobra.Command) string {
	dir, _ := cmd.Flags().GetString("project")
	return dir
}
