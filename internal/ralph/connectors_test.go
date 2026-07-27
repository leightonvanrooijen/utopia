package ralph

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/leightonvanrooijen/utopia/internal/domain"
)

// captureStdout redirects os.Stdout while fn runs and returns everything it
// printed, so tests can assert on the resolution ledger.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	orig := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stdout = w
	fn()
	if err := w.Close(); err != nil {
		t.Fatalf("close pipe: %v", err)
	}
	os.Stdout = orig
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read pipe: %v", err)
	}
	return string(out)
}

func TestCompileConnectors_NotifyCompilesToLaunchOnly(t *testing.T) {
	subs := CompileConnectors([]domain.ConnectorConfig{
		{Name: "slack", On: []string{EventExecutionCompleted, EventExecutionFailed}, Command: "echo hi"},
	}, t.TempDir())

	if len(subs) != 2 {
		t.Fatalf("expected one subscription per subscribed event, got %d", len(subs))
	}
	for i, wantLaunch := range []string{EventExecutionCompleted, EventExecutionFailed} {
		if subs[i].Launch != wantLaunch {
			t.Errorf("subs[%d].Launch = %q, want %q", i, subs[i].Launch, wantLaunch)
		}
		if subs[i].Join != "" {
			t.Errorf("notify must compile with no join, got %q", subs[i].Join)
		}
		if len(subs[i].Cancel) != 0 {
			t.Errorf("notify must compile with no cancel events, got %v", subs[i].Cancel)
		}
	}
}

func TestCompileConnectors_GatingCompilesToLaunchAndJoinOnSameEvent(t *testing.T) {
	subs := CompileConnectors([]domain.ConnectorConfig{
		{Name: "lint", On: []string{EventWorkItemVerified}, Command: "npm run lint", Mode: domain.ConnectorModeGating, Timeout: "30s"},
	}, t.TempDir())

	if len(subs) != 1 {
		t.Fatalf("expected 1 subscription, got %d", len(subs))
	}
	if subs[0].Launch != EventWorkItemVerified || subs[0].Join != EventWorkItemVerified {
		t.Errorf("gating must launch and join on the same event, got launch %q join %q", subs[0].Launch, subs[0].Join)
	}
	if subs[0].Timeout != 30*time.Second {
		t.Errorf("timeout must compile to a duration, got %v", subs[0].Timeout)
	}
}

// gatingRunner builds a runner whose handles resolve synchronously at the
// event, so tests can inspect collected results deterministically.
func gatingRunner(t *testing.T, dir, name, event, command string) *ConnectorRunner {
	t.Helper()
	return NewConnectorRunner([]domain.ConnectorConfig{
		{Name: name, On: []string{event}, Command: command, Mode: domain.ConnectorModeGating},
	}, dir)
}

func TestConnectorRunner_PayloadDeliveredAsJSONOnStdin(t *testing.T) {
	runner := gatingRunner(t, t.TempDir(), "echo-stdin", EventWorkItemStarted, "cat")

	payload := EventPayload{
		CRID:           "cr-42",
		CRTitle:        "Add connectors",
		SpecID:         "cr-42/phase-1",
		WorkItemID:     "wi-1",
		IterationCount: 3,
		CommitSHA:      "abc123",
	}
	if err := runner.Handle(context.Background(), Event{Name: EventWorkItemStarted, Payload: payload}); err != nil {
		t.Fatalf("connector failed: %v", err)
	}

	result := runner.engine.handles[0].result
	var got EventPayload
	if err := json.Unmarshal([]byte(result.Stdout), &got); err != nil {
		t.Fatalf("stdout is not valid JSON payload: %v (stdout=%q)", err, result.Stdout)
	}
	if got != payload {
		t.Errorf("payload mismatch: got %+v, want %+v", got, payload)
	}
}

func TestConnectorRunner_EnvironmentVariablesSet(t *testing.T) {
	dir := t.TempDir()
	runner := gatingRunner(t, dir, "env-check", EventExecutionStarted, `echo "$UTOPIA_EVENT|$UTOPIA_CR_ID|$UTOPIA_PROJECT_DIR"`)

	if err := runner.Handle(context.Background(), Event{
		Name:    EventExecutionStarted,
		Payload: EventPayload{CRID: "cr-7"},
	}); err != nil {
		t.Fatalf("connector failed: %v", err)
	}

	want := EventExecutionStarted + "|cr-7|" + dir + "\n"
	if got := runner.engine.handles[0].result.Stdout; got != want {
		t.Errorf("expected env vars %q, got %q", want, got)
	}
}

func TestConnectorRunner_RunsInProjectDirectory(t *testing.T) {
	dir := t.TempDir()
	runner := gatingRunner(t, dir, "pwd", EventExecutionStarted, "pwd")

	if err := runner.Handle(context.Background(), Event{Name: EventExecutionStarted}); err != nil {
		t.Fatalf("connector failed: %v", err)
	}

	// Resolve symlinks: on macOS t.TempDir() is under /var which links to /private/var
	wantDir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatalf("failed to resolve temp dir: %v", err)
	}
	gotDir, err := filepath.EvalSymlinks(strings.TrimSpace(runner.engine.handles[0].result.Stdout))
	if err != nil {
		t.Fatalf("failed to resolve reported working dir: %v", err)
	}
	if gotDir != wantDir {
		t.Errorf("expected working directory %q, got %q", wantDir, gotDir)
	}
}

func TestConnectorRunner_JoinCapturesOutputAndExitCode(t *testing.T) {
	runner := gatingRunner(t, t.TempDir(), "both", EventWorkItemVerified, "echo out; echo err >&2; exit 3")

	err := runner.Handle(context.Background(), Event{Name: EventWorkItemVerified})
	if err == nil {
		t.Fatal("expected gate error for non-zero exit")
	}

	result := runner.engine.handles[0].result
	if result.Stdout != "out\n" {
		t.Errorf("expected stdout %q, got %q", "out\n", result.Stdout)
	}
	if result.Stderr != "err\n" {
		t.Errorf("expected stderr %q, got %q", "err\n", result.Stderr)
	}
	if result.ExitCode != 3 {
		t.Errorf("expected exit code 3, got %d", result.ExitCode)
	}
	if result.TimedOut {
		t.Error("non-zero exit must not be reported as a timeout")
	}
}

func TestConnectorRunner_Handle_GatingExitZeroProceeds(t *testing.T) {
	runner := gatingRunner(t, t.TempDir(), "gate", EventWorkItemVerified, "echo ok")

	if err := runner.Handle(context.Background(), Event{Name: EventWorkItemVerified}); err != nil {
		t.Errorf("gating connector exiting 0 must not block, got %v", err)
	}
	if got := runner.engine.handles[0].result.ExitCode; got != 0 {
		t.Errorf("expected exit code 0, got %d", got)
	}
}

func TestConnectorRunner_Handle_GatingNonZeroBlocksWithStdout(t *testing.T) {
	runner := gatingRunner(t, t.TempDir(), "gate", EventWorkItemVerified, "echo lint failed; exit 1")

	err := runner.Handle(context.Background(), Event{Name: EventWorkItemVerified})

	var ge *GateError
	if !errors.As(err, &ge) {
		t.Fatalf("expected *GateError, got %v", err)
	}
	if ge.Connector != "gate" || ge.Event != EventWorkItemVerified {
		t.Errorf("gate error identifies %s on %s, want gate on %s", ge.Connector, ge.Event, EventWorkItemVerified)
	}
	if ge.Stdout != "lint failed\n" {
		t.Errorf("gate error must carry connector stdout, got %q", ge.Stdout)
	}
	if !strings.Contains(err.Error(), "lint failed") {
		t.Errorf("gate error message must include stdout, got %q", err.Error())
	}
}

func TestConnectorRunner_Handle_GatingTimeoutBlocks(t *testing.T) {
	runner := NewConnectorRunner([]domain.ConnectorConfig{
		{Name: "slow-gate", On: []string{EventWorkItemStarted}, Command: "sleep 5", Mode: domain.ConnectorModeGating, Timeout: "100ms"},
	}, t.TempDir())

	start := time.Now()
	err := runner.Handle(context.Background(), Event{Name: EventWorkItemStarted})

	var ge *GateError
	if !errors.As(err, &ge) {
		t.Fatalf("timed-out gating connector must block like a non-zero exit, got %v", err)
	}
	if elapsed := time.Since(start); elapsed >= 5*time.Second {
		t.Errorf("command was not killed at timeout; took %v", elapsed)
	}
	result := runner.engine.handles[0].result
	if !result.TimedOut {
		t.Error("expected TimedOut to be true")
	}
	if result.Err == nil {
		t.Error("timed-out handle outcome must be recorded as failed")
	}
}

func TestConnectorRunner_Handle_NotifyFailureDoesNotBlock(t *testing.T) {
	runner := NewConnectorRunner([]domain.ConnectorConfig{
		{Name: "notify", On: []string{EventWorkItemVerified}, Command: "exit 1"},
	}, t.TempDir())

	if err := runner.Handle(context.Background(), Event{Name: EventWorkItemVerified}); err != nil {
		t.Errorf("notify connector failure must not block, got %v", err)
	}

	runner.engine.Drain()
	h := runner.engine.handles[0]
	if h.state != handleDrained {
		t.Errorf("notify handle must drain, got %s", h.state)
	}
	if h.result.Err == nil {
		t.Error("notify failure must still be recorded in the handle outcome")
	}
}

func TestConnectorRunner_Handle_NotifyTimeoutDoesNotBlock(t *testing.T) {
	runner := NewConnectorRunner([]domain.ConnectorConfig{
		{Name: "slow-notify", On: []string{EventWorkItemVerified}, Command: "sleep 5", Timeout: "100ms"},
	}, t.TempDir())

	if err := runner.Handle(context.Background(), Event{Name: EventWorkItemVerified}); err != nil {
		t.Errorf("notify connector timeout must not block, got %v", err)
	}

	start := time.Now()
	runner.engine.Drain()
	if elapsed := time.Since(start); elapsed >= 5*time.Second {
		t.Errorf("timed-out notify command was not killed; drain took %v", elapsed)
	}
	result := runner.engine.handles[0].result
	if !result.TimedOut || result.Err == nil {
		t.Errorf("timed-out handle outcome must be recorded as failed, got %+v", result)
	}
}

func TestConnectorRunner_Handle_NotifyRunsInBackground(t *testing.T) {
	dir := t.TempDir()
	runner := NewConnectorRunner([]domain.ConnectorConfig{
		{Name: "slow-notify", On: []string{EventWorkItemStarted}, Command: "sleep 0.2; echo done > done.txt"},
	}, dir)

	start := time.Now()
	if err := runner.Handle(context.Background(), Event{Name: EventWorkItemStarted}); err != nil {
		t.Fatalf("notify connector must not block, got %v", err)
	}
	if elapsed := time.Since(start); elapsed > 150*time.Millisecond {
		t.Errorf("launch-only connector must not block the event, took %v", elapsed)
	}

	runner.engine.Drain()
	if _, err := os.Stat(filepath.Join(dir, "done.txt")); err != nil {
		t.Errorf("notify command must run to completion in the background: %v", err)
	}
}

func TestConnectorRunner_Handle_TerminalEventDrainsRunningHandles(t *testing.T) {
	dir := t.TempDir()
	runner := NewConnectorRunner([]domain.ConnectorConfig{
		{Name: "final-notify", On: []string{EventExecutionCompleted}, Command: "sleep 0.2; echo ran > ran.txt"},
	}, dir)

	if err := runner.Handle(context.Background(), Event{Name: EventExecutionCompleted}); err != nil {
		t.Fatalf("notify connector must not block, got %v", err)
	}

	// Handle drains on terminal events, so the command finished before return.
	if _, err := os.Stat(filepath.Join(dir, "ran.txt")); err != nil {
		t.Errorf("terminal events must drain running handles before the loop exits: %v", err)
	}
	if got := runner.engine.handles[0].state; got != handleDrained {
		t.Errorf("handle state = %s, want %s", got, handleDrained)
	}
}

func TestConnectorRunner_Handle_BlockedGateStillRunsLaterConnectors(t *testing.T) {
	dir := t.TempDir()
	runner := NewConnectorRunner([]domain.ConnectorConfig{
		{Name: "gate", On: []string{EventWorkItemVerified}, Command: "exit 1", Mode: domain.ConnectorModeGating},
		{Name: "notify", On: []string{EventWorkItemVerified}, Command: "echo ran >> after.txt"},
	}, dir)

	err := runner.Handle(context.Background(), Event{Name: EventWorkItemVerified})

	if err == nil {
		t.Fatal("expected gate error")
	}
	runner.engine.Drain()
	if _, statErr := os.Stat(filepath.Join(dir, "after.txt")); statErr != nil {
		t.Errorf("connectors after a blocked gate must still run: %v", statErr)
	}
}

func TestEngine_CancelSendsSIGTERMToProcessGroup(t *testing.T) {
	cc := domain.ConnectorConfig{Name: "trap-term", Command: `trap 'echo terminated; exit 0' TERM; sleep 5`}
	sub := Subscription{
		Name:   "trap-term",
		Launch: EventWorkItemStarted,
		Cancel: []string{EventWorkItemVerified},
		Action: commandAction(cc, t.TempDir()),
	}
	en := NewEngine([]Subscription{sub})

	if err := en.Emit(context.Background(), Event{Name: EventWorkItemStarted}); err != nil {
		t.Fatalf("launch emit failed: %v", err)
	}
	// Give the shell a moment to install the trap before signalling.
	time.Sleep(50 * time.Millisecond)

	start := time.Now()
	if err := en.Emit(context.Background(), Event{Name: EventWorkItemVerified}); err != nil {
		t.Fatalf("cancel emit failed: %v", err)
	}

	if elapsed := time.Since(start); elapsed >= 5*time.Second {
		t.Errorf("cancel did not terminate the process group; took %v", elapsed)
	}
	h := en.handles[0]
	if h.state != handleCancelled {
		t.Errorf("handle state = %s, want %s", h.state, handleCancelled)
	}
	if !strings.Contains(h.result.Stdout, "terminated") {
		t.Errorf("expected the command's TERM trap to run, stdout = %q", h.result.Stdout)
	}
}

func TestEngine_CancelEscalatesToSIGKILLAfterGracePeriod(t *testing.T) {
	oldGrace := cancelGracePeriod
	cancelGracePeriod = 100 * time.Millisecond
	defer func() { cancelGracePeriod = oldGrace }()

	// The shell ignores SIGTERM and respawns children, so only the SIGKILL
	// escalation to the process group can end it.
	cc := domain.ConnectorConfig{Name: "term-proof", Command: `trap '' TERM; while :; do sleep 0.05; done`}
	sub := Subscription{
		Name:   "term-proof",
		Launch: EventWorkItemStarted,
		Cancel: []string{EventWorkItemVerified},
		Action: commandAction(cc, t.TempDir()),
	}
	en := NewEngine([]Subscription{sub})

	if err := en.Emit(context.Background(), Event{Name: EventWorkItemStarted}); err != nil {
		t.Fatalf("launch emit failed: %v", err)
	}
	time.Sleep(50 * time.Millisecond)

	start := time.Now()
	if err := en.Emit(context.Background(), Event{Name: EventWorkItemVerified}); err != nil {
		t.Fatalf("cancel emit failed: %v", err)
	}

	if elapsed := time.Since(start); elapsed >= 3*time.Second {
		t.Errorf("SIGKILL escalation did not end a TERM-proof process group; took %v", elapsed)
	}
	h := en.handles[0]
	if h.state != handleCancelled {
		t.Errorf("handle state = %s, want %s", h.state, handleCancelled)
	}
	if h.result.Err == nil {
		t.Error("a killed run must record a failure outcome")
	}
}

func TestResolutionLedger_JoinRecordsNameTypeExitCodeAndOutput(t *testing.T) {
	runner := gatingRunner(t, t.TempDir(), "flaky", EventWorkItemVerified, "echo posting failed; echo hook 500 >&2; exit 3")

	out := captureStdout(t, func() {
		if err := runner.Handle(context.Background(), Event{Name: EventWorkItemVerified}); err == nil {
			t.Fatal("expected failure")
		}
	})

	for _, want := range []string{"flaky", handleJoined, "exit 3", "posting failed", "hook 500"} {
		if !strings.Contains(out, want) {
			t.Errorf("join ledger must include %q, got:\n%s", want, out)
		}
	}
}

func TestResolutionLedger_DrainRecordsTimeoutResolution(t *testing.T) {
	runner := NewConnectorRunner([]domain.ConnectorConfig{
		{Name: "slow-notify", On: []string{EventExecutionCompleted}, Command: "sleep 5", Timeout: "100ms"},
	}, t.TempDir())

	out := captureStdout(t, func() {
		if err := runner.Handle(context.Background(), Event{Name: EventExecutionCompleted}); err != nil {
			t.Fatalf("notify must not block, got %v", err)
		}
	})

	for _, want := range []string{"slow-notify", handleDrained, "timed out"} {
		if !strings.Contains(out, want) {
			t.Errorf("drain ledger must include %q, got:\n%s", want, out)
		}
	}
}

func TestResolutionLedger_CancelRecordsCancelledResolution(t *testing.T) {
	sub := Subscription{
		Name:   "speculative",
		Launch: EventWorkItemStarted,
		Cancel: []string{EventWorkItemVerified},
		Action: commandAction(domain.ConnectorConfig{Name: "speculative", Command: "sleep 5"}, t.TempDir()),
	}
	en := NewEngine([]Subscription{sub})

	if err := en.Emit(context.Background(), Event{Name: EventWorkItemStarted}); err != nil {
		t.Fatalf("launch emit failed: %v", err)
	}

	out := captureStdout(t, func() {
		if err := en.Emit(context.Background(), Event{Name: EventWorkItemVerified}); err != nil {
			t.Fatalf("cancel emit failed: %v", err)
		}
	})

	if !strings.Contains(out, "speculative") || !strings.Contains(out, handleCancelled) {
		t.Errorf("cancel ledger must name the connector and its resolution, got:\n%s", out)
	}
}
