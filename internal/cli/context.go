package cli

import (
	"fmt"
	"strings"

	"github.com/leightonvanrooijen/utopia/internal/domain"
)

// buildDomainContext creates a formatted domain context for Claude context injection.
// It formats domain docs into ubiquitous language guidance ("use X not Y")
// and entity relationship context to help Claude use consistent terminology.
func buildDomainContext(docs []*domain.DomainDoc) string {
	if len(docs) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("## Domain Language Guide\n\n")
	sb.WriteString("Use consistent terminology from the project's ubiquitous language:\n\n")

	// First, collect all terms across all bounded contexts
	for _, doc := range docs {
		if len(doc.Terms) == 0 {
			continue
		}

		sb.WriteString(fmt.Sprintf("### %s\n", doc.Title))

		for _, term := range doc.Terms {
			// Primary guidance: use X (not Y alternatives)
			if len(term.Aliases) > 0 {
				sb.WriteString(fmt.Sprintf("- Use **%s** (not %s)\n", term.Term, formatAliases(term.Aliases)))
			} else {
				sb.WriteString(fmt.Sprintf("- **%s**\n", term.Term))
			}

			// Include brief definition
			definition := strings.TrimSpace(term.Definition)
			if len(definition) > 150 {
				definition = definition[:150] + "..."
			}
			// Replace newlines with spaces for compact display
			definition = strings.ReplaceAll(definition, "\n", " ")
			sb.WriteString(fmt.Sprintf("  Meaning: %s\n", definition))
		}
		sb.WriteString("\n")
	}

	// Add entity relationships section
	hasRelationships := false
	for _, doc := range docs {
		if len(doc.Entities) > 0 {
			for _, entity := range doc.Entities {
				if len(entity.Relationships) > 0 {
					hasRelationships = true
					break
				}
			}
		}
		if hasRelationships {
			break
		}
	}

	if hasRelationships {
		sb.WriteString("### Entity Relationships\n")
		for _, doc := range docs {
			for _, entity := range doc.Entities {
				for _, rel := range entity.Relationships {
					sb.WriteString(fmt.Sprintf("- %s %s %s\n", entity.Name, rel.Type, rel.Target))
				}
			}
		}
		sb.WriteString("\n")
	}

	return sb.String()
}

// formatAliases formats a list of aliases for display
func formatAliases(aliases []string) string {
	if len(aliases) == 0 {
		return ""
	}
	if len(aliases) == 1 {
		return fmt.Sprintf("\"%s\"", aliases[0])
	}

	quoted := make([]string, len(aliases))
	for i, a := range aliases {
		quoted[i] = fmt.Sprintf("\"%s\"", a)
	}
	return strings.Join(quoted, ", ")
}
