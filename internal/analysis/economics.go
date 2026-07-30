// Package analysis holds derived views over stored artifacts: aggregations a
// reader computes from what execution already persisted, never by re-running
// anything. Nothing here makes a Claude call or touches a transcript.
package analysis

import (
	"fmt"
	"sort"
	"strings"

	"github.com/leightonvanrooijen/utopia/internal/domain"
)

// ObservationalCaveat is what every report built here prints about its own
// limits, verbatim, because the reader who needs it is reading the report.
//
// A change request escalates because it was already failing, so the escalated
// population is selected for difficulty. That makes a row comparing one model
// against another a comparison of different work rather than of the same work
// under two models, and no amount of accumulated records fixes it - only an
// experiment that assigns the model independently of the difficulty would.
const ObservationalCaveat = "Observational, not controlled: change requests escalate because they were already " +
	"failing, so escalated change requests are selected for difficulty. A model-versus-model row " +
	"compares different work, not the same work under two models."

// UnavailableUsageNote explains the unavailable column wherever one is printed:
// those attempts ran and spent tokens whose count was not readable, so they are
// counted and excluded rather than summed as zero.
const UnavailableUsageNote = "Attempts with unavailable usage are counted separately and excluded from token and " +
	"cost totals: their spend happened and is not known, so folding it in as zero would understate every figure."

// GroupBy is the dimension a model comparison groups its rows by.
type GroupBy string

const (
	// GroupByModel is the default: one row per model and effort pair, over every
	// run record in the repository.
	GroupByModel GroupBy = ""
	// GroupBySpec groups the same rows by the spec the work item implemented.
	GroupBySpec GroupBy = "spec"
	// GroupByCRType groups the same rows by change request type.
	GroupByCRType GroupBy = "cr_type"
)

// ParseGroupBy resolves the --by flag value. An empty value is the ungrouped
// report rather than an error, so the flag's absence and an explicit empty
// string mean the same thing.
func ParseGroupBy(value string) (GroupBy, error) {
	switch GroupBy(value) {
	case GroupByModel:
		return GroupByModel, nil
	case GroupBySpec:
		return GroupBySpec, nil
	case GroupByCRType:
		return GroupByCRType, nil
	default:
		return "", fmt.Errorf("unknown grouping %q (expected \"spec\" or \"cr_type\")", value)
	}
}

// ModelRow is what one model at one effort did across the run records: how many
// attempts it made, how many work items it finished, and what those attempts
// spent.
//
// Attempts is counted from every usage entry on that pair, while the work item
// columns are credited to the pair of the attempt that concluded the item. The
// two denominators are deliberately different: an attempt is what a model spent,
// a work item is what it achieved, and a run whose last attempt escalated must
// not credit its completion to the model that failed first.
type ModelRow struct {
	Model  string
	Effort string

	// Attempts is every usage entry recorded on this pair, including the ones whose
	// accounting could not be read. Unavailable counts that subset, and is why the
	// token and cost figures on this row are a floor rather than the whole spend.
	Attempts    int
	Unavailable int

	// WorkItems is the run records this pair concluded, and Completed how many of
	// those completed. FirstPass is the subset that completed on the first attempt.
	WorkItems int
	Completed int
	FirstPass int

	// IterationsToCompletion is the iteration count of the completed items summed,
	// the numerator of the mean.
	IterationsToCompletion int

	// Usage is the spend of this pair's attempts, with charged dollars and
	// list-price equivalents kept in separate columns (see domain.CostBasis).
	Usage domain.UsageTotals
}

// FirstPassRate is the share of the work items this pair concluded that it
// completed on the first attempt, or 0 when it concluded none.
func (r ModelRow) FirstPassRate() float64 {
	if r.WorkItems == 0 {
		return 0
	}
	return float64(r.FirstPass) / float64(r.WorkItems)
}

// MeanIterationsToCompletion is the mean iteration count of the work items this
// pair completed. It returns 0 when it completed none, which a caller renders as
// "no completions" rather than as a mean of zero iterations.
func (r ModelRow) MeanIterationsToCompletion() float64 {
	if r.Completed == 0 {
		return 0
	}
	return float64(r.IterationsToCompletion) / float64(r.Completed)
}

// CostPerCompleted divides this pair's spend by the work items it completed,
// keeping the bases apart: the returned totals are per-completion figures in the
// same three columns, so a charged dollar is never averaged together with a
// list-price equivalent. A pair with no completions returns zeroes.
func (r ModelRow) CostPerCompleted() (charged, listPrice, unknownBasis float64) {
	if r.Completed == 0 {
		return 0, 0, 0
	}
	n := float64(r.Completed)
	return r.Usage.ChargedCostUSD / n, r.Usage.ListPriceCostUSD / n, r.Usage.UnknownBasisCostUSD / n
}

// TokensPerCompleted is this pair's measured tokens per completed work item, or 0
// when it completed none.
func (r ModelRow) TokensPerCompleted() float64 {
	if r.Completed == 0 {
		return 0
	}
	return float64(r.Usage.TotalTokens()) / float64(r.Completed)
}

// ModelGroup is the rows for one value of the grouping dimension. Key is empty
// for the ungrouped report.
type ModelGroup struct {
	Key  string
	Rows []ModelRow
}

// ModelReport is the whole model comparison: the grouped rows plus what the read
// could not account for.
type ModelReport struct {
	By GroupBy

	// Groups are ordered by key, and each group's rows by model then effort, so two
	// runs of the report over the same records print identically.
	Groups []ModelGroup

	// Records is how many run records were read, and RecordsWithoutUsage how many of
	// them carry no usage list at all - runs written before usage was persisted.
	// Those are unknown spend rather than zero and contribute to no row, so the
	// report says how many it left out.
	Records             int
	RecordsWithoutUsage int
}

// Empty reports whether there were no run records at all, which is a repository
// that has nothing to report rather than a report with no rows.
func (r ModelReport) Empty() bool { return r.Records == 0 }

// HasUnknownBasisCost reports whether any row carries dollars whose auth mode was
// not resolved, so the report can say those are excluded from both cost columns
// rather than silently dropping them.
func (r ModelReport) HasUnknownBasisCost() bool {
	for _, g := range r.Groups {
		for _, row := range g.Rows {
			if row.Usage.UnknownBasisCostUSD != 0 {
				return true
			}
		}
	}
	return false
}

// ModelComparison aggregates persisted run records into one row per model and
// effort pair, grouped by the requested dimension.
//
// Records carrying no usage list are counted and skipped: there is no model to
// key them on, and crediting their completions to a pair chosen by guesswork
// would put invented evidence into the comparison the report exists to support.
func ModelComparison(runs []*domain.ExecutionRun, by GroupBy) ModelReport {
	report := ModelReport{By: by}

	// Entries are collected per row and summed by domain.TotalUsage at the end, so
	// the charged/list-price/unavailable split has exactly one implementation.
	type accumulator struct {
		row     ModelRow
		entries []domain.UsageEntry
	}
	groups := map[string]map[modelKey]*accumulator{}

	for _, run := range runs {
		if run == nil {
			continue
		}
		report.Records++
		if len(run.Usage) == 0 {
			report.RecordsWithoutUsage++
			continue
		}

		groupKey := groupKeyOf(run, by)
		rows, ok := groups[groupKey]
		if !ok {
			rows = map[modelKey]*accumulator{}
			groups[groupKey] = rows
		}
		accumulatorFor := func(k modelKey) *accumulator {
			a, ok := rows[k]
			if !ok {
				a = &accumulator{row: ModelRow{Model: k.model, Effort: k.effort}}
				rows[k] = a
			}
			return a
		}

		for _, entry := range run.Usage {
			a := accumulatorFor(keyOf(entry))
			a.row.Attempts++
			a.entries = append(a.entries, entry)
		}

		concluding := concludingEntry(run.Usage)
		a := accumulatorFor(keyOf(concluding))
		a.row.WorkItems++
		if run.Outcome == domain.RunCompleted {
			iterations := iterationsOf(run)
			a.row.Completed++
			a.row.IterationsToCompletion += iterations
			if iterations == 1 {
				a.row.FirstPass++
			}
		}
	}

	for groupKey, rows := range groups {
		group := ModelGroup{Key: groupKey}
		for _, a := range rows {
			a.row.Usage = domain.TotalUsage(a.entries)
			a.row.Unavailable = a.row.Usage.Unavailable
			group.Rows = append(group.Rows, a.row)
		}
		sort.Slice(group.Rows, func(i, j int) bool {
			if group.Rows[i].Model != group.Rows[j].Model {
				return group.Rows[i].Model < group.Rows[j].Model
			}
			return group.Rows[i].Effort < group.Rows[j].Effort
		})
		report.Groups = append(report.Groups, group)
	}
	sort.Slice(report.Groups, func(i, j int) bool { return report.Groups[i].Key < report.Groups[j].Key })

	return report
}

// modelKey is the model and effort pair a row is keyed on. Effort is part of the
// key because the same model at two efforts is two different choices with two
// different prices.
type modelKey struct {
	model  string
	effort string
}

func keyOf(entry domain.UsageEntry) modelKey {
	model, effort := entry.Model, entry.Effort
	if model == "" {
		model = unknownValue
	}
	if effort == "" {
		effort = defaultEffort
	}
	return modelKey{model: model, effort: effort}
}

const (
	// unknownValue stands in for a dimension the record does not carry, so a row for
	// it is visibly unattributed rather than blank.
	unknownValue = "unknown"
	// defaultEffort names the absence of a configured effort: the claude CLI's own
	// default applied, which is a choice like any other and gets its own row.
	defaultEffort = "cli-default"
)

// concludingEntry is the attempt a work item's outcome is credited to: the one
// that passed, or the last one that ran when none did. The last attempt is the
// one the loop gave up on, so a failed item is charged to the tier that failed
// last rather than to the one it started on.
func concludingEntry(entries []domain.UsageEntry) domain.UsageEntry {
	for _, e := range entries {
		if e.Outcome == domain.AttemptPassed {
			return e
		}
	}
	return entries[len(entries)-1]
}

// iterationsOf is how many iterations the run took, falling back to the number of
// usage entries on records whose iteration count was not written.
func iterationsOf(run *domain.ExecutionRun) int {
	if run.Iterations > 0 {
		return run.Iterations
	}
	return len(run.Usage)
}

// groupKeyOf is the run's value for the grouping dimension. A run with no routing
// record has no cr_type, and groups under unknown rather than being dropped: its
// usage is real and the reader can see it is unattributed.
func groupKeyOf(run *domain.ExecutionRun, by GroupBy) string {
	switch by {
	case GroupBySpec:
		if spec, _, found := strings.Cut(run.SpecRef, "."); found {
			return spec
		}
		if run.SpecRef == "" {
			return unknownValue
		}
		return run.SpecRef
	case GroupByCRType:
		if run.Routing == nil || run.Routing.CRType == "" {
			return unknownValue
		}
		return string(run.Routing.CRType)
	default:
		return ""
	}
}

// EscalationSide is what one tier of a change request's execution spent and which
// models spent it - the before or the after of an escalation.
type EscalationSide struct {
	// Models are the distinct resolved models that ran on this side, ordered, so a
	// side that ran on two models does not read as one.
	Models []string

	// Attempts is every entry on this side and Unavailable the subset whose
	// accounting could not be read.
	Attempts    int
	Unavailable int

	Usage domain.UsageTotals
}

// EscalationRow is one escalated change request: what it spent before leaving the
// default executor, what it spent after, and how it ended.
type EscalationRow struct {
	CRID string

	// WorkItems is how many run records the change request has, and Escalated how
	// many of them left the default executor.
	WorkItems int
	Escalated int

	// ScopingEscalations is how many times the change request was sent for rewrite.
	// A change request may be escalated by rewrite alone, in which case the After
	// side carries no attempts and the spend of escalating is the rewrite's, which
	// is not on these records.
	ScopingEscalations int

	Before EscalationSide
	After  EscalationSide

	// Outcome is the change request's final state across its work items, and
	// Completed whether every one of them completed.
	Outcome   string
	Completed bool

	// RecordsWithoutUsage is how many of this change request's run records carry no
	// usage list, so a row whose spend is partial says so.
	RecordsWithoutUsage int
}

// EscalationReport is the before-versus-after view plus the aggregate the
// escalation question is actually asked in.
type EscalationReport struct {
	// Rows are the escalated change requests, ordered by id.
	Rows []EscalationRow

	// Records is every run record read, so an empty repository is distinguishable
	// from one where nothing escalated.
	Records int

	// Baseline is the change requests that never escalated, and how many of them
	// completed. It is the comparison the marginal completion rate is read against,
	// and the reason the observational caveat is printed: these are the easier ones
	// by the same selection that made the escalated ones the harder.
	BaselineCRs       int
	BaselineCompleted int
}

// EscalatedCRs is how many change requests escalated, the denominator of every
// marginal figure below.
func (r EscalationReport) EscalatedCRs() int { return len(r.Rows) }

// CompletedAfterEscalating is how many escalated change requests ended with every
// work item complete - the completions escalating bought.
func (r EscalationReport) CompletedAfterEscalating() int {
	n := 0
	for _, row := range r.Rows {
		if row.Completed {
			n++
		}
	}
	return n
}

// MarginalCompletionRate is the share of escalated change requests that completed.
// Escalation happens because the work had not completed, so every change request
// counted here was incomplete when it escalated: the rate is what escalating
// achieved on work the default executor had not finished.
func (r EscalationReport) MarginalCompletionRate() float64 {
	if len(r.Rows) == 0 {
		return 0
	}
	return float64(r.CompletedAfterEscalating()) / float64(len(r.Rows))
}

// BaselineCompletionRate is the completion rate of the change requests that never
// escalated. It is context for the marginal rate, not a control group.
func (r EscalationReport) BaselineCompletionRate() float64 {
	if r.BaselineCRs == 0 {
		return 0
	}
	return float64(r.BaselineCompleted) / float64(r.BaselineCRs)
}

// MarginalUsage is the total spend after escalation across every escalated change
// request: the cost escalating added, in the same three dollar columns.
func (r EscalationReport) MarginalUsage() domain.UsageTotals {
	var t domain.UsageTotals
	for _, row := range r.Rows {
		t = t.Add(row.After.Usage)
	}
	return t
}

// MarginalCostPerEscalation divides the after-escalation spend by the escalated
// change requests, keeping charged dollars and list-price equivalents apart.
func (r EscalationReport) MarginalCostPerEscalation() (charged, listPrice, unknownBasis float64) {
	if len(r.Rows) == 0 {
		return 0, 0, 0
	}
	t := r.MarginalUsage()
	n := float64(len(r.Rows))
	return t.ChargedCostUSD / n, t.ListPriceCostUSD / n, t.UnknownBasisCostUSD / n
}

// Empty reports whether there were no run records at all.
func (r EscalationReport) Empty() bool { return r.Records == 0 }

// Escalations aggregates persisted run records into one row per escalated change
// request, splitting each one's spend at the escalation boundary.
//
// The split is by executor role rather than by model: a project may configure the
// default and escalated executors to the same model and still have escalated, and
// the role is what the record keeps for exactly that reason.
func Escalations(runs []*domain.ExecutionRun) EscalationReport {
	var report EscalationReport

	type crAccumulator struct {
		row           EscalationRow
		beforeEntries []domain.UsageEntry
		afterEntries  []domain.UsageEntry
		beforeModels  map[string]struct{}
		afterModels   map[string]struct{}
		needsHuman    bool
		abandoned     bool
	}
	byCR := map[string]*crAccumulator{}
	var order []string

	for _, run := range runs {
		if run == nil {
			continue
		}
		report.Records++

		a, ok := byCR[run.CRID]
		if !ok {
			a = &crAccumulator{
				row:          EscalationRow{CRID: run.CRID, Completed: true},
				beforeModels: map[string]struct{}{},
				afterModels:  map[string]struct{}{},
			}
			byCR[run.CRID] = a
			order = append(order, run.CRID)
		}

		a.row.WorkItems++
		if run.Outcome != domain.RunCompleted {
			a.row.Completed = false
		}
		if len(run.Usage) == 0 {
			a.row.RecordsWithoutUsage++
		}
		if run.Routing != nil {
			if run.Routing.Escalated() {
				a.row.Escalated++
			}
			a.row.ScopingEscalations += run.Routing.ScopingEscalations
			switch run.Routing.Outcome {
			case domain.RoutingNeedsHuman:
				a.needsHuman = true
			case domain.RoutingAbandoned:
				a.abandoned = true
			}
		}

		for _, entry := range run.Usage {
			if entry.Role == domain.ExecutorRoleEscalated {
				a.afterEntries = append(a.afterEntries, entry)
				if entry.Model != "" {
					a.afterModels[entry.Model] = struct{}{}
				}
				continue
			}
			// An entry with no role recorded predates role capture; it is default-executor
			// work by construction, since nothing escalated before escalation existed.
			a.beforeEntries = append(a.beforeEntries, entry)
			if entry.Model != "" {
				a.beforeModels[entry.Model] = struct{}{}
			}
		}
	}

	for _, crID := range order {
		a := byCR[crID]
		if a.row.Escalated == 0 {
			report.BaselineCRs++
			if a.row.Completed {
				report.BaselineCompleted++
			}
			continue
		}
		a.row.Before = sideOf(a.beforeEntries, a.beforeModels)
		a.row.After = sideOf(a.afterEntries, a.afterModels)
		a.row.Outcome = outcomeOf(a.row.Completed, a.needsHuman, a.abandoned)
		report.Rows = append(report.Rows, a.row)
	}
	sort.Slice(report.Rows, func(i, j int) bool { return report.Rows[i].CRID < report.Rows[j].CRID })

	return report
}

func sideOf(entries []domain.UsageEntry, models map[string]struct{}) EscalationSide {
	side := EscalationSide{Attempts: len(entries), Usage: domain.TotalUsage(entries)}
	side.Unavailable = side.Usage.Unavailable
	for model := range models {
		side.Models = append(side.Models, model)
	}
	sort.Strings(side.Models)
	return side
}

// outcomeOf names how a change request ended across its work items. needs_human
// wins over abandoned because it is the stronger statement: every bounded
// escalation path was spent, so no further attempt could have changed it.
func outcomeOf(completed, needsHuman, abandoned bool) string {
	switch {
	case completed:
		return "completed"
	case needsHuman:
		return string(domain.RoutingNeedsHuman)
	case abandoned:
		return string(domain.RoutingAbandoned)
	default:
		return string(domain.RunFailed)
	}
}
