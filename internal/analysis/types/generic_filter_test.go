package types

import (
	"testing"
)

func TestGenericTermFilter_PureGenericTerms(t *testing.T) {
	filter := NewGenericTermFilter()

	pureGenericCases := []string{
		"Handler",
		"Manager",
		"Service",
		"Repository",
		"Controller",
		"Factory",
		"Builder",
		"Helper",
		"Util",
		"Utils",
		"Base",
		"Abstract",
		"Default",
		"Mock",
		"Test",
	}

	for _, term := range pureGenericCases {
		result := filter.Filter(term)
		if !result.IsFiltered {
			t.Errorf("expected %q to be filtered as pure generic term", term)
		}
		if result.ExtractedDomainTerm != "" {
			t.Errorf("expected no extracted domain term for pure generic %q, got %q", term, result.ExtractedDomainTerm)
		}
	}
}

func TestGenericTermFilter_GenericSuffixExtraction(t *testing.T) {
	filter := NewGenericTermFilter()

	cases := []struct {
		input    string
		filtered bool
		extract  string
		reason   string
	}{
		{"OrderService", true, "Order", "has generic suffix: Service"},
		{"UserHandler", true, "User", "has generic suffix: Handler"},
		{"ProductRepository", true, "Product", "has generic suffix: Repository"},
		{"PaymentProcessor", true, "Payment", "has generic suffix: Processor"},
		{"CustomerValidator", true, "Customer", "has generic suffix: Validator"},
		{"InvoiceMapper", true, "Invoice", "has generic suffix: Mapper"},
		{"EmailHelper", true, "Email", "has generic suffix: Helper"},
		{"DateUtil", true, "Date", "has generic suffix: Util"},
		{"StringUtils", true, "String", "has generic suffix: Utils"},
		{"DataLoader", true, "Data", "has generic suffix: Loader"},
		{"OrderImpl", true, "Order", "has generic suffix: Impl"},
	}

	for _, c := range cases {
		result := filter.Filter(c.input)
		if result.IsFiltered != c.filtered {
			t.Errorf("Filter(%q): expected IsFiltered=%v, got %v", c.input, c.filtered, result.IsFiltered)
		}
		if result.ExtractedDomainTerm != c.extract {
			t.Errorf("Filter(%q): expected ExtractedDomainTerm=%q, got %q", c.input, c.extract, result.ExtractedDomainTerm)
		}
		if c.reason != "" && result.Reason != c.reason {
			t.Errorf("Filter(%q): expected Reason=%q, got %q", c.input, c.reason, result.Reason)
		}
	}
}

func TestGenericTermFilter_GenericPrefixExtraction(t *testing.T) {
	filter := NewGenericTermFilter()

	cases := []struct {
		input    string
		filtered bool
		extract  string
	}{
		{"BaseEntity", true, "Entity"},
		{"AbstractOrder", true, "Order"},
		{"DefaultConfig", true, "Config"},
		{"CommonUtils", true, ""}, // Utils is also generic, so no extraction
		{"GenericType", true, "Type"},
		{"InternalService", true, ""}, // Service is also generic
		{"MockRepository", true, ""}, // Repository is also generic
	}

	for _, c := range cases {
		result := filter.Filter(c.input)
		if result.IsFiltered != c.filtered {
			t.Errorf("Filter(%q): expected IsFiltered=%v, got %v", c.input, c.filtered, result.IsFiltered)
		}
		if result.ExtractedDomainTerm != c.extract {
			t.Errorf("Filter(%q): expected ExtractedDomainTerm=%q, got %q", c.input, c.extract, result.ExtractedDomainTerm)
		}
	}
}

func TestGenericTermFilter_DomainTermsNotFiltered(t *testing.T) {
	filter := NewGenericTermFilter()

	domainTerms := []string{
		"Order",
		"Customer",
		"Product",
		"Invoice",
		"Payment",
		"User",
		"Account",
		"LineItem",
		"ShoppingCart",
		"Inventory",
		"Shipment",
		"Address",
		"Email",
		"Phone",
		"Document",
		"Category",
		"Tag",
		"Comment",
		"Review",
		"Rating",
		"OrderItem",   // "Item" is not a generic suffix
		"OrderStatus", // "Status" is not a generic suffix
		"Metadata",    // Common but domain-meaningful as field name
		"Items",       // Common but domain-meaningful as field name
		"Config",      // Common but domain-meaningful as field name
	}

	for _, term := range domainTerms {
		result := filter.Filter(term)
		if result.IsFiltered {
			t.Errorf("expected %q to NOT be filtered, but it was (reason: %s)", term, result.Reason)
		}
	}
}

func TestGenericTermFilter_SingleLetterPrefixHandling(t *testing.T) {
	filter := NewGenericTermFilter()

	cases := []struct {
		input    string
		filtered bool
		extract  string
	}{
		// Suffix check happens first, so "IRepository" is filtered by "Repository" suffix
		{"IRepository", true, "I"},    // Repository suffix found, extracts "I" (but single char)
		{"IOrderService", true, "IOrder"}, // Service suffix found, extracts "IOrder"
		{"IOrder", true, "Order"},     // "I" prefix with "Order" suffix (>= 3 chars)
		{"ID", false, ""},             // "D" has only 1 char < 3, not I-prefix
		{"IP", false, ""},             // "P" has only 1 char < 3
		{"Ice", false, ""},            // Lowercase after I, not a prefix pattern
		{"Item", false, ""},           // Item is a domain term, no I-prefix pattern
		{"IAbcde", true, "Abcde"},     // "I" prefix, "Abcde" has 5 chars >= 3
		{"IAbc", true, "Abc"},         // "I" prefix, "Abc" has 3 chars == 3
		{"IAb", false, ""},            // "Ab" has 2 chars < 3, not I-prefix
	}

	for _, c := range cases {
		result := filter.Filter(c.input)
		if result.IsFiltered != c.filtered {
			t.Errorf("Filter(%q): expected IsFiltered=%v, got %v (reason: %s)", c.input, c.filtered, result.IsFiltered, result.Reason)
		}
		if result.ExtractedDomainTerm != c.extract {
			t.Errorf("Filter(%q): expected ExtractedDomainTerm=%q, got %q", c.input, c.extract, result.ExtractedDomainTerm)
		}
	}
}

func TestGenericTermFilter_FilterTermsDeduplication(t *testing.T) {
	filter := NewGenericTermFilter()

	// Input terms with potential duplicates after extraction
	terms := []string{
		"OrderService",   // -> "Order"
		"OrderHandler",   // -> "Order"
		"OrderRepository", // -> "Order"
		"Order",          // -> "Order" (not filtered)
		"Customer",       // -> "Customer" (not filtered)
	}

	domainTerms, _ := filter.FilterTerms(terms)

	// Should have unique domain terms only
	expected := map[string]bool{
		"Order":    true,
		"Customer": true,
	}

	if len(domainTerms) != len(expected) {
		t.Errorf("expected %d domain terms, got %d: %v", len(expected), len(domainTerms), domainTerms)
	}

	for _, term := range domainTerms {
		if !expected[term] {
			t.Errorf("unexpected domain term: %q", term)
		}
	}
}

func TestGenericTermFilter_IncludeFilteredFlag(t *testing.T) {
	filter := NewGenericTermFilter()
	filter.IncludeFiltered = true

	terms := []string{
		"OrderService",
		"UserHandler",
		"Order",
	}

	_, filteredResults := filter.FilterTerms(terms)

	// Should have 2 filtered results (OrderService and UserHandler)
	if len(filteredResults) != 2 {
		t.Errorf("expected 2 filtered results, got %d", len(filteredResults))
	}

	// Verify filtered results contain original terms and reasons
	foundOrderService := false
	foundUserHandler := false
	for _, fr := range filteredResults {
		if fr.OriginalTerm == "OrderService" {
			foundOrderService = true
			if fr.Reason != "has generic suffix: Service" {
				t.Errorf("expected reason 'has generic suffix: Service', got %q", fr.Reason)
			}
		}
		if fr.OriginalTerm == "UserHandler" {
			foundUserHandler = true
		}
	}

	if !foundOrderService {
		t.Error("expected OrderService in filtered results")
	}
	if !foundUserHandler {
		t.Error("expected UserHandler in filtered results")
	}
}

func TestGenericTermFilter_EmptyTerm(t *testing.T) {
	filter := NewGenericTermFilter()

	result := filter.Filter("")
	if !result.IsFiltered {
		t.Error("expected empty term to be filtered")
	}
	if result.Reason != "empty term" {
		t.Errorf("expected reason 'empty term', got %q", result.Reason)
	}
}

func TestGenericTermFilter_ExtractDomainTerm(t *testing.T) {
	filter := NewGenericTermFilter()

	cases := []struct {
		input    string
		expected string
	}{
		{"OrderService", "Order"},
		{"BaseEntity", "Entity"},
		{"Order", "Order"},           // Not filtered, returns as-is
		{"Handler", ""},              // Pure generic, no extraction
		{"MockService", ""},          // Both Mock prefix and Service suffix are generic
	}

	for _, c := range cases {
		result := filter.ExtractDomainTerm(c.input)
		if result != c.expected {
			t.Errorf("ExtractDomainTerm(%q): expected %q, got %q", c.input, c.expected, result)
		}
	}
}

func TestGenericTermFilter_HasGenericSuffix(t *testing.T) {
	filter := NewGenericTermFilter()

	cases := []struct {
		input    string
		hasSuffix bool
		suffix   string
	}{
		{"OrderService", true, "Service"},
		{"UserHandler", true, "Handler"},
		{"Order", false, ""},
		{"Handler", false, ""},  // Exact match, not suffix
	}

	for _, c := range cases {
		hasSuffix, suffix := filter.HasGenericSuffix(c.input)
		if hasSuffix != c.hasSuffix {
			t.Errorf("HasGenericSuffix(%q): expected hasSuffix=%v, got %v", c.input, c.hasSuffix, hasSuffix)
		}
		if suffix != c.suffix {
			t.Errorf("HasGenericSuffix(%q): expected suffix=%q, got %q", c.input, c.suffix, suffix)
		}
	}
}

func TestGenericTermFilter_HasGenericPrefix(t *testing.T) {
	filter := NewGenericTermFilter()

	cases := []struct {
		input     string
		hasPrefix bool
		prefix    string
	}{
		{"BaseEntity", true, "Base"},
		{"AbstractOrder", true, "Abstract"},
		{"Order", false, ""},
		{"Base", false, ""}, // Exact match, not prefix
	}

	for _, c := range cases {
		hasPrefix, prefix := filter.HasGenericPrefix(c.input)
		if hasPrefix != c.hasPrefix {
			t.Errorf("HasGenericPrefix(%q): expected hasPrefix=%v, got %v", c.input, c.hasPrefix, hasPrefix)
		}
		if prefix != c.prefix {
			t.Errorf("HasGenericPrefix(%q): expected prefix=%q, got %q", c.input, c.prefix, prefix)
		}
	}
}

func TestGenericTermFilter_IsPureGenericTerm(t *testing.T) {
	filter := NewGenericTermFilter()

	pureCases := []string{"Handler", "Manager", "Service", "Repository", "Helper", "Mock", "Test"}
	for _, term := range pureCases {
		if !filter.IsPureGenericTerm(term) {
			t.Errorf("expected %q to be pure generic term", term)
		}
	}

	notPureCases := []string{"Order", "Customer", "OrderService", "BaseEntity"}
	for _, term := range notPureCases {
		if filter.IsPureGenericTerm(term) {
			t.Errorf("expected %q to NOT be pure generic term", term)
		}
	}
}
