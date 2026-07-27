package ralph

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/leightonvanrooijen/utopia/internal/domain"
)

// ConnectorResult holds the outcome of a single connector execution.
type ConnectorResult struct {
	// Name is the connector name from config
	Name string
	// Event is the lifecycle event that triggered the run
	Event string
	// Mode is the connector mode (gating or notify) from config
	Mode string
	// Stdout is the captured standard output of the command
	Stdout string
	// Stderr is the captured standard error of the command
	Stderr string
	// TimedOut is true if the command was killed for exceeding its timeout
	TimedOut bool
	// Err is non-nil if the command failed: non-zero exit, timeout, or failure to start
	Err error
}

// GateError reports a gating connector that blocked loop progression.
// Stdout carries the connector's output so gate sites can surface it in
// abort errors or inject it as feedback into the next iteration.
type GateError struct {
	// Connector is the name of the gating connector that blocked
	Connector string
	// Event is the lifecycle event where the gate fired
	Event string
	// Stdout is the connector's captured standard output
	Stdout string
}

func (e *GateError) Error() string {
	msg := fmt.Sprintf("gating connector %s blocked %s", e.Connector, e.Event)
	if out := strings.TrimSpace(e.Stdout); out != "" {
		msg += ": " + out
	}
	return msg
}

// ConnectorRunner executes configured connector commands when their
// subscribed lifecycle events fire. It is wired as a dispatcher subscriber.
type ConnectorRunner struct {
	connectors []domain.ConnectorConfig
	projectDir string
}

// NewConnectorRunner creates a runner for the given connector configs.
// Commands execute with projectDir as their working directory.
func NewConnectorRunner(connectors []domain.ConnectorConfig, projectDir string) *ConnectorRunner {
	return &ConnectorRunner{connectors: connectors, projectDir: projectDir}
}

// Run executes all connectors subscribed to the event sequentially in config
// order and returns one result per connector run. Connectors not subscribed
// to the event are skipped.
func (r *ConnectorRunner) Run(ctx context.Context, e Event) []ConnectorResult {
	var results []ConnectorResult
	for _, cc := range r.connectors {
		if !subscribesTo(cc, e.Name) {
			continue
		}
		results = append(results, r.runConnector(ctx, cc, e))
	}
	return results
}

// Handle runs all connectors subscribed to the event and applies mode
// semantics: notify failures are logged as warnings and never block, while
// the first gating failure is returned as a *GateError. Timeouts are
// failures like any other. All subscribed connectors run even after a gate
// blocks, so notify side effects are not skipped.
func (r *ConnectorRunner) Handle(ctx context.Context, e Event) error {
	var gateErr error
	for _, cr := range r.Run(ctx, e) {
		if cr.Err == nil {
			continue
		}
		if cr.Mode == domain.ConnectorModeGating {
			fmt.Printf("  gating connector %s blocked %s: %v\n", cr.Name, cr.Event, cr.Err)
			if gateErr == nil {
				gateErr = &GateError{Connector: cr.Name, Event: cr.Event, Stdout: cr.Stdout}
			}
			continue
		}
		fmt.Printf("  warning: connector %s failed on %s: %v\n", cr.Name, cr.Event, cr.Err)
	}
	return gateErr
}

// subscribesTo reports whether the connector subscribes to the named event.
func subscribesTo(cc domain.ConnectorConfig, eventName string) bool {
	for _, name := range cc.On {
		if name == eventName {
			return true
		}
	}
	return false
}

// runConnector executes a single connector command. The command receives the
// event payload as JSON on stdin plus UTOPIA_EVENT, UTOPIA_CR_ID, and
// UTOPIA_PROJECT_DIR environment variables. A command exceeding its timeout
// is killed and reported as failed.
func (r *ConnectorRunner) runConnector(ctx context.Context, cc domain.ConnectorConfig, e Event) ConnectorResult {
	result := ConnectorResult{Name: cc.Name, Event: e.Name, Mode: cc.GetMode()}

	// Invalid timeouts are rejected by config validation, so parse errors
	// here are unreachable in practice; run without a timeout if one slips through.
	if cc.Timeout != "" {
		if d, err := time.ParseDuration(cc.Timeout); err == nil {
			var cancel context.CancelFunc
			ctx, cancel = context.WithTimeout(ctx, d)
			defer cancel()
		}
	}

	payload, err := json.Marshal(e.Payload)
	if err != nil {
		result.Err = fmt.Errorf("connector %s: failed to encode event payload: %w", cc.Name, err)
		return result
	}

	cmd := exec.CommandContext(ctx, "sh", "-c", cc.Command)
	cmd.Dir = r.projectDir
	cmd.Stdin = bytes.NewReader(payload)
	cmd.Env = append(os.Environ(),
		"UTOPIA_EVENT="+e.Name,
		"UTOPIA_CR_ID="+e.Payload.CRID,
		"UTOPIA_PROJECT_DIR="+r.projectDir,
	)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	runErr := cmd.Run()

	result.Stdout = stdout.String()
	result.Stderr = stderr.String()

	if ctx.Err() == context.DeadlineExceeded {
		result.TimedOut = true
		result.Err = fmt.Errorf("connector %s: timed out after %s", cc.Name, cc.Timeout)
		return result
	}

	if runErr != nil {
		result.Err = fmt.Errorf("connector %s: %w", cc.Name, runErr)
	}

	return result
}
