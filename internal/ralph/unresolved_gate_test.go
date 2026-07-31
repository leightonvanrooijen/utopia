package ralph

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/leightonvanrooijen/utopia/internal"
	"github.com/leightonvanrooijen/utopia/internal/domain"
	"github.com/leightonvanrooijen/utopia/internal/ui"
	"github.com/leightonvanrooijen/utopia/internal/validators"
)

// A gate is unresolved only when nothing was concluded about the code. One real
// verdict alongside the faults makes it a validation failure like any other,
// because a validator did read the diff and did disapprove.
func TestGateUnresolved(t *testing.T) {
	errored := &validators.AggregateResult{
		Errors: []validators.ValidatorError{{ID: "style", Err: errNoBinary}},
	}
	mixed := &validators.AggregateResult{
		FailureClass: validators.FailureMechanical,
		Failures:     []validators.ValidatorFailure{{ID: "arch", Verdict: &validators.Verdict{FailureClass: validators.FailureMechanical}}},
		Errors:       []validators.ValidatorError{{ID: "style", Err: errNoBinary}},
	}
	failed := &validators.AggregateResult{
		FailureClass: validators.FailureComprehension,
		Failures:     []validators.ValidatorFailure{{ID: "arch", Verdict: &validators.Verdict{FailureClass: validators.FailureComprehension}}},
	}

	if !gateUnresolved(errored) {
		t.Error("an error-only aggregate must be unresolved: no verdict was reached")
	}
	if gateUnresolved(mixed) {
		t.Error("an aggregate carrying a verdict must route on that verdict, whatever errored beside it")
	}
	if gateUnresolved(failed) {
		t.Error("a failure-only aggregate is a validation failure, not an unresolved gate")
	}
	if gateUnresolved(nil) {
		t.Error("a gating connector carries no aggregate and is not an unresolved gate")
	}
}

// The unresolved streak is bounded by the invocation-error cap, and the halt has
// to say the validators could not run rather than that they rejected the work.
func TestChargeUnresolvedGate(t *testing.T) {
	caps := testCaps() // InvocationErrors: 3

	t.Run("gates below the cap are retried", func(t *testing.T) {
		item := &domain.WorkItem{ID: "item"}
		for i := 1; i < caps.InvocationErrors; i++ {
			if halt := chargeUnresolvedGate(item, caps, errNoBinary); halt != nil {
				t.Fatalf("gate %d halted the item, want it retried under the cap", i)
			}
			if item.UnresolvedGateCount != i {
				t.Errorf("UnresolvedGateCount = %d, want %d", item.UnresolvedGateCount, i)
			}
		}
	})

	t.Run("the gate at the cap halts, naming the validator fault", func(t *testing.T) {
		item := &domain.WorkItem{ID: "item", UnresolvedGateCount: caps.InvocationErrors - 1}

		halt := chargeUnresolvedGate(item, caps, errors.New("validator style could not run: no such file"))

		if halt == nil {
			t.Fatal("halt = nil, want the item halted at the cap")
		}
		if halt.Cap != "escalation.invocation_errors" || halt.Limit != caps.InvocationErrors {
			t.Errorf("halt reports %s = %d, want the invocation-error cap", halt.Cap, halt.Limit)
		}
		msg := halt.Error()
		for _, want := range []string{"could not run", "never judged", "validator style"} {
			if !strings.Contains(msg, want) {
				t.Errorf("halt message %q missing %q, want the validator fault named", msg, want)
			}
		}
		// The distinction the halt exists to make: a validator that could not run is
		// not a validator that rejected the work.
		for _, unwanted := range []string{"comprehension", "rejected"} {
			if strings.Contains(msg, unwanted) {
				t.Errorf("halt message %q reads as a verdict about the work", msg)
			}
		}
	})

	t.Run("a gate that reached a verdict clears the streak", func(t *testing.T) {
		item := &domain.WorkItem{ID: "item", UnresolvedGateCount: 2}

		clearUnresolvedGates(item)

		if item.UnresolvedGateCount != 0 {
			t.Errorf("UnresolvedGateCount = %d, want the streak cleared", item.UnresolvedGateCount)
		}
	})
}

// The cause carries what a person has to go and fix: which validator, and the
// fault that stopped it.
func TestUnresolvedGateCause(t *testing.T) {
	agg := &validators.AggregateResult{Errors: []validators.ValidatorError{
		{ID: "style", Err: errors.New("failed to get git diff")},
		{ID: "arch", Err: errors.New("validator timed out after 5m")},
	}}

	got := unresolvedGateCause(agg).Error()

	for _, want := range []string{"validator style could not run: failed to get git diff", "validator arch could not run: validator timed out after 5m"} {
		if !strings.Contains(got, want) {
			t.Errorf("cause %q missing %q", got, want)
		}
	}
	if unresolvedGateCause(nil) != nil {
		t.Error("no aggregate, no cause")
	}
}

// The log line reports no class and no model change, because the iteration
// concluded nothing and the next attempt runs on whatever the item already ran on.
func TestUnresolvedGateLogLine(t *testing.T) {
	caps := testCaps()

	retry := unresolvedGateLogLine(&domain.WorkItem{UnresolvedGateCount: 1}, caps, 2, 10)
	for _, want := range []string{"class=none", "the validators could not run", "unresolved=1/3", "iteration=2/10", "route=unresolved-retry", "model=unchanged"} {
		if !strings.Contains(retry, want) {
			t.Errorf("log line %q missing %q", retry, want)
		}
	}

	halted := unresolvedGateLogLine(&domain.WorkItem{UnresolvedGateCount: 3}, caps, 3, 10)
	if !strings.Contains(halted, "escalation.invocation_errors exhausted") {
		t.Errorf("log line %q must name the exhausted cap at the halt", halted)
	}
}

// A validator that cannot be run must not spend the escalation budget: the item
// retries the gate, keeps the verdict it already had, and halts on the
// invocation-error cap with the validator fault named. Driven through Execute
// because the defect was in how the loop routed the gate rather than in any
// helper.
func TestExecute_UnresolvedGateHaltsWithoutEscalating(t *testing.T) {
	projectDir := t.TempDir()
	store := internal.NewYAMLStore(filepath.Join(projectDir, ".utopia"))
	const specID = "cr-validator-fault"

	saveEscalationCR(t, store, specID)
	// A genuine verdict from an earlier iteration, which the unresolved gates below
	// must not overwrite with an infrastructure fault.
	const priorVerdict = "Validator arch failed:\nthe repository layer is bypassed\n"
	item := &domain.WorkItem{ID: "wi-1", Order: 1, Status: domain.WorkItemPending,
		Prompt: "do the thing", LastValidatorFeedback: priorVerdict}
	if err := store.SaveWorkItemForSpec(specID, item); err != nil {
		t.Fatalf("SaveWorkItemForSpec() = %v", err)
	}
	initGitRepo(t, projectDir)
	// Claude answers every executor invocation with a completion token, and fails
	// to run at all for the validator's - so the gate blocks carrying an
	// invocation error and no verdict.
	markerClaudeOnPath(t, validatorMarker)
	config := &domain.Config{
		Verification: domain.VerificationConfig{MaxIterations: 20},
		Validators:   []domain.ValidatorConfig{{Path: brokenValidator(t, projectDir), Always: true}},
	}

	var stdout, stderr bytes.Buffer
	var result *Result
	var err error
	captureStdout(t, func() {
		result, err = Execute(context.Background(), specID, store, config, projectDir, "",
			Overrides{Out: ui.NewPrinter(&stdout, &stderr)})
	})
	if err != nil {
		t.Fatalf("Execute() error = %v, want the halted item reported on the result", err)
	}
	if len(result.NeedsHuman) != 1 {
		t.Fatalf("NeedsHuman = %v (completed %d), want the item halted for a person\nstdout: %s", result.NeedsHuman, result.Completed, stdout.String())
	}

	halted := reloadWorkItem(t, store, specID, item.ID)
	if halted.Status != domain.WorkItemNeedsHuman {
		t.Errorf("Status = %q, want %q", halted.Status, domain.WorkItemNeedsHuman)
	}
	if halted.UnresolvedGateCount != DefaultInvocationErrorCap {
		t.Errorf("UnresolvedGateCount = %d, want the default cap of %d", halted.UnresolvedGateCount, DefaultInvocationErrorCap)
	}
	// None of the routing state moved: nothing was concluded about the code, so
	// there was nothing to escalate on.
	if halted.ComprehensionCount != 0 {
		t.Errorf("ComprehensionCount = %d, want it unmoved by a validator that could not run", halted.ComprehensionCount)
	}
	if halted.OpusExecutionAttempts != 0 {
		t.Errorf("OpusExecutionAttempts = %d, want no escalated attempt", halted.OpusExecutionAttempts)
	}
	if halted.ScopingEscalationCount != 0 {
		t.Errorf("ScopingEscalationCount = %d, want no change request rewritten", halted.ScopingEscalationCount)
	}
	if len(halted.FailureConclusions) != 0 {
		t.Errorf("FailureConclusions = %v, want none: no validator concluded anything", halted.FailureConclusions)
	}
	if halted.LastValidatorFeedback != priorVerdict {
		t.Errorf("LastValidatorFeedback = %q, want the earlier verdict %q kept", halted.LastValidatorFeedback, priorVerdict)
	}
	// The attempts ran and spent, and each is recorded as having reached no verdict.
	if len(halted.ExecutorAttempts) != DefaultInvocationErrorCap {
		t.Fatalf("ExecutorAttempts = %d, want one per unresolved gate", len(halted.ExecutorAttempts))
	}
	for i, a := range halted.ExecutorAttempts {
		if a.Outcome != domain.AttemptErrored {
			t.Errorf("attempt %d outcome = %q, want %q", i+1, a.Outcome, domain.AttemptErrored)
		}
		if a.FailureClass != "" {
			t.Errorf("attempt %d class = %q, want none: no verdict was reached", i+1, a.FailureClass)
		}
	}
	// Progress lines are diagnostics, so they land on the printer's stderr writer.
	if out := stderr.String(); !strings.Contains(out, "the validators could not run") {
		t.Errorf("run output must say the validators could not run, got:\n%s", out)
	}
}

// brokenValidator writes an after-workitem validator and returns its config path.
// It is always-run so the relevance router is skipped: what is under test is the
// gate, not which validators reach it.
func brokenValidator(t *testing.T, projectDir string) string {
	t.Helper()

	dir := filepath.Join(projectDir, ".utopia", "validators")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("failed to create validators dir: %v", err)
	}
	content := "---\nid: style\n---\n" + validatorMarker + " Review {{changed_files}}\n"
	if err := os.WriteFile(filepath.Join(dir, "style.md"), []byte(content), 0o644); err != nil {
		t.Fatalf("failed to write validator: %v", err)
	}
	return "validators/style.md"
}

// validatorMarker rides in the validator's prompt so the stand-in claude can tell
// a validator invocation from an executor one and fail only the former.
const validatorMarker = "STYLE-VALIDATOR-MARKER"

// markerClaudeOnPath installs a stand-in claude that exits non-zero whenever the
// prompt carries the marker and answers with a completion token otherwise. It goes
// on PATH because Execute builds its own *internal.CLI, so only a real spawn
// produces a real invocation failure.
func markerClaudeOnPath(t *testing.T, marker string) {
	t.Helper()

	dir := t.TempDir()
	script := "#!/bin/sh\n" +
		"case \"$*\" in *" + marker + "*)\n" +
		"  echo 'claude: connection reset' >&2\n" +
		"  exit 1\n" +
		"esac\n" +
		`echo '{"type":"system","subtype":"init","model":"claude-test"}'` + "\n" +
		`echo '{"type":"result","subtype":"success","result":"done <COMPLETE>"}'` + "\n"
	if err := os.WriteFile(filepath.Join(dir, "claude"), []byte(script), 0o755); err != nil {
		t.Fatalf("failed to write fake claude: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// initGitRepo makes projectDir a repository with one commit, so the validators'
// diff computes and the gate blocks on the validator invocation under test rather
// than on a missing repository.
func initGitRepo(t *testing.T, dir string) {
	t.Helper()

	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v = %v: %s", args, err, out)
		}
	}
	run("init")
	run("config", "user.email", "test@example.com")
	run("config", "user.name", "Test User")
	if err := os.WriteFile(filepath.Join(dir, "file.txt"), []byte("initial\n"), 0o644); err != nil {
		t.Fatalf("failed to write file: %v", err)
	}
	run("add", ".")
	run("commit", "-m", "initial")
}
