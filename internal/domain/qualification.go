package domain

import (
	"fmt"
	"strings"
)

// SpecQualificationCriteria defines what makes a candidate spec-worthy.
// Core principle: Specs answer "what can I do?" not "how is it built?"
//
// Qualification criteria are applied by the Qualifier Agent during discovery
// to filter candidates before they become draft specifications.
type SpecQualificationCriteria struct{}

// QualificationResult represents whether a candidate passes qualification
// and why.
type QualificationResult struct {
	Qualified bool
	Reason    string
}

// Qualifications returns the criteria that must ALL be satisfied for a
// candidate to become a draft spec.
//
// A spec-worthy candidate:
//  1. Describes a user-observable capability (command, output, interaction, behavior)
//  2. Can be verified by using the system (testable from outside)
//  3. Represents a coherent, bounded feature users care about
//  4. Description focuses on WHAT users can achieve, not HOW it's implemented
func (c SpecQualificationCriteria) Qualifications() []string {
	return []string{
		"Capability is user-observable (command, output, interaction, behavior)",
		"Capability can be verified by using the system (testable from outside)",
		"Capability represents a coherent bounded feature users care about",
		"Description focuses on WHAT users can achieve, not HOW it's implemented",
	}
}

// AcceptanceCriteriaRequirements returns the constraints on acceptance criteria
// for qualified specs.
func (c SpecQualificationCriteria) AcceptanceCriteriaRequirements() []string {
	return []string{
		"Acceptance criteria describe observable outcomes, not internal state",
	}
}

// Disqualifications returns criteria where ANY match disqualifies a candidate.
// These are implementation details that should not become specs.
func (c SpecQualificationCriteria) Disqualifications() []string {
	return []string{
		"Implementation details (data structures, algorithms, patterns used)",
		"Internal code organization (services, handlers, repositories, utils)",
		"Technical plumbing users don't interact with (middleware, adapters)",
		"Standard practices covered by language/framework conventions",
		"Infrastructure concerns (logging, monitoring, deployment)",
		"Code quality practices (error handling patterns, validation approaches)",
		"Architectural decisions (those belong in ADRs)",
		"Domain vocabulary definitions (those belong in Domain docs)",
	}
}

// LitmusTest returns the core question to determine spec-worthiness.
// If the answer is YES, the candidate is spec-worthy.
func (c SpecQualificationCriteria) LitmusTest() string {
	return "Could a user verify this by using the system?"
}

// IsSpecWorthy applies the litmus test logic. A capability is spec-worthy
// if it can be verified by a user through normal system usage.
//
// Examples of spec-worthy capabilities:
//   - "Users can initialize a project" - YES, run `utopia init` and verify
//   - "Users can discover specs from code" - YES, run `utopia discover` and verify
//
// Examples of NOT spec-worthy (implementation details):
//   - "YAML parser validates spec schema" - NO, internal implementation
//   - "Repository uses file-based storage" - NO, technical plumbing
func (c SpecQualificationCriteria) IsSpecWorthy(description string, canUserVerify bool) QualificationResult {
	if !canUserVerify {
		return QualificationResult{
			Qualified: false,
			Reason:    "Cannot be verified by using the system - likely implementation detail",
		}
	}
	return QualificationResult{
		Qualified: true,
		Reason:    "User can verify this capability by using the system",
	}
}

// FormatForAgent returns the qualification criteria formatted for inclusion
// in agent prompts. This ensures consistency between the domain definition
// and the agent's instructions.
func (c SpecQualificationCriteria) FormatForAgent() string {
	var sb strings.Builder

	sb.WriteString("## Qualification Criteria (ALL must be true)\n")
	for i, q := range c.Qualifications() {
		sb.WriteString(fmt.Sprintf("%d. %s\n", i+1, q))
	}
	for i, r := range c.AcceptanceCriteriaRequirements() {
		sb.WriteString(fmt.Sprintf("%d. %s\n", len(c.Qualifications())+i+1, r))
	}

	sb.WriteString("\n## Litmus Test\n")
	sb.WriteString(fmt.Sprintf("Ask: %q\n", c.LitmusTest()))
	sb.WriteString("YES = Spec worthy\n")
	sb.WriteString("NO = Implementation detail, disqualify\n")

	sb.WriteString("\n## Disqualification Criteria (ANY disqualifies)\n")
	for _, d := range c.Disqualifications() {
		sb.WriteString(fmt.Sprintf("- %s\n", d))
	}

	return strings.TrimSuffix(sb.String(), "\n")
}
