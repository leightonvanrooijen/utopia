package cli

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/leightonvanrooijen/utopia/internal/strategies/readme"
)

// ParseREADMEDocumented extracts documented elements from README content.
// Returns a READMEDocumented struct that can be passed to detection strategies.
func ParseREADMEDocumented(readmeContent string) *readme.READMEDocumented {
	doc := &readme.READMEDocumented{
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

// BuildREADMESignalsSummary creates a summary of README signal candidates for the harvest prompt.
// This formats the candidates detected by a strategy into human-readable markdown.
func BuildREADMESignalsSummary(candidates []readme.SignalCandidate) string {
	if len(candidates) == 0 {
		return "(No README documentation signals found - all qualifying features are already documented)"
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("**%d potential README documentation signals found:**\n\n", len(candidates)))

	// Group by category
	byCategory := make(map[string][]readme.SignalCandidate)
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
