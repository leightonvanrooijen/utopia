package validators

import (
	"strings"
	"testing"
)

func TestParseVerdict_WellFormed(t *testing.T) {
	tests := []struct {
		name   string
		output string
		want   Verdict
	}{
		{
			name: "pass with null failure_class",
			output: `Checked the diff.
<VERDICT>
{"verdict": "pass", "failure_class": null, "diagnosis": null, "corrected_intent": null, "confidence": "high", "spec_defect_suspected": false}
</VERDICT>`,
			want: Verdict{Outcome: OutcomePass, Confidence: ConfidenceHigh},
		},
		{
			name: "mechanical failure",
			output: `<VERDICT>
{"verdict": "fail", "failure_class": "mechanical", "diagnosis": "os/exec imported but unused", "corrected_intent": null, "confidence": "high", "spec_defect_suspected": false}
</VERDICT>`,
			want: Verdict{
				Outcome:      OutcomeFail,
				FailureClass: FailureMechanical,
				Diagnosis:    "os/exec imported but unused",
				Confidence:   ConfidenceHigh,
			},
		},
		{
			name: "comprehension failure carries corrected intent",
			output: `<VERDICT>
{"verdict": "fail", "failure_class": "comprehension", "diagnosis": "aggregates per validator, spec asks per phase", "corrected_intent": "Aggregate across the whole phase, not each validator.", "confidence": "high", "spec_defect_suspected": true}
</VERDICT>`,
			want: Verdict{
				Outcome:             OutcomeFail,
				FailureClass:        FailureComprehension,
				Diagnosis:           "aggregates per validator, spec asks per phase",
				CorrectedIntent:     "Aggregate across the whole phase, not each validator.",
				Confidence:          ConfidenceHigh,
				SpecDefectSuspected: true,
			},
		},
		{
			name: "spec_defect_suspected omitted defaults to false",
			output: `<VERDICT>
{"verdict": "fail", "failure_class": "mechanical", "diagnosis": "missing import", "confidence": "high"}
</VERDICT>`,
			want: Verdict{
				Outcome:      OutcomeFail,
				FailureClass: FailureMechanical,
				Diagnosis:    "missing import",
				Confidence:   ConfidenceHigh,
			},
		},
		{
			name:   "fenced JSON inside the block",
			output: "<VERDICT>\n```json\n{\"verdict\": \"fail\", \"failure_class\": \"mechanical\", \"diagnosis\": \"lint\", \"confidence\": \"high\"}\n```\n</VERDICT>",
			want: Verdict{
				Outcome:      OutcomeFail,
				FailureClass: FailureMechanical,
				Diagnosis:    "lint",
				Confidence:   ConfidenceHigh,
			},
		},
		{
			name: "unknown fields are tolerated",
			output: `<VERDICT>
{"verdict": "pass", "failure_class": null, "confidence": "high", "reviewed_files": 3}
</VERDICT>`,
			want: Verdict{Outcome: OutcomePass, Confidence: ConfidenceHigh},
		},
		{
			name: "corrected_intent on a mechanical failure is dropped",
			output: `<VERDICT>
{"verdict": "fail", "failure_class": "mechanical", "diagnosis": "typo", "corrected_intent": "not needed here", "confidence": "high"}
</VERDICT>`,
			want: Verdict{
				Outcome:      OutcomeFail,
				FailureClass: FailureMechanical,
				Diagnosis:    "typo",
				Confidence:   ConfidenceHigh,
			},
		},
		{
			name: "corrected_intent on a pass is dropped",
			output: `<VERDICT>
{"verdict": "pass", "corrected_intent": "leftover from a template", "confidence": "high"}
</VERDICT>`,
			want: Verdict{Outcome: OutcomePass, Confidence: ConfidenceHigh},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseVerdict(tt.output)
			if err != nil {
				t.Fatalf("ParseVerdict returned error: %v", err)
			}
			if got == nil {
				t.Fatal("ParseVerdict returned nil verdict")
			}
			if *got != tt.want {
				t.Errorf("verdict = %+v, want %+v", *got, tt.want)
			}
		})
	}
}

// A low-confidence failure resolves to comprehension whatever class it reported:
// the validator that cannot tell them apart must not send the same executor back
// to re-derive the same wrong intent.
func TestParseVerdict_LowConfidenceResolvesToComprehension(t *testing.T) {
	tests := []struct {
		name            string
		reportedClass   string
		correctedIntent string
		wantIntent      string
	}{
		{
			name:          "reported mechanical, no corrected intent to demand",
			reportedClass: "mechanical",
			wantIntent:    "",
		},
		{
			name:            "reported comprehension keeps its corrected intent",
			reportedClass:   "comprehension",
			correctedIntent: `"the runner should parse, not the aggregator"`,
			wantIntent:      "the runner should parse, not the aggregator",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			intent := "null"
			if tt.correctedIntent != "" {
				intent = tt.correctedIntent
			}
			output := `<VERDICT>{"verdict": "fail", "failure_class": "` + tt.reportedClass +
				`", "diagnosis": "unclear whether the intent or the execution is wrong", "corrected_intent": ` +
				intent + `, "confidence": "low"}</VERDICT>`

			got, err := ParseVerdict(output)
			if err != nil {
				t.Fatalf("ParseVerdict returned error: %v", err)
			}
			if got.FailureClass != FailureComprehension {
				t.Errorf("failure_class = %q, want %q", got.FailureClass, FailureComprehension)
			}
			if got.Confidence != ConfidenceLow {
				t.Errorf("confidence = %q, want %q (the reported confidence is preserved)", got.Confidence, ConfidenceLow)
			}
			if got.CorrectedIntent != tt.wantIntent {
				t.Errorf("corrected_intent = %q, want %q", got.CorrectedIntent, tt.wantIntent)
			}
		})
	}
}

func TestParseVerdict_Rejected(t *testing.T) {
	tests := []struct {
		name    string
		output  string
		wantErr string
	}{
		{
			name:    "malformed JSON",
			output:  `<VERDICT>{"verdict": "fail", oops}</VERDICT>`,
			wantErr: "not valid JSON",
		},
		{
			name:    "verdict field omitted",
			output:  `<VERDICT>{"failure_class": "mechanical", "diagnosis": "x", "confidence": "high"}</VERDICT>`,
			wantErr: "omits the verdict field",
		},
		{
			name:    "unknown verdict value",
			output:  `<VERDICT>{"verdict": "maybe"}</VERDICT>`,
			wantErr: `verdict is "maybe"`,
		},
		{
			name:    "fail omits failure_class",
			output:  `<VERDICT>{"verdict": "fail", "diagnosis": "x", "confidence": "high"}</VERDICT>`,
			wantErr: "omits failure_class",
		},
		{
			name:    "unknown failure_class value",
			output:  `<VERDICT>{"verdict": "fail", "failure_class": "cosmetic", "diagnosis": "x", "confidence": "high"}</VERDICT>`,
			wantErr: `failure_class is "cosmetic"`,
		},
		{
			name:    "fail omits diagnosis",
			output:  `<VERDICT>{"verdict": "fail", "failure_class": "mechanical", "confidence": "high"}</VERDICT>`,
			wantErr: "omits diagnosis",
		},
		{
			name:    "fail omits confidence",
			output:  `<VERDICT>{"verdict": "fail", "failure_class": "mechanical", "diagnosis": "x"}</VERDICT>`,
			wantErr: "omits confidence",
		},
		{
			name:    "unknown confidence value",
			output:  `<VERDICT>{"verdict": "fail", "failure_class": "mechanical", "diagnosis": "x", "confidence": "medium"}</VERDICT>`,
			wantErr: `confidence is "medium"`,
		},
		{
			name:    "comprehension omits corrected_intent",
			output:  `<VERDICT>{"verdict": "fail", "failure_class": "comprehension", "diagnosis": "x", "confidence": "high"}</VERDICT>`,
			wantErr: "omits corrected_intent",
		},
		{
			name:    "pass carries a failure_class",
			output:  `<VERDICT>{"verdict": "pass", "failure_class": "mechanical", "confidence": "high"}</VERDICT>`,
			wantErr: "must be null on pass",
		},
		{
			name:    "two blocks leave no single verdict",
			output:  `<VERDICT>{"verdict": "pass"}</VERDICT> and also <VERDICT>{"verdict": "fail"}</VERDICT>`,
			wantErr: "want exactly 1",
		},
		{
			name:    "unterminated block",
			output:  `<VERDICT>{"verdict": "pass"}`,
			wantErr: "missing its </VERDICT> tag",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseVerdict(tt.output)
			if err == nil {
				t.Fatalf("ParseVerdict succeeded, want error; got %+v", got)
			}
			if got != nil {
				t.Errorf("verdict = %+v, want nil alongside the error", got)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error = %q, want it to contain %q", err, tt.wantErr)
			}
		})
	}
}

func TestParseVerdict_NoBlock(t *testing.T) {
	for _, output := range []string{"", "Looks fine to me. <PASSED>", "Found three problems."} {
		got, err := ParseVerdict(output)
		if err != nil {
			t.Errorf("ParseVerdict(%q) returned error: %v", output, err)
		}
		if got != nil {
			t.Errorf("ParseVerdict(%q) = %+v, want nil (no block present)", output, got)
		}
	}
}

func TestParseVerdict_TruncatesOverLongFields(t *testing.T) {
	long := strings.Repeat("é", 2000) // multi-byte, so a byte-wise cut would split a rune

	output := `<VERDICT>{"verdict": "fail", "failure_class": "comprehension", "diagnosis": "` + long +
		`", "corrected_intent": "` + long + `", "confidence": "high"}</VERDICT>`

	got, err := ParseVerdict(output)
	if err != nil {
		t.Fatalf("ParseVerdict returned error: %v", err)
	}
	if n := len([]rune(got.Diagnosis)); n != MaxDiagnosisChars {
		t.Errorf("diagnosis length = %d characters, want %d", n, MaxDiagnosisChars)
	}
	if n := len([]rune(got.CorrectedIntent)); n != MaxCorrectedIntentChars {
		t.Errorf("corrected_intent length = %d characters, want %d", n, MaxCorrectedIntentChars)
	}
	if strings.ContainsRune(got.Diagnosis, '�') {
		t.Error("diagnosis was cut mid-rune")
	}
}

func TestInterpretOutput(t *testing.T) {
	tests := []struct {
		name             string
		output           string
		wantPassed       bool
		wantFeedback     bool
		wantOutcome      Outcome
		wantClass        FailureClass
		wantDiagnosisSub string
	}{
		{
			name:        "verdict pass",
			output:      `<VERDICT>{"verdict": "pass", "confidence": "high"}</VERDICT>`,
			wantPassed:  true,
			wantOutcome: OutcomePass,
		},
		{
			name: "PASSED with no verdict block still passes",
			// Validator prompts written before this contract keep working.
			output:      "Everything checks out. <PASSED>",
			wantPassed:  true,
			wantOutcome: OutcomePass,
		},
		{
			name:         "verdict fail keeps the full output as feedback",
			output:       `Detail about the failure. <VERDICT>{"verdict": "fail", "failure_class": "mechanical", "diagnosis": "unused import", "confidence": "high"}</VERDICT>`,
			wantPassed:   false,
			wantFeedback: true,
			wantOutcome:  OutcomeFail,
			wantClass:    FailureMechanical,
		},
		{
			name:             "neither marker is a comprehension failure",
			output:           "I reviewed the diff and have some thoughts.",
			wantPassed:       false,
			wantFeedback:     true,
			wantOutcome:      OutcomeFail,
			wantClass:        FailureComprehension,
			wantDiagnosisSub: "neither <PASSED>",
		},
		{
			name:             "empty output is a comprehension failure",
			output:           "",
			wantPassed:       false,
			wantOutcome:      OutcomeFail,
			wantClass:        FailureComprehension,
			wantDiagnosisSub: "neither <PASSED>",
		},
		{
			name:             "unparseable block is a comprehension failure",
			output:           `<VERDICT>{"verdict": "fail", truncated`,
			wantPassed:       false,
			wantFeedback:     true,
			wantOutcome:      OutcomeFail,
			wantClass:        FailureComprehension,
			wantDiagnosisSub: "missing its </VERDICT> tag",
		},
		{
			name:             "block omitting a required field is a comprehension failure",
			output:           `<VERDICT>{"verdict": "fail", "failure_class": "mechanical", "diagnosis": "x"}</VERDICT>`,
			wantPassed:       false,
			wantFeedback:     true,
			wantOutcome:      OutcomeFail,
			wantClass:        FailureComprehension,
			wantDiagnosisSub: "omits confidence",
		},
		{
			name: "a fail verdict outranks a PASSED token",
			// Where the two disagree, failing is the resolution that cannot
			// silently ship a violation.
			output:       `<PASSED> <VERDICT>{"verdict": "fail", "failure_class": "mechanical", "diagnosis": "unused import", "confidence": "high"}</VERDICT>`,
			wantPassed:   false,
			wantFeedback: true,
			wantOutcome:  OutcomeFail,
			wantClass:    FailureMechanical,
		},
		{
			name:             "an unusable block outranks a PASSED token",
			output:           "<PASSED> <VERDICT>not json</VERDICT>",
			wantPassed:       false,
			wantFeedback:     true,
			wantOutcome:      OutcomeFail,
			wantClass:        FailureComprehension,
			wantDiagnosisSub: "not valid JSON",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := InterpretOutput(tt.output)

			if got.Passed != tt.wantPassed {
				t.Errorf("Passed = %v, want %v", got.Passed, tt.wantPassed)
			}
			if tt.wantFeedback && got.Feedback != tt.output {
				t.Errorf("Feedback = %q, want the full output", got.Feedback)
			}
			if !tt.wantFeedback && got.Feedback != "" {
				t.Errorf("Feedback = %q, want empty", got.Feedback)
			}
			if got.Verdict == nil {
				t.Fatal("Verdict is nil; it must always be populated")
			}
			if got.Verdict.Outcome != tt.wantOutcome {
				t.Errorf("verdict = %q, want %q", got.Verdict.Outcome, tt.wantOutcome)
			}
			if got.Verdict.FailureClass != tt.wantClass {
				t.Errorf("failure_class = %q, want %q", got.Verdict.FailureClass, tt.wantClass)
			}
			if tt.wantDiagnosisSub != "" && !strings.Contains(got.Verdict.Diagnosis, tt.wantDiagnosisSub) {
				t.Errorf("diagnosis = %q, want it to contain %q", got.Verdict.Diagnosis, tt.wantDiagnosisSub)
			}
			if n := len([]rune(got.Verdict.Diagnosis)); n > MaxDiagnosisChars {
				t.Errorf("diagnosis length = %d characters, want at most %d", n, MaxDiagnosisChars)
			}
		})
	}
}

func TestVerdict_Failed(t *testing.T) {
	if (&Verdict{Outcome: OutcomePass}).Failed() {
		t.Error("a pass verdict reports Failed")
	}
	if !(&Verdict{Outcome: OutcomeFail}).Failed() {
		t.Error("a fail verdict does not report Failed")
	}
}
