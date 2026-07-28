package cli

import (
	"errors"
	"strings"
	"testing"

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
