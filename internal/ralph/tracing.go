package ralph

import (
	"context"
	"fmt"
	"sync"
	"time"

	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"

	"github.com/leightonvanrooijen/utopia/internal/ui"
)

// tracerName identifies this package's spans to anything reading the
// collected trace data, the same role an import path plays in a stack trace.
const tracerName = "github.com/leightonvanrooijen/utopia/internal/ralph"

// Span attribute keys. Kept as constants so a span's attributes and whatever
// later reads them (a persistence phase, a test) agree on the name.
const (
	attrCRID           = "cr_id"
	attrSpecRef        = "spec_ref"
	attrWorkItemID     = "work_item_id"
	attrIterationCount = "iteration_count"
	attrOutcome        = "outcome"
	attrConnector      = "connector"
	attrExitCode       = "exit_code"
	attrResolution     = "resolution_state"
	attrError          = "error"
)

// spanCollector is a sdktrace.SpanProcessor that keeps every ended span in
// memory instead of shipping it anywhere. It is the seam a later phase
// persists spans through: adding a second processor (an OTLP exporter, a
// file writer) is registering it alongside this one, not re-modelling how
// the loop reports timing.
//
// No exporter, collector, or network call is configured anywhere in this
// package - spans live and die inside this process.
type spanCollector struct {
	mu    sync.Mutex
	spans []sdktrace.ReadOnlySpan
}

func newSpanCollector() *spanCollector {
	return &spanCollector{}
}

// OnStart is required by sdktrace.SpanProcessor; this collector only cares
// about finished spans, so it does nothing here.
func (c *spanCollector) OnStart(context.Context, sdktrace.ReadWriteSpan) {}

// OnEnd records a finished span's read-only snapshot.
func (c *spanCollector) OnEnd(s sdktrace.ReadOnlySpan) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.spans = append(c.spans, s)
}

// Shutdown and ForceFlush satisfy sdktrace.SpanProcessor. There is nothing to
// flush - spans are already resident in c.spans the moment OnEnd returns.
func (c *spanCollector) Shutdown(context.Context) error   { return nil }
func (c *spanCollector) ForceFlush(context.Context) error { return nil }

// durationOf returns how long the span identified by id ran, or 0 if it has
// not ended yet (or was never recorded).
func (c *spanCollector) durationOf(id trace.SpanID) time.Duration {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, s := range c.spans {
		if s.SpanContext().SpanID() == id {
			return s.EndTime().Sub(s.StartTime())
		}
	}
	return 0
}

// sumChildDurations sums the wall clock of every ended span named name whose
// parent is the span identified by parent. A work item's Claude invocations,
// verification runs, and validator joins each recur across iterations, so
// their category total is the sum of every child span with that name rather
// than any single one of them.
func (c *spanCollector) sumChildDurations(parent trace.SpanID, name string) time.Duration {
	c.mu.Lock()
	defer c.mu.Unlock()
	var total time.Duration
	for _, s := range c.spans {
		if s.Parent().SpanID() == parent && s.Name() == name {
			total += s.EndTime().Sub(s.StartTime())
		}
	}
	return total
}

// newTracerProvider creates a TracerProvider over an in-process collector and
// returns both: the provider is what starts spans, the collector is what a
// later phase reads them back from. Registered as a plain
// sdktrace.SpanProcessor so adding a second one - an OTLP exporter - is a
// registration at this call site, not a change to any span's call site.
func newTracerProvider() (*sdktrace.TracerProvider, *spanCollector) {
	collector := newSpanCollector()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(collector))
	return tp, collector
}

// noopTracer is the fallback for an Engine constructed without a tracer
// wired in (every test that builds one directly). It creates spans that are
// never recorded, so tests observe the same behaviour they always have.
var noopTracer = noop.NewTracerProvider().Tracer(tracerName)

// attrStrings builds a []attribute.KeyValue of string attributes from the
// given key/value pairs, so span-start call sites read as a flat list of
// names rather than a wall of attribute.String(...) calls.
func attrStrings(pairs ...string) []attribute.KeyValue {
	attrs := make([]attribute.KeyValue, 0, len(pairs)/2)
	for i := 0; i+1 < len(pairs); i += 2 {
		attrs = append(attrs, attribute.String(pairs[i], pairs[i+1]))
	}
	return attrs
}

// renderTimingSummary renders a work item's total wall clock alongside its
// per-category breakdown, in the shape stepTimings.summary() used to produce.
//
// total is measured, not summed from claude+verification+validators: the
// categories account for part of it rather than partitioning it. Validators
// launch speculatively at workitem-completion-claimed and run alongside
// verification, so their share here is only what the loop still had to wait
// for at the join - the engine's resolution ledger reports each run's full
// duration. Time spent waiting out a Claude usage limit belongs to no step
// and shows up as the shortfall.
func renderTimingSummary(total, claude, verification, validators time.Duration) string {
	return fmt.Sprintf("total %s (claude %s, verification %s, validators %s)",
		ui.Duration(total),
		ui.Duration(claude),
		ui.Duration(verification),
		ui.Duration(validators),
	)
}
