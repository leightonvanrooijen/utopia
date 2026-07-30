package ui

import (
	"fmt"
	"io"
	"log/slog"
	"time"
)

// Progress renders phase progress ("[n/N] name... done (X.Xs)") to a writer,
// with detail lines in between.
//
// Both are diagnostics, so both obey the process-wide level rather than a flag
// chosen at construction: a phase line is info, a detail line is debug. A run at
// warn or error therefore emits no progress at all, and a run at debug emits the
// detail a --verbose flag used to ask for - the two spellings are one request.
type Progress struct {
	w           io.Writer
	phaseStart  time.Time
	totalPhases int
}

// NewProgress returns a Progress over w.
func NewProgress(w io.Writer, totalPhases int) *Progress {
	return &Progress{
		w:           w,
		phaseStart:  time.Now(),
		totalPhases: totalPhases,
	}
}

// StartPhase restarts the phase timer and writes the "[n/N] name..." prefix.
//
// The timer runs whichever level is active, so a phase whose line is suppressed
// still reports the duration it took if the level is raised - the level decides
// what is written, not what is measured.
func (p *Progress) StartPhase(phaseNum int, name string) {
	p.phaseStart = time.Now()
	if !Enabled(slog.LevelInfo) {
		return
	}
	fmt.Fprintf(p.w, "[%d/%d] %s...", phaseNum, p.totalPhases, name)
}

// EndPhase completes the phase line with elapsed time and an optional detail.
func (p *Progress) EndPhase(detail string) {
	if !Enabled(slog.LevelInfo) {
		return
	}
	elapsed := time.Since(p.phaseStart)
	if detail != "" {
		fmt.Fprintf(p.w, " done (%.1fs, %s)\n", elapsed.Seconds(), detail)
	} else {
		fmt.Fprintf(p.w, " done (%.1fs)\n", elapsed.Seconds())
	}
}

// Verbosef writes a detail line, which the active level admits only at debug.
func (p *Progress) Verbosef(format string, a ...any) {
	if !Enabled(slog.LevelDebug) {
		return
	}
	fmt.Fprintf(p.w, format, a...)
}

// Verbose reports whether the active level admits debug detail - the state
// --verbose and --log-level debug both ask for - for a caller with more to do
// than format one line before it decides.
func (p *Progress) Verbose() bool { return Enabled(slog.LevelDebug) }
