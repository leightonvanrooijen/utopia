package ralph

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"github.com/leightonvanrooijen/utopia/internal/cli/ui"
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
}

// NewEngine creates an engine over a fixed set of subscriptions.
func NewEngine(subs []Subscription) *Engine {
	return &Engine{subs: subs}
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
			en.handles = append(en.handles, launchHandle(ctx, &en.subs[i], e))
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
		fmt.Printf("  gating connector %s blocked %s: %v\n", res.Name, e.Name, res.Err)
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
	line := fmt.Sprintf("  connector %s %s in %s (exit %d)",
		h.sub.Name, h.state, ui.Duration(time.Since(h.launchedAt)), h.result.ExitCode)
	if h.result.Err != nil {
		line += ": " + h.result.Err.Error()
	}
	fmt.Println(line)
	out := strings.TrimSpace(h.result.Stdout + h.result.Stderr)
	if out == "" {
		return
	}
	if h.result.Err != nil {
		printFailureBlock(h.sub.Name, out)
		return
	}
	for _, l := range strings.Split(out, "\n") {
		fmt.Println("    " + l)
	}
}

// launchHandle starts the subscription's action under a per-handle context
// carrying the subscription timeout, and spawns the collector goroutine that
// records the outcome and releases the context once the work exits.
func launchHandle(ctx context.Context, sub *Subscription, e Event) *handle {
	runCtx, cancel := context.WithCancel(ctx)
	if sub.Timeout > 0 {
		timeoutCtx, timeoutCancel := context.WithTimeout(runCtx, sub.Timeout)
		runCtx = timeoutCtx
		runCancel := cancel
		cancel = func() { timeoutCancel(); runCancel() }
	}

	h := &handle{sub: sub, cancel: cancel, done: make(chan struct{}), state: handleRunning, launchedAt: time.Now()}
	wait := sub.Action(runCtx, e)
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
