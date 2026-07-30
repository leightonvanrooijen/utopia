package ralph

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"sync"
	"testing"
	"time"
)

// eventLog records observable engine actions from multiple goroutines.
type eventLog struct {
	mu      sync.Mutex
	entries []string
}

func (l *eventLog) add(entry string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.entries = append(l.entries, entry)
}

func (l *eventLog) all() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.entries...)
}

// instantAction completes immediately with the given result.
func instantAction(result ConnectorResult) Action {
	return func(ctx context.Context, e Event) func() ConnectorResult {
		return func() ConnectorResult { return result }
	}
}

// blockingAction logs its launch, then blocks until release closes or ctx is
// cancelled. A cancelled run logs "<name>:killed" and reports ctx's error.
func blockingAction(name string, log *eventLog, release chan struct{}) Action {
	return func(ctx context.Context, e Event) func() ConnectorResult {
		log.add("launch:" + name)
		return func() ConnectorResult {
			select {
			case <-release:
				return ConnectorResult{Name: name}
			case <-ctx.Done():
				log.add(name + ":killed")
				return ConnectorResult{Name: name, Err: ctx.Err(), TimedOut: ctx.Err() == context.DeadlineExceeded}
			}
		}
	}
}

func TestEngine_EmitPassOrder_CancelThenLaunchThenJoin(t *testing.T) {
	log := &eventLog{}
	gate := Subscription{
		Name:   "gate",
		Launch: EventWorkItemVerified,
		Join:   EventWorkItemVerified,
		Action: func(ctx context.Context, e Event) func() ConnectorResult {
			log.add("launch:gate")
			return func() ConnectorResult { return ConnectorResult{Name: "gate"} }
		},
	}
	speculative := Subscription{
		Name:   "speculative",
		Launch: EventWorkItemStarted,
		Cancel: []string{EventWorkItemVerified},
		Action: blockingAction("speculative", log, nil),
	}
	en := NewEngine([]Subscription{gate, speculative})

	if err := en.Emit(context.Background(), Event{Name: EventWorkItemStarted}); err != nil {
		t.Fatalf("launch emit failed: %v", err)
	}
	if err := en.Emit(context.Background(), Event{Name: EventWorkItemVerified}); err != nil {
		t.Fatalf("cancel+launch+join emit failed: %v", err)
	}

	want := []string{"launch:speculative", "speculative:killed", "launch:gate"}
	got := log.all()
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("expected cancel pass to complete before launch pass, got %v, want %v", got, want)
	}

	if en.handles[0].state != handleCancelled {
		t.Errorf("cancelled handle state = %s, want %s", en.handles[0].state, handleCancelled)
	}
	if en.handles[1].state != handleJoined {
		t.Errorf("joined handle state = %s, want %s", en.handles[1].state, handleJoined)
	}
}

func TestEngine_EmitDoesNotCancelHandleLaunchedBySameEvent(t *testing.T) {
	log := &eventLog{}
	release := make(chan struct{})
	sub := Subscription{
		Name:   "self",
		Launch: EventWorkItemStarted,
		Cancel: []string{EventWorkItemStarted},
		Action: blockingAction("self", log, release),
	}
	en := NewEngine([]Subscription{sub})

	if err := en.Emit(context.Background(), Event{Name: EventWorkItemStarted}); err != nil {
		t.Fatalf("emit failed: %v", err)
	}
	if en.handles[0].state != handleRunning {
		t.Errorf("handle launched by an event must not be cancelled by that same emit, state = %s", en.handles[0].state)
	}

	close(release)
	en.Drain()
	if en.handles[0].state != handleDrained {
		t.Errorf("drained handle state = %s, want %s", en.handles[0].state, handleDrained)
	}
}

func TestEngine_JoinFailureReturnsGateErrorWithStdout(t *testing.T) {
	sub := Subscription{
		Name:   "gate",
		Launch: EventWorkItemVerified,
		Join:   EventWorkItemVerified,
		Action: instantAction(ConnectorResult{Name: "gate", Stdout: "lint failed\n", Err: errors.New("exit status 1")}),
	}
	en := NewEngine([]Subscription{sub})

	err := en.Emit(context.Background(), Event{Name: EventWorkItemVerified})

	var ge *GateError
	if !errors.As(err, &ge) {
		t.Fatalf("expected *GateError, got %v", err)
	}
	if ge.Connector != "gate" || ge.Event != EventWorkItemVerified {
		t.Errorf("gate error identifies %s on %s, want gate on %s", ge.Connector, ge.Event, EventWorkItemVerified)
	}
	if ge.Stdout != "lint failed\n" {
		t.Errorf("gate error must carry stdout, got %q", ge.Stdout)
	}
	if en.handles[0].state != handleJoined {
		t.Errorf("failed join still resolves the handle as joined, got %s", en.handles[0].state)
	}
}

func TestEngine_FirstJoinFailureWinsButAllJoinsCollect(t *testing.T) {
	failing := func(name string) Subscription {
		return Subscription{
			Name:   name,
			Launch: EventWorkItemVerified,
			Join:   EventWorkItemVerified,
			Action: instantAction(ConnectorResult{Name: name, Err: errors.New("exit status 1")}),
		}
	}
	en := NewEngine([]Subscription{failing("first-gate"), failing("second-gate")})

	err := en.Emit(context.Background(), Event{Name: EventWorkItemVerified})

	var ge *GateError
	if !errors.As(err, &ge) {
		t.Fatalf("expected *GateError, got %v", err)
	}
	if ge.Connector != "first-gate" {
		t.Errorf("first join failure wins, got %s", ge.Connector)
	}
	for i, h := range en.handles {
		if h.state != handleJoined {
			t.Errorf("handle %d must still be collected after an earlier gate blocked, state = %s", i, h.state)
		}
	}
}

func TestEngine_LaunchOnlyHandleReapedAsDrainedOnLaterEmit(t *testing.T) {
	sub := Subscription{
		Name:   "notify",
		Launch: EventWorkItemStarted,
		Action: instantAction(ConnectorResult{Name: "notify"}),
	}
	en := NewEngine([]Subscription{sub})

	if err := en.Emit(context.Background(), Event{Name: EventWorkItemStarted}); err != nil {
		t.Fatalf("emit failed: %v", err)
	}
	<-en.handles[0].done

	if err := en.Emit(context.Background(), Event{Name: EventWorkItemCommitted}); err != nil {
		t.Fatalf("emit failed: %v", err)
	}
	if en.handles[0].state != handleDrained {
		t.Errorf("completed launch-only handle must reap as drained, got %s", en.handles[0].state)
	}
}

func TestEngine_CancelDropsCancellationCausedError(t *testing.T) {
	// The shape of the after-workitem validator abandoned by a verification
	// failure: its git diff subprocess is killed by the cancellation and the
	// wrapped "signal: killed" must not resolve as a validator failure.
	killed := func(ctx context.Context, e Event) func() ConnectorResult {
		cmd := exec.CommandContext(ctx, "sh", "-c", "sleep 5")
		if err := cmd.Start(); err != nil {
			t.Fatalf("start failed: %v", err)
		}
		return func() ConnectorResult {
			err := cmd.Wait()
			return ConnectorResult{Name: "validators:after-workitem", Err: fmt.Errorf("failed to compute git diff: %w", err)}
		}
	}
	sub := Subscription{
		Name:   "validators:after-workitem",
		Launch: EventWorkItemCompletionClaimed,
		Join:   EventWorkItemVerified,
		Cancel: []string{EventWorkItemVerificationFailed},
		Action: killed,
	}
	en := NewEngine([]Subscription{sub})

	if err := en.Emit(context.Background(), Event{Name: EventWorkItemCompletionClaimed}); err != nil {
		t.Fatalf("launch emit failed: %v", err)
	}
	if err := en.Emit(context.Background(), Event{Name: EventWorkItemVerificationFailed}); err != nil {
		t.Fatalf("cancel emit failed: %v", err)
	}

	h := en.handles[0]
	if h.state != handleCancelled {
		t.Fatalf("handle state = %s, want %s", h.state, handleCancelled)
	}
	if h.result.Err != nil {
		t.Errorf("cancellation-caused error must not be reported as a failure, got %v", h.result.Err)
	}
}

func TestCausedByCancellation(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"context cancelled", fmt.Errorf("running validators: %w", context.Canceled), true},
		{"deadline exceeded", fmt.Errorf("timed out: %w", context.DeadlineExceeded), false},
		{"genuine failure", errors.New("validators failed"), false},
		{"nonzero exit", fmt.Errorf("git diff failed: %w", exitErr(t, "exit 1")), false},
		{"killed subprocess", fmt.Errorf("git diff failed: %w", exitErr(t, "kill -KILL $$")), true},
		{"terminated subprocess", fmt.Errorf("git diff failed: %w", exitErr(t, "kill -TERM $$")), true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := causedByCancellation(tc.err); got != tc.want {
				t.Errorf("causedByCancellation(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

// exitErr runs a shell command expected to fail and returns its exit error.
func exitErr(t *testing.T, command string) error {
	t.Helper()
	err := exec.Command("sh", "-c", command).Run()
	if err == nil {
		t.Fatalf("command %q unexpectedly succeeded", command)
	}
	return err
}

func TestEngine_TimeoutKillsHandleAndRecordsFailure(t *testing.T) {
	log := &eventLog{}
	sub := Subscription{
		Name:    "slow-gate",
		Launch:  EventWorkItemVerified,
		Join:    EventWorkItemVerified,
		Timeout: 50 * time.Millisecond,
		Action:  blockingAction("slow-gate", log, nil),
	}
	en := NewEngine([]Subscription{sub})

	start := time.Now()
	err := en.Emit(context.Background(), Event{Name: EventWorkItemVerified})

	if err == nil {
		t.Fatal("timed-out join must block, got nil")
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Errorf("timeout did not kill the handle promptly, took %v", elapsed)
	}
	if !en.handles[0].result.TimedOut {
		t.Error("expected the handle outcome to record the timeout as a failure")
	}
}
