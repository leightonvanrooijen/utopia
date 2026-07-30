package ui

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

func TestParseLevel(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  slog.Level
	}{
		{name: "debug", input: "debug", want: slog.LevelDebug},
		{name: "info", input: "info", want: slog.LevelInfo},
		{name: "warn", input: "warn", want: slog.LevelWarn},
		{name: "error", input: "error", want: slog.LevelError},
		{name: "mixed case", input: "Debug", want: slog.LevelDebug},
		{name: "surrounding space", input: " warn ", want: slog.LevelWarn},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseLevel(tt.input)
			if err != nil {
				t.Fatalf("ParseLevel(%q) error = %v", tt.input, err)
			}
			if got != tt.want {
				t.Errorf("ParseLevel(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

// An unknown name must fail loudly: a run that asked for debug and silently got
// info would report a missing diagnostic as a missing event.
func TestParseLevelRejectsUnknownName(t *testing.T) {
	for _, input := range []string{"", "trace", "verbose", "warning", "INFO!"} {
		_, err := ParseLevel(input)
		if err == nil {
			t.Fatalf("ParseLevel(%q) = nil error, want an error", input)
		}
		if !strings.Contains(err.Error(), LevelNames) {
			t.Errorf("error %q does not list the accepted values", err)
		}
		if !strings.Contains(err.Error(), input) {
			t.Errorf("error %q does not name the rejected value %q", err, input)
		}
	}
}

func TestSetLevelGatesDiagnostics(t *testing.T) {
	t.Cleanup(func() { SetLevel(DefaultLevel) })

	tests := []struct {
		name  string
		level slog.Level
		want  []string
		gone  []string
	}{
		{name: "debug admits everything", level: slog.LevelDebug,
			want: []string{"detail", "progress", "careful", "broken"}},
		{name: "info drops debug", level: slog.LevelInfo,
			want: []string{"progress", "careful", "broken"}, gone: []string{"detail"}},
		{name: "warn drops progress", level: slog.LevelWarn,
			want: []string{"careful", "broken"}, gone: []string{"detail", "progress"}},
		{name: "error drops all but error", level: slog.LevelError,
			want: []string{"broken"}, gone: []string{"detail", "progress", "careful"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			SetLevel(tt.level)
			var stdout, stderr bytes.Buffer
			p := NewPrinter(&stdout, &stderr)

			p.Debugf("detail\n")
			p.Progressf("progress\n")
			p.Warnf("careful")
			p.Errorf("broken")
			// The result channel is not a level: it is emitted whatever the
			// threshold, including at error.
			p.Printf("the answer\n")

			for _, want := range tt.want {
				if !strings.Contains(stderr.String(), want) {
					t.Errorf("stderr %q is missing %q", stderr.String(), want)
				}
			}
			for _, gone := range tt.gone {
				if strings.Contains(stderr.String(), gone) {
					t.Errorf("stderr %q contains suppressed %q", stderr.String(), gone)
				}
			}
			if got := stdout.String(); got != "the answer\n" {
				t.Errorf("stdout = %q, want the result at every level", got)
			}
		})
	}
}

// A Printer built before the level was resolved must still honour it: handlers
// are constructed per command, the level is chosen once for the invocation.
func TestSetLevelAppliesToExistingPrinter(t *testing.T) {
	t.Cleanup(func() { SetLevel(DefaultLevel) })

	var stderr bytes.Buffer
	p := NewPrinter(&bytes.Buffer{}, &stderr)

	p.Debugf("before\n")
	SetLevel(slog.LevelDebug)
	p.Debugf("after\n")

	if got := stderr.String(); got != "after\n" {
		t.Errorf("stderr = %q, want only the line emitted after SetLevel", got)
	}
}

// --log-level debug and --verbose are one request, so a debug level turns on the
// detail lines a verbose flag would have.
func TestProgressVerboseFollowsDebugLevel(t *testing.T) {
	t.Cleanup(func() { SetLevel(DefaultLevel) })

	var quiet bytes.Buffer
	if prog := NewProgress(&quiet, 1); prog.Verbose() {
		t.Error("Progress is verbose at the default level")
	}

	SetLevel(slog.LevelDebug)
	var loud bytes.Buffer
	prog := NewProgress(&loud, 1)
	if !prog.Verbose() {
		t.Fatal("Progress is not verbose at debug level")
	}
	prog.Verbosef("  Collected: %s", "a.go")
	if got := loud.String(); got != "  Collected: a.go" {
		t.Errorf("detail line = %q, want the verbose output", got)
	}
}

// Phase progress is a diagnostic like any other, so the one threshold silences
// it too: a run quieter than info gets no "[n/N] name... done" line at all,
// while the default and debug keep the output every existing run has.
func TestProgressPhaseLinesObeyLevel(t *testing.T) {
	t.Cleanup(func() { SetLevel(DefaultLevel) })

	tests := []struct {
		name    string
		level   slog.Level
		written bool
	}{
		{name: "debug writes the phase line", level: slog.LevelDebug, written: true},
		{name: "info writes the phase line", level: slog.LevelInfo, written: true},
		{name: "warn writes nothing", level: slog.LevelWarn},
		{name: "error writes nothing", level: slog.LevelError},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			SetLevel(tt.level)
			var buf bytes.Buffer
			prog := NewProgress(&buf, 4)

			prog.StartPhase(1, "Scanning files")
			prog.EndPhase("12 files found")

			if written := buf.Len() > 0; written != tt.written {
				t.Errorf("progress output = %q, want written = %v", buf.String(), tt.written)
			}
		})
	}
}
