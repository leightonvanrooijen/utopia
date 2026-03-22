package domain

import (
	"errors"
	"testing"
)

func TestResolveModel(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		expected  string
		wantError bool
	}{
		{
			name:      "haiku maps correctly",
			input:     "haiku",
			expected:  "claude-3-5-haiku-20241022",
			wantError: false,
		},
		{
			name:      "sonnet maps correctly",
			input:     "sonnet",
			expected:  "claude-sonnet-4-20250514",
			wantError: false,
		},
		{
			name:      "opus maps correctly",
			input:     "opus",
			expected:  "claude-opus-4-20250514",
			wantError: false,
		},
		{
			name:      "invalid model returns error",
			input:     "invalid",
			expected:  "",
			wantError: true,
		},
		{
			name:      "empty string returns error",
			input:     "",
			expected:  "",
			wantError: true,
		},
		{
			name:      "case sensitive - HAIKU fails",
			input:     "HAIKU",
			expected:  "",
			wantError: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ResolveModel(tc.input)

			if tc.wantError {
				if err == nil {
					t.Errorf("ResolveModel(%q) expected error, got nil", tc.input)
				}
				var invalidErr *InvalidModelError
				if !errors.As(err, &invalidErr) {
					t.Errorf("ResolveModel(%q) error type = %T, want *InvalidModelError", tc.input, err)
				}
			} else {
				if err != nil {
					t.Errorf("ResolveModel(%q) unexpected error: %v", tc.input, err)
				}
				if got != tc.expected {
					t.Errorf("ResolveModel(%q) = %q, want %q", tc.input, got, tc.expected)
				}
			}
		})
	}
}

func TestInvalidModelError_Message(t *testing.T) {
	err := &InvalidModelError{Name: "foobar"}
	expected := `invalid model name "foobar": valid options are haiku, sonnet, opus`
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

	if len(names) != 3 {
		t.Fatalf("ValidModelNames() returned %d names, want 3", len(names))
	}

	expected := []ModelName{ModelHaiku, ModelSonnet, ModelOpus}
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
			config:    &ModelConfig{Default: "sonnet", CR: "opus", Execute: "haiku"},
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
