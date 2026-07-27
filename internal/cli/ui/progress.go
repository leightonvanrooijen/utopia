package ui

import (
	"fmt"
	"io"
	"time"
)

// Progress renders phase progress ("[n/N] name... done (X.Xs)") to a writer,
// with optional verbose detail lines in between.
type Progress struct {
	w           io.Writer
	phaseStart  time.Time
	totalPhases int
	verbose     bool
}

func NewProgress(w io.Writer, totalPhases int, verbose bool) *Progress {
	return &Progress{w: w, phaseStart: time.Now(), totalPhases: totalPhases, verbose: verbose}
}

// StartPhase restarts the phase timer and writes the "[n/N] name..." prefix.
func (p *Progress) StartPhase(phaseNum int, name string) {
	p.phaseStart = time.Now()
	fmt.Fprintf(p.w, "[%d/%d] %s...", phaseNum, p.totalPhases, name)
}

// EndPhase completes the phase line with elapsed time and an optional detail.
func (p *Progress) EndPhase(detail string) {
	elapsed := time.Since(p.phaseStart)
	if detail != "" {
		fmt.Fprintf(p.w, " done (%.1fs, %s)\n", elapsed.Seconds(), detail)
	} else {
		fmt.Fprintf(p.w, " done (%.1fs)\n", elapsed.Seconds())
	}
}

// Verbosef writes only when verbose mode is enabled.
func (p *Progress) Verbosef(format string, a ...any) {
	if p.verbose {
		fmt.Fprintf(p.w, format, a...)
	}
}

// Verbose reports whether verbose mode is enabled.
func (p *Progress) Verbose() bool { return p.verbose }
