package ui

import (
	"fmt"
	"log/slog"
	"strings"
)

// LevelNames lists the accepted level names, lowest severity first, in the form
// an error message or a flag's help text names them.
const LevelNames = "debug, info, warn, error"

// level is the process-wide diagnostic threshold every Printer reads.
//
// It is package state rather than a Printer field on purpose: verbosity is one
// property of an invocation, chosen once from --log-level or UTOPIA_LOG_LEVEL,
// while Printers are constructed per command handler (and per domain helper that
// was handed none). A slog.LevelVar means the threshold is honoured by Printers
// built before it was set, so the root command can resolve the level without
// every construction site having to be handed it.
//
// Only diagnostics consult it. Results are written straight to stdout, so no
// level can suppress a command's answer.
var level = new(slog.LevelVar) // the zero LevelVar is LevelInfo, which is DefaultLevel

// Level reports the active diagnostic threshold.
func Level() slog.Level { return level.Level() }

// SetLevel sets the active diagnostic threshold for every Printer.
func SetLevel(l slog.Level) { level.Set(l) }

// ParseLevel maps a level name to its slog level. An unrecognised name is an
// error naming the accepted values rather than a silent fallback to the default:
// a run asked for debug output and given info has no way to tell.
func ParseLevel(name string) (slog.Level, error) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "debug":
		return slog.LevelDebug, nil
	case "info":
		return slog.LevelInfo, nil
	case "warn":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return 0, fmt.Errorf("invalid log level %q (accepted: %s)", name, LevelNames)
	}
}
