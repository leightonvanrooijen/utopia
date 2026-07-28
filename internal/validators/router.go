package validators

import (
	"context"
	"fmt"
	"strings"

	"github.com/leightonvanrooijen/utopia/internal/domain"
)

// DefaultRouterModel is the model the relevance router uses when config sets no
// models.validator_router override. It defaults to a cheap haiku-tier model,
// independently of models.validators, because routing is a high-volume recall
// filter run over every change, not the precise pass/fail gate.
const DefaultRouterModel = string(domain.ModelHaiku)

// resolveRouterModel determines which model the relevance router uses.
// Priority: models.validator_router > DefaultRouterModel (haiku). It never falls
// through to models.validators or models.default, so the router stays cheap and
// independent of the (potentially larger) validators execution model.
func (r *Runner) resolveRouterModel() string {
	if r.modelConfig != nil && r.modelConfig.ValidatorRouter != "" {
		return r.modelConfig.ValidatorRouter
	}
	return DefaultRouterModel
}

// SelectApplicable runs the relevance router: given the configured validators
// and the change diff, it returns the ids of the validators that should run.
//
// It is a recall-biased filter, not a precise classifier. Two classes of
// validator bypass the router and are always included: those marked always-run
// and those with no description (the router has no signal to route on). On-demand
// validators are never selected. The remaining validators are offered to a cheap
// model along with the diff, and the model is asked to include any whose
// relevance is even uncertain.
//
// The returned ids are the exact set the caller should run; each validator body
// remains the real pass/fail gate, so over-inclusion only costs an extra run. If
// the router call fails, SelectApplicable falls back to returning every
// applicable id (all non-on-demand validators) so a routing failure never
// silently skips validation; the underlying error is returned alongside for
// logging.
func (r *Runner) SelectApplicable(ctx context.Context, vs []*domain.Validator, diff string) ([]string, error) {
	var (
		bypass   []string            // always-run or no-description: always included
		eligible []*domain.Validator // offered to the router for a relevance judgement
	)
	for _, v := range vs {
		if v.GetRun() == domain.RunOnDemand {
			continue // on-demand validators are never selected by the router
		}
		if v.BypassesRouter() {
			bypass = append(bypass, v.ID)
			continue
		}
		eligible = append(eligible, v)
	}

	// Nothing for the router to decide: either every applicable validator bypasses
	// routing, or there are no applicable validators at all. Skip the model call.
	if len(eligible) == 0 {
		return bypass, nil
	}

	prompt := buildRouterPrompt(eligible, diff)
	cli := r.cli.WithAllowedTools(nil).WithModel(r.resolveRouterModel())

	result, err := cli.Prompt(ctx, prompt)
	if err != nil {
		// Fallback: run every applicable validator rather than skip validation.
		return allApplicableIDs(bypass, eligible), fmt.Errorf("validator router call failed: %w", err)
	}

	return append(bypass, parseRouterSelection(result.Stdout, eligible)...), nil
}

// allApplicableIDs returns every applicable validator id (bypass plus eligible),
// used as the router's fallback selection when the model call fails.
func allApplicableIDs(bypass []string, eligible []*domain.Validator) []string {
	ids := make([]string, 0, len(bypass)+len(eligible))
	ids = append(ids, bypass...)
	for _, v := range eligible {
		ids = append(ids, v.ID)
	}
	return ids
}

// buildRouterPrompt renders the single cheap-model call: it lists each eligible
// validator's id and description, includes the change diff, and instructs the
// model to select relevance with a recall bias (include when uncertain).
func buildRouterPrompt(eligible []*domain.Validator, diff string) string {
	var b strings.Builder
	b.WriteString("You are a relevance router selecting which project-standards validators should review a code change.\n\n")
	b.WriteString("Each validator below has an id and a description of what it checks and when it applies. ")
	b.WriteString("Given the change diff, decide which validators are potentially relevant to this change.\n\n")
	b.WriteString("This is a RECALL filter, not a precise decision. Each selected validator then runs its full check, which is the real pass/fail gate. ")
	b.WriteString("A validator you include unnecessarily only costs one extra run; a validator you leave out ships an unchecked violation. ")
	b.WriteString("Therefore, whenever you are even slightly uncertain whether a validator applies, INCLUDE it.\n\n")
	b.WriteString("## Validators\n\n")
	for _, v := range eligible {
		b.WriteString(fmt.Sprintf("- %s: %s\n", v.ID, strings.TrimSpace(v.Description)))
	}
	b.WriteString("\n## Change diff\n\n```diff\n")
	b.WriteString(diff)
	b.WriteString("\n```\n\n")
	b.WriteString("## Output\n\n")
	b.WriteString("Output ONLY the ids of the validators that should run, one per line, and nothing else. ")
	b.WriteString("If none apply, output nothing.\n")
	return b.String()
}

// parseRouterSelection extracts the eligible validator ids the router chose from
// its raw output. It tokenizes the output on non-id characters and selects an
// eligible validator when its id appears as a whole token. Whole-token matching
// tolerates the model wrapping ids in prose, bullets, or punctuation while
// avoiding partial-id collisions (e.g. "security" never matches "security-headers").
func parseRouterSelection(output string, eligible []*domain.Validator) []string {
	tokens := make(map[string]bool)
	for _, tok := range strings.FieldsFunc(output, func(rr rune) bool { return !isIDRune(rr) }) {
		tokens[tok] = true
	}

	var selected []string
	for _, v := range eligible {
		if tokens[v.ID] {
			selected = append(selected, v.ID)
		}
	}
	return selected
}

// isIDRune reports whether r can appear in a validator id (kebab/snake-case
// alphanumerics). Everything else is treated as a token boundary.
func isIDRune(r rune) bool {
	return r == '-' || r == '_' ||
		(r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
		(r >= '0' && r <= '9')
}
