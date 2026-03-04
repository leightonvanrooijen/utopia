package cli

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/leightonvanrooijen/utopia/internal/domain"
)

// READMEDocumented captures what's already documented in the README
type READMEDocumented struct {
	// Commands documented in README (e.g., "utopia cr", "utopia execute")
	Commands []string
	// Artifact types documented (e.g., "ADRs", "Concepts", "Domain")
	ArtifactTypes []string
	// Directories documented in the project structure section
	Directories []string
	// Workflow steps documented in "The Loop" section
	WorkflowSteps []string
}

// READMESignalCandidate represents a potential README documentation signal
type READMESignalCandidate struct {
	SpecID           string
	FeatureID        string
	Title            string
	Category         string // "command", "artifact", "workflow", "directory"
	Confidence       domain.SignalConfidence
	SuggestedSection string
}

// ParseREADMEDocumented extracts documented elements from README content
func ParseREADMEDocumented(readmeContent string) *READMEDocumented {
	doc := &READMEDocumented{
		Commands:      []string{},
		ArtifactTypes: []string{},
		Directories:   []string{},
		WorkflowSteps: []string{},
	}

	// Extract commands - look for "utopia <cmd>" patterns
	cmdPattern := regexp.MustCompile(`utopia\s+(\w+)`)
	cmdMatches := cmdPattern.FindAllStringSubmatch(readmeContent, -1)
	seenCmds := make(map[string]bool)
	for _, match := range cmdMatches {
		cmd := match[1]
		if !seenCmds[cmd] {
			doc.Commands = append(doc.Commands, cmd)
			seenCmds[cmd] = true
		}
	}

	// Extract artifact types from Knowledge Artifacts section
	if strings.Contains(readmeContent, "ADR") || strings.Contains(readmeContent, "Architecture Decision") {
		doc.ArtifactTypes = append(doc.ArtifactTypes, "ADR")
	}
	if strings.Contains(readmeContent, "Concept") {
		doc.ArtifactTypes = append(doc.ArtifactTypes, "Concept")
	}
	if strings.Contains(readmeContent, "Domain") {
		doc.ArtifactTypes = append(doc.ArtifactTypes, "Domain")
	}

	// Extract directories from project structure section
	dirPattern := regexp.MustCompile(`├──\s+(\S+)/?`)
	dirMatches := dirPattern.FindAllStringSubmatch(readmeContent, -1)
	for _, match := range dirMatches {
		dir := strings.TrimSuffix(match[1], "/")
		doc.Directories = append(doc.Directories, dir)
	}

	// Extract workflow steps - look for numbered steps or "The Loop" section
	if strings.Contains(readmeContent, "Converse") {
		doc.WorkflowSteps = append(doc.WorkflowSteps, "converse")
	}
	if strings.Contains(readmeContent, "Execute") {
		doc.WorkflowSteps = append(doc.WorkflowSteps, "execute")
	}
	if strings.Contains(readmeContent, "Harvest") {
		doc.WorkflowSteps = append(doc.WorkflowSteps, "harvest")
	}

	return doc
}

// ScanSpecsForREADMECandidates scans specs for features that qualify for README documentation
// using strict qualification criteria.
func ScanSpecsForREADMECandidates(specs []*domain.Spec, documented *READMEDocumented) []READMESignalCandidate {
	var candidates []READMESignalCandidate

	for _, spec := range specs {
		for _, feature := range spec.Features {
			if candidate := qualifyFeatureForREADME(spec, feature, documented); candidate != nil {
				candidates = append(candidates, *candidate)
			}
		}
	}

	return candidates
}

// qualifyFeatureForREADME applies strict qualification criteria to determine if a feature
// should be documented in the README
func qualifyFeatureForREADME(spec *domain.Spec, feature domain.Feature, documented *READMEDocumented) *READMESignalCandidate {
	desc := strings.ToLower(feature.Description)
	featureID := strings.ToLower(feature.ID)

	// DISQUALIFICATION CHECKS (any of these excludes)
	if isEnhancementToExisting(feature, documented) {
		return nil
	}
	if isInternalImplementation(feature) {
		return nil
	}
	if isConfigOption(feature) {
		return nil
	}
	if isSpecOnlyChange(feature) {
		return nil
	}

	// QUALIFICATION CHECKS (must meet at least one)

	// Check 1: New primary CLI command
	if isNewPrimaryCLICommand(feature, documented) {
		return &READMESignalCandidate{
			SpecID:           spec.ID,
			FeatureID:        feature.ID,
			Title:            extractCommandName(feature),
			Category:         "command",
			Confidence:       domain.SignalConfidenceHigh,
			SuggestedSection: "Quick Start / The Loop",
		}
	}

	// Check 2: New knowledge artifact type
	if isNewArtifactType(feature, documented) {
		return &READMESignalCandidate{
			SpecID:           spec.ID,
			FeatureID:        feature.ID,
			Title:            extractArtifactName(feature),
			Category:         "artifact",
			Confidence:       domain.SignalConfidenceHigh,
			SuggestedSection: "Knowledge Artifacts",
		}
	}

	// Check 3: Change to core workflow/loop
	if isCoreWorkflowChange(feature, documented) {
		return &READMESignalCandidate{
			SpecID:           spec.ID,
			FeatureID:        feature.ID,
			Title:            fmt.Sprintf("Workflow change: %s", feature.ID),
			Category:         "workflow",
			Confidence:       domain.SignalConfidenceMedium,
			SuggestedSection: "The Solution / Quick Start",
		}
	}

	// Check 4: New top-level .utopia/ directory
	if isNewTopLevelDirectory(desc, featureID, documented) {
		return &READMESignalCandidate{
			SpecID:           spec.ID,
			FeatureID:        feature.ID,
			Title:            extractDirectoryName(feature),
			Category:         "directory",
			Confidence:       domain.SignalConfidenceHigh,
			SuggestedSection: "Project Structure",
		}
	}

	return nil
}

// Disqualification helpers

func isEnhancementToExisting(feature domain.Feature, documented *READMEDocumented) bool {
	desc := strings.ToLower(feature.Description)
	featureID := strings.ToLower(feature.ID)

	// Check if this enhances an already-documented command
	for _, cmd := range documented.Commands {
		cmdLower := strings.ToLower(cmd)
		// If the feature ID or description mentions an existing command, it's likely an enhancement
		if strings.Contains(featureID, cmdLower) || strings.Contains(desc, "utopia "+cmdLower) {
			// Only disqualify if it's clearly an enhancement, not a new command definition
			if !strings.Contains(desc, "new command") && !strings.Contains(desc, "cli command to") {
				return true
			}
		}
	}

	// Check for enhancement language
	enhancementPatterns := []string{
		"extend", "improve", "add to existing", "enhance",
		"modify the", "update the", "additional", "also include",
	}
	for _, pattern := range enhancementPatterns {
		if strings.Contains(desc, pattern) {
			return true
		}
	}

	return false
}

func isInternalImplementation(feature domain.Feature) bool {
	desc := strings.ToLower(feature.Description)
	featureID := strings.ToLower(feature.ID)

	// Internal implementation markers
	internalPatterns := []string{
		"internal", "implementation detail", "helper", "utility",
		"private", "refactor", "cleanup", "storage layer",
		"marshaler", "parser", "validator",
	}
	for _, pattern := range internalPatterns {
		if strings.Contains(desc, pattern) || strings.Contains(featureID, pattern) {
			return true
		}
	}

	return false
}

func isConfigOption(feature domain.Feature) bool {
	desc := strings.ToLower(feature.Description)
	featureID := strings.ToLower(feature.ID)

	// Config option markers
	configPatterns := []string{
		"config option", "configuration", "setting", "flag",
		"parameter", "yaml field", "option field",
	}
	for _, pattern := range configPatterns {
		if strings.Contains(desc, pattern) || strings.Contains(featureID, pattern) {
			return true
		}
	}

	return false
}

func isSpecOnlyChange(feature domain.Feature) bool {
	desc := strings.ToLower(feature.Description)

	// Spec-only markers
	specOnlyPatterns := []string{
		"spec format", "spec structure", "acceptance criteria format",
		"domain knowledge field", "spec validation",
	}
	for _, pattern := range specOnlyPatterns {
		if strings.Contains(desc, pattern) {
			return true
		}
	}

	return false
}

// Qualification helpers

func isNewPrimaryCLICommand(feature domain.Feature, documented *READMEDocumented) bool {
	desc := strings.ToLower(feature.Description)
	featureID := strings.ToLower(feature.ID)

	// Must indicate it's a CLI command
	if !strings.Contains(desc, "command") && !strings.Contains(featureID, "command") {
		return false
	}

	// Must be a primary command (not a flag or subcommand)
	if strings.Contains(desc, "flag") || strings.Contains(desc, "subcommand") ||
		strings.Contains(desc, "option") {
		return false
	}

	// Check it's not already documented
	cmdPattern := regexp.MustCompile(`utopia\s+(\w+)`)
	matches := cmdPattern.FindAllStringSubmatch(desc, -1)
	for _, match := range matches {
		cmdName := strings.ToLower(match[1])
		isDocumented := false
		for _, docCmd := range documented.Commands {
			if strings.ToLower(docCmd) == cmdName {
				isDocumented = true
				break
			}
		}
		// Found a command that's not in README
		if !isDocumented {
			return true
		}
	}

	// Also check feature ID for command names
	if strings.HasSuffix(featureID, "-command") {
		cmdName := strings.TrimSuffix(featureID, "-command")
		for _, docCmd := range documented.Commands {
			if strings.ToLower(docCmd) == cmdName {
				return false
			}
		}
		return true
	}

	return false
}

func isNewArtifactType(feature domain.Feature, documented *READMEDocumented) bool {
	desc := strings.ToLower(feature.Description)

	// Must indicate it's a knowledge artifact type
	artifactIndicators := []string{
		"knowledge artifact", "document type", "documentation type",
		"harvested from", "extracted from conversation",
	}

	hasArtifactIndicator := false
	for _, indicator := range artifactIndicators {
		if strings.Contains(desc, indicator) {
			hasArtifactIndicator = true
			break
		}
	}

	if !hasArtifactIndicator {
		return false
	}

	// Check it's not one of the existing types
	existingTypes := []string{"adr", "concept", "domain"}
	for _, existing := range existingTypes {
		if strings.Contains(desc, existing) {
			return false // Enhancement to existing type
		}
	}

	return true
}

func isCoreWorkflowChange(feature domain.Feature, documented *READMEDocumented) bool {
	desc := strings.ToLower(feature.Description)

	// Must be about the core workflow/loop
	workflowIndicators := []string{
		"core loop", "workflow", "the loop", "main flow",
		"fundamental change", "new phase", "new step",
	}

	hasWorkflowIndicator := false
	for _, indicator := range workflowIndicators {
		if strings.Contains(desc, indicator) {
			hasWorkflowIndicator = true
			break
		}
	}

	if !hasWorkflowIndicator {
		return false
	}

	// Must be a change to the loop, not just mentioning it
	changeIndicators := []string{
		"add", "new", "introduce", "change", "modify",
	}
	for _, change := range changeIndicators {
		if strings.Contains(desc, change) {
			return true
		}
	}

	return false
}

func isNewTopLevelDirectory(desc, featureID string, documented *READMEDocumented) bool {
	// Look for directory creation patterns
	dirPattern := regexp.MustCompile(`\.utopia/(\w+)/?`)
	matches := dirPattern.FindAllStringSubmatch(desc, -1)

	for _, match := range matches {
		dirName := match[1]
		// Check if this is a new directory (not already in README)
		isNew := true
		for _, docDir := range documented.Directories {
			if strings.EqualFold(docDir, dirName) || strings.EqualFold(docDir, dirName+"/") {
				isNew = false
				break
			}
		}
		if isNew {
			// Check it's top-level (no subdirectory pattern)
			if !strings.Contains(match[0], "/"+dirName+"/") {
				return true
			}
		}
	}

	return false
}

// Extraction helpers

func extractCommandName(feature domain.Feature) string {
	desc := feature.Description
	cmdPattern := regexp.MustCompile(`utopia\s+(\w+)`)
	if matches := cmdPattern.FindStringSubmatch(desc); len(matches) > 1 {
		return fmt.Sprintf("utopia %s command", matches[1])
	}
	if strings.HasSuffix(strings.ToLower(feature.ID), "-command") {
		cmdName := strings.TrimSuffix(feature.ID, "-command")
		return fmt.Sprintf("utopia %s command", cmdName)
	}
	return feature.ID
}

func extractArtifactName(feature domain.Feature) string {
	// Try to extract artifact type name from description
	return fmt.Sprintf("New artifact type: %s", feature.ID)
}

func extractDirectoryName(feature domain.Feature) string {
	desc := feature.Description
	dirPattern := regexp.MustCompile(`\.utopia/(\w+)/?`)
	if matches := dirPattern.FindStringSubmatch(desc); len(matches) > 1 {
		return fmt.Sprintf(".utopia/%s/", matches[1])
	}
	return feature.ID
}

// BuildREADMESignalsSummary creates a summary of README signal candidates for the harvest prompt
func BuildREADMESignalsSummary(candidates []READMESignalCandidate) string {
	if len(candidates) == 0 {
		return "(No README documentation signals found - all qualifying features are already documented)"
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("**%d potential README documentation signals found:**\n\n", len(candidates)))

	// Group by category
	byCategory := make(map[string][]READMESignalCandidate)
	for _, c := range candidates {
		byCategory[c.Category] = append(byCategory[c.Category], c)
	}

	categoryOrder := []string{"command", "artifact", "workflow", "directory"}
	categoryNames := map[string]string{
		"command":   "New CLI Commands",
		"artifact":  "New Knowledge Artifact Types",
		"workflow":  "Core Workflow Changes",
		"directory": "New .utopia/ Directories",
	}

	for _, cat := range categoryOrder {
		if cands, ok := byCategory[cat]; ok {
			sb.WriteString(fmt.Sprintf("### %s\n", categoryNames[cat]))
			for _, c := range cands {
				sb.WriteString(fmt.Sprintf("- **%s** (spec: %s, feature: %s)\n", c.Title, c.SpecID, c.FeatureID))
				sb.WriteString(fmt.Sprintf("  - Confidence: %s\n", c.Confidence))
				sb.WriteString(fmt.Sprintf("  - Suggested README section: %s\n", c.SuggestedSection))
			}
			sb.WriteString("\n")
		}
	}

	return sb.String()
}
