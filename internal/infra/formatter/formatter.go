// Package formatter provides YAML formatting using Google's yamlfmt library
// with Utopia-specific configuration defaults.
package formatter

import (
	"github.com/google/yamlfmt/formatters/basic"
)

// defaultConfig is the centralized formatting configuration for all Utopia YAML files.
// These defaults ensure consistent, readable YAML output across the project.
var defaultConfig = map[string]any{
	"indent":                   2,
	"retain_line_breaks":       true,
	"eof_newline":              true,
	"trim_trailing_whitespace": true,
}

// Format formats YAML content using the Utopia default configuration.
// It returns the formatted content or an error if formatting fails.
func Format(content []byte) ([]byte, error) {
	factory := basic.BasicFormatterFactory{}
	f, err := factory.NewFormatter(defaultConfig)
	if err != nil {
		return nil, err
	}
	return f.Format(content)
}
