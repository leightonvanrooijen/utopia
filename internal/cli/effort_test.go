package cli

import (
	"errors"
	"strings"
	"testing"

	"github.com/leightonvanrooijen/utopia/internal/domain"
	"github.com/spf13/cobra"
)

func TestResolveEffortFlag(t *testing.T) {
	tests := []struct {
		name    string
		set     bool
		value   string
		want    string
		wantErr bool
	}{
		{name: "flag omitted leaves the configured effort", set: false, want: ""},
		{name: "empty value leaves the configured effort", set: true, value: "", want: ""},
		{name: "low", set: true, value: "low", want: "low"},
		{name: "medium", set: true, value: "medium", want: "medium"},
		{name: "high", set: true, value: "high", want: "high"},
		{name: "xhigh", set: true, value: "xhigh", want: "xhigh"},
		{name: "max", set: true, value: "max", want: "max"},
		{name: "unrecognised level", set: true, value: "extreme", wantErr: true},
		{name: "model alias is not an effort level", set: true, value: "opus", wantErr: true},
		{name: "wrong case", set: true, value: "High", wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cmd := &cobra.Command{Use: "fake"}
			var effortFlag string
			cmd.Flags().StringVar(&effortFlag, "effort", "", "reasoning effort per turn")
			if tc.set {
				if err := cmd.Flags().Set("effort", tc.value); err != nil {
					t.Fatalf("Set(effort, %q) = %v", tc.value, err)
				}
			}

			got, err := ResolveEffortFlag(cmd)

			if tc.wantErr {
				if err == nil {
					t.Fatalf("ResolveEffortFlag() = (%q, nil), want an error", got)
				}
				if !errors.Is(err, &domain.InvalidEffortError{}) {
					t.Errorf("error %v is not an *InvalidEffortError", err)
				}
				if !strings.Contains(err.Error(), tc.value) {
					t.Errorf("error %q does not name the rejected value %q", err, tc.value)
				}
				if got != "" {
					t.Errorf("ResolveEffortFlag() = %q on error, want \"\"", got)
				}
				return
			}

			if err != nil {
				t.Fatalf("ResolveEffortFlag() error = %v", err)
			}
			if got != tc.want {
				t.Errorf("ResolveEffortFlag() = %q, want %q", got, tc.want)
			}
		})
	}
}

// A command without the flag registered reads as "no override", like --model.
func TestResolveEffortFlagUnregistered(t *testing.T) {
	effort, err := ResolveEffortFlag(&cobra.Command{Use: "fake"})
	if err != nil || effort != "" {
		t.Errorf("ResolveEffortFlag() = (%q, %v), want (\"\", nil)", effort, err)
	}
}

// --effort mirrors --model: wherever the execution loop takes a model override
// it takes an effort override too, and each level is named in the help.
func TestEffortFlagMirrorsModelFlagOnExecuteCommands(t *testing.T) {
	execute := NewExecuteCmd()
	for _, tc := range []struct {
		path string
		cmd  *cobra.Command
	}{
		{path: "execute", cmd: execute},
		{path: "execute run", cmd: findCommand(t, execute, []string{"run"})},
	} {
		if tc.cmd.Flags().Lookup("model") == nil {
			t.Errorf("%q has no --model flag", tc.path)
		}
		flag := tc.cmd.Flags().Lookup("effort")
		if flag == nil {
			t.Errorf("%q has no --effort flag", tc.path)
			continue
		}
		if flag.DefValue != "" {
			t.Errorf("%q --effort default = %q, want \"\"", tc.path, flag.DefValue)
		}
		for _, level := range domain.ValidEffortLevels() {
			if !strings.Contains(flag.Usage, string(level)) {
				t.Errorf("%q --effort usage %q does not mention %q", tc.path, flag.Usage, level)
			}
		}
	}
}

func TestResolveExecuteOverrides(t *testing.T) {
	newCmd := func() *cobra.Command {
		cmd := &cobra.Command{Use: "fake"}
		cmd.Flags().String("model", "", "model")
		cmd.Flags().String("effort", "", "effort")
		return cmd
	}

	cmd := newCmd()
	if err := cmd.Flags().Set("model", "opus"); err != nil {
		t.Fatalf("Set(model) = %v", err)
	}
	if err := cmd.Flags().Set("effort", "high"); err != nil {
		t.Fatalf("Set(effort) = %v", err)
	}

	over, err := resolveExecuteOverrides(cmd)
	if err != nil {
		t.Fatalf("resolveExecuteOverrides() error = %v", err)
	}
	if over.Model != "opus" || over.Effort != "high" {
		t.Errorf("resolveExecuteOverrides() = %+v, want {Model:opus Effort:high}", over)
	}

	// An invalid level fails before any work starts, and carries no partial
	// override with it.
	cmd = newCmd()
	if err := cmd.Flags().Set("effort", "extreme"); err != nil {
		t.Fatalf("Set(effort) = %v", err)
	}
	over, err = resolveExecuteOverrides(cmd)
	if err == nil {
		t.Fatalf("resolveExecuteOverrides() = (%+v, nil), want an error", over)
	}
	if over.Model != "" || over.Effort != "" {
		t.Errorf("resolveExecuteOverrides() = %+v on error, want the zero value", over)
	}
}
