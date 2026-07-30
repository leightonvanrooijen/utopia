package domain

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// The three states have to survive the round trip to YAML, because the record on
// disk is what a report reads. An attempt whose accounting was never captured, one
// whose accounting could not be read, and one that genuinely spent nothing are
// three different facts, and only the third may be summed as zero.
func TestAttemptUsage_UnavailableIsDistinguishableFromZero(t *testing.T) {
	unavailable, err := yaml.Marshal(&ExecutorAttempt{
		Iteration: 1,
		Role:      ExecutorRoleDefault,
		Model:     "opus",
		Usage:     UnavailableUsage("the claude CLI produced no parseable terminal result object"),
	})
	if err != nil {
		t.Fatalf("yaml.Marshal() error = %v", err)
	}

	zero, err := yaml.Marshal(&ExecutorAttempt{
		Iteration: 2,
		Role:      ExecutorRoleDefault,
		Model:     "opus",
		Usage:     &AttemptUsage{Available: true, Model: "claude-opus-5-20260101", CostBasis: CostBasisCharged},
	})
	if err != nil {
		t.Fatalf("yaml.Marshal() error = %v", err)
	}

	never, err := yaml.Marshal(&ExecutorAttempt{Iteration: 3, Role: ExecutorRoleDefault, Model: "opus"})
	if err != nil {
		t.Fatalf("yaml.Marshal() error = %v", err)
	}

	if !strings.Contains(string(unavailable), "available: false") {
		t.Errorf("unavailable usage =\n%s\nwant an explicit available: false", unavailable)
	}
	if !strings.Contains(string(unavailable), "unavailable_reason:") {
		t.Errorf("unavailable usage =\n%s\nwant the reason recorded", unavailable)
	}
	if !strings.Contains(string(zero), "available: true") {
		t.Errorf("zero usage =\n%s\nwant an explicit available: true", zero)
	}
	if strings.Contains(string(zero), "input_tokens") {
		t.Errorf("zero usage =\n%s\nwant zero counts omitted, since the flag carries the meaning", zero)
	}
	if strings.Contains(string(never), "usage:") {
		t.Errorf("uncaptured attempt =\n%s\nwant no usage key at all", never)
	}

	// Reading them back keeps them apart, which is what a report has to rely on.
	var back [3]ExecutorAttempt
	for i, data := range [][]byte{unavailable, zero, never} {
		if err := yaml.Unmarshal(data, &back[i]); err != nil {
			t.Fatalf("yaml.Unmarshal() error = %v", err)
		}
	}

	if back[0].Usage == nil || back[0].Usage.IsAvailable() {
		t.Errorf("unavailable round-tripped to %+v, want a record that is not available", back[0].Usage)
	}
	if !back[1].Usage.IsAvailable() {
		t.Errorf("zero usage round-tripped to %+v, want available", back[1].Usage)
	}
	if back[1].Usage.InputTokens != 0 || back[1].Usage.CostUSD != 0 {
		t.Errorf("zero usage round-tripped to %+v, want zero counts", back[1].Usage)
	}
	if back[2].Usage != nil {
		t.Errorf("uncaptured attempt round-tripped to %+v, want nil", back[2].Usage)
	}
}

// Tokens are a fact under both auth modes; dollars are only money under api-key
// auth. The basis travels with the number so a reader cannot pool the two.
func TestCostBasisForAuth(t *testing.T) {
	tests := []struct {
		mode AuthMode
		want CostBasis
	}{
		{AuthModeAPIKey, CostBasisCharged},
		{AuthModeSubscription, CostBasisListPriceEstimate},
		{"", CostBasisUnknown},
	}

	for _, tt := range tests {
		if got := CostBasisForAuth(tt.mode); got != tt.want {
			t.Errorf("CostBasisForAuth(%q) = %q, want %q", tt.mode, got, tt.want)
		}
	}

	subscription := &AttemptUsage{Available: true, CostUSD: 12, CostBasis: CostBasisListPriceEstimate}
	if subscription.CostIsCharged() {
		t.Error("a subscription cost reports as charged, want it marked a list-price estimate")
	}

	var missing *AttemptUsage
	if missing.IsAvailable() || missing.CostIsCharged() {
		t.Error("a nil usage reports as available, want the nil case to read as not captured")
	}
}
