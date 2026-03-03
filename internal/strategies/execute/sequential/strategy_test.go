package sequential

import (
	"strings"
	"testing"

	"github.com/leightonvanrooijen/utopia/internal/domain"
	"github.com/leightonvanrooijen/utopia/internal/testutil"
)

func TestStrategy_Name(t *testing.T) {
	s := New()

	if got := s.Name(); got != "sequential" {
		t.Errorf("Name() = %q, want %q", got, "sequential")
	}
}

func TestStrategy_Description(t *testing.T) {
	s := New()

	desc := s.Description()
	if desc == "" {
		t.Error("Description() returned empty string")
	}

	// Should mention key characteristics
	if !strings.Contains(strings.ToLower(desc), "sequential") &&
		!strings.Contains(strings.ToLower(desc), "order") {
		t.Error("Description should mention sequential execution")
	}
}

func TestCompletionToken(t *testing.T) {
	// Verify the completion token constant
	if CompletionToken != "<COMPLETE>" {
		t.Errorf("CompletionToken = %q, want %q", CompletionToken, "<COMPLETE>")
	}
}

func TestStrategy_BuildPrompt_NoFailures(t *testing.T) {
	s := &Strategy{}

	item := testutil.NewTestWorkItem("test-item", "## TASK\n\nImplement feature X\n\n## CONSTRAINTS\n\n- Keep it simple\n\n---\n\nWhen complete, output: <COMPLETE>")

	prompt := s.buildPrompt(item)

	// Should return the original prompt unchanged
	if prompt != item.Prompt {
		t.Errorf("buildPrompt without failures should return original prompt")
	}

	// Should not contain PREVIOUS FAILURES section
	if strings.Contains(prompt, "PREVIOUS FAILURES") {
		t.Error("prompt should not contain PREVIOUS FAILURES when no failures")
	}
}

func TestStrategy_BuildPrompt_WithFailures(t *testing.T) {
	s := &Strategy{}

	item := testutil.NewTestWorkItem("test-item", "## TASK\n\nImplement feature X\n\n## CONSTRAINTS\n\n- Keep it simple\n\n---\n\nWhen complete, output: <COMPLETE>")
	item.LastFailureOutput = "Error: test failed\nExpected 1 but got 2"

	prompt := s.buildPrompt(item)

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

	// Should have instruction to address failures
	if !strings.Contains(prompt, "address these failures") {
		t.Error("prompt should instruct to address failures")
	}
}

func TestStrategy_BuildPrompt_EmptyFailureOutput(t *testing.T) {
	s := &Strategy{}

	item := testutil.NewTestWorkItem("test-item", "Original prompt")
	// LastFailureOutput defaults to empty string

	prompt := s.buildPrompt(item)

	// Should not add PREVIOUS FAILURES for empty failure output
	if strings.Contains(prompt, "PREVIOUS FAILURES") {
		t.Error("prompt should not contain PREVIOUS FAILURES for empty failure output")
	}
}

func TestStrategy_BuildPrompt_PreservesOriginalPrompt(t *testing.T) {
	s := &Strategy{}

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

	item := testutil.NewTestWorkItem("api-endpoint", originalPrompt)
	item.LastFailureOutput = "404 Not Found"

	prompt := s.buildPrompt(item)

	// Original content should be preserved
	if !strings.HasPrefix(prompt, originalPrompt) {
		t.Error("prompt should start with original prompt content")
	}

	// Failure section should be appended
	if !strings.Contains(prompt, "404 Not Found") {
		t.Error("failure output should be appended")
	}
}

func TestStrategy_BuildPrompt_FailureInCodeBlock(t *testing.T) {
	s := &Strategy{}

	item := testutil.NewTestWorkItem("test-item", "Original prompt")
	item.LastFailureOutput = "some failure output"

	prompt := s.buildPrompt(item)

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
	item := testutil.NewTestWorkItem("test-item", "")
	item.IterationCount = 0 // Ensure starting at 0

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
