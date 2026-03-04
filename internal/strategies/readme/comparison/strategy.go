package comparison

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/leightonvanrooijen/utopia/internal/domain"
	"github.com/leightonvanrooijen/utopia/internal/strategies/readme"
)

// Strategy implements comparison-based README signal detection.
// It compares the current README content against spec features to identify
// undocumented commands, artifacts, workflows, and directories.
type Strategy struct{}

// New creates a new comparison-based detection strategy.
func New() *Strategy {
	return &Strategy{}
}

// Name returns the strategy identifier.
func (s *Strategy) Name() string {
	return "comparison"
}

// Description returns a human-readable description for CLI help.
func (s *Strategy) Description() string {
	return "Compare README content against spec features to find undocumented items"
}

// Detect scans specs and compares against documented items to find signals.
func (s *Strategy) Detect(specs []*domain.Spec, documented *readme.READMEDocumented) []readme.SignalCandidate {
	var candidates []readme.SignalCandidate

	for _, spec := range specs {
		for _, feature := range spec.Features {
			if candidate := s.qualifyFeature(spec, feature, documented); candidate != nil {
				candidates = append(candidates, *candidate)
			}
		}
	}

	return candidates
}

// qualifyFeature applies strict qualification criteria to determine if a feature
// should be documented in the README.
func (s *Strategy) qualifyFeature(spec *domain.Spec, feature domain.Feature, documented *readme.READMEDocumented) *readme.SignalCandidate {
	desc := strings.ToLower(feature.Description)
	featureID := strings.ToLower(feature.ID)

	// DISQUALIFICATION CHECKS (any of these excludes)
	if s.isEnhancementToExisting(feature, documented) {
		return nil
	}
	if s.isInternalImplementation(feature) {
		return nil
	}
	if s.isConfigOption(feature) {
		return nil
	}
	if s.isSpecOnlyChange(feature) {
		return nil
	}

	// QUALIFICATION CHECKS (must meet at least one)

	// Check 1: New primary CLI command
	if s.isNewPrimaryCLICommand(feature, documented) {
		return &readme.SignalCandidate{
			SpecID:           spec.ID,
			FeatureID:        feature.ID,
			Title:            s.extractCommandName(feature),
			Category:         "command",
			Confidence:       domain.SignalConfidenceHigh,
			SuggestedSection: "Quick Start / The Loop",
		}
	}

	// Check 2: New knowledge artifact type
	if s.isNewArtifactType(feature, documented) {
		return &readme.SignalCandidate{
			SpecID:           spec.ID,
			FeatureID:        feature.ID,
			Title:            s.extractArtifactName(feature),
			Category:         "artifact",
			Confidence:       domain.SignalConfidenceHigh,
			SuggestedSection: "Knowledge Artifacts",
		}
	}

	// Check 3: Change to core workflow/loop
	if s.isCoreWorkflowChange(feature, documented) {
		return &readme.SignalCandidate{
			SpecID:           spec.ID,
			FeatureID:        feature.ID,
			Title:            fmt.Sprintf("Workflow change: %s", feature.ID),
			Category:         "workflow",
			Confidence:       domain.SignalConfidenceMedium,
			SuggestedSection: "The Solution / Quick Start",
		}
	}

	// Check 4: New top-level .utopia/ directory
	if s.isNewTopLevelDirectory(desc, featureID, documented) {
		return &readme.SignalCandidate{
			SpecID:           spec.ID,
			FeatureID:        feature.ID,
			Title:            s.extractDirectoryName(feature),
			Category:         "directory",
			Confidence:       domain.SignalConfidenceHigh,
			SuggestedSection: "Project Structure",
		}
	}

	return nil
}

// Disqualification helpers

func (s *Strategy) isEnhancementToExisting(feature domain.Feature, documented *readme.READMEDocumented) bool {
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

func (s *Strategy) isInternalImplementation(feature domain.Feature) bool {
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

func (s *Strategy) isConfigOption(feature domain.Feature) bool {
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

func (s *Strategy) isSpecOnlyChange(feature domain.Feature) bool {
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

func (s *Strategy) isNewPrimaryCLICommand(feature domain.Feature, documented *readme.READMEDocumented) bool {
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

func (s *Strategy) isNewArtifactType(feature domain.Feature, documented *readme.READMEDocumented) bool {
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

func (s *Strategy) isCoreWorkflowChange(feature domain.Feature, documented *readme.READMEDocumented) bool {
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

func (s *Strategy) isNewTopLevelDirectory(desc, featureID string, documented *readme.READMEDocumented) bool {
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

func (s *Strategy) extractCommandName(feature domain.Feature) string {
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

func (s *Strategy) extractArtifactName(feature domain.Feature) string {
	// Try to extract artifact type name from description
	return fmt.Sprintf("New artifact type: %s", feature.ID)
}

func (s *Strategy) extractDirectoryName(feature domain.Feature) string {
	desc := feature.Description
	dirPattern := regexp.MustCompile(`\.utopia/(\w+)/?`)
	if matches := dirPattern.FindStringSubmatch(desc); len(matches) > 1 {
		return fmt.Sprintf(".utopia/%s/", matches[1])
	}
	return feature.ID
}
