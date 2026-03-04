// Package types provides static analysis of type definitions in source files.
package types

import (
	"strings"
	"unicode"
)

// GenericTermFilter filters out generic programming terms from domain vocabulary.
// It identifies terms that are purely technical (Handler, Service, etc.) versus
// terms that contain domain concepts with technical suffixes (OrderService -> Order).
type GenericTermFilter struct {
	// IncludeFiltered when true, includes filtered terms in results for review
	IncludeFiltered bool
}

// FilterResult contains the outcome of filtering a term
type FilterResult struct {
	// OriginalTerm is the input term before filtering
	OriginalTerm string

	// IsFiltered indicates whether this term should be excluded from domain vocabulary
	IsFiltered bool

	// Reason explains why the term was filtered (empty if not filtered)
	Reason string

	// ExtractedDomainTerm is the domain prefix extracted from a term with generic suffix
	// For example, "OrderService" extracts "Order"
	// Empty if no domain term could be extracted
	ExtractedDomainTerm string
}

// genericSuffixes are technical suffixes that indicate implementation patterns, not domain concepts.
// When a term ends with these, we extract the prefix as the potential domain term.
// NOTE: This list intentionally excludes domain-preserving suffixes like "Item", "Model", "Entity"
// because terms like "OrderItem", "LineItem", "UserModel" are legitimate domain concepts.
var genericSuffixes = []string{
	// Architectural/Design Pattern Suffixes - these indicate technical implementation
	"Handler",
	"Manager",
	"Service",
	"Repository",
	"Controller",
	"Factory",
	"Builder",
	"Adapter",
	"Wrapper",
	"Decorator",
	"Facade",
	"Proxy",
	"Observer",
	"Listener",
	"Provider",
	"Consumer",
	"Producer",
	"Processor",
	"Executor",
	"Scheduler",
	"Dispatcher",
	"Resolver",
	"Validator",
	"Converter",
	"Transformer",
	"Mapper",
	"Serializer",
	"Deserializer",
	"Parser",
	"Formatter",
	"Renderer",
	"Generator",
	"Loader",
	"Saver",
	"Reader",
	"Writer",
	"Client",
	"Server",
	"Gateway",
	"Middleware",
	"Interceptor",
	"Registry",
	// Utility/Helper Suffixes
	"Helper",
	"Util",
	"Utils",
	"Utility",
	"Utilities",
	// Implementation Detail Suffixes
	"Impl",
	"Implementation",
}

// genericPrefixes are technical prefixes that indicate implementation details.
// When a term starts with these, we extract the suffix as the potential domain term.
var genericPrefixes = []string{
	// Abstraction Prefixes
	"Base",
	"Abstract",
	"Default",
	"Generic",
	"Common",
	"Shared",
	"Core",
	"Internal",
	"Private",
	"Public",
	// Implementation Prefixes
	"Basic",
	"Simple",
	"Standard",
	"Custom",
	"Extended",
	"Enhanced",
	// Mock/Test Prefixes
	"Mock",
	"Stub",
	"Fake",
	"Test",
	"Dummy",
	// Interface/Implementation Prefixes
	"I", // e.g., IRepository (common in C#/Java conventions)
}

// pureGenericTerms are terms that are entirely generic with no domain meaning.
// These should be excluded entirely from domain vocabulary when used as TYPE names.
// NOTE: This list is intentionally minimal. Many programming terms like "Items",
// "Metadata", "Config" are NOT included because they often carry domain meaning
// when used as field names (e.g., Order.Items refers to line items).
var pureGenericTerms = map[string]bool{
	// Architectural pattern names (standalone only - no domain meaning when used alone)
	"Handler":      true,
	"Manager":      true,
	"Service":      true,
	"Repository":   true,
	"Controller":   true,
	"Factory":      true,
	"Builder":      true,
	"Adapter":      true,
	"Wrapper":      true,
	"Decorator":    true,
	"Facade":       true,
	"Proxy":        true,
	"Observer":     true,
	"Listener":     true,
	"Provider":     true,
	"Processor":    true,
	"Executor":     true,
	"Scheduler":    true,
	"Dispatcher":   true,
	"Resolver":     true,
	"Validator":    true,
	"Converter":    true,
	"Transformer":  true,
	"Mapper":       true,
	"Serializer":   true,
	"Deserializer": true,
	"Parser":       true,
	"Formatter":    true,
	"Renderer":     true,
	"Generator":    true,
	"Loader":       true,
	"Registry":     true,
	"Middleware":   true,
	"Interceptor":  true,
	"Iterator":     true,
	"Visitor":      true,
	"Mediator":     true,
	"Singleton":    true,
	"DAO":          true,
	"DTO":          true,
	// Utility terms (standalone only)
	"Helper":    true,
	"Helpers":   true,
	"Util":      true,
	"Utils":     true,
	"Utility":   true,
	"Utilities": true,
	"Impl":      true,
	// Abstraction prefixes when standalone
	"Base":     true,
	"Abstract": true,
	"Default":  true,
	"Generic":  true,
	"Common":   true,
	"Internal": true,
	// Testing terms (standalone)
	"Mock":      true,
	"Mocks":     true,
	"Stub":      true,
	"Stubs":     true,
	"Fake":      true,
	"Fakes":     true,
	"Fixture":   true,
	"Fixtures":  true,
	"Test":      true,
	"Tests":     true,
	"Benchmark": true,
}

// NewGenericTermFilter creates a new filter with default settings
func NewGenericTermFilter() *GenericTermFilter {
	return &GenericTermFilter{
		IncludeFiltered: false,
	}
}

// Filter analyzes a term and returns the filter result
func (f *GenericTermFilter) Filter(term string) FilterResult {
	result := FilterResult{
		OriginalTerm: term,
		IsFiltered:   false,
	}

	// Empty terms are filtered
	if term == "" {
		result.IsFiltered = true
		result.Reason = "empty term"
		return result
	}

	// Check if it's a pure generic term (exact match)
	if pureGenericTerms[term] {
		result.IsFiltered = true
		result.Reason = "pure generic term"
		return result
	}

	// Check for generic suffix and extract domain prefix
	for _, suffix := range genericSuffixes {
		if strings.HasSuffix(term, suffix) && len(term) > len(suffix) {
			prefix := term[:len(term)-len(suffix)]
			// Validate the prefix is a proper identifier (starts with uppercase)
			if len(prefix) > 0 && unicode.IsUpper(rune(prefix[0])) {
				// Check if the extracted prefix itself is generic
				if !pureGenericTerms[prefix] {
					result.ExtractedDomainTerm = prefix
				}
			}
			result.IsFiltered = true
			result.Reason = "has generic suffix: " + suffix
			return result
		}
	}

	// Check for generic prefix and extract domain suffix
	for _, prefix := range genericPrefixes {
		if strings.HasPrefix(term, prefix) && len(term) > len(prefix) {
			// Special handling for single-letter prefixes like "I"
			if len(prefix) == 1 {
				// Only consider it a prefix if:
				// 1. The next char is uppercase
				// 2. The remaining part is at least 3 characters (to avoid "ID" -> "D")
				// This distinguishes IRepository from short terms like "ID" or "IP"
				remaining := term[len(prefix):]
				if len(remaining) >= 3 && unicode.IsUpper(rune(remaining[0])) {
					if !pureGenericTerms[remaining] {
						result.ExtractedDomainTerm = remaining
					}
					result.IsFiltered = true
					result.Reason = "has generic prefix: " + prefix
					return result
				}
				continue
			}

			suffix := term[len(prefix):]
			// Validate the suffix is a proper identifier (starts with uppercase)
			if len(suffix) > 0 && unicode.IsUpper(rune(suffix[0])) {
				// Check if the extracted suffix itself is generic
				if !pureGenericTerms[suffix] {
					result.ExtractedDomainTerm = suffix
				}
				result.IsFiltered = true
				result.Reason = "has generic prefix: " + prefix
				return result
			}
		}
	}

	// Term passes filtering - it's a domain term
	return result
}

// FilterTerms processes multiple terms and returns both filtered and extracted domain terms
func (f *GenericTermFilter) FilterTerms(terms []string) (domainTerms []string, filteredResults []FilterResult) {
	seen := make(map[string]bool)

	for _, term := range terms {
		result := f.Filter(term)

		if f.IncludeFiltered && result.IsFiltered {
			filteredResults = append(filteredResults, result)
		}

		// Add the term itself if not filtered
		if !result.IsFiltered {
			if !seen[term] {
				domainTerms = append(domainTerms, term)
				seen[term] = true
			}
		}

		// Add extracted domain term if available
		if result.ExtractedDomainTerm != "" {
			if !seen[result.ExtractedDomainTerm] {
				domainTerms = append(domainTerms, result.ExtractedDomainTerm)
				seen[result.ExtractedDomainTerm] = true
			}
		}
	}

	return domainTerms, filteredResults
}

// IsPureGenericTerm checks if a term is entirely generic (no domain value)
func (f *GenericTermFilter) IsPureGenericTerm(term string) bool {
	return pureGenericTerms[term]
}

// HasGenericSuffix checks if a term ends with a generic suffix
func (f *GenericTermFilter) HasGenericSuffix(term string) (bool, string) {
	for _, suffix := range genericSuffixes {
		if strings.HasSuffix(term, suffix) && len(term) > len(suffix) {
			return true, suffix
		}
	}
	return false, ""
}

// HasGenericPrefix checks if a term starts with a generic prefix
func (f *GenericTermFilter) HasGenericPrefix(term string) (bool, string) {
	for _, prefix := range genericPrefixes {
		if strings.HasPrefix(term, prefix) && len(term) > len(prefix) {
			// Special handling for single-letter prefixes
			if len(prefix) == 1 {
				remaining := term[len(prefix):]
				if len(remaining) > 0 && unicode.IsUpper(rune(remaining[0])) {
					return true, prefix
				}
				continue
			}
			remaining := term[len(prefix):]
			if len(remaining) > 0 && unicode.IsUpper(rune(remaining[0])) {
				return true, prefix
			}
		}
	}
	return false, ""
}

// ExtractDomainTerm extracts the domain portion from a term with generic affixes
func (f *GenericTermFilter) ExtractDomainTerm(term string) string {
	result := f.Filter(term)
	if result.ExtractedDomainTerm != "" {
		return result.ExtractedDomainTerm
	}
	if !result.IsFiltered {
		return term
	}
	return ""
}
