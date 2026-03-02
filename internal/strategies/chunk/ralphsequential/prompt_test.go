package ralphsequential

import (
	"strings"
	"testing"

	"github.com/leightonvanrooijen/utopia/internal/domain"
)

func TestBuildPrompt_BasicFeature(t *testing.T) {
	feature := domain.Feature{
		ID:          "test-feature",
		Description: "Implement user authentication",
		AcceptanceCriteria: []string{
			"Users can log in with email and password",
			"Invalid credentials return 401",
		},
	}

	prompt := BuildPrompt(feature, nil)

	// Should contain TASK section
	if !strings.Contains(prompt, "## TASK") {
		t.Error("prompt should contain TASK section")
	}

	// Should contain feature description
	if !strings.Contains(prompt, "Implement user authentication") {
		t.Error("prompt should contain feature description")
	}

	// Should contain acceptance criteria
	if !strings.Contains(prompt, "Users can log in with email and password") {
		t.Error("prompt should contain first acceptance criterion")
	}
	if !strings.Contains(prompt, "Invalid credentials return 401") {
		t.Error("prompt should contain second acceptance criterion")
	}

	// Should contain CONSTRAINTS section
	if !strings.Contains(prompt, "## CONSTRAINTS") {
		t.Error("prompt should contain CONSTRAINTS section")
	}

	// Should contain completion token
	if !strings.Contains(prompt, "<COMPLETE>") {
		t.Error("prompt should contain <COMPLETE> token")
	}

	// Should NOT contain PREVIOUS FAILURES when no failures
	if strings.Contains(prompt, "PREVIOUS FAILURES") {
		t.Error("prompt should not contain PREVIOUS FAILURES when none provided")
	}
}

func TestBuildPrompt_WithFailures(t *testing.T) {
	feature := domain.Feature{
		ID:                 "failing-feature",
		Description:        "A feature that failed",
		AcceptanceCriteria: []string{"Should work"},
	}

	failures := []string{
		"Error: assertion failed at line 42",
		"Expected true, got false",
	}

	prompt := BuildPrompt(feature, failures)

	// Should contain PREVIOUS FAILURES section
	if !strings.Contains(prompt, "## PREVIOUS FAILURES") {
		t.Error("prompt should contain PREVIOUS FAILURES section")
	}

	// Should contain failure messages
	if !strings.Contains(prompt, "assertion failed at line 42") {
		t.Error("prompt should contain first failure message")
	}
	if !strings.Contains(prompt, "Expected true, got false") {
		t.Error("prompt should contain second failure message")
	}

	// Should instruct to address failures
	if !strings.Contains(prompt, "address these failures") {
		t.Error("prompt should instruct to address failures")
	}
}

func TestBuildPrompt_EmptyFailures(t *testing.T) {
	feature := domain.Feature{
		ID:                 "test",
		Description:        "Test feature",
		AcceptanceCriteria: []string{"Works"},
	}

	// Empty slice should be same as nil
	prompt := BuildPrompt(feature, []string{})

	if strings.Contains(prompt, "PREVIOUS FAILURES") {
		t.Error("prompt should not contain PREVIOUS FAILURES for empty failures slice")
	}
}

func TestBuildPromptWithConstraints(t *testing.T) {
	feature := domain.Feature{
		ID:                 "constrained-feature",
		Description:        "Feature with custom constraints",
		AcceptanceCriteria: []string{"Does the thing"},
	}

	customConstraints := []string{
		"Use only standard library",
		"No network calls",
	}

	prompt := BuildPromptWithConstraints(feature, customConstraints, nil, nil)

	// Should contain custom constraints
	if !strings.Contains(prompt, "Use only standard library") {
		t.Error("prompt should contain first custom constraint")
	}
	if !strings.Contains(prompt, "No network calls") {
		t.Error("prompt should contain second custom constraint")
	}
}

func TestBuildPromptWithConstraints_AndFailures(t *testing.T) {
	feature := domain.Feature{
		ID:                 "complex-feature",
		Description:        "Complex feature",
		AcceptanceCriteria: []string{"Works correctly"},
	}

	constraints := []string{"Constraint A"}
	failures := []string{"Test failed: got nil"}

	prompt := BuildPromptWithConstraints(feature, constraints, failures, nil)

	// Should have both constraints and failures
	if !strings.Contains(prompt, "Constraint A") {
		t.Error("prompt should contain constraint")
	}
	if !strings.Contains(prompt, "Test failed: got nil") {
		t.Error("prompt should contain failure")
	}
}

func TestRebuildPromptWithFailures(t *testing.T) {
	feature := domain.Feature{
		ID:                 "rebuild-feature",
		Description:        "Feature to rebuild",
		AcceptanceCriteria: []string{"Should pass"},
	}

	workItem := &domain.WorkItem{
		ID:          "test-item",
		Prompt:      "original prompt",
		Constraints: []string{"Keep it simple"},
	}

	failures := []string{"Test failed"}

	RebuildPromptWithFailures(workItem, feature, failures)

	// Prompt should be updated
	if workItem.Prompt == "original prompt" {
		t.Error("RebuildPromptWithFailures should update the prompt")
	}

	// Should contain the failure
	if !strings.Contains(workItem.Prompt, "Test failed") {
		t.Error("rebuilt prompt should contain failure")
	}

	// Should contain the constraint
	if !strings.Contains(workItem.Prompt, "Keep it simple") {
		t.Error("rebuilt prompt should preserve constraints")
	}
}

func TestBuildTaskWithCriteria(t *testing.T) {
	feature := domain.Feature{
		ID:          "task-feature",
		Description: "Build a REST endpoint",
		AcceptanceCriteria: []string{
			"Returns 200 OK",
			"Response is JSON",
			"Handles errors gracefully",
		},
	}

	task := buildTaskWithCriteria(feature)

	// Should contain description
	if !strings.Contains(task, "Build a REST endpoint") {
		t.Error("task should contain feature description")
	}

	// Should contain "Acceptance criteria:" header
	if !strings.Contains(task, "Acceptance criteria:") {
		t.Error("task should contain 'Acceptance criteria:' header")
	}

	// Should contain all criteria as bullet points
	for _, criterion := range feature.AcceptanceCriteria {
		if !strings.Contains(task, "- "+criterion) {
			t.Errorf("task should contain criterion as bullet: %q", criterion)
		}
	}
}

func TestBuildTaskWithCriteria_EmptyCriteria(t *testing.T) {
	feature := domain.Feature{
		ID:                 "no-criteria",
		Description:        "Feature without criteria",
		AcceptanceCriteria: []string{},
	}

	task := buildTaskWithCriteria(feature)

	// Should still contain description
	if !strings.Contains(task, "Feature without criteria") {
		t.Error("task should contain feature description")
	}

	// Should contain acceptance criteria header (even if empty)
	if !strings.Contains(task, "Acceptance criteria:") {
		t.Error("task should contain 'Acceptance criteria:' header")
	}
}

func TestEscapeTemplateContent(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "no template syntax",
			input:    "Regular text without templates",
			expected: "Regular text without templates",
		},
		{
			name:     "opening braces",
			input:    "Use {{.Field}} for templates",
			expected: "Use { {.Field} } for templates",
		},
		{
			name:     "closing braces only",
			input:    "Some text }}",
			expected: "Some text } }",
		},
		{
			name:     "multiple template expressions",
			input:    "{{.A}} and {{.B}}",
			expected: "{ {.A} } and { {.B} }",
		},
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "single braces unchanged",
			input:    "JSON: {key: value}",
			expected: "JSON: {key: value}",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := escapeTemplateContent(tt.input)
			if got != tt.expected {
				t.Errorf("escapeTemplateContent(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestPromptTemplate_Structure(t *testing.T) {
	// Verify the template has expected sections
	if !strings.Contains(PromptTemplate, "## TASK") {
		t.Error("PromptTemplate should contain TASK section")
	}

	if !strings.Contains(PromptTemplate, "## CONSTRAINTS") {
		t.Error("PromptTemplate should contain CONSTRAINTS section")
	}

	if !strings.Contains(PromptTemplate, "<COMPLETE>") {
		t.Error("PromptTemplate should contain completion token")
	}

	if !strings.Contains(PromptTemplate, "{{.Task}}") {
		t.Error("PromptTemplate should have Task placeholder")
	}

	if !strings.Contains(PromptTemplate, "{{range .Constraints}}") {
		t.Error("PromptTemplate should have Constraints range loop")
	}
}

func TestPromptTemplate_OptionalFailures(t *testing.T) {
	// Template should conditionally include failures
	if !strings.Contains(PromptTemplate, "{{if .PreviousFailures}}") {
		t.Error("PromptTemplate should conditionally include PreviousFailures")
	}

	if !strings.Contains(PromptTemplate, "{{end}}") {
		t.Error("PromptTemplate should close conditional block")
	}
}

func TestBuildPrompt_TemplateInjectionPrevention(t *testing.T) {
	// Feature with template-like content that could cause injection
	feature := domain.Feature{
		ID:          "injection-test",
		Description: "Handle {{.Malicious}} template syntax",
		AcceptanceCriteria: []string{
			"Process {{range .Items}}{{.}}{{end}} safely",
		},
	}

	// This should not panic
	prompt := BuildPrompt(feature, nil)

	// Template syntax should be escaped
	if strings.Contains(prompt, "{{.Malicious}}") {
		t.Error("template syntax in description should be escaped")
	}

	if strings.Contains(prompt, "{{range .Items}}") {
		t.Error("template syntax in criteria should be escaped")
	}

	// Content should still be present (escaped)
	if !strings.Contains(prompt, "Malicious") {
		t.Error("escaped content should still contain the word")
	}
}

func TestBuildPrompt_FailureTemplateInjectionPrevention(t *testing.T) {
	feature := domain.Feature{
		ID:                 "failure-injection",
		Description:        "Test",
		AcceptanceCriteria: []string{"Works"},
	}

	// Failure output with template syntax
	failures := []string{
		"Error in {{.Function}}: unexpected token",
	}

	prompt := BuildPrompt(feature, failures)

	// Should not contain unescaped template syntax
	if strings.Contains(prompt, "{{.Function}}") {
		t.Error("template syntax in failures should be escaped")
	}
}

func TestRenderTemplate_PanicsOnInvalidTemplate(t *testing.T) {
	// This test documents expected behavior - the template is hardcoded
	// and should always be valid, so panic is appropriate for bugs

	// We can't easily test the panic case without modifying PromptTemplate,
	// so we just verify the happy path works
	data := PromptData{
		Task:        "Test task",
		Constraints: []string{"C1", "C2"},
	}

	// Should not panic
	result := renderTemplate(data)

	if result == "" {
		t.Error("renderTemplate should return non-empty string")
	}
}

func TestBuildPrompt_MultilineDescription(t *testing.T) {
	feature := domain.Feature{
		ID: "multiline",
		Description: `This is a multi-line description.
It spans multiple lines.
And has various content.`,
		AcceptanceCriteria: []string{"Works"},
	}

	prompt := BuildPrompt(feature, nil)

	// All lines should be present
	if !strings.Contains(prompt, "This is a multi-line description") {
		t.Error("prompt should contain first line of description")
	}
	if !strings.Contains(prompt, "It spans multiple lines") {
		t.Error("prompt should contain second line of description")
	}
}

func TestBuildPrompt_SpecialCharactersInCriteria(t *testing.T) {
	feature := domain.Feature{
		ID:          "special-chars",
		Description: "Handle special characters",
		AcceptanceCriteria: []string{
			"Returns JSON with \"quotes\"",
			"Handles <html> tags",
			"Supports $variables and %percentages",
			"Works with regex: ^[a-z]+$",
		},
	}

	// Should not panic
	prompt := BuildPrompt(feature, nil)

	// Special chars should be preserved
	if !strings.Contains(prompt, "\"quotes\"") {
		t.Error("quotes should be preserved")
	}
	if !strings.Contains(prompt, "<html>") {
		t.Error("angle brackets should be preserved")
	}
}

