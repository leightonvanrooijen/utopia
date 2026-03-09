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
		{"MockRepository", true, ""},  // Repository is also generic
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
		{"IRepository", true, "I"},        // Repository suffix found, extracts "I" (but single char)
		{"IOrderService", true, "IOrder"}, // Service suffix found, extracts "IOrder"
		{"IOrder", true, "Order"},         // "I" prefix with "Order" suffix (>= 3 chars)
		{"ID", false, ""},                 // "D" has only 1 char < 3, not I-prefix
		{"IP", false, ""},                 // "P" has only 1 char < 3
		{"Ice", false, ""},                // Lowercase after I, not a prefix pattern
		{"Item", false, ""},               // Item is a domain term, no I-prefix pattern
		{"IAbcde", true, "Abcde"},         // "I" prefix, "Abcde" has 5 chars >= 3
		{"IAbc", true, "Abc"},             // "I" prefix, "Abc" has 3 chars == 3
		{"IAb", false, ""},                // "Ab" has 2 chars < 3, not I-prefix
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

