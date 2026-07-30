package ralph

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/leightonvanrooijen/utopia/internal"
	"github.com/leightonvanrooijen/utopia/internal/chunk"
	"github.com/leightonvanrooijen/utopia/internal/domain"
	"github.com/leightonvanrooijen/utopia/internal/ui"
	"github.com/leightonvanrooijen/utopia/internal/validators"
)

// maxEscalationADRs caps how many ADRs an escalated context quotes, for the same
// reason the scoper's excerpt is capped: the excerpt is a starting point, and the
// escalated attempt can open any other ADR it wants.
const maxEscalationADRs = maxScoperADRs

// escalationContext is the fresh context an escalated attempt is built, in place
// of the transcript of the attempts that failed. Both escalated paths - an
// escalated execution attempt and a scoping escalation - are built from this one
// type, so they cannot drift into carrying different evidence.
//
// What is here is what survives a failed attempt: the specification as it stands,
// each failure's conclusion, and the evidence of what actually broke. What is
// deliberately not here is the attempts themselves - no model reasoning, no
// partial diffs other than the last attempt's final one, no tool-call logs.
// Replaying those anchors the escalated model to the mental model that just
// failed, which is the specific thing escalation exists to escape, and it pays
// the higher tier's rate to read the cheaper tier's dead ends.
//
// The fields are prose, not structs, because every one of them is rendered into a
// prompt and nothing downstream routes on them.
type escalationContext struct {
	// changeRequest is the change request source exactly as it sits on disk.
	changeRequest string
	// spec is the specification the failing feature lands in, or empty when the
	// change request creates it.
	spec string
	// adrs are the decisions most likely to bear on the failure.
	adrs string
	// conclusions is one line per prior failure: its diagnosis and, on a
	// comprehension failure, its corrected intent.
	conclusions []string
	// finalDiff is the diff the last attempt left in the working tree. It is
	// evidence rather than reasoning, which is why it survives when the attempt
	// that produced it does not.
	finalDiff string
	// verification is the verification command's output, verbatim.
	verification string
	// validatorOutput is the failing validators' output, verbatim.
	validatorOutput string
}

// buildEscalationContext gathers the evidence for one escalated attempt. The
// fallback diagnoses are used only when the work item carries no accumulated
// failure conclusions - a work item escalated from persisted counters after a
// resume, whose conclusions were reached in an earlier process.
func buildEscalationContext(
	store *internal.YAMLStore,
	item *domain.WorkItem,
	cr *domain.ChangeRequest,
	crID, finalDiff string,
	fallbackDiagnoses []string,
) (*escalationContext, error) {
	source, err := changeRequestSource(store, crID)
	if err != nil {
		return nil, err
	}

	ec := &escalationContext{
		changeRequest:   source,
		conclusions:     failureConclusionLines(item, fallbackDiagnoses),
		finalDiff:       strings.TrimSpace(finalDiff),
		verification:    strings.TrimSpace(item.LastFailureOutput),
		validatorOutput: strings.TrimSpace(item.LastValidatorFeedback),
	}
	ec.spec = specExcerpt(store, cr, item, phaseOf(item))
	ec.adrs = adrExcerpt(store, item.Title+" "+strings.Join(ec.conclusions, " "))
	return ec, nil
}

// render writes the shared evidence sections. Both escalated prompts call it, so
// a scoping escalation context is constructed by the same rules an escalated
// execution context is; only the instructions around it differ.
func (ec *escalationContext) render(b *strings.Builder) {
	fmt.Fprintf(b, "\n## THE CHANGE REQUEST AS IT STANDS\n\n```yaml\n%s\n```\n", ec.changeRequest)

	if ec.spec != "" {
		fmt.Fprintf(b, "\n## THE SPEC THIS LANDS IN\n\n%s\n", ec.spec)
	}
	if ec.adrs != "" {
		fmt.Fprintf(b, "\n## RELEVANT DECISIONS ALREADY MADE\n\n%s\n", ec.adrs)
	}

	b.WriteString("\n## WHAT THE FAILED ATTEMPTS CONCLUDED\n\n")
	if len(ec.conclusions) == 0 {
		b.WriteString("No validator diagnosis was recorded.\n")
	}
	for _, line := range ec.conclusions {
		fmt.Fprintf(b, "- %s\n", line)
	}

	if ec.finalDiff != "" {
		fmt.Fprintf(b, "\n## THE DIFF THE LAST ATTEMPT LEFT\n\n```diff\n%s\n```\n", ec.finalDiff)
	}
	if ec.verification != "" {
		fmt.Fprintf(b, "\n## VERIFICATION OUTPUT\n\nVerbatim, from the verification command:\n\n```\n%s\n```\n", ec.verification)
	}
	if ec.validatorOutput != "" {
		fmt.Fprintf(b, "\n## VALIDATOR OUTPUT\n\nVerbatim, from the failing validators:\n\n```\n%s\n```\n", ec.validatorOutput)
	}

	b.WriteString(`
## WHAT IS NOT HERE

You have not been given the failed attempts themselves - not their reasoning,
not the partial diffs they went through, not the commands they ran. That is
deliberate. Everything those attempts concluded is above, and re-reading how
they got there would only lead you where they went. Do not ask for it; work from
the specification and the evidence.
`)
}

// buildEscalatedPrompt composes what an escalated execution attempt is sent: the
// work item as it stands, the specification behind it, and the conclusions and
// evidence the failed attempts produced. It is a fresh context rather than the
// failure-injection prompt a mechanical retry gets, because the escalated
// attempt's job is to re-derive the intent, not to patch the last diff.
func buildEscalatedPrompt(store *internal.YAMLStore, item *domain.WorkItem, crID, finalDiff string) (string, error) {
	cr, err := store.LoadChangeRequest(crID)
	if err != nil {
		return "", fmt.Errorf("failed to load change request %s for an escalated attempt: %w", crID, err)
	}

	ec, err := buildEscalationContext(store, item, cr, crID, finalDiff, nil)
	if err != nil {
		return "", err
	}

	var b strings.Builder
	fmt.Fprintf(&b, `You are an escalated Executor. Work item %q failed validation on a cheaper
executor, and the failures were classified as comprehension failures: the
implementation was not merely sloppy, the intent behind it was wrong.

Read the specification below and derive what the work item means for yourself.
The previous attempts' conclusions are here as evidence of what has already been
ruled out - not as a plan to continue. Where the diff below contradicts the
specification, the diff is what is wrong.

## THE WORK ITEM

`+"```\n%s\n```"+`
`, item.ID, strings.TrimSpace(item.Prompt))

	ec.render(&b)

	fmt.Fprintf(&b, `
## WHAT TO DO

Implement work item %q so that it satisfies its acceptance criteria and the
project standards its prompt names. Fix the diff above where it is wrong, and
replace it where it was built on the wrong intent.

When the work is complete and the verification command passes, commit your
changes and output: %s
`, item.ID, CompletionToken)

	return b.String(), nil
}

// recordFailureConclusions appends what this attempt's failing validators
// concluded to the work item, so a later escalated attempt can be handed the
// conclusions without the attempts. Only the conclusion is recorded: a validator
// that failed without stating a diagnosis contributes nothing to carry forward.
func recordFailureConclusions(item *domain.WorkItem, agg *validators.AggregateResult) {
	if agg == nil {
		return
	}
	for _, f := range agg.Failures {
		if f.Verdict == nil || f.Verdict.Diagnosis == "" {
			continue
		}
		item.FailureConclusions = append(item.FailureConclusions, domain.FailureConclusion{
			Iteration:       item.IterationCount,
			ValidatorID:     f.ID,
			FailureClass:    string(f.Verdict.FailureClass),
			Diagnosis:       f.Verdict.Diagnosis,
			CorrectedIntent: f.Verdict.CorrectedIntent,
		})
	}
}

// failureConclusionLines renders one line per prior failure. The corrected intent
// rides on the same line as the diagnosis it belongs to: it is what that
// validator says the work item should have been understood to mean, and it is the
// closest thing to a corrected implementation anyone has already written down.
func failureConclusionLines(item *domain.WorkItem, fallback []string) []string {
	if len(item.FailureConclusions) == 0 {
		return fallback
	}
	lines := make([]string, 0, len(item.FailureConclusions))
	for _, c := range item.FailureConclusions {
		line := fmt.Sprintf("iteration %d, %s", c.Iteration, c.ValidatorID)
		if c.FailureClass != "" {
			line += " (" + c.FailureClass + ")"
		}
		line += ": " + c.Diagnosis
		if c.CorrectedIntent != "" {
			line += " (corrected intent: " + c.CorrectedIntent + ")"
		}
		lines = append(lines, line)
	}
	return lines
}

// escalationDiff returns the diff the last attempt left, for an escalated
// context to carry as evidence. A diff that cannot be computed costs the
// escalated attempt some evidence, which is never worth stopping a run over, so
// the failure is reported and the context is built without it.
func escalationDiff(ctx context.Context, out *ui.Printer, runner *validators.Runner) string {
	if runner == nil {
		return ""
	}
	diff, err := runner.GetGitDiff(ctx)
	if err != nil {
		ui.OrDefault(out).Progressf("  warning: could not compute the diff for the escalated context: %v\n", err)
		return ""
	}
	return diff
}

// changeRequestSource reads the change request exactly as it sits on disk. The
// file is quoted rather than the parsed struct re-marshalled so the escalated
// attempt sees the comments and formatting a person wrote, which is often where
// the intent that never made it into a criterion actually lives.
func changeRequestSource(store *internal.YAMLStore, crID string) (string, error) {
	path, err := store.ChangeRequestPath(crID)
	if err != nil {
		return "", fmt.Errorf("failed to locate change request %s: %w", crID, err)
	}
	source, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("failed to read change request %s: %w", path, err)
	}
	return strings.TrimSpace(string(source)), nil
}

// specExcerpt renders the spec the failing feature lands in, so an escalated
// attempt can see the language the rest of that spec already uses. A change
// request that creates a new spec has nothing to quote, which is not an error -
// it is the case where there is no established language to match yet.
func specExcerpt(store *internal.YAMLStore, cr *domain.ChangeRequest, item *domain.WorkItem, phase int) string {
	if cr == nil {
		return ""
	}
	specID := chunk.SpecForFeature(changesForPhase(cr, phase), tasksForPhase(cr, phase), featureIDOf(item))
	if specID == "" {
		return ""
	}
	spec, err := store.LoadSpec(specID)
	if err != nil {
		return ""
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Spec %s (%s):\n", spec.ID, spec.Title)
	for _, dk := range spec.DomainKnowledge {
		fmt.Fprintf(&b, "- domain knowledge: %s\n", strings.TrimSpace(dk))
	}
	for _, f := range spec.Features {
		fmt.Fprintf(&b, "\n### %s\n%s\n", f.ID, strings.TrimSpace(f.Description))
		for _, c := range f.AcceptanceCriteria {
			fmt.Fprintf(&b, "- %s\n", c)
		}
	}
	return strings.TrimSpace(b.String())
}

// adrExcerpt quotes the ADRs most likely to bear on the failure, ranked by how
// much vocabulary they share with the given prose - the work item's title and the
// failures' conclusions. It is a starting point rather than a search: the
// escalated attempt has Read and can open any other ADR the excerpt makes it want
// to see.
func adrExcerpt(store *internal.YAMLStore, against string) string {
	adrs, err := store.ListADRs()
	if err != nil || len(adrs) == 0 {
		return ""
	}

	terms := termsOf(against)
	type scored struct {
		adr   *domain.ADR
		score int
	}
	ranked := make([]scored, 0, len(adrs))
	for _, adr := range adrs {
		ranked = append(ranked, scored{adr: adr, score: overlap(terms, termsOf(adr.Title+" "+adr.Context+" "+adr.Decision))})
	}
	sort.SliceStable(ranked, func(i, j int) bool { return ranked[i].score > ranked[j].score })

	var b strings.Builder
	quoted := 0
	for _, r := range ranked {
		if r.score == 0 || quoted >= maxEscalationADRs {
			break
		}
		fmt.Fprintf(&b, "### %s - %s\n%s\n\n", r.adr.ID, r.adr.Title, strings.TrimSpace(r.adr.Decision))
		quoted++
	}
	return strings.TrimSpace(b.String())
}
