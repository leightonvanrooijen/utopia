package validators

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/leightonvanrooijen/utopia/internal"
	"github.com/leightonvanrooijen/utopia/internal/domain"
	"golang.org/x/sync/errgroup"
)

// PassedToken is the marker validators must include to indicate success
const PassedToken = "<PASSED>"

// DefaultValidatorTimeout bounds a single validator's Claude invocation. Without
// it, one hung invocation would block the whole gate on g.Wait until a global
// Ctrl+C. With it, a stuck validator resolves as its own failure and the rest of
// the batch proceeds unaffected.
const DefaultValidatorTimeout = 10 * time.Minute

// Result holds the outcome of a validator run
type Result struct {
	// Passed is true if the validator passed - a <VERDICT> block reporting pass,
	// or (for prompts predating the verdict contract) a bare <PASSED> token
	Passed bool
	// Feedback is the validator output (empty if passed, full output if failed)
	Feedback string
	// Verdict is the parsed classification of the run. It is never nil: output
	// carrying no usable verdict is classified as a comprehension failure rather
	// than left for callers to interpret. See InterpretOutput.
	Verdict *Verdict
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
	workDir          string
	cli              *internal.CLI
	modelConfig      *domain.ModelConfig
	effort           string
	validatorTimeout time.Duration
}

// NewRunner creates a validator runner that operates in the given directory.
// Use WithModelConfig to configure model fallback for validators.
func NewRunner(workDir string) *Runner {
	return &Runner{
		workDir:          workDir,
		cli:              internal.NewCLI().WithVerbose(false),
		validatorTimeout: DefaultValidatorTimeout,
	}
}

// WithValidatorTimeout sets the per-validator timeout used when running
// validators concurrently. A value <= 0 falls back to DefaultValidatorTimeout.
func (r *Runner) WithValidatorTimeout(timeout time.Duration) *Runner {
	return &Runner{
		workDir:          r.workDir,
		cli:              r.cli,
		modelConfig:      r.modelConfig,
		effort:           r.effort,
		validatorTimeout: timeout,
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
		workDir:          r.workDir,
		cli:              r.cli,
		modelConfig:      mc,
		effort:           r.effort,
		validatorTimeout: r.validatorTimeout,
	}
}

// WithAuth selects the credential every validator invocation authenticates
// with. Validators run as part of the same invocation as the work they check,
// so they must bill to the account that invocation resolved rather than to
// whatever the ambient environment happens to hold.
//
// The empty mode inherits the ambient environment, which is the pre-auth
// behaviour for a runner whose caller never resolved a mode.
func (r *Runner) WithAuth(mode domain.AuthMode) *Runner {
	return &Runner{
		workDir:          r.workDir,
		cli:              r.cli.WithAuth(mode, filepath.Join(r.workDir, ".utopia")),
		modelConfig:      r.modelConfig,
		effort:           r.effort,
		validatorTimeout: r.validatorTimeout,
	}
}

// WithEffort sets the reasoning effort every validator invocation runs at,
// resolved by the caller from effort.validators. The empty string leaves the
// claude CLI on its own default.
//
// It is a single level rather than a per-validator resolution because effort is a
// property of the validator role: a validator that fails is re-run against a
// fixed change, not asked to think harder about the same one.
func (r *Runner) WithEffort(effort string) *Runner {
	return &Runner{
		workDir:          r.workDir,
		cli:              r.cli,
		modelConfig:      r.modelConfig,
		effort:           effort,
		validatorTimeout: r.validatorTimeout,
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
// It loads the git diff, expands the validator prompt, invokes Claude, and
// interprets the output's verdict.
// The model used is resolved via: validator override > models.validators > models.default.
func (r *Runner) Run(ctx context.Context, validator *domain.Validator) (*Result, error) {
	// Get the git diff of the current work item's uncommitted changes
	changedFiles, err := r.getGitDiff(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get git diff: %w", err)
	}

	return r.runWithDiff(ctx, validator, changedFiles)
}

// RunAll executes multiple validators concurrently for the given run trigger.
// It computes the git diff once and shares it across all validators.
// Each validator runs in its own goroutine under its own timeout, so one stuck
// validator resolves as a failure without stalling the rest.
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
// Each validator runs in its own goroutine under its own timeout. Context
// cancellation stops all validators.
func (r *Runner) RunAllWithDiff(ctx context.Context, validators []*domain.Validator, trigger domain.RunTrigger, changedFiles string) []ValidatorResult {
	return r.RunAllWithDiffLimited(ctx, validators, trigger, changedFiles, 0)
}

// DefaultValidatorConcurrency is the default number of validators that run in parallel
const DefaultValidatorConcurrency = 4

// RunAllWithDiffLimited executes multiple validators with a concurrency limit.
// If concurrencyLimit <= 0, defaults to DefaultValidatorConcurrency.
// Validators run in parallel up to the limit, then wait for slots to free.
// Each validator runs under its own timeout (see WithValidatorTimeout), so a
// single stuck validator fails on its own without blocking the batch.
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

	// Resolve the per-validator timeout, guarding against a zero value from a
	// Runner that was not built via NewRunner.
	timeout := r.validatorTimeout
	if timeout <= 0 {
		timeout = DefaultValidatorTimeout
	}

	// Use errgroup with concurrency limit for structured concurrency
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(concurrencyLimit)

	for _, validator := range matching {
		v := validator // capture for goroutine
		g.Go(func() error {
			var vr ValidatorResult
			vr.ID = v.ID

			// Give each validator its own timeout derived from the per-run
			// context. The child deadline is independent, so one validator
			// timing out neither delays nor cancels the others; deriving from
			// gctx keeps global cancellation (Ctrl+C) propagating to all.
			vctx, cancel := context.WithTimeout(gctx, timeout)
			defer cancel()

			result, err := r.runWithDiff(vctx, v, changedFiles)

			// A validator that exceeds its own deadline resolves as a failure
			// with a clear timeout message. Guard on gctx so a global Ctrl+C
			// (which also trips the child deadline) is not mislabeled a timeout.
			if vctx.Err() == context.DeadlineExceeded && gctx.Err() == nil {
				result = nil
				err = fmt.Errorf("validator timed out after %s", timeout)
			}

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
// This is the internal execution method used by Run and RunAll, so both paths
// interpret a validator's output through the same contract.
// The model used is resolved via: validator override > models.validators > models.default.
func (r *Runner) runWithDiff(ctx context.Context, validator *domain.Validator, changedFiles string) (*Result, error) {
	// Expand the prompt with changed files
	prompt := validator.ExpandPrompt(changedFiles)

	// Configure CLI with validator's allowed tools and resolved model
	cli := r.cli.WithAllowedTools(validator.GetAllowedTools())
	model := r.resolveModel(validator)
	cli = cli.WithModel(model).WithEffort(r.effort)

	// Invoke Claude with the constructed prompt
	promptResult, err := cli.Prompt(ctx, prompt)
	if err != nil {
		return nil, fmt.Errorf("claude invocation failed: %w", err)
	}

	// Interpret the output: pass/fail plus the failure classification later
	// phases route on. Unusable output is a comprehension failure, not an error -
	// the run itself succeeded, the validator just could not state its verdict.
	return InterpretOutput(promptResult.Stdout), nil
}

// GetGitDiff returns the uncommitted changes of the current work item.
// This is useful when you need to compute the diff once and share it
// across multiple operations (e.g., parallel verification and validation).
func (r *Runner) GetGitDiff(ctx context.Context) (string, error) {
	return r.getGitDiff(ctx)
}

// GetGitDiffSince returns the diff between the given baseline commit and the
// working tree. After-phase validators pass the HEAD SHA recorded before the
// phase began, so the diff covers every commit produced during the phase plus
// any as-yet-uncommitted fix in progress. An empty baseline (repo with no
// commit at phase start) falls back to the uncommitted-changes diff so the
// call never fails on an unresolvable ref.
func (r *Runner) GetGitDiffSince(ctx context.Context, baseline string) (string, error) {
	if baseline == "" {
		return r.getGitDiff(ctx)
	}
	return r.gitDiff(ctx, baseline)
}

// getGitDiff returns the diff of the current work item's uncommitted changes.
// Validators run before the work item is committed, so HEAD is the previous
// work item's commit and "git diff HEAD" scopes exactly to the changes under
// review. It also works on a repo whose only commit is HEAD, where "HEAD~1"
// would not resolve.
func (r *Runner) getGitDiff(ctx context.Context) (string, error) {
	return r.gitDiff(ctx, "HEAD")
}

// gitDiff runs "git diff <args...>" in the runner's work dir and returns its
// output. It is the shared execution path for the workitem-scoped and
// phase-scoped diffs above.
func (r *Runner) gitDiff(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"diff"}, args...)...)
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
	// FailureClass is the strongest class across the failing validators: empty
	// on pass, FailureComprehension when any failure was a comprehension
	// failure, and FailureMechanical only when every failure was mechanical.
	// Validators that disagree resolve toward escalation - one validator
	// reporting a misread specification is not cancelled out by three reporting
	// lint errors.
	FailureClass FailureClass
	// SpecDefectSuspected is true when any failing validator suspects the
	// specification rather than the execution. One validator raising it is
	// enough: it is a positive claim about the spec, and the others' silence
	// does not contradict it.
	SpecDefectSuspected bool
	// Failures carries each failing validator's verdict so its diagnosis and
	// corrected_intent survive aggregation instead of being flattened into
	// Feedback, which is prose for the next iteration rather than routing
	// input. Ordered as the results were collected.
	Failures []ValidatorFailure
}

// ValidatorFailure pairs a failing validator's ID with the verdict behind its
// failure.
type ValidatorFailure struct {
	// ID is the failing validator's unique identifier
	ID string
	// Verdict is the classification the validator emitted. It is never nil: a
	// failure that carried no usable verdict is classified rather than left for
	// callers to interpret, matching InterpretOutput.
	Verdict *Verdict
}

// AggregateResults combines results from parallel validator execution.
// Overall pass requires ALL validators to pass (output <PASSED>).
// Only failed validators are included in feedback to reduce noise.
// Alongside the prose, the aggregate carries the classification later phases
// route on: the strongest failure class, whether any validator suspects a spec
// defect, and each failing validator's verdict.
func AggregateResults(results []ValidatorResult) *AggregateResult {
	aggregate := &AggregateResult{Passed: true}

	var feedback strings.Builder
	for _, vr := range results {
		if vr.Err != nil {
			feedback.WriteString(fmt.Sprintf("Validator %s error: %v\n\n", vr.ID, vr.Err))
			// The run never reached a verdict, so nothing in it supports the
			// cheaper class - the same resolution unusable output gets.
			aggregate.addFailure(vr.ID, unclassifiedFailure(fmt.Sprintf("validator did not complete: %v", vr.Err)))
		} else if vr.Result != nil && !vr.Result.Passed {
			feedback.WriteString(fmt.Sprintf("Validator %s failed:\n%s\n\n", vr.ID, vr.Result.Feedback))
			aggregate.addFailure(vr.ID, vr.Result.Verdict)
		}
		// Passing validators are not included in feedback
	}

	aggregate.Feedback = strings.TrimSpace(feedback.String())
	return aggregate
}

// addFailure records one failing validator and folds its verdict into the
// aggregate classification. A failure with no verdict, or one naming no class,
// resolves to comprehension: comprehension is the class a caller cannot recover
// from guessing wrong, because retrying on the same executor re-derives the same
// wrong intent.
func (a *AggregateResult) addFailure(id string, verdict *Verdict) {
	a.Passed = false

	if verdict == nil {
		verdict = unclassifiedFailure("validator reported a failure with no verdict")
	}
	class := verdict.FailureClass
	if class == "" {
		class = FailureComprehension
	}

	a.Failures = append(a.Failures, ValidatorFailure{ID: id, Verdict: verdict})
	a.FailureClass = strongerFailureClass(a.FailureClass, class)
	a.SpecDefectSuspected = a.SpecDefectSuspected || verdict.SpecDefectSuspected
}
