package validators

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"sync"

	"github.com/leightonvanrooijen/utopia/internal"
	"github.com/leightonvanrooijen/utopia/internal/domain"
	"golang.org/x/sync/errgroup"
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

// ValidatorResult pairs a validator ID with its execution result
type ValidatorResult struct {
	// ID is the validator's unique identifier
	ID string
	// Result holds the pass/fail outcome and feedback
	Result *Result
	// Err captures any error during execution (nil if successful)
	Err error
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

// RunAll executes multiple validators concurrently for the given run trigger.
// It computes the git diff once and shares it across all validators.
// Each validator runs in its own goroutine with independent timeouts.
// Context cancellation (e.g., Ctrl+C) stops all running validators.
func (r *Runner) RunAll(ctx context.Context, validators []*domain.Validator, trigger domain.RunTrigger) []ValidatorResult {
	// Filter validators matching the trigger
	var matching []*domain.Validator
	for _, v := range validators {
		if v.GetRun() == trigger {
			matching = append(matching, v)
		}
	}

	if len(matching) == 0 {
		return nil
	}

	// Compute git diff once, shared across all validators
	changedFiles, diffErr := r.getGitDiff(ctx)

	var (
		results []ValidatorResult
		mu      sync.Mutex
	)

	// Use errgroup for structured concurrency with context cancellation
	g, gctx := errgroup.WithContext(ctx)

	for _, validator := range matching {
		v := validator // capture for goroutine
		g.Go(func() error {
			var vr ValidatorResult
			vr.ID = v.ID

			// If diff failed, record error for this validator
			if diffErr != nil {
				vr.Err = fmt.Errorf("failed to get git diff: %w", diffErr)
				mu.Lock()
				results = append(results, vr)
				mu.Unlock()
				return nil // don't fail the group, collect the error
			}

			// Run the validator
			result, err := r.runWithDiff(gctx, v, changedFiles)
			vr.Result = result
			vr.Err = err

			mu.Lock()
			results = append(results, vr)
			mu.Unlock()

			return nil // don't fail the group, collect individual results
		})
	}

	// Wait for all validators to complete
	_ = g.Wait() // errors are captured in ValidatorResult.Err

	return results
}

// runWithDiff executes a single validator with a pre-computed git diff.
// This is the internal execution method used by RunAll.
func (r *Runner) runWithDiff(ctx context.Context, validator *domain.Validator, changedFiles string) (*Result, error) {
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
