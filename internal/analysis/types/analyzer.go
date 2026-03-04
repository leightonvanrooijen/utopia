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
	Name       string             // The type name (e.g., "DomainDoc", "UserAccount")
	Kind       string             // "struct", "interface", "type", "class"
	FilePath   string             // Relative path to source file
	LineNumber int                // Line number where the type is defined
	Fields     []DiscoveredField  // Fields/properties of the type
	Methods    []DiscoveredMethod // Methods of the type (for interfaces)
	Language   string             // "go" or "typescript"

	// Filtering metadata
	OriginalName     string // Original name before domain extraction (e.g., "OrderService")
	WasFiltered      bool   // True if this type was filtered but domain term extracted
	FilterReason     string // Why the original was filtered (e.g., "has generic suffix: Service")
	ExtractedFromTerm string // The term this was extracted from (e.g., "OrderService" -> "Order")
}

// DiscoveredField represents a field within a type definition
type DiscoveredField struct {
	Name       string // Field name
	Type       string // Field type (if extractable)
	LineNumber int    // Line number where the field is defined
	IsEmbedded bool   // True if this is an embedded/anonymous field (Go) or extends (TS)
}

// DiscoveredMethod represents a method signature within a type definition (interfaces)
type DiscoveredMethod struct {
	Name        string   // Method name
	Parameters  []string // Parameter types
	ReturnTypes []string // Return types
	LineNumber  int      // Line number where the method is defined
}

// TermOccurrence tracks where a term appears across the codebase
type TermOccurrence struct {
	Term       string            // The domain term
	Files      []string          // Files where this term appears
	Lines      []string          // Specific code lines (file:line format)
	Confidence TermConfidence    // Based on occurrence count and type
	Types      []*DiscoveredType // Types that use this term
}

// TermConfidence indicates how likely a term is to be domain vocabulary
type TermConfidence string

const (
	TermConfidenceHigh   TermConfidence = "high"   // Appears in multiple files as a type
	TermConfidenceMedium TermConfidence = "medium" // Appears in one file as a type or multiple as fields
	TermConfidenceLow    TermConfidence = "low"    // Appears only as fields in one file
)

// legacyGenericTerms is kept for backward compatibility with existing code
// that may reference it. New code should use GenericTermFilter instead.
var legacyGenericTerms = map[string]bool{
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
	goEmbeddedRegex  *regexp.Regexp
	goMethodRegex    *regexp.Regexp
	tsInterfaceRegex *regexp.Regexp
	tsClassRegex     *regexp.Regexp
	tsTypeRegex      *regexp.Regexp
	tsFieldRegex     *regexp.Regexp
	tsMethodRegex    *regexp.Regexp
	tsExtendsRegex   *regexp.Regexp
	filter           *GenericTermFilter
}

// NewAnalyzer creates a new type definition analyzer
func NewAnalyzer() *Analyzer {
	return NewAnalyzerWithFilter(NewGenericTermFilter())
}

// NewAnalyzerWithFilter creates a new analyzer with a custom filter
func NewAnalyzerWithFilter(filter *GenericTermFilter) *Analyzer {
	return &Analyzer{
		// Go patterns
		goStructRegex:    regexp.MustCompile(`^type\s+([A-Z][a-zA-Z0-9]*)\s+struct\s*\{`),
		goInterfaceRegex: regexp.MustCompile(`^type\s+([A-Z][a-zA-Z0-9]*)\s+interface\s*\{`),
		goTypeAliasRegex: regexp.MustCompile(`^type\s+([A-Z][a-zA-Z0-9]*)\s+(\w+)`),
		// Go field: captures name and type (handles pointers, slices, maps)
		// Examples: "Name string", "Items []Item", "User *User", "Data map[string]Value"
		goFieldRegex: regexp.MustCompile(`^\s+([A-Z][a-zA-Z0-9]*)\s+(\*?\[?\]?\*?(?:map\[[^\]]+\])?[A-Za-z][A-Za-z0-9]*)`),
		// Go embedded field: just a type name on its own line (no field name)
		// Examples: "  User" (embedded struct), "  *Config" (embedded pointer)
		goEmbeddedRegex: regexp.MustCompile(`^\s+(\*?)([A-Z][a-zA-Z0-9]*)\s*$`),
		// Go method signature in interface: "MethodName(params) (returns)"
		goMethodRegex: regexp.MustCompile(`^\s+([A-Z][a-zA-Z0-9]*)\s*\(([^)]*)\)\s*(?:\(([^)]*)\)|([A-Za-z*\[\]]+))?`),
		// TypeScript patterns
		tsInterfaceRegex: regexp.MustCompile(`^(?:export\s+)?interface\s+([A-Z][a-zA-Z0-9]*)`),
		tsClassRegex:     regexp.MustCompile(`^(?:export\s+)?(?:abstract\s+)?class\s+([A-Z][a-zA-Z0-9]*)`),
		tsTypeRegex:      regexp.MustCompile(`^(?:export\s+)?type\s+([A-Z][a-zA-Z0-9]*)\s*=`),
		// TypeScript field: captures name and type
		// Examples: "name: string", "items: Item[]", "user?: User"
		tsFieldRegex: regexp.MustCompile(`^\s+(?:readonly\s+)?([a-zA-Z][a-zA-Z0-9]*)\s*\??\s*:\s*([^;=]+)`),
		// TypeScript method signature: "methodName(params): returnType"
		tsMethodRegex: regexp.MustCompile(`^\s+([a-zA-Z][a-zA-Z0-9]*)\s*\(([^)]*)\)\s*:\s*([^;{]+)`),
		// TypeScript extends clause: "interface Foo extends Bar, Baz"
		tsExtendsRegex: regexp.MustCompile(`extends\s+([A-Z][a-zA-Z0-9]*(?:\s*,\s*[A-Z][a-zA-Z0-9]*)*)`),
		filter:         filter,
	}
}

// SetIncludeFiltered enables including filtered terms in results for review
func (a *Analyzer) SetIncludeFiltered(include bool) {
	a.filter.IncludeFiltered = include
}

// GetFilter returns the analyzer's generic term filter
func (a *Analyzer) GetFilter() *GenericTermFilter {
	return a.filter
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
			result := a.filterTerm(typeName)

			if !result.IsFiltered {
				// Not filtered - use as-is
				currentType = &DiscoveredType{
					Name:       typeName,
					Kind:       "struct",
					FilePath:   filePath,
					LineNumber: lineNum,
					Language:   "go",
				}
				types = append(types, currentType)
			} else if result.ExtractedDomainTerm != "" {
				// Filtered but has extractable domain term (e.g., "OrderService" -> "Order")
				currentType = &DiscoveredType{
					Name:              result.ExtractedDomainTerm,
					Kind:              "struct",
					FilePath:          filePath,
					LineNumber:        lineNum,
					Language:          "go",
					OriginalName:      typeName,
					WasFiltered:       true,
					FilterReason:      result.Reason,
					ExtractedFromTerm: typeName,
				}
				types = append(types, currentType)
			} else if a.filter.IncludeFiltered {
				// Pure generic term but include for review
				currentType = &DiscoveredType{
					Name:         typeName,
					Kind:         "struct",
					FilePath:     filePath,
					LineNumber:   lineNum,
					Language:     "go",
					WasFiltered:  true,
					FilterReason: result.Reason,
				}
				types = append(types, currentType)
			} else {
				// Skip type but still track brace depth for field extraction
				currentType = nil
			}
			braceDepth = 1
			continue
		}

		// Check for interface definition
		if matches := a.goInterfaceRegex.FindStringSubmatch(trimmedLine); matches != nil {
			typeName := matches[1]
			result := a.filterTerm(typeName)

			if !result.IsFiltered {
				currentType = &DiscoveredType{
					Name:       typeName,
					Kind:       "interface",
					FilePath:   filePath,
					LineNumber: lineNum,
					Language:   "go",
				}
				types = append(types, currentType)
			} else if result.ExtractedDomainTerm != "" {
				currentType = &DiscoveredType{
					Name:              result.ExtractedDomainTerm,
					Kind:              "interface",
					FilePath:          filePath,
					LineNumber:        lineNum,
					Language:          "go",
					OriginalName:      typeName,
					WasFiltered:       true,
					FilterReason:      result.Reason,
					ExtractedFromTerm: typeName,
				}
				types = append(types, currentType)
			} else if a.filter.IncludeFiltered {
				currentType = &DiscoveredType{
					Name:         typeName,
					Kind:         "interface",
					FilePath:     filePath,
					LineNumber:   lineNum,
					Language:     "go",
					WasFiltered:  true,
					FilterReason: result.Reason,
				}
				types = append(types, currentType)
			} else {
				currentType = nil
			}
			braceDepth = 1
			continue
		}

		// Check for type alias (not struct or interface)
		if currentType == nil && strings.HasPrefix(trimmedLine, "type ") {
			if matches := a.goTypeAliasRegex.FindStringSubmatch(trimmedLine); matches != nil {
				typeName := matches[1]
				baseType := matches[2]
				// Skip if it's struct or interface (handled above)
				if baseType != "struct" && baseType != "interface" {
					result := a.filterTerm(typeName)

					if !result.IsFiltered {
						types = append(types, &DiscoveredType{
							Name:       typeName,
							Kind:       "type",
							FilePath:   filePath,
							LineNumber: lineNum,
							Language:   "go",
						})
					} else if result.ExtractedDomainTerm != "" {
						types = append(types, &DiscoveredType{
							Name:              result.ExtractedDomainTerm,
							Kind:              "type",
							FilePath:          filePath,
							LineNumber:        lineNum,
							Language:          "go",
							OriginalName:      typeName,
							WasFiltered:       true,
							FilterReason:      result.Reason,
							ExtractedFromTerm: typeName,
						})
					} else if a.filter.IncludeFiltered {
						types = append(types, &DiscoveredType{
							Name:         typeName,
							Kind:         "type",
							FilePath:     filePath,
							LineNumber:   lineNum,
							Language:     "go",
							WasFiltered:  true,
							FilterReason: result.Reason,
						})
					}
				}
			}
			continue
		}

		// Track fields/methods within struct/interface
		if braceDepth > 0 {
			// Count braces
			braceDepth += strings.Count(trimmedLine, "{") - strings.Count(trimmedLine, "}")

			// Only extract fields/methods if we're tracking a type
			if currentType != nil {
				// For interfaces, extract method signatures
				if currentType.Kind == "interface" {
					if matches := a.goMethodRegex.FindStringSubmatch(line); matches != nil {
						methodName := matches[1]
						result := a.filterTerm(methodName)
						effectiveName := methodName
						if result.IsFiltered && result.ExtractedDomainTerm != "" {
							effectiveName = result.ExtractedDomainTerm
						}
						if !result.IsFiltered || result.ExtractedDomainTerm != "" || a.filter.IncludeFiltered {
							params := a.extractGoTypes(matches[2])
							var returns []string
							if matches[3] != "" {
								returns = a.extractGoTypes(matches[3])
							} else if matches[4] != "" {
								returns = a.extractGoTypes(matches[4])
							}
							currentType.Methods = append(currentType.Methods, DiscoveredMethod{
								Name:        effectiveName,
								Parameters:  params,
								ReturnTypes: returns,
								LineNumber:  lineNum,
							})
						}
					}
				}

				// For structs, extract fields
				if currentType.Kind == "struct" {
					// Check for embedded field first
					if matches := a.goEmbeddedRegex.FindStringSubmatch(line); matches != nil {
						typeName := matches[2]
						result := a.filterTerm(typeName)
						effectiveName := typeName
						if result.IsFiltered && result.ExtractedDomainTerm != "" {
							effectiveName = result.ExtractedDomainTerm
						}
						if !result.IsFiltered || result.ExtractedDomainTerm != "" || a.filter.IncludeFiltered {
							currentType.Fields = append(currentType.Fields, DiscoveredField{
								Name:       effectiveName,
								Type:       typeName,
								LineNumber: lineNum,
								IsEmbedded: true,
							})
						}
					} else if matches := a.goFieldRegex.FindStringSubmatch(line); matches != nil {
						fieldName := matches[1]
						fieldType := a.normalizeGoType(matches[2])
						result := a.filterTerm(fieldName)
						effectiveName := fieldName
						if result.IsFiltered && result.ExtractedDomainTerm != "" {
							effectiveName = result.ExtractedDomainTerm
						}
						if !result.IsFiltered || result.ExtractedDomainTerm != "" || a.filter.IncludeFiltered {
							currentType.Fields = append(currentType.Fields, DiscoveredField{
								Name:       effectiveName,
								Type:       fieldType,
								LineNumber: lineNum,
								IsEmbedded: false,
							})
						}
					}
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

// extractGoTypes extracts type names from a Go parameter/return list
// Examples: "ctx context.Context, id string" -> ["Context"], "*User, error" -> ["User"]
func (a *Analyzer) extractGoTypes(typeList string) []string {
	if typeList == "" {
		return nil
	}

	var types []string
	// Split by comma for multiple params/returns
	parts := strings.Split(typeList, ",")

	for _, part := range parts {
		part = strings.TrimSpace(part)
		// Extract the type (last word, handling pointers and slices)
		// For "ctx context.Context", we want "Context"
		// For "*User", we want "User"
		// For "[]Item", we want "Item"
		normalized := a.normalizeGoType(part)
		if normalized != "" {
			// Extract just the type name (after any package prefix)
			if idx := strings.LastIndex(normalized, "."); idx >= 0 {
				normalized = normalized[idx+1:]
			}
			// Only include exported types (starting with uppercase)
			if len(normalized) > 0 && normalized[0] >= 'A' && normalized[0] <= 'Z' {
				types = append(types, normalized)
			}
		}
	}

	return types
}

// normalizeGoType extracts the core type name from a Go type expression
// Examples: "*User" -> "User", "[]Item" -> "Item", "map[string]Value" -> "Value"
func (a *Analyzer) normalizeGoType(typeExpr string) string {
	typeExpr = strings.TrimSpace(typeExpr)

	// Handle "name type" format (e.g., "id string")
	parts := strings.Fields(typeExpr)
	if len(parts) >= 2 {
		typeExpr = parts[len(parts)-1]
	}

	// Remove pointer prefix
	typeExpr = strings.TrimPrefix(typeExpr, "*")

	// Remove slice prefix
	typeExpr = strings.TrimPrefix(typeExpr, "[]")
	typeExpr = strings.TrimPrefix(typeExpr, "*") // Handle []*Type

	// Handle map - extract value type
	if strings.HasPrefix(typeExpr, "map[") {
		// Find the closing bracket of the key type
		bracketCount := 1
		for i := 4; i < len(typeExpr); i++ {
			if typeExpr[i] == '[' {
				bracketCount++
			} else if typeExpr[i] == ']' {
				bracketCount--
				if bracketCount == 0 {
					typeExpr = typeExpr[i+1:]
					break
				}
			}
		}
		typeExpr = strings.TrimPrefix(typeExpr, "*")
	}

	// Extract just the type name (after any package prefix)
	if idx := strings.LastIndex(typeExpr, "."); idx >= 0 {
		typeExpr = typeExpr[idx+1:]
	}

	return typeExpr
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
			result := a.filterTerm(typeName)

			if !result.IsFiltered {
				currentType = &DiscoveredType{
					Name:       typeName,
					Kind:       "interface",
					FilePath:   filePath,
					LineNumber: lineNum,
					Language:   "typescript",
				}
				types = append(types, currentType)
			} else if result.ExtractedDomainTerm != "" {
				currentType = &DiscoveredType{
					Name:              result.ExtractedDomainTerm,
					Kind:              "interface",
					FilePath:          filePath,
					LineNumber:        lineNum,
					Language:          "typescript",
					OriginalName:      typeName,
					WasFiltered:       true,
					FilterReason:      result.Reason,
					ExtractedFromTerm: typeName,
				}
				types = append(types, currentType)
			} else if a.filter.IncludeFiltered {
				currentType = &DiscoveredType{
					Name:         typeName,
					Kind:         "interface",
					FilePath:     filePath,
					LineNumber:   lineNum,
					Language:     "typescript",
					WasFiltered:  true,
					FilterReason: result.Reason,
				}
				types = append(types, currentType)
			} else {
				currentType = nil
			}

			// Check for extends clause to capture inheritance as embedded fields
			if currentType != nil {
				if extendsMatches := a.tsExtendsRegex.FindStringSubmatch(trimmedLine); extendsMatches != nil {
					extendedTypes := strings.Split(extendsMatches[1], ",")
					for _, ext := range extendedTypes {
						ext = strings.TrimSpace(ext)
						if ext == "" {
							continue
						}
						extResult := a.filterTerm(ext)
						effectiveName := ext
						if extResult.IsFiltered && extResult.ExtractedDomainTerm != "" {
							effectiveName = extResult.ExtractedDomainTerm
						}
						if !extResult.IsFiltered || extResult.ExtractedDomainTerm != "" || a.filter.IncludeFiltered {
							currentType.Fields = append(currentType.Fields, DiscoveredField{
								Name:       effectiveName,
								Type:       ext,
								LineNumber: lineNum,
								IsEmbedded: true,
							})
						}
					}
				}
			}

			if strings.Contains(trimmedLine, "{") {
				braceDepth = 1
			}
			continue
		}

		// Check for class definition
		if matches := a.tsClassRegex.FindStringSubmatch(trimmedLine); matches != nil {
			typeName := matches[1]
			result := a.filterTerm(typeName)

			if !result.IsFiltered {
				currentType = &DiscoveredType{
					Name:       typeName,
					Kind:       "class",
					FilePath:   filePath,
					LineNumber: lineNum,
					Language:   "typescript",
				}
				types = append(types, currentType)
			} else if result.ExtractedDomainTerm != "" {
				currentType = &DiscoveredType{
					Name:              result.ExtractedDomainTerm,
					Kind:              "class",
					FilePath:          filePath,
					LineNumber:        lineNum,
					Language:          "typescript",
					OriginalName:      typeName,
					WasFiltered:       true,
					FilterReason:      result.Reason,
					ExtractedFromTerm: typeName,
				}
				types = append(types, currentType)
			} else if a.filter.IncludeFiltered {
				currentType = &DiscoveredType{
					Name:         typeName,
					Kind:         "class",
					FilePath:     filePath,
					LineNumber:   lineNum,
					Language:     "typescript",
					WasFiltered:  true,
					FilterReason: result.Reason,
				}
				types = append(types, currentType)
			} else {
				currentType = nil
			}

			// Check for extends clause
			if currentType != nil {
				if extendsMatches := a.tsExtendsRegex.FindStringSubmatch(trimmedLine); extendsMatches != nil {
					extendedTypes := strings.Split(extendsMatches[1], ",")
					for _, ext := range extendedTypes {
						ext = strings.TrimSpace(ext)
						if ext == "" {
							continue
						}
						extResult := a.filterTerm(ext)
						effectiveName := ext
						if extResult.IsFiltered && extResult.ExtractedDomainTerm != "" {
							effectiveName = extResult.ExtractedDomainTerm
						}
						if !extResult.IsFiltered || extResult.ExtractedDomainTerm != "" || a.filter.IncludeFiltered {
							currentType.Fields = append(currentType.Fields, DiscoveredField{
								Name:       effectiveName,
								Type:       ext,
								LineNumber: lineNum,
								IsEmbedded: true,
							})
						}
					}
				}
			}

			if strings.Contains(trimmedLine, "{") {
				braceDepth = 1
			}
			continue
		}

		// Check for type alias
		if matches := a.tsTypeRegex.FindStringSubmatch(trimmedLine); matches != nil {
			typeName := matches[1]
			result := a.filterTerm(typeName)

			if !result.IsFiltered {
				types = append(types, &DiscoveredType{
					Name:       typeName,
					Kind:       "type",
					FilePath:   filePath,
					LineNumber: lineNum,
					Language:   "typescript",
				})
			} else if result.ExtractedDomainTerm != "" {
				types = append(types, &DiscoveredType{
					Name:              result.ExtractedDomainTerm,
					Kind:              "type",
					FilePath:          filePath,
					LineNumber:        lineNum,
					Language:          "typescript",
					OriginalName:      typeName,
					WasFiltered:       true,
					FilterReason:      result.Reason,
					ExtractedFromTerm: typeName,
				})
			} else if a.filter.IncludeFiltered {
				types = append(types, &DiscoveredType{
					Name:         typeName,
					Kind:         "type",
					FilePath:     filePath,
					LineNumber:   lineNum,
					Language:     "typescript",
					WasFiltered:  true,
					FilterReason: result.Reason,
				})
			}
			continue
		}

		// Track fields/methods within interface/class
		if braceDepth > 0 {
			// Count braces
			braceDepth += strings.Count(trimmedLine, "{") - strings.Count(trimmedLine, "}")

			if currentType != nil {
				// Check for method signature first
				if matches := a.tsMethodRegex.FindStringSubmatch(line); matches != nil {
					methodName := matches[1]
					if !a.isCommonMethodName(methodName) {
						result := a.filterTerm(methodName)
						effectiveName := methodName
						if result.IsFiltered && result.ExtractedDomainTerm != "" {
							effectiveName = result.ExtractedDomainTerm
						}
						if !result.IsFiltered || result.ExtractedDomainTerm != "" || a.filter.IncludeFiltered {
							params := a.extractTSTypes(matches[2])
							returns := a.extractTSTypes(matches[3])
							currentType.Methods = append(currentType.Methods, DiscoveredMethod{
								Name:        effectiveName,
								Parameters:  params,
								ReturnTypes: returns,
								LineNumber:  lineNum,
							})
						}
					}
				} else if matches := a.tsFieldRegex.FindStringSubmatch(line); matches != nil {
					fieldName := matches[1]
					fieldType := a.normalizeTSType(matches[2])
					if !a.isCommonMethodName(fieldName) {
						result := a.filterTerm(fieldName)
						effectiveName := fieldName
						if result.IsFiltered && result.ExtractedDomainTerm != "" {
							effectiveName = result.ExtractedDomainTerm
						}
						if !result.IsFiltered || result.ExtractedDomainTerm != "" || a.filter.IncludeFiltered {
							currentType.Fields = append(currentType.Fields, DiscoveredField{
								Name:       effectiveName,
								Type:       fieldType,
								LineNumber: lineNum,
								IsEmbedded: false,
							})
						}
					}
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

// extractTSTypes extracts type names from a TypeScript parameter/return type
// Examples: "user: User, id: string" -> ["User"], "Promise<Order>" -> ["Order"]
func (a *Analyzer) extractTSTypes(typeExpr string) []string {
	if typeExpr == "" {
		return nil
	}

	var types []string
	// Split by comma for multiple params
	parts := strings.Split(typeExpr, ",")

	for _, part := range parts {
		normalized := a.normalizeTSType(part)
		if normalized != "" && len(normalized) > 0 && normalized[0] >= 'A' && normalized[0] <= 'Z' {
			types = append(types, normalized)
		}
	}

	return types
}

// normalizeTSType extracts the core type name from a TypeScript type expression
// Examples: "User[]" -> "User", "Promise<Order>" -> "Order", "user: User" -> "User"
func (a *Analyzer) normalizeTSType(typeExpr string) string {
	typeExpr = strings.TrimSpace(typeExpr)

	// Handle "name: Type" format
	if colonIdx := strings.Index(typeExpr, ":"); colonIdx >= 0 {
		typeExpr = strings.TrimSpace(typeExpr[colonIdx+1:])
	}

	// Remove array suffix
	typeExpr = strings.TrimSuffix(typeExpr, "[]")

	// Handle Promise<Type>, Array<Type>, etc.
	if ltIdx := strings.Index(typeExpr, "<"); ltIdx >= 0 {
		// Extract the inner type
		inner := typeExpr[ltIdx+1:]
		if gtIdx := strings.LastIndex(inner, ">"); gtIdx >= 0 {
			inner = inner[:gtIdx]
		}
		// Use the inner type if it's a domain type
		inner = strings.TrimSpace(inner)
		if len(inner) > 0 && inner[0] >= 'A' && inner[0] <= 'Z' {
			typeExpr = inner
		} else {
			// Otherwise use the outer type (e.g., Promise -> skip)
			typeExpr = typeExpr[:ltIdx]
		}
	}

	// Handle union types - take first domain type
	if pipeIdx := strings.Index(typeExpr, "|"); pipeIdx >= 0 {
		parts := strings.Split(typeExpr, "|")
		for _, part := range parts {
			part = strings.TrimSpace(part)
			if len(part) > 0 && part[0] >= 'A' && part[0] <= 'Z' {
				typeExpr = part
				break
			}
		}
	}

	typeExpr = strings.TrimSpace(typeExpr)

	// Remove any remaining array brackets
	typeExpr = strings.TrimSuffix(typeExpr, "[]")

	return typeExpr
}

// isGenericTerm checks if a term is a generic programming term to filter out.
// Note: This returns true for terms that should be filtered, but the caller
// should also check for extracted domain terms via filterTerm().
func (a *Analyzer) isGenericTerm(term string) bool {
	result := a.filter.Filter(term)
	return result.IsFiltered
}

// filterTerm filters a term and returns both whether it should be filtered
// and any domain term that was extracted from it.
func (a *Analyzer) filterTerm(term string) FilterResult {
	return a.filter.Filter(term)
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
