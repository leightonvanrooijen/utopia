package ralph

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/leightonvanrooijen/utopia/internal"
	"github.com/leightonvanrooijen/utopia/internal/chunk"
	"github.com/leightonvanrooijen/utopia/internal/domain"
	"github.com/leightonvanrooijen/utopia/internal/ui"
	"github.com/leightonvanrooijen/utopia/internal/validators"
)

// scoperTools is what the scoper is allowed to do. It reads freely - the specs,
// the ADRs, the code the executor wrote - and writes exactly one thing: the
// rewritten change request. Bash is limited to the change request validator so
// the scoper can check its own output before handing it back, which is the same
// contract the cr command already gives the change request agent.
//
// Write is granted rather than withheld because the rewritten change request is
// the artefact, and the loop resumes against a file on disk rather than against
// something parsed out of a transcript. The specs directory is off limits, and
// that is stated in the prompt: specs are only updated by merge, and a scoper
// that edits them directly breaks the merge invariant.
var scoperTools = []string{"Read", "Glob", "Grep", "Write", "Bash(utopia cr validate:*)"}

// maxScoperADRs bounds how many ADRs are quoted to the scoper. The excerpt is
// context for a rewrite, not an index: a scoper handed every ADR in the project
// reads none of them carefully. It can still open the rest with Read.
const maxScoperADRs = 3

// ScopingRewriteError reports a scoping escalation that produced no change
// request the loop could resume against - no file, unparseable YAML, a change
// request that fails validation, or one that dropped the feature the work item
// implements.
//
// It is typed so the caller can tell it apart from the routing decision that
// asked for the rewrite: the escalation was spent (it counts against
// scoping_escalations either way) and there is nothing to resume against.
type ScopingRewriteError struct {
	// WorkItemID is the item whose change request was being rewritten.
	WorkItemID string
	// Path is where the rewritten change request was expected.
	Path string
	// Reason says what was wrong with what came back, in the scoper's terms.
	Reason string
	// Cause is the underlying parse or validation failure, when there was one.
	Cause error
}

func (e *ScopingRewriteError) Error() string {
	msg := fmt.Sprintf("scoping escalation for work item %s produced no usable change request at %s: %s",
		e.WorkItemID, e.Path, e.Reason)
	if e.Cause != nil {
		return msg + ": " + e.Cause.Error()
	}
	return msg
}

// Unwrap exposes the parse or validation failure underneath.
func (e *ScopingRewriteError) Unwrap() error { return e.Cause }

// Is allows errors.Is to match any ScopingRewriteError.
func (e *ScopingRewriteError) Is(target error) bool {
	_, ok := target.(*ScopingRewriteError)
	return ok
}

// resolveScoperModel determines which model rewrites a change request. Priority:
// models.scoper > models.execute_escalated > opus. It never falls through to
// models.execute or models.default: the scoper is asked what the change request
// should have said, which is a harder question than the one the executor already
// failed to answer, so it cannot run on a weaker model than the escalated
// executor.
func resolveScoperModel(mc *domain.ModelConfig) string {
	return mc.ScoperModel()
}

// scoper rewrites a change request that the executor keeps misreading. It is the
// only path in the loop that changes the specification rather than the code.
type scoper struct {
	// out receives the escalation's status lines. Nil means the process's own
	// streams.
	out   *ui.Printer
	store *internal.YAMLStore
	// cli is the loop's CLI, cloned per invocation so the scoper's model and tool
	// whitelist do not leak into the executor's next attempt.
	cli   *internal.CLI
	model string
	// effort is the reasoning depth the rewrite runs at, resolved with the rest of
	// the run's roles before any work item started.
	effort string
	// standards is the index injected into a rebuilt work item prompt, so a
	// resumed item carries the same standards section a freshly chunked one does.
	standards []domain.StandardsDocMeta
}

// escalateScoping runs one scoping escalation for a work item and, when it
// yields a usable change request, resumes execution against it. It returns nil
// exactly when the caller should carry on looping.
//
// The rewrite is persisted as a change request artefact in
// .utopia/change-requests/ before the loop reads it back, so a run that produces
// a sharper specification has produced something durable even if it produces no
// working code. Nothing is held in memory that a later merge or harvest cannot
// find on disk.
//
// A rewrite that produces nothing usable does not resume execution. The
// escalation was charged against scoping_escalations before it ran, so it counts
// either way; when that charge exhausts the cap the item halts as needs_human,
// because rewriting the change request is the last path available and there is
// nothing beyond it to route to.
func escalateScoping(
	ctx context.Context,
	s *scoper,
	item *domain.WorkItem,
	specID, crID string,
	esc *ScopingEscalationError,
	caps EscalationCaps,
	rec *runRecorder,
	finalDiff string,
) error {
	appendScopingEscalation(&rec.transcript, esc)

	rewritten, err := s.rewrite(ctx, item, crID, esc, finalDiff)
	if err == nil {
		err = resumeAgainst(item, rewritten, specID, s.store, s.standards)
	}
	if err != nil {
		ui.OrDefault(s.out).Progressf("  Scoping escalation produced no change request to resume against: %v\n", err)
		fmt.Fprintf(&rec.transcript, "\nScoping escalation failed: %v\n", err)
		if halt := exhaustedScoping(item, caps, err); halt != nil {
			return haltNeedsHuman(s.store, specID, crID, item, rec, halt)
		}
		_ = s.store.SaveWorkItemForSpec(specID, item)
		writeRunTranscript(s.store, crID, item, rec, domain.RunFailed)
		return err
	}

	// opus_execution_attempts is deliberately not reset, and is reported so the
	// operator can see it carrying across: it bounds total spend on the expensive
	// path, and resetting it here would turn rewrite-then-retry into an unbounded
	// loop through a different door.
	ui.OrDefault(s.out).Progressf("  Scoping escalation: resuming against %s (comprehension_count reset to 0, opus_execution_attempts still %d/%d)\n",
		rewritten.ID, item.OpusExecutionAttempts, caps.OpusExecutionAttempts)
	fmt.Fprintf(&rec.transcript, "\nChange request rewritten as %s; execution resumes against it.\n", rewritten.ID)
	return nil
}

// exhaustedScoping reports the halt for a scoping escalation that produced
// nothing to resume against, or nil when the change request may still be
// rewritten again.
//
// The escalation is charged before it runs, so an attempt that produced nothing
// has still been spent - which is the whole point of charging it up front. When
// that charge reaches the cap there is nothing beyond it to route to: rewriting
// the change request is the last path the loop has.
func exhaustedScoping(item *domain.WorkItem, caps EscalationCaps, cause error) *NeedsHumanError {
	if item.ScopingEscalationCount < caps.ScopingEscalations {
		return nil
	}
	return &NeedsHumanError{
		WorkItemID: item.ID,
		Cap:        "escalation.scoping_escalations",
		Limit:      caps.ScopingEscalations,
		Detail: fmt.Sprintf("%d scoping escalation(s) did not produce a change request the executor could satisfy",
			item.ScopingEscalationCount),
		Cause: cause,
	}
}

// appendScopingEscalation records the escalation on the run transcript. The
// transcript is what harvest reads, and a run that ended in a rewrite is exactly
// the run worth reading: the diagnoses below are usually a missing domain term
// or an undocumented decision, stated as a complaint about a specification.
func appendScopingEscalation(transcript *strings.Builder, esc *ScopingEscalationError) {
	fmt.Fprintf(transcript, "\n## SCOPING ESCALATION\n\n%s\n\n", esc.Error())
	for _, d := range esc.Diagnoses {
		fmt.Fprintf(transcript, "- %s\n", d)
	}
}

// rewrite sends the change request, the diagnoses and the surrounding spec and
// ADR excerpts to the scoper, and reads back what it wrote. The evidence it is
// given is the same escalation context an escalated execution attempt gets, built
// by the same rules: conclusions and evidence, never the failed attempts.
func (s *scoper) rewrite(
	ctx context.Context,
	item *domain.WorkItem,
	crID string,
	esc *ScopingEscalationError,
	finalDiff string,
) (*domain.ChangeRequest, error) {
	cr, err := s.store.LoadChangeRequest(crID)
	if err != nil {
		return nil, fmt.Errorf("failed to load change request %s for scoping escalation: %w", crID, err)
	}

	base := rewrittenCRBase(crID, item.ScopingEscalationCount)
	abs := filepath.Join(s.store.ChangeRequestsDir(), base+".yaml")

	prompt, err := s.buildPrompt(item, cr, crID, abs, esc, finalDiff)
	if err != nil {
		return nil, err
	}

	ui.OrDefault(s.out).Progressf("  Scoping escalation: rewriting the change request on %s (effort %s)...\n", s.model, s.effort)
	// The scoper is an invocation the execution loop makes, like the executor
	// attempt and the after-phase fix, so its accounting is captured too; only
	// validators and discovery stay on prose output.
	if _, err := s.cli.Clone().WithModel(s.model).WithEffort(s.effort).WithAllowedTools(scoperTools).WithUsageCapture(true).Prompt(ctx, prompt); err != nil {
		return nil, fmt.Errorf("scoper invocation failed for work item %s: %w", item.ID, err)
	}

	rewritten, err := s.loadRewrite(item, cr, base, abs, esc)
	if err != nil {
		return nil, err
	}
	return rewritten, nil
}

// loadRewrite reads back what the scoper wrote and decides whether it is
// something the loop can resume against. Every rejection here is the same
// outcome for the caller - the escalation is spent and execution does not resume
// - so they are reported as one error type carrying what was wrong.
//
// The provenance is stamped by the loop rather than trusted from the scoper. The
// loop knows exactly which change request was superseded and which diagnoses
// motivated the rewrite; a scoper that omitted or misremembered either would
// leave the artefact untraceable, and this is the one part of the file that does
// not depend on the model getting anything right.
func (s *scoper) loadRewrite(item *domain.WorkItem, cr *domain.ChangeRequest, base, abs string, esc *ScopingEscalationError) (*domain.ChangeRequest, error) {
	if _, err := os.Stat(abs); err != nil {
		return nil, &ScopingRewriteError{WorkItemID: item.ID, Path: abs, Reason: "the scoper wrote no change request file"}
	}

	rewritten, err := s.store.LoadChangeRequest(base)
	if err != nil {
		return nil, &ScopingRewriteError{WorkItemID: item.ID, Path: abs, Reason: "the change request could not be parsed", Cause: err}
	}

	// The id is stamped, not read. A scoper that copied the change request and
	// forgot to change its id would otherwise have the save below resolve back to
	// the superseded change request's own file and overwrite the very thing the
	// rewrite supersedes.
	rewritten.ID = rewrittenCRBase(cr.ID, item.ScopingEscalationCount)

	rewritten.Rewrite = &domain.ScopingRewrite{
		Supersedes: cr.ID,
		WorkItem:   item.ID,
		Diagnoses:  esc.Diagnoses,
	}
	if len(rewritten.Rewrite.Diagnoses) == 0 {
		// A spec defect suspected with no diagnosis behind it still has a reason,
		// and the artefact has to carry one: an empty diagnoses list would fail the
		// change request validation the merge path also runs.
		rewritten.Rewrite.Diagnoses = []string{esc.Error()}
	}

	// Validated with the same function `utopia cr validate` uses, so a malformed
	// rewrite fails here rather than at merge, when the run is long over.
	if err := domain.ValidateChangeRequest(rewritten); err != nil {
		return nil, &ScopingRewriteError{WorkItemID: item.ID, Path: abs, Reason: "the change request is not valid", Cause: err}
	}

	if chunk.SpecForFeature(allChanges(rewritten), allTasks(rewritten), featureIDOf(item)) == "" &&
		!hasFeature(rewritten, featureIDOf(item)) {
		return nil, &ScopingRewriteError{
			WorkItemID: item.ID,
			Path:       abs,
			Reason: fmt.Sprintf("the change request no longer describes feature %q, so there is nothing to resume against",
				featureIDOf(item)),
		}
	}

	// Written back so the stamped provenance is on disk, which is where harvest
	// and any later reader look for it.
	if err := s.store.SaveChangeRequest(rewritten); err != nil {
		return nil, fmt.Errorf("failed to persist rewritten change request %s: %w", rewritten.ID, err)
	}
	return rewritten, nil
}

// resumeAgainst re-points a work item at the rewritten change request: its
// prompt, title and constraints are re-derived from the rewritten feature by the
// same chunking that built them originally, so the executor's next attempt reads
// the new specification rather than the one it kept misreading.
//
// The counters that measure the executor's grasp of the old text are cleared -
// comprehension_count, because the text changed, and the mechanical streak,
// because it counts slips in a row against one specification. The counters that
// bound spend are not: opus_execution_attempts and scoping_escalations both
// carry across, which is what keeps the rewrite path bounded.
func resumeAgainst(item *domain.WorkItem, rewritten *domain.ChangeRequest, specID string, store *internal.YAMLStore, standards []domain.StandardsDocMeta) error {
	rebuilt, err := chunkRewritten(rewritten, specID, store, standards)
	if err != nil {
		return err
	}

	featureID := featureIDOf(item)
	for _, candidate := range rebuilt {
		if featureIDOf(candidate) != featureID {
			continue
		}
		item.Title = candidate.Title
		item.Prompt = candidate.Prompt
		item.Constraints = candidate.Constraints
		item.ComprehensionCount = 0
		item.MechanicalRetryCount = 0
		item.LastFailureOutput = ""
		item.LastValidatorFeedback = ""
		// The conclusions were reached about a text that no longer exists, so they
		// are dropped with the counters that measured it. They survive on the
		// rewrite's provenance, which is where a reader looks for why it was written.
		item.FailureConclusions = nil
		// The rewrite took effect, which is the fact the routing record reports as
		// spec_rewritten. It is set here rather than from ScopingEscalationCount
		// because that counter includes rewrites that produced nothing usable, and the
		// hypothesis under test - that rewriting the change request helps - can only be
		// argued from rewrites execution actually ran against.
		item.SpecRewritten = true
		item.Status = domain.WorkItemPending
		return store.SaveWorkItemForSpec(specID, item)
	}

	return &ScopingRewriteError{
		WorkItemID: item.ID,
		Path:       rewritten.ID,
		Reason:     fmt.Sprintf("no work item could be chunked for feature %q from the rewritten change request", featureID),
	}
}

// chunkRewritten runs the rewritten change request through chunking, picking the
// phase-scoped path for an initiative so a rewritten phase produces the same
// shape of work item the original phase did.
func chunkRewritten(rewritten *domain.ChangeRequest, specID string, store *internal.YAMLStore, standards []domain.StandardsDocMeta) ([]*domain.WorkItem, error) {
	phaseIndex, isPhase := phaseIndexFromSpecID(specID)
	if !isPhase {
		items, err := chunk.Chunk(rewritten, store.LoadSpec, standards, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to chunk rewritten change request %s: %w", rewritten.ID, err)
		}
		return items, nil
	}

	if phaseIndex < 0 || phaseIndex >= len(rewritten.Phases) {
		return nil, &ScopingRewriteError{
			Path:   rewritten.ID,
			Reason: fmt.Sprintf("the rewritten change request has no phase %d to resume", phaseIndex+1),
		}
	}
	items, err := chunk.ChunkPhase(extractCRID(specID), phaseIndex, &rewritten.Phases[phaseIndex], store.LoadSpec, standards, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to chunk phase %d of rewritten change request %s: %w", phaseIndex+1, rewritten.ID, err)
	}
	return items, nil
}

// buildPrompt composes what the scoper is sent: the work item that failed, and
// then the same escalation context an escalated execution attempt is built - the
// change request as it stands, the spec and ADR excerpts, each failure's
// conclusion, and the evidence of what broke. A scoping escalation is an
// escalation, so it is built by the same rules: no prior-attempt transcript.
//
// The instruction is the load-bearing part. A model handed a failing work item
// and a diagnosis will write code unless told not to; the whole point of this
// path is that the code is not what is wrong.
func (s *scoper) buildPrompt(item *domain.WorkItem, cr *domain.ChangeRequest, crID, abs string, esc *ScopingEscalationError, finalDiff string) (string, error) {
	ec, err := buildEscalationContext(s.store, item, cr, crID, finalDiff, esc.Diagnoses)
	if err != nil {
		return "", err
	}

	var b strings.Builder
	fmt.Fprintf(&b, `You are a Scoper - you rewrite a change request that an executor could not
implement correctly, so that the next attempt reads a specification it can.

Work item %q has failed validation repeatedly, and the failures were classified
as comprehension failures rather than mechanical ones. That is evidence about
the change request, not only about the executor: the text below was read
carefully and still produced the wrong implementation.

## DO NOT WRITE CODE

You are not fixing the implementation. Do not edit, create, or delete any source
file, and do not run the build or the tests. Your entire output is one rewritten
change request file.

You must not write to .utopia/specs/. Specs are only ever updated when a change
request merges, and a scoper that edits them directly breaks that invariant.

## THE WORK ITEM THAT FAILED

- id: %s
- feature: %s
- title: %s

This is the prompt the executor read, which is what the change request became
after chunking:

`+"```\n%s\n```"+`

## WHY IT FAILED

%s
`,
		item.ID, item.ID, featureIDOf(item), strings.TrimSpace(item.Title), strings.TrimSpace(item.Prompt), escalationReason(esc))

	ec.render(&b)

	fmt.Fprintf(&b, `
## WHAT TO WRITE

Write one YAML file to:

%s

It is a complete change request in the same format as the one above - same type,
same structure - not a diff against it. Rules:

1. Keep the feature id %q exactly. Execution resumes against that feature, and a
   rewrite that renames or drops it cannot be resumed.
2. Set id to %q.
3. Rewrite the feature's description and acceptance criteria so the misreading
   above is impossible. Say what the diagnoses show was missing: name the term
   that was ambiguous, state the constraint that was implied, make the criterion
   that was misread checkable.
4. Leave the other features alone unless the diagnoses show they are ambiguous
   in the same way.
5. Do not add acceptance criteria describing how to implement the change. A
   criterion states what must be true afterwards.

Then validate what you wrote:

    utopia cr validate %s

Fix anything it reports and re-run it until it passes. Print a one-paragraph
summary of what you changed and why when you are done.
`, abs, featureIDOf(item), rewrittenCRBase(cr.ID, item.ScopingEscalationCount), abs)

	return b.String(), nil
}

// escalationReason states why this work item reached the scoping path at all. The
// diagnoses behind it are rendered by the shared escalation context, so this is
// the framing rather than the evidence: whether a validator doubted the
// specification outright, or the executor simply kept misreading it.
func escalationReason(esc *ScopingEscalationError) string {
	if esc.SpecDefectSuspected {
		return "A validator suspects the specification itself is wrong, not the implementation."
	}
	return fmt.Sprintf("The work item has failed comprehension validation %d time(s).", esc.ComprehensionCount)
}

// rewrittenCRBase names a rewrite after what it supersedes: the change request's
// filename gives the file it is written to, and the change request's id gives its
// internal id. The two are passed separately because they differ - a filename
// may carry the numeric ordering prefix that the id deliberately does not.
//
// The escalation number is in the name so a second rewrite, on a project that
// raised the scoping cap, never overwrites the first.
func rewrittenCRBase(name string, escalation int) string {
	return fmt.Sprintf("%s-rewrite-%d", name, escalation)
}

// featureIDOf returns the feature a work item implements. A work item's SpecRef
// is "<chunking scope>.<feature id>", and the chunking scope is the change
// request (or one of its phases), not a spec.
func featureIDOf(item *domain.WorkItem) string {
	if i := strings.LastIndex(item.SpecRef, "."); i != -1 {
		return item.SpecRef[i+1:]
	}
	return item.SpecRef
}

// phaseOf returns the phase index a work item belongs to, or -1 when its change
// request is not an initiative.
func phaseOf(item *domain.WorkItem) int {
	idx, ok := phaseIndexFromSpecID(item.SpecRef)
	if !ok {
		return -1
	}
	return idx
}

// phaseIndexFromSpecID reads the phase index out of a phase-scoped identifier -
// the "<cr-id>/phase-<n>" a phase's work items are stored under, or the
// "<cr-id>-phase-<n>.<feature>" its work items carry as SpecRef.
func phaseIndexFromSpecID(id string) (int, bool) {
	for _, sep := range []string{"/phase-", "-phase-"} {
		i := strings.LastIndex(id, sep)
		if i == -1 {
			continue
		}
		rest := id[i+len(sep):]
		if dot := strings.Index(rest, "."); dot != -1 {
			rest = rest[:dot]
		}
		n, err := strconv.Atoi(rest)
		if err != nil {
			continue
		}
		return n, true
	}
	return 0, false
}

// changesForPhase and tasksForPhase narrow a change request to the phase a work
// item came from, so a feature id is looked up against the phase that defines it
// rather than against a sibling phase that happens to reuse the name.
func changesForPhase(cr *domain.ChangeRequest, phase int) []domain.Change {
	if phase >= 0 && phase < len(cr.Phases) {
		return cr.Phases[phase].Changes
	}
	return cr.Changes
}

func tasksForPhase(cr *domain.ChangeRequest, phase int) []domain.Task {
	if phase >= 0 && phase < len(cr.Phases) {
		return cr.Phases[phase].Tasks
	}
	return cr.Tasks
}

// allChanges and allTasks flatten a change request across its phases, for the
// checks that only ask whether a feature survived the rewrite at all.
func allChanges(cr *domain.ChangeRequest) []domain.Change {
	changes := append([]domain.Change(nil), cr.Changes...)
	for _, p := range cr.Phases {
		changes = append(changes, p.Changes...)
	}
	return changes
}

func allTasks(cr *domain.ChangeRequest) []domain.Task {
	tasks := append([]domain.Task(nil), cr.Tasks...)
	for _, p := range cr.Phases {
		tasks = append(tasks, p.Tasks...)
	}
	return tasks
}

// hasFeature reports whether a change request still describes featureID. It
// backs up SpecForFeature, which returns "" both for a missing feature and for a
// feature whose change names no spec.
func hasFeature(cr *domain.ChangeRequest, featureID string) bool {
	for _, t := range allTasks(cr) {
		if t.ID == featureID {
			return true
		}
	}
	for _, c := range allChanges(cr) {
		if c.Feature != nil && c.Feature.ID == featureID {
			return true
		}
		if c.FeatureID != "" && (featureID == "modify-"+c.FeatureID || featureID == "remove-"+c.FeatureID) {
			return true
		}
		if c.Spec != "" && featureID == "delete-spec-"+c.Spec {
			return true
		}
	}
	return false
}

// termsOf lowercases a blob of prose into the set of words long enough to carry
// meaning. Short words are dropped because they match everything.
func termsOf(s string) map[string]bool {
	terms := map[string]bool{}
	for _, field := range strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !('a' <= r && r <= 'z') && !('0' <= r && r <= '9')
	}) {
		if len(field) > 3 {
			terms[field] = true
		}
	}
	return terms
}

// overlap counts how many of a's terms appear in b.
func overlap(a, b map[string]bool) int {
	n := 0
	for term := range a {
		if b[term] {
			n++
		}
	}
	return n
}

// scopingDiagnoses formats the failing validators' diagnoses for the scoper,
// pairing each diagnosis with the corrected intent when the validator supplied
// one. It is what ScopingEscalationError carries into the rewrite.
func scopingDiagnoses(agg *validators.AggregateResult) []string {
	if agg == nil {
		return nil
	}
	var out []string
	for _, f := range agg.Failures {
		if f.Verdict == nil || f.Verdict.Diagnosis == "" {
			continue
		}
		line := f.ID + ": " + f.Verdict.Diagnosis
		if f.Verdict.CorrectedIntent != "" {
			line += " (corrected intent: " + f.Verdict.CorrectedIntent + ")"
		}
		out = append(out, line)
	}
	return out
}
