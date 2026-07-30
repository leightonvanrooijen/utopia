package ui

import (
	"bytes"
	"context"
	"log/slog"
	"testing"
)

// recorder collects records instead of rendering them, so a test can assert on
// what a diagnostic carries rather than on how it happened to be printed.
type recorder struct {
	records *[]slog.Record // shared with every handler derived via WithAttrs
	attrs   []slog.Attr
}

func newRecorder() *recorder { return &recorder{records: &[]slog.Record{}} }

func (r *recorder) Enabled(context.Context, slog.Level) bool { return true }

func (r *recorder) Handle(_ context.Context, rec slog.Record) error {
	rec.AddAttrs(r.attrs...)
	*r.records = append(*r.records, rec)
	return nil
}

func (r *recorder) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &recorder{records: r.records, attrs: append(append([]slog.Attr(nil), r.attrs...), attrs...)}
}

func (r *recorder) WithGroup(string) slog.Handler { return r }

func (r *recorder) all() []slog.Record { return *r.records }

func attrsOf(t *testing.T, rec slog.Record) map[string]any {
	t.Helper()
	got := map[string]any{}
	rec.Attrs(func(a slog.Attr) bool {
		got[a.Key] = a.Value.Any()
		return true
	})
	return got
}

// The point of routing diagnostics through slog: a caller reads the values off
// the record by key, so a message can be reworded without breaking anything that
// consumes it. This test never looks at rendered text.
func TestDiagnosticCarriesTypedAttributes(t *testing.T) {
	var stdout bytes.Buffer
	rec := newRecorder()
	p := NewPrinter(&stdout, &bytes.Buffer{}).WithHandler(rec)

	p.Warn("validator missing run field",
		slog.String("validator", "reviewer.yaml"),
		slog.Int("attempt", 2))

	if len(rec.all()) != 1 {
		t.Fatalf("recorded %d diagnostics, want 1", len(rec.all()))
	}
	r := rec.all()[0]
	if r.Level != slog.LevelWarn {
		t.Errorf("level = %v, want %v", r.Level, slog.LevelWarn)
	}
	got := attrsOf(t, r)
	if got["validator"] != "reviewer.yaml" {
		t.Errorf("validator attr = %v, want reviewer.yaml", got["validator"])
	}
	if got["attempt"] != int64(2) {
		t.Errorf("attempt attr = %v (%T), want int64(2)", got["attempt"], got["attempt"])
	}
}

// Run context is attached once and inherited, so a diagnostic raised deep in a
// run reports the change request and work item it belongs to without any call
// site interpolating them into a message.
func TestWithAttrsAttachesRunContextToEveryDiagnostic(t *testing.T) {
	rec := newRecorder()
	run := NewPrinter(&bytes.Buffer{}, &bytes.Buffer{}).WithHandler(rec).
		WithAttrs(slog.String("cr_id", "06_observability")).
		WithAttrs(slog.String("work_item_id", "phase-2-structured-diagnostics"))

	run.Progressf("Analyzing %d files...\n", 7)
	run.Info("verification passed")

	if len(rec.all()) != 2 {
		t.Fatalf("recorded %d diagnostics, want 2", len(rec.all()))
	}
	for _, r := range rec.all() {
		got := attrsOf(t, r)
		if got["cr_id"] != "06_observability" {
			t.Errorf("%q cr_id = %v, want 06_observability", r.Message, got["cr_id"])
		}
		if got["work_item_id"] != "phase-2-structured-diagnostics" {
			t.Errorf("%q work_item_id = %v, want phase-2-structured-diagnostics", r.Message, got["work_item_id"])
		}
	}
}

func TestFormattedDiagnosticsCarryTheirSeverity(t *testing.T) {
	cases := []struct {
		name string
		emit func(*Printer)
		want slog.Level
	}{
		{"Debugf", func(p *Printer) { p.Debugf("detail\n") }, slog.LevelDebug},
		{"Progressf", func(p *Printer) { p.Progressf("progress\n") }, slog.LevelInfo},
		{"Warnf", func(p *Printer) { p.Warnf("careful") }, slog.LevelWarn},
		{"Errorf", func(p *Printer) { p.Errorf("broken") }, slog.LevelError},
	}
	for _, c := range cases {
		rec := newRecorder()
		p := NewPrinter(&bytes.Buffer{}, &bytes.Buffer{}).WithHandler(rec)
		c.emit(p)
		if len(rec.all()) != 1 {
			t.Fatalf("%s recorded %d diagnostics, want 1", c.name, len(rec.all()))
		}
		if got := rec.all()[0].Level; got != c.want {
			t.Errorf("%s level = %v, want %v", c.name, got, c.want)
		}
	}
}

// A result is the command's answer, not an INFO record. Nothing on the result
// channel may reach the diagnostic handler, or a quiet run would swallow the
// thing the caller ran the command for.
func TestResultMethodsBypassSlog(t *testing.T) {
	var stdout, stderr bytes.Buffer
	rec := newRecorder()
	p := NewPrinter(&stdout, &stderr).WithHandler(rec)

	p.Printf("Created %d drafts\n", 3)
	p.Successf("cr.yaml is valid")
	p.Banner("DISCOVERY COMPLETE")
	p.Rule()
	p.Summary(Summary{BannerTitle: "DISCOVERY COMPLETE", Items: []SummaryItem{{Confidence: "high", Title: "a draft"}}})

	if len(rec.all()) != 0 {
		t.Errorf("result output produced %d slog records, want 0", len(rec.all()))
	}
	if stdout.Len() == 0 {
		t.Error("results did not reach stdout")
	}
	if stderr.Len() != 0 {
		t.Errorf("results leaked to stderr: %q", stderr.String())
	}
}

// The handler a user gets without configuring anything renders the same lines
// the printer wrote before slog existed: no level tag, no key=value trailer, and
// no newline added to a message that carried its own (or deliberately did not).
func TestDefaultHandlerRendersPreformattedLinesVerbatim(t *testing.T) {
	var stderr bytes.Buffer
	p := NewPrinter(&bytes.Buffer{}, &stderr).
		WithAttrs(slog.String("cr_id", "06_observability"))

	p.Progressf("[1/3] Collecting...")
	p.Progressf(" done (0.4s)\n")
	p.Warnf("failed to commit: %s", "boom")
	p.Errorf("validator %s crashed", "reviewer")

	want := "[1/3] Collecting... done (0.4s)\n" +
		Warning + " failed to commit: boom\n" +
		Failure + " validator reviewer crashed\n"
	if got := stderr.String(); got != want {
		t.Errorf("stderr = %q, want %q", got, want)
	}
}

// A structured diagnostic has no rendered line to preserve, so the default
// handler shows its attributes rather than dropping them.
func TestDefaultHandlerRendersAttributesOfStructuredDiagnostics(t *testing.T) {
	var stderr bytes.Buffer
	p := NewPrinter(&bytes.Buffer{}, &stderr).WithAttrs(slog.String("cr_id", "06"))

	p.Warn("validator missing run field", slog.String("validator", "reviewer.yaml"))

	want := Warning + " validator missing run field cr_id=06 validator=reviewer.yaml\n"
	if got := stderr.String(); got != want {
		t.Errorf("stderr = %q, want %q", got, want)
	}
}

func TestDiagnosticsBelowTheHandlerLevelAreDropped(t *testing.T) {
	var stderr bytes.Buffer
	p := NewPrinter(&bytes.Buffer{}, &bytes.Buffer{}).
		WithHandler(NewTextHandler(&stderr, slog.LevelWarn))

	p.Debugf("detail\n")
	p.Progressf("progress\n")
	p.Warnf("careful")

	if got, want := stderr.String(), Warning+" careful\n"; got != want {
		t.Errorf("stderr = %q, want %q", got, want)
	}
}
