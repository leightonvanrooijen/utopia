package domain

import (
	"errors"
	"strings"
	"testing"
)

func TestValidateAuthMode(t *testing.T) {
	tests := []struct {
		name      string
		mode      string
		wantError bool
	}{
		{
			name:      "api-key is valid",
			mode:      "api-key",
			wantError: false,
		},
		{
			name:      "subscription is valid",
			mode:      "subscription",
			wantError: false,
		},
		{
			name:      "empty means no selection and is valid",
			mode:      "",
			wantError: false,
		},
		{
			name:      "unknown mode is invalid",
			mode:      "oauth",
			wantError: true,
		},
		{
			name:      "case sensitive - API-KEY fails",
			mode:      "API-KEY",
			wantError: true,
		},
		{
			name:      "underscore variant fails",
			mode:      "api_key",
			wantError: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateAuthMode(tc.mode)

			if tc.wantError {
				if err == nil {
					t.Fatalf("ValidateAuthMode(%q) expected error, got nil", tc.mode)
				}
				var modeErr *InvalidAuthModeError
				if !errors.As(err, &modeErr) {
					t.Fatalf("ValidateAuthMode(%q) error type = %T, want *InvalidAuthModeError", tc.mode, err)
				}
				if !strings.Contains(err.Error(), tc.mode) {
					t.Errorf("error %q should name the invalid value %q", err.Error(), tc.mode)
				}
			} else if err != nil {
				t.Errorf("ValidateAuthMode(%q) unexpected error: %v", tc.mode, err)
			}
		})
	}
}

func TestValidateAuthConfig(t *testing.T) {
	tests := []struct {
		name      string
		config    *AuthConfig
		wantError bool
	}{
		{
			name:      "nil config is valid",
			config:    nil,
			wantError: false,
		},
		{
			name:      "empty config is valid",
			config:    &AuthConfig{},
			wantError: false,
		},
		{
			name:      "api-key mode is valid",
			config:    &AuthConfig{Mode: "api-key"},
			wantError: false,
		},
		{
			name:      "subscription mode is valid",
			config:    &AuthConfig{Mode: "subscription"},
			wantError: false,
		},
		{
			name:      "unknown mode is invalid",
			config:    &AuthConfig{Mode: "bedrock"},
			wantError: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateAuthConfig(tc.config)

			if tc.wantError && err == nil {
				t.Error("expected error, got nil")
			}
			if !tc.wantError && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestAuthConfig_GetMode(t *testing.T) {
	tests := []struct {
		name     string
		config   *AuthConfig
		expected AuthMode
	}{
		{
			name:     "nil config has no mode",
			config:   nil,
			expected: "",
		},
		{
			name:     "empty config has no mode",
			config:   &AuthConfig{},
			expected: "",
		},
		{
			name:     "api-key mode",
			config:   &AuthConfig{Mode: "api-key"},
			expected: AuthModeAPIKey,
		},
		{
			name:     "subscription mode",
			config:   &AuthConfig{Mode: "subscription"},
			expected: AuthModeSubscription,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.config.GetMode(); got != tc.expected {
				t.Errorf("GetMode() = %q, want %q", got, tc.expected)
			}
		})
	}
}

func TestValidAuthModes(t *testing.T) {
	modes := ValidAuthModes()

	if len(modes) != 2 {
		t.Fatalf("ValidAuthModes() returned %d modes, want 2", len(modes))
	}

	expected := []AuthMode{AuthModeAPIKey, AuthModeSubscription}
	for i, mode := range expected {
		if modes[i] != mode {
			t.Errorf("ValidAuthModes()[%d] = %q, want %q", i, modes[i], mode)
		}
	}
}

func TestInvalidAuthModeError_Message(t *testing.T) {
	err := &InvalidAuthModeError{Mode: "oauth"}
	expected := `invalid auth mode "oauth": valid options are api-key, subscription`
	if err.Error() != expected {
		t.Errorf("Error() = %q, want %q", err.Error(), expected)
	}
}

func TestInvalidAuthModeError_Is(t *testing.T) {
	err1 := &InvalidAuthModeError{Mode: "foo"}
	err2 := &InvalidAuthModeError{Mode: "bar"}

	if !errors.Is(err1, err2) {
		t.Error("errors.Is should match any InvalidAuthModeError")
	}
}
