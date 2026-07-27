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
	workDir     string
	cli         *internal.CLI
	modelConfig *domain.ModelConfig
}

// NewRunner creates a validator runner that operates in the given directory.
// Use WithModelConfig to configure model fallback for validators.
func NewRunner(workDir string) *Runner {
	return &Runner{
		workDir: workDir,
		cli:     internal.NewCLI().WithVerbose(false),
	}
}

// WithModelConfig sets the model configuration for resolving per-validator models.
// When a validator runs, the model is resolved in order:
// 1. Validator's specific model override (from config.yaml validators list)
// 2. models.validators from config
// 3. models.default from config
// 4. "sonnet" as the implicit default
func (r *Runner) WithModelConfig(mc *domain.ModelConfig) *Runner {
	return &Runner{
		workDir:     r.workDir,
		cli:         r.cli,
		modelConfig: mc,
	}
}

// resolveModel determines which model to use for a validator.
// Priority: validator override > models.validators > models.default > "sonnet"
func (r *Runner) resolveModel(validator *domain.Validator) string {
	// First check validator's specific override
	if model := validator.GetModel(); model != "" {
		return model
	}
	// Fall back to models.validators or models.default via ModelForCommand
	return r.modelConfig.ModelForCommand("validators")
}

// Run executes a validator against the current work item's uncommitted changes.
// It loads the git diff, expands the validator prompt, invokes Claude,
// and checks the output for the <PASSED> token.
// The model used is resolved via: validator override > models.validators > models.default.
func (r *Runner) Run(ctx context.Context, validator *domain.Validator) (*Result, error) {
	// Get the git diff of the current work item's uncommitted changes
	changedFiles, err := r.getGitDiff(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get git diff: %w", err)
	}

	// Expand the prompt with changed files
	prompt := validator.ExpandPrompt(changedFiles)

	// Configure CLI with validator's allowed tools and resolved model
	cli := r.cli.WithAllowedTools(validator.GetAllowedTools())
	model := r.resolveModel(validator)
	cli = cli.WithModel(model)

	// Invoke Claude with the constructed prompt
	promptResult, err := cli.Prompt(ctx, prompt)
	if err != nil {
		return nil, fmt.Errorf("claude invocation failed: %w", err)
	}

	// Check for <PASSED> token in output
	passed := strings.Contains(promptResult.Stdout, PassedToken)

	result := &Result{
		Passed: passed,
	}

	// If failed, include the full output as feedback
	if !passed {
		result.Feedback = promptResult.Stdout
	}

	return result, nil
}

// RunAll executes multiple validators concurrently for the given run trigger.
// It computes the git diff once and shares it across all validators.
// Each validator runs in its own goroutine with independent timeouts.
// Context cancellation (e.g., Ctrl+C) stops all running validators.
func (r *Runner) RunAll(ctx context.Context, validators []*domain.Validator, trigger domain.RunTrigger) []ValidatorResult {
	// Compute git diff once, shared across all validators
	changedFiles, diffErr := r.getGitDiff(ctx)
	if diffErr != nil {
		// If diff failed, return error for all matching validators
		var results []ValidatorResult
		for _, v := range validators {
			if v.GetRun() == trigger {
				results = append(results, ValidatorResult{
					ID:  v.ID,
					Err: fmt.Errorf("failed to get git diff: %w", diffErr),
				})
			}
		}
		return results
	}

	return r.RunAllWithDiff(ctx, validators, trigger, changedFiles)
}

// RunAllWithDiff executes multiple validators concurrently with a pre-computed git diff.
// This allows the caller to compute the diff once and share it, which is useful when
// running validators in parallel with other operations that also need the diff.
// Each validator runs in its own goroutine. Context cancellation stops all validators.
func (r *Runner) RunAllWithDiff(ctx context.Context, validators []*domain.Validator, trigger domain.RunTrigger, changedFiles string) []ValidatorResult {
	return r.RunAllWithDiffLimited(ctx, validators, trigger, changedFiles, 0)
}

// DefaultValidatorConcurrency is the default number of validators that run in parallel
const DefaultValidatorConcurrency = 4

// RunAllWithDiffLimited executes multiple validators with a concurrency limit.
// If concurrencyLimit <= 0, defaults to DefaultValidatorConcurrency.
// Validators run in parallel up to the limit, then wait for slots to free.
// Context cancellation stops all validators.
func (r *Runner) RunAllWithDiffLimited(ctx context.Context, validators []*domain.Validator, trigger domain.RunTrigger, changedFiles string, concurrencyLimit int) []ValidatorResult {
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

	// Apply default concurrency limit
	if concurrencyLimit <= 0 {
		concurrencyLimit = DefaultValidatorConcurrency
	}

	var (
		results []ValidatorResult
		mu      sync.Mutex
	)

	// Use errgroup with concurrency limit for structured concurrency
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(concurrencyLimit)

	for _, validator := range matching {
		v := validator // capture for goroutine
		g.Go(func() error {
			var vr ValidatorResult
			vr.ID = v.ID

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
// The model used is resolved via: validator override > models.validators > models.default.
func (r *Runner) runWithDiff(ctx context.Context, validator *domain.Validator, changedFiles string) (*Result, error) {
	// Expand the prompt with changed files
	prompt := validator.ExpandPrompt(changedFiles)

	// Configure CLI with validator's allowed tools and resolved model
	cli := r.cli.WithAllowedTools(validator.GetAllowedTools())
	model := r.resolveModel(validator)
	cli = cli.WithModel(model)

	// Invoke Claude with the constructed prompt
	promptResult, err := cli.Prompt(ctx, prompt)
	if err != nil {
		return nil, fmt.Errorf("claude invocation failed: %w", err)
	}

	// Check for <PASSED> token in output
	passed := strings.Contains(promptResult.Stdout, PassedToken)

	result := &Result{
		Passed: passed,
	}

	// If failed, include the full output as feedback
	if !passed {
		result.Feedback = promptResult.Stdout
	}

	return result, nil
}

// GetGitDiff returns the uncommitted changes of the current work item.
// This is useful when you need to compute the diff once and share it
// across multiple operations (e.g., parallel verification and validation).
func (r *Runner) GetGitDiff(ctx context.Context) (string, error) {
	return r.getGitDiff(ctx)
}

// getGitDiff returns the diff of the current work item's uncommitted changes.
// Validators run before the work item is committed, so HEAD is the previous
// work item's commit and "git diff HEAD" scopes exactly to the changes under
// review. It also works on a repo whose only commit is HEAD, where "HEAD~1"
// would not resolve.
func (r *Runner) getGitDiff(ctx context.Context) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "diff", "HEAD")
	cmd.Dir = r.workDir

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git diff failed: %w (%s)", err, stderr.String())
	}

	return stdout.String(), nil
}

// AggregateResult holds the combined outcome of multiple validator runs
type AggregateResult struct {
	// Passed is true only if ALL validators passed
	Passed bool
	// Feedback contains combined failure messages from failed validators only
	Feedback string
}

// AggregateResults combines results from parallel validator execution.
// Overall pass requires ALL validators to pass (output <PASSED>).
// Only failed validators are included in feedback to reduce noise.
func AggregateResults(results []ValidatorResult) *AggregateResult {
	aggregate := &AggregateResult{Passed: true}

	var feedback strings.Builder
	for _, vr := range results {
		if vr.Err != nil {
			aggregate.Passed = false
			feedback.WriteString(fmt.Sprintf("Validator %s error: %v\n\n", vr.ID, vr.Err))
		} else if vr.Result != nil && !vr.Result.Passed {
			aggregate.Passed = false
			feedback.WriteString(fmt.Sprintf("Validator %s failed:\n%s\n\n", vr.ID, vr.Result.Feedback))
		}
		// Passing validators are not included in feedback
	}

	aggregate.Feedback = strings.TrimSpace(feedback.String())
	return aggregate
}
