package chunk

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/leightonvanrooijen/utopia/internal/domain"
)

// sizerBlock wraps a JSON body in the answer contract's tags.
func sizerBlock(body string) string {
	return "I explored the codebase.\n\n" + SizingOpenTag + "\n" + body + "\n" + SizingCloseTag + "\n"
}

// twoFeatureCR is a change request with one small and one large feature.
func twoFeatureCR() *domain.ChangeRequest {
	small := domain.Feature{ID: "feature-1", Description: "Small feature", AcceptanceCriteria: []string{"A"}}
	large := domain.Feature{ID: "feature-2", Description: "Large feature", AcceptanceCriteria: []string{"B", "C", "D"}}
	return &domain.ChangeRequest{
		ID:    "test-cr",
		Type:  domain.CRTypeFeature,
		Title: "Test CR",
		Changes: []domain.Change{
			{Operation: "add", Feature: &small},
			{Operation: "add", Feature: &large},
		},
	}
}

func TestSizeChangeRequest_OneInvocationForEveryFeature(t *testing.T) {
	var calls int
	var prompt string
	opts := SizerOptions{
		TurnBudget: 25,
		Prompt: func(_ context.Context, p string) (string, error) {
			calls++
			prompt = p
			return sizerBlock(`{"features":[
				{"id":"feature-1","fits_budget":true,"reason":"one file"},
				{"id":"feature-2","fits_budget":true,"reason":"two files"}
			]}`), nil
		},
	}

	sizing := SizeChangeRequest(context.Background(), twoFeatureCR(), opts)

	if calls != 1 {
		t.Errorf("sizer invoked %d times, want exactly 1 per chunk operation", calls)
	}
	for _, want := range []string{"feature-1", "feature-2", "Small feature", "Large feature", "25 turns"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt is missing %q:\n%s", want, prompt)
		}
	}
	if len(sizing.Verdicts) != 2 {
		t.Fatalf("got %d verdicts, want one per feature", len(sizing.Verdicts))
	}
	if len(sizing.Splits) != 0 {
		t.Errorf("features that fit the budget must not be split, got %v", sizing.Splits)
	}
	for _, v := range sizing.Verdicts {
		if !v.Assessed || !v.FitsBudget || v.WorkItems != 1 {
			t.Errorf("verdict %+v, want an assessed feature fitting the budget with 1 work item", v)
		}
	}
}

func TestSizeChangeRequest_PartitionedSplitKeepsEveryCriterion(t *testing.T) {
	opts := SizerOptions{
		TurnBudget: 25,
		Prompt: func(_ context.Context, _ string) (string, error) {
			return sizerBlock(`{"features":[
				{"id":"feature-1","fits_budget":true,"reason":"one file"},
				{"id":"feature-2","fits_budget":false,"reason":"forty call sites","work_items":[
					{"description":"Large feature, part 1","criteria":["B"]},
					{"description":"Large feature, part 2","criteria":["C","D"]}
				]}
			]}`), nil
		},
	}

	cr := twoFeatureCR()
	sizing := SizeChangeRequest(context.Background(), cr, opts)

	if sizing.Fallback != "" {
		t.Fatalf("unexpected fallback: %s", sizing.Fallback)
	}
	if got := len(sizing.Splits["feature-2"]); got != 2 {
		t.Fatalf("feature-2 split into %d slices, want 2", got)
	}
	if sizing.Verdicts[1].WorkItems != 2 || sizing.Verdicts[1].FitsBudget {
		t.Errorf("verdict = %+v, want a feature exceeding the budget with 2 work items", sizing.Verdicts[1])
	}

	items, err := Chunk(cr, nil, nil, sizing.Splits)
	if err != nil {
		t.Fatalf("Chunk() error = %v", err)
	}
	if len(items) != 3 {
		t.Fatalf("Chunk() returned %d work items, want 3", len(items))
	}

	// Every criterion of the feature lands in exactly one work item.
	counts := map[string]int{}
	for _, item := range items[1:] {
		for _, criterion := range []string{"B", "C", "D"} {
			if strings.Contains(item.Prompt, "- "+criterion+"\n") {
				counts[criterion]++
			}
		}
		if item.SourceFeatureID != "feature-2" {
			t.Errorf("SourceFeatureID = %q, want %q", item.SourceFeatureID, "feature-2")
		}
		if item.CriteriaOrigin != domain.CriteriaOriginPartitioned {
			t.Errorf("CriteriaOrigin = %q, want %q", item.CriteriaOrigin, domain.CriteriaOriginPartitioned)
		}
	}
	for _, criterion := range []string{"B", "C", "D"} {
		if counts[criterion] != 1 {
			t.Errorf("criterion %q appears in %d work items, want exactly 1", criterion, counts[criterion])
		}
	}
	// The feature that fit the budget is untouched.
	if items[0].CriteriaOrigin != "" || items[0].ID != "test-cr-feature-1" {
		t.Errorf("unsplit item = %+v, want an unchanged feature-1 work item", items[0])
	}
}

func TestSizeChangeRequest_AuthoredSplitOfASingleCriterion(t *testing.T) {
	feature := domain.Feature{ID: "feature-1", Description: "One huge criterion", AcceptanceCriteria: []string{"Migrate every call site"}}
	cr := &domain.ChangeRequest{
		ID:      "test-cr",
		Type:    domain.CRTypeFeature,
		Title:   "Test CR",
		Changes: []domain.Change{{Operation: "add", Feature: &feature}},
	}

	opts := SizerOptions{
		TurnBudget: 25,
		Prompt: func(_ context.Context, _ string) (string, error) {
			return sizerBlock(`{"features":[
				{"id":"feature-1","fits_budget":false,"reason":"one criterion, forty call sites","work_items":[
					{"description":"Migrate the store call sites","criteria":["Store call sites use the new API"]},
					{"description":"Migrate the CLI call sites","criteria":["CLI call sites use the new API"]}
				]}
			]}`), nil
		},
	}

	sizing := SizeChangeRequest(context.Background(), cr, opts)

	items, err := Chunk(cr, nil, nil, sizing.Splits)
	if err != nil {
		t.Fatalf("Chunk() error = %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("Chunk() returned %d work items, want 2", len(items))
	}
	for _, item := range items {
		if item.CriteriaOrigin != domain.CriteriaOriginAuthored {
			t.Errorf("CriteriaOrigin = %q, want %q", item.CriteriaOrigin, domain.CriteriaOriginAuthored)
		}
	}
}

func TestSizeChangeRequest_RejectsAnAuthoredSplitOfAPartitionableFeature(t *testing.T) {
	opts := SizerOptions{
		TurnBudget: 25,
		Prompt: func(_ context.Context, _ string) (string, error) {
			// feature-2 has three criteria the sizer could have partitioned, but it
			// reworded them instead - so a criterion no longer traces to the CR.
			return sizerBlock(`{"features":[
				{"id":"feature-1","fits_budget":true,"reason":"one file"},
				{"id":"feature-2","fits_budget":false,"reason":"big","work_items":[
					{"description":"part 1","criteria":["Something like B"]},
					{"description":"part 2","criteria":["Something like C and D"]}
				]}
			]}`), nil
		},
	}

	sizing := SizeChangeRequest(context.Background(), twoFeatureCR(), opts)

	if len(sizing.Splits) != 0 {
		t.Errorf("a split that loses criteria must not be applied, got %v", sizing.Splits)
	}
	verdict := sizing.Verdicts[1]
	if verdict.WorkItems != 1 {
		t.Errorf("WorkItems = %d, want 1 after the split was rejected", verdict.WorkItems)
	}
	if !strings.Contains(verdict.Reason, "split rejected") {
		t.Errorf("Reason = %q, want it to report the rejection", verdict.Reason)
	}
}

func TestSizeChangeRequest_RejectsASplitIntoOneWorkItem(t *testing.T) {
	opts := SizerOptions{
		TurnBudget: 25,
		Prompt: func(_ context.Context, _ string) (string, error) {
			return sizerBlock(`{"features":[
				{"id":"feature-1","fits_budget":true,"reason":"one file"},
				{"id":"feature-2","fits_budget":false,"reason":"big","work_items":[
					{"description":"the whole thing","criteria":["B","C","D"]}
				]}
			]}`), nil
		},
	}

	sizing := SizeChangeRequest(context.Background(), twoFeatureCR(), opts)

	if len(sizing.Splits) != 0 {
		t.Errorf("a one-item split is not a split, got %v", sizing.Splits)
	}
}

func TestSizeChangeRequest_FallsBackWhenTheInvocationFails(t *testing.T) {
	opts := SizerOptions{
		TurnBudget: 25,
		Prompt: func(_ context.Context, _ string) (string, error) {
			return "", fmt.Errorf("claude prompt failed: exit status 1")
		},
	}

	sizing := SizeChangeRequest(context.Background(), twoFeatureCR(), opts)

	if sizing.Fallback == "" {
		t.Fatal("a failed invocation must report the fallback")
	}
	if len(sizing.Splits) != 0 {
		t.Errorf("fallback must not split anything, got %v", sizing.Splits)
	}
	if len(sizing.Verdicts) != 2 {
		t.Fatalf("got %d verdicts, want one per feature", len(sizing.Verdicts))
	}
	for _, v := range sizing.Verdicts {
		if v.Assessed || v.WorkItems != 1 {
			t.Errorf("verdict %+v, want an unassessed feature producing 1 work item", v)
		}
	}

	items, err := Chunk(twoFeatureCR(), nil, nil, sizing.Splits)
	if err != nil {
		t.Fatalf("Chunk() error = %v", err)
	}
	if len(items) != 2 {
		t.Errorf("fallback produced %d work items, want one per feature", len(items))
	}
}

func TestSizeChangeRequest_FallsBackOnUnparseableOutput(t *testing.T) {
	for name, stdout := range map[string]string{
		"no block":       "I think feature-1 is fine and feature-2 is large.",
		"two blocks":     sizerBlock(`{"features":[]}`) + sizerBlock(`{"features":[]}`),
		"not json":       sizerBlock(`features: [feature-1]`),
		"empty features": sizerBlock(`{"features":[]}`),
		"unclosed":       SizingOpenTag + `{"features":[]}`,
	} {
		t.Run(name, func(t *testing.T) {
			opts := SizerOptions{
				TurnBudget: 25,
				Prompt: func(_ context.Context, _ string) (string, error) {
					return stdout, nil
				},
			}

			sizing := SizeChangeRequest(context.Background(), twoFeatureCR(), opts)

			if sizing.Fallback == "" {
				t.Errorf("unparseable output must report the fallback, got %+v", sizing)
			}
			if len(sizing.Verdicts) != 2 {
				t.Errorf("got %d verdicts, want one per feature", len(sizing.Verdicts))
			}
		})
	}
}

func TestSizeChangeRequest_AcceptsAFencedBlock(t *testing.T) {
	opts := SizerOptions{
		TurnBudget: 25,
		Prompt: func(_ context.Context, _ string) (string, error) {
			return sizerBlock("```json\n" + `{"features":[{"id":"feature-1","fits_budget":true,"reason":"small"}]}` + "\n```"), nil
		},
	}

	sizing := SizeChangeRequest(context.Background(), twoFeatureCR(), opts)

	if sizing.Fallback != "" {
		t.Fatalf("unexpected fallback: %s", sizing.Fallback)
	}
	if !sizing.Verdicts[0].Assessed {
		t.Error("feature-1 should be assessed")
	}
	// feature-2 is missing from the answer: it is reported as unassessed and
	// produces one work item rather than being folded into feature-1.
	if sizing.Verdicts[1].Assessed || sizing.Verdicts[1].WorkItems != 1 {
		t.Errorf("verdict = %+v, want an unassessed feature-2 with 1 work item", sizing.Verdicts[1])
	}
}

func TestSizeChangeRequest_NeverMergesTwoFeatures(t *testing.T) {
	opts := SizerOptions{
		TurnBudget: 25,
		Prompt: func(_ context.Context, _ string) (string, error) {
			// The sizer tries to fold both features into one work item under an id
			// of its own invention. Unknown ids are ignored, so both features are
			// still sized on their own.
			return sizerBlock(`{"features":[
				{"id":"feature-1-and-2","fits_budget":true,"reason":"they touch the same files"}
			]}`), nil
		},
	}

	cr := twoFeatureCR()
	sizing := SizeChangeRequest(context.Background(), cr, opts)

	items, err := Chunk(cr, nil, nil, sizing.Splits)
	if err != nil {
		t.Fatalf("Chunk() error = %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("got %d work items, want one per feature", len(items))
	}
	wantSources := []string{"feature-1", "feature-2"}
	for i, item := range items {
		if item.SourceFeatureID != wantSources[i] {
			t.Errorf("items[%d].SourceFeatureID = %q, want %q", i, item.SourceFeatureID, wantSources[i])
		}
	}
}

func TestSizePhase_SizesThePhasesFeatures(t *testing.T) {
	large := domain.Feature{ID: "feature-1", Description: "Large feature", AcceptanceCriteria: []string{"A", "B"}}
	phase := &domain.Phase{
		Type:    domain.CRTypeFeature,
		Changes: []domain.Change{{Operation: "add", Feature: &large}},
	}

	var calls int
	opts := SizerOptions{
		TurnBudget: 25,
		Prompt: func(_ context.Context, _ string) (string, error) {
			calls++
			return sizerBlock(`{"features":[
				{"id":"feature-1","fits_budget":false,"reason":"two subsystems","work_items":[
					{"description":"part 1","criteria":["A"]},
					{"description":"part 2","criteria":["B"]}
				]}
			]}`), nil
		},
	}

	sizing := SizePhase(context.Background(), phase, opts)

	if calls != 1 {
		t.Errorf("sizer invoked %d times, want exactly 1 per chunk operation", calls)
	}
	items, err := ChunkPhase("initiative-1", 0, phase, nil, nil, sizing.Splits)
	if err != nil {
		t.Fatalf("ChunkPhase() error = %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("got %d work items, want 2", len(items))
	}
	for _, item := range items {
		if item.CriteriaOrigin != domain.CriteriaOriginPartitioned {
			t.Errorf("CriteriaOrigin = %q, want %q", item.CriteriaOrigin, domain.CriteriaOriginPartitioned)
		}
	}
}

func TestSizeFeatures_NoFeaturesNeedsNoInvocation(t *testing.T) {
	var calls int
	opts := SizerOptions{Prompt: func(_ context.Context, _ string) (string, error) {
		calls++
		return "", nil
	}}

	sizing := sizeFeatures(context.Background(), nil, opts)

	if calls != 0 {
		t.Errorf("sizer invoked %d times for an empty document, want 0", calls)
	}
	if len(sizing.Verdicts) != 0 || sizing.Fallback != "" {
		t.Errorf("sizing = %+v, want an empty result", sizing)
	}
}

func TestSizerTools_AreReadOnly(t *testing.T) {
	want := map[string]bool{"Read": true, "Grep": true, "Glob": true}
	if len(SizerTools) != len(want) {
		t.Fatalf("SizerTools = %v, want exactly the read-only tools %v", SizerTools, want)
	}
	for _, tool := range SizerTools {
		if !want[tool] {
			t.Errorf("SizerTools contains %q, which is not a read-only tool", tool)
		}
	}
}

func TestIsPartitionOf(t *testing.T) {
	criteria := []string{"A", "B", "C"}

	tests := []struct {
		name   string
		slices []domain.Feature
		want   bool
	}{
		{"every criterion once", []domain.Feature{{AcceptanceCriteria: []string{"A"}}, {AcceptanceCriteria: []string{"B", "C"}}}, true},
		{"whitespace only differs", []domain.Feature{{AcceptanceCriteria: []string{" A "}}, {AcceptanceCriteria: []string{"B", "C"}}}, true},
		{"one dropped", []domain.Feature{{AcceptanceCriteria: []string{"A"}}, {AcceptanceCriteria: []string{"B"}}}, false},
		{"one duplicated", []domain.Feature{{AcceptanceCriteria: []string{"A", "B"}}, {AcceptanceCriteria: []string{"B", "C"}}}, false},
		{"one invented", []domain.Feature{{AcceptanceCriteria: []string{"A", "B"}}, {AcceptanceCriteria: []string{"C", "D"}}}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isPartitionOf(criteria, tt.slices); got != tt.want {
				t.Errorf("isPartitionOf() = %v, want %v", got, tt.want)
			}
		})
	}
}
