package domain

import (
	"testing"
)

func TestValidator_GetRun_Defaults(t *testing.T) {
	tests := []struct {
		name     string
		run      RunTrigger
		expected RunTrigger
	}{
		{"empty defaults to after-workitem", "", RunAfterWorkitem},
		{"after-workitem returns as-is", RunAfterWorkitem, RunAfterWorkitem},
		{"after-phase returns as-is", RunAfterPhase, RunAfterPhase},
		{"on-demand returns as-is", RunOnDemand, RunOnDemand},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			v := Validator{Run: tc.run}
			if got := v.GetRun(); got != tc.expected {
				t.Errorf("GetRun() = %q, want %q", got, tc.expected)
			}
		})
	}
}

func TestValidator_GetAllowedTools_Defaults(t *testing.T) {
	t.Run("empty defaults to read-only tools", func(t *testing.T) {
		v := Validator{}
		got := v.GetAllowedTools()
		expected := DefaultAllowedTools()

		if len(got) != len(expected) {
			t.Fatalf("GetAllowedTools() returned %d tools, want %d", len(got), len(expected))
		}
		for i := range expected {
			if got[i] != expected[i] {
				t.Errorf("GetAllowedTools()[%d] = %q, want %q", i, got[i], expected[i])
			}
		}
	})

	t.Run("custom tools returned as-is", func(t *testing.T) {
		v := Validator{AllowedTools: []string{"Read", "Bash"}}
		got := v.GetAllowedTools()

		if len(got) != 2 {
			t.Fatalf("GetAllowedTools() returned %d tools, want 2", len(got))
		}
		if got[0] != "Read" || got[1] != "Bash" {
			t.Errorf("GetAllowedTools() = %v, want [Read, Bash]", got)
		}
	})
}

func TestValidator_ExpandPrompt(t *testing.T) {
	tests := []struct {
		name         string
		prompt       string
		changedFiles string
		expected     string
	}{
		{
			name:         "replaces placeholder",
			prompt:       "Review: {{changed_files}}\nDone.",
			changedFiles: "diff --git a/file.go",
			expected:     "Review: diff --git a/file.go\nDone.",
		},
		{
			name:         "multiple placeholders",
			prompt:       "Changes: {{changed_files}}\nAgain: {{changed_files}}",
			changedFiles: "DIFF",
			expected:     "Changes: DIFF\nAgain: DIFF",
		},
		{
			name:         "no placeholder",
			prompt:       "Static prompt with no placeholder",
			changedFiles: "should be ignored",
			expected:     "Static prompt with no placeholder",
		},
		{
			name:         "empty changed_files",
			prompt:       "Review: {{changed_files}}",
			changedFiles: "",
			expected:     "Review: ",
		},
		{
			name:         "multiline diff",
			prompt:       "{{changed_files}}",
			changedFiles: "line1\nline2\nline3",
			expected:     "line1\nline2\nline3",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			v := Validator{Prompt: tc.prompt}
			got := v.ExpandPrompt(tc.changedFiles)
			if got != tc.expected {
				t.Errorf("ExpandPrompt() = %q, want %q", got, tc.expected)
			}
		})
	}
}

func TestDefaultAllowedTools(t *testing.T) {
	tools := DefaultAllowedTools()

	expected := []string{"Read", "Glob", "Grep"}
	if len(tools) != len(expected) {
		t.Fatalf("DefaultAllowedTools() returned %d tools, want %d", len(tools), len(expected))
	}
	for i, tool := range expected {
		if tools[i] != tool {
			t.Errorf("DefaultAllowedTools()[%d] = %q, want %q", i, tools[i], tool)
		}
	}
}
