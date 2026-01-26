package formatter

import (
	"strings"
	"testing"
)

func TestFormat_IndentsWithTwoSpaces(t *testing.T) {
	input := `root:
  nested:
      deeply: value`

	formatted, err := Format([]byte(input))
	if err != nil {
		t.Fatalf("Format() error = %v", err)
	}

	// Check that output uses 2-space indentation
	result := string(formatted)
	if !strings.Contains(result, "  nested:") {
		t.Errorf("expected 2-space indent, got:\n%s", result)
	}
}

func TestFormat_RetainsLineBreaks(t *testing.T) {
	input := `first: value

second: value`

	formatted, err := Format([]byte(input))
	if err != nil {
		t.Fatalf("Format() error = %v", err)
	}

	result := string(formatted)
	// With retain_line_breaks=true, blank line should be preserved
	if !strings.Contains(result, "value\n\nsecond") {
		t.Errorf("expected blank line to be retained, got:\n%s", result)
	}
}

func TestFormat_AddsEOFNewline(t *testing.T) {
	input := `key: value`

	formatted, err := Format([]byte(input))
	if err != nil {
		t.Fatalf("Format() error = %v", err)
	}

	if !strings.HasSuffix(string(formatted), "\n") {
		t.Errorf("expected trailing newline, got:\n%q", formatted)
	}
}

func TestFormat_TrimsTrailingWhitespace(t *testing.T) {
	input := "key: value   \nother: data  "

	formatted, err := Format([]byte(input))
	if err != nil {
		t.Fatalf("Format() error = %v", err)
	}

	result := string(formatted)
	if strings.Contains(result, "value   ") || strings.Contains(result, "data  ") {
		t.Errorf("expected trailing whitespace to be trimmed, got:\n%q", result)
	}
}

func TestFormat_InvalidYAML(t *testing.T) {
	input := `key: [unclosed`

	_, err := Format([]byte(input))
	if err == nil {
		t.Error("expected error for invalid YAML, got nil")
	}
}

func TestFormat_ValidYAML(t *testing.T) {
	input := `id: test
title: Test Spec
features:
  - id: feature-1
    description: A feature`

	formatted, err := Format([]byte(input))
	if err != nil {
		t.Fatalf("Format() error = %v", err)
	}

	result := string(formatted)
	if !strings.Contains(result, "id: test") {
		t.Errorf("expected formatted output to contain original content, got:\n%s", result)
	}
}

func TestFormat_SpecLikeYAML(t *testing.T) {
	// Test with a realistic Utopia spec structure
	input := `id: yaml-formatting
title: YAML Formatting System
status: draft
description: |
  A standardized YAML formatting package.

domain_knowledge:
  - Google yamlfmt is used as a Go library

features:
  - id: formatter-package
    description: |
      A dedicated formatter package.
    acceptance_criteria:
      - Package lives at internal/infra/formatter
      - Exposes a Format function`

	formatted, err := Format([]byte(input))
	if err != nil {
		t.Fatalf("Format() error = %v", err)
	}

	result := string(formatted)

	// Verify structure is preserved
	if !strings.Contains(result, "id: yaml-formatting") {
		t.Errorf("expected id to be preserved")
	}
	if !strings.Contains(result, "description: |") {
		t.Errorf("expected multiline description to be preserved")
	}
	if !strings.Contains(result, "features:") {
		t.Errorf("expected features to be preserved")
	}
	// Verify proper indentation
	if !strings.Contains(result, "  - id: formatter-package") {
		t.Errorf("expected features list to use 2-space indent")
	}
}
