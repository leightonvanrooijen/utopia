package ralph

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/leightonvanrooijen/utopia/internal"
	"github.com/leightonvanrooijen/utopia/internal/domain"
	"github.com/leightonvanrooijen/utopia/internal/validators"
)

// scopingFixture is a store holding one change request, one spec and one ADR:
// the smallest project a scoping escalation can run against.
func scopingFixture(t *testing.T) (*internal.YAMLStore, *domain.ChangeRequest) {
	t.Helper()
	store := internal.NewYAMLStore(t.TempDir())

	cr := &domain.ChangeRequest{
		ID:     "escalation-routing",
		Type:   domain.CRTypeFeature,
		Title:  "Route validation failures on failure class",
		Status: domain.ChangeRequestInProgress,
		Changes: []domain.Change{{
			Operation: "add",
			Spec:      "escalation-routing",
			Feature: &domain.Feature{
				ID:                 "scoping-escalation",
				Description:        "Rewrite the change request when comprehension fails repeatedly.",
				AcceptanceCriteria: []string{"The scoper rewrites the change request"},
			},
		}},
	}
	if err := store.SaveChangeRequest(cr); err != nil {
		t.Fatalf("SaveChangeRequest: %v", err)
	}

	spec := domain.NewSpec("escalation-routing", "Escalation routing")
	spec.DomainKnowledge = []string{"A comprehension failure is one the same executor cannot fix by trying harder"}
	spec.Features = []domain.Feature{{
		ID:                 "failure-class-routing",
		Description:        "Route on the failure class the validators reported.",
		AcceptanceCriteria: []string{"A mechanical failure retries on the default executor"},
	}}
	if err := store.SaveSpec(spec); err != nil {
		t.Fatalf("SaveSpec: %v", err)
	}

	adr := &domain.ADR{
		ID:       "ADR-007",
		Title:    "Bound every escalation path",
		Status:   domain.ADRStatusAccepted,
		Category: domain.ADRCategoryStructure,
		Context:  "Escalation without a bound is an unbounded spend.",
		Decision: "Every escalation path carries a configurable cap, and exhausting one halts the work item.",
	}
	if err := internal.Save(store, filepath.Join("adrs", adr.ID+".yaml"), adr); err != nil {
		t.Fatalf("save ADR: %v", err)
	}

	return store, cr
}

func scopingItem() *domain.WorkItem {
	return &domain.WorkItem{
		ID:                     "escalation-routing-scoping-escalation",
		SpecRef:                "escalation-routing.scoping-escalation",
		Title:                  "Rewrite the change request when comprehension fails repeatedly.",
		Prompt:                 "## TASK\n\nRewrite the change request.",
		Status:                 domain.WorkItemInProgress,
		ComprehensionCount:     2,
		OpusExecutionAttempts:  2,
		ScopingEscalationCount: 1,
		IterationCount:         5,
		MechanicalRetryCount:   3,
		LastValidatorFeedback:  "spec-fidelity: aggregated per validator",
	}
}

func scopingError() *ScopingEscalationError {
	return &ScopingEscalationError{
		WorkItemID:         "escalation-routing-scoping-escalation",
		ComprehensionCount: 2,
		Diagnoses:          []string{"spec-fidelity: 'escalation' was read as executor escalation, the spec means scoping escalation"},
	}
}

// writeRewrite puts a rewritten change request on disk where the loop expects
// the scoper to have written one, without the provenance block - which the loop
// stamps itself rather than trusting the model to have recorded.
func writeRewrite(t *testing.T, store *internal.YAMLStore, base, body string) string {
	t.Helper()
	dir := store.ChangeRequestsDir()
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	path := filepath.Join(dir, base+".yaml")
	if err := os.WriteFile(path, []byte(body), 0644); err != nil {
		t.Fatalf("write rewrite: %v", err)
	}
	return path
}

const validRewriteYAML = `id: escalation-routing-rewrite-1
type: feature
title: Rewrite the change request, not the code
status: draft
changes:
  - operation: add
    spec: escalation-routing
    feature:
      id: scoping-escalation
      description: |
        Scoping escalation rewrites the change request.
      acceptance_criteria:
        - The scoper is instructed to rewrite the change request, not to write code
`

func TestScoperPrompt_CarriesTheChangeRequestDiagnosesSpecAndADR(t *testing.T) {
	store, cr := scopingFixture(t)
	s := &scoper{store: store, model: "opus"}
	item := scopingItem()

	prompt, err := s.buildPrompt(item, cr, cr.ID, "/tmp/rewrite.yaml", scopingError(), "")
	if err != nil {
		t.Fatalf("buildPrompt: %v", err)
	}

	for _, want := range []string{
		"Route validation failures on failure class",                                   // the change request itself
		"'escalation' was read as executor escalation",                                 // the validator diagnosis
		"A comprehension failure is one the same executor cannot fix by trying harder", // the spec excerpt
		"Every escalation path carries a configurable cap",                             // the ADR excerpt
		"## TASK\n\nRewrite the change request.",                                       // what the executor actually read
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("scoper prompt missing %q", want)
		}
	}
}

func TestScoperPrompt_InstructsARewriteRatherThanCode(t *testing.T) {
	store, cr := scopingFixture(t)
	s := &scoper{store: store, model: "opus"}

	prompt, err := s.buildPrompt(scopingItem(), cr, cr.ID, "/tmp/rewrite.yaml", scopingError(), "")
	if err != nil {
		t.Fatalf("buildPrompt: %v", err)
	}

	for _, want := range []string{
		"## DO NOT WRITE CODE",
		"You are not fixing the implementation",
		"You must not write to .utopia/specs/",
		"utopia cr validate",
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("scoper prompt missing instruction %q", want)
		}
	}
}

func TestLoadRewrite_PersistsProvenanceOnTheArtefact(t *testing.T) {
	store, cr := scopingFixture(t)
	s := &scoper{store: store}
	item := scopingItem()
	base := rewrittenCRBase(cr.ID, item.ScopingEscalationCount)
	abs := writeRewrite(t, store, base, validRewriteYAML)

	rewritten, err := s.loadRewrite(item, cr, base, abs, scopingError())
	if err != nil {
		t.Fatalf("loadRewrite: %v", err)
	}

	if rewritten.Rewrite == nil {
		t.Fatal("rewritten change request carries no rewrite block")
	}
	if rewritten.ID == cr.ID {
		t.Fatalf("rewrite kept the superseded change request's id %q, which would overwrite its file", cr.ID)
	}
	if rewritten.Rewrite.Supersedes != cr.ID {
		t.Errorf("Supersedes = %q, want %q", rewritten.Rewrite.Supersedes, cr.ID)
	}
	if rewritten.Rewrite.WorkItem != item.ID {
		t.Errorf("WorkItem = %q, want %q", rewritten.Rewrite.WorkItem, item.ID)
	}
	if len(rewritten.Rewrite.Diagnoses) != 1 || !strings.Contains(rewritten.Rewrite.Diagnoses[0], "executor escalation") {
		t.Errorf("Diagnoses = %v, want the validator diagnosis that motivated the rewrite", rewritten.Rewrite.Diagnoses)
	}

	// The provenance has to survive on disk, not only in memory: a git clone with
	// no database is all a later reader gets.
	reloaded, err := store.LoadChangeRequest(base)
	if err != nil {
		t.Fatalf("reload rewritten change request: %v", err)
	}
	if !reloaded.IsRewrite() || reloaded.Rewrite.Supersedes != cr.ID {
		t.Errorf("persisted rewrite provenance = %+v, want supersedes %q", reloaded.Rewrite, cr.ID)
	}
}

func TestLoadRewrite_RejectsWhatCannotBeResumedAgainst(t *testing.T) {
	tests := []struct {
		name string
		body string // empty means the scoper wrote no file at all
		want string
	}{
		{"no file", "", "wrote no change request file"},
		{"unparseable", "id: [broken\n", "could not be parsed"},
		{
			"invalid change request",
			"id: escalation-routing-rewrite-1\ntype: feature\ntitle: no changes\nstatus: draft\n",
			"not valid",
		},
		{
			"feature dropped",
			strings.Replace(validRewriteYAML, "id: scoping-escalation", "id: something-else", 1),
			"no longer describes feature",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store, cr := scopingFixture(t)
			s := &scoper{store: store}
			item := scopingItem()
			base := rewrittenCRBase(cr.ID, item.ScopingEscalationCount)
			abs := filepath.Join(store.ChangeRequestsDir(), base+".yaml")
			if tt.body != "" {
				abs = writeRewrite(t, store, base, tt.body)
			}

			_, err := s.loadRewrite(item, cr, base, abs, scopingError())
			if !errors.Is(err, &ScopingRewriteError{}) {
				t.Fatalf("err = %v, want a *ScopingRewriteError", err)
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("err = %q, want it to mention %q", err, tt.want)
			}
		})
	}
}

func TestResumeAgainst_ResetsComprehensionButNotEscalatedAttempts(t *testing.T) {
	store, cr := scopingFixture(t)
	s := &scoper{store: store}
	item := scopingItem()
	base := rewrittenCRBase(cr.ID, item.ScopingEscalationCount)
	abs := writeRewrite(t, store, base, validRewriteYAML)

	rewritten, err := s.loadRewrite(item, cr, base, abs, scopingError())
	if err != nil {
		t.Fatalf("loadRewrite: %v", err)
	}
	if err := resumeAgainst(item, rewritten, cr.ID, store, nil); err != nil {
		t.Fatalf("resumeAgainst: %v", err)
	}

	if item.ComprehensionCount != 0 {
		t.Errorf("ComprehensionCount = %d, want 0 - the text the executor misread has changed", item.ComprehensionCount)
	}
	if item.MechanicalRetryCount != 0 {
		t.Errorf("MechanicalRetryCount = %d, want 0", item.MechanicalRetryCount)
	}
	if item.OpusExecutionAttempts != 2 {
		t.Errorf("OpusExecutionAttempts = %d, want 2 - a scoping escalation does not refund escalated attempts", item.OpusExecutionAttempts)
	}
	if item.ScopingEscalationCount != 1 {
		t.Errorf("ScopingEscalationCount = %d, want 1 - the escalation is still spent", item.ScopingEscalationCount)
	}
	if item.LastValidatorFeedback != "" {
		t.Error("LastValidatorFeedback survived the rewrite, want it cleared - it is feedback about the old specification")
	}
	if !strings.Contains(item.Prompt, "not to write code") {
		t.Errorf("work item prompt was not re-derived from the rewritten change request:\n%s", item.Prompt)
	}

	// The reset has to be persisted, or a resume re-reads the counter that routed
	// the item to scoping escalation in the first place.
	saved, err := store.ListWorkItemsForSpec(cr.ID)
	if err != nil || len(saved) != 1 {
		t.Fatalf("reload work items: %v (got %d)", err, len(saved))
	}
	if saved[0].ComprehensionCount != 0 || saved[0].OpusExecutionAttempts != 2 {
		t.Errorf("persisted counters = comprehension %d, escalated attempts %d; want 0 and 2",
			saved[0].ComprehensionCount, saved[0].OpusExecutionAttempts)
	}
}

func TestListRewrittenChangeRequests_FindsTheArtefactForHarvest(t *testing.T) {
	store, cr := scopingFixture(t)
	s := &scoper{store: store}
	item := scopingItem()
	base := rewrittenCRBase(cr.ID, item.ScopingEscalationCount)
	abs := writeRewrite(t, store, base, validRewriteYAML)

	if _, err := s.loadRewrite(item, cr, base, abs, scopingError()); err != nil {
		t.Fatalf("loadRewrite: %v", err)
	}

	rewrites, err := store.ListRewrittenChangeRequests()
	if err != nil {
		t.Fatalf("ListRewrittenChangeRequests: %v", err)
	}
	if len(rewrites) != 1 {
		t.Fatalf("found %d rewritten change requests, want 1 (the original must not match)", len(rewrites))
	}
	if rewrites[0].Rewrite.Supersedes != cr.ID {
		t.Errorf("Supersedes = %q, want %q", rewrites[0].Rewrite.Supersedes, cr.ID)
	}
}

// A rewrite that produced nothing is still an escalation that was spent, so it
// counts against the cap - and when that charge exhausts the cap the item halts
// rather than resuming.
func TestExhaustedScoping_ChargesTheFailedRewriteAgainstTheCap(t *testing.T) {
	cause := &ScopingRewriteError{WorkItemID: "item", Reason: "the scoper wrote no change request file"}

	spent := exhaustedScoping(&domain.WorkItem{ID: "item", ScopingEscalationCount: 1}, testCaps(), cause)
	if spent == nil {
		t.Fatal("a failed rewrite at the cap did not halt the work item")
	}
	if spent.Cap != "escalation.scoping_escalations" {
		t.Errorf("Cap = %q, want the scoping cap named", spent.Cap)
	}
	if !errors.Is(spent, &ScopingRewriteError{}) {
		t.Error("the halt does not unwrap to the rewrite failure that caused it")
	}

	caps := testCaps()
	caps.ScopingEscalations = 2
	if remaining := exhaustedScoping(&domain.WorkItem{ID: "item", ScopingEscalationCount: 1}, caps, cause); remaining != nil {
		t.Errorf("halted at %d of %d escalations, want a rewrite still available", 1, caps.ScopingEscalations)
	}
}

func TestResolveScoperModel(t *testing.T) {
	tests := []struct {
		name string
		mc   *domain.ModelConfig
		want string
	}{
		{"unset falls through to the escalated executor default", nil, string(domain.ModelOpus)},
		{"scoper wins", &domain.ModelConfig{Scoper: "fable", ExecuteEscalated: "opus", Execute: "haiku"}, "fable"},
		{"falls back to the escalated executor", &domain.ModelConfig{ExecuteEscalated: "fable", Default: "haiku"}, "fable"},
		{"never falls back to models.execute", &domain.ModelConfig{Execute: "haiku", Default: "haiku"}, string(domain.ModelOpus)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := resolveScoperModel(tt.mc); got != tt.want {
				t.Errorf("resolveScoperModel() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestScopingDiagnoses_CarryCorrectedIntent(t *testing.T) {
	agg := &validators.AggregateResult{
		FailureClass: validators.FailureComprehension,
		Failures: []validators.ValidatorFailure{
			{ID: "spec-fidelity", Verdict: &validators.Verdict{
				Diagnosis:       "aggregated per validator",
				CorrectedIntent: "Aggregate across the phase.",
			}},
			{ID: "silent", Verdict: &validators.Verdict{}},
		},
	}

	got := scopingDiagnoses(agg)

	if len(got) != 1 {
		t.Fatalf("got %d diagnoses, want 1 - a validator with no diagnosis contributes nothing", len(got))
	}
	if !strings.Contains(got[0], "spec-fidelity: aggregated per validator") || !strings.Contains(got[0], "Aggregate across the phase.") {
		t.Errorf("diagnosis = %q, want it to carry both the diagnosis and the corrected intent", got[0])
	}
}

func TestPhaseIndexFromSpecID(t *testing.T) {
	tests := []struct {
		id        string
		want      int
		wantPhase bool
	}{
		{"12_initiative/phase-2", 2, true},
		{"12_initiative-phase-0.scoping-escalation", 0, true},
		{"escalation-routing", 0, false},
		{"escalation-routing.scoping-escalation", 0, false},
	}

	for _, tt := range tests {
		t.Run(tt.id, func(t *testing.T) {
			got, ok := phaseIndexFromSpecID(tt.id)
			if ok != tt.wantPhase || (ok && got != tt.want) {
				t.Errorf("phaseIndexFromSpecID(%q) = %d, %v; want %d, %v", tt.id, got, ok, tt.want, tt.wantPhase)
			}
		})
	}
}

// A scoper that copies the change request wholesale and forgets to change its id
// must not end up overwriting the change request it was meant to supersede.
func TestLoadRewrite_DoesNotOverwriteTheSupersededChangeRequest(t *testing.T) {
	store, cr := scopingFixture(t)
	s := &scoper{store: store}
	item := scopingItem()
	base := rewrittenCRBase(cr.ID, item.ScopingEscalationCount)
	abs := writeRewrite(t, store, base, strings.Replace(validRewriteYAML, "id: escalation-routing-rewrite-1", "id: escalation-routing", 1))

	if _, err := s.loadRewrite(item, cr, base, abs, scopingError()); err != nil {
		t.Fatalf("loadRewrite: %v", err)
	}

	original, err := store.LoadChangeRequest(cr.ID)
	if err != nil {
		t.Fatalf("reload superseded change request: %v", err)
	}
	if original.IsRewrite() || original.Title != cr.Title {
		t.Errorf("the superseded change request was overwritten by its own rewrite: %+v", original)
	}
}
