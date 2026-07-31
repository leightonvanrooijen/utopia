package internal

import (
	"errors"
	"strings"
)

// Markdown frontmatter delimiters. The opening delimiter must start the
// document; the closing one is the first "\n---" after it.
const (
	frontmatterOpen  = "---\n"
	frontmatterClose = "\n---"
)

var (
	// errNoFrontmatter is returned when a document does not begin with "---\n".
	errNoFrontmatter = errors.New("missing YAML frontmatter")
	// errUnclosedFrontmatter is returned when the opening delimiter is never closed.
	errUnclosedFrontmatter = errors.New("unclosed YAML frontmatter")
)

// splitFrontmatter divides a markdown document into its raw YAML frontmatter
// and its body. The frontmatter is returned unparsed - callers unmarshal it
// into whatever struct they need. The body has a single leading newline
// trimmed, and is empty when nothing follows the closing delimiter.
func splitFrontmatter(content string) (frontmatter, body string, err error) {
	if !strings.HasPrefix(content, frontmatterOpen) {
		return "", "", errNoFrontmatter
	}

	rest := content[len(frontmatterOpen):]
	end := strings.Index(rest, frontmatterClose)
	if end == -1 {
		return "", "", errUnclosedFrontmatter
	}

	frontmatter = rest[:end]

	bodyStart := len(frontmatterOpen) + end + len(frontmatterClose)
	if bodyStart < len(content) {
		body = strings.TrimPrefix(content[bodyStart:], "\n")
	}

	return frontmatter, body, nil
}
