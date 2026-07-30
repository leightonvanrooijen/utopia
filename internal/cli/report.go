package cli

import (
	"fmt"

	"github.com/leightonvanrooijen/utopia/internal/analysis"
	"github.com/leightonvanrooijen/utopia/internal/cli/ui"
	"github.com/spf13/cobra"
)

// noRunRecords is what both reports print for a repository that has executed
// nothing yet. It is a result, not an error: there is no failure in asking for a
// report before there is anything to report, so the command exits zero.
const noRunRecords = "No run records yet. Run 'utopia execute' to produce some, then re-run this report."

// NewReportCmd builds the report command family: readers over the persisted run
// records under .utopia/runs. Nothing here executes work or calls Claude - every
// figure printed was written by a previous run.
func NewReportCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "report",
		Short: "Report on what past runs spent and achieved",
		Long: `Aggregate the usage entries persisted on run records under .utopia/runs.

Reports read only what execution already wrote - no Claude call is made, and no
transcript is re-read.`,
	}
	cmd.AddCommand(newReportModelsCmd(), newReportEscalationsCmd(), newReportOutcomesCmd())
	return cmd
}

func init() {
	rootCmd.AddCommand(NewReportCmd())
}

func newReportModelsCmd() *cobra.Command {
	var by string
	cmd := &cobra.Command{
		Use:   "models",
		Short: "Compare model and effort choices by what they finished and spent",
		Long: `Print one row per model and effort pair: how many attempts it made, how many work
items it finished, its first-pass rate, its mean iterations to completion, and
its tokens and cost per completed work item.

Use --by spec or --by cr_type to group the same rows by that dimension.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			grouping, err := analysis.ParseGroupBy(by)
			if err != nil {
				return err
			}
			_, _, store, err := ResolveProject(cmd)
			if err != nil {
				return err
			}
			runs, err := store.ListExecutionRuns()
			if err != nil {
				return fmt.Errorf("failed to read run records: %w", err)
			}

			out := ui.NewPrinter(cmd.OutOrStdout(), cmd.ErrOrStderr())
			printModelReport(out, analysis.ModelComparison(runs, grouping))
			return nil
		},
	}
	cmd.Flags().StringVar(&by, "by", "", "group rows by a dimension: spec, cr_type")
	return cmd
}

func newReportEscalationsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "escalations",
		Short: "Compare what escalated change requests spent before and after escalating",
		Long: `Print one row per escalated change request: the model and spend before it left the
default executor, the model and spend after, and how it ended - followed by the
marginal cost and marginal completion rate of escalating.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			_, _, store, err := ResolveProject(cmd)
			if err != nil {
				return err
			}
			runs, err := store.ListExecutionRuns()
			if err != nil {
				return fmt.Errorf("failed to read run records: %w", err)
			}

			out := ui.NewPrinter(cmd.OutOrStdout(), cmd.ErrOrStderr())
			printEscalationReport(out, analysis.Escalations(runs))
			return nil
		},
	}
}

func newReportOutcomesCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "outcomes",
		Short: "Report what each change request outcome cost",
		Long: `Print one row per way a change request ended - completed, needs_human, abandoned -
carrying how many ended that way, how many escalated, and what they spent per
change request.

Cost is read from the usage each attempt recorded, not approximated from attempt
counts. Dollars charged under api-key auth and the list-price estimate of tokens
spent under subscription auth are shown in separate columns and never summed.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			_, _, store, err := ResolveProject(cmd)
			if err != nil {
				return err
			}
			runs, err := store.ListExecutionRuns()
			if err != nil {
				return fmt.Errorf("failed to read run records: %w", err)
			}

			out := ui.NewPrinter(cmd.OutOrStdout(), cmd.ErrOrStderr())
			printOutcomeReport(out, analysis.OutcomeCosts(runs))
			return nil
		},
	}
}
