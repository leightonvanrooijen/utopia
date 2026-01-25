package verification

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
)

const (
	// MaxOutputLength is the maximum characters to retain from output
	MaxOutputLength = 2000
)

// Result holds the outcome of a verification run
type Result struct {
	Passed bool
	Output string // Captured stdout+stderr (truncated to tail if needed)
}

// Runner executes verification commands
type Runner struct {
	workDir string
}

// NewRunner creates a runner that executes commands in the given directory
func NewRunner(workDir string) *Runner {
	return &Runner{workDir: workDir}
}

// Run executes the verification command and returns the result
// Pass/fail is determined by exit code: 0 = pass, non-zero = fail
func (r *Runner) Run(ctx context.Context, command string) (*Result, error) {
	if command == "" {
		return nil, fmt.Errorf("verification command is empty")
	}

	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	cmd.Dir = r.workDir

	var combined bytes.Buffer
	cmd.Stdout = &combined
	cmd.Stderr = &combined

	err := cmd.Run()

	output := combined.String()
	output = truncateTail(output, MaxOutputLength)

	// Exit code 0 = pass, non-zero = fail
	passed := err == nil

	return &Result{
		Passed: passed,
		Output: output,
	}, nil
}

// truncateTail keeps the last maxLen characters of s (preserving most recent output)
func truncateTail(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[len(s)-maxLen:]
}
