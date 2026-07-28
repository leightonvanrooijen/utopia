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

func TestApplyEnvFile(t *testing.T) {
	tests := []struct {
		name    string
		ambient []string
		envFile map[string]string
		want    []string
	}{
		{
			name:    "a file-only variable is appended",
			ambient: []string{"PATH=/usr/bin"},
			envFile: map[string]string{"ANTHROPIC_BASE_URL": "https://gateway"},
			want:    []string{"PATH=/usr/bin", "ANTHROPIC_BASE_URL=https://gateway"},
		},
		{
			name:    "the file wins over the same ambient variable",
			ambient: []string{"ANTHROPIC_BASE_URL=https://ambient", "PATH=/usr/bin"},
			envFile: map[string]string{"ANTHROPIC_BASE_URL": "https://file"},
			want:    []string{"ANTHROPIC_BASE_URL=https://file", "PATH=/usr/bin"},
		},
		{
			name:    "an overridden variable is replaced, not duplicated",
			ambient: []string{"ANTHROPIC_BASE_URL=https://ambient"},
			envFile: map[string]string{"ANTHROPIC_BASE_URL": "https://file"},
			want:    []string{"ANTHROPIC_BASE_URL=https://file"},
		},
		{
			name:    "a name duplicated in ambient is overridden at every position",
			ambient: []string{"ANTHROPIC_BASE_URL=https://first", "ANTHROPIC_BASE_URL=https://second"},
			envFile: map[string]string{"ANTHROPIC_BASE_URL": "https://file"},
			want:    []string{"ANTHROPIC_BASE_URL=https://file", "ANTHROPIC_BASE_URL=https://file"},
		},
		{
			name:    "file-only variables are appended in sorted order",
			ambient: []string{"PATH=/usr/bin"},
			envFile: map[string]string{
				"ANTHROPIC_SMALL_FAST_MODEL": "haiku",
				"ANTHROPIC_BASE_URL":         "https://gateway",
				"ANTHROPIC_LOG":              "debug",
			},
			want: []string{
				"PATH=/usr/bin",
				"ANTHROPIC_BASE_URL=https://gateway",
				"ANTHROPIC_LOG=debug",
				"ANTHROPIC_SMALL_FAST_MODEL=haiku",
			},
		},
		{
			// The auth mode owns the credential slot; the file's copy must not be
			// reapplied here or api-key mode would send both credentials at once.
			name:    "credential variables are not overlaid",
			ambient: []string{"PATH=/usr/bin"},
			envFile: map[string]string{
				APIKeyEnvVar:    "sk-file",
				AuthTokenEnvVar: "tok-file",
			},
			want: []string{"PATH=/usr/bin"},
		},
		{
			name:    "an ambient credential is left for the mode to handle",
			ambient: []string{"ANTHROPIC_API_KEY=sk-ambient", "PATH=/usr/bin"},
			envFile: map[string]string{APIKeyEnvVar: "sk-file"},
			want:    []string{"ANTHROPIC_API_KEY=sk-ambient", "PATH=/usr/bin"},
		},
		{
			name:    "an empty value from the file still overrides",
			ambient: []string{"ANTHROPIC_LOG=debug"},
			envFile: map[string]string{"ANTHROPIC_LOG": ""},
			want:    []string{"ANTHROPIC_LOG="},
		},
		{
			name:    "a missing file leaves the environment alone",
			ambient: []string{"PATH=/usr/bin"},
			envFile: nil,
			want:    []string{"PATH=/usr/bin"},
		},
		{
			name:    "an empty ambient environment yields just the file",
			ambient: nil,
			envFile: map[string]string{"ANTHROPIC_BASE_URL": "https://gateway"},
			want:    []string{"ANTHROPIC_BASE_URL=https://gateway"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := ApplyEnvFile(tc.ambient, tc.envFile)

			if len(got) != len(tc.want) {
				t.Fatalf("ApplyEnvFile() = %v, want %v", got, tc.want)
			}
			for i := range tc.want {
				if got[i] != tc.want[i] {
					t.Errorf("ApplyEnvFile()[%d] = %q, want %q", i, got[i], tc.want[i])
				}
			}
		})
	}
}

func TestApplyEnvFileDoesNotMutateAmbient(t *testing.T) {
	ambient := []string{"ANTHROPIC_BASE_URL=https://ambient", "PATH=/usr/bin"}

	ApplyEnvFile(ambient, map[string]string{"ANTHROPIC_BASE_URL": "https://file"})

	if ambient[0] != "ANTHROPIC_BASE_URL=https://ambient" || ambient[1] != "PATH=/usr/bin" {
		t.Errorf("ApplyEnvFile() mutated the ambient slice: %v", ambient)
	}
}

func TestSubscriptionEnv(t *testing.T) {
	tests := []struct {
		name    string
		ambient []string
		want    []string
	}{
		{
			name:    "ambient api key is removed",
			ambient: []string{"PATH=/usr/bin", "ANTHROPIC_API_KEY=sk-ambient"},
			want:    []string{"PATH=/usr/bin"},
		},
		{
			name:    "ambient auth token is removed",
			ambient: []string{"PATH=/usr/bin", "ANTHROPIC_AUTH_TOKEN=tok"},
			want:    []string{"PATH=/usr/bin"},
		},
		{
			name:    "both credentials are removed together",
			ambient: []string{"ANTHROPIC_API_KEY=sk-ambient", "PATH=/usr/bin", "ANTHROPIC_AUTH_TOKEN=tok"},
			want:    []string{"PATH=/usr/bin"},
		},
		{
			name:    "every duplicate of a credential is removed",
			ambient: []string{"ANTHROPIC_API_KEY=sk-one", "PATH=/usr/bin", "ANTHROPIC_API_KEY=sk-two"},
			want:    []string{"PATH=/usr/bin"},
		},
		{
			name:    "every other variable passes through unchanged and in order",
			ambient: []string{"HOME=/home/u", "ANTHROPIC_BASE_URL=https://x", "LANG=en_NZ", "EMPTY="},
			want:    []string{"HOME=/home/u", "ANTHROPIC_BASE_URL=https://x", "LANG=en_NZ", "EMPTY="},
		},
		{
			name:    "an environment with no credentials is passed through whole",
			ambient: []string{"PATH=/usr/bin", "TERM=xterm"},
			want:    []string{"PATH=/usr/bin", "TERM=xterm"},
		},
		{
			name:    "a name merely prefixed by a credential name is kept",
			ambient: []string{"ANTHROPIC_API_KEY_FILE=/tmp/k", "ANTHROPIC_AUTH_TOKEN_PATH=/tmp/t"},
			want:    []string{"ANTHROPIC_API_KEY_FILE=/tmp/k", "ANTHROPIC_AUTH_TOKEN_PATH=/tmp/t"},
		},
		{
			name:    "empty ambient environment yields an empty environment",
			ambient: nil,
			want:    []string{},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := SubscriptionEnv(tc.ambient)

			if len(got) != len(tc.want) {
				t.Fatalf("SubscriptionEnv() = %v, want %v", got, tc.want)
			}
			for i := range tc.want {
				if got[i] != tc.want[i] {
					t.Errorf("SubscriptionEnv()[%d] = %q, want %q", i, got[i], tc.want[i])
				}
			}
		})
	}
}

// A nil Env makes os/exec inherit the parent environment, which would hand the
// subprocess the ambient key this mode exists to suppress. The empty case must
// therefore be an empty environment, not an absent one.
func TestSubscriptionEnv_NeverReturnsNil(t *testing.T) {
	for _, ambient := range [][]string{nil, {}, {"ANTHROPIC_API_KEY=sk-ambient"}} {
		if SubscriptionEnv(ambient) == nil {
			t.Errorf("SubscriptionEnv(%v) returned nil, which os/exec reads as inherit-the-parent-environment", ambient)
		}
	}
}

// Blanking a credential leaves it in the precedence chain, so the removed names
// must not reappear with an empty value.
func TestSubscriptionEnv_RemovesRatherThanBlanks(t *testing.T) {
	ambient := []string{"ANTHROPIC_API_KEY=sk-ambient", "ANTHROPIC_AUTH_TOKEN=tok", "PATH=/usr/bin"}

	for _, entry := range SubscriptionEnv(ambient) {
		switch envName(entry) {
		case APIKeyEnvVar, AuthTokenEnvVar:
			t.Errorf("SubscriptionEnv() kept %q; the variable must be absent, not blank", entry)
		}
	}
}

func TestSubscriptionEnv_DoesNotMutateAmbient(t *testing.T) {
	ambient := []string{"ANTHROPIC_API_KEY=sk-ambient", "PATH=/usr/bin"}

	SubscriptionEnv(ambient)

	if ambient[0] != "ANTHROPIC_API_KEY=sk-ambient" || ambient[1] != "PATH=/usr/bin" {
		t.Errorf("SubscriptionEnv() mutated the ambient slice: %v", ambient)
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

func TestAuthSelectionReport(t *testing.T) {
	tests := []struct {
		name      string
		selection AuthSelection
		want      string
	}{
		{
			name:      "no selection reports nothing",
			selection: AuthSelection{},
			want:      "",
		},
		{
			name:      "api-key names the project env file it came from",
			selection: AuthSelection{Mode: AuthModeAPIKey, Source: CredentialSourceEnvFile},
			want:      "Auth: api-key (ANTHROPIC_API_KEY from .utopia/.env)",
		},
		{
			name:      "api-key names the inherited environment it came from",
			selection: AuthSelection{Mode: AuthModeAPIKey, Source: CredentialSourceEnvironment},
			want:      "Auth: api-key (ANTHROPIC_API_KEY from the environment)",
		},
		{
			name:      "subscription with a clean environment names only the mode",
			selection: AuthSelection{Mode: AuthModeSubscription},
			want:      "Auth: subscription",
		},
		{
			name: "subscription says an ambient key was ignored",
			selection: AuthSelection{
				Mode:       AuthModeSubscription,
				Suppressed: []string{APIKeyEnvVar},
			},
			want: "Auth: subscription (ambient ANTHROPIC_API_KEY ignored)",
		},
		{
			name: "subscription names every credential it ignored",
			selection: AuthSelection{
				Mode:       AuthModeSubscription,
				Suppressed: []string{APIKeyEnvVar, AuthTokenEnvVar},
			},
			want: "Auth: subscription (ambient ANTHROPIC_API_KEY, ANTHROPIC_AUTH_TOKEN ignored)",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.selection.Report(); got != tc.want {
				t.Errorf("Report() = %q, want %q", got, tc.want)
			}
		})
	}
}

// The report exists to name the account that pays, never the credential itself -
// a key in any field of the selection must not reach the line. AuthSelection has
// no field to hold one, and this pins that: a key placed where a value could
// plausibly be mistaken for a location still cannot appear.
func TestAuthSelectionReportNeverPrintsACredential(t *testing.T) {
	const secret = "sk-ant-api03-DO-NOT-PRINT"

	selections := []AuthSelection{
		{Mode: AuthModeAPIKey, Source: CredentialSourceEnvFile},
		{Mode: AuthModeAPIKey, Source: CredentialSourceEnvironment},
		{Mode: AuthModeSubscription, Suppressed: []string{APIKeyEnvVar, AuthTokenEnvVar}},
	}

	for _, selection := range selections {
		report := selection.Report()
		if strings.Contains(report, secret) {
			t.Errorf("Report() = %q leaks the credential", report)
		}
		// Not even a prefix of it: a truncated key is still key material.
		if strings.Contains(report, secret[:12]) {
			t.Errorf("Report() = %q leaks part of the credential", report)
		}
	}
}

// An unrecognised mode is still reported rather than passed over in silence. It
// cannot reach here through either entry point, but a report is the wrong place
// to decide a mode does not exist - the resolution that rejects it is.
func TestAuthSelectionReportUnknownMode(t *testing.T) {
	got := AuthSelection{Mode: AuthMode("teamplan")}.Report()
	if want := "Auth: teamplan"; got != want {
		t.Errorf("Report() = %q, want %q", got, want)
	}
}

func TestSuppressedCredentialVars(t *testing.T) {
	tests := []struct {
		name    string
		ambient []string
		want    []string
	}{
		{
			name:    "a clean environment suppresses nothing",
			ambient: []string{"PATH=/usr/bin", "HOME=/home/dev"},
			want:    nil,
		},
		{
			name:    "an ambient api key",
			ambient: []string{"PATH=/usr/bin", APIKeyEnvVar + "=sk-ambient"},
			want:    []string{APIKeyEnvVar},
		},
		{
			name:    "an ambient auth token",
			ambient: []string{AuthTokenEnvVar + "=token-ambient"},
			want:    []string{AuthTokenEnvVar},
		},
		{
			name:    "both, always in the same order",
			ambient: []string{AuthTokenEnvVar + "=token-ambient", APIKeyEnvVar + "=sk-ambient"},
			want:    []string{APIKeyEnvVar, AuthTokenEnvVar},
		},
		{
			// An empty value is not a credential, so there is nothing to say was
			// ignored - and SubscriptionEnv drops it either way.
			name:    "an empty value is not a suppressed credential",
			ambient: []string{APIKeyEnvVar + "=", AuthTokenEnvVar + "="},
			want:    nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := SuppressedCredentialVars(tc.ambient)
			if len(got) != len(tc.want) {
				t.Fatalf("SuppressedCredentialVars() = %v, want %v", got, tc.want)
			}
			for i, name := range tc.want {
				if got[i] != name {
					t.Errorf("SuppressedCredentialVars()[%d] = %q, want %q", i, got[i], name)
				}
			}
		})
	}
}
