package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/leightonvanrooijen/utopia/internal"
	"github.com/leightonvanrooijen/utopia/internal/analysis"
	"github.com/leightonvanrooijen/utopia/internal/domain"
)

// reportProject creates a project directory with the given run records already
// persisted, so the report reads the same YAML a real run would have written.
func reportProject(t *testing.T, runs ...*domain.ExecutionRun) string {
	t.Helper()
	projectDir := t.TempDir()
	utopiaDir := filepath.Join(projectDir, ".utopia")
	if err := os.MkdirAll(utopiaDir, 0755); err != nil {
		t.Fatalf("MkdirAll(%s) = %v", utopiaDir, err)
	}
	store := internal.NewYAMLStore(utopiaDir)
	for _, run := range runs {
		if err := store.SaveExecutionRun(run); err != nil {
			t.Fatalf("SaveExecutionRun(%s) = %v", run.WorkItemID, err)
		}
	}
	return projectDir
}

// runReport executes "report <args...>" against projectDir and returns stdout.
func runReport(t *testing.T, projectDir string, args ...string) string {
	t.Helper()
	cmd := NewReportCmd()
	cmd.PersistentFlags().StringP("project", "p", ".", "project directory")
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs(append(args, "--project", projectDir))

	if err := cmd.Execute(); err != nil {
		t.Fatalf("report %v = %v (stderr %q)", args, err, stderr.String())
	}
	return stdout.String()
}

func usageEntry(iteration int, role, model, effort string, outcome domain.AttemptOutcome, tokens int, cost float64, basis domain.CostBasis) domain.UsageEntry {
	return domain.UsageEntry{
		Iteration: iteration,
		Role:      role,
		Outcome:   outcome,
		AttemptUsage: domain.AttemptUsage{
			Available:   true,
			Model:       model,
			Effort:      effort,
			InputTokens: tokens,
			CostUSD:     cost,
			CostBasis:   basis,
		},
	}
}

func unavailableEntry(iteration int, role, model, effort string) domain.UsageEntry {
	return domain.UsageEntry{
		Iteration: iteration,
		Role:      role,
		Outcome:   domain.AttemptFailed,
		AttemptUsage: domain.AttemptUsage{
			Available:         false,
			UnavailableReason: "the invocation reported no usage",
			Model:             model,
			Effort:            effort,
		},
	}
}

// reportRuns is the fixture both reports read: one work item completed
// first-pass on the default executor, one that failed twice and escalated to
// completion, and one whose change request never escalated.
func reportRuns() []*domain.ExecutionRun {
	return []*domain.ExecutionRun{
		{
			CRID: "01_alpha", WorkItemID: "alpha-one", SpecRef: "spec-alpha.feat-one",
			Iterations: 1, Outcome: domain.RunCompleted,
			Routing: &domain.RoutingRecord{CRType: domain.CRTypeFeature, Outcome: domain.RoutingPassed},
			Usage: []domain.UsageEntry{
				usageEntry(1, domain.ExecutorRoleDefault, "sonnet-live", "high", domain.AttemptPassed, 1000, 0.5, domain.CostBasisCharged),
			},
		},
		{
			CRID: "02_beta", WorkItemID: "beta-one", SpecRef: "spec-beta.feat-two",
			Iterations: 3, Outcome: domain.RunCompleted,
			Routing: &domain.RoutingRecord{
				CRType: domain.CRTypeEnhancement, OpusExecutionAttempts: 1, Outcome: domain.RoutingPassed,
			},
			Usage: []domain.UsageEntry{
				usageEntry(1, domain.ExecutorRoleDefault, "sonnet-live", "high", domain.AttemptFailed, 1000, 0.5, domain.CostBasisCharged),
				unavailableEntry(2, domain.ExecutorRoleDefault, "sonnet-live", "high"),
				usageEntry(3, domain.ExecutorRoleEscalated, "opus-live", "high", domain.AttemptPassed, 4000, 4.0, domain.CostBasisListPriceEstimate),
			},
		},
	}
}

func TestReportModelsPrintsOneRowPerModelAndEffortPair(t *testing.T) {
	out := runReport(t, reportProject(t, reportRuns()...), "models")

	for _, want := range []string{
		"MODEL", "EFFORT", "ATTEMPTS", "UNAVAIL", "ITEMS", "DONE", "FIRST-PASS", "MEAN ITERS", "TOKENS", "COST/DONE",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output is missing the %q column:\n%s", want, out)
		}
	}
	sonnet := rowFor(t, out, "sonnet-live")
	if !strings.Contains(sonnet, "high") {
		t.Errorf("sonnet row %q does not carry its effort", sonnet)
	}
	// Three attempts, one of them with unreadable usage, so 2,000 tokens - not the
	// 2,000 plus a zero that folding the unreadable one in would produce.
	if !strings.Contains(sonnet, "2,000") {
		t.Errorf("sonnet row %q does not report only the readable attempts' tokens", sonnet)
	}
	if !strings.Contains(sonnet, "100.0%") {
		t.Errorf("sonnet row %q does not report the first-pass rate of the item it concluded", sonnet)
	}
	// Opus concluded the escalated item on its third iteration: a completion, but
	// not a first pass.
	opus := rowFor(t, out, "opus-live")
	if !strings.Contains(opus, "4,000") {
		t.Errorf("opus row %q does not report its tokens", opus)
	}
	if !strings.Contains(opus, "0.0%") || !strings.Contains(opus, "3.0") {
		t.Errorf("opus row %q does not report 0%% first-pass and 3.0 mean iterations to completion", opus)
	}
}

func TestReportModelsSeparatesUnavailableUsageFromZero(t *testing.T) {
	out := runReport(t, reportProject(t, reportRuns()...), "models")

	sonnet := rowFor(t, out, "sonnet-live")
	fields := strings.Fields(sonnet)
	// MODEL EFFORT ATTEMPTS UNAVAIL ...
	if len(fields) < 4 || fields[2] != "3" || fields[3] != "1" {
		t.Errorf("sonnet row %q, want 3 attempts with 1 counted in the unavailable column", sonnet)
	}
	if !strings.Contains(out, "excluded from token and cost totals") {
		t.Errorf("output does not explain that unavailable usage is excluded rather than summed as zero:\n%s", out)
	}
}

func TestReportModelsKeepsSubscriptionCostsOutOfChargedTotals(t *testing.T) {
	out := runReport(t, reportProject(t, reportRuns()...), "models")

	opus := rowFor(t, out, "opus-live")
	if !strings.Contains(opus, "list-est") {
		t.Errorf("opus row %q does not mark its subscription cost as a list-price estimate", opus)
	}
	if strings.Contains(opus, "charged") {
		t.Errorf("opus row %q sums a list-price estimate into a charged figure", opus)
	}
	sonnet := rowFor(t, out, "sonnet-live")
	if !strings.Contains(sonnet, "charged") {
		t.Errorf("sonnet row %q does not mark its api-key cost as charged", sonnet)
	}
	if !strings.Contains(out, "list-price equivalent") {
		t.Errorf("output does not explain the two cost bases:\n%s", out)
	}
}

func TestReportModelsGroupsBySpecAndCRType(t *testing.T) {
	projectDir := reportProject(t, reportRuns()...)

	bySpec := runReport(t, projectDir, "models", "--by", "spec")
	if !strings.Contains(bySpec, "SPEC: spec-alpha") || !strings.Contains(bySpec, "SPEC: spec-beta") {
		t.Errorf("--by spec output is not grouped by spec:\n%s", bySpec)
	}

	byType := runReport(t, projectDir, "models", "--by", "cr_type")
	if !strings.Contains(byType, "CR_TYPE: feature") || !strings.Contains(byType, "CR_TYPE: enhancement") {
		t.Errorf("--by cr_type output is not grouped by change request type:\n%s", byType)
	}
	// The rows inside a group are the same model and effort rows.
	if !strings.Contains(byType, "MODEL") || !strings.Contains(byType, "sonnet-live") {
		t.Errorf("--by cr_type output does not carry the model rows inside its groups:\n%s", byType)
	}
}

func TestReportModelsRejectsUnknownGrouping(t *testing.T) {
	cmd := NewReportCmd()
	cmd.PersistentFlags().StringP("project", "p", ".", "project directory")
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{"models", "--by", "model", "--project", reportProject(t)})

	err := cmd.Execute()

	if err == nil {
		t.Fatal("report models --by model = nil, want an error naming the valid dimensions")
	}
	if !strings.Contains(err.Error(), "spec") || !strings.Contains(err.Error(), "cr_type") {
		t.Errorf("error %q does not name the valid dimensions", err)
	}
}

func TestReportEscalationsRowsAndAggregate(t *testing.T) {
	out := runReport(t, reportProject(t, reportRuns()...), "escalations")

	if !strings.Contains(out, "02_beta") {
		t.Fatalf("output is missing the escalated change request:\n%s", out)
	}
	if strings.Contains(out, "01_alpha") {
		t.Errorf("output includes a change request that never escalated as a row:\n%s", out)
	}
	row := rowFor(t, out, "02_beta")
	if !strings.Contains(row, "sonnet-live") || !strings.Contains(row, "opus-live") {
		t.Errorf("row %q does not carry the model before and the model after escalation", row)
	}
	if !strings.Contains(row, "1,000") || !strings.Contains(row, "4,000") {
		t.Errorf("row %q does not carry the spend before and after escalation", row)
	}
	if !strings.Contains(row, "completed") {
		t.Errorf("row %q does not carry the final outcome", row)
	}

	if !strings.Contains(out, "marginal completion rate 100.0%") {
		t.Errorf("output has no aggregate marginal completion rate:\n%s", out)
	}
	if !strings.Contains(out, "marginal cost per escalation $4.0000 list-est") {
		t.Errorf("output has no aggregate marginal cost:\n%s", out)
	}
	if !strings.Contains(out, "Baseline: 1 of 1") {
		t.Errorf("output does not report the non-escalated baseline:\n%s", out)
	}
}

// What an outcome cost is two figures, not one: dollars charged under api-key auth
// and the list-price estimate of tokens spent under subscription auth. The report
// prints them in their own columns and never adds them together.
func TestReportOutcomesCostByAuthMode(t *testing.T) {
	out := runReport(t, reportProject(t, reportRuns()...), "outcomes")

	if !strings.Contains(out, "2 run record(s) read across 2 change request(s)") {
		t.Fatalf("output does not say what it read:\n%s", out)
	}
	row := rowFor(t, out, "completed")
	// 01_alpha spent $0.50 charged, 02_beta $0.50 charged plus a $4.00 list-price
	// estimate, over two change requests that both completed.
	if !strings.Contains(row, "$1.0000") || !strings.Contains(row, "$4.0000") {
		t.Errorf("row %q does not carry the charged dollars and the list-price estimate apart", row)
	}
	if !strings.Contains(row, "$0.5000") || !strings.Contains(row, "$2.0000") {
		t.Errorf("row %q does not carry either cost per change request", row)
	}
	if strings.Contains(row, "$5.0000") {
		t.Errorf("row %q sums charged dollars with a list-price estimate into one figure", row)
	}
	// The escalated change request's second attempt reported no usage, so it is
	// counted and excluded rather than folded in as zero.
	if !strings.Contains(row, "6,000") {
		t.Errorf("row %q does not carry the measured token total", row)
	}
	if !strings.Contains(out, "CHARGED") || !strings.Contains(out, "LIST-EST") {
		t.Errorf("output has no column per cost basis:\n%s", out)
	}
}

func TestBothReportsPrintTheObservationalCaveat(t *testing.T) {
	projectDir := reportProject(t, reportRuns()...)

	for _, args := range [][]string{{"models"}, {"escalations"}, {"outcomes"}} {
		out := runReport(t, projectDir, args...)
		if !strings.Contains(out, analysis.ObservationalCaveat) {
			t.Errorf("report %v does not print the observational caveat:\n%s", args, out)
		}
		if !strings.Contains(out, "selected for difficulty") {
			t.Errorf("report %v does not say escalated change requests are selected for difficulty:\n%s", args, out)
		}
	}
}

func TestReportsOnEmptyRepositorySaySoAndExitZero(t *testing.T) {
	projectDir := reportProject(t)

	for _, args := range [][]string{{"models"}, {"escalations"}, {"outcomes"}} {
		// runReport fails the test on a non-nil error, which is the exit-zero assertion.
		out := runReport(t, projectDir, args...)
		if !strings.Contains(out, "No run records yet") {
			t.Errorf("report %v on an empty repository printed:\n%s", args, out)
		}
	}
}

// rowFor returns the first output line containing needle.
func rowFor(t *testing.T, out, needle string) string {
	t.Helper()
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, needle) {
			return line
		}
	}
	t.Fatalf("no line contains %q:\n%s", needle, out)
	return ""
}
