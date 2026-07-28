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

func TestResolveAuthMode(t *testing.T) {
	tests := []struct {
		name     string
		override AuthMode
		config   *AuthConfig
		expected AuthMode
	}{
		{
			name:     "no override and no config inherits the environment",
			override: "",
			config:   nil,
			expected: "",
		},
		{
			name:     "config applies when no override is given",
			override: "",
			config:   &AuthConfig{Mode: "subscription"},
			expected: AuthModeSubscription,
		},
		{
			name:     "override wins over the configured mode",
			override: AuthModeAPIKey,
			config:   &AuthConfig{Mode: "subscription"},
			expected: AuthModeAPIKey,
		},
		{
			name:     "override wins in the other direction too",
			override: AuthModeSubscription,
			config:   &AuthConfig{Mode: "api-key"},
			expected: AuthModeSubscription,
		},
		{
			name:     "override applies when no config section exists",
			override: AuthModeSubscription,
			config:   nil,
			expected: AuthModeSubscription,
		},
		{
			name:     "override matching the config is a no-op",
			override: AuthModeAPIKey,
			config:   &AuthConfig{Mode: "api-key"},
			expected: AuthModeAPIKey,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := ResolveAuthMode(tc.override, tc.config); got != tc.expected {
				t.Errorf("ResolveAuthMode(%q, %v) = %q, want %q", tc.override, tc.config, got, tc.expected)
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

func TestResolveAPIKeyCredential(t *testing.T) {
	tests := []struct {
		name       string
		envFile    map[string]string
		ambient    []string
		wantKey    string
		wantSource CredentialSource
		wantError  bool
	}{
		{
			name:       "env file key is used",
			envFile:    map[string]string{"ANTHROPIC_API_KEY": "sk-file"},
			ambient:    []string{"PATH=/usr/bin"},
			wantKey:    "sk-file",
			wantSource: CredentialSourceEnvFile,
		},
		{
			name:       "ambient key is used when the env file has none",
			envFile:    nil,
			ambient:    []string{"PATH=/usr/bin", "ANTHROPIC_API_KEY=sk-ambient"},
			wantKey:    "sk-ambient",
			wantSource: CredentialSourceEnvironment,
		},
		{
			name:       "env file wins over the ambient environment",
			envFile:    map[string]string{"ANTHROPIC_API_KEY": "sk-file"},
			ambient:    []string{"ANTHROPIC_API_KEY=sk-ambient"},
			wantKey:    "sk-file",
			wantSource: CredentialSourceEnvFile,
		},
		{
			name:       "empty env file value falls through to the environment",
			envFile:    map[string]string{"ANTHROPIC_API_KEY": ""},
			ambient:    []string{"ANTHROPIC_API_KEY=sk-ambient"},
			wantKey:    "sk-ambient",
			wantSource: CredentialSourceEnvironment,
		},
		{
			name:      "no key in either location is an error",
			envFile:   nil,
			ambient:   []string{"PATH=/usr/bin"},
			wantError: true,
		},
		{
			name:      "empty ambient value counts as absent",
			envFile:   nil,
			ambient:   []string{"ANTHROPIC_API_KEY="},
			wantError: true,
		},
		{
			name:      "an auth token alone does not satisfy api-key mode",
			envFile:   nil,
			ambient:   []string{"ANTHROPIC_AUTH_TOKEN=tok"},
			wantError: true,
		},
		{
			name:       "value containing an equals sign survives intact",
			envFile:    nil,
			ambient:    []string{"ANTHROPIC_API_KEY=sk-a=b=c"},
			wantKey:    "sk-a=b=c",
			wantSource: CredentialSourceEnvironment,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			credential, err := ResolveAPIKeyCredential(tc.envFile, tc.ambient)

			if tc.wantError {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				var missing *MissingAPIKeyError
				if !errors.As(err, &missing) {
					t.Fatalf("error type = %T, want *MissingAPIKeyError", err)
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if credential.Key != tc.wantKey {
				t.Errorf("Key = %q, want %q", credential.Key, tc.wantKey)
			}
			if credential.Source != tc.wantSource {
				t.Errorf("Source = %q, want %q", credential.Source, tc.wantSource)
			}
		})
	}
}

func TestAPIKeyCredential_Env(t *testing.T) {
	tests := []struct {
		name    string
		key     string
		ambient []string
		want    []string
	}{
		{
			name:    "resolved key is added",
			key:     "sk-file",
			ambient: []string{"PATH=/usr/bin"},
			want:    []string{"PATH=/usr/bin", "ANTHROPIC_API_KEY=sk-file"},
		},
		{
			name:    "auth token is removed entirely, not blanked",
			key:     "sk-file",
			ambient: []string{"ANTHROPIC_AUTH_TOKEN=tok", "PATH=/usr/bin"},
			want:    []string{"PATH=/usr/bin", "ANTHROPIC_API_KEY=sk-file"},
		},
		{
			name:    "ambient key is replaced rather than duplicated",
			key:     "sk-file",
			ambient: []string{"ANTHROPIC_API_KEY=sk-ambient", "PATH=/usr/bin"},
			want:    []string{"PATH=/usr/bin", "ANTHROPIC_API_KEY=sk-file"},
		},
		{
			name:    "every other variable passes through unchanged",
			key:     "sk-file",
			ambient: []string{"HOME=/home/u", "ANTHROPIC_BASE_URL=https://x", "LANG=en_NZ"},
			want:    []string{"HOME=/home/u", "ANTHROPIC_BASE_URL=https://x", "LANG=en_NZ", "ANTHROPIC_API_KEY=sk-file"},
		},
		{
			name:    "empty ambient environment yields just the key",
			key:     "sk-file",
			ambient: nil,
			want:    []string{"ANTHROPIC_API_KEY=sk-file"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			credential := APIKeyCredential{Key: tc.key, Source: CredentialSourceEnvFile}

			got := credential.Env(tc.ambient)

			if len(got) != len(tc.want) {
				t.Fatalf("Env() = %v, want %v", got, tc.want)
			}
			for i := range tc.want {
				if got[i] != tc.want[i] {
					t.Errorf("Env()[%d] = %q, want %q", i, got[i], tc.want[i])
				}
			}
		})
	}
}

func TestAPIKeyCredential_EnvDoesNotMutateAmbient(t *testing.T) {
	ambient := []string{"ANTHROPIC_AUTH_TOKEN=tok", "PATH=/usr/bin"}

	APIKeyCredential{Key: "sk-file"}.Env(ambient)

	if ambient[0] != "ANTHROPIC_AUTH_TOKEN=tok" || ambient[1] != "PATH=/usr/bin" {
		t.Errorf("Env() mutated the ambient slice: %v", ambient)
	}
}

func TestMissingAPIKeyError_Message(t *testing.T) {
	err := &MissingAPIKeyError{}

	// The message must name both locations that were searched, so the user
	// knows where to put the key.
	for _, location := range []string{"ANTHROPIC_API_KEY", ".utopia/.env", "the environment"} {
		if !strings.Contains(err.Error(), location) {
			t.Errorf("error %q should mention %q", err.Error(), location)
		}
	}
}

func TestMissingAPIKeyError_Is(t *testing.T) {
	if !errors.Is(&MissingAPIKeyError{}, &MissingAPIKeyError{}) {
		t.Error("errors.Is should match any MissingAPIKeyError")
	}
	if errors.Is(&MissingAPIKeyError{}, &InvalidAuthModeError{}) {
		t.Error("MissingAPIKeyError should not match a different auth error")
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
