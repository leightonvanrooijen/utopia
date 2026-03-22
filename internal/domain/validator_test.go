package domain

import (
	"testing"

	"gopkg.in/yaml.v3"
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

func TestValidator_GetRun(t *testing.T) {
	tests := []struct {
		name     string
		run      RunTrigger
		expected RunTrigger
	}{
		{"returns run when set", RunAfterPhase, RunAfterPhase},
		{"returns on-demand when set", RunOnDemand, RunOnDemand},
		{"returns default when empty", "", RunAfterWorkitem},
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

func TestValidator_GetModel(t *testing.T) {
	tests := []struct {
		name          string
		modelOverride string
		expected      string
	}{
		{"returns model when set", "opus", "opus"},
		{"returns empty when not set", "", ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			v := Validator{ModelOverride: tc.modelOverride}
			if got := v.GetModel(); got != tc.expected {
				t.Errorf("GetModel() = %q, want %q", got, tc.expected)
			}
		})
	}
}

func TestValidatorConfig_UnmarshalYAML_StringFormat(t *testing.T) {
	yamlContent := `validators/code-standards.md`

	var vc ValidatorConfig
	err := yaml.Unmarshal([]byte(yamlContent), &vc)
	if err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if vc.GetPath() != "validators/code-standards.md" {
		t.Errorf("expected path 'validators/code-standards.md', got %q", vc.GetPath())
	}
	if vc.GetModel() != "" {
		t.Errorf("expected empty model, got %q", vc.GetModel())
	}
	if vc.GetRun() != "" {
		t.Errorf("expected empty run, got %q", vc.GetRun())
	}
}

func TestValidatorConfig_UnmarshalYAML_ObjectFormat(t *testing.T) {
	yamlContent := `
path: validators/security.md
model: opus
run: after-phase
`

	var vc ValidatorConfig
	err := yaml.Unmarshal([]byte(yamlContent), &vc)
	if err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if vc.GetPath() != "validators/security.md" {
		t.Errorf("expected path 'validators/security.md', got %q", vc.GetPath())
	}
	if vc.GetModel() != "opus" {
		t.Errorf("expected model 'opus', got %q", vc.GetModel())
	}
	if vc.GetRun() != "after-phase" {
		t.Errorf("expected run 'after-phase', got %q", vc.GetRun())
	}
}

func TestValidatorConfig_UnmarshalYAML_ObjectPartial(t *testing.T) {
	// Object format with only path (model and run optional)
	yamlContent := `
path: validators/naming.md
`

	var vc ValidatorConfig
	err := yaml.Unmarshal([]byte(yamlContent), &vc)
	if err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if vc.GetPath() != "validators/naming.md" {
		t.Errorf("expected path 'validators/naming.md', got %q", vc.GetPath())
	}
	if vc.GetModel() != "" {
		t.Errorf("expected empty model, got %q", vc.GetModel())
	}
	if vc.GetRun() != "" {
		t.Errorf("expected empty run, got %q", vc.GetRun())
	}
}

func TestValidatorConfig_UnmarshalYAML_MixedList(t *testing.T) {
	// Test unmarshaling a list with mixed formats
	yamlContent := `
- validators/code-standards.md
- path: validators/security.md
  model: opus
- validators/naming.md
`

	var configs []ValidatorConfig
	err := yaml.Unmarshal([]byte(yamlContent), &configs)
	if err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if len(configs) != 3 {
		t.Fatalf("expected 3 configs, got %d", len(configs))
	}

	// First: string format
	if configs[0].GetPath() != "validators/code-standards.md" {
		t.Errorf("expected first path 'validators/code-standards.md', got %q", configs[0].GetPath())
	}
	if configs[0].GetModel() != "" {
		t.Errorf("expected first model to be empty, got %q", configs[0].GetModel())
	}

	// Second: object format
	if configs[1].GetPath() != "validators/security.md" {
		t.Errorf("expected second path 'validators/security.md', got %q", configs[1].GetPath())
	}
	if configs[1].GetModel() != "opus" {
		t.Errorf("expected second model 'opus', got %q", configs[1].GetModel())
	}

	// Third: string format
	if configs[2].GetPath() != "validators/naming.md" {
		t.Errorf("expected third path 'validators/naming.md', got %q", configs[2].GetPath())
	}
}
