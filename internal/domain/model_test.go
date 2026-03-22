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
