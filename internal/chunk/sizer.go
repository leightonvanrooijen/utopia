package chunk

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/leightonvanrooijen/utopia/internal"
	"github.com/leightonvanrooijen/utopia/internal/domain"
)

// Sizing tags delimit the sizer's machine-readable answer. The block is tagged
// rather than assumed to be the whole of stdout because the sizer explores the
// codebase first, so its output carries reasoning the parser must skip past.
const (
	SizingOpenTag  = "<SIZING>"
	SizingCloseTag = "</SIZING>"
)

// SizerTools is the tool allowlist every sizer invocation runs under. The sizer
// reasons about a change it must never make, so the allowlist holds only the
// three read-only tools it needs to explore the codebase: nothing here can edit
// a file, write one, or run a command.
var SizerTools = []string{"Read", "Grep", "Glob"}

// Prompter runs one sizer invocation and returns its stdout. It exists so the
// sizing logic - which features are assessed, how a split is validated, what
// happens when the answer is unusable - can be exercised without spawning a
// claude subprocess. Leave SizerOptions.Prompt nil in production and the
// read-only claude CLI is used.
type Prompter func(ctx context.Context, prompt string) (string, error)

// SizerOptions configures one sizing pass.
type SizerOptions struct {
	// TurnBudget is work_items.turn_budget: the turns one work item may spend.
	// It is the yardstick every feature is assessed against.
	TurnBudget int
	// Model is the model the sizer runs on; empty leaves the claude CLI default.
	Model string
	// Auth selects the credential the invocation authenticates with; the empty
	// mode inherits the ambient environment.
	Auth domain.AuthMode
	// UtopiaDir is the project's .utopia directory, where api-key auth looks for
	// the key.
	UtopiaDir string
	// Prompt overrides how the invocation is made. Nil uses the read-only CLI.
	Prompt Prompter
}

// SizingVerdict is what the sizer concluded about one feature, and how many work
// items that conclusion produces. It is reported rather than kept internal so a
// split is visible at chunk time instead of only discoverable from the work
// items it left behind.
type SizingVerdict struct {
	// FeatureID names the feature the verdict is about.
	FeatureID string
	// Assessed is false when the sizer returned nothing usable for this feature,
	// in which case FitsBudget carries no judgement and the feature produces one
	// work item.
	Assessed bool
	// FitsBudget is whether the feature fits the turn budget as written.
	FitsBudget bool
	// Reason is the sizer's justification, plus any explanation of why a split it
	// proposed was rejected.
	Reason string
	// WorkItems is how many work items the feature produces.
	WorkItems int
}

// Sizing is one pass of the sizer over a whole document: the splits to hand to
// Chunk, and a verdict per feature to report.
type Sizing struct {
	// Splits is the sizer's accepted splits, ready to pass to Chunk or
	// ChunkPhase. Nil when nothing was split.
	Splits FeatureSplits
	// Verdicts holds one verdict per feature, in document order.
	Verdicts []SizingVerdict
	// Fallback is empty on a pass that ran. When the invocation failed or its
	// answer could not be parsed it explains why, and every feature produces one
	// work item - chunking must not become unusable because a sizer call failed.
	Fallback string
}

// SizeChangeRequest assesses every feature of a change request against the turn
// budget in a single sizer invocation. It never returns an error: a sizer that
// fails falls back to one work item per feature and says so in Fallback.
func SizeChangeRequest(ctx context.Context, cr *domain.ChangeRequest, opts SizerOptions) *Sizing {
	features, _ := extractFeatures(cr)
	return sizeFeatures(ctx, features, opts)
}

// SizePhase assesses every feature of one initiative phase against the turn
// budget in a single sizer invocation. Like SizeChangeRequest it falls back
// rather than failing.
func SizePhase(ctx context.Context, phase *domain.Phase, opts SizerOptions) *Sizing {
	features, _ := extractFeaturesFromPhase(phase)
	return sizeFeatures(ctx, features, opts)
}

// sizeFeatures runs exactly one sizer invocation over all the features. One pass
// rather than one per feature: the exploration is amortised across the document,
// and the sizer can see that two features touch the same files and size both
// accordingly.
func sizeFeatures(ctx context.Context, features []domain.Feature, opts SizerOptions) *Sizing {
	if len(features) == 0 {
		return &Sizing{}
	}

	prompt := opts.Prompt
	if prompt == nil {
		prompt = claudePrompter(opts)
	}

	stdout, err := prompt(ctx, buildSizerPrompt(features, opts.TurnBudget))
	if err != nil {
		return fallbackSizing(features, fmt.Sprintf("sizer invocation failed: %v", err))
	}

	assessments, err := parseSizerResponse(stdout)
	if err != nil {
		return fallbackSizing(features, err.Error())
	}

	return applyAssessments(features, assessments)
}

// claudePrompter builds the read-only invocation the sizer runs under: the
// allowlist restricts it to the three exploration tools, and the permission mode
// is left on the CLI default so a tool outside the allowlist is denied rather
// than waved through, which is what bypassing permissions would do.
func claudePrompter(opts SizerOptions) Prompter {
	return func(ctx context.Context, prompt string) (string, error) {
		cli := internal.NewCLI().
			WithPermissionMode(internal.PermissionDefault).
			WithAllowedTools(SizerTools).
			WithAuth(opts.Auth, opts.UtopiaDir)
		if opts.Model != "" {
			cli = cli.WithModel(opts.Model)
		}

		result, err := cli.Prompt(ctx, prompt)
		if err != nil {
			return "", err
		}
		return result.Stdout, nil
	}
}

// fallbackSizing is the result of a pass that could not be used: no splits, and
// a verdict per feature recording that it was not assessed.
func fallbackSizing(features []domain.Feature, reason string) *Sizing {
	sizing := &Sizing{Fallback: reason}
	for _, feature := range features {
		sizing.Verdicts = append(sizing.Verdicts, SizingVerdict{
			FeatureID: feature.ID,
			Reason:    "not assessed",
			WorkItems: 1,
		})
	}
	return sizing
}

// sizerAssessment is one feature's entry in the sizer's answer.
type sizerAssessment struct {
	ID         string           `json:"id"`
	FitsBudget bool             `json:"fits_budget"`
	Reason     string           `json:"reason"`
	WorkItems  []sizersWorkItem `json:"work_items"`
}

// sizersWorkItem is one slice of a feature the sizer split.
type sizersWorkItem struct {
	Description string   `json:"description"`
	Criteria    []string `json:"criteria"`
	Hints       []string `json:"hints"`
}

// sizerResponse is the shape of the JSON inside the sizing block.
type sizerResponse struct {
	Features []sizerAssessment `json:"features"`
}

// parseSizerResponse extracts the sizing block and returns its assessments keyed
// by feature ID. An answer with no block, a malformed block or no features at
// all is an error: the caller falls back rather than guessing at half an answer.
func parseSizerResponse(stdout string) (map[string]sizerAssessment, error) {
	body, err := extractSizingBlock(stdout)
	if err != nil {
		return nil, err
	}

	var response sizerResponse
	if err := json.Unmarshal([]byte(body), &response); err != nil {
		return nil, fmt.Errorf("sizer block is not valid JSON: %v", err)
	}
	if len(response.Features) == 0 {
		return nil, fmt.Errorf("sizer block contains no feature assessments")
	}

	assessments := make(map[string]sizerAssessment, len(response.Features))
	for _, assessment := range response.Features {
		if assessment.ID == "" {
			continue
		}
		assessments[assessment.ID] = assessment
	}
	if len(assessments) == 0 {
		return nil, fmt.Errorf("sizer block contains no identified feature assessments")
	}
	return assessments, nil
}

// extractSizingBlock returns the contents between the sizing tags. Exactly one
// block is the contract: none means the sizer never answered, and more than one
// means there is no single answer to size against.
func extractSizingBlock(stdout string) (string, error) {
	opens := strings.Count(stdout, SizingOpenTag)
	switch {
	case opens == 0:
		return "", fmt.Errorf("sizer output contains no %s block", SizingOpenTag)
	case opens > 1:
		return "", fmt.Errorf("sizer output contains %d %s blocks, want exactly 1", opens, SizingOpenTag)
	}

	start := strings.Index(stdout, SizingOpenTag) + len(SizingOpenTag)
	end := strings.Index(stdout[start:], SizingCloseTag)
	if end < 0 {
		return "", fmt.Errorf("sizer block is missing its %s tag", SizingCloseTag)
	}
	return stripSizingFence(stdout[start : start+end]), nil
}

// stripSizingFence removes a markdown code fence wrapping the JSON object.
// Models reach for one by habit even when the contract does not ask for it.
func stripSizingFence(body string) string {
	trimmed := strings.TrimSpace(body)
	if !strings.HasPrefix(trimmed, "```") {
		return trimmed
	}

	// Drop the opening fence line, which may carry a language tag, and the
	// closing fence.
	if newline := strings.Index(trimmed, "\n"); newline >= 0 {
		trimmed = trimmed[newline+1:]
	}
	if fence := strings.LastIndex(trimmed, "```"); fence >= 0 {
		trimmed = trimmed[:fence]
	}
	return strings.TrimSpace(trimmed)
}

// applyAssessments turns the sizer's answer into splits and verdicts, walking
// the features in document order.
//
// Features are only ever divided, never combined: each one is looked up by its
// own ID and expands independently, so no answer the sizer can give merges two
// features into one work item.
func applyAssessments(features []domain.Feature, assessments map[string]sizerAssessment) *Sizing {
	sizing := &Sizing{}

	for _, feature := range features {
		assessment, ok := assessments[feature.ID]
		if !ok {
			sizing.Verdicts = append(sizing.Verdicts, SizingVerdict{
				FeatureID: feature.ID,
				Reason:    "the sizer returned no assessment for this feature",
				WorkItems: 1,
			})
			continue
		}

		verdict := SizingVerdict{
			FeatureID:  feature.ID,
			Assessed:   true,
			FitsBudget: assessment.FitsBudget,
			Reason:     assessment.Reason,
			WorkItems:  1,
		}

		if assessment.FitsBudget {
			sizing.Verdicts = append(sizing.Verdicts, verdict)
			continue
		}

		slices, err := validateSplit(feature, assessment)
		if err != nil {
			// A split that cannot be trusted is not applied: the feature stays whole
			// rather than losing criteria to a division no one reviewed.
			verdict.Reason = joinReason(assessment.Reason, fmt.Sprintf("split rejected: %v", err))
			sizing.Verdicts = append(sizing.Verdicts, verdict)
			continue
		}

		if sizing.Splits == nil {
			sizing.Splits = FeatureSplits{}
		}
		sizing.Splits[feature.ID] = slices
		verdict.WorkItems = len(slices)
		sizing.Verdicts = append(sizing.Verdicts, verdict)
	}

	return sizing
}

// joinReason appends an explanation to the sizer's own reason without losing it.
func joinReason(reason, note string) string {
	if strings.TrimSpace(reason) == "" {
		return note
	}
	return reason + "; " + note
}

// validateSplit checks a proposed split before it is trusted with a feature's
// criteria, and returns the slices to chunk.
//
// The split must either partition the feature's criteria - every criterion
// assigned to exactly one slice, none dropped, none duplicated - or, for a
// feature whose single criterion cannot be partitioned further, author sub-steps
// in their place. Anything else is rejected, because a criterion silently
// dropped or reworded is a criterion that no longer traces to the reviewed
// change request.
func validateSplit(feature domain.Feature, assessment sizerAssessment) ([]domain.Feature, error) {
	if len(assessment.WorkItems) < 2 {
		return nil, fmt.Errorf("feature exceeds the budget but the sizer returned %d work item(s)", len(assessment.WorkItems))
	}

	slices := make([]domain.Feature, 0, len(assessment.WorkItems))
	for n, item := range assessment.WorkItems {
		if len(item.Criteria) == 0 {
			return nil, fmt.Errorf("work item %d has no acceptance criteria", n+1)
		}
		description := strings.TrimSpace(item.Description)
		if description == "" {
			description = fmt.Sprintf("%s (part %d)", feature.Description, n+1)
		}
		slices = append(slices, domain.Feature{
			Description:        description,
			AcceptanceCriteria: item.Criteria,
			Hints:              item.Hints,
		})
	}

	if isPartitionOf(feature.AcceptanceCriteria, slices) {
		return slices, nil
	}
	if len(feature.AcceptanceCriteria) == 1 {
		// The escalation: one criterion too large to partition, so the sizer wrote
		// the sub-steps itself. expandFeatures records that on the work items.
		return slices, nil
	}
	return nil, fmt.Errorf("criteria are neither a partition of the feature's %d criteria nor an authored split of a single criterion", len(feature.AcceptanceCriteria))
}

// isPartitionOf reports whether the slices between them assign every criterion
// of the feature to exactly one work item, with none dropped and none
// duplicated. Criteria are compared on their trimmed text, which is what makes a
// slice traceable back to the change request it was reviewed in.
func isPartitionOf(criteria []string, slices []domain.Feature) bool {
	remaining := make(map[string]int, len(criteria))
	for _, criterion := range criteria {
		remaining[strings.TrimSpace(criterion)]++
	}

	assigned := 0
	for _, slice := range slices {
		for _, criterion := range slice.AcceptanceCriteria {
			key := strings.TrimSpace(criterion)
			if remaining[key] == 0 {
				return false
			}
			remaining[key]--
			assigned++
		}
	}

	return assigned == len(criteria)
}

// buildSizerPrompt renders the one prompt the sizer is given: every feature in
// the document, the budget to assess them against, and the answer contract.
func buildSizerPrompt(features []domain.Feature, turnBudget int) string {
	var sb strings.Builder

	sb.WriteString("You are sizing the work items of a change request before they are generated.\n\n")
	sb.WriteString(fmt.Sprintf("A work item is executed by a single agent with a budget of %d turns. ", turnBudget))
	sb.WriteString("For each feature below, explore this codebase and decide whether one agent could ")
	sb.WriteString("complete the whole feature - implementation, tests and verification - inside that budget.\n\n")

	sb.WriteString("You are read-only. Reason about the change; never make it.\n\n")

	sb.WriteString("## FEATURES\n\n")
	for _, feature := range features {
		sb.WriteString(fmt.Sprintf("### %s\n\n", feature.ID))
		sb.WriteString(feature.Description)
		sb.WriteString("\n\nAcceptance criteria:\n")
		for _, criterion := range feature.AcceptanceCriteria {
			sb.WriteString("- ")
			sb.WriteString(criterion)
			sb.WriteString("\n")
		}
		sb.WriteString("\n")
	}

	sb.WriteString("## SPLITTING RULES\n\n")
	sb.WriteString("- A feature that fits the budget is not split: report it and move on.\n")
	sb.WriteString("- A feature that exceeds the budget is split by partitioning its acceptance\n")
	sb.WriteString("  criteria into two or more bundles. Copy each criterion verbatim into exactly\n")
	sb.WriteString("  one bundle. Do not drop, duplicate, reword or merge criteria.\n")
	sb.WriteString("- Only when a feature has a single criterion that is itself too large may you\n")
	sb.WriteString("  author new criteria describing its sub-steps.\n")
	sb.WriteString("- Never combine two features into one work item. Each feature is sized on its own.\n")
	sb.WriteString("- Split bundles are executed in the order you return them, so order them so each\n")
	sb.WriteString("  one leaves the codebase working.\n\n")

	sb.WriteString("## ANSWER\n\n")
	sb.WriteString("Explore first, then end your response with exactly one ")
	sb.WriteString(SizingOpenTag)
	sb.WriteString(" block holding JSON and nothing else:\n\n")
	sb.WriteString(SizingOpenTag)
	sb.WriteString("\n")
	sb.WriteString(`{
  "features": [
    {
      "id": "<feature id, exactly as given above>",
      "fits_budget": true,
      "reason": "<one sentence on the size of the change>",
      "work_items": []
    },
    {
      "id": "<feature id>",
      "fits_budget": false,
      "reason": "<one sentence on why it exceeds the budget>",
      "work_items": [
        {
          "description": "<what this work item does>",
          "criteria": ["<criterion, verbatim from the feature>"],
          "hints": ["<optional implementation pointer>"]
        }
      ]
    }
  ]
}`)
	sb.WriteString("\n")
	sb.WriteString(SizingCloseTag)
	sb.WriteString("\n\nEvery feature above must appear exactly once in the answer. ")
	sb.WriteString("A feature with fits_budget false must carry two or more work items.\n")

	return sb.String()
}
