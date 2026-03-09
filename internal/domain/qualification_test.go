package domain

import (
	"strings"
	"testing"
)

func TestSpecQualificationCriteria_Qualifications(t *testing.T) {
	criteria := SpecQualificationCriteria{}
	qualifications := criteria.Qualifications()

	if len(qualifications) != 4 {
		t.Errorf("expected 4 qualifications, got %d", len(qualifications))
	}

	// Verify key concepts are covered
	allText := strings.Join(qualifications, " ")
	requiredConcepts := []string{
		"user-observable",
		"verified",
		"coherent",
		"WHAT",
	}

	for _, concept := range requiredConcepts {
		if !strings.Contains(allText, concept) {
			t.Errorf("qualifications should mention %q", concept)
		}
	}
}

func TestSpecQualificationCriteria_Disqualifications(t *testing.T) {
	criteria := SpecQualificationCriteria{}
	disqualifications := criteria.Disqualifications()

	if len(disqualifications) != 8 {
		t.Errorf("expected 8 disqualifications, got %d", len(disqualifications))
	}

	// Verify all disqualification categories are covered
	allText := strings.Join(disqualifications, " ")
	requiredExclusions := []string{
		"Implementation details",
		"Internal code organization",
		"Technical plumbing",
		"conventions",
		"Infrastructure concerns",
		"Code quality practices",
		"Architectural decisions",
		"Domain vocabulary definitions",
	}

	for _, exclusion := range requiredExclusions {
		if !strings.Contains(allText, exclusion) {
			t.Errorf("disqualifications should mention %q", exclusion)
		}
	}
}

func TestSpecQualificationCriteria_LitmusTest(t *testing.T) {
	criteria := SpecQualificationCriteria{}
	litmus := criteria.LitmusTest()

	expected := "Could a user verify this by using the system?"
	if litmus != expected {
		t.Errorf("expected litmus test %q, got %q", expected, litmus)
	}
}

func TestSpecQualificationCriteria_FormatForAgent(t *testing.T) {
	criteria := SpecQualificationCriteria{}
	formatted := criteria.FormatForAgent()

	// Verify all qualifications are included
	for _, q := range criteria.Qualifications() {
		// Check key terms from each qualification
		keyTerms := extractKeyTerms(q)
		for _, term := range keyTerms {
			if !strings.Contains(formatted, term) {
				t.Errorf("formatted output should contain key term %q from qualification %q", term, q)
			}
		}
	}

	// Verify litmus test is included
	if !strings.Contains(formatted, "Could a user verify") {
		t.Error("formatted output should contain litmus test")
	}

	// Verify disqualifications are included
	for _, d := range criteria.Disqualifications() {
		if !strings.Contains(formatted, d) {
			t.Errorf("formatted output should contain disqualification %q", d)
		}
	}
}

func TestSpecQualificationCriteria_AcceptanceCriteriaRequirements(t *testing.T) {
	criteria := SpecQualificationCriteria{}
	requirements := criteria.AcceptanceCriteriaRequirements()

	if len(requirements) == 0 {
		t.Error("should have at least one acceptance criteria requirement")
	}

	// Verify observable outcomes requirement
	allText := strings.Join(requirements, " ")
	if !strings.Contains(allText, "observable outcomes") {
		t.Error("should require acceptance criteria to describe observable outcomes")
	}
}

// extractKeyTerms gets important terms from a qualification string
func extractKeyTerms(s string) []string {
	// Extract the most distinctive terms
	terms := []string{}
	if strings.Contains(s, "user-observable") {
		terms = append(terms, "user-observable")
	}
	if strings.Contains(s, "verified") {
		terms = append(terms, "verified")
	}
	if strings.Contains(s, "coherent") {
		terms = append(terms, "coherent")
	}
	if strings.Contains(s, "WHAT") {
		terms = append(terms, "WHAT")
	}
	return terms
}
