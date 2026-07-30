package ralph

import (
	"strings"
	"testing"

	"github.com/leightonvanrooijen/utopia/internal"
	"github.com/leightonvanrooijen/utopia/internal/domain"
	"github.com/leightonvanrooijen/utopia/internal/validators"
)

// The evidence an escalated attempt is allowed to carry, and the diff it is
// handed as the last attempt's final state.
const (
	escalationDiagnosis      = "'escalation' was read as executor escalation, the spec means scoping escalation"
	escalationIntent         = "Rewrite the change request rather than retrying the executor"
	escalationVerifyOutput   = "--- FAIL: TestScopingEscalation (0.00s)\n    want a rewrite, got a retry"
	escalationValidatorOut   = "Validator spec-fidelity failed:\n<VERDICT>{\"verdict\":\"fail\"}</VERDICT>"
	escalationFinalDiff      = "--- a/internal/ralph/escalation.go\n+++ b/internal/ralph/escalation.go\n+// the last attempt's final state"
	escalationPartialDiff    = "+// SENTINEL a partial diff from halfway through iteration 2"
	escalationModelReasoning = "SENTINEL let me think about this differently, maybe the cap is the problem"
	escalationToolCallLog    = "SENTINEL tool: Bash(go test ./...) -> exit 1"
)

// escalatedItem is a work item mid-escalation: it has failed comprehension
// validation once, so the next attempt runs on the escalated executor, and it
// carries the conclusions of the failures behind it.
func escalatedItem() *domain.WorkItem {
	item := scopingItem()
	item.ComprehensionCount = 1
	item.LastFailureOutput = escalationVerifyOutput
	item.LastValidatorFeedback = escalationValidatorOut
	item.FailureConclusions = []domain.FailureConclusion{
		{
			Iteration:       4,
			ValidatorID:     "spec-fidelity",
			FailureClass:    string(validators.FailureComprehension),
			Diagnosis:       escalationDiagnosis,
			CorrectedIntent: escalationIntent,
		},
		{
			Iteration:    5,
			ValidatorID:  "go-standards",
			FailureClass: string(validators.FailureMechanical),
			Diagnosis:    "the exported function ships without a doc comment",
		},
	}
	return item
}

// plantPriorAttemptTranscript writes the run transcript a failed attempt leaves
// behind, carrying exactly what an escalated context must not replay: the model's
// reasoning, the partial diffs it went through, and its tool-call log.
func plantPriorAttemptTranscript(store *internal.YAMLStore, crID string, item *domain.WorkItem) {
	transcript := strings.Join([]string{
		"--- iteration 4 ---",
		escalationModelReasoning,
		escalationToolCallLog,
		escalationPartialDiff,
	}, "\n")
	writeRunTranscript(store, crID, item, recorderWith("", transcript), domain.RunFailed)
}

func TestEscalatedPrompt_CarriesTheSpecificationAndTheEvidence(t *testing.T) {
	store, cr := scopingFixture(t)
	item := escalatedItem()

	prompt, err := buildEscalatedPrompt(store, item, cr.ID, escalationFinalDiff)
	if err != nil {
		t.Fatalf("buildEscalatedPrompt: %v", err)
	}

	for _, want := range []string{
		"Route validation failures on failure class",                                   // the change request itself
		"A comprehension failure is one the same executor cannot fix by trying harder", // the spec excerpt
		"Every escalation path carries a configurable cap",                             // the ADR excerpt
		escalationDiagnosis, // each prior failure's diagnosis
		escalationIntent,    // and its corrected intent
		"the exported function ships without a doc comment",
		escalationFinalDiff,                      // the final diff from the last attempt
		escalationVerifyOutput,                   // verbatim verification output
		escalationValidatorOut,                   // verbatim validator output
		"## TASK\n\nRewrite the change request.", // the work item itself
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("escalated prompt missing %q", want)
		}
	}
}

// The criterion is about what was sent, so it is asserted against the constructed
// prompt rather than left to inspection: a prior attempt's transcript sits on
// disk, and none of it may reach the escalated model.
func TestEscalatedPrompt_ExcludesThePriorAttemptTranscript(t *testing.T) {
	store, cr := scopingFixture(t)
	item := escalatedItem()
	plantPriorAttemptTranscript(store, cr.ID, item)

	prompt, err := buildEscalatedPrompt(store, item, cr.ID, escalationFinalDiff)
	if err != nil {
		t.Fatalf("buildEscalatedPrompt: %v", err)
	}

	assertNoPriorTranscript(t, "escalated prompt", prompt)
}

// A scoping escalation is an escalation, so its context is built by the same
// rules: the same evidence, and the same absent transcript.
func TestScoperPrompt_ExcludesThePriorAttemptTranscript(t *testing.T) {
	store, cr := scopingFixture(t)
	s := &scoper{store: store, model: "opus"}
	item := escalatedItem()
	plantPriorAttemptTranscript(store, cr.ID, item)

	prompt, err := s.buildPrompt(item, cr, cr.ID, "/tmp/rewrite.yaml", scopingError(), escalationFinalDiff)
	if err != nil {
		t.Fatalf("buildPrompt: %v", err)
	}

	assertNoPriorTranscript(t, "scoper prompt", prompt)
	for _, want := range []string{escalationDiagnosis, escalationFinalDiff, escalationVerifyOutput, escalationValidatorOut} {
		if !strings.Contains(prompt, want) {
			t.Errorf("scoper prompt missing %q, which the escalated execution context carries", want)
		}
	}
}

func assertNoPriorTranscript(t *testing.T, what, prompt string) {
	t.Helper()
	for _, banned := range []string{
		escalationModelReasoning, // no prior-attempt model reasoning
		escalationPartialDiff,    // no partial diff other than the final one
		escalationToolCallLog,    // no tool-call logs
		"--- iteration 4 ---",    // no iteration transcript at all
		"SENTINEL",
	} {
		if strings.Contains(prompt, banned) {
			t.Errorf("%s replays the prior attempt: it contains %q", what, banned)
		}
	}
}

// A non-escalated retry is untouched by any of this: it still gets the
// failure-injection prompt, because its intent was right and the last diff is
// what it is fixing.
func TestBuildPrompt_MechanicalRetryKeepsTheFailureInjectionPrompt(t *testing.T) {
	item := &domain.WorkItem{
		ID:                    "mechanical-retry",
		Prompt:                "## TASK\n\nAdd the doc comment.",
		LastFailureOutput:     escalationVerifyOutput,
		LastValidatorFeedback: escalationValidatorOut,
		MechanicalRetryCount:  1,
	}

	prompt := buildPrompt(item)

	if !strings.HasPrefix(prompt, item.Prompt) {
		t.Error("a mechanical retry must still start from the work item prompt")
	}
	for _, want := range []string{"## PREVIOUS FAILURES", "## PROJECT STANDARDS FEEDBACK", escalationVerifyOutput, escalationValidatorOut} {
		if !strings.Contains(prompt, want) {
			t.Errorf("failure-injection prompt missing %q", want)
		}
	}
	for _, banned := range []string{"## THE CHANGE REQUEST AS IT STANDS", "## WHAT THE FAILED ATTEMPTS CONCLUDED", "escalated Executor"} {
		if strings.Contains(prompt, banned) {
			t.Errorf("a non-escalated retry must not get the escalated context, got %q", banned)
		}
	}
}

func TestRecordFailureConclusions_KeepsConclusionsOnly(t *testing.T) {
	item := &domain.WorkItem{ID: "item", IterationCount: 3}
	agg := &validators.AggregateResult{
		Failures: []validators.ValidatorFailure{
			{ID: "spec-fidelity", Verdict: &validators.Verdict{
				Outcome:         validators.OutcomeFail,
				FailureClass:    validators.FailureComprehension,
				Diagnosis:       escalationDiagnosis,
				CorrectedIntent: escalationIntent,
			}},
			// A validator that failed without stating a diagnosis has no conclusion
			// to carry forward.
			{ID: "silent", Verdict: &validators.Verdict{Outcome: validators.OutcomeFail}},
		},
	}

	recordFailureConclusions(item, agg)

	if len(item.FailureConclusions) != 1 {
		t.Fatalf("FailureConclusions = %d entries, want 1", len(item.FailureConclusions))
	}
	got := item.FailureConclusions[0]
	if got.Iteration != 3 || got.ValidatorID != "spec-fidelity" {
		t.Errorf("conclusion = iteration %d from %q, want iteration 3 from spec-fidelity", got.Iteration, got.ValidatorID)
	}
	if got.Diagnosis != escalationDiagnosis || got.CorrectedIntent != escalationIntent {
		t.Errorf("conclusion dropped the diagnosis or the corrected intent: %+v", got)
	}
}

func TestFailureConclusionLines_FallsBackToTheEscalationDiagnoses(t *testing.T) {
	// A work item escalated from persisted counters after a resume reached its
	// conclusions in an earlier process, so the escalation error's diagnoses are
	// all there is.
	item := &domain.WorkItem{ID: "resumed"}

	lines := failureConclusionLines(item, []string{"spec-fidelity: " + escalationDiagnosis})

	if len(lines) != 1 || !strings.Contains(lines[0], escalationDiagnosis) {
		t.Errorf("lines = %v, want the fallback diagnosis", lines)
	}
}
