package cli

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/leightonvanrooijen/utopia/internal/analysis"
	"github.com/leightonvanrooijen/utopia/internal/ui"
	"github.com/leightonvanrooijen/utopia/internal/domain"
)

// printModelReport renders the model comparison: a header saying what was read,
// one table per group, and the report's own limits.
func printModelReport(out *ui.Printer, report analysis.ModelReport) {
	out.Printf("MODEL ECONOMICS\n")
	out.Printf("===============\n")
	if report.Empty() {
		out.Printf("  %s\n", noRunRecords)
		return
	}
	out.Printf("  %d run record(s) read\n", report.Records)
	if report.RecordsWithoutUsage > 0 {
		out.Printf("  %d carry no usage entries (written before usage was captured): unknown spend, excluded from every row\n",
			report.RecordsWithoutUsage)
	}
	out.Printf("\n")

	if len(report.Groups) == 0 {
		out.Printf("  No run record carries usage entries yet, so there is nothing to compare.\n\n")
		printCaveats(out, report.HasUnknownBasisCost())
		return
	}

	for _, group := range report.Groups {
		if group.Key != "" {
			out.Printf("%s: %s\n", strings.ToUpper(groupLabel(report.By)), group.Key)
		}
		out.Table(ui.Table{
			Headers: []string{
				"MODEL", "EFFORT", "ATTEMPTS", "UNAVAIL", "ITEMS", "DONE",
				"FIRST-PASS", "MEAN ITERS", "TOKENS", "TOKENS/DONE", "COST/DONE",
			},
			Rows: modelRows(group.Rows),
		})
		out.Printf("\n")
	}

	printCaveats(out, report.HasUnknownBasisCost())
}

func modelRows(rows []analysis.ModelRow) [][]string {
	cells := make([][]string, 0, len(rows))
	for _, row := range rows {
		charged, listPrice, unknownBasis := row.CostPerCompleted()
		cells = append(cells, []string{
			row.Model,
			row.Effort,
			strconv.Itoa(row.Attempts),
			strconv.Itoa(row.Unavailable),
			strconv.Itoa(row.WorkItems),
			strconv.Itoa(row.Completed),
			formatRate(row.FirstPassRate(), row.WorkItems > 0),
			formatMean(row.MeanIterationsToCompletion(), row.Completed > 0),
			formatTokenTotal(row.Usage),
			formatTokenMean(row.TokensPerCompleted(), row.Completed > 0 && row.Usage.Available > 0),
			formatCost(charged, listPrice, unknownBasis),
		})
	}
	return cells
}

// printEscalationReport renders the before-versus-after view, its aggregate line,
// and the report's own limits.
func printEscalationReport(out *ui.Printer, report analysis.EscalationReport) {
	out.Printf("ESCALATIONS\n")
	out.Printf("===========\n")
	if report.Empty() {
		out.Printf("  %s\n", noRunRecords)
		return
	}
	out.Printf("  %d run record(s) read\n\n", report.Records)

	if report.EscalatedCRs() == 0 {
		out.Printf("  No change request has escalated, so there is no before-and-after to compare.\n")
		out.Printf("  Baseline: %d of %d change request(s) that never escalated completed (%s).\n\n",
			report.BaselineCompleted, report.BaselineCRs, formatRate(report.BaselineCompletionRate(), report.BaselineCRs > 0))
		printCaveats(out, false)
		return
	}

	out.Table(ui.Table{
		Headers: []string{
			"CHANGE REQUEST", "ITEMS", "ESC ITEMS",
			"BEFORE MODEL", "B-ATT", "B-UNAVAIL", "B-TOKENS", "B-COST",
			"AFTER MODEL", "A-ATT", "A-UNAVAIL", "A-TOKENS", "A-COST",
			"OUTCOME",
		},
		Rows: escalationRows(report.Rows),
	})
	out.Printf("\n")

	charged, listPrice, unknownBasis := report.MarginalCostPerEscalation()
	marginal := report.MarginalUsage()
	out.Printf("Aggregate: %d of %d escalated change request(s) completed after escalating (marginal completion rate %s).\n",
		report.CompletedAfterEscalating(), report.EscalatedCRs(), formatRate(report.MarginalCompletionRate(), true))
	if marginal.Available == 0 {
		out.Printf("  Spend after escalation: not measured on any of these records, so the marginal cost is unknown rather than zero.\n")
	} else {
		out.Printf("  Spend after escalation: %s over %s token(s); marginal cost per escalation %s.\n",
			formatCost(marginal.ChargedCostUSD, marginal.ListPriceCostUSD, marginal.UnknownBasisCostUSD),
			formatTokenTotal(marginal),
			formatCost(charged, listPrice, unknownBasis))
	}
	if marginal.Unavailable > 0 {
		out.Printf("  %d attempt(s) after escalation had unavailable usage and are excluded from those figures.\n",
			marginal.Unavailable)
	}
	if without := recordsWithoutUsage(report); without > 0 {
		out.Printf("  %d run record(s) of these change requests carry no usage entries (written before usage was captured),\n", without)
		out.Printf("    so their spend is shown as \"-\": unknown, not zero.\n")
	}
	out.Printf("  Baseline: %d of %d change request(s) that never escalated completed (%s).\n\n",
		report.BaselineCompleted, report.BaselineCRs, formatRate(report.BaselineCompletionRate(), report.BaselineCRs > 0))

	printCaveats(out, hasUnknownBasis(report))
}

// printOutcomeReport renders cost per change request outcome. Charged dollars and
// list-price estimates get a column each rather than a joined cell: the whole point
// of the view is that the reader can see what an outcome cost under each auth mode
// without the two ever being added up.
func printOutcomeReport(out *ui.Printer, report analysis.OutcomeReport) {
	out.Printf("COST PER OUTCOME\n")
	out.Printf("================\n")
	if report.Empty() {
		out.Printf("  %s\n", noRunRecords)
		return
	}
	out.Printf("  %d run record(s) read across %d change request(s)\n\n", report.Records, report.ChangeRequests)

	out.Table(ui.Table{
		Headers: []string{
			"OUTCOME", "CRS", "ITEMS", "ESC", "ATTEMPTS", "UNAVAIL", "TOKENS",
			"CHARGED", "LIST-EST", "CHARGED/CR", "LIST-EST/CR",
		},
		Rows: outcomeRows(report.Rows),
	})
	out.Printf("\n")

	for _, row := range report.Rows {
		if row.RecordsWithoutUsage > 0 {
			out.Printf("  %s %s: %d run record(s) carry no usage entries (written before usage was captured),\n",
				ui.Bullet, row.Outcome, row.RecordsWithoutUsage)
			out.Printf("    so that outcome's spend is a floor - unknown, not zero.\n")
		}
		if row.Usage.UnknownBasisCostUSD != 0 {
			out.Printf("  %s %s: $%.4f of spend has no resolved auth mode, so it is in neither column above.\n",
				ui.Bullet, row.Outcome, row.Usage.UnknownBasisCostUSD)
		}
	}
	out.Printf("\n")

	printCaveats(out, report.HasUnknownBasisCost())
}

func outcomeRows(rows []analysis.OutcomeRow) [][]string {
	cells := make([][]string, 0, len(rows))
	for _, row := range rows {
		charged, listPrice, _ := row.CostPerChangeRequest()
		measured := row.Usage.Available > 0
		cells = append(cells, []string{
			row.Outcome,
			strconv.Itoa(row.ChangeRequests),
			strconv.Itoa(row.WorkItems),
			strconv.Itoa(row.Escalated),
			strconv.Itoa(row.Attempts),
			strconv.Itoa(row.Unavailable),
			formatTokenTotal(row.Usage),
			formatBasisCost(row.Usage.ChargedCostUSD, measured),
			formatBasisCost(row.Usage.ListPriceCostUSD, measured),
			formatBasisCost(charged, measured),
			formatBasisCost(listPrice, measured),
		})
	}
	return cells
}

// formatBasisCost renders one basis's dollars on its own, or "-" when no attempt
// behind the figure carried readable accounting or the basis saw no spend: a zero
// printed in a cost column says the work was free, which is not what an
// unmeasured attempt means.
func formatBasisCost(amount float64, measured bool) string {
	if !measured || amount == 0 {
		return "-"
	}
	return fmt.Sprintf("$%.4f", amount)
}

func escalationRows(rows []analysis.EscalationRow) [][]string {
	cells := make([][]string, 0, len(rows))
	for _, row := range rows {
		cells = append(cells, []string{
			row.CRID,
			strconv.Itoa(row.WorkItems),
			strconv.Itoa(row.Escalated),
			formatModels(row.Before),
			strconv.Itoa(row.Before.Attempts),
			strconv.Itoa(row.Before.Unavailable),
			formatTokenTotal(row.Before.Usage),
			formatCost(row.Before.Usage.ChargedCostUSD, row.Before.Usage.ListPriceCostUSD, row.Before.Usage.UnknownBasisCostUSD),
			formatModels(row.After),
			strconv.Itoa(row.After.Attempts),
			strconv.Itoa(row.After.Unavailable),
			formatTokenTotal(row.After.Usage),
			formatCost(row.After.Usage.ChargedCostUSD, row.After.Usage.ListPriceCostUSD, row.After.Usage.UnknownBasisCostUSD),
			row.Outcome,
		})
	}
	return cells
}

// recordsWithoutUsage is how many run records behind these rows carry no usage
// list at all, so the report can say why a side reads as "-".
func recordsWithoutUsage(report analysis.EscalationReport) int {
	n := 0
	for _, row := range report.Rows {
		n += row.RecordsWithoutUsage
	}
	return n
}

// hasUnknownBasis reports whether any side of any row carries dollars whose auth
// mode was not resolved.
func hasUnknownBasis(report analysis.EscalationReport) bool {
	for _, row := range report.Rows {
		if row.Before.Usage.UnknownBasisCostUSD != 0 || row.After.Usage.UnknownBasisCostUSD != 0 {
			return true
		}
	}
	return false
}

// printCaveats prints what the report does not claim: the unavailable-usage
// exclusion, the two cost bases, and that the comparison is observational.
func printCaveats(out *ui.Printer, unknownBasis bool) {
	out.Printf("Reading these figures\n")
	out.Printf("---------------------\n")
	out.Printf("  %s %s\n", ui.Bullet, analysis.UnavailableUsageNote)
	out.Printf("  %s Costs are kept apart by basis and never summed into one figure: %q is money billed under\n",
		ui.Bullet, costLabel(domain.CostBasisCharged))
	out.Printf("    api-key auth, %q is the list-price equivalent of tokens spent under subscription auth, where no\n",
		costLabel(domain.CostBasisListPriceEstimate))
	out.Printf("    per-token charge was incurred at all.\n")
	if unknownBasis {
		out.Printf("  %s %q is spend whose auth mode Utopia did not resolve, so which of the two above applies is not\n",
			ui.Bullet, costLabel(domain.CostBasisUnknown))
		out.Printf("    knowable from the record.\n")
	}
	out.Printf("  %s %s\n", ui.Bullet, analysis.ObservationalCaveat)
}

// groupLabel names the grouping dimension for a group header.
func groupLabel(by analysis.GroupBy) string {
	switch by {
	case analysis.GroupBySpec:
		return "spec"
	case analysis.GroupByCRType:
		return "cr_type"
	default:
		return "all"
	}
}

// costLabel is the short tag a dollar figure carries in a cost cell, one per
// basis, so a reader can tell what any number in that column means.
func costLabel(basis domain.CostBasis) string {
	switch basis {
	case domain.CostBasisCharged:
		return "charged"
	case domain.CostBasisListPriceEstimate:
		return "list-est"
	default:
		return "unknown-basis"
	}
}

// formatCost renders the three dollar bases as separate labelled parts joined by
// " + ", never added together: charged money and a list-price equivalent are
// different quantities, and one figure summing both is neither (see ADR-007).
func formatCost(charged, listPrice, unknownBasis float64) string {
	var parts []string
	if charged != 0 {
		parts = append(parts, fmt.Sprintf("$%.4f %s", charged, costLabel(domain.CostBasisCharged)))
	}
	if listPrice != 0 {
		parts = append(parts, fmt.Sprintf("$%.4f %s", listPrice, costLabel(domain.CostBasisListPriceEstimate)))
	}
	if unknownBasis != 0 {
		parts = append(parts, fmt.Sprintf("$%.4f %s", unknownBasis, costLabel(domain.CostBasisUnknown)))
	}
	if len(parts) == 0 {
		return "-"
	}
	return strings.Join(parts, " + ")
}

// formatModels renders a side's models, or "-" when nothing ran on it - which is
// the change request escalated by rewrite alone, where the escalated executor
// never took an attempt.
func formatModels(side analysis.EscalationSide) string {
	if len(side.Models) == 0 {
		return "-"
	}
	return strings.Join(side.Models, ",")
}

// formatRate renders a share as a percentage, or "-" when its denominator was
// zero: a rate over nothing is undefined, not 0%.
func formatRate(rate float64, defined bool) string {
	if !defined {
		return "-"
	}
	return fmt.Sprintf("%.1f%%", rate*100)
}

// formatMean renders a mean to one decimal, or "-" when nothing was averaged.
func formatMean(mean float64, defined bool) string {
	if !defined {
		return "-"
	}
	return fmt.Sprintf("%.1f", mean)
}

// formatTokenMean renders a per-completion token figure as a whole grouped
// number: a tenth of a token is not a quantity anyone reads.
func formatTokenMean(mean float64, defined bool) string {
	if !defined {
		return "-"
	}
	return formatTokens(int(mean + 0.5))
}

// formatTokenTotal renders a measured token count, or "-" when no attempt in the
// total carried readable counts. A total of zero over zero readable attempts is
// unknown spend, and printing "0" for it would say the attempts spent nothing.
func formatTokenTotal(t domain.UsageTotals) string {
	if t.Available == 0 {
		return "-"
	}
	return formatTokens(t.TotalTokens())
}

// formatTokens groups a token count in thousands so a seven-figure column is
// scannable.
func formatTokens(n int) string {
	s := strconv.Itoa(n)
	if n < 0 {
		return s
	}
	var b strings.Builder
	for i, digit := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(digit)
	}
	return b.String()
}
