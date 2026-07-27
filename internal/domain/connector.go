package domain

import (
	"fmt"
	"time"
)

// Lifecycle event names emitted by the execution loop, in loop order.
// These are the canonical names connectors subscribe to via the "on" field.
const (
	EventExecutionStarted   = "execution-started"
	EventWorkItemStarted    = "workitem-started"
	EventWorkItemVerified   = "workitem-verified"
	EventWorkItemCommitted  = "workitem-committed"
	EventPhaseVerified      = "phase-verified"
	EventPhaseCompleted     = "phase-completed"
	EventExecutionCompleted = "execution-completed"
	EventExecutionFailed    = "execution-failed"
)

// LifecycleEventNames returns all lifecycle event names in loop order.
func LifecycleEventNames() []string {
	return []string{
		EventExecutionStarted,
		EventWorkItemStarted,
		EventWorkItemVerified,
		EventWorkItemCommitted,
		EventPhaseVerified,
		EventPhaseCompleted,
		EventExecutionCompleted,
		EventExecutionFailed,
	}
}

// Connector modes control how connector failures affect the execution loop.
const (
	// ConnectorModeGating blocks loop progression on non-zero exit.
	ConnectorModeGating = "gating"
	// ConnectorModeNotify is fire-and-forget; failures are logged only.
	ConnectorModeNotify = "notify"
)

// gatingEvents lists events where a gating connector can block progression.
// Post-commit and terminal events (workitem-committed, phase-completed,
// execution-completed, execution-failed) fire after the outcome they describe
// is already final, so gating has no semantics there.
var gatingEvents = map[string]bool{
	EventExecutionStarted: true,
	EventWorkItemStarted:  true,
	EventWorkItemVerified: true,
	EventPhaseVerified:    true,
}

// ConnectorConfig registers an external command that runs when subscribed
// lifecycle events fire.
//
// Example:
//
//	connectors:
//	  - name: slack-notify
//	    on: [execution-completed, execution-failed]
//	    command: ./scripts/notify.sh
//	  - name: lint-gate
//	    on: [workitem-verified]
//	    command: npm run lint
//	    mode: gating
//	    timeout: 30s
type ConnectorConfig struct {
	// Name identifies the connector in logs and error messages (required)
	Name string `yaml:"name"`
	// On lists the lifecycle event names this connector subscribes to (required)
	On []string `yaml:"on"`
	// Command is the shell command to execute when a subscribed event fires (required)
	Command string `yaml:"command"`
	// Mode is "gating" or "notify". Defaults to notify when omitted.
	Mode string `yaml:"mode,omitempty"`
	// Timeout is an optional duration string (e.g. "30s") limiting command runtime
	Timeout string `yaml:"timeout,omitempty"`
}

// GetMode returns the connector mode, defaulting to notify when unset.
func (cc *ConnectorConfig) GetMode() string {
	if cc.Mode == "" {
		return ConnectorModeNotify
	}
	return cc.Mode
}

// ValidateConnectors validates all connector entries in a config.
// Returns nil if the list is empty or all entries are valid.
// Returns an error describing all problems found.
func ValidateConnectors(connectors []ConnectorConfig) error {
	var issues []string

	for i, cc := range connectors {
		// Name gives error messages a stable handle; fall back to the index.
		label := cc.Name
		if label == "" {
			label = fmt.Sprintf("connectors[%d]", i)
			issues = append(issues, fmt.Sprintf("%s: name is required", label))
		}

		if cc.Command == "" {
			issues = append(issues, fmt.Sprintf("%s: command is required", label))
		}

		if len(cc.On) == 0 {
			issues = append(issues, fmt.Sprintf("%s: on must list at least one lifecycle event", label))
		}

		mode := cc.GetMode()
		if mode != ConnectorModeGating && mode != ConnectorModeNotify {
			issues = append(issues, fmt.Sprintf("%s: invalid mode %q (valid options: gating, notify)", label, cc.Mode))
		}

		for _, event := range cc.On {
			if !isLifecycleEvent(event) {
				issues = append(issues, fmt.Sprintf("%s: unknown event %q in on", label, event))
				continue
			}
			if mode == ConnectorModeGating && !gatingEvents[event] {
				issues = append(issues, fmt.Sprintf("%s: mode gating is not supported on event %q", label, event))
			}
		}

		if cc.Timeout != "" {
			if _, err := time.ParseDuration(cc.Timeout); err != nil {
				issues = append(issues, fmt.Sprintf("%s: invalid timeout %q (expected a duration like \"30s\")", label, cc.Timeout))
			}
		}
	}

	if len(issues) == 0 {
		return nil
	}

	return &InvalidConnectorConfigError{Issues: issues}
}

// isLifecycleEvent reports whether name is a known lifecycle event.
func isLifecycleEvent(name string) bool {
	for _, event := range LifecycleEventNames() {
		if event == name {
			return true
		}
	}
	return false
}

// InvalidConnectorConfigError indicates one or more invalid connector entries in config.
type InvalidConnectorConfigError struct {
	Issues []string
}

func (e *InvalidConnectorConfigError) Error() string {
	return fmt.Sprintf("invalid connector configuration: %s", joinWithComma(e.Issues))
}

// Is allows errors.Is to match any InvalidConnectorConfigError.
func (e *InvalidConnectorConfigError) Is(target error) bool {
	_, ok := target.(*InvalidConnectorConfigError)
	return ok
}
