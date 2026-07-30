package ui

import (
	"bytes"
	"strings"
	"testing"
)

func TestTableAlignsColumnsToTheirWidestCell(t *testing.T) {
	var stdout, stderr bytes.Buffer
	p := NewPrinter(&stdout, &stderr)

	p.Table(Table{
		Headers: []string{"MODEL", "ATTEMPTS"},
		Rows: [][]string{
			{"sonnet", "3"},
			{"a-very-long-model-id", "1200"},
		},
	})

	lines := strings.Split(strings.TrimRight(stdout.String(), "\n"), "\n")
	if len(lines) != 4 {
		t.Fatalf("lines = %q, want header, rule and two rows", lines)
	}
	width := len([]rune(lines[0]))
	for _, line := range lines {
		if len([]rune(line)) != width {
			t.Errorf("line %q is %d wide, want every line %d wide", line, len([]rune(line)), width)
		}
	}
	// First column left-aligned, the rest right-aligned against the widest cell.
	if !strings.HasPrefix(lines[2], "sonnet ") {
		t.Errorf("row %q does not left-align its first column", lines[2])
	}
	if !strings.HasSuffix(lines[2], "   3") {
		t.Errorf("row %q does not right-align its figure under ATTEMPTS", lines[2])
	}
	if stderr.Len() != 0 {
		t.Errorf("expected empty stderr, got %q", stderr.String())
	}
}

func TestTableShortRowIsPaddedNotDropped(t *testing.T) {
	var stdout, stderr bytes.Buffer
	p := NewPrinter(&stdout, &stderr)

	p.Table(Table{Headers: []string{"A", "B", "C"}, Rows: [][]string{{"x"}}})

	lines := strings.Split(strings.TrimRight(stdout.String(), "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("lines = %q, want the short row printed", lines)
	}
	if strings.TrimSpace(lines[2]) != "x" {
		t.Errorf("short row = %q, want its one cell with the rest empty", lines[2])
	}
}

func TestTableWithNoHeadersPrintsNothing(t *testing.T) {
	var stdout, stderr bytes.Buffer
	p := NewPrinter(&stdout, &stderr)

	p.Table(Table{Rows: [][]string{{"x"}}})

	if stdout.Len() != 0 {
		t.Errorf("stdout = %q, want nothing without headers", stdout.String())
	}
}
