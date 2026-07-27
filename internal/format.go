package internal

import (
	"strings"

	"github.com/google/yamlfmt/formatters/basic"
)

// defaultFormatConfig is the centralized formatting configuration for all Utopia YAML files.
// These defaults ensure consistent, readable YAML output across the project.
var defaultFormatConfig = map[string]any{
	"indent":                   2,
	"retain_line_breaks":       true,
	"eof_newline":              true,
	"trim_trailing_whitespace": true,
}

// FormatYAML formats YAML content using the Utopia default configuration.
// It returns the formatted content or an error if formatting fails.
func FormatYAML(content []byte) ([]byte, error) {
	factory := basic.BasicFormatterFactory{}
	f, err := factory.NewFormatter(defaultFormatConfig)
	if err != nil {
		return nil, err
	}
	return f.Format(content)
}

// ExtractYAMLBlock extracts the first fenced YAML block from Claude output.
// Falls back to scanning for a bare "drafts:" document when no fence is found.
func ExtractYAMLBlock(text string) string {
	startMarkers := []string{"```yaml", "```yml"}
	endMarker := "```"
	for _, start := range startMarkers {
		startIdx := strings.Index(text, start)
		if startIdx == -1 {
			continue
		}
		contentStart := startIdx + len(start)
		remaining := text[contentStart:]
		endIdx := strings.Index(remaining, endMarker)
		if endIdx == -1 {
			continue
		}
		return strings.TrimSpace(remaining[:endIdx])
	}
	if strings.Contains(text, "drafts:") {
		lines := strings.Split(text, "\n")
		var yamlLines []string
		inYAML := false
		for _, line := range lines {
			if strings.HasPrefix(strings.TrimSpace(line), "drafts:") {
				inYAML = true
			}
			if inYAML {
				yamlLines = append(yamlLines, line)
			}
		}
		if len(yamlLines) > 0 {
			return strings.Join(yamlLines, "\n")
		}
	}
	return ""
}
