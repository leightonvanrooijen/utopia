package domain

import (
	"errors"
	"strings"
	"testing"
)

func TestValidateEffort(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantError bool
	}{
		{name: "low accepted", input: "low"},
		{name: "medium accepted", input: "medium"},
		{name: "high accepted", input: "high"},
		{name: "xhigh accepted", input: "xhigh"},
		{name: "max accepted", input: "max"},
		{name: "unrecognised level returns error", input: "extreme", wantError: true},
		{name: "model alias is not an effort level", input: "opus", wantError: true},
		{name: "numeric level returns error", input: "3", wantError: true},
		{name: "empty string returns error", input: "", wantError: true},
		{name: "case sensitive - HIGH fails", input: "HIGH", wantError: true},
		{name: "extra-high spelling fails", input: "extra-high", wantError: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateEffort(tc.input)
			if tc.wantError {
				if err == nil {
					t.Fatalf("ValidateEffort(%q) = nil, want an error", tc.input)
				}
				if !errors.Is(err, &InvalidEffortError{}) {
					t.Errorf("error %v is not an *InvalidEffortError", err)
				}
				// The message is the only feedback before any claude process starts,
				// so it must name the rejected value and the levels that would work.
				if !strings.Contains(err.Error(), "low, medium, high, xhigh, max") {
					t.Errorf("error %q does not list the valid levels", err)
				}
				return
			}
			if err != nil {
				t.Errorf("ValidateEffort(%q) = %v, want nil", tc.input, err)
			}
		})
	}
}

func TestValidEffortLevels(t *testing.T) {
	want := []EffortLevel{EffortLow, EffortMedium, EffortHigh, EffortXHigh, EffortMax}
	got := ValidEffortLevels()

	if len(got) != len(want) {
		t.Fatalf("ValidEffortLevels() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("ValidEffortLevels()[%d] = %q, want %q", i, got[i], want[i])
		}
	}

	// The returned slice is a copy: a caller that mutates it must not change the
	// vocabulary every later validation is checked against.
	got[0] = "tampered"
	if ValidEffortLevels()[0] != EffortLow {
		t.Error("ValidEffortLevels() exposes the package-level slice")
	}
}

// Each role's built-in default is asserted explicitly: they are the levels the
// loop runs at when a project configures nothing, so a change to one has to be
// an intentional edit here.
func TestEffortConfig_RoleDefaults(t *testing.T) {
	var ec *EffortConfig // the omitted-section case

	if got := ec.ExecutorEffort(); got != string(EffortMedium) {
		t.Errorf("ExecutorEffort() = %q, want %q", got, EffortMedium)
	}
	if got := ec.EscalatedExecutorEffort(); got != string(EffortHigh) {
		t.Errorf("EscalatedExecutorEffort() = %q, want %q", got, EffortHigh)
	}
	if got := ec.ValidatorEffort(); got != string(EffortMedium) {
		t.Errorf("ValidatorEffort() = %q, want %q", got, EffortMedium)
	}
	if got := ec.ScoperEffort(); got != string(EffortHigh) {
		t.Errorf("ScoperEffort() = %q, want %q", got, EffortHigh)
	}
}

func TestEffortConfig_EffortForCommand(t *testing.T) {
	tests := []struct {
		name    string
		config  *EffortConfig
		command string
		want    string
	}{
		{name: "nil config leaves the CLI default", config: nil, command: "execute", want: ""},
		{name: "empty config leaves the CLI default", config: &EffortConfig{}, command: "execute", want: ""},
		{name: "default applies when the role is unset", config: &EffortConfig{Default: "low"}, command: "execute", want: "low"},
		{name: "role override wins over default", config: &EffortConfig{Default: "low", Execute: "high"}, command: "execute", want: "high"},
		{name: "cr", config: &EffortConfig{CR: "max"}, command: "cr", want: "max"},
		{name: "harvest", config: &EffortConfig{Harvest: "low"}, command: "harvest", want: "low"},
		{name: "execute_escalated", config: &EffortConfig{ExecuteEscalated: "xhigh"}, command: "execute_escalated", want: "xhigh"},
		{name: "scoper", config: &EffortConfig{Scoper: "max"}, command: "scoper", want: "max"},
		{name: "validators", config: &EffortConfig{Validators: "high"}, command: "validators", want: "high"},
		{name: "validator_router", config: &EffortConfig{ValidatorRouter: "low"}, command: "validator_router", want: "low"},
		{name: "discover", config: &EffortConfig{Discover: "medium"}, command: "discover", want: "medium"},
		{name: "standards", config: &EffortConfig{Standards: "medium"}, command: "standards", want: "medium"},
		{name: "refactor", config: &EffortConfig{Refactor: "high"}, command: "refactor", want: "high"},
		{name: "shape", config: &EffortConfig{Shape: "high"}, command: "shape", want: "high"},
		{name: "validator_create", config: &EffortConfig{ValidatorCreate: "medium"}, command: "validator_create", want: "medium"},
		{name: "validator_edit", config: &EffortConfig{ValidatorEdit: "medium"}, command: "validator_edit", want: "medium"},
		{name: "unknown command falls back to default", config: &EffortConfig{Default: "high"}, command: "merge", want: "high"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.config.EffortForCommand(tc.command); got != tc.want {
				t.Errorf("EffortForCommand(%q) = %q, want %q", tc.command, got, tc.want)
			}
		})
	}
}

// effort.default stands in for a missing role key, and the role's own built-in
// default is only reached when both are missing.
func TestEffortConfig_RoleResolutionPrecedence(t *testing.T) {
	ec := &EffortConfig{Default: "low"}

	if got := ec.ExecutorEffort(); got != "low" {
		t.Errorf("ExecutorEffort() = %q, want the configured default %q", got, "low")
	}
	if got := ec.EscalatedExecutorEffort(); got != "low" {
		t.Errorf("EscalatedExecutorEffort() = %q, want the configured default %q", got, "low")
	}
	if got := ec.ValidatorEffort(); got != "low" {
		t.Errorf("ValidatorEffort() = %q, want the configured default %q", got, "low")
	}
	if got := ec.ScoperEffort(); got != "low" {
		t.Errorf("ScoperEffort() = %q, want the configured default %q", got, "low")
	}

	ec = &EffortConfig{Default: "low", Execute: "medium", ExecuteEscalated: "max", Validators: "high", Scoper: "xhigh"}

	if got := ec.ExecutorEffort(); got != "medium" {
		t.Errorf("ExecutorEffort() = %q, want %q", got, "medium")
	}
	if got := ec.EscalatedExecutorEffort(); got != "max" {
		t.Errorf("EscalatedExecutorEffort() = %q, want %q", got, "max")
	}
	if got := ec.ValidatorEffort(); got != "high" {
		t.Errorf("ValidatorEffort() = %q, want %q", got, "high")
	}
	if got := ec.ScoperEffort(); got != "xhigh" {
		t.Errorf("ScoperEffort() = %q, want %q", got, "xhigh")
	}
}

func TestValidateEffortConfig(t *testing.T) {
	tests := []struct {
		name       string
		config     *EffortConfig
		wantErr    bool
		wantFields []string
	}{
		{name: "nil config is valid", config: nil},
		{name: "empty config is valid", config: &EffortConfig{}},
		{
			name:   "every recognised level is valid",
			config: &EffortConfig{Default: "medium", Execute: "low", ExecuteEscalated: "high", Scoper: "xhigh", Validators: "max"},
		},
		{
			name:       "invalid default is rejected",
			config:     &EffortConfig{Default: "extreme"},
			wantErr:    true,
			wantFields: []string{"effort.default", "extreme"},
		},
		{
			name:       "invalid role level is rejected",
			config:     &EffortConfig{Execute: "opus"},
			wantErr:    true,
			wantFields: []string{"effort.execute", "opus"},
		},
		{
			name:       "every offending key is named",
			config:     &EffortConfig{ExecuteEscalated: "highest", Scoper: "9", Validators: "MEDIUM"},
			wantErr:    true,
			wantFields: []string{"effort.execute_escalated", "effort.scoper", "effort.validators"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateEffortConfig(tc.config)
			if !tc.wantErr {
				if err != nil {
					t.Fatalf("ValidateEffortConfig() = %v, want nil", err)
				}
				return
			}
			if err == nil {
				t.Fatal("ValidateEffortConfig() = nil, want an error")
			}
			if !errors.Is(err, &InvalidEffortConfigError{}) {
				t.Errorf("error %v is not an *InvalidEffortConfigError", err)
			}
			for _, field := range tc.wantFields {
				if !strings.Contains(err.Error(), field) {
					t.Errorf("error %q does not name %q", err, field)
				}
			}
			if !strings.Contains(err.Error(), "low, medium, high, xhigh, max") {
				t.Errorf("error %q does not list the valid levels", err)
			}
		})
	}
}
