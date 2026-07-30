package cli

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"

	"github.com/leightonvanrooijen/utopia/internal/ui"
	"github.com/spf13/cobra"
)

// levelCmd is a stand-in for whichever command the root's PersistentPreRunE
// runs before: it carries the inherited --log-level flag and nothing else.
func levelCmd(t *testing.T, value string, set bool) *cobra.Command {
	t.Helper()
	cmd := &cobra.Command{Use: "fake"}
	cmd.Flags().String("log-level", "", logLevelFlagUsage)
	if set {
		if err := cmd.Flags().Set("log-level", value); err != nil {
			t.Fatalf("Set(log-level, %q) = %v", value, err)
		}
	}
	return cmd
}

func TestApplyLogLevel(t *testing.T) {
	tests := []struct {
		name string
		flag string
		env  string
		want slog.Level
	}{
		{name: "neither source defaults to info", want: slog.LevelInfo},
		{name: "flag debug", flag: "debug", want: slog.LevelDebug},
		{name: "flag warn", flag: "warn", want: slog.LevelWarn},
		{name: "flag error", flag: "error", want: slog.LevelError},
		{name: "environment variable", env: "warn", want: slog.LevelWarn},
		{name: "flag beats environment variable", flag: "debug", env: "error", want: slog.LevelDebug},
		{name: "environment variable applies when flag is empty", flag: "", env: "debug", want: slog.LevelDebug},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Cleanup(func() { ui.SetLevel(ui.DefaultLevel) })
			// A stale level from an earlier command must not leak into this one.
			ui.SetLevel(slog.LevelError)
			if tt.env != "" {
				t.Setenv(logLevelEnvVar, tt.env)
			}

			if err := applyLogLevel(levelCmd(t, tt.flag, tt.flag != ""), nil); err != nil {
				t.Fatalf("applyLogLevel() error = %v", err)
			}
			if got := ui.Level(); got != tt.want {
				t.Errorf("ui.Level() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestApplyLogLevelRejectsInvalidValue(t *testing.T) {
	tests := []struct {
		name       string
		flag       string
		env        string
		wantSource string
	}{
		{name: "invalid flag", flag: "trace", wantSource: "--log-level"},
		{name: "invalid environment variable", env: "loud", wantSource: logLevelEnvVar},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Cleanup(func() { ui.SetLevel(ui.DefaultLevel) })
			if tt.env != "" {
				t.Setenv(logLevelEnvVar, tt.env)
			}

			err := applyLogLevel(levelCmd(t, tt.flag, tt.flag != ""), nil)

			if err == nil {
				t.Fatal("applyLogLevel() = nil, want an error rather than a silent default")
			}
			if !strings.Contains(err.Error(), tt.wantSource) {
				t.Errorf("error %q does not name the source %q", err, tt.wantSource)
			}
			if !strings.Contains(err.Error(), ui.LevelNames) {
				t.Errorf("error %q does not list the accepted values", err)
			}
		})
	}
}

func TestApplyVerbose(t *testing.T) {
	tests := []struct {
		name    string
		start   slog.Level
		verbose bool
		want    slog.Level
	}{
		{name: "verbose raises info to debug", start: slog.LevelInfo, verbose: true, want: slog.LevelDebug},
		{name: "verbose raises error to debug", start: slog.LevelError, verbose: true, want: slog.LevelDebug},
		{name: "no flag leaves the level alone", start: slog.LevelWarn, want: slog.LevelWarn},
		{name: "verbose never lowers", start: slog.LevelDebug, verbose: true, want: slog.LevelDebug},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Cleanup(func() { ui.SetLevel(ui.DefaultLevel) })
			ui.SetLevel(tt.start)

			applyVerbose(tt.verbose)

			if got := ui.Level(); got != tt.want {
				t.Errorf("ui.Level() = %v, want %v", got, tt.want)
			}
		})
	}
}

// The flag has to be persistent and resolved before any handler runs, or a
// subcommand's first diagnostic escapes the level the invocation asked for.
func TestRootRegistersLogLevel(t *testing.T) {
	if flag := rootCmd.PersistentFlags().Lookup("log-level"); flag == nil {
		t.Fatal("rootCmd has no persistent --log-level flag")
	}
	if rootCmd.PersistentPreRunE == nil {
		t.Fatal("rootCmd has no PersistentPreRunE to resolve the level")
	}
}

// An invalid level fails the command instead of running it at the default.
func TestRootRejectsInvalidLogLevel(t *testing.T) {
	t.Cleanup(func() {
		ui.SetLevel(ui.DefaultLevel)
		rootCmd.SetArgs(nil)
		_ = rootCmd.PersistentFlags().Set("log-level", "")
	})

	var stdout, stderr bytes.Buffer
	rootCmd.SetOut(&stdout)
	rootCmd.SetErr(&stderr)
	rootCmd.SetArgs([]string{"status", "--log-level", "trace"})

	err := rootCmd.Execute()

	if err == nil {
		t.Fatal("rootCmd.Execute() = nil, want an error for an unknown level")
	}
	if !strings.Contains(err.Error(), ui.LevelNames) {
		t.Errorf("error %q does not list the accepted values", err)
	}
}
