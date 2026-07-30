// Package ui holds the shared terminal-output vocabulary for utopia
// commands: glyph constants, banners, rules, phase progress, and discovery
// summaries. Commands construct a Printer over cobra's injectable writers
// (cmd.OutOrStdout() / cmd.ErrOrStderr()) so output is capturable in tests.
//
// Results and diagnostics are two channels, not two levels. Results go straight
// to stdout; diagnostics go through log/slog, so each one carries a severity and
// typed attributes and can be re-rendered by any handler.
package ui

import (
	"context"
	"fmt"
	"io"
	"log/slog"
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

// Printer routes results to out (stdout) and diagnostics through a slog.Logger
// whose default handler writes the human-readable form to err (stderr).
//
// The split is deliberate and it is not a level: a result is the command's
// answer, so "Created 3 drafts" is not an INFO record that a quieter run may
// drop - it is what the caller ran the command for. Results therefore go
// straight to the writer, and only diagnostics carry a severity.
type Printer struct {
	out io.Writer // results (stdout)
	err io.Writer // diagnostics (stderr)
	log *slog.Logger
}

// NewPrinter returns a Printer whose diagnostics are gated by the process-wide
// level (see SetLevel) and whose results are never gated at all.
func NewPrinter(out, err io.Writer) *Printer {
	return &Printer{out: out, err: err, log: slog.New(NewTextHandler(err, level))}
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

// Warnf emits a warn-level diagnostic, rendered by the default handler as a
// ⚠-prefixed line on stderr.
func (p *Printer) Warnf(format string, a ...any) {
	p.logf(slog.LevelWarn, format, a...)
}

// Errorf emits an error-level diagnostic, rendered by the default handler as a
// ✗-prefixed line on stderr. It reports a problem; it does not end the command -
// a failure the caller must act on is a returned error, per error-handling.
func (p *Printer) Errorf(format string, a ...any) {
	p.logf(slog.LevelError, format, a...)
}

// Progressf emits an info-level diagnostic (progress/status), leaving stdout
// clean for pipeable results.
func (p *Printer) Progressf(format string, a ...any) { p.logf(slog.LevelInfo, format, a...) }

// Debugf emits a debug-level diagnostic: detail an operator asks for, dropped
// unless the printer's level admits it.
func (p *Printer) Debugf(format string, a ...any) { p.logf(slog.LevelDebug, format, a...) }

// logf emits a pre-rendered line. The message is the whole line, so the handler
// is told not to append attributes or a newline to it - which is what keeps the
// ~172 formatted call sites byte-identical to what they printed before slog.
func (p *Printer) logf(level slog.Level, format string, a ...any) {
	if !p.log.Enabled(preformattedCtx, level) {
		return
	}
	p.log.Log(preformattedCtx, level, fmt.Sprintf(format, a...))
}

// Debug, Info, Warn and Error emit a diagnostic whose contextual values are
// typed slog attributes rather than text interpolated into the message. They sit
// alongside the formatted methods above rather than replacing them, so call
// sites move to attributes one at a time.
func (p *Printer) Debug(msg string, attrs ...slog.Attr) { p.logAttrs(slog.LevelDebug, msg, attrs) }
func (p *Printer) Info(msg string, attrs ...slog.Attr)  { p.logAttrs(slog.LevelInfo, msg, attrs) }
func (p *Printer) Warn(msg string, attrs ...slog.Attr)  { p.logAttrs(slog.LevelWarn, msg, attrs) }
func (p *Printer) Error(msg string, attrs ...slog.Attr) { p.logAttrs(slog.LevelError, msg, attrs) }

func (p *Printer) logAttrs(level slog.Level, msg string, attrs []slog.Attr) {
	p.log.LogAttrs(context.Background(), level, msg, attrs...)
}

// WithAttrs returns a copy of p whose every diagnostic carries attrs. An
// execution run attaches its cr_id and work_item_id here once, so no call site
// below has to thread them into a message.
func (p *Printer) WithAttrs(attrs ...slog.Attr) *Printer {
	if len(attrs) == 0 {
		return p
	}
	clone := *p
	clone.log = slog.New(p.log.Handler().WithAttrs(attrs))
	return &clone
}

// WithHandler returns a copy of p whose diagnostics go to h instead of the
// default human-readable handler. Results are unaffected: they are never routed
// through slog.
func (p *Printer) WithHandler(h slog.Handler) *Printer {
	clone := *p
	clone.log = slog.New(h)
	return &clone
}

// Logger exposes the diagnostic logger for a caller that already speaks slog.
func (p *Printer) Logger() *slog.Logger { return p.log }

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
