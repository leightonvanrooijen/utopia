// Package ui holds the shared terminal-output vocabulary for utopia
// commands: glyph constants, banners, rules, phase progress, and discovery
// summaries. Commands construct a Printer over cobra's injectable writers
// (cmd.OutOrStdout() / cmd.ErrOrStderr()) so output is capturable in tests.
package ui

import (
	"fmt"
	"io"
	"os"
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

// DefaultPrinter returns a Printer over the process's own streams.
//
// It exists so os.Stdout and os.Stderr stay named in this package alone. A
// domain package that was handed no printer - a helper reached from a test, or a
// warning raised deep inside the store - still writes where it always did,
// without reaching for the process streams itself.
func DefaultPrinter() *Printer { return NewPrinter(os.Stdout, os.Stderr) }

// OrDefault returns p, or a printer over the process streams when p is nil.
// Domain packages call it once at the entry point so an optional printer field
// can be left unset without every internal call site checking for nil.
func OrDefault(p *Printer) *Printer {
	if p == nil {
		return DefaultPrinter()
	}
	return p
}

// Out returns the results writer, for the rare case of wiring a subprocess's
// stdout straight to it.
func (p *Printer) Out() io.Writer { return p.out }

// Err returns the diagnostics writer, for wiring a subprocess's stderr to it.
func (p *Printer) Err() io.Writer { return p.err }

// Printf writes a result to stdout.
func (p *Printer) Printf(format string, a ...any) { fmt.Fprintf(p.out, format, a...) }

// Print writes a result to stdout with no trailing newline.
func (p *Printer) Print(a ...any) { fmt.Fprint(p.out, a...) }

// Println writes a result to stdout followed by a newline.
func (p *Printer) Println(a ...any) { fmt.Fprintln(p.out, a...) }

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
