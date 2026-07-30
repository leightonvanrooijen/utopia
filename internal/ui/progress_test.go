package ui

import (
	"bytes"
	"regexp"
	"testing"
)

func TestProgressPhaseLifecycle(t *testing.T) {
	var buf bytes.Buffer
	prog := NewProgress(&buf, 4, false)

	prog.StartPhase(1, "Scanning files")
	prog.EndPhase("12 files found")

	want := regexp.MustCompile(`^\[1/4\] Scanning files\.\.\. done \(\d+\.\ds, 12 files found\)\n$`)
	if got := buf.String(); !want.MatchString(got) {
		t.Errorf("progress output = %q, want match for %q", got, want)
	}
}

func TestProgressEndPhaseWithoutDetail(t *testing.T) {
	var buf bytes.Buffer
	prog := NewProgress(&buf, 2, false)

	prog.StartPhase(2, "Saving drafts")
	prog.EndPhase("")

	want := regexp.MustCompile(`^\[2/2\] Saving drafts\.\.\. done \(\d+\.\ds\)\n$`)
	if got := buf.String(); !want.MatchString(got) {
		t.Errorf("progress output = %q, want match for %q", got, want)
	}
}

func TestVerbosefIsSilentWhenNotVerbose(t *testing.T) {
	var buf bytes.Buffer
	prog := NewProgress(&buf, 1, false)

	prog.Verbosef("  Collected: %s", "main.go")

	if buf.Len() != 0 {
		t.Errorf("expected no output, got %q", buf.String())
	}
	if prog.Verbose() {
		t.Error("Verbose() = true, want false")
	}
}

func TestVerbosefWritesWhenVerbose(t *testing.T) {
	var buf bytes.Buffer
	prog := NewProgress(&buf, 1, true)

	prog.Verbosef("  Collected: %s", "main.go")

	if got, want := buf.String(), "  Collected: main.go"; got != want {
		t.Errorf("verbose output = %q, want %q", got, want)
	}
	if !prog.Verbose() {
		t.Error("Verbose() = false, want true")
	}
}
