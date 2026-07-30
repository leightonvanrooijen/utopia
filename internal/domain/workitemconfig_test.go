package domain

import (
	"errors"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestValidateWorkItemsConfig(t *testing.T) {
	tests := []struct {
		name    string
		config  *WorkItemsConfig
		wantErr bool
	}{
		{"omitted section", nil, false},
		{"empty section takes the default", &WorkItemsConfig{}, false},
		{"configured budget", &WorkItemsConfig{TurnBudget: capPtr(80)}, false},
		{"one is the smallest budget that buys anything", &WorkItemsConfig{TurnBudget: capPtr(1)}, false},
		{"zero", &WorkItemsConfig{TurnBudget: capPtr(0)}, true},
		{"negative", &WorkItemsConfig{TurnBudget: capPtr(-5)}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateWorkItemsConfig(tt.config)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateWorkItemsConfig = %v, wantErr %v", err, tt.wantErr)
			}
			if err != nil && !errors.Is(err, &InvalidTurnBudgetError{}) {
				t.Errorf("error does not match InvalidTurnBudgetError: %v", err)
			}
		})
	}
}

// The error names the field, because "invalid config" sends the operator hunting
// through every section.
func TestValidateWorkItemsConfig_NamesTheField(t *testing.T) {
	err := ValidateWorkItemsConfig(&WorkItemsConfig{TurnBudget: capPtr(0)})
	if err == nil {
		t.Fatal("expected an error, got nil")
	}
	for _, want := range []string{"work_items.turn_budget", "0", "at least 1"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q missing %q", err.Error(), want)
		}
	}
}

// An omitted key and an explicit value are distinguishable, which is the whole
// reason the budget is a pointer.
func TestTurnBudgetOr(t *testing.T) {
	var nilConfig *WorkItemsConfig
	if got := nilConfig.TurnBudgetOr(); got != DefaultTurnBudget {
		t.Errorf("nil config TurnBudgetOr() = %d, want the default %d", got, DefaultTurnBudget)
	}
	if got := (&WorkItemsConfig{}).TurnBudgetOr(); got != DefaultTurnBudget {
		t.Errorf("omitted key TurnBudgetOr() = %d, want the default %d", got, DefaultTurnBudget)
	}
	if got := (&WorkItemsConfig{TurnBudget: capPtr(12)}).TurnBudgetOr(); got != 12 {
		t.Errorf("TurnBudgetOr() = %d, want the configured 12", got)
	}
}

func TestDefaultTurnBudgetIs40(t *testing.T) {
	if DefaultTurnBudget != 40 {
		t.Errorf("DefaultTurnBudget = %d, want 40", DefaultTurnBudget)
	}
}

// A config file predating the section must keep loading, and one that has it
// must round-trip.
func TestConfigYAML_WorkItemsSection(t *testing.T) {
	var without Config
	if err := yaml.Unmarshal([]byte("verification:\n  command: ./verify.sh\n"), &without); err != nil {
		t.Fatalf("config with no work_items section failed to parse: %v", err)
	}
	if without.WorkItems != nil {
		t.Errorf("WorkItems = %+v, want nil for an omitted section", without.WorkItems)
	}
	if got := without.WorkItems.TurnBudgetOr(); got != DefaultTurnBudget {
		t.Errorf("TurnBudgetOr() = %d, want the default %d", got, DefaultTurnBudget)
	}

	var with Config
	if err := yaml.Unmarshal([]byte("work_items:\n  turn_budget: 25\n"), &with); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got := with.WorkItems.TurnBudgetOr(); got != 25 {
		t.Errorf("TurnBudgetOr() = %d, want 25", got)
	}
}
