package ralph

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/leightonvanrooijen/utopia/internal/ui"
)

// Handle states. Every launched handle starts running and resolves as
// exactly one of joined, cancelled, or drained.
const (
	// handleRunning means the work is in flight and its outcome uncollected.
	handleRunning = "running"
	// handleJoined means a join event blocked on the work and collected it.
	handleJoined = "joined"
	// handleCancelled means a cancel event killed the work in flight.
	handleCancelled = "cancelled"
	// handleDrained means the outcome was collected without a join event:
	// launch-only work that finished on its own, or leftover work collected
	// when the loop ends.
	handleDrained = "drained"
)

// Subscription binds engine verbs to lifecycle events: launch spawns the
// action when Launch fires, join blocks at Join and collects the outcome,
// and cancel kills the in-flight run when any Cancel event fires.
//
// User-facing connector modes compile down to subscription shapes: gating is
// launch and join on the same event, notify is launch only. The engine knows
// nothing about modes.
type Subscription struct {
	// Name identifies the subscription in logs and gate errors
	Name string
	// Launch is the event that spawns the action (required)
	Launch string
	// Join is the event that blocks on the action and collects its outcome.
	// Empty means the action is never joined; it resolves by draining.
	Join string
	// Cancel lists events that kill the action while it is in flight
	Cancel []string
	// Timeout bounds the action's runtime from launch; 0 means unlimited.
	// An action exceeding it is killed and its outcome recorded as failed.
	Timeout time.Duration
	// Action starts the subscribed work when Launch fires
	Action Action
}

// Action starts a subscription's work. It must return without waiting for
// the work to finish; calling the returned wait function blocks until the
// work completes and reports its outcome. Cancelling ctx must terminate
// in-flight work promptly so wait returns.
type Action func(ctx context.Context, e Event) (wait func() ConnectorResult)

// handle tracks one launched run of a subscription's action through the
// state machine running -> joined | cancelled | drained. The result is
// written by the collector goroutine before done closes and must only be
// read after done closes.
type handle struct {
	sub    *Subscription
	cancel context.CancelFunc
	done   chan struct{}
	result ConnectorResult
	state  string
	// launchedAt is when the action started, so a resolving handle can report
	// how long its work ran - a validator batch or connector command timed as
	// a black-box call from the outside
	launchedAt time.Time
	// out is where this handle's ledger line and failure block are written. It is
	// copied from the engine at launch so every door - join, cancel, drain -
	// reports to the same printer; nil means the process's own streams.
	out *ui.Printer
	// span records this handle's run as a child of the work item span (or the
	// execution span, for run-scoped connectors) that launched it. It carries
	// the connector name from its start and is closed with the exit code,
	// resolution state, and error once the handle resolves.
	span trace.Span
}

// join blocks until the work exits, collects its outcome, and marks the
// handle joined.
func (h *handle) join() ConnectorResult {
	<-h.done
	h.state = handleJoined
	logResolution(h)
	return h.result
}

// cancelRun kills the in-flight work and blocks until it exits. The action's
// SIGTERM -> SIGKILL escalation bounds the wait to the grace period.
//
// An error the cancellation itself caused is dropped: the outcome of a
// cancelled handle is the cancellation, so reporting the kill's shrapnel as
// well would read as a genuine connector failure.
func (h *handle) cancelRun() {
	h.cancel()
	<-h.done
	h.state = handleCancelled
	if causedByCancellation(h.result.Err) {
		h.result.Err = nil
	}
	logResolution(h)
}

// causedByCancellation reports whether err describes the kill rather than the
// work: the run's context being cancelled, or a subprocess killed by the
// SIGTERM/SIGKILL the cancellation sent. A timeout is deliberately not
// cancellation - it is the handle's own failure and stays reported as one.
func causedByCancellation(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) {
		return true
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		if status, ok := exitErr.Sys().(syscall.WaitStatus); ok && status.Signaled() {
			sig := status.Signal()
			return sig == syscall.SIGTERM || sig == syscall.SIGKILL
		}
	}
	return false
}

// Engine launches, joins, and cancels subscription actions as lifecycle
// events fire. It is not safe for concurrent use; the execution loop emits
// events from a single goroutine.
type Engine struct {
	subs    []Subscription
	handles []*handle
	// out receives the resolution ledger and the gate lines Emit reports. The
	// execution loop sets it to the printer the run was handed; nil means the
	// process's own streams.
	out *ui.Printer
	// tracer starts each launched handle's span. The execution loop sets it to
	// the run's real tracer; a noop tracer is the default so an engine built
	// directly, as every existing test does, still runs without one wired in.
	tracer trace.Tracer
}

// NewEngine creates an engine over a fixed set of subscriptions.
func NewEngine(subs []Subscription) *Engine {
	return &Engine{subs: subs, tracer: noopTracer}
}

// Emit processes subscriptions for the event in fixed pass order: cancel
// in-flight handles first, then launch new ones, then join. A handle
// launched by this event is never cancelled by it, and is joined by it when
// the subscription launches and joins on the same event (gating shape).
//
// Join failures block: the first is returned as a *GateError carrying the
// action's stdout. All matching joins still collect even after one blocks,
// so no outcome is silently dropped.
func (en *Engine) Emit(ctx context.Context, e Event) error {
	en.reapCompleted()

	// Pass 1: cancel.
	for _, h := range en.handles {
		if h.state == handleRunning && containsEvent(h.sub.Cancel, e.Name) {
			h.cancelRun()
		}
	}

	// Pass 2: launch.
	for i := range en.subs {
		if en.subs[i].Launch == e.Name {
			en.handles = append(en.handles, launchHandle(ctx, &en.subs[i], e, en.out, en.tracer))
		}
	}

	// Pass 3: join.
	var gateErr error
	for _, h := range en.handles {
		if h.state != handleRunning || h.sub.Join != e.Name {
			continue
		}
		res := h.join()
		if res.Err == nil {
			continue
		}
		ui.OrDefault(en.out).Progressf("  gating connector %s blocked %s: %v\n", res.Name, e.Name, res.Err)
		if gateErr == nil {
			gateErr = &GateError{Connector: res.Name, Event: e.Name, Stdout: res.Stdout, Aggregate: res.Aggregate}
		}
	}
	return gateErr
}

// Drain blocks until every running handle resolves and records each outcome
// to the resolution ledger. Timeouts are already armed per handle, so a
// timed-out action is killed rather than awaited forever; an action with no
// timeout is awaited indefinitely. Once Drain returns, no handle is left
// running, so no connector child process outlives the run.
func (en *Engine) Drain() {
	for _, h := range en.handles {
		if h.state != handleRunning {
			continue
		}
		<-h.done
		h.state = handleDrained
		logResolution(h)
	}
}

// reapCompleted resolves launch-only handles whose work finished on its own
// since the last emit. They drain: their outcome is collected without a join
// event and recorded to the resolution ledger.
func (en *Engine) reapCompleted() {
	for _, h := range en.handles {
		if h.state != handleRunning || h.sub.Join != "" {
			continue
		}
		select {
		case <-h.done:
			h.state = handleDrained
			logResolution(h)
		default:
		}
	}
}

// logResolution appends one line to the handle-resolution ledger: the
// connector name, how the handle resolved (joined, cancelled, or drained),
// how long its work ran, its exit code, and the failure cause when present,
// with any captured output beneath. Every door - join, cancel, drain
// - logs through here, so a finished run leaves a record of every launched
// handle, how long it took, and no outcome is silently dropped.
//
// A failing handle's captured output is the feedback the loop injects into the
// next iteration, so it is rendered as a failure-output block - the same block
// the verification command's output gets - rather than as indented ledger
// chatter. This is the only place the validator subscriptions' feedback is
// printed; the loop's own call sites deliberately do not print it again, because
// one failure shown twice reads as two. Output from a handle that did not fail
// is incidental connector chatter and stays indented under its line.
//
// The elapsed time is the handle's full run - launch to resolution - which for
// a validators subscription is how long the validator agents actually took,
// however much of it overlapped verification.
func logResolution(h *handle) {
	if h.span != nil {
		h.span.SetAttributes(
			attribute.String(attrConnector, h.sub.Name),
			attribute.Int(attrExitCode, h.result.ExitCode),
			attribute.String(attrResolution, h.state),
		)
		if h.result.Err != nil {
			h.span.SetAttributes(attribute.String(attrError, h.result.Err.Error()))
		}
		h.span.End()
	}

	p := ui.OrDefault(h.out)
	line := fmt.Sprintf("  connector %s %s in %s (exit %d)",
		h.sub.Name, h.state, ui.Duration(time.Since(h.launchedAt)), h.result.ExitCode)
	if h.result.Err != nil {
		line += ": " + h.result.Err.Error()
	}
	p.Progressf("%s\n", line)
	out := strings.TrimSpace(h.result.Stdout + h.result.Stderr)
	if out == "" {
		return
	}
	if h.result.Err != nil {
		printFailureBlock(p, h.sub.Name, out)
		return
	}
	for _, l := range strings.Split(out, "\n") {
		p.Progressf("    %s\n", l)
	}
}

// launchHandle starts the subscription's action under a per-handle context
// carrying the subscription timeout, and spawns the collector goroutine that
// records the outcome and releases the context once the work exits.
//
// The handle's span is parented on the event payload's span context when the
// dispatch that launched it carries one - a work item's span for
// work-item-scoped events, the execution span otherwise - rather than on
// whatever span happens to be live on ctx, since ctx here is the run's own
// context and carries no per-item span of its own.
func launchHandle(ctx context.Context, sub *Subscription, e Event, out *ui.Printer, tracer trace.Tracer) *handle {
	runCtx, cancel := context.WithCancel(ctx)
	if sub.Timeout > 0 {
		timeoutCtx, timeoutCancel := context.WithTimeout(runCtx, sub.Timeout)
		runCtx = timeoutCtx
		runCancel := cancel
		cancel = func() { timeoutCancel(); runCancel() }
	}

	spanParentCtx := runCtx
	if sc := e.Payload.parentSpanCtx; sc.IsValid() {
		spanParentCtx = trace.ContextWithSpanContext(runCtx, sc)
	}
	spanCtx, span := tracer.Start(spanParentCtx, sub.Name)

	h := &handle{sub: sub, cancel: cancel, done: make(chan struct{}), state: handleRunning, launchedAt: time.Now(), out: out, span: span}
	wait := sub.Action(spanCtx, e)
	go func() {
		h.result = wait()
		h.cancel()
		close(h.done)
	}()
	return h
}

// containsEvent reports whether name is in events.
func containsEvent(events []string, name string) bool {
	for _, event := range events {
		if event == name {
			return true
		}
	}
	return false
}
