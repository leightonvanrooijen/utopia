package validators

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Verdict block delimiters. Utopia drives the claude CLI rather than the
// Anthropic API, so there is no response schema to enforce the verdict's shape.
// It travels in stdout inside these tags: a convention the validator prompt
// teaches and this file parses, with every parse failure resolving toward the
// more expensive classification rather than the cheaper one.
const (
	VerdictOpenTag  = "<VERDICT>"
	VerdictCloseTag = "</VERDICT>"
)

// Outcome is the pass/fail half of a verdict - the same question the <PASSED>
// token answered before the verdict block existed.
type Outcome string

const (
	// OutcomePass means the validator's check succeeded.
	OutcomePass Outcome = "pass"
	// OutcomeFail means the validator's check failed and carries a FailureClass.
	OutcomeFail Outcome = "fail"
)

// FailureClass distinguishes the two failures a caller can act on differently.
// The distinction is about intent, not severity.
type FailureClass string

const (
	// FailureMechanical means the intent was right and the execution slipped -
	// a missing import, a wrong signature. The same executor can retry.
	FailureMechanical FailureClass = "mechanical"
	// FailureComprehension means the intent was wrong - the specification was
	// misread. The same executor cannot fix this by trying harder.
	FailureComprehension FailureClass = "comprehension"
)

// strongerFailureClass returns the class a caller must act on when two failures
// disagree. Comprehension is the stronger class: acting on it when the failure
// was merely mechanical costs one iteration on a more expensive executor, while
// acting on mechanical when the intent was wrong sends the same executor back to
// re-derive the same misreading. An empty class contributes nothing, so folding
// over no failures leaves the result empty.
func strongerFailureClass(a, b FailureClass) FailureClass {
	switch {
	case a == FailureComprehension || b == FailureComprehension:
		return FailureComprehension
	case a == FailureMechanical || b == FailureMechanical:
		return FailureMechanical
	default:
		return ""
	}
}

// Confidence is how sure the validator is of the class it assigned. A validator
// that cannot tell the two apart is asked to report ConfidenceLow rather than
// guess, because a low-confidence failure is resolved as comprehension.
type Confidence string

const (
	// ConfidenceHigh means the reported failure_class is trusted as reported.
	ConfidenceHigh Confidence = "high"
	// ConfidenceLow means the validator could not confidently classify, so the
	// failure resolves to FailureComprehension whatever class it reported.
	ConfidenceLow Confidence = "low"
)

// Field length caps. They bound what a downstream prompt has to carry: a
// diagnosis is a sentence or two, a corrected intent a short paragraph. An
// over-length field is a formatting slip rather than a comprehension signal, so
// it is truncated to the cap instead of invalidating the classification.
const (
	// MaxDiagnosisChars caps Diagnosis.
	MaxDiagnosisChars = 400
	// MaxCorrectedIntentChars caps CorrectedIntent.
	MaxCorrectedIntentChars = 1500
)

// Verdict is the classification a validator emits alongside its pass/fail
// answer. It is what later phases route on: a mechanical failure retries on the
// same executor, a comprehension failure escalates, and a suspected spec defect
// escalates the scoping rather than the executor.
type Verdict struct {
	// Outcome is "pass" or "fail" (JSON field "verdict").
	Outcome Outcome `json:"verdict"`

	// FailureClass is empty (JSON null) when Outcome is pass and non-empty when
	// it is fail.
	FailureClass FailureClass `json:"failure_class"`

	// Diagnosis is why the check failed, at most MaxDiagnosisChars characters.
	Diagnosis string `json:"diagnosis"`

	// CorrectedIntent is what the executor should have understood the work item
	// to mean. It is required when FailureClass is comprehension, empty
	// otherwise, and at most MaxCorrectedIntentChars characters.
	CorrectedIntent string `json:"corrected_intent"`

	// Confidence is "high" or "low" on a failure.
	Confidence Confidence `json:"confidence"`

	// SpecDefectSuspected reports that the specification itself, rather than the
	// execution, looks wrong. Absent in the JSON means false: a validator that
	// says nothing is not suspecting anything.
	SpecDefectSuspected bool `json:"spec_defect_suspected"`
}

// Failed reports whether this verdict is a failure.
func (v *Verdict) Failed() bool {
	return v.Outcome == OutcomeFail
}

// InterpretOutput turns a validator's raw stdout into a Result. It is the single
// place the output contract is applied, so Run and runWithDiff cannot drift.
//
// The rules, in the order they are tried:
//
//  1. A parseable <VERDICT> block decides the outcome. It outranks <PASSED>,
//     because the structured verdict is the deliberate statement and, where the
//     two disagree, failing is the resolution that cannot silently ship a
//     violation.
//  2. A <VERDICT> block that does not parse, or that omits a required field, is
//     a comprehension failure - a validator that cannot state its verdict is not
//     evidence that the work was merely sloppy.
//  3. With no block at all, <PASSED> still passes. Validator prompts written
//     before this contract keep working unchanged.
//  4. Neither marker is a comprehension failure, for the same reason as (2).
//
// The returned Result always carries a non-nil Verdict, so callers routing on
// the classification never have to nil-check it.
func InterpretOutput(stdout string) *Result {
	verdict, err := ParseVerdict(stdout)
	switch {
	case err != nil:
		return failedResult(stdout, unclassifiedFailure(err.Error()))
	case verdict != nil:
		if verdict.Outcome == OutcomePass {
			return &Result{Passed: true, Verdict: verdict}
		}
		return failedResult(stdout, verdict)
	case strings.Contains(stdout, PassedToken):
		// A pre-contract validator: it passed and said nothing else. Confidence
		// is left unset because there is no classification to be confident about.
		return &Result{Passed: true, Verdict: &Verdict{Outcome: OutcomePass}}
	default:
		return failedResult(stdout, unclassifiedFailure(
			"validator output contained neither "+PassedToken+" nor a "+VerdictOpenTag+" block"))
	}
}

// failedResult builds the failing Result, which keeps handing back the whole
// output as feedback: the verdict adds a classification, it does not replace the
// prose the next iteration is given.
func failedResult(stdout string, verdict *Verdict) *Result {
	return &Result{Passed: false, Feedback: stdout, Verdict: verdict}
}

// unclassifiedFailure is the verdict for output that carried no usable one. It
// resolves to comprehension with low confidence: nothing in the output supports
// the cheaper class, and guessing mechanical would send the same executor back to
// re-derive the same wrong intent.
func unclassifiedFailure(reason string) *Verdict {
	return &Verdict{
		Outcome:      OutcomeFail,
		FailureClass: FailureComprehension,
		Diagnosis:    truncateChars(reason, MaxDiagnosisChars),
		Confidence:   ConfidenceLow,
	}
}

// ParseVerdict extracts and validates the <VERDICT> block from a validator's
// output. It has three outcomes:
//
//   - (verdict, nil) - a well-formed block, normalized against the contract.
//   - (nil, nil)     - no block present; the caller falls back to <PASSED>.
//   - (nil, err)     - a block that cannot be trusted; err explains why and the
//     caller resolves it as a comprehension failure.
func ParseVerdict(stdout string) (*Verdict, error) {
	body, found, err := extractVerdictBlock(stdout)
	if err != nil || !found {
		return nil, err
	}

	var v Verdict
	if err := json.Unmarshal([]byte(body), &v); err != nil {
		return nil, fmt.Errorf("verdict block is not valid JSON: %w", err)
	}
	if err := v.normalize(); err != nil {
		return nil, err
	}
	return &v, nil
}

// extractVerdictBlock returns the contents between the verdict tags. Exactly one
// block is the contract: none means a pre-contract validator (found is false),
// while two or more means there is no single verdict to route on, which is an
// error rather than a silent choice between them.
func extractVerdictBlock(stdout string) (body string, found bool, err error) {
	opens := strings.Count(stdout, VerdictOpenTag)
	switch {
	case opens == 0:
		return "", false, nil
	case opens > 1:
		return "", false, fmt.Errorf("validator output contains %d %s blocks, want exactly 1", opens, VerdictOpenTag)
	}

	start := strings.Index(stdout, VerdictOpenTag) + len(VerdictOpenTag)
	end := strings.Index(stdout[start:], VerdictCloseTag)
	if end < 0 {
		return "", false, fmt.Errorf("verdict block is missing its %s tag", VerdictCloseTag)
	}
	return stripCodeFence(stdout[start : start+end]), true, nil
}

// stripCodeFence removes a markdown code fence wrapping the JSON object. Models
// fence JSON by habit, and a fenced object is a formatting quirk rather than a
// broken verdict.
func stripCodeFence(body string) string {
	body = strings.TrimSpace(body)
	if !strings.HasPrefix(body, "```") {
		return body
	}
	nl := strings.IndexByte(body, '\n')
	if nl < 0 {
		return body
	}
	body = body[nl+1:]
	if closing := strings.LastIndex(body, "```"); closing >= 0 {
		body = body[:closing]
	}
	return strings.TrimSpace(body)
}

// normalize checks the parsed verdict against the contract and coerces the
// fields the contract fixes. A missing or invalid required field is an error, so
// the caller resolves the whole block as a comprehension failure.
//
// Validation runs against the class the validator reported, before the
// low-confidence promotion below, so a low-confidence mechanical failure is not
// held to comprehension's corrected_intent requirement it was never asked to
// meet.
func (v *Verdict) normalize() error {
	v.Diagnosis = strings.TrimSpace(v.Diagnosis)
	v.CorrectedIntent = strings.TrimSpace(v.CorrectedIntent)

	switch v.Outcome {
	case OutcomePass:
		if v.FailureClass != "" {
			return fmt.Errorf("verdict %q carries failure_class %q, which must be null on pass", OutcomePass, v.FailureClass)
		}
		// A pass classifies nothing, so anything the validator attached to it is
		// dropped rather than carried forward as routing input.
		v.CorrectedIntent = ""
		return nil
	case OutcomeFail:
		return v.normalizeFailure()
	case "":
		return fmt.Errorf("verdict block omits the verdict field")
	default:
		return fmt.Errorf("verdict is %q, want %q or %q", v.Outcome, OutcomePass, OutcomeFail)
	}
}

// normalizeFailure validates the fields a failing verdict must carry and applies
// the two coercions the contract mandates: corrected_intent is null for anything
// but comprehension, and a low-confidence failure is comprehension.
func (v *Verdict) normalizeFailure() error {
	switch v.FailureClass {
	case FailureMechanical, FailureComprehension:
	case "":
		return fmt.Errorf("verdict %q omits failure_class", OutcomeFail)
	default:
		return fmt.Errorf("failure_class is %q, want %q or %q", v.FailureClass, FailureMechanical, FailureComprehension)
	}

	if v.Diagnosis == "" {
		return fmt.Errorf("verdict %q omits diagnosis", OutcomeFail)
	}

	switch v.Confidence {
	case ConfidenceHigh, ConfidenceLow:
	case "":
		return fmt.Errorf("verdict %q omits confidence", OutcomeFail)
	default:
		return fmt.Errorf("confidence is %q, want %q or %q", v.Confidence, ConfidenceHigh, ConfidenceLow)
	}

	if v.FailureClass == FailureComprehension && v.CorrectedIntent == "" {
		return fmt.Errorf("verdict %q with failure_class %q omits corrected_intent", OutcomeFail, FailureComprehension)
	}
	if v.FailureClass == FailureMechanical {
		// The intent was right, so there is no corrected intent to carry.
		v.CorrectedIntent = ""
	}

	// The validator could not tell the classes apart. Mechanical is the class a
	// caller cannot recover from being wrong about - it retries the executor that
	// already misunderstood - so an unsure failure resolves as comprehension.
	if v.Confidence == ConfidenceLow {
		v.FailureClass = FailureComprehension
	}

	v.Diagnosis = truncateChars(v.Diagnosis, MaxDiagnosisChars)
	v.CorrectedIntent = truncateChars(v.CorrectedIntent, MaxCorrectedIntentChars)
	return nil
}

// truncateChars cuts s to at most max characters. It counts runes, not bytes, so
// the cap means characters as the contract states it and never splits one.
func truncateChars(s string, max int) string {
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max])
}
