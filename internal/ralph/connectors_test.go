package ralph

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/leightonvanrooijen/utopia/internal/domain"
)

func TestConnectorRunner_RunsSubscribedConnectorsSequentiallyInConfigOrder(t *testing.T) {
	dir := t.TempDir()
	runner := NewConnectorRunner([]domain.ConnectorConfig{
		{Name: "first", On: []string{EventWorkItemVerified}, Command: "echo first >> order.txt"},
		{Name: "skipped", On: []string{EventExecutionFailed}, Command: "echo skipped >> order.txt"},
		{Name: "second", On: []string{EventWorkItemVerified}, Command: "echo second >> order.txt"},
	}, dir)

	results := runner.Run(context.Background(), Event{Name: EventWorkItemVerified})

	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if results[0].Name != "first" || results[1].Name != "second" {
		t.Errorf("expected results in config order [first second], got [%s %s]", results[0].Name, results[1].Name)
	}

	content, err := os.ReadFile(filepath.Join(dir, "order.txt"))
	if err != nil {
		t.Fatalf("failed to read order file: %v", err)
	}
	if got := string(content); got != "first\nsecond\n" {
		t.Errorf("expected sequential execution in config order, got %q", got)
	}
}

func TestConnectorRunner_UnsubscribedEventRunsNothing(t *testing.T) {
	runner := NewConnectorRunner([]domain.ConnectorConfig{
		{Name: "notify", On: []string{EventExecutionCompleted}, Command: "echo hi"},
	}, t.TempDir())

	results := runner.Run(context.Background(), Event{Name: EventWorkItemStarted})

	if len(results) != 0 {
		t.Errorf("expected no results for unsubscribed event, got %d", len(results))
	}
}

func TestConnectorRunner_PayloadDeliveredAsJSONOnStdin(t *testing.T) {
	runner := NewConnectorRunner([]domain.ConnectorConfig{
		{Name: "echo-stdin", On: []string{EventWorkItemCommitted}, Command: "cat"},
	}, t.TempDir())

	payload := EventPayload{
		CRID:           "cr-42",
		CRTitle:        "Add connectors",
		SpecID:         "cr-42/phase-1",
		WorkItemID:     "wi-1",
		IterationCount: 3,
		CommitSHA:      "abc123",
	}
	results := runner.Run(context.Background(), Event{Name: EventWorkItemCommitted, Payload: payload})

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Err != nil {
		t.Fatalf("connector failed: %v", results[0].Err)
	}

	var got EventPayload
	if err := json.Unmarshal([]byte(results[0].Stdout), &got); err != nil {
		t.Fatalf("stdout is not valid JSON payload: %v (stdout=%q)", err, results[0].Stdout)
	}
	if got != payload {
		t.Errorf("payload mismatch: got %+v, want %+v", got, payload)
	}
}

func TestConnectorRunner_EnvironmentVariablesSet(t *testing.T) {
	dir := t.TempDir()
	runner := NewConnectorRunner([]domain.ConnectorConfig{
		{
			Name:    "env-check",
			On:      []string{EventExecutionStarted},
			Command: `echo "$UTOPIA_EVENT|$UTOPIA_CR_ID|$UTOPIA_PROJECT_DIR"`,
		},
	}, dir)

	results := runner.Run(context.Background(), Event{
		Name:    EventExecutionStarted,
		Payload: EventPayload{CRID: "cr-7"},
	})

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	want := EventExecutionStarted + "|cr-7|" + dir + "\n"
	if results[0].Stdout != want {
		t.Errorf("expected env vars %q, got %q", want, results[0].Stdout)
	}
}

func TestConnectorRunner_RunsInProjectDirectory(t *testing.T) {
	dir := t.TempDir()
	runner := NewConnectorRunner([]domain.ConnectorConfig{
		{Name: "pwd", On: []string{EventExecutionStarted}, Command: "pwd"},
	}, dir)

	results := runner.Run(context.Background(), Event{Name: EventExecutionStarted})

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	// Resolve symlinks: on macOS t.TempDir() is under /var which links to /private/var
	wantDir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatalf("failed to resolve temp dir: %v", err)
	}
	gotDir, err := filepath.EvalSymlinks(strings.TrimSpace(results[0].Stdout))
	if err != nil {
		t.Fatalf("failed to resolve reported working dir: %v", err)
	}
	if gotDir != wantDir {
		t.Errorf("expected working directory %q, got %q", wantDir, gotDir)
	}
}

func TestConnectorRunner_CapturesStdoutAndStderr(t *testing.T) {
	runner := NewConnectorRunner([]domain.ConnectorConfig{
		{Name: "both", On: []string{EventPhaseCompleted}, Command: "echo out; echo err >&2"},
	}, t.TempDir())

	results := runner.Run(context.Background(), Event{Name: EventPhaseCompleted})

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Stdout != "out\n" {
		t.Errorf("expected stdout %q, got %q", "out\n", results[0].Stdout)
	}
	if results[0].Stderr != "err\n" {
		t.Errorf("expected stderr %q, got %q", "err\n", results[0].Stderr)
	}
}

func TestConnectorRunner_NonZeroExitReportsError(t *testing.T) {
	runner := NewConnectorRunner([]domain.ConnectorConfig{
		{Name: "failing", On: []string{EventWorkItemVerified}, Command: "echo output; exit 3"},
	}, t.TempDir())

	results := runner.Run(context.Background(), Event{Name: EventWorkItemVerified})

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Err == nil {
		t.Fatal("expected error for non-zero exit, got nil")
	}
	if results[0].TimedOut {
		t.Error("non-zero exit must not be reported as a timeout")
	}
	if results[0].Stdout != "output\n" {
		t.Errorf("output must still be captured on failure, got %q", results[0].Stdout)
	}
}

func TestConnectorRunner_Handle_GatingExitZeroProceeds(t *testing.T) {
	runner := NewConnectorRunner([]domain.ConnectorConfig{
		{Name: "gate", On: []string{EventWorkItemVerified}, Command: "echo ok", Mode: domain.ConnectorModeGating},
	}, t.TempDir())

	if err := runner.Handle(context.Background(), Event{Name: EventWorkItemVerified}); err != nil {
		t.Errorf("gating connector exiting 0 must not block, got %v", err)
	}
}

func TestConnectorRunner_Handle_GatingNonZeroBlocksWithStdout(t *testing.T) {
	runner := NewConnectorRunner([]domain.ConnectorConfig{
		{Name: "gate", On: []string{EventWorkItemVerified}, Command: "echo lint failed; exit 1", Mode: domain.ConnectorModeGating},
	}, t.TempDir())

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

	err := runner.Handle(context.Background(), Event{Name: EventWorkItemStarted})

	var ge *GateError
	if !errors.As(err, &ge) {
		t.Fatalf("timed-out gating connector must block like a non-zero exit, got %v", err)
	}
}

func TestConnectorRunner_Handle_NotifyFailureDoesNotBlock(t *testing.T) {
	runner := NewConnectorRunner([]domain.ConnectorConfig{
		{Name: "notify", On: []string{EventWorkItemVerified}, Command: "exit 1"},
	}, t.TempDir())

	if err := runner.Handle(context.Background(), Event{Name: EventWorkItemVerified}); err != nil {
		t.Errorf("notify connector failure must not block, got %v", err)
	}
}

func TestConnectorRunner_Handle_NotifyTimeoutDoesNotBlock(t *testing.T) {
	runner := NewConnectorRunner([]domain.ConnectorConfig{
		{Name: "slow-notify", On: []string{EventWorkItemVerified}, Command: "sleep 5", Timeout: "100ms"},
	}, t.TempDir())

	if err := runner.Handle(context.Background(), Event{Name: EventWorkItemVerified}); err != nil {
		t.Errorf("notify connector timeout must not block, got %v", err)
	}
}

func TestFormatNotifyFailure_IncludesNameExitCodeAndOutput(t *testing.T) {
	runner := NewConnectorRunner([]domain.ConnectorConfig{
		{Name: "flaky", On: []string{EventExecutionCompleted}, Command: "echo posting failed; echo hook 500 >&2; exit 3"},
	}, t.TempDir())

	results := runner.Run(context.Background(), Event{Name: EventExecutionCompleted})
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	msg := formatNotifyFailure(results[0])

	if !strings.Contains(msg, "flaky") {
		t.Errorf("log must include the connector name, got %q", msg)
	}
	if !strings.Contains(msg, "exit status 3") {
		t.Errorf("log must include the exit code, got %q", msg)
	}
	if !strings.Contains(msg, "posting failed") {
		t.Errorf("log must include captured stdout, got %q", msg)
	}
	if !strings.Contains(msg, "hook 500") {
		t.Errorf("log must include captured stderr, got %q", msg)
	}
}

func TestFormatNotifyFailure_TimeoutReported(t *testing.T) {
	runner := NewConnectorRunner([]domain.ConnectorConfig{
		{Name: "slow-notify", On: []string{EventExecutionCompleted}, Command: "sleep 5", Timeout: "100ms"},
	}, t.TempDir())

	results := runner.Run(context.Background(), Event{Name: EventExecutionCompleted})
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	msg := formatNotifyFailure(results[0])

	if !strings.Contains(msg, "slow-notify") || !strings.Contains(msg, "timed out") {
		t.Errorf("timeout log must include the connector name and timeout cause, got %q", msg)
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
	if _, statErr := os.Stat(filepath.Join(dir, "after.txt")); statErr != nil {
		t.Errorf("connectors after a blocked gate must still run: %v", statErr)
	}
}

func TestConnectorRunner_TimeoutKillsCommandAndReportsFailed(t *testing.T) {
	runner := NewConnectorRunner([]domain.ConnectorConfig{
		{Name: "slow", On: []string{EventWorkItemVerified}, Command: "sleep 5", Timeout: "100ms"},
	}, t.TempDir())

	start := time.Now()
	results := runner.Run(context.Background(), Event{Name: EventWorkItemVerified})
	elapsed := time.Since(start)

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if !results[0].TimedOut {
		t.Error("expected TimedOut to be true")
	}
	if results[0].Err == nil {
		t.Fatal("expected error for timed-out connector, got nil")
	}
	if elapsed >= 5*time.Second {
		t.Errorf("command was not killed at timeout; took %v", elapsed)
	}
}
