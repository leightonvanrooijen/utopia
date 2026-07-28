package ui

import (
	"bytes"
	"strings"
	"testing"
)

func TestSummaryRendersFullReport(t *testing.T) {
	var stdout, stderr bytes.Buffer
	p := NewPrinter(&stdout, &stderr)

	p.Summary(Summary{
		BannerTitle:  "DISCOVERY COMPLETE",
		CreatedNoun:  "draft specifications",
		SectionTitle: "Draft Specifications:",
		DraftsDir:    "/tmp/drafts",
		Items: []SummaryItem{
			{
				Confidence:    "low",
				Title:         "Low Draft",
				Details:       []string{"ID: low-draft"},
				Uncertainties: []string{"unclear behavior"},
			},
			{
				Confidence: "high",
				Title:      "High Draft",
				Details:    []string{"ID: high-draft", "Features: 2"},
			},
		},
		NextSteps: []string{
			"1. Review drafts in /tmp/drafts",
			"2. Run 'utopia shape' to validate and refine drafts",
		},
	})

	doubleRule := strings.Repeat("═", 63)
	singleRule := strings.Repeat("─", 63)
	pad := strings.Repeat(" ", (63-len("DISCOVERY COMPLETE"))/2)
	want := strings.Join([]string{
		"",
		doubleRule,
		pad + "DISCOVERY COMPLETE",
		doubleRule,
		"",
		"Created 2 draft specifications:",
		"  • HIGH confidence:   1",
		"  • MEDIUM confidence: 0",
		"  • LOW confidence:    1",
		"",
		"Drafts saved to: /tmp/drafts",
		"",
		"Draft Specifications:",
		singleRule,
		"",
		"● [HIGH] High Draft",
		"  ID: high-draft",
		"  Features: 2",
		"",
		"○ [LOW] Low Draft",
		"  ID: low-draft",
		"  Uncertainties:",
		"    ⚠ unclear behavior",
		"",
		singleRule,
		"Next steps:",
		"  1. Review drafts in /tmp/drafts",
		"  2. Run 'utopia shape' to validate and refine drafts",
		"",
	}, "\n") + "\n"

	if got := stdout.String(); got != want {
		t.Errorf("summary output = %q, want %q", got, want)
	}
	if stderr.Len() != 0 {
		t.Errorf("expected empty stderr, got %q", stderr.String())
	}
}

func TestSummarySortsItemsHighToLow(t *testing.T) {
	var stdout, stderr bytes.Buffer
	p := NewPrinter(&stdout, &stderr)

	p.Summary(Summary{
		BannerTitle:  "DISCOVERY COMPLETE",
		CreatedNoun:  "draft specifications",
		SectionTitle: "Draft Specifications:",
		DraftsDir:    "/tmp/drafts",
		Items: []SummaryItem{
			{Confidence: "medium", Title: "Middle"},
			{Confidence: "low", Title: "Last"},
			{Confidence: "high", Title: "First"},
		},
	})

	out := stdout.String()
	first := strings.Index(out, "● [HIGH] First")
	middle := strings.Index(out, "◐ [MEDIUM] Middle")
	last := strings.Index(out, "○ [LOW] Last")
	if first == -1 || middle == -1 || last == -1 {
		t.Fatalf("missing item lines in output:\n%s", out)
	}
	if !(first < middle && middle < last) {
		t.Errorf("items not sorted high → low: positions %d, %d, %d", first, middle, last)
	}
}
