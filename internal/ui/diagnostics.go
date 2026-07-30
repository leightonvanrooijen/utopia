package ui

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
)

// DefaultLevel is the severity a Printer emits at when nothing configures it.
const DefaultLevel = slog.LevelInfo

// preformattedKey marks a record whose message is already a rendered terminal
// line - the output of Progressf, Warnf and the other formatted methods.
//
// The flag rides on the context rather than on an attribute so that only this
// package's handler sees it: a JSON or third-party handler receives the record
// with nothing extra in it. TextHandler needs the distinction because a
// preformatted message carries its own newline and its own interpolated values,
// so appending a newline or a key=value trailer to it would change output that
// predates slog.
type preformattedKey struct{}

var preformattedCtx = context.WithValue(context.Background(), preformattedKey{}, true)

func isPreformatted(ctx context.Context) bool {
	v, _ := ctx.Value(preformattedKey{}).(bool)
	return v
}

// TextHandler renders diagnostics in utopia's human-readable terminal form:
// a ⚠ or ✗ glyph for warn and error, the bare message for info and debug, and
// a key=value trailer for the attributes of a structured record.
//
// It is the handler a Printer uses when the caller configures nothing, which is
// why it renders rather than serializes: the diagnostic channel is read by a
// person watching a run, not by a log collector. Swap it for slog.JSONHandler
// (Printer.WithHandler) when a machine is reading.
type TextHandler struct {
	w     io.Writer
	level slog.Leveler
	mu    *sync.Mutex
	attrs []slog.Attr
	group string
}

// NewTextHandler returns a handler writing human-readable diagnostics to w at
// or above level. A nil level means DefaultLevel.
func NewTextHandler(w io.Writer, level slog.Leveler) *TextHandler {
	if level == nil {
		level = DefaultLevel
	}
	return &TextHandler{w: w, level: level, mu: &sync.Mutex{}}
}

func (h *TextHandler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= h.level.Level()
}

func (h *TextHandler) Handle(ctx context.Context, r slog.Record) error {
	var b strings.Builder

	switch {
	case r.Level >= slog.LevelError:
		b.WriteString(Failure + " ")
	case r.Level >= slog.LevelWarn:
		b.WriteString(Warning + " ")
	}
	b.WriteString(r.Message)

	if isPreformatted(ctx) {
		// The message is the line. Info and debug lines are written exactly as
		// their caller formatted them, trailing newline included or omitted, so a
		// streamed partial line stays partial; warn and error lines get the
		// newline that the glyph prefix implies.
		if r.Level >= slog.LevelWarn {
			b.WriteString("\n")
		}
	} else {
		for _, a := range h.attrs {
			h.appendAttr(&b, h.group, a)
		}
		r.Attrs(func(a slog.Attr) bool {
			h.appendAttr(&b, h.group, a)
			return true
		})
		b.WriteString("\n")
	}

	h.mu.Lock()
	defer h.mu.Unlock()
	_, err := io.WriteString(h.w, b.String())
	return err
}

func (h *TextHandler) appendAttr(b *strings.Builder, group string, a slog.Attr) {
	a.Value = a.Value.Resolve()
	if a.Equal(slog.Attr{}) {
		return
	}
	key := a.Key
	if group != "" {
		key = group + "." + key
	}
	if a.Value.Kind() == slog.KindGroup {
		for _, sub := range a.Value.Group() {
			h.appendAttr(b, key, sub)
		}
		return
	}
	fmt.Fprintf(b, " %s=%v", key, a.Value.Any())
}

func (h *TextHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	if len(attrs) == 0 {
		return h
	}
	clone := *h
	clone.attrs = append(append([]slog.Attr(nil), h.attrs...), attrs...)
	return &clone
}

func (h *TextHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	clone := *h
	if h.group != "" {
		name = h.group + "." + name
	}
	clone.group = name
	return &clone
}
