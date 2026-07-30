package ralph

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/leightonvanrooijen/utopia/internal"
	"github.com/leightonvanrooijen/utopia/internal/domain"
)

func TestGateFeedback_CarriesConnectorStdout(t *testing.T) {
	err := &GateError{Connector: "lint-gate", Event: EventWorkItemVerified, Stdout: "3 lint errors\n"}

	feedback := gateFeedback(err)

	if !strings.Contains(feedback, "lint-gate") {
		t.Errorf("feedback must name the connector, got %q", feedback)
	}
	if !strings.Contains(feedback, "3 lint errors") {
		t.Errorf("feedback must carry the connector stdout, got %q", feedback)
	}
}

func TestGateFeedback_FallsBackToErrorMessageWithoutStdout(t *testing.T) {
	feedback := gateFeedback(errors.New("something blocked"))

	if !strings.Contains(feedback, "something blocked") {
		t.Errorf("feedback must fall back to the error message, got %q", feedback)
	}
}

func TestGateError_MessageWithoutStdout(t *testing.T) {
	err := &GateError{Connector: "gate", Event: EventWorkItemStarted}

	want := "gating connector gate blocked " + EventWorkItemStarted
	if err.Error() != want {
		t.Errorf("Error() = %q, want %q", err.Error(), want)
	}
}

func TestCompletionToken(t *testing.T) {
	// Verify the completion token constant
	if CompletionToken != "<COMPLETE>" {
		t.Errorf("CompletionToken = %q, want %q", CompletionToken, "<COMPLETE>")
	}
}

func TestBuildPrompt_NoFailures(t *testing.T) {
	item := &domain.WorkItem{
		ID:         "test-item",
		Prompt:     "## TASK\n\nImplement feature X\n\n## CONSTRAINTS\n\n- Keep it simple\n\n---\n\nWhen complete, output: <COMPLETE>",
		Status:     domain.WorkItemPending,
		Complexity: domain.ComplexityMedium,
	}

	prompt := buildPrompt(item)

	// Should return the original prompt unchanged
	if prompt != item.Prompt {
		t.Errorf("buildPrompt without failures should return original prompt")
	}

	// Should not contain PREVIOUS FAILURES section
	if strings.Contains(prompt, "PREVIOUS FAILURES") {
		t.Error("prompt should not contain PREVIOUS FAILURES when no failures")
	}
}

func TestBuildPrompt_WithFailures(t *testing.T) {
	item := &domain.WorkItem{
		ID:                "test-item",
		Prompt:            "## TASK\n\nImplement feature X\n\n## CONSTRAINTS\n\n- Keep it simple\n\n---\n\nWhen complete, output: <COMPLETE>",
		Status:            domain.WorkItemPending,
		Complexity:        domain.ComplexityMedium,
		LastFailureOutput: "Error: test failed\nExpected 1 but got 2",
	}

	prompt := buildPrompt(item)

	// Should contain original prompt
	if !strings.Contains(prompt, "## TASK") {
		t.Error("prompt should contain original TASK section")
	}

	// Should contain PREVIOUS FAILURES section
	if !strings.Contains(prompt, "## PREVIOUS FAILURES") {
		t.Error("prompt should contain PREVIOUS FAILURES section")
	}

	// Should contain the failure output
	if !strings.Contains(prompt, "Error: test failed") {
		t.Error("prompt should contain the failure output")
	}

	if !strings.Contains(prompt, "Expected 1 but got 2") {
		t.Error("prompt should contain full failure output")
	}

	// Should have instruction to fix failures regardless of origin
	if !strings.Contains(prompt, "fix all failures regardless of whether they were introduced by this work item") {
		t.Error("prompt should instruct to fix all failures regardless of origin")
	}
}

func TestBuildPrompt_EmptyFailureOutput(t *testing.T) {
	item := &domain.WorkItem{
		ID:         "test-item",
		Prompt:     "Original prompt",
		Status:     domain.WorkItemPending,
		Complexity: domain.ComplexityMedium,
	}
	// LastFailureOutput defaults to empty string

	prompt := buildPrompt(item)

	// Should not add PREVIOUS FAILURES for empty failure output
	if strings.Contains(prompt, "PREVIOUS FAILURES") {
		t.Error("prompt should not contain PREVIOUS FAILURES for empty failure output")
	}
}

func TestBuildPrompt_PreservesOriginalPrompt(t *testing.T) {
	originalPrompt := `## TASK

Build a REST API endpoint

Acceptance criteria:
- Returns 200 OK
- Responds with JSON

## CONSTRAINTS

- Do not use external libraries
- Keep response time under 100ms

---

When complete, commit your changes and output: <COMPLETE>`

	item := &domain.WorkItem{
		ID:                "api-endpoint",
		Prompt:            originalPrompt,
		Status:            domain.WorkItemPending,
		Complexity:        domain.ComplexityMedium,
		LastFailureOutput: "404 Not Found",
	}

	prompt := buildPrompt(item)

	// Original content should be preserved
	if !strings.HasPrefix(prompt, originalPrompt) {
		t.Error("prompt should start with original prompt content")
	}

	// Failure section should be appended
	if !strings.Contains(prompt, "404 Not Found") {
		t.Error("failure output should be appended")
	}
}

func TestBuildPrompt_FailureInCodeBlock(t *testing.T) {
	item := &domain.WorkItem{
		ID:                "test-item",
		Prompt:            "Original prompt",
		Status:            domain.WorkItemPending,
		Complexity:        domain.ComplexityMedium,
		LastFailureOutput: "some failure output",
	}

	prompt := buildPrompt(item)

	// Failure should be wrapped in code block for readability
	if !strings.Contains(prompt, "```") {
		t.Error("failure output should be in a code block")
	}
}

// TestWorkItemStatusTransitions verifies the expected status flow
func TestWorkItemStatusTransitions(t *testing.T) {
	tests := []struct {
		name     string
		from     domain.WorkItemStatus
		to       domain.WorkItemStatus
		valid    bool
		scenario string
	}{
		{
			name:     "pending to in_progress",
			from:     domain.WorkItemPending,
			to:       domain.WorkItemInProgress,
			valid:    true,
			scenario: "starting execution",
		},
		{
			name:     "in_progress to completed",
			from:     domain.WorkItemInProgress,
			to:       domain.WorkItemCompleted,
			valid:    true,
			scenario: "verification passed",
		},
		{
			name:     "in_progress to failed",
			from:     domain.WorkItemInProgress,
			to:       domain.WorkItemFailed,
			valid:    true,
			scenario: "max iterations reached",
		},
		{
			name:     "in_progress stays in_progress",
			from:     domain.WorkItemInProgress,
			to:       domain.WorkItemInProgress,
			valid:    true,
			scenario: "retry iteration",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			item := &domain.WorkItem{Status: tt.from}
			item.Status = tt.to

			if item.Status != tt.to {
				t.Errorf("failed to transition from %s to %s", tt.from, tt.to)
			}
		})
	}
}

// TestIterationCountTracking verifies iteration counting behavior
func TestIterationCountTracking(t *testing.T) {
	item := &domain.WorkItem{
		ID:             "test-item",
		Prompt:         "",
		Status:         domain.WorkItemPending,
		Complexity:     domain.ComplexityMedium,
		IterationCount: 0, // Ensure starting at 0
	}

	// Simulate multiple iterations
	for i := 1; i <= 5; i++ {
		item.IterationCount++
		item.Status = domain.WorkItemInProgress

		if item.IterationCount != i {
			t.Errorf("after iteration %d, IterationCount = %d", i, item.IterationCount)
		}
	}

	// Complete the item
	item.Status = domain.WorkItemCompleted

	// Iteration count should be preserved
	if item.IterationCount != 5 {
		t.Errorf("completed item should preserve iteration count, got %d", item.IterationCount)
	}
}

// TestMaxIterationsCheck verifies the max iterations logic
func TestMaxIterationsCheck(t *testing.T) {
	tests := []struct {
		name          string
		maxIterations int
		currentIter   int
		shouldStop    bool
	}{
		{"under limit", 10, 5, false},
		{"at limit", 10, 10, false},
		{"over limit", 10, 11, true},
		{"unlimited (0)", 0, 100, false},
		{"unlimited (0) high count", 0, 1000, false},
		{"limit of 1, first iter", 1, 1, false},
		{"limit of 1, second iter", 1, 2, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// The check in executeWorkItem is: maxIterations > 0 && item.IterationCount > maxIterations
			shouldStop := tt.maxIterations > 0 && tt.currentIter > tt.maxIterations

			if shouldStop != tt.shouldStop {
				t.Errorf("maxIterations=%d, currentIter=%d: shouldStop=%v, want %v",
					tt.maxIterations, tt.currentIter, shouldStop, tt.shouldStop)
			}
		})
	}
}

// TestCompletionTokenDetection verifies token detection in output
func TestCompletionTokenDetection(t *testing.T) {
	tests := []struct {
		name     string
		output   string
		hasToken bool
	}{
		{
			name:     "token present",
			output:   "Done implementing the feature.\n<COMPLETE>",
			hasToken: true,
		},
		{
			name:     "token at start",
			output:   "<COMPLETE>\nAll done!",
			hasToken: true,
		},
		{
			name:     "token in middle",
			output:   "Step 1 done.\n<COMPLETE>\nCleaning up.",
			hasToken: true,
		},
		{
			name:     "no token",
			output:   "Still working on the feature...",
			hasToken: false,
		},
		{
			name:     "partial token",
			output:   "<COMPLE",
			hasToken: false,
		},
		{
			name:     "similar but wrong token",
			output:   "<COMPLETED>",
			hasToken: false,
		},
		{
			name:     "lowercase token",
			output:   "<complete>",
			hasToken: false, // Token is case-sensitive
		},
		{
			name:     "empty output",
			output:   "",
			hasToken: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hasToken := strings.Contains(tt.output, CompletionToken)

			if hasToken != tt.hasToken {
				t.Errorf("output %q: hasToken=%v, want %v", tt.output, hasToken, tt.hasToken)
			}
		})
	}
}

// TestExtractCRID verifies CR ID extraction from spec IDs
func TestExtractCRID(t *testing.T) {
	tests := []struct {
		name     string
		specID   string
		expected string
	}{
		{
			name:     "regular CR",
			specID:   "my-change-request",
			expected: "my-change-request",
		},
		{
			name:     "initiative phase 0",
			specID:   "my-initiative/phase-0",
			expected: "my-initiative",
		},
		{
			name:     "initiative phase 5",
			specID:   "my-initiative/phase-5",
			expected: "my-initiative",
		},
		{
			name:     "CR with dashes",
			specID:   "add-user-auth-feature",
			expected: "add-user-auth-feature",
		},
		{
			name:     "nested path (edge case)",
			specID:   "cr-id/phase-0/extra",
			expected: "cr-id",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractCRID(tt.specID)
			if got != tt.expected {
				t.Errorf("extractCRID(%q) = %q, want %q", tt.specID, got, tt.expected)
			}
		})
	}
}

func TestResult_Fields(t *testing.T) {
	result := &Result{
		Completed: 5,
		Total:     10,
		StoppedAt: "work-item-6",
		Reason:    "max iterations reached",
	}

	if result.Completed != 5 {
		t.Errorf("Completed = %d, want %d", result.Completed, 5)
	}

	if result.Total != 10 {
		t.Errorf("Total = %d, want %d", result.Total, 10)
	}

	if result.StoppedAt != "work-item-6" {
		t.Errorf("StoppedAt = %q, want %q", result.StoppedAt, "work-item-6")
	}

	if result.Reason != "max iterations reached" {
		t.Errorf("Reason = %q, want %q", result.Reason, "max iterations reached")
	}
}

func TestBuildPrompt_WithValidatorFeedbackOnly(t *testing.T) {
	item := &domain.WorkItem{
		ID:                    "test-item",
		Prompt:                "## TASK\n\nImplement feature X",
		Status:                domain.WorkItemPending,
		Complexity:            domain.ComplexityMedium,
		LastValidatorFeedback: "Validator code-style failed:\nMissing error handling in handler.go",
	}

	prompt := buildPrompt(item)

	// Should contain original prompt
	if !strings.Contains(prompt, "## TASK") {
		t.Error("prompt should contain original TASK section")
	}

	// Should contain PROJECT STANDARDS FEEDBACK section (not PREVIOUS FAILURES)
	if !strings.Contains(prompt, "## PROJECT STANDARDS FEEDBACK") {
		t.Error("prompt should contain PROJECT STANDARDS FEEDBACK section")
	}

	// Should NOT contain PREVIOUS FAILURES section
	if strings.Contains(prompt, "## PREVIOUS FAILURES") {
		t.Error("prompt should not contain PREVIOUS FAILURES when only validator feedback exists")
	}

	// Should contain the validator feedback
	if !strings.Contains(prompt, "Missing error handling") {
		t.Error("prompt should contain the validator feedback")
	}

	// Should have instruction to address standards
	if !strings.Contains(prompt, "standards violations") {
		t.Error("prompt should instruct to address standards violations")
	}
}

func TestBuildPrompt_WithBothFailuresAndValidatorFeedback(t *testing.T) {
	item := &domain.WorkItem{
		ID:                    "test-item",
		Prompt:                "## TASK\n\nImplement feature X",
		Status:                domain.WorkItemPending,
		Complexity:            domain.ComplexityMedium,
		LastFailureOutput:     "FAIL: TestHandler\nExpected 200 but got 500",
		LastValidatorFeedback: "Validator security-check failed:\nSQL injection vulnerability detected",
	}

	prompt := buildPrompt(item)

	// Should contain both sections
	if !strings.Contains(prompt, "## PREVIOUS FAILURES") {
		t.Error("prompt should contain PREVIOUS FAILURES section")
	}

	if !strings.Contains(prompt, "## PROJECT STANDARDS FEEDBACK") {
		t.Error("prompt should contain PROJECT STANDARDS FEEDBACK section")
	}

	// PREVIOUS FAILURES should come before PROJECT STANDARDS FEEDBACK
	failuresIndex := strings.Index(prompt, "## PREVIOUS FAILURES")
	standardsIndex := strings.Index(prompt, "## PROJECT STANDARDS FEEDBACK")
	if failuresIndex > standardsIndex {
		t.Error("PREVIOUS FAILURES section should come before PROJECT STANDARDS FEEDBACK section")
	}

	// Should contain both types of feedback
	if !strings.Contains(prompt, "Expected 200 but got 500") {
		t.Error("prompt should contain test failure output")
	}

	if !strings.Contains(prompt, "SQL injection vulnerability") {
		t.Error("prompt should contain validator feedback")
	}
}

func TestBuildPrompt_NoFeedback(t *testing.T) {
	item := &domain.WorkItem{
		ID:         "test-item",
		Prompt:     "Original prompt",
		Status:     domain.WorkItemPending,
		Complexity: domain.ComplexityMedium,
		// Both fields empty/default
	}

	prompt := buildPrompt(item)

	// Should not contain either section
	if strings.Contains(prompt, "PREVIOUS FAILURES") {
		t.Error("prompt should not contain PREVIOUS FAILURES when empty")
	}

	if strings.Contains(prompt, "PROJECT STANDARDS FEEDBACK") {
		t.Error("prompt should not contain PROJECT STANDARDS FEEDBACK when empty")
	}

	// Should just be the original prompt
	if prompt != item.Prompt {
		t.Errorf("prompt should be unchanged, got %q", prompt)
	}
}

func TestBuildPrompt_TestFailuresInCodeBlock(t *testing.T) {
	item := &domain.WorkItem{
		ID:                "test-item",
		Prompt:            "Original prompt",
		Status:            domain.WorkItemPending,
		Complexity:        domain.ComplexityMedium,
		LastFailureOutput: "test failure",
	}

	prompt := buildPrompt(item)

	// Test failures should be in a code block
	if !strings.Contains(prompt, "```\ntest failure\n```") {
		t.Error("test failure output should be wrapped in code blocks")
	}
}

func TestBuildPrompt_ValidatorFeedbackNotInCodeBlock(t *testing.T) {
	item := &domain.WorkItem{
		ID:                    "test-item",
		Prompt:                "Original prompt",
		Status:                domain.WorkItemPending,
		Complexity:            domain.ComplexityMedium,
		LastValidatorFeedback: "Validator feedback here",
	}

	prompt := buildPrompt(item)

	// Validator feedback is already formatted by validators, not wrapped in additional code block
	// The feedback should appear directly in the issues section
	if !strings.Contains(prompt, "standards issues:\n\nValidator feedback here") {
		t.Error("validator feedback should be included without additional code block wrapping")
	}
}

func TestAppendIterationOutput_AccumulatesEveryIteration(t *testing.T) {
	var transcript strings.Builder

	appendIterationOutput(&transcript, 1, &internal.PromptResult{Stdout: "tried the cache approach\n"})
	appendIterationOutput(&transcript, 2, &internal.PromptResult{Stdout: "switched to a queue <COMPLETE>\n"})

	got := transcript.String()
	// The abandoned first attempt is the decision record harvest needs, so it
	// must survive alongside the attempt that completed.
	if !strings.Contains(got, "tried the cache approach") {
		t.Errorf("transcript must keep the first iteration's output, got %q", got)
	}
	if !strings.Contains(got, "switched to a queue") {
		t.Errorf("transcript must keep the completing iteration's output, got %q", got)
	}
	if !strings.Contains(got, "--- iteration 1 ---") || !strings.Contains(got, "--- iteration 2 ---") {
		t.Errorf("transcript must label each iteration, got %q", got)
	}
	if strings.Index(got, "iteration 1") > strings.Index(got, "iteration 2") {
		t.Errorf("iterations must be in order, got %q", got)
	}
}

func TestAppendIterationOutput_SkipsEmptyAndMissingResults(t *testing.T) {
	var transcript strings.Builder

	// Claude can fail before producing any output; the loop still calls this.
	appendIterationOutput(&transcript, 1, nil)
	appendIterationOutput(&transcript, 2, &internal.PromptResult{Stdout: ""})

	if transcript.Len() != 0 {
		t.Errorf("empty iterations must contribute nothing, got %q", transcript.String())
	}
}

func TestWriteRunTranscript_RecordsJoinKeysAndOutcome(t *testing.T) {
	dir := t.TempDir()
	store := internal.NewYAMLStore(dir)
	item := &domain.WorkItem{
		ID:             "cr-1-phase-0-add-thing",
		SpecRef:        "my-spec.add-thing",
		IterationCount: 3,
	}

	writeRunTranscript(store, "cr-1", item, recorderWith("", "the streamed output"), domain.RunCompleted)

	run, err := internal.Load[domain.ExecutionRun](store, "runs/cr-1/cr-1-phase-0-add-thing.yaml")
	if err != nil {
		t.Fatalf("run transcript should be written to runs/<cr-id>/<workitem-id>.yaml: %v", err)
	}
	if run.WorkItemID != item.ID || run.CRID != "cr-1" {
		t.Errorf("run must carry both join keys, got workitem %q cr %q", run.WorkItemID, run.CRID)
	}
	if run.SpecRef != "my-spec.add-thing" {
		t.Errorf("SpecRef = %q, want %q", run.SpecRef, "my-spec.add-thing")
	}
	if run.Iterations != 3 {
		t.Errorf("Iterations = %d, want 3", run.Iterations)
	}
	if run.Outcome != domain.RunCompleted {
		t.Errorf("Outcome = %q, want %q", run.Outcome, domain.RunCompleted)
	}
	if run.Transcript != "the streamed output" {
		t.Errorf("Transcript = %q, want the streamed output", run.Transcript)
	}
	if run.CompletedAt.IsZero() {
		t.Error("CompletedAt must be stamped")
	}
}

func TestWriteRunTranscript_UnwritableStoreDoesNotPanic(t *testing.T) {
	// A store rooted at a file rather than a directory cannot create runs/.
	// The loop must survive that - the transcript is a byproduct, not the work.
	dir := t.TempDir()
	notADir := filepath.Join(dir, "blocked")
	if err := os.WriteFile(notADir, []byte("x"), 0644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	writeRunTranscript(internal.NewYAMLStore(notADir), "cr-1", &domain.WorkItem{ID: "item"}, recorderWith("", "out"), domain.RunFailed)
}

// TestBuildPrompt_NeverMentionsTheTurnBudget guards the deliberate exclusion:
// the retry prompt carries task information (PREVIOUS FAILURES) but never a word
// about turns, limits or the cap. A scarcity signal says hurry, and hurrying is
// what invites the shortcuts the loop then has to undo.
func TestBuildPrompt_NeverMentionsTheTurnBudget(t *testing.T) {
	item := &domain.WorkItem{
		ID:                    "test-item",
		Prompt:                "## TASK\n\nImplement feature X\n\n---\n\nWhen complete, output: <COMPLETE>",
		Status:                domain.WorkItemInProgress,
		LastFailureOutput:     "Error: test failed",
		LastValidatorFeedback: "Validator: naming is off",
	}

	prompt := strings.ToLower(buildPrompt(item))

	for _, banned := range []string{"turn budget", "max-turns", "max turns", "turn limit", "turn cap", "running out of"} {
		if strings.Contains(prompt, banned) {
			t.Errorf("retry prompt must not reference the turn cap, but contains %q", banned)
		}
	}
}
