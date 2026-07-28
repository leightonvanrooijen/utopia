package internal

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/leightonvanrooijen/utopia/internal/domain"
)

// writeEnvFile creates a .utopia directory containing the given .env content
// and returns the .utopia path.
func writeEnvFile(t *testing.T, content string) string {
	t.Helper()

	utopiaDir := filepath.Join(t.TempDir(), ".utopia")
	if err := os.MkdirAll(utopiaDir, 0o755); err != nil {
		t.Fatalf("failed to create utopia dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(utopiaDir, ".env"), []byte(content), 0o600); err != nil {
		t.Fatalf("failed to write .env: %v", err)
	}

	return utopiaDir
}

func TestLoadAnthropicEnvFile(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    map[string]string
	}{
		{
			name:    "reads an anthropic key",
			content: "ANTHROPIC_API_KEY=sk-file\n",
			want:    map[string]string{"ANTHROPIC_API_KEY": "sk-file"},
		},
		{
			name:    "ignores names outside the ANTHROPIC_ prefix",
			content: "ANTHROPIC_API_KEY=sk-file\nAWS_SECRET_ACCESS_KEY=nope\nPATH=/evil\n",
			want:    map[string]string{"ANTHROPIC_API_KEY": "sk-file"},
		},
		{
			name:    "skips blank lines and comments",
			content: "# the key that pays\n\nANTHROPIC_API_KEY=sk-file\n\n",
			want:    map[string]string{"ANTHROPIC_API_KEY": "sk-file"},
		},
		{
			name:    "tolerates an export prefix",
			content: "export ANTHROPIC_API_KEY=sk-file\n",
			want:    map[string]string{"ANTHROPIC_API_KEY": "sk-file"},
		},
		{
			name:    "strips double quotes",
			content: "ANTHROPIC_API_KEY=\"sk-file\"\n",
			want:    map[string]string{"ANTHROPIC_API_KEY": "sk-file"},
		},
		{
			name:    "strips single quotes",
			content: "ANTHROPIC_API_KEY='sk-file'\n",
			want:    map[string]string{"ANTHROPIC_API_KEY": "sk-file"},
		},
		{
			name:    "keeps other anthropic variables",
			content: "ANTHROPIC_API_KEY=sk-file\nANTHROPIC_AUTH_TOKEN=tok\n",
			want:    map[string]string{"ANTHROPIC_API_KEY": "sk-file", "ANTHROPIC_AUTH_TOKEN": "tok"},
		},
		{
			name:    "ignores a line with no assignment",
			content: "garbage\nANTHROPIC_API_KEY=sk-file\n",
			want:    map[string]string{"ANTHROPIC_API_KEY": "sk-file"},
		},
		{
			name:    "keeps an equals sign inside the value",
			content: "ANTHROPIC_API_KEY=sk-a=b\n",
			want:    map[string]string{"ANTHROPIC_API_KEY": "sk-a=b"},
		},
		{
			name:    "an empty file defines nothing",
			content: "",
			want:    map[string]string{},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			vars, err := LoadAnthropicEnvFile(writeEnvFile(t, tc.content))
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if len(vars) != len(tc.want) {
				t.Fatalf("loaded %v, want %v", vars, tc.want)
			}
			for name, want := range tc.want {
				if vars[name] != want {
					t.Errorf("%s = %q, want %q", name, vars[name], want)
				}
			}
		})
	}
}

func TestLoadAnthropicEnvFileMissingFileIsNotAnError(t *testing.T) {
	vars, err := LoadAnthropicEnvFile(t.TempDir())

	if err != nil {
		t.Fatalf("a missing .env should not error, got: %v", err)
	}
	if len(vars) != 0 {
		t.Errorf("expected no variables, got %v", vars)
	}
}

func TestResolveAPIKeyEnv(t *testing.T) {
	tests := []struct {
		name       string
		content    string
		ambient    []string
		wantEnv    []string
		wantSource domain.CredentialSource
	}{
		{
			name:       "key from the env file, auth token dropped",
			content:    "ANTHROPIC_API_KEY=sk-file\n",
			ambient:    []string{"PATH=/usr/bin", "ANTHROPIC_AUTH_TOKEN=tok"},
			wantEnv:    []string{"PATH=/usr/bin", "ANTHROPIC_API_KEY=sk-file"},
			wantSource: domain.CredentialSourceEnvFile,
		},
		{
			name:       "env file overrides an ambient key",
			content:    "ANTHROPIC_API_KEY=sk-file\n",
			ambient:    []string{"ANTHROPIC_API_KEY=sk-ambient", "PATH=/usr/bin"},
			wantEnv:    []string{"PATH=/usr/bin", "ANTHROPIC_API_KEY=sk-file"},
			wantSource: domain.CredentialSourceEnvFile,
		},
		{
			name:       "falls back to the ambient key",
			content:    "# no key here\n",
			ambient:    []string{"PATH=/usr/bin", "ANTHROPIC_API_KEY=sk-ambient"},
			wantEnv:    []string{"PATH=/usr/bin", "ANTHROPIC_API_KEY=sk-ambient"},
			wantSource: domain.CredentialSourceEnvironment,
		},
		{
			name:       "other anthropic variables in the file reach the subprocess",
			content:    "ANTHROPIC_API_KEY=sk-file\nANTHROPIC_BASE_URL=https://gateway\n",
			ambient:    []string{"PATH=/usr/bin"},
			wantEnv:    []string{"PATH=/usr/bin", "ANTHROPIC_BASE_URL=https://gateway", "ANTHROPIC_API_KEY=sk-file"},
			wantSource: domain.CredentialSourceEnvFile,
		},
		{
			name:       "the file overrides an ambient anthropic variable",
			content:    "ANTHROPIC_API_KEY=sk-file\nANTHROPIC_BASE_URL=https://file\n",
			ambient:    []string{"ANTHROPIC_BASE_URL=https://ambient", "PATH=/usr/bin"},
			wantEnv:    []string{"ANTHROPIC_BASE_URL=https://file", "PATH=/usr/bin", "ANTHROPIC_API_KEY=sk-file"},
			wantSource: domain.CredentialSourceEnvFile,
		},
		{
			// An auth token in the file must not ride along with the resolved key:
			// sending both credentials is a 401.
			name:       "an auth token in the file is not restored alongside the key",
			content:    "ANTHROPIC_API_KEY=sk-file\nANTHROPIC_AUTH_TOKEN=tok-file\n",
			ambient:    []string{"PATH=/usr/bin"},
			wantEnv:    []string{"PATH=/usr/bin", "ANTHROPIC_API_KEY=sk-file"},
			wantSource: domain.CredentialSourceEnvFile,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			env, source, err := ResolveAPIKeyEnv(writeEnvFile(t, tc.content), tc.ambient)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if source != tc.wantSource {
				t.Errorf("source = %q, want %q", source, tc.wantSource)
			}
			if len(env) != len(tc.wantEnv) {
				t.Fatalf("env = %v, want %v", env, tc.wantEnv)
			}
			for i := range tc.wantEnv {
				if env[i] != tc.wantEnv[i] {
					t.Errorf("env[%d] = %q, want %q", i, env[i], tc.wantEnv[i])
				}
			}
		})
	}
}

// The ANTHROPIC_ filter is what stops .utopia/.env being general-purpose
// environment injection. A file that tries to redirect PATH, add a shell
// preload, or leak another provider's secret must contribute none of it - not
// even by overriding a name the subprocess already inherited.
func TestResolveAPIKeyEnvIgnoresNamesOutsideTheAnthropicPrefix(t *testing.T) {
	utopiaDir := writeEnvFile(t, strings.Join([]string{
		"ANTHROPIC_API_KEY=sk-file",
		"PATH=/evil",
		"LD_PRELOAD=/evil/lib.so",
		"AWS_SECRET_ACCESS_KEY=nope",
		"", // trailing newline
	}, "\n"))
	ambient := []string{"PATH=/usr/bin", "HOME=/home/u"}

	env, _, err := ResolveAPIKeyEnv(utopiaDir, ambient)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := []string{"PATH=/usr/bin", "HOME=/home/u", "ANTHROPIC_API_KEY=sk-file"}
	if len(env) != len(want) {
		t.Fatalf("env = %v, want %v", env, want)
	}
	for i := range want {
		if env[i] != want[i] {
			t.Errorf("env[%d] = %q, want %q", i, env[i], want[i])
		}
	}
}

func TestResolveAPIKeyEnvErrorsWhenNoKeyExists(t *testing.T) {
	// Neither location holds a key: the run must fail here, before any claude
	// process is started.
	env, source, err := ResolveAPIKeyEnv(t.TempDir(), []string{"PATH=/usr/bin"})

	if err == nil {
		t.Fatal("expected an error when no key can be resolved")
	}
	var missing *domain.MissingAPIKeyError
	if !errors.As(err, &missing) {
		t.Fatalf("error type = %T, want *domain.MissingAPIKeyError", err)
	}
	if env != nil {
		t.Errorf("expected no environment on failure, got %v", env)
	}
	if source != "" {
		t.Errorf("expected no source on failure, got %q", source)
	}
}

// Subscription mode is a pure function over the environment, so it lives in
// internal/domain. Its real-process behaviour is exercised here, alongside the
// api-key counterpart below, because only this package can write a .utopia/.env
// to disk and confirm the mode leaves it alone.
func TestSubscriptionEnvUsesRealProcessEnvironment(t *testing.T) {
	// The direnv case: both credentials are genuinely exported in this process.
	t.Setenv("ANTHROPIC_API_KEY", "sk-exported")
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "tok-exported")
	// A key on disk must not rescue the credential either. Nothing passes this
	// directory to SubscriptionEnv - the signature takes no path at all, which is
	// what makes the file unreachable from this mode.
	writeEnvFile(t, "ANTHROPIC_API_KEY=sk-file\n")

	ambient := os.Environ()
	env := domain.SubscriptionEnv(ambient)

	for _, entry := range env {
		name, _, _ := strings.Cut(entry, "=")
		if name == "ANTHROPIC_API_KEY" || name == "ANTHROPIC_AUTH_TOKEN" {
			t.Errorf("%s should be absent from the subprocess environment, found %q", name, entry)
		}
	}

	// Everything else the process inherited survives, in order.
	var want []string
	for _, entry := range ambient {
		name, _, _ := strings.Cut(entry, "=")
		if name != "ANTHROPIC_API_KEY" && name != "ANTHROPIC_AUTH_TOKEN" {
			want = append(want, entry)
		}
	}
	if len(env) != len(want) {
		t.Fatalf("passed through %d variables, want %d", len(env), len(want))
	}
	for i := range want {
		if env[i] != want[i] {
			t.Errorf("env[%d] = %q, want %q", i, env[i], want[i])
		}
	}
	// PATH is always inherited, so an empty want would mean the comparison above
	// proved nothing.
	if len(want) == 0 {
		t.Fatal("expected the real process environment to hold other variables")
	}
}

func TestResolveAPIKeyEnvUsesRealProcessEnvironment(t *testing.T) {
	// The direnv case: the key is genuinely exported in this process and
	// os.Environ() is what gets passed through.
	t.Setenv("ANTHROPIC_API_KEY", "sk-exported")
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "tok-exported")

	env, source, err := ResolveAPIKeyEnv(t.TempDir(), os.Environ())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if source != domain.CredentialSourceEnvironment {
		t.Errorf("source = %q, want %q", source, domain.CredentialSourceEnvironment)
	}

	var sawKey bool
	for _, entry := range env {
		if entry == "ANTHROPIC_API_KEY=sk-exported" {
			sawKey = true
		}
		if entry == "ANTHROPIC_AUTH_TOKEN=tok-exported" || entry == "ANTHROPIC_AUTH_TOKEN=" {
			t.Errorf("ANTHROPIC_AUTH_TOKEN should be absent, found %q", entry)
		}
	}
	if !sawKey {
		t.Error("resolved key missing from the subprocess environment")
	}
}

func TestResolveAuthSelection(t *testing.T) {
	const ambientKey = "sk-ant-ambient-DO-NOT-PRINT"
	const fileKey = "sk-ant-file-DO-NOT-PRINT"

	tests := []struct {
		name       string
		mode       domain.AuthMode
		envFile    string
		ambient    []string
		wantReport string
	}{
		{
			name:       "no mode selected reports nothing",
			mode:       "",
			ambient:    []string{domain.APIKeyEnvVar + "=" + ambientKey},
			wantReport: "",
		},
		{
			name:       "api-key resolved from the project env file",
			mode:       domain.AuthModeAPIKey,
			envFile:    domain.APIKeyEnvVar + "=" + fileKey + "\n",
			ambient:    []string{domain.APIKeyEnvVar + "=" + ambientKey},
			wantReport: "Auth: api-key (ANTHROPIC_API_KEY from .utopia/.env)",
		},
		{
			name:       "api-key resolved from the inherited environment",
			mode:       domain.AuthModeAPIKey,
			envFile:    "",
			ambient:    []string{domain.APIKeyEnvVar + "=" + ambientKey},
			wantReport: "Auth: api-key (ANTHROPIC_API_KEY from the environment)",
		},
		{
			name:       "subscription with nothing to suppress",
			mode:       domain.AuthModeSubscription,
			ambient:    []string{"PATH=/usr/bin"},
			wantReport: "Auth: subscription",
		},
		{
			// The case the report exists for: the shell exports a key, the run
			// bills the subscription anyway, and the user is told so.
			name:       "subscription over an ambient api key",
			mode:       domain.AuthModeSubscription,
			envFile:    domain.APIKeyEnvVar + "=" + fileKey + "\n",
			ambient:    []string{domain.APIKeyEnvVar + "=" + ambientKey},
			wantReport: "Auth: subscription (ambient ANTHROPIC_API_KEY ignored)",
		},
		{
			name: "subscription over both ambient credentials",
			mode: domain.AuthModeSubscription,
			ambient: []string{
				domain.APIKeyEnvVar + "=" + ambientKey,
				domain.AuthTokenEnvVar + "=token-ambient",
			},
			wantReport: "Auth: subscription (ambient ANTHROPIC_API_KEY, ANTHROPIC_AUTH_TOKEN ignored)",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			utopiaDir := writeEnvFile(t, tc.envFile)

			selection, err := ResolveAuthSelection(tc.mode, utopiaDir, tc.ambient)
			if err != nil {
				t.Fatalf("ResolveAuthSelection() error = %v", err)
			}

			report := selection.Report()
			if report != tc.wantReport {
				t.Errorf("Report() = %q, want %q", report, tc.wantReport)
			}
			for _, key := range []string{ambientKey, fileKey} {
				if strings.Contains(report, key) {
					t.Errorf("Report() = %q leaks a credential", report)
				}
			}
		})
	}
}

// The selection carries no credential at all, so nothing downstream of it can
// print one even by accident.
func TestResolveAuthSelectionHoldsNoCredential(t *testing.T) {
	const fileKey = "sk-ant-file-DO-NOT-PRINT"

	utopiaDir := writeEnvFile(t, domain.APIKeyEnvVar+"="+fileKey+"\n")

	selection, err := ResolveAuthSelection(domain.AuthModeAPIKey, utopiaDir, nil)
	if err != nil {
		t.Fatalf("ResolveAuthSelection() error = %v", err)
	}

	if rendered := fmt.Sprintf("%+v", selection); strings.Contains(rendered, fileKey) {
		t.Errorf("selection %s holds the credential", rendered)
	}
}

// Reporting resolves the credential too, so api-key mode with no key anywhere
// fails while reporting - before the command does any work - rather than at the
// first spawn.
func TestResolveAuthSelectionAPIKeyMissing(t *testing.T) {
	_, err := ResolveAuthSelection(domain.AuthModeAPIKey, writeEnvFile(t, ""), nil)
	if err == nil {
		t.Fatal("ResolveAuthSelection should fail when api-key mode can resolve no key")
	}

	if !errors.Is(err, &domain.MissingAPIKeyError{}) {
		t.Errorf("error %v is not a *MissingAPIKeyError", err)
	}
}

func TestResolveAuthSelectionUnknownMode(t *testing.T) {
	_, err := ResolveAuthSelection(domain.AuthMode("teamplan"), "", nil)
	if err == nil {
		t.Fatal("ResolveAuthSelection should fail for an unrecognised auth mode")
	}

	if !errors.Is(err, &domain.InvalidAuthModeError{}) {
		t.Errorf("error %v is not an *InvalidAuthModeError", err)
	}
}

// The report must describe the environment the subprocess actually gets. One
// resolution returns both halves so they cannot disagree; this pins the pairing
// for each mode.
func TestResolveAuthMatchesTheEnvironmentItReports(t *testing.T) {
	const fileKey = "sk-ant-file"
	ambient := []string{"PATH=/usr/bin", domain.APIKeyEnvVar + "=sk-ambient"}
	utopiaDir := writeEnvFile(t, domain.APIKeyEnvVar+"="+fileKey+"\n")

	env, selection, err := ResolveAuth(domain.AuthModeAPIKey, utopiaDir, ambient)
	if err != nil {
		t.Fatalf("ResolveAuth(api-key) error = %v", err)
	}
	if selection.Source != domain.CredentialSourceEnvFile {
		t.Errorf("reported source = %q, want %q", selection.Source, domain.CredentialSourceEnvFile)
	}
	if got := lookupTestEnv(env, domain.APIKeyEnvVar); got != fileKey {
		t.Errorf("environment carries %q, but the report named %q", got, selection.Source)
	}

	env, selection, err = ResolveAuth(domain.AuthModeSubscription, utopiaDir, ambient)
	if err != nil {
		t.Fatalf("ResolveAuth(subscription) error = %v", err)
	}
	if len(selection.Suppressed) != 1 || selection.Suppressed[0] != domain.APIKeyEnvVar {
		t.Errorf("reported suppressed = %v, want [%s]", selection.Suppressed, domain.APIKeyEnvVar)
	}
	if envVarNames(env)[domain.APIKeyEnvVar] {
		t.Errorf("%s reported as ignored but still present in the environment", domain.APIKeyEnvVar)
	}
}
