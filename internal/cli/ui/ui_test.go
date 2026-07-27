package ui

import (
	"bytes"
	"strings"
	"testing"
)

func TestPrintfWritesToStdout(t *testing.T) {
	var stdout, stderr bytes.Buffer
	p := NewPrinter(&stdout, &stderr)

	p.Printf("Created %d drafts\n", 3)

	if got, want := stdout.String(), "Created 3 drafts\n"; got != want {
		t.Errorf("stdout = %q, want %q", got, want)
	}
	if stderr.Len() != 0 {
		t.Errorf("expected empty stderr, got %q", stderr.String())
	}
}

func TestSuccessfWritesGlyphToStdout(t *testing.T) {
	var stdout, stderr bytes.Buffer
	p := NewPrinter(&stdout, &stderr)

	p.Successf("%s is valid", "cr.yaml")

	if got, want := stdout.String(), "✓ cr.yaml is valid\n"; got != want {
		t.Errorf("stdout = %q, want %q", got, want)
	}
	if stderr.Len() != 0 {
		t.Errorf("expected empty stderr, got %q", stderr.String())
	}
}

func TestWarnfWritesGlyphToStderr(t *testing.T) {
	var stdout, stderr bytes.Buffer
	p := NewPrinter(&stdout, &stderr)

	p.Warnf("failed to commit: %s", "boom")

	if got, want := stderr.String(), "⚠ failed to commit: boom\n"; got != want {
		t.Errorf("stderr = %q, want %q", got, want)
	}
	if stdout.Len() != 0 {
		t.Errorf("expected empty stdout, got %q", stdout.String())
	}
}

func TestBannerCentersTitleBetweenComputedRules(t *testing.T) {
	var stdout, stderr bytes.Buffer
	p := NewPrinter(&stdout, &stderr)

	p.Banner("DISCOVERY COMPLETE")

	rule := strings.Repeat("═", 63)
	pad := strings.Repeat(" ", (63-len("DISCOVERY COMPLETE"))/2)
	want := "\n" + rule + "\n" + pad + "DISCOVERY COMPLETE\n" + rule + "\n\n"
	if got := stdout.String(); got != want {
		t.Errorf("banner = %q, want %q", got, want)
	}
}

func TestBannerHandlesTitleWiderThanRule(t *testing.T) {
	var stdout, stderr bytes.Buffer
	p := NewPrinter(&stdout, &stderr)

	long := strings.Repeat("X", 80)
	p.Banner(long)

	lines := strings.Split(stdout.String(), "\n")
	if got, want := lines[2], long; got != want {
		t.Errorf("title line = %q, want unpadded %q", got, want)
	}
}

func TestRuleIsComputedWidth(t *testing.T) {
	var stdout, stderr bytes.Buffer
	p := NewPrinter(&stdout, &stderr)

	p.Rule()

	if got, want := stdout.String(), strings.Repeat("─", 63)+"\n"; got != want {
		t.Errorf("rule = %q, want %q", got, want)
	}
}

func TestConfidenceGlyph(t *testing.T) {
	cases := map[string]string{
		"high":    "●",
		"medium":  "◐",
		"low":     "○",
		"unknown": "○",
		"":        "○",
	}
	for confidence, want := range cases {
		if got := ConfidenceGlyph(confidence); got != want {
			t.Errorf("ConfidenceGlyph(%q) = %q, want %q", confidence, got, want)
		}
	}
}
