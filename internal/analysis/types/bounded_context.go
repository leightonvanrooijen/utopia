// Package types provides static analysis of type definitions in source files.
// This file adds bounded context inference from package/module structure.
package types

import (
	"path/filepath"
	"sort"
	"strings"
)

// BoundedContext represents a discovered bounded context from package structure
type BoundedContext struct {
	Name  string // Kebab-case name derived from package (e.g., "order", "user-account")
	Title string // Human-readable title (e.g., "Order", "User Account")
	// RootPath is the relative path to this context's root package
	RootPath string
	// Files contains all files belonging to this context
	Files []string
}

// ContextualTerm represents a term occurrence with its bounded context
type ContextualTerm struct {
	*TermOccurrence
	// BoundedContext is the inferred bounded context for this term
	BoundedContext string
}

// BoundedContextAnalyzer infers bounded contexts from package structure
// and groups terms by their context.
type BoundedContextAnalyzer struct {
	// domainPaths are path prefixes that indicate domain code
	// (e.g., "internal", "pkg", "src", "domain", "core")
	domainPaths []string
}

// NewBoundedContextAnalyzer creates a new analyzer with default domain paths
func NewBoundedContextAnalyzer() *BoundedContextAnalyzer {
	return &BoundedContextAnalyzer{
		domainPaths: []string{
			"internal",
			"pkg",
			"src",
			"domain",
			"core",
			"lib",
			"app",
			"modules",
		},
	}
}

// InferBoundedContext determines the bounded context for a file path.
// It looks at the first meaningful package level under a domain path.
//
// Examples:
//   - "internal/order/order.go" -> "order"
//   - "internal/user/account/account.go" -> "user"
//   - "src/domain/billing/invoice.go" -> "billing"
//   - "pkg/api/handlers/user.go" -> "api"
func (a *BoundedContextAnalyzer) InferBoundedContext(filePath string) string {
	// Normalize path separators
	normalizedPath := filepath.ToSlash(filePath)
	parts := strings.Split(normalizedPath, "/")

	// Find the domain path index
	domainIdx := -1
	for i, part := range parts {
		for _, domainPath := range a.domainPaths {
			if part == domainPath {
				domainIdx = i
				break
			}
		}
		if domainIdx >= 0 {
			break
		}
	}

	// If no domain path found, try to use the first directory
	if domainIdx < 0 {
		if len(parts) > 1 {
			// Use the first directory as context
			return a.normalizeContextName(parts[0])
		}
		return "unknown"
	}

	// Get the next directory after the domain path as the bounded context
	// Skip common infrastructure layers to find the actual domain package
	infraLayers := map[string]bool{
		"domain":       true, // "internal/domain/order" -> use "order"
		"core":         true,
		"models":       true,
		"entities":     true,
		"aggregates":   true,
		"valueobjects": true,
	}

	for i := domainIdx + 1; i < len(parts)-1; i++ { // -1 to skip filename
		part := parts[i]
		// Skip infrastructure layer markers
		if infraLayers[part] {
			continue
		}
		// Found the bounded context
		return a.normalizeContextName(part)
	}

	// If we only have domain path + filename, use domain path
	if domainIdx >= 0 && domainIdx < len(parts)-1 {
		return a.normalizeContextName(parts[domainIdx])
	}

	return "unknown"
}

// normalizeContextName converts a package name to a consistent kebab-case context name
func (a *BoundedContextAnalyzer) normalizeContextName(name string) string {
	// Handle camelCase or PascalCase -> kebab-case
	var result strings.Builder
	for i, r := range name {
		if i > 0 && r >= 'A' && r <= 'Z' {
			result.WriteRune('-')
		}
		result.WriteRune(r)
	}

	// Convert to lowercase and replace underscores with hyphens
	normalized := strings.ToLower(result.String())
	normalized = strings.ReplaceAll(normalized, "_", "-")

	return normalized
}

// contextTitle converts a kebab-case context name to a human-readable title
func (a *BoundedContextAnalyzer) contextTitle(name string) string {
	// Split by hyphens and title-case each word
	parts := strings.Split(name, "-")
	for i, part := range parts {
		if len(part) > 0 {
			parts[i] = strings.ToUpper(part[:1]) + part[1:]
		}
	}
	return strings.Join(parts, " ")
}

// DiscoverBoundedContexts analyzes file paths to discover bounded contexts
func (a *BoundedContextAnalyzer) DiscoverBoundedContexts(filePaths []string) []*BoundedContext {
	// Group files by context
	contextFiles := make(map[string][]string)
	contextRoots := make(map[string]string)

	for _, path := range filePaths {
		ctx := a.InferBoundedContext(path)
		contextFiles[ctx] = append(contextFiles[ctx], path)

		// Track the shortest path as the root
		if existing, ok := contextRoots[ctx]; !ok || len(path) < len(existing) {
			// Extract directory from file path
			dir := filepath.Dir(path)
			contextRoots[ctx] = dir
		}
	}

	// Convert to BoundedContext slice
	var contexts []*BoundedContext
	for name, files := range contextFiles {
		contexts = append(contexts, &BoundedContext{
			Name:     name,
			Title:    a.contextTitle(name),
			RootPath: contextRoots[name],
			Files:    files,
		})
	}

	// Sort by name for consistent ordering
	sort.Slice(contexts, func(i, j int) bool {
		return contexts[i].Name < contexts[j].Name
	})

	return contexts
}

// GroupTermsByContext takes discovered types and groups them by bounded context
func (a *BoundedContextAnalyzer) GroupTermsByContext(types []*DiscoveredType) map[string][]*ContextualTerm {
	// First, group types by term name AND context
	// A term can appear in multiple contexts with different meanings
	type termKey struct {
		term    string
		context string
	}

	termOccurrences := make(map[termKey]*ContextualTerm)

	for _, t := range types {
		ctx := a.InferBoundedContext(t.FilePath)
		key := termKey{term: t.Name, context: ctx}

		if _, exists := termOccurrences[key]; !exists {
			termOccurrences[key] = &ContextualTerm{
				TermOccurrence: &TermOccurrence{
					Term:  t.Name,
					Files: []string{},
					Lines: []string{},
				},
				BoundedContext: ctx,
			}
		}

		occ := termOccurrences[key]
		lineRef := formatLineReference(t.FilePath, t.LineNumber)

		if !containsString(occ.Files, t.FilePath) {
			occ.Files = append(occ.Files, t.FilePath)
		}
		occ.Lines = append(occ.Lines, lineRef)
		occ.Types = append(occ.Types, t)

		// Also track field names within the same context
		for _, field := range t.Fields {
			fieldKey := termKey{term: field.Name, context: ctx}
			if _, exists := termOccurrences[fieldKey]; !exists {
				termOccurrences[fieldKey] = &ContextualTerm{
					TermOccurrence: &TermOccurrence{
						Term:  field.Name,
						Files: []string{},
						Lines: []string{},
					},
					BoundedContext: ctx,
				}
			}

			fieldOcc := termOccurrences[fieldKey]
			fieldLineRef := formatLineReference(t.FilePath, field.LineNumber)

			if !containsString(fieldOcc.Files, t.FilePath) {
				fieldOcc.Files = append(fieldOcc.Files, t.FilePath)
			}
			fieldOcc.Lines = append(fieldOcc.Lines, fieldLineRef)
		}
	}

	// Group by context and calculate confidence
	result := make(map[string][]*ContextualTerm)
	analyzer := NewAnalyzer()

	for _, ct := range termOccurrences {
		ct.Confidence = analyzer.calculateConfidence(ct.TermOccurrence)
		result[ct.BoundedContext] = append(result[ct.BoundedContext], ct)
	}

	// Sort terms within each context by confidence then alphabetically
	for ctx := range result {
		sort.Slice(result[ctx], func(i, j int) bool {
			confidenceOrder := map[TermConfidence]int{
				TermConfidenceHigh:   0,
				TermConfidenceMedium: 1,
				TermConfidenceLow:    2,
			}

			if confidenceOrder[result[ctx][i].Confidence] != confidenceOrder[result[ctx][j].Confidence] {
				return confidenceOrder[result[ctx][i].Confidence] < confidenceOrder[result[ctx][j].Confidence]
			}

			if len(result[ctx][i].Files) != len(result[ctx][j].Files) {
				return len(result[ctx][i].Files) > len(result[ctx][j].Files)
			}

			return result[ctx][i].Term < result[ctx][j].Term
		})
	}

	return result
}

// FindCrossContextTerms identifies terms that appear in multiple contexts
// These often require cross_context_note in domain documentation
func (a *BoundedContextAnalyzer) FindCrossContextTerms(contextTerms map[string][]*ContextualTerm) map[string][]string {
	// Map term -> list of contexts where it appears
	termContexts := make(map[string][]string)

	for ctx, terms := range contextTerms {
		for _, t := range terms {
			// Only consider type definitions (not fields) for cross-context analysis
			if len(t.Types) > 0 {
				termContexts[t.Term] = append(termContexts[t.Term], ctx)
			}
		}
	}

	// Filter to only terms appearing in multiple contexts
	crossContext := make(map[string][]string)
	for term, contexts := range termContexts {
		if len(contexts) > 1 {
			// Deduplicate contexts
			seen := make(map[string]bool)
			var unique []string
			for _, ctx := range contexts {
				if !seen[ctx] {
					seen[ctx] = true
					unique = append(unique, ctx)
				}
			}
			if len(unique) > 1 {
				sort.Strings(unique)
				crossContext[term] = unique
			}
		}
	}

	return crossContext
}
