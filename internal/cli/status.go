package cli

import (
	"fmt"

	"github.com/leightonvanrooijen/utopia/internal/cli/ui"
	"github.com/spf13/cobra"
)

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show project status",
	Long:  `Display the current state of specs and work items in the project.`,
	RunE:  runStatus,
}

func init() {
	rootCmd.AddCommand(statusCmd)
}

func runStatus(cmd *cobra.Command, args []string) error {
	out := ui.NewPrinter(cmd.OutOrStdout(), cmd.ErrOrStderr())
	_, _, store, err := ResolveProject(cmd)
	if err != nil {
		return err
	}

	// Load and display specs
	specs, err := store.ListSpecs()
	if err != nil {
		return fmt.Errorf("failed to load specs: %w", err)
	}

	out.Printf("SPECIFICATIONS\n")
	out.Printf("==============\n")
	if len(specs) == 0 {
		out.Printf("  No specs yet. Run 'utopia cr' to create your first change request.\n")
	} else {
		for _, spec := range specs {
			featureCount := len(spec.Features)
			criteriaCount := 0
			for _, f := range spec.Features {
				criteriaCount += len(f.AcceptanceCriteria)
			}
			out.Printf("  %s\n", spec.Title)
			out.Printf("    %d features, %d acceptance criteria\n", featureCount, criteriaCount)
		}
	}
	out.Printf("\n")

	// Load and display work items
	items, err := store.ListWorkItems()
	if err != nil {
		return fmt.Errorf("failed to load work items: %w", err)
	}

	out.Printf("WORK ITEMS\n")
	out.Printf("==========\n")
	if len(items) == 0 {
		out.Printf("  No work items yet. Run 'utopia execute' to chunk a change request into work items.\n")
	} else {
		pending, inProgress, completed, failed := 0, 0, 0, 0
		for _, item := range items {
			switch item.Status {
			case "pending":
				pending++
			case "in_progress":
				inProgress++
			case "completed":
				completed++
			case "failed":
				failed++
			}
		}
		out.Printf("  Total: %d\n", len(items))
		out.Printf("  Pending: %d | In Progress: %d | Completed: %d | Failed: %d\n",
			pending, inProgress, completed, failed)
	}

	return nil
}
