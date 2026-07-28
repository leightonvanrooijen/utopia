package domain

import (
	"errors"
	"strings"
	"testing"
)

func TestConnectorConfig_GetMode(t *testing.T) {
	tests := []struct {
		name     string
		mode     string
		expected string
	}{
		{name: "empty mode defaults to notify", mode: "", expected: "notify"},
		{name: "explicit notify", mode: "notify", expected: "notify"},
		{name: "explicit gating", mode: "gating", expected: "gating"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cc := ConnectorConfig{Mode: tc.mode}
			if got := cc.GetMode(); got != tc.expected {
				t.Errorf("GetMode() = %q, want %q", got, tc.expected)
			}
		})
	}
}

func TestValidateConnectors(t *testing.T) {
	tests := []struct {
		name       string
		connectors []ConnectorConfig
		wantError  bool
		wantIssue  string
	}{
		{
			name:       "nil connectors is valid",
			connectors: nil,
			wantError:  false,
		},
		{
			name: "valid notify connector",
			connectors: []ConnectorConfig{
				{Name: "slack", On: []string{"execution-completed", "execution-failed"}, Command: "./notify.sh"},
			},
			wantError: false,
		},
		{
			name: "valid gating connector on gating-capable events",
			connectors: []ConnectorConfig{
				{Name: "lint", On: []string{"execution-started", "workitem-started", "workitem-verified", "phase-verified"}, Command: "npm run lint", Mode: "gating"},
			},
			wantError: false,
		},
		{
			name: "valid connector with timeout",
			connectors: []ConnectorConfig{
				{Name: "slow", On: []string{"phase-completed"}, Command: "./slow.sh", Timeout: "30s"},
			},
			wantError: false,
		},
		{
			name: "valid notify connector on speculative-execution events",
			connectors: []ConnectorConfig{
				{Name: "spec", On: []string{"workitem-completion-claimed", "workitem-verification-failed"}, Command: "./spec.sh"},
			},
			wantError: false,
		},
		{
			name: "gating on workitem-completion-claimed rejected",
			connectors: []ConnectorConfig{
				{Name: "bad", On: []string{"workitem-completion-claimed"}, Command: "./x.sh", Mode: "gating"},
			},
			wantError: true,
			wantIssue: `bad: mode gating is not supported on event "workitem-completion-claimed"`,
		},
		{
			name: "gating on workitem-verification-failed rejected",
			connectors: []ConnectorConfig{
				{Name: "bad", On: []string{"workitem-verification-failed"}, Command: "./x.sh", Mode: "gating"},
			},
			wantError: true,
			wantIssue: `bad: mode gating is not supported on event "workitem-verification-failed"`,
		},
		{
			name: "unknown event name rejected",
			connectors: []ConnectorConfig{
				{Name: "bad", On: []string{"workitem-finished"}, Command: "./x.sh"},
			},
			wantError: true,
			wantIssue: `bad: unknown event "workitem-finished" in on`,
		},
		{
			name: "invalid mode rejected",
			connectors: []ConnectorConfig{
				{Name: "bad", On: []string{"workitem-verified"}, Command: "./x.sh", Mode: "blocking"},
			},
			wantError: true,
			wantIssue: `bad: invalid mode "blocking"`,
		},
		{
			name: "gating on workitem-committed rejected",
			connectors: []ConnectorConfig{
				{Name: "bad", On: []string{"workitem-committed"}, Command: "./x.sh", Mode: "gating"},
			},
			wantError: true,
			wantIssue: `bad: mode gating is not supported on event "workitem-committed"`,
		},
		{
			name: "gating on phase-completed rejected",
			connectors: []ConnectorConfig{
				{Name: "bad", On: []string{"phase-completed"}, Command: "./x.sh", Mode: "gating"},
			},
			wantError: true,
			wantIssue: `bad: mode gating is not supported on event "phase-completed"`,
		},
		{
			name: "gating on execution-completed rejected",
			connectors: []ConnectorConfig{
				{Name: "bad", On: []string{"execution-completed"}, Command: "./x.sh", Mode: "gating"},
			},
			wantError: true,
			wantIssue: `bad: mode gating is not supported on event "execution-completed"`,
		},
		{
			name: "gating on execution-failed rejected",
			connectors: []ConnectorConfig{
				{Name: "bad", On: []string{"execution-failed"}, Command: "./x.sh", Mode: "gating"},
			},
			wantError: true,
			wantIssue: `bad: mode gating is not supported on event "execution-failed"`,
		},
		{
			name: "missing name rejected",
			connectors: []ConnectorConfig{
				{On: []string{"workitem-verified"}, Command: "./x.sh"},
			},
			wantError: true,
			wantIssue: "connectors[0]: name is required",
		},
		{
			name: "missing command rejected",
			connectors: []ConnectorConfig{
				{Name: "bad", On: []string{"workitem-verified"}},
			},
			wantError: true,
			wantIssue: "bad: command is required",
		},
		{
			name: "empty on rejected",
			connectors: []ConnectorConfig{
				{Name: "bad", Command: "./x.sh"},
			},
			wantError: true,
			wantIssue: "bad: on must list at least one lifecycle event",
		},
		{
			name: "invalid timeout rejected",
			connectors: []ConnectorConfig{
				{Name: "bad", On: []string{"workitem-verified"}, Command: "./x.sh", Timeout: "soon"},
			},
			wantError: true,
			wantIssue: `bad: invalid timeout "soon"`,
		},
		{
			name: "multiple issues all reported",
			connectors: []ConnectorConfig{
				{Name: "first", On: []string{"not-an-event"}, Command: "./x.sh"},
				{Name: "second", On: []string{"execution-failed"}, Command: "./y.sh", Mode: "gating"},
			},
			wantError: true,
			wantIssue: `first: unknown event "not-an-event" in on`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateConnectors(tc.connectors)

			if !tc.wantError {
				if err != nil {
					t.Errorf("ValidateConnectors() unexpected error: %v", err)
				}
				return
			}

			if err == nil {
				t.Fatalf("ValidateConnectors() expected error, got nil")
			}
			var invalidErr *InvalidConnectorConfigError
			if !errors.As(err, &invalidErr) {
				t.Fatalf("ValidateConnectors() error type = %T, want *InvalidConnectorConfigError", err)
			}
			if tc.wantIssue != "" && !strings.Contains(err.Error(), tc.wantIssue) {
				t.Errorf("ValidateConnectors() error = %q, want it to contain %q", err.Error(), tc.wantIssue)
			}
		})
	}
}

func TestValidateConnectors_MultipleIssuesReportsAll(t *testing.T) {
	err := ValidateConnectors([]ConnectorConfig{
		{Name: "first", On: []string{"not-an-event"}, Command: "./x.sh"},
		{Name: "second", On: []string{"execution-failed"}, Command: "./y.sh", Mode: "gating"},
	})

	var invalidErr *InvalidConnectorConfigError
	if !errors.As(err, &invalidErr) {
		t.Fatalf("expected *InvalidConnectorConfigError, got %T", err)
	}
	if len(invalidErr.Issues) != 2 {
		t.Errorf("expected 2 issues, got %d: %v", len(invalidErr.Issues), invalidErr.Issues)
	}
}

func TestLifecycleEventNames_MatchGatingEvents(t *testing.T) {
	known := map[string]bool{}
	for _, name := range LifecycleEventNames() {
		known[name] = true
	}
	for name := range gatingEvents {
		if !known[name] {
			t.Errorf("gating event %q is not a known lifecycle event", name)
		}
	}
}
