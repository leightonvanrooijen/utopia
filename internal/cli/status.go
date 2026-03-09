package cli

import (
	"fmt"

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
	_, _, store, err := ResolveProject(cmd)
	if err != nil {
		return err
	}

	// Load and display specs
	specs, err := store.ListSpecs()
	if err != nil {
		return fmt.Errorf("failed to load specs: %w", err)
	}

	fmt.Println("SPECIFICATIONS")
	fmt.Println("==============")
	if len(specs) == 0 {
		fmt.Println("  No specs yet. Run 'utopia spec' to create one.")
	} else {
		for _, spec := range specs {
			featureCount := len(spec.Features)
			criteriaCount := 0
			for _, f := range spec.Features {
				criteriaCount += len(f.AcceptanceCriteria)
			}
			fmt.Printf("  %s\n", spec.Title)
			fmt.Printf("    %d features, %d acceptance criteria\n", featureCount, criteriaCount)
		}
	}
	fmt.Println()

	// Load and display work items
	items, err := store.ListWorkItems()
	if err != nil {
		return fmt.Errorf("failed to load work items: %w", err)
	}

	fmt.Println("WORK ITEMS")
	fmt.Println("==========")
	if len(items) == 0 {
		fmt.Println("  No work items yet. Run 'utopia chunk' after creating specs.")
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
		fmt.Printf("  Total: %d\n", len(items))
		fmt.Printf("  Pending: %d | In Progress: %d | Completed: %d | Failed: %d\n",
			pending, inProgress, completed, failed)
	}

	return nil
}
