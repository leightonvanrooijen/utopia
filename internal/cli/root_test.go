package cli

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/leightonvanrooijen/utopia/internal"
	"github.com/leightonvanrooijen/utopia/internal/domain"
	"github.com/spf13/cobra"
)

func TestResolveAuthFlag(t *testing.T) {
	tests := []struct {
		name     string
		set      bool
		value    string
		want     domain.AuthMode
		wantErr  bool
		errNames string
	}{
		{name: "flag omitted resolves to the empty mode", set: false, want: ""},
		{name: "empty value resolves to the empty mode", set: true, value: "", want: ""},
		{name: "api-key", set: true, value: "api-key", want: domain.AuthModeAPIKey},
		{name: "subscription", set: true, value: "subscription", want: domain.AuthModeSubscription},
		{name: "unknown mode", set: true, value: "oauth", wantErr: true, errNames: "oauth"},
		{name: "config key instead of mode", set: true, value: "mode", wantErr: true, errNames: "mode"},
		{name: "wrong case", set: true, value: "API-KEY", wantErr: true, errNames: "API-KEY"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := &cobra.Command{Use: "fake"}
			var authFlag string
			cmd.Flags().StringVar(&authFlag, "auth", "", "credential to use")
			if tt.set {
				if err := cmd.Flags().Set("auth", tt.value); err != nil {
					t.Fatalf("Set(auth, %q) = %v", tt.value, err)
				}
			}

			got, err := ResolveAuthFlag(cmd)

			if tt.wantErr {
				if err == nil {
					t.Fatalf("ResolveAuthFlag() = (%q, nil), want an error", got)
				}
				if !errors.Is(err, &domain.InvalidAuthModeError{}) {
					t.Errorf("error %v is not an *InvalidAuthModeError", err)
				}
				// The message must name the offending value and the valid options,
				// since it is the only feedback before any claude process starts.
				if !strings.Contains(err.Error(), tt.errNames) {
					t.Errorf("error %q does not name the rejected value %q", err, tt.errNames)
				}
				if !strings.Contains(err.Error(), "api-key, subscription") {
					t.Errorf("error %q does not list the valid options", err)
				}
				if got != "" {
					t.Errorf("ResolveAuthFlag() mode = %q on error, want \"\"", got)
				}
				return
			}

			if err != nil {
				t.Fatalf("ResolveAuthFlag() error = %v", err)
			}
			if got != tt.want {
				t.Errorf("ResolveAuthFlag() = %q, want %q", got, tt.want)
			}
		})
	}
}

// A command without the flag registered must not fail: the helper is shared by
// every handler, and an absent flag reads as "no override", not as an error.
func TestResolveAuthFlagUnregistered(t *testing.T) {
	mode, err := ResolveAuthFlag(&cobra.Command{Use: "fake"})
	if err != nil || mode != "" {
		t.Errorf("ResolveAuthFlag() = (%q, %v), want (\"\", nil)", mode, err)
	}
}

// claudeCommandPaths is every command that spawns a claude subprocess and so
// must accept --auth. execute is checked separately: it is wired through
// NewExecuteCmd rather than a package-level var.
var claudeCommandPaths = [][]string{
	{"cr"},
	{"harvest"},
	{"discover"},
	{"discover", "domain"},
	{"standards", "generate"},
	{"refactor"},
	{"shape"},
	{"shape", "domain"},
	{"validator", "create"},
	{"validator", "edit"},
}

func TestAuthFlagRegisteredOnClaudeInvokingCommands(t *testing.T) {
	for _, path := range claudeCommandPaths {
		assertAuthFlag(t, findCommand(t, rootCmd, path), strings.Join(path, " "))
	}

	execute := NewExecuteCmd()
	assertAuthFlag(t, execute, "execute")
	assertAuthFlag(t, findCommand(t, execute, []string{"run"}), "execute run")
}

func findCommand(t *testing.T, root *cobra.Command, path []string) *cobra.Command {
	t.Helper()
	cmd, _, err := root.Find(path)
	if err != nil {
		t.Fatalf("Find(%v) error = %v", path, err)
	}
	// Find falls back to the closest parent, so confirm we landed on the leaf.
	if want := path[len(path)-1]; cmd.Name() != want {
		t.Fatalf("Find(%v) resolved to %q, want %q", path, cmd.Name(), want)
	}
	return cmd
}

func assertAuthFlag(t *testing.T, cmd *cobra.Command, path string) {
	t.Helper()
	flag := cmd.Flags().Lookup("auth")
	if flag == nil {
		t.Errorf("%q has no --auth flag", path)
		return
	}
	// Optional, defaulting to whatever config.auth.mode selected.
	if flag.DefValue != "" {
		t.Errorf("%q --auth default = %q, want \"\"", path, flag.DefValue)
	}
	for _, mode := range domain.ValidAuthModes() {
		if !strings.Contains(flag.Usage, string(mode)) {
			t.Errorf("%q --auth usage %q does not mention %q", path, flag.Usage, mode)
		}
	}
}

// authTestCmd returns a command with the flags ResolveAuth reads, rooted at a
// project directory holding the given config.yaml and .utopia/.env contents.
// Either file is skipped when its content is empty.
func authTestCmd(t *testing.T, config, envFile string) (*cobra.Command, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()

	utopiaDir := filepath.Join(t.TempDir(), ".utopia")
	if err := os.MkdirAll(utopiaDir, 0o755); err != nil {
		t.Fatalf("failed to create utopia dir: %v", err)
	}
	for name, content := range map[string]string{"config.yaml": config, ".env": envFile} {
		if content == "" {
			continue
		}
		if err := os.WriteFile(filepath.Join(utopiaDir, name), []byte(content), 0o600); err != nil {
			t.Fatalf("failed to write %s: %v", name, err)
		}
	}

	cmd := &cobra.Command{Use: "fake"}
	var authFlag string
	cmd.Flags().StringVar(&authFlag, "auth", "", "credential to use")
	cmd.Flags().StringP("project", "p", filepath.Dir(utopiaDir), "project directory")

	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)

	return cmd, &stdout, &stderr
}

func TestResolveAuth(t *testing.T) {
	const configSubscription = "auth:\n  mode: subscription\n"
	const configAPIKey = "auth:\n  mode: api-key\n"

	tests := []struct {
		name       string
		config     string
		envFile    string
		flag       string
		ambientKey string
		wantMode   domain.AuthMode
		wantStderr string
	}{
		{
			// Backward compatibility: a project that never configured a credential
			// gets the output it always got.
			name:       "no flag and no auth section reports nothing",
			config:     "",
			wantMode:   "",
			wantStderr: "",
		},
		{
			name:       "an auth section with no flag is consumed, not just validated",
			config:     configSubscription,
			wantMode:   domain.AuthModeSubscription,
			wantStderr: "Auth: subscription\n",
		},
		{
			name:       "the flag overrides the configured mode in the report too",
			config:     configSubscription,
			envFile:    "ANTHROPIC_API_KEY=sk-ant-file\n",
			flag:       "api-key",
			wantMode:   domain.AuthModeAPIKey,
			wantStderr: "Auth: api-key (ANTHROPIC_API_KEY from .utopia/.env)\n",
		},
		{
			name:       "api-key from the project env file outranks the shell",
			config:     configAPIKey,
			envFile:    "ANTHROPIC_API_KEY=sk-ant-file\n",
			ambientKey: "sk-ant-ambient",
			wantMode:   domain.AuthModeAPIKey,
			wantStderr: "Auth: api-key (ANTHROPIC_API_KEY from .utopia/.env)\n",
		},
		{
			name:       "api-key falling back to the environment says so",
			config:     configAPIKey,
			ambientKey: "sk-ant-ambient",
			wantMode:   domain.AuthModeAPIKey,
			wantStderr: "Auth: api-key (ANTHROPIC_API_KEY from the environment)\n",
		},
		{
			name:       "subscription names the ambient key it ignored",
			config:     configSubscription,
			ambientKey: "sk-ant-ambient",
			wantMode:   domain.AuthModeSubscription,
			wantStderr: "Auth: subscription (ambient ANTHROPIC_API_KEY ignored)\n",
		},
		{
			name:       "the flag alone reports without any auth section",
			config:     "",
			flag:       "subscription",
			ambientKey: "sk-ant-ambient",
			wantMode:   domain.AuthModeSubscription,
			wantStderr: "Auth: subscription (ambient ANTHROPIC_API_KEY ignored)\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(domain.APIKeyEnvVar, tt.ambientKey)
			t.Setenv(domain.AuthTokenEnvVar, "")

			cmd, stdout, stderr := authTestCmd(t, tt.config, tt.envFile)
			if tt.flag != "" {
				if err := cmd.Flags().Set("auth", tt.flag); err != nil {
					t.Fatalf("Set(auth, %q) = %v", tt.flag, err)
				}
			}

			mode, err := ResolveAuth(cmd)
			if err != nil {
				t.Fatalf("ResolveAuth() error = %v", err)
			}

			if mode != tt.wantMode {
				t.Errorf("ResolveAuth() = %q, want %q", mode, tt.wantMode)
			}
			if got := stderr.String(); got != tt.wantStderr {
				t.Errorf("stderr = %q, want %q", got, tt.wantStderr)
			}
			// The report is a diagnostic: stdout stays pipeable.
			if stdout.Len() != 0 {
				t.Errorf("stdout = %q, want the report on stderr only", stdout.String())
			}
			// Whichever location won, the key itself never appears.
			combined := stdout.String() + stderr.String()
			for _, key := range []string{"sk-ant-file", "sk-ant-ambient"} {
				if strings.Contains(combined, key) {
					t.Errorf("output %q leaks the credential", combined)
				}
			}
		})
	}
}

// An invalid mode fails before anything is reported: a rejected value selected
// no credential, so there is none to name.
func TestResolveAuthInvalidFlag(t *testing.T) {
	cmd, stdout, stderr := authTestCmd(t, "auth:\n  mode: subscription\n", "")
	if err := cmd.Flags().Set("auth", "oauth"); err != nil {
		t.Fatalf("Set(auth, oauth) = %v", err)
	}

	mode, err := ResolveAuth(cmd)
	if err == nil {
		t.Fatalf("ResolveAuth() = (%q, nil), want an error", mode)
	}
	if !errors.Is(err, &domain.InvalidAuthModeError{}) {
		t.Errorf("error %v is not an *InvalidAuthModeError", err)
	}
	if stdout.Len() != 0 || stderr.Len() != 0 {
		t.Errorf("ResolveAuth() reported %q / %q for a rejected mode", stdout, stderr)
	}
}

// api-key mode with no key anywhere fails while reporting, before the command
// does any work.
func TestResolveAuthAPIKeyMissing(t *testing.T) {
	t.Setenv(domain.APIKeyEnvVar, "")

	cmd, _, _ := authTestCmd(t, "auth:\n  mode: api-key\n", "")

	if _, err := ResolveAuth(cmd); !errors.Is(err, &domain.MissingAPIKeyError{}) {
		t.Errorf("ResolveAuth() error = %v, want a *MissingAPIKeyError", err)
	}
}

// The credential is chosen once per invocation, and so is the line. ralph loops
// over work items and discover fans out over goroutines, each resolving the
// subprocess environment again from the same mode - resolution repeats, the
// report does not.
func TestResolveAuthReportsOncePerInvocation(t *testing.T) {
	t.Setenv(domain.APIKeyEnvVar, "sk-ant-ambient")
	t.Setenv(domain.AuthTokenEnvVar, "")

	cmd, stdout, stderr := authTestCmd(t, "auth:\n  mode: subscription\n", "")

	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		mode, err := ResolveAuth(cmd)
		if err != nil {
			return err
		}
		// Stand in for the spawn loop: every claude subprocess resolves the
		// environment for itself, and none of them reports.
		for i := 0; i < 5; i++ {
			if _, _, err := internal.ResolveAuth(mode, "", os.Environ()); err != nil {
				return err
			}
		}
		return nil
	}

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	if got := strings.Count(stderr.String(), "Auth:"); got != 1 {
		t.Errorf("reported %d times, want 1 (stderr = %q)", got, stderr.String())
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout = %q, want the report on stderr only", stdout.String())
	}
}
