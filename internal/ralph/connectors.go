package ralph

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"github.com/leightonvanrooijen/utopia/internal/domain"
	"github.com/leightonvanrooijen/utopia/internal/validators"
)

// ConnectorResult holds the outcome of a single connector run.
type ConnectorResult struct {
	// Name is the connector name from config
	Name string
	// Event is the lifecycle event that launched the run
	Event string
	// ExitCode is the command's exit code; -1 if it was killed by a signal
	// or never started
	ExitCode int
	// Stdout is the captured standard output of the command
	Stdout string
	// Stderr is the captured standard error of the command
	Stderr string
	// TimedOut is true if the command was killed for exceeding its timeout
	TimedOut bool
	// Err is non-nil if the command failed: non-zero exit, timeout, or failure to start
	Err error
	// Aggregate is the validators' aggregated outcome when this result came from
	// the validators action, and nil for a connector command. It carries the
	// failure classification the escalation routing reads, which Stdout - prose for
	// the next iteration - cannot express.
	Aggregate *validators.AggregateResult
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
	// Aggregate is the validators' aggregated outcome when a failing validator
	// blocked, and nil when a gating connector did. Escalation routing reads the
	// failure class from it; a nil aggregate is routed as mechanical, because a
	// connector never claimed anything about intent.
	Aggregate *validators.AggregateResult
}

func (e *GateError) Error() string {
	msg := fmt.Sprintf("gating connector %s blocked %s", e.Connector, e.Event)
	if out := strings.TrimSpace(e.Stdout); out != "" {
		msg += ": " + out
	}
	return msg
}

// ConnectorRunner executes configured connector commands when their
// subscribed lifecycle events fire. It is wired as a dispatcher subscriber
// and delegates to the subscription engine: configs compile to subscriptions
// once at construction, and each dispatched event becomes an engine emit.
type ConnectorRunner struct {
	engine *Engine
}

// NewConnectorRunner creates a runner for the given connector configs.
// Commands execute with projectDir as their working directory.
func NewConnectorRunner(connectors []domain.ConnectorConfig, projectDir string) *ConnectorRunner {
	return &ConnectorRunner{engine: NewEngine(CompileConnectors(connectors, projectDir))}
}

// Handle emits the event through the engine. Gating connectors (launch and
// join on the same event) block by returning a *GateError; notify connectors
// (launch only) run in the background and have failures logged as warnings.
// After a terminal event the loop is over, so remaining handles are drained
// rather than orphaned when the process exits.
func (r *ConnectorRunner) Handle(ctx context.Context, e Event) error {
	err := r.engine.Emit(ctx, e)
	if e.Name == EventExecutionCompleted || e.Name == EventExecutionFailed {
		r.engine.Drain()
	}
	return err
}

// CompileConnectors compiles connector configs to engine subscriptions.
// It is a pure mapping: each (connector, subscribed event) pair becomes one
// subscription, and mode is erased into subscription shape - gating is
// launch and join on the same event, notify is launch only. New connector
// patterns are new compile cases here, not new engine behavior.
func CompileConnectors(connectors []domain.ConnectorConfig, projectDir string) []Subscription {
	var subs []Subscription
	for _, cc := range connectors {
		for _, event := range cc.On {
			sub := Subscription{
				Name:   cc.Name,
				Launch: event,
				Action: commandAction(cc, projectDir),
			}
			if cc.GetMode() == domain.ConnectorModeGating {
				sub.Join = event
			}
			// Invalid timeouts are rejected by config validation, so parse
			// errors here are unreachable in practice; run without a timeout
			// if one slips through.
			if cc.Timeout != "" {
				if d, err := time.ParseDuration(cc.Timeout); err == nil {
					sub.Timeout = d
				}
			}
			subs = append(subs, sub)
		}
	}
	return subs
}

// cancelGracePeriod is how long a cancelled command gets to exit after
// SIGTERM before its process group is SIGKILLed. A variable so tests can
// shorten it.
var cancelGracePeriod = 5 * time.Second

// commandAction builds the engine action that runs the connector's shell
// command. The command receives the event payload as JSON on stdin plus
// UTOPIA_EVENT, UTOPIA_CR_ID, and UTOPIA_PROJECT_DIR environment variables,
// and runs in its own process group so cancellation can signal the whole
// tree: SIGTERM first, then SIGKILL after the grace period.
func commandAction(cc domain.ConnectorConfig, projectDir string) Action {
	// Snapshot at compile time so in-flight escalation goroutines never read
	// the package variable concurrently with a test overriding it.
	grace := cancelGracePeriod
	return func(ctx context.Context, e Event) func() ConnectorResult {
		result := ConnectorResult{Name: cc.Name, Event: e.Name, ExitCode: -1}

		payload, err := json.Marshal(e.Payload)
		if err != nil {
			result.Err = fmt.Errorf("connector %s: failed to encode event payload: %w", cc.Name, err)
			return func() ConnectorResult { return result }
		}

		cmd := exec.CommandContext(ctx, "sh", "-c", cc.Command)
		cmd.Dir = projectDir
		cmd.Stdin = bytes.NewReader(payload)
		cmd.Env = append(os.Environ(),
			"UTOPIA_EVENT="+e.Name,
			"UTOPIA_CR_ID="+e.Payload.CRID,
			"UTOPIA_PROJECT_DIR="+projectDir,
		)

		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr

		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		exited := make(chan struct{})
		cmd.Cancel = func() error {
			// Negative pid signals the whole process group.
			pgid := cmd.Process.Pid
			err := syscall.Kill(-pgid, syscall.SIGTERM)
			go func() {
				select {
				case <-exited:
				case <-time.After(grace):
					_ = syscall.Kill(-pgid, syscall.SIGKILL)
				}
			}()
			return err
		}
		// Backstop: force Wait to return even if an orphaned grandchild
		// holds the output pipes open past the SIGKILL.
		cmd.WaitDelay = grace + time.Second

		if err := cmd.Start(); err != nil {
			result.Err = fmt.Errorf("connector %s: %w", cc.Name, err)
			return func() ConnectorResult { return result }
		}

		return func() ConnectorResult {
			runErr := cmd.Wait()
			close(exited)

			result.Stdout = stdout.String()
			result.Stderr = stderr.String()
			result.ExitCode = cmd.ProcessState.ExitCode()

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
	}
}
