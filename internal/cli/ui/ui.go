// Package ui holds the shared terminal-output vocabulary for utopia
// commands: glyph constants, banners, rules, phase progress, and discovery
// summaries. Commands construct a Printer over cobra's injectable writers
// (cmd.OutOrStdout() / cmd.ErrOrStderr()) so output is capturable in tests.
package ui

import (
	"fmt"
	"io"
	"strings"
)

const (
	Success = "✓"
	Failure = "✗"
	Warning = "⚠"
	Bullet  = "•"

	ruleWidth = 63
)

// ConfidenceGlyph returns the glyph for a draft confidence level:
// ● for "high", ◐ for "medium", ○ otherwise.
func ConfidenceGlyph(confidence string) string {
	switch confidence {
	case "high":
		return "●"
	case "medium":
		return "◐"
	default:
		return "○"
	}
}

// Printer routes results to out (stdout) and diagnostics to err (stderr).
type Printer struct {
	out io.Writer // results (stdout)
	err io.Writer // diagnostics (stderr)
}

func NewPrinter(out, err io.Writer) *Printer {
	return &Printer{out: out, err: err}
}

// Printf writes a result to stdout.
func (p *Printer) Printf(format string, a ...any) { fmt.Fprintf(p.out, format, a...) }

// Successf writes a ✓-prefixed line to stdout.
func (p *Printer) Successf(format string, a ...any) {
	fmt.Fprintf(p.out, "%s %s\n", Success, fmt.Sprintf(format, a...))
}

// Warnf writes a ⚠-prefixed line to stderr.
func (p *Printer) Warnf(format string, a ...any) {
	fmt.Fprintf(p.err, "%s %s\n", Warning, fmt.Sprintf(format, a...))
}

// Progressf writes a diagnostic (progress/status) line to stderr, leaving
// stdout clean for pipeable results.
func (p *Printer) Progressf(format string, a ...any) { fmt.Fprintf(p.err, format, a...) }

// Banner writes the title centered between two ═ rules, framed by blank lines.
func (p *Printer) Banner(title string) {
	rule := strings.Repeat("═", ruleWidth)
	pad := max(0, (ruleWidth-len(title))/2)
	fmt.Fprintf(p.out, "\n%s\n%s%s\n%s\n\n", rule, strings.Repeat(" ", pad), title, rule)
}

// Rule writes a ─ divider line.
func (p *Printer) Rule() {
	fmt.Fprintln(p.out, strings.Repeat("─", ruleWidth))
}
