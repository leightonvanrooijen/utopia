package ralphsequential

import (
	"bytes"
	"regexp"
	"strings"
	"text/template"

	"github.com/leightonvanrooijen/utopia/internal/domain"
)

// PromptTemplate is the minimal template for Ralph execution.
// It uses {{handlebars}} style syntax.
const PromptTemplate = `## TASK

{{.Task}}
{{if .Reference}}

## REFERENCE

The following spec feature defines the correct behavior:

{{.Reference}}
{{end}}

## CONSTRAINTS

{{range .Constraints}}- {{.}}
{{end}}
{{if .PreviousFailures}}
## PREVIOUS FAILURES

The previous attempt failed with the following output:

{{.PreviousFailures}}

Please address these failures in your implementation.
{{end}}
---

When complete, commit your changes and output: <COMPLETE>`

// PromptData holds the data for rendering the prompt template.
type PromptData struct {
	Task             string
	Reference        string // Optional: for bugfix items, the referenced spec feature content
	Constraints      []string
	PreviousFailures string
}

// BuildPrompt creates a prompt for a feature, optionally including previous failures.
// For first iteration, pass nil for failures.
// For retry iterations, pass the extracted failure output.
func BuildPrompt(feature domain.Feature, failures []string) string {
	return BuildPromptWithConstraints(feature, DefaultConstraints, failures, nil)
}

// BuildPromptWithConstraints creates a prompt with custom constraints.
// For bugfix items, refFeature contains the spec feature that defines correct behavior.
func BuildPromptWithConstraints(feature domain.Feature, constraints []string, failures []string, refFeature *domain.Feature) string {
	task := buildTaskWithCriteria(feature)

	data := PromptData{
		Task:        task,
		Constraints: constraints,
	}

	// For bugfix items, include the referenced feature content
	if refFeature != nil {
		data.Reference = buildReferenceSection(refFeature)
	}

	if len(failures) > 0 {
		data.PreviousFailures = strings.Join(failures, "\n\n")
	}

	return renderTemplate(data)
}

// buildReferenceSection formats a spec feature for the REFERENCE section.
func buildReferenceSection(feature *domain.Feature) string {
	var sb strings.Builder

	sb.WriteString(feature.Description)
	sb.WriteString("\n\n")

	sb.WriteString("Acceptance criteria:\n")
	for _, criterion := range feature.AcceptanceCriteria {
		sb.WriteString("- ")
		sb.WriteString(criterion)
		sb.WriteString("\n")
	}

	return strings.TrimSpace(sb.String())
}

// RebuildPromptWithFailures updates a work item's prompt to include failure output.
// Note: This rebuilds without the reference feature. For bugfix items that need
// the reference feature on retry, caller should use BuildPromptWithConstraints directly.
func RebuildPromptWithFailures(workItem *domain.WorkItem, feature domain.Feature, failures []string) {
	workItem.Prompt = BuildPromptWithConstraints(feature, workItem.Constraints, failures, nil)
}

// buildTaskWithCriteria merges feature description with acceptance criteria
// into a single TASK block.
func buildTaskWithCriteria(feature domain.Feature) string {
	var sb strings.Builder

	// Feature description becomes the task headline
	sb.WriteString(feature.Description)
	sb.WriteString("\n\n")

	// Acceptance criteria are listed as bullet points
	sb.WriteString("Acceptance criteria:\n")
	for _, criterion := range feature.AcceptanceCriteria {
		sb.WriteString("- ")
		sb.WriteString(criterion)
		sb.WriteString("\n")
	}

	return strings.TrimSpace(sb.String())
}

// renderTemplate executes the prompt template with the given data.
func renderTemplate(data PromptData) string {
	// Escape any template syntax in user content
	data.Task = escapeTemplateContent(data.Task)
	data.Reference = escapeTemplateContent(data.Reference)
	data.PreviousFailures = escapeTemplateContent(data.PreviousFailures)

	tmpl, err := template.New("prompt").Parse(PromptTemplate)
	if err != nil {
		// This should never happen with a valid template
		panic("invalid prompt template: " + err.Error())
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		// This should never happen with valid data
		panic("failed to execute template: " + err.Error())
	}

	return buf.String()
}

// escapeTemplateContent escapes Go template syntax in user-provided content.
// This prevents user content from being interpreted as template directives.
func escapeTemplateContent(s string) string {
	if s == "" {
		return s
	}
	// Escape {{ and }} to prevent template injection
	re := regexp.MustCompile(`\{\{|\}\}`)
	return re.ReplaceAllStringFunc(s, func(match string) string {
		if match == "{{" {
			return "{ {"
		}
		return "} }"
	})
}
