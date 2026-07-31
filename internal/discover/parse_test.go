package discover

import (
	"strings"
	"testing"

	"github.com/leightonvanrooijen/utopia/internal/domain"
)

const sampleQualificationOutput = "Here are the results:\n```yaml\n" + `qualified:
  - id: create-widget
    title: "Create a Widget"
    description: "Users can create widgets"
    source_files: ["widget.go", "widget_handler.go"]
    evidence_type: code
    qualification_reason: "User runs create and sees the widget"
  - id: list-widgets
    title: "List Widgets"
    description: "Users can list widgets"
    qualification_reason: "User runs list"
disqualified:
  - id: internal-cache
    reason: "Not user-observable"
` + "```\nDone."

func TestParseQualifiedCandidates(t *testing.T) {
	qualified := parseQualifiedCandidates(sampleQualificationOutput)
	if len(qualified) != 2 {
		t.Fatalf("expected 2 qualified candidates, got %d", len(qualified))
	}
	first := qualified[0]
	if first.ID != "create-widget" {
		t.Errorf("expected ID create-widget, got %q", first.ID)
	}
	if first.Title != "Create a Widget" {
		t.Errorf("expected title, got %q", first.Title)
	}
	if len(first.SourceFiles) != 2 {
		t.Errorf("expected 2 source files, got %d", len(first.SourceFiles))
	}
	if first.QualificationReason != "User runs create and sees the widget" {
		t.Errorf("unexpected qualification reason: %q", first.QualificationReason)
	}
}

func TestParseQualifiedCandidates_NoYAML(t *testing.T) {
	if got := parseQualifiedCandidates("no yaml here"); got != nil {
		t.Errorf("expected nil for output without YAML, got %v", got)
	}
}

func TestParseQualifiedCandidates_MalformedYAML(t *testing.T) {
	output := "```yaml\nqualified: [unclosed\n```"
	if got := parseQualifiedCandidates(output); got != nil {
		t.Errorf("expected nil for malformed YAML, got %v", got)
	}
}

func TestParseDisqualifiedCandidates(t *testing.T) {
	disqualified := parseDisqualifiedCandidates(sampleQualificationOutput)
	if len(disqualified) != 1 {
		t.Fatalf("expected 1 disqualified candidate, got %d", len(disqualified))
	}
	if disqualified[0].ID != "internal-cache" {
		t.Errorf("expected ID internal-cache, got %q", disqualified[0].ID)
	}
	if disqualified[0].Reason != "Not user-observable" {
		t.Errorf("unexpected reason: %q", disqualified[0].Reason)
	}
}

func TestCountYAMLItems(t *testing.T) {
	if got := countYAMLItems(sampleQualificationOutput, "qualified"); got != 2 {
		t.Errorf("expected 2 qualified, got %d", got)
	}
	if got := countYAMLItems(sampleQualificationOutput, "disqualified"); got != 1 {
		t.Errorf("expected 1 disqualified, got %d", got)
	}
	if got := countYAMLItems(sampleQualificationOutput, "missing"); got != 0 {
		t.Errorf("expected 0 for missing key, got %d", got)
	}
	if got := countYAMLItems("no yaml", "qualified"); got != 0 {
		t.Errorf("expected 0 without YAML block, got %d", got)
	}
}

func TestParseSingleDraftFromOutput_DraftForm(t *testing.T) {
	output := "```yaml\n" + `draft:
  id: create-widget
  title: "Create a Widget"
  description: "Users can create widgets"
  confidence: high
  discovered_from: ["widget.go"]
  uncertainty_notes: ["Exact limits unclear"]
  evidence:
    code_files: ["widget.go"]
    test_files: ["widget_test.go"]
  features:
    - id: widget-create
      description: "Create via CLI"
      acceptance_criteria:
        - "Given no widget, when create runs, then a widget exists"
  domain_knowledge: ["Widget: a thing"]
` + "```"
	draft, err := parseSingleDraftFromOutput(output)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if draft.ID != "create-widget" {
		t.Errorf("expected ID create-widget, got %q", draft.ID)
	}
	if draft.Confidence != domain.DraftConfidenceHigh {
		t.Errorf("expected high confidence, got %q", draft.Confidence)
	}
	if len(draft.Features) != 1 || draft.Features[0].ID != "widget-create" {
		t.Errorf("unexpected features: %+v", draft.Features)
	}
	if len(draft.Evidence.TestFiles) != 1 {
		t.Errorf("expected 1 test file, got %d", len(draft.Evidence.TestFiles))
	}
	if len(draft.UncertaintyNotes) != 1 {
		t.Errorf("expected 1 uncertainty note, got %d", len(draft.UncertaintyNotes))
	}
}

func TestParseSingleDraftFromOutput_DraftsListFallback(t *testing.T) {
	output := "```yaml\n" + `drafts:
  - id: first-draft
    title: "First"
    confidence: low
  - id: second-draft
    title: "Second"
` + "```"
	draft, err := parseSingleDraftFromOutput(output)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if draft.ID != "first-draft" {
		t.Errorf("expected first draft, got %q", draft.ID)
	}
	if draft.Confidence != domain.DraftConfidenceLow {
		t.Errorf("expected low confidence, got %q", draft.Confidence)
	}
}

func TestParseSingleDraftFromOutput_NoYAML(t *testing.T) {
	if _, err := parseSingleDraftFromOutput("no yaml at all"); err == nil {
		t.Error("expected error for output without YAML block")
	}
}

func TestParseSingleDraftFromOutput_EmptyDrafts(t *testing.T) {
	if _, err := parseSingleDraftFromOutput("```yaml\nother: value\n```"); err == nil {
		t.Error("expected error when no drafts found")
	}
}

func TestConvertDraftOutput_ConfidenceMapping(t *testing.T) {
	tests := []struct {
		input string
		want  domain.DraftConfidence
	}{
		{"high", domain.DraftConfidenceHigh},
		{"HIGH", domain.DraftConfidenceHigh},
		{"low", domain.DraftConfidenceLow},
		{"medium", domain.DraftConfidenceMedium},
		{"", domain.DraftConfidenceMedium},
		{"garbage", domain.DraftConfidenceMedium},
	}
	for _, tt := range tests {
		draft := convertDraftOutput(draftOutput{ID: "x", Confidence: tt.input})
		if draft.Confidence != tt.want {
			t.Errorf("confidence %q: expected %q, got %q", tt.input, tt.want, draft.Confidence)
		}
	}
}

func TestParseDomainDraftsFromOutput(t *testing.T) {
	output := "```yaml\n" + `drafts:
  - id: widget-context
    title: "Widget Context"
    bounded_context: widget
    description: "Owns widgets"
    confidence: high
    evidence:
      type_files: ["widget.go"]
    terms:
      - term: Widget
        canonical: true
        code_usage: "widget.go - Widget"
        definition: "A thing users create"
        aliases: ["Gadget"]
        evidence:
          files: ["widget.go"]
          lines: ["widget.go:10"]
    entities:
      - name: Widget
        description: "The widget entity"
        relationships:
          - type: contains
            target: Part
  - id: billing-context
    title: "Billing Context"
    bounded_context: billing
    confidence: low
` + "```"
	drafts, err := parseDomainDraftsFromOutput(output)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(drafts) != 2 {
		t.Fatalf("expected 2 drafts, got %d", len(drafts))
	}
	first := drafts[0]
	if first.BoundedContext != "widget" {
		t.Errorf("expected bounded context widget, got %q", first.BoundedContext)
	}
	if first.Confidence != domain.DraftDomainConfidenceHigh {
		t.Errorf("expected high confidence, got %q", first.Confidence)
	}
	if len(first.Terms) != 1 || first.Terms[0].Term != "Widget" {
		t.Fatalf("unexpected terms: %+v", first.Terms)
	}
	if first.Terms[0].Evidence == nil || len(first.Terms[0].Evidence.Lines) != 1 {
		t.Errorf("expected term evidence with 1 line, got %+v", first.Terms[0].Evidence)
	}
	if len(first.Entities) != 1 || len(first.Entities[0].Relationships) != 1 {
		t.Errorf("unexpected entities: %+v", first.Entities)
	}
	if drafts[1].Confidence != domain.DraftDomainConfidenceLow {
		t.Errorf("expected low confidence, got %q", drafts[1].Confidence)
	}
}

func TestParseDomainDraftsFromOutput_BareDraftsFallback(t *testing.T) {
	output := "Some preamble\ndrafts:\n  - id: widget-context\n    title: \"Widget Context\"\n"
	drafts, err := parseDomainDraftsFromOutput(output)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(drafts) != 1 || drafts[0].ID != "widget-context" {
		t.Errorf("expected 1 draft widget-context, got %+v", drafts)
	}
}

func TestParseDomainExcludedCandidates(t *testing.T) {
	output := "```yaml\n" + `drafts: []
excluded_candidates:
  - name: RetryBackoffCap
    description: "Max retry backoff is capped at 30s for this spec's poller"
    likely_spec: run-executor-retries
` + "```"
	excluded := parseDomainExcludedCandidates(output)
	if len(excluded) != 1 {
		t.Fatalf("expected 1 excluded candidate, got %d", len(excluded))
	}
	if excluded[0].Name != "RetryBackoffCap" || excluded[0].LikelySpec != "run-executor-retries" {
		t.Errorf("unexpected excluded candidate: %+v", excluded[0])
	}
	converted := convertExcludedCandidates(excluded)
	if len(converted) != 1 || converted[0].Description != excluded[0].Description {
		t.Errorf("unexpected conversion: %+v", converted)
	}
}

func TestParseDomainExcludedCandidates_NoYAML(t *testing.T) {
	if got := parseDomainExcludedCandidates("nothing here"); got != nil {
		t.Errorf("expected nil, got %+v", got)
	}
}

func TestParseDomainDraftsFromOutput_NoYAML(t *testing.T) {
	if _, err := parseDomainDraftsFromOutput("nothing here"); err == nil {
		t.Error("expected error for output without YAML block")
	}
}

func TestBuildIdentifyQualifyPrompt(t *testing.T) {
	prompt := buildIdentifyQualifyPrompt("CODEBASE-CONTEXT", "SPECS-SUMMARY")
	for _, want := range []string{"CODEBASE-CONTEXT", "SPECS-SUMMARY", "qualified:", "disqualified:", "IDENTIFY", "QUALIFY"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt missing %q", want)
		}
	}
}

func TestBuildRefinementAgentPrompt(t *testing.T) {
	candidate := qualifiedCandidate{
		ID:                  "create-widget",
		Title:               "Create a Widget",
		Description:         "Users can create widgets",
		SourceFiles:         []string{"widget.go", "handler.go"},
		QualificationReason: "User sees the widget",
	}
	prompt := buildRefinementAgentPrompt(candidate, 2, 3)
	for _, want := range []string{
		"iteration 2 of 3",
		"ID: create-widget",
		"Title: Create a Widget",
		"Source Files: widget.go, handler.go",
		"Qualification Reason: User sees the widget",
		"id: create-widget",
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt missing %q", want)
		}
	}
}
