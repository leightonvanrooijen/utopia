package validators

import (
	"context"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/leightonvanrooijen/utopia/internal/domain"
)

func TestResolveRouterModel(t *testing.T) {
	t.Run("defaults to haiku independent of validators model", func(t *testing.T) {
		r := NewRunner(t.TempDir()).WithModelConfig(&domain.ModelConfig{
			Default:    "opus",
			Validators: "sonnet",
		})
		if got := r.resolveRouterModel(); got != DefaultRouterModel {
			t.Errorf("expected router to default to %q independent of validators/default, got %q", DefaultRouterModel, got)
		}
	})

	t.Run("honors explicit validator_router override", func(t *testing.T) {
		r := NewRunner(t.TempDir()).WithModelConfig(&domain.ModelConfig{ValidatorRouter: "opus"})
		if got := r.resolveRouterModel(); got != "opus" {
			t.Errorf("expected explicit override 'opus', got %q", got)
		}
	})

	t.Run("nil model config falls back to default", func(t *testing.T) {
		r := NewRunner(t.TempDir())
		if got := r.resolveRouterModel(); got != DefaultRouterModel {
			t.Errorf("expected %q with nil config, got %q", DefaultRouterModel, got)
		}
	})
}

func TestSelectApplicable_BypassAndOnDemandWithoutModelCall(t *testing.T) {
	// When no validator is router-eligible, SelectApplicable must not invoke the
	// model at all - it returns the always-run and no-description validators and
	// drops on-demand ones. NewRunner points at the real "claude" binary, so this
	// also proves the model call is skipped (it would otherwise error/hang here).
	vs := []*domain.Validator{
		{ID: "security", Description: "checks for secrets", Always: true}, // always -> bypass
		{ID: "no-desc"}, // empty description -> bypass
		{ID: "on-demand-only", Description: "manual check", Run: domain.RunOnDemand}, // never selected
	}

	got, err := NewRunner(t.TempDir()).SelectApplicable(context.Background(), vs, "some diff")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := []string{"security", "no-desc"}
	sort.Strings(got)
	sort.Strings(want)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("expected bypass ids %v (on-demand excluded), got %v", want, got)
	}
}

func TestParseRouterSelection(t *testing.T) {
	eligible := []*domain.Validator{
		{ID: "no-console-logs", Description: "x"},
		{ID: "security-headers", Description: "y"},
		{ID: "security", Description: "z"},
	}

	tests := []struct {
		name   string
		output string
		want   []string
	}{
		{
			name:   "one id per line",
			output: "no-console-logs\nsecurity\n",
			want:   []string{"no-console-logs", "security"},
		},
		{
			name:   "ignores surrounding prose and punctuation",
			output: "The relevant validators are: `security-headers`, and no-console-logs.",
			want:   []string{"no-console-logs", "security-headers"},
		},
		{
			name:   "whole-token match avoids partial-id collisions",
			output: "security-headers",
			want:   []string{"security-headers"}, // must NOT also select "security"
		},
		{
			name:   "empty output selects nothing",
			output: "",
			want:   nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseRouterSelection(tt.output, eligible)
			sort.Strings(got)
			want := append([]string(nil), tt.want...)
			sort.Strings(want)
			if !reflect.DeepEqual(got, want) {
				t.Errorf("parseRouterSelection(%q) = %v, want %v", tt.output, got, want)
			}
		})
	}
}

func TestBuildRouterPrompt_ContainsRecallBiasAndInputs(t *testing.T) {
	eligible := []*domain.Validator{{ID: "no-console-logs", Description: "no console.log in prod code"}}
	prompt := buildRouterPrompt(eligible, "diff --git a/x b/x")

	for _, want := range []string{"no-console-logs", "no console.log in prod code", "diff --git a/x b/x", "INCLUDE"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("router prompt missing %q\n---\n%s", want, prompt)
		}
	}
}
