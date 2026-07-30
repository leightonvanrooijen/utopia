package ralph

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/leightonvanrooijen/utopia/internal/domain"
	"github.com/leightonvanrooijen/utopia/internal/validators"
)

// aggregatedFeedback is shaped exactly as validators.AggregateResults builds it,
// so the tests below assert on the string a real failing validator run carries -
// validator ID included.
const aggregatedFeedback = "Validator go-style failed:\nexported func Foo missing a doc comment\n\n"

func TestPrintFailureBlock_FramesContentInMatchingDelimiters(t *testing.T) {
	out := captureStdout(t, func() { printFailureBlock("verification", "--- FAIL: TestFoo (0.00s)") })

	want := "\n--- Failure Output: verification ---\n--- FAIL: TestFoo (0.00s)\n--- End Failure Output: verification ---\n\n"
	if out != want {
		t.Errorf("block = %q, want %q", out, want)
	}
}

func TestPrintFailureBlock_EmptyContentPrintsNoFrame(t *testing.T) {
	for _, content := range []string{"", "  \n\t"} {
		out := captureStdout(t, func() { printFailureBlock("verification", content) })
		if out != "" {
			t.Errorf("content %q must print nothing, got %q", content, out)
		}
	}
}

// TestVerificationFailure_PrintsFailureOutputBlock covers the verification path:
// the failing command's output, already truncated by the verifier and about to be
// assigned to item.LastFailureOutput, is shown verbatim inside a block naming
// verification as the source. The loop body calls this with exactly the string it
// then injects, so the assertion here is that the block does not alter it.
func TestVerificationFailure_PrintsFailureOutputBlock(t *testing.T) {
	verifyOutput := "--- FAIL: TestUnify (0.01s)\n    want one block, got two"

	out := captureStdout(t, func() { printFailureBlock(failureSourceVerification, verifyOutput) })

	if !strings.Contains(out, verifyOutput) {
		t.Errorf("verification output must be printed verbatim, got:\n%s", out)
	}
	if !strings.Contains(out, "Failure Output: verification") {
		t.Errorf("block must name verification as the source, got:\n%s", out)
	}
	if got := strings.Count(out, "--- Failure Output:"); got != 1 {
		t.Errorf("verification must print exactly one block, got %d:\n%s", got, out)
	}
}

// failingValidatorSub mirrors the subscription CompileValidators produces for a
// trigger, with the action replaced by the ConnectorResult a failing validator
// run reports: the aggregated feedback as stdout, a non-nil error, and the
// aggregate riding along.
func failingValidatorSub(trigger domain.RunTrigger, launch, join string) Subscription {
	name := "validators:" + string(trigger)
	return Subscription{
		Name:   name,
		Launch: launch,
		Join:   join,
		Action: instantAction(ConnectorResult{
			Name:      name,
			Stdout:    aggregatedFeedback,
			Err:       errors.New("validators failed"),
			Aggregate: &validators.AggregateResult{Passed: false, Feedback: aggregatedFeedback},
		}),
	}
}

// assertOneFeedbackBlock checks the printed output carries the validator feedback
// exactly once, inside one failure-output block naming the subscription, and that
// what was printed is what the gate error hands the next prompt.
func assertOneFeedbackBlock(t *testing.T, out, subName string, gateErr error) {
	t.Helper()

	var ge *GateError
	if !errors.As(gateErr, &ge) {
		t.Fatalf("expected a *GateError from the blocked join, got %v", gateErr)
	}
	if !strings.Contains(out, strings.TrimSpace(ge.Stdout)) {
		t.Errorf("printed block must carry the feedback the prompt is injected with %q, got:\n%s", ge.Stdout, out)
	}
	if !strings.Contains(out, "Validator go-style failed:") {
		t.Errorf("printed feedback must keep the validator ID, got:\n%s", out)
	}
	if got := strings.Count(out, "--- Failure Output: "+subName+" ---"); got != 1 {
		t.Errorf("expected exactly one failure-output block for %s, got %d:\n%s", subName, got, out)
	}
	if got := strings.Count(out, "Validator go-style failed:"); got != 1 {
		t.Errorf("feedback must be printed once, not duplicated, got %d:\n%s", got, out)
	}
}

// TestAfterWorkitemValidatorFailure_PrintsFeedbackBlock is the regression test for
// the path that had no print of its own: after-workitem validators launch
// speculatively at workitem-completion-claimed and are joined at
// workitem-verified, and their feedback must reach the terminal when they block
// that join - not only the AI's next prompt.
func TestAfterWorkitemValidatorFailure_PrintsFeedbackBlock(t *testing.T) {
	sub := failingValidatorSub(domain.RunAfterWorkitem, EventWorkItemCompletionClaimed, EventWorkItemVerified)
	en := NewEngine([]Subscription{sub})

	if err := en.Emit(context.Background(), Event{Name: EventWorkItemCompletionClaimed}); err != nil {
		t.Fatalf("launch emit must not block: %v", err)
	}

	var gateErr error
	out := captureStdout(t, func() {
		gateErr = en.Emit(context.Background(), Event{Name: EventWorkItemVerified})
	})
	if gateErr == nil {
		t.Fatal("failing after-workitem validators must block workitem-verified")
	}

	assertOneFeedbackBlock(t, out, sub.Name, gateErr)
}

// TestAfterPhaseValidatorFailure_PrintsFeedbackBlock covers the gating shape:
// after-phase validators launch and join on phase-verified, so one dispatch runs
// them and their feedback is printed once as the gate blocks.
func TestAfterPhaseValidatorFailure_PrintsFeedbackBlock(t *testing.T) {
	sub := failingValidatorSub(domain.RunAfterPhase, EventPhaseVerified, EventPhaseVerified)
	en := NewEngine([]Subscription{sub})

	var gateErr error
	out := captureStdout(t, func() {
		gateErr = en.Emit(context.Background(), Event{Name: EventPhaseVerified})
	})
	if gateErr == nil {
		t.Fatal("failing after-phase validators must block phase-verified")
	}

	assertOneFeedbackBlock(t, out, sub.Name, gateErr)
}

// TestResolutionLedger_PassingHandleOutputStaysIndented pins the other half of the
// ledger's behaviour: only a failure gets the block. A connector that succeeded
// while printing chatter keeps its output indented under its ledger line, so the
// block stays a signal that something failed.
func TestResolutionLedger_PassingHandleOutputStaysIndented(t *testing.T) {
	sub := Subscription{
		Name:   "slack",
		Launch: EventWorkItemVerified,
		Join:   EventWorkItemVerified,
		Action: instantAction(ConnectorResult{Name: "slack", Stdout: "posted to #builds"}),
	}
	en := NewEngine([]Subscription{sub})

	out := captureStdout(t, func() {
		if err := en.Emit(context.Background(), Event{Name: EventWorkItemVerified}); err != nil {
			t.Fatalf("passing connector must not block: %v", err)
		}
	})

	if strings.Contains(out, "--- Failure Output:") {
		t.Errorf("a passing handle must not print a failure-output block, got:\n%s", out)
	}
	if !strings.Contains(out, "    posted to #builds") {
		t.Errorf("passing handle output must stay indented under the ledger line, got:\n%s", out)
	}
}
