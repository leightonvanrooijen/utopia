package ui

import (
	"fmt"
	"strings"
)

// Table is a column-aligned block of rows for a report: a header row, a rule
// under it, and one line per row. It exists because a report's value is in
// scanning a column - tokens per completed work item down the page - and that
// only works if the columns line up whatever the widths of the values.
//
// The first column is left-aligned and every other column right-aligned, which
// is the shape every report here has: one identifier followed by figures.
type Table struct {
	Headers []string
	Rows    [][]string
}

// Table writes t to stdout, sizing each column to its widest cell. Rows shorter
// than the header are padded with empty cells rather than dropped, so a partial
// row is visible instead of silently misaligning the ones after it.
func (p *Printer) Table(t Table) {
	if len(t.Headers) == 0 {
		return
	}

	widths := make([]int, len(t.Headers))
	for i, h := range t.Headers {
		widths[i] = len([]rune(h))
	}
	for _, row := range t.Rows {
		for i, cell := range row {
			if i < len(widths) && len([]rune(cell)) > widths[i] {
				widths[i] = len([]rune(cell))
			}
		}
	}

	fmt.Fprintln(p.out, tableLine(t.Headers, widths))
	rule := make([]string, len(widths))
	for i, w := range widths {
		rule[i] = strings.Repeat("─", w)
	}
	fmt.Fprintln(p.out, tableLine(rule, widths))
	for _, row := range t.Rows {
		fmt.Fprintln(p.out, tableLine(row, widths))
	}
}

// tableLine renders one line: first cell left-aligned, the rest right-aligned,
// two spaces between columns, no trailing whitespace.
func tableLine(cells []string, widths []int) string {
	var b strings.Builder
	for i, w := range widths {
		cell := ""
		if i < len(cells) {
			cell = cells[i]
		}
		if i > 0 {
			b.WriteString("  ")
		}
		pad := w - len([]rune(cell))
		if pad < 0 {
			pad = 0
		}
		if i == 0 {
			b.WriteString(cell)
			b.WriteString(strings.Repeat(" ", pad))
			continue
		}
		b.WriteString(strings.Repeat(" ", pad))
		b.WriteString(cell)
	}
	return strings.TrimRight(b.String(), " ")
}
