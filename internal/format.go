package internal

import (
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
