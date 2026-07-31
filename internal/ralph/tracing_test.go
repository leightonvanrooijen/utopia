package ralph

import (
	"context"
	"regexp"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/leightonvanrooijen/utopia/internal/domain"
)

func TestRenderTimingSummary_ReportsTotalAndPerCategoryBreakdown(t *testing.T) {
	got := renderTimingSummary(
		14*time.Minute+22*time.Second,
		11*time.Minute+30*time.Second,
		2*time.Minute+10*time.Second,
		2100*time.Millisecond,
	)

	for _, want := range []string{"total 14m22s", "claude 11m30s", "verification 2m10s", "validators 2.1s"} {
		if !strings.Contains(got, want) {
			t.Errorf("summary must report %q, got %q", want, got)
		}
	}
}

func TestRenderTimingSummary_ReportsUntimedStepsAsZero(t *testing.T) {
	// A work item that completed with no verification command and no
	// validators configured still reports every category, so the breakdown
	// has a fixed shape across runs.
	got := renderTimingSummary(0, 0, 0, 0)

	for _, want := range []string{"verification 0.0s", "validators 0.0s"} {
		if !strings.Contains(got, want) {
			t.Errorf("summary must report %q, got %q", want, got)
		}
	}
}

// The resolution ledger is where a validator or connector run reports how long
// it actually took, labelled with its name - the loop only sees the aggregate
// outcome at the join.
func TestResolutionLedger_RecordsRunDurationLabelledWithName(t *testing.T) {
	sub := Subscription{
		Name:   "validators:after-workitem",
		Launch: EventWorkItemVerified,
		Join:   EventWorkItemVerified,
		Action: commandAction(domain.ConnectorConfig{Name: "validators:after-workitem", Command: "sleep 0.2"}, t.TempDir()),
	}
	en := NewEngine([]Subscription{sub})

	_, ledger := captureStd(t, func() {
		if err := en.Emit(context.Background(), Event{Name: EventWorkItemVerified}); err != nil {
			t.Fatalf("emit failed: %v", err)
		}
	})

	// A duration, not a fixed value: the assertion is that the elapsed run
	// time is reported next to the name, and that it is plausibly the 200ms
	// the action slept for rather than a zeroed clock.
	want := regexp.MustCompile(`validators:after-workitem ` + handleJoined + ` in 0\.[2-9]s`)
	if !want.MatchString(ledger) {
		t.Errorf("ledger must report the run duration beside the name, got:\n%s", ledger)
	}
}

// spanRecordsUnder is what a run record's persisted spans come from, so it has
// to capture the tree - name, parent linkage, attributes - and leave out
// anything the root span is not an ancestor of.
func TestSpanCollector_SpanRecordsUnder_CapturesTreeAndAttributes(t *testing.T) {
	tp, collector := newTracerProvider()
	tracer := tp.Tracer(tracerName)

	rootCtx, root := tracer.Start(context.Background(), "workitem-started", trace.WithAttributes(
		attribute.String(attrWorkItemID, "item-1"),
	))
	_, claude := tracer.Start(rootCtx, "claude")
	claude.End()
	_, unrelated := tracer.Start(context.Background(), "claude")
	unrelated.End()
	root.End()

	records := collector.spanRecordsUnder(root.SpanContext().SpanID())
	if len(records) != 2 {
		t.Fatalf("records = %+v, want the root and its claude child only", records)
	}
	if records[0].Name != "workitem-started" || records[1].Name != "claude" {
		t.Errorf("records = %+v, want root then child, ordered by start time", records)
	}
	if records[1].ParentSpanID != records[0].SpanID {
		t.Errorf("child parent_span_id = %q, want the root's span_id %q", records[1].ParentSpanID, records[0].SpanID)
	}
	if records[0].ParentSpanID != "" {
		t.Errorf("root parent_span_id = %q, want empty - its real parent is outside this item's tree", records[0].ParentSpanID)
	}
	if records[0].Attributes[attrWorkItemID] != "item-1" {
		t.Errorf("attributes = %+v, want work_item_id captured as a string", records[0].Attributes)
	}
	if records[0].DurationMS < 0 || records[1].DurationMS < 0 {
		t.Errorf("durations = %+v, want non-negative measured durations", records)
	}
}

// A run that fails before its own span ends must still persist whatever child
// spans finished before the failure, rather than reporting a zero-duration
// entry for the span that never closed.
func TestSpanCollector_SpanRecordsUnder_OmitsSpansThatHaveNotEnded(t *testing.T) {
	tp, collector := newTracerProvider()
	tracer := tp.Tracer(tracerName)

	rootCtx, root := tracer.Start(context.Background(), "workitem-started")
	_, claude := tracer.Start(rootCtx, "claude")
	claude.End()
	// root stays open, as it would on a path that writes the record before the
	// item's own span ends.

	records := collector.spanRecordsUnder(root.SpanContext().SpanID())
	if len(records) != 1 || records[0].Name != "claude" {
		t.Fatalf("records = %+v, want only the ended child, not the still-open root", records)
	}
}
