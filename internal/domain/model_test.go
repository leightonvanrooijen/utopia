package domain

import (
	"errors"
	"strings"
	"testing"
)

func TestValidateModel(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantError bool
	}{
		{name: "haiku alias accepted", input: "haiku"},
		{name: "sonnet alias accepted", input: "sonnet"},
		{name: "opus alias accepted", input: "opus"},
		{name: "fable alias accepted", input: "fable"},
		{name: "full model identifier accepted", input: "claude-model-1-20260101"},
		{name: "undated model identifier accepted", input: "claude-sonnet-4-6"},
		{name: "identifier with context-window suffix accepted", input: "claude-opus-5[1m]"},
		{name: "unrecognised name returns error", input: "invalid", wantError: true},
		{name: "other vendor identifier returns error", input: "gpt-5", wantError: true},
		{name: "bare claude prefix returns error", input: "claude-", wantError: true},
		{name: "empty string returns error", input: "", wantError: true},
		{name: "case sensitive - HAIKU fails", input: "HAIKU", wantError: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateModel(tc.input)

			if tc.wantError {
				if err == nil {
					t.Fatalf("ValidateModel(%q) expected error, got nil", tc.input)
				}
				var invalidErr *InvalidModelError
				if !errors.As(err, &invalidErr) {
					t.Errorf("ValidateModel(%q) error type = %T, want *InvalidModelError", tc.input, err)
				}
			} else if err != nil {
				t.Errorf("ValidateModel(%q) unexpected error: %v", tc.input, err)
			}
		})
	}
}

func TestInvalidModelError_Message(t *testing.T) {
	err := &InvalidModelError{Name: "foobar"}
	expected := `invalid model "foobar": use an alias (haiku, sonnet, opus, fable) or a full model identifier beginning with "claude-"`
	if err.Error() != expected {
		t.Errorf("Error() = %q, want %q", err.Error(), expected)
	}
}

func TestInvalidModelError_Is(t *testing.T) {
	err1 := &InvalidModelError{Name: "foo"}
	err2 := &InvalidModelError{Name: "bar"}

	if !errors.Is(err1, err2) {
		t.Error("errors.Is should match any InvalidModelError")
	}
}

func TestValidModelNames(t *testing.T) {
	names := ValidModelNames()

	if len(names) != 4 {
		t.Fatalf("ValidModelNames() returned %d names, want 4", len(names))
	}

	expected := []ModelName{ModelHaiku, ModelSonnet, ModelOpus, ModelFable}
	for i, name := range expected {
		if names[i] != name {
			t.Errorf("ValidModelNames()[%d] = %q, want %q", i, names[i], name)
		}
	}
}

func TestModelConfig_ModelForCommand(t *testing.T) {
	tests := []struct {
		name     string
		config   *ModelConfig
		command  string
		expected string
	}{
		{
			name:     "nil config returns sonnet",
			config:   nil,
			command:  "cr",
			expected: "sonnet",
		},
		{
			name:     "empty config returns sonnet",
			config:   &ModelConfig{},
			command:  "cr",
			expected: "sonnet",
		},
		{
			name:     "uses default when command not specified",
			config:   &ModelConfig{Default: "opus"},
			command:  "cr",
			expected: "opus",
		},
		{
			name:     "command override takes precedence over default",
			config:   &ModelConfig{Default: "opus", CR: "haiku"},
			command:  "cr",
			expected: "haiku",
		},
		{
			name:     "harvest command",
			config:   &ModelConfig{Harvest: "opus"},
			command:  "harvest",
			expected: "opus",
		},
		{
			name:     "execute command",
			config:   &ModelConfig{Execute: "haiku"},
			command:  "execute",
			expected: "haiku",
		},
		{
			name:     "validators command",
			config:   &ModelConfig{Validators: "opus"},
			command:  "validators",
			expected: "opus",
		},
		{
			name:     "discover command",
			config:   &ModelConfig{Discover: "haiku"},
			command:  "discover",
			expected: "haiku",
		},
		{
			name:     "standards command",
			config:   &ModelConfig{Standards: "opus"},
			command:  "standards",
			expected: "opus",
		},
		{
			name:     "refactor command",
			config:   &ModelConfig{Refactor: "haiku"},
			command:  "refactor",
			expected: "haiku",
		},
		{
			name:     "shape command",
			config:   &ModelConfig{Shape: "opus"},
			command:  "shape",
			expected: "opus",
		},
		{
			name:     "validator_create command",
			config:   &ModelConfig{ValidatorCreate: "haiku"},
			command:  "validator_create",
			expected: "haiku",
		},
		{
			name:     "validator_edit command",
			config:   &ModelConfig{ValidatorEdit: "opus"},
			command:  "validator_edit",
			expected: "opus",
		},
		{
			name:     "unknown command falls back to default",
			config:   &ModelConfig{Default: "opus"},
			command:  "unknown",
			expected: "opus",
		},
		{
			name:     "unknown command with no default returns sonnet",
			config:   &ModelConfig{CR: "opus"},
			command:  "unknown",
			expected: "sonnet",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.config.ModelForCommand(tc.command)
			if got != tc.expected {
				t.Errorf("ModelForCommand(%q) = %q, want %q", tc.command, got, tc.expected)
			}
		})
	}
}

func TestValidateModelConfig(t *testing.T) {
	tests := []struct {
		name      string
		config    *ModelConfig
		wantError bool
		errCount  int // number of invalid fields expected in error
	}{
		{
			name:      "nil config is valid",
			config:    nil,
			wantError: false,
		},
		{
			name:      "empty config is valid",
			config:    &ModelConfig{},
			wantError: false,
		},
		{
			name:      "all valid model names",
			config:    &ModelConfig{Default: "sonnet", CR: "opus", Execute: "haiku", Harvest: "fable"},
			wantError: false,
		},
		{
			name:      "full model identifier is valid",
			config:    &ModelConfig{Default: "claude-model-1-20260101"},
			wantError: false,
		},
		{
			name:      "invalid default",
			config:    &ModelConfig{Default: "gpt4"},
			wantError: true,
			errCount:  1,
		},
		{
			name:      "invalid cr",
			config:    &ModelConfig{CR: "invalid"},
			wantError: true,
			errCount:  1,
		},
		{
			name:      "multiple invalid fields",
			config:    &ModelConfig{Default: "gpt4", CR: "invalid", Execute: "bad"},
			wantError: true,
			errCount:  3,
		},
		{
			name:      "mixed valid and invalid",
			config:    &ModelConfig{Default: "sonnet", CR: "invalid"},
			wantError: true,
			errCount:  1,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateModelConfig(tc.config)

			if tc.wantError {
				if err == nil {
					t.Error("expected error, got nil")
					return
				}
				var configErr *InvalidModelConfigError
				if !errors.As(err, &configErr) {
					t.Errorf("error type = %T, want *InvalidModelConfigError", err)
					return
				}
				if len(configErr.InvalidFields) != tc.errCount {
					t.Errorf("got %d invalid fields, want %d", len(configErr.InvalidFields), tc.errCount)
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			}
		})
	}
}

func TestValidateValidatorModels(t *testing.T) {
	tests := []struct {
		name       string
		validators []ValidatorConfig
		wantError  bool
	}{
		{
			name:       "no validators is valid",
			validators: nil,
		},
		{
			name:       "no model override is valid",
			validators: []ValidatorConfig{{Path: "validators/a.md"}},
		},
		{
			name: "aliases and identifiers are valid",
			validators: []ValidatorConfig{
				{Path: "validators/a.md", Model: "fable"},
				{Path: "validators/b.md", Model: "claude-model-1-20260101"},
			},
		},
		{
			name:       "unrecognised model is invalid",
			validators: []ValidatorConfig{{Path: "validators/a.md", Model: "gpt4"}},
			wantError:  true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateValidatorModels(tc.validators)

			if tc.wantError {
				if !errors.Is(err, &InvalidModelConfigError{}) {
					t.Errorf("ValidateValidatorModels() error = %v, want *InvalidModelConfigError", err)
				}
			} else if err != nil {
				t.Errorf("ValidateValidatorModels() unexpected error: %v", err)
			}
		})
	}
}

func TestInvalidModelConfigError_Message(t *testing.T) {
	err := &InvalidModelConfigError{InvalidFields: []string{`models.default: "gpt4"`, `models.cr: "invalid"`}}
	msg := err.Error()

	if msg == "" {
		t.Error("expected non-empty error message")
	}
	if !errors.Is(err, &InvalidModelConfigError{}) {
		t.Error("errors.Is should match InvalidModelConfigError")
	}
}

func TestValidateEscalationOrder(t *testing.T) {
	tests := []struct {
		name      string
		config    *ModelConfig
		wantError bool
		wantCount int // escalation paths named in the error
	}{
		{
			name:   "nil config is valid",
			config: nil,
		},
		{
			name:   "empty config defaults to sonnet executor and opus escalations",
			config: &ModelConfig{},
		},
		{
			name:   "explicit upward escalation is valid",
			config: &ModelConfig{Execute: "sonnet", ExecuteEscalated: "opus", Scoper: "fable"},
		},
		{
			name:   "escalating sideways is valid",
			config: &ModelConfig{Execute: "opus", ExecuteEscalated: "opus"},
		},
		{
			name:      "escalated executor below executor is invalid",
			config:    &ModelConfig{Execute: "opus", ExecuteEscalated: "sonnet"},
			wantError: true,
			wantCount: 2, // scoper inherits execute_escalated
		},
		{
			name:      "scoper below executor is invalid",
			config:    &ModelConfig{Execute: "opus", ExecuteEscalated: "fable", Scoper: "haiku"},
			wantError: true,
			wantCount: 1,
		},
		{
			name:      "inherited opus default below a fable executor is invalid",
			config:    &ModelConfig{Execute: "fable"},
			wantError: true,
			wantCount: 2,
		},
		{
			name:   "executor inherits models.default for the comparison",
			config: &ModelConfig{Default: "fable", ExecuteEscalated: "fable"},
		},
		{
			name:   "pinned executor identifier is unrankable so ordering is not checked",
			config: &ModelConfig{Execute: "claude-model-1-20260101", ExecuteEscalated: "haiku"},
		},
		{
			name:   "pinned escalation identifier is unrankable so ordering is not checked",
			config: &ModelConfig{Execute: "opus", ExecuteEscalated: "claude-model-1-20260101"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateEscalationOrder(tc.config)

			if !tc.wantError {
				if err != nil {
					t.Errorf("ValidateEscalationOrder() unexpected error: %v", err)
				}
				return
			}

			var downErr *DownwardEscalationError
			if !errors.As(err, &downErr) {
				t.Fatalf("error type = %T, want *DownwardEscalationError", err)
			}
			if len(downErr.Downward) != tc.wantCount {
				t.Errorf("got %d downward paths, want %d: %v", len(downErr.Downward), tc.wantCount, downErr.Downward)
			}
		})
	}
}

func TestDownwardEscalationError_Message(t *testing.T) {
	err := &DownwardEscalationError{
		Executor: "opus",
		Downward: []DownwardEscalation{{Field: "models.scoper", Model: "haiku"}},
	}
	msg := err.Error()

	for _, want := range []string{"models.scoper", `"haiku"`, "models.execute", `"opus"`} {
		if !strings.Contains(msg, want) {
			t.Errorf("message %q missing %q", msg, want)
		}
	}
	if !errors.Is(err, &DownwardEscalationError{}) {
		t.Error("errors.Is should match DownwardEscalationError")
	}
}

func TestModelConfig_EscalationResolution(t *testing.T) {
	tests := []struct {
		name          string
		config        *ModelConfig
		wantExecutor  string
		wantEscalated string
		wantScoper    string
	}{
		{
			name:          "nil config",
			config:        nil,
			wantExecutor:  "sonnet",
			wantEscalated: "opus",
			wantScoper:    "opus",
		},
		{
			name:          "escalations ignore default and execute",
			config:        &ModelConfig{Default: "haiku", Execute: "sonnet"},
			wantExecutor:  "sonnet",
			wantEscalated: "opus",
			wantScoper:    "opus",
		},
		{
			name:          "scoper inherits execute_escalated",
			config:        &ModelConfig{ExecuteEscalated: "fable"},
			wantExecutor:  "sonnet",
			wantEscalated: "fable",
			wantScoper:    "fable",
		},
		{
			name:          "explicit scoper wins",
			config:        &ModelConfig{ExecuteEscalated: "opus", Scoper: "fable"},
			wantExecutor:  "sonnet",
			wantEscalated: "opus",
			wantScoper:    "fable",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.config.ExecutorModel(); got != tc.wantExecutor {
				t.Errorf("ExecutorModel() = %q, want %q", got, tc.wantExecutor)
			}
			if got := tc.config.EscalatedExecutorModel(); got != tc.wantEscalated {
				t.Errorf("EscalatedExecutorModel() = %q, want %q", got, tc.wantEscalated)
			}
			if got := tc.config.ScoperModel(); got != tc.wantScoper {
				t.Errorf("ScoperModel() = %q, want %q", got, tc.wantScoper)
			}
		})
	}
}
