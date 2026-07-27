package ralph

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"time"

	"github.com/leightonvanrooijen/utopia/internal/domain"
)

// ConnectorResult holds the outcome of a single connector execution.
type ConnectorResult struct {
	// Name is the connector name from config
	Name string
	// Event is the lifecycle event that triggered the run
	Event string
	// Stdout is the captured standard output of the command
	Stdout string
	// Stderr is the captured standard error of the command
	Stderr string
	// TimedOut is true if the command was killed for exceeding its timeout
	TimedOut bool
	// Err is non-nil if the command failed: non-zero exit, timeout, or failure to start
	Err error
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
	result := ConnectorResult{Name: cc.Name, Event: e.Name}

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
