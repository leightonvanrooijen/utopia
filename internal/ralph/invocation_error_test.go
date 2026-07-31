package ralph

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/leightonvanrooijen/utopia/internal"
	"github.com/leightonvanrooijen/utopia/internal/domain"
	"github.com/leightonvanrooijen/utopia/internal/ui"
)

// The consecutive-error counter bounds a fault that is happening now, and the
// halt it produces has to name that fault rather than a comprehension failure:
// nothing about the change request changes the outcome of a claude that will not
// start.
func TestChargeInvocationError(t *testing.T) {
	caps := testCaps() // InvocationErrors: 3

	t.Run("errors below the cap keep the item running", func(t *testing.T) {
		item := &domain.WorkItem{ID: "item"}
		for i := 1; i < caps.InvocationErrors; i++ {
			if halt := chargeInvocationError(item, caps, errNoBinary); halt != nil {
				t.Fatalf("error %d halted the item, want it retried under the cap", i)
			}
			if item.InvocationErrorCount != i {
				t.Errorf("InvocationErrorCount = %d, want %d", item.InvocationErrorCount, i)
			}
		}
	})

	t.Run("the error at the cap halts, naming the invocation failure", func(t *testing.T) {
		item := &domain.WorkItem{ID: "item", InvocationErrorCount: caps.InvocationErrors - 1}

		halt := chargeInvocationError(item, caps, errNoBinary)

		if halt == nil {
			t.Fatal("halt = nil, want the item halted at the cap")
		}
		if halt.Cap != "escalation.invocation_errors" || halt.Limit != caps.InvocationErrors {
			t.Errorf("halt reports %s = %d, want the invocation-error cap", halt.Cap, halt.Limit)
		}
		msg := halt.Error()
		for _, want := range []string{"invocation", "reached a verdict", "claude"} {
			if !strings.Contains(msg, want) {
				t.Errorf("halt message %q missing %q, want the invocation failure named", msg, want)
			}
		}
		if strings.Contains(msg, "comprehension") {
			t.Errorf("halt message %q reads as a comprehension failure", msg)
		}
	})

	t.Run("an invocation that ran clears the streak", func(t *testing.T) {
		item := &domain.WorkItem{ID: "item", InvocationErrorCount: 2}

		clearInvocationErrors(item)

		if item.InvocationErrorCount != 0 {
			t.Errorf("InvocationErrorCount = %d, want the streak cleared", item.InvocationErrorCount)
		}
	})

	t.Run("a raised cap allows further errors", func(t *testing.T) {
		item := &domain.WorkItem{ID: "item", InvocationErrorCount: 3}
		if halt := chargeInvocationError(item, EscalationCaps{InvocationErrors: 5}, errNoBinary); halt != nil {
			t.Errorf("halt = %v, want the error allowed under the raised cap", halt)
		}
	})
}

// The refund is of routing budget only: an escalated attempt that produced no
// verdict gives its charge back, and an unescalated one never had a charge.
func TestRefundEscalatedAttempt(t *testing.T) {
	item := &domain.WorkItem{ID: "item", ComprehensionCount: 1, OpusExecutionAttempts: 2}

	refundEscalatedAttempt(item, true)
	if item.OpusExecutionAttempts != 1 {
		t.Errorf("OpusExecutionAttempts = %d, want the charge returned", item.OpusExecutionAttempts)
	}

	refundEscalatedAttempt(item, false)
	if item.OpusExecutionAttempts != 1 {
		t.Errorf("OpusExecutionAttempts = %d, want an uncharged attempt left alone", item.OpusExecutionAttempts)
	}
}

// A claude that fails every time must halt the work item on its own counter,
// without spending the escalation budget on it. Driven through Execute against a
// stand-in binary that exits non-zero, because the defect was in how the loop
// treated the error rather than in any helper: a test that stopped at the helpers
// would have passed against it.
func TestExecute_InvocationErrorsHaltOnTheirOwnCap(t *testing.T) {
	projectDir := t.TempDir()
	store := internal.NewYAMLStore(filepath.Join(projectDir, ".utopia"))
	const specID = "cr-fault"

	saveEscalationCR(t, store, specID)
	// Already escalated, so every attempt below is charged against the
	// escalated-execution cap of 2 - which two crashed invocations used to exhaust.
	item := &domain.WorkItem{ID: "wi-1", Order: 1, Status: domain.WorkItemPending,
		Prompt: "do the thing", ComprehensionCount: 1}
	if err := store.SaveWorkItemForSpec(specID, item); err != nil {
		t.Fatalf("SaveWorkItemForSpec() = %v", err)
	}
	// Every invocation fails: the fault is the binary, not the work.
	failingClaudeOnPath(t, 99)

	var stdout, stderr bytes.Buffer
	var result *Result
	var err error
	captureStdout(t, func() {
		result, err = Execute(context.Background(), specID, store,
			&domain.Config{Verification: domain.VerificationConfig{MaxIterations: 20}}, projectDir, "",
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
	if halted.InvocationErrorCount != DefaultInvocationErrorCap {
		t.Errorf("InvocationErrorCount = %d, want the default cap of %d", halted.InvocationErrorCount, DefaultInvocationErrorCap)
	}
	// The routing budget is untouched: no attempt reached a verdict, so none of it
	// was evidence the executor got anything wrong.
	if halted.OpusExecutionAttempts != 0 {
		t.Errorf("OpusExecutionAttempts = %d, want every crashed attempt refunded", halted.OpusExecutionAttempts)
	}
	if halted.ComprehensionCount != 1 {
		t.Errorf("ComprehensionCount = %d, want it unmoved by invocation errors", halted.ComprehensionCount)
	}
	if halted.ScopingEscalationCount != 0 {
		t.Errorf("ScopingEscalationCount = %d, want no escalation on an invocation fault", halted.ScopingEscalationCount)
	}
	// The accounting is kept, though: the attempts ran and spent whatever they
	// spent, and each is recorded as having reached no verdict.
	if len(halted.ExecutorAttempts) != DefaultInvocationErrorCap {
		t.Fatalf("ExecutorAttempts = %d, want one per errored invocation", len(halted.ExecutorAttempts))
	}
	for i, a := range halted.ExecutorAttempts {
		if a.Outcome != domain.AttemptErrored {
			t.Errorf("attempt %d outcome = %q, want %q", i+1, a.Outcome, domain.AttemptErrored)
		}
		if a.Usage == nil {
			t.Errorf("attempt %d has no usage, want the spend recorded", i+1)
		}
	}
	// The iterations stand, so an item cannot run unbounded on errors alone.
	if halted.IterationCount != DefaultInvocationErrorCap {
		t.Errorf("IterationCount = %d, want each errored attempt counted", halted.IterationCount)
	}
}

// An intermittent fault must not accumulate across an otherwise healthy run: the
// counter is a streak, so the invocation that runs clears it.
func TestExecute_SuccessfulInvocationResetsTheErrorStreak(t *testing.T) {
	projectDir := t.TempDir()
	store := internal.NewYAMLStore(filepath.Join(projectDir, ".utopia"))
	const specID = "cr-flaky"

	item := &domain.WorkItem{ID: "wi-1", Order: 1, Status: domain.WorkItemPending, Prompt: "do the thing"}
	if err := store.SaveWorkItemForSpec(specID, item); err != nil {
		t.Fatalf("SaveWorkItemForSpec() = %v", err)
	}
	// Two faults, then an invocation that runs, then two more: five in total, above
	// the cap of three, but never three in a row.
	failingClaudeOnPath(t, 2)

	var stdout, stderr bytes.Buffer
	var result *Result
	var err error
	captureStdout(t, func() {
		result, err = Execute(context.Background(), specID, store,
			&domain.Config{Verification: domain.VerificationConfig{MaxIterations: 20}}, projectDir, "",
			Overrides{Out: ui.NewPrinter(&stdout, &stderr)})
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if result.Completed != 1 {
		t.Fatalf("Completed = %d, want the item to survive the intermittent faults (needs human: %v)", result.Completed, result.NeedsHuman)
	}

	done := reloadWorkItem(t, store, specID, item.ID)
	// The two faults happened and are on the record, so the reset is what carried
	// the item through rather than the faults never occurring.
	if len(done.ExecutorAttempts) != 3 {
		t.Fatalf("ExecutorAttempts = %d, want the two errored attempts and the one that ran", len(done.ExecutorAttempts))
	}
	for i, want := range []domain.AttemptOutcome{domain.AttemptErrored, domain.AttemptErrored, domain.AttemptPassed} {
		if got := done.ExecutorAttempts[i].Outcome; got != want {
			t.Errorf("attempt %d outcome = %q, want %q", i+1, got, want)
		}
	}
	if done.InvocationErrorCount != 0 {
		t.Errorf("InvocationErrorCount = %d, want the streak cleared by the invocation that ran", done.InvocationErrorCount)
	}
}

// errNoBinary stands in for whatever the CLI reports when the subprocess never
// ran. The counter does not read the error, only that there was one.
var errNoBinary = errors.New("claude prompt failed: exit status 1")

// failingClaudeOnPath installs a stand-in claude that exits non-zero for the
// first failures invocations and then answers with a completion token. It goes on
// PATH for the same reason the arg-recording one does: Execute builds its own
// *internal.CLI, so only a real spawn produces a real invocation failure.
func failingClaudeOnPath(t *testing.T, failures int) {
	t.Helper()

	dir := t.TempDir()
	countPath := filepath.Join(dir, "count")
	script := "#!/bin/sh\n" +
		"printf 'x' >> " + countPath + "\n" +
		"if [ $(wc -c < " + countPath + ") -le " + strconv.Itoa(failures) + " ]; then\n" +
		`  echo 'claude: connection reset' >&2` + "\n" +
		"  exit 1\n" +
		"fi\n" +
		`echo '{"type":"system","subtype":"init","model":"claude-test"}'` + "\n" +
		`echo '{"type":"result","subtype":"success","result":"done <COMPLETE>"}'` + "\n"
	if err := os.WriteFile(filepath.Join(dir, "claude"), []byte(script), 0o755); err != nil {
		t.Fatalf("failed to write fake claude: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// reloadWorkItem reads the item back from disk, which is where the counters the
// caps bound have to survive: the loop persists them every iteration so a resume
// sees the same bounds.
func reloadWorkItem(t *testing.T, store *internal.YAMLStore, specID, id string) *domain.WorkItem {
	t.Helper()

	items, err := store.ListWorkItemsForSpec(specID)
	if err != nil {
		t.Fatalf("ListWorkItemsForSpec() = %v", err)
	}
	for _, it := range items {
		if it.ID == id {
			return it
		}
	}
	t.Fatalf("work item %s not found in %s", id, specID)
	return nil
}

// saveEscalationCR persists the change request an escalated attempt rebuilds its
// context from. Without it the escalated prompt cannot be built and the loop
// would leave on that error rather than on the invocation faults under test.
func saveEscalationCR(t *testing.T, store *internal.YAMLStore, crID string) {
	t.Helper()

	cr := &domain.ChangeRequest{
		ID:     crID,
		Type:   domain.CRTypeBugfix,
		Title:  "Stop harness failures from spending the escalation budget",
		Status: domain.ChangeRequestInProgress,
	}
	if err := store.SaveChangeRequest(cr); err != nil {
		t.Fatalf("SaveChangeRequest: %v", err)
	}
}
