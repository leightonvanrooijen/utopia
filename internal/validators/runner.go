package validators

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/leightonvanrooijen/utopia/internal"
	"github.com/leightonvanrooijen/utopia/internal/domain"
)

// PassedToken is the marker validators must include to indicate success
const PassedToken = "<PASSED>"

// Result holds the outcome of a validator run
type Result struct {
	// Passed is true if the validator output contained <PASSED>
	Passed bool
	// Feedback is the validator output (empty if passed, full output if failed)
	Feedback string
}

// Runner executes validators by invoking Claude with read-only tools
type Runner struct {
	workDir string
	cli     *internal.CLI
}

// NewRunner creates a validator runner that operates in the given directory
func NewRunner(workDir string) *Runner {
	return &Runner{
		workDir: workDir,
		cli:     internal.NewCLI().WithVerbose(false),
	}
}

// Run executes a validator against the changed files from the last commit.
// It loads the git diff, expands the validator prompt, invokes Claude,
// and checks the output for the <PASSED> token.
func (r *Runner) Run(ctx context.Context, validator *domain.Validator) (*Result, error) {
	// Get the git diff of changed files from last commit
	changedFiles, err := r.getGitDiff(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get git diff: %w", err)
	}

	// Expand the prompt with changed files
	prompt := validator.ExpandPrompt(changedFiles)

	// Configure CLI with validator's allowed tools
	cli := r.cli.WithAllowedTools(validator.GetAllowedTools())

	// Invoke Claude with the constructed prompt
	output, err := cli.Prompt(ctx, prompt)
	if err != nil {
		return nil, fmt.Errorf("claude invocation failed: %w", err)
	}

	// Check for <PASSED> token in output
	passed := strings.Contains(output, PassedToken)

	result := &Result{
		Passed: passed,
	}

	// If failed, include the full output as feedback
	if !passed {
		result.Feedback = output
	}

	return result, nil
}

// getGitDiff returns the diff of changes from the last commit
func (r *Runner) getGitDiff(ctx context.Context) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "diff", "HEAD~1")
	cmd.Dir = r.workDir

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git diff failed: %w (%s)", err, stderr.String())
	}

	return stdout.String(), nil
}
