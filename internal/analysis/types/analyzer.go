// Package types provides static analysis of type definitions in source files.
// It extracts domain vocabulary from struct/interface/class definitions in Go and TypeScript.
package types

import (
	"bufio"
	"regexp"
	"sort"
	"strings"
)

// DiscoveredType represents a type definition found in source code
type DiscoveredType struct {
	Name       string            // The type name (e.g., "DomainDoc", "UserAccount")
	Kind       string            // "struct", "interface", "type", "class"
	FilePath   string            // Relative path to source file
	LineNumber int               // Line number where the type is defined
	Fields     []DiscoveredField // Fields/properties of the type
	Language   string            // "go" or "typescript"
}

// DiscoveredField represents a field within a type definition
type DiscoveredField struct {
	Name       string // Field name
	Type       string // Field type (if extractable)
	LineNumber int    // Line number where the field is defined
}

// TermOccurrence tracks where a term appears across the codebase
type TermOccurrence struct {
	Term       string              // The domain term
	Files      []string            // Files where this term appears
	Lines      []string            // Specific code lines (file:line format)
	Confidence TermConfidence      // Based on occurrence count and type
	Types      []*DiscoveredType   // Types that use this term
}

// TermConfidence indicates how likely a term is to be domain vocabulary
type TermConfidence string

const (
	TermConfidenceHigh   TermConfidence = "high"   // Appears in multiple files as a type
	TermConfidenceMedium TermConfidence = "medium" // Appears in one file as a type or multiple as fields
	TermConfidenceLow    TermConfidence = "low"    // Appears only as fields in one file
)

// genericTerms are programming terms that should be filtered out as they're
// not domain-specific vocabulary
var genericTerms = map[string]bool{
	"Handler":    true,
	"Manager":    true,
	"Service":    true,
	"Repository": true,
	"Controller": true,
	"Factory":    true,
	"Builder":    true,
	"Adapter":    true,
	"Wrapper":    true,
	"Helper":     true,
	"Util":       true,
	"Utils":      true,
	"Base":       true,
	"Abstract":   true,
	"Default":    true,
	"Common":     true,
	"Generic":    true,
	"Internal":   true,
}

// Analyzer extracts type definitions from source files
type Analyzer struct {
	goStructRegex    *regexp.Regexp
	goInterfaceRegex *regexp.Regexp
	goTypeAliasRegex *regexp.Regexp
	goFieldRegex     *regexp.Regexp
	tsInterfaceRegex *regexp.Regexp
	tsClassRegex     *regexp.Regexp
	tsTypeRegex      *regexp.Regexp
	tsFieldRegex     *regexp.Regexp
}

// NewAnalyzer creates a new type definition analyzer
func NewAnalyzer() *Analyzer {
	return &Analyzer{
		// Go patterns
		goStructRegex:    regexp.MustCompile(`^type\s+([A-Z][a-zA-Z0-9]*)\s+struct\s*\{`),
		goInterfaceRegex: regexp.MustCompile(`^type\s+([A-Z][a-zA-Z0-9]*)\s+interface\s*\{`),
		goTypeAliasRegex: regexp.MustCompile(`^type\s+([A-Z][a-zA-Z0-9]*)\s+(\w+)`),
		goFieldRegex:     regexp.MustCompile(`^\s+([A-Z][a-zA-Z0-9]*)\s+`),
		// TypeScript patterns
		tsInterfaceRegex: regexp.MustCompile(`^(?:export\s+)?interface\s+([A-Z][a-zA-Z0-9]*)`),
		tsClassRegex:     regexp.MustCompile(`^(?:export\s+)?(?:abstract\s+)?class\s+([A-Z][a-zA-Z0-9]*)`),
		tsTypeRegex:      regexp.MustCompile(`^(?:export\s+)?type\s+([A-Z][a-zA-Z0-9]*)\s*=`),
		tsFieldRegex:     regexp.MustCompile(`^\s+(?:readonly\s+)?([a-zA-Z][a-zA-Z0-9]*)\s*[?:]`),
	}
}

// AnalyzeGoFile extracts type definitions from Go source code
func (a *Analyzer) AnalyzeGoFile(filePath, content string) []*DiscoveredType {
	var types []*DiscoveredType
	scanner := bufio.NewScanner(strings.NewReader(content))
	lineNum := 0
	var currentType *DiscoveredType
	braceDepth := 0

	for scanner.Scan() {
		lineNum++
		line := scanner.Text()
		trimmedLine := strings.TrimSpace(line)

		// Check for struct definition
		if matches := a.goStructRegex.FindStringSubmatch(trimmedLine); matches != nil {
			typeName := matches[1]
			if !a.isGenericTerm(typeName) {
				currentType = &DiscoveredType{
					Name:       typeName,
					Kind:       "struct",
					FilePath:   filePath,
					LineNumber: lineNum,
					Language:   "go",
				}
				types = append(types, currentType)
				braceDepth = 1
			}
			continue
		}

		// Check for interface definition
		if matches := a.goInterfaceRegex.FindStringSubmatch(trimmedLine); matches != nil {
			typeName := matches[1]
			if !a.isGenericTerm(typeName) {
				currentType = &DiscoveredType{
					Name:       typeName,
					Kind:       "interface",
					FilePath:   filePath,
					LineNumber: lineNum,
					Language:   "go",
				}
				types = append(types, currentType)
				braceDepth = 1
			}
			continue
		}

		// Check for type alias (not struct or interface)
		if currentType == nil && strings.HasPrefix(trimmedLine, "type ") {
			if matches := a.goTypeAliasRegex.FindStringSubmatch(trimmedLine); matches != nil {
				typeName := matches[1]
				baseType := matches[2]
				// Skip if it's struct or interface (handled above)
				if baseType != "struct" && baseType != "interface" && !a.isGenericTerm(typeName) {
					types = append(types, &DiscoveredType{
						Name:       typeName,
						Kind:       "type",
						FilePath:   filePath,
						LineNumber: lineNum,
						Language:   "go",
					})
				}
			}
			continue
		}

		// Track fields within struct/interface
		if currentType != nil && braceDepth > 0 {
			// Count braces
			braceDepth += strings.Count(trimmedLine, "{") - strings.Count(trimmedLine, "}")

			// Extract field names (exported fields start with uppercase)
			if matches := a.goFieldRegex.FindStringSubmatch(line); matches != nil {
				fieldName := matches[1]
				if !a.isGenericTerm(fieldName) {
					currentType.Fields = append(currentType.Fields, DiscoveredField{
						Name:       fieldName,
						LineNumber: lineNum,
					})
				}
			}

			// Reset when struct/interface ends
			if braceDepth == 0 {
				currentType = nil
			}
		}
	}

	return types
}

// AnalyzeTypeScriptFile extracts type definitions from TypeScript source code
func (a *Analyzer) AnalyzeTypeScriptFile(filePath, content string) []*DiscoveredType {
	var types []*DiscoveredType
	scanner := bufio.NewScanner(strings.NewReader(content))
	lineNum := 0
	var currentType *DiscoveredType
	braceDepth := 0

	for scanner.Scan() {
		lineNum++
		line := scanner.Text()
		trimmedLine := strings.TrimSpace(line)

		// Skip comments and empty lines
		if strings.HasPrefix(trimmedLine, "//") || strings.HasPrefix(trimmedLine, "/*") || trimmedLine == "" {
			continue
		}

		// Check for interface definition
		if matches := a.tsInterfaceRegex.FindStringSubmatch(trimmedLine); matches != nil {
			typeName := matches[1]
			if !a.isGenericTerm(typeName) {
				currentType = &DiscoveredType{
					Name:       typeName,
					Kind:       "interface",
					FilePath:   filePath,
					LineNumber: lineNum,
					Language:   "typescript",
				}
				types = append(types, currentType)
				if strings.Contains(trimmedLine, "{") {
					braceDepth = 1
				}
			}
			continue
		}

		// Check for class definition
		if matches := a.tsClassRegex.FindStringSubmatch(trimmedLine); matches != nil {
			typeName := matches[1]
			if !a.isGenericTerm(typeName) {
				currentType = &DiscoveredType{
					Name:       typeName,
					Kind:       "class",
					FilePath:   filePath,
					LineNumber: lineNum,
					Language:   "typescript",
				}
				types = append(types, currentType)
				if strings.Contains(trimmedLine, "{") {
					braceDepth = 1
				}
			}
			continue
		}

		// Check for type alias
		if matches := a.tsTypeRegex.FindStringSubmatch(trimmedLine); matches != nil {
			typeName := matches[1]
			if !a.isGenericTerm(typeName) {
				types = append(types, &DiscoveredType{
					Name:       typeName,
					Kind:       "type",
					FilePath:   filePath,
					LineNumber: lineNum,
					Language:   "typescript",
				})
			}
			continue
		}

		// Track fields within interface/class
		if currentType != nil && braceDepth > 0 {
			// Count braces
			braceDepth += strings.Count(trimmedLine, "{") - strings.Count(trimmedLine, "}")

			// Extract field/property names
			if matches := a.tsFieldRegex.FindStringSubmatch(line); matches != nil {
				fieldName := matches[1]
				// Filter out common method names and generic terms
				if !a.isGenericTerm(fieldName) && !a.isCommonMethodName(fieldName) {
					currentType.Fields = append(currentType.Fields, DiscoveredField{
						Name:       fieldName,
						LineNumber: lineNum,
					})
				}
			}

			// Reset when type ends
			if braceDepth == 0 {
				currentType = nil
			}
		}
	}

	return types
}

// isGenericTerm checks if a term is a generic programming term to filter out
func (a *Analyzer) isGenericTerm(term string) bool {
	// Check exact match
	if genericTerms[term] {
		return true
	}

	// Check if term ends with a generic suffix
	for generic := range genericTerms {
		if strings.HasSuffix(term, generic) {
			return true
		}
	}

	return false
}

// isCommonMethodName filters out common method/property names
func (a *Analyzer) isCommonMethodName(name string) bool {
	commonMethods := map[string]bool{
		"constructor": true,
		"toString":    true,
		"valueOf":     true,
		"toJSON":      true,
		"get":         true,
		"set":         true,
		"id":          true,
		"type":        true,
		"name":        true,
		"value":       true,
		"data":        true,
		"config":      true,
		"options":     true,
		"props":       true,
		"state":       true,
		"error":       true,
		"message":     true,
		"result":      true,
		"status":      true,
		"length":      true,
		"size":        true,
		"count":       true,
		"index":       true,
		"key":         true,
	}
	return commonMethods[strings.ToLower(name)]
}

// AggregateTerms collects all discovered types and computes confidence based on occurrences
func (a *Analyzer) AggregateTerms(types []*DiscoveredType) map[string]*TermOccurrence {
	termMap := make(map[string]*TermOccurrence)

	for _, t := range types {
		// Add the type name as a term
		term := t.Name
		if _, exists := termMap[term]; !exists {
			termMap[term] = &TermOccurrence{
				Term:  term,
				Files: []string{},
				Lines: []string{},
			}
		}

		occ := termMap[term]
		lineRef := formatLineReference(t.FilePath, t.LineNumber)

		// Add file if not already present
		if !containsString(occ.Files, t.FilePath) {
			occ.Files = append(occ.Files, t.FilePath)
		}
		occ.Lines = append(occ.Lines, lineRef)
		occ.Types = append(occ.Types, t)

		// Also track field names as potential terms
		for _, field := range t.Fields {
			fieldTerm := field.Name
			if _, exists := termMap[fieldTerm]; !exists {
				termMap[fieldTerm] = &TermOccurrence{
					Term:  fieldTerm,
					Files: []string{},
					Lines: []string{},
				}
			}

			fieldOcc := termMap[fieldTerm]
			fieldLineRef := formatLineReference(t.FilePath, field.LineNumber)

			if !containsString(fieldOcc.Files, t.FilePath) {
				fieldOcc.Files = append(fieldOcc.Files, t.FilePath)
			}
			fieldOcc.Lines = append(fieldOcc.Lines, fieldLineRef)
		}
	}

	// Calculate confidence for each term
	for _, occ := range termMap {
		occ.Confidence = a.calculateConfidence(occ)
	}

	return termMap
}

// calculateConfidence determines confidence based on occurrence patterns
func (a *Analyzer) calculateConfidence(occ *TermOccurrence) TermConfidence {
	fileCount := len(occ.Files)
	hasTypeDefinition := len(occ.Types) > 0

	// High: Type defined in multiple files (indicates core domain concept)
	if hasTypeDefinition && fileCount > 1 {
		return TermConfidenceHigh
	}

	// Medium: Type defined in one file, or field appears in multiple files
	if hasTypeDefinition || fileCount > 1 {
		return TermConfidenceMedium
	}

	// Low: Only appears as field in one file
	return TermConfidenceLow
}

// GetHighConfidenceTerms returns terms sorted by confidence (high first)
func (a *Analyzer) GetHighConfidenceTerms(termMap map[string]*TermOccurrence) []*TermOccurrence {
	var terms []*TermOccurrence
	for _, occ := range termMap {
		terms = append(terms, occ)
	}

	// Sort by confidence (high > medium > low), then by file count, then alphabetically
	sort.Slice(terms, func(i, j int) bool {
		confidenceOrder := map[TermConfidence]int{
			TermConfidenceHigh:   0,
			TermConfidenceMedium: 1,
			TermConfidenceLow:    2,
		}

		if confidenceOrder[terms[i].Confidence] != confidenceOrder[terms[j].Confidence] {
			return confidenceOrder[terms[i].Confidence] < confidenceOrder[terms[j].Confidence]
		}

		if len(terms[i].Files) != len(terms[j].Files) {
			return len(terms[i].Files) > len(terms[j].Files)
		}

		return terms[i].Term < terms[j].Term
	})

	return terms
}

// formatLineReference creates a file:line reference string
func formatLineReference(filePath string, lineNumber int) string {
	return filePath + ":" + intToString(lineNumber)
}

// intToString converts an int to string without importing strconv
func intToString(n int) string {
	if n == 0 {
		return "0"
	}

	var result []byte
	negative := n < 0
	if negative {
		n = -n
	}

	for n > 0 {
		result = append([]byte{byte('0' + n%10)}, result...)
		n /= 10
	}

	if negative {
		result = append([]byte{'-'}, result...)
	}

	return string(result)
}

// containsString checks if a slice contains a string
func containsString(slice []string, s string) bool {
	for _, item := range slice {
		if item == s {
			return true
		}
	}
	return false
}
