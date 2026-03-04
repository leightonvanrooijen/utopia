package types

import (
	"testing"
)

func TestInferBoundedContext_InternalPackages(t *testing.T) {
	analyzer := NewBoundedContextAnalyzer()

	tests := []struct {
		filePath string
		expected string
	}{
		// Standard internal package structure
		{"internal/order/order.go", "order"},
		{"internal/order/line_item.go", "order"},
		{"internal/customer/customer.go", "customer"},
		{"internal/customer/address/address.go", "customer"},

		// Nested domain packages should use top-level as context
		{"internal/billing/invoice/invoice.go", "billing"},
		{"internal/billing/payment/payment.go", "billing"},

		// Infrastructure layers should be skipped
		{"internal/domain/order/order.go", "order"},
		{"internal/domain/customer/customer.go", "customer"},

		// CamelCase and PascalCase package names
		{"internal/userAccount/user.go", "user-account"},
		{"internal/OrderManagement/order.go", "order-management"},
	}

	for _, tt := range tests {
		t.Run(tt.filePath, func(t *testing.T) {
			result := analyzer.InferBoundedContext(tt.filePath)
			if result != tt.expected {
				t.Errorf("InferBoundedContext(%q) = %q, want %q", tt.filePath, result, tt.expected)
			}
		})
	}
}

func TestInferBoundedContext_SrcPackages(t *testing.T) {
	analyzer := NewBoundedContextAnalyzer()

	tests := []struct {
		filePath string
		expected string
	}{
		{"src/order/order.ts", "order"},
		{"src/domain/billing/invoice.ts", "billing"},
		{"src/models/user/user.ts", "user"},
		// components is the first package after src, so it becomes the context
		{"src/components/dashboard/Dashboard.tsx", "components"},
	}

	for _, tt := range tests {
		t.Run(tt.filePath, func(t *testing.T) {
			result := analyzer.InferBoundedContext(tt.filePath)
			if result != tt.expected {
				t.Errorf("InferBoundedContext(%q) = %q, want %q", tt.filePath, result, tt.expected)
			}
		})
	}
}

func TestInferBoundedContext_PkgPackages(t *testing.T) {
	analyzer := NewBoundedContextAnalyzer()

	tests := []struct {
		filePath string
		expected string
	}{
		{"pkg/order/order.go", "order"},
		{"pkg/api/handlers.go", "api"},
		{"pkg/billing/invoice/invoice.go", "billing"},
	}

	for _, tt := range tests {
		t.Run(tt.filePath, func(t *testing.T) {
			result := analyzer.InferBoundedContext(tt.filePath)
			if result != tt.expected {
				t.Errorf("InferBoundedContext(%q) = %q, want %q", tt.filePath, result, tt.expected)
			}
		})
	}
}

func TestInferBoundedContext_NoMatchingDomainPath(t *testing.T) {
	analyzer := NewBoundedContextAnalyzer()

	tests := []struct {
		filePath string
		expected string
	}{
		// Files without recognized domain paths use first directory
		{"order/order.go", "order"},
		{"billing/invoice.go", "billing"},
		// Single file with no directory
		{"main.go", "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.filePath, func(t *testing.T) {
			result := analyzer.InferBoundedContext(tt.filePath)
			if result != tt.expected {
				t.Errorf("InferBoundedContext(%q) = %q, want %q", tt.filePath, result, tt.expected)
			}
		})
	}
}

func TestNormalizeContextName(t *testing.T) {
	analyzer := NewBoundedContextAnalyzer()

	tests := []struct {
		input    string
		expected string
	}{
		{"order", "order"},
		{"userAccount", "user-account"},
		{"UserAccount", "user-account"},
		{"user_account", "user-account"},
		{"OrderManagement", "order-management"},
		{"HTTPClient", "h-t-t-p-client"}, // Edge case - acronyms
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := analyzer.normalizeContextName(tt.input)
			if result != tt.expected {
				t.Errorf("normalizeContextName(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestContextTitle(t *testing.T) {
	analyzer := NewBoundedContextAnalyzer()

	tests := []struct {
		input    string
		expected string
	}{
		{"order", "Order"},
		{"user-account", "User Account"},
		{"order-management", "Order Management"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := analyzer.contextTitle(tt.input)
			if result != tt.expected {
				t.Errorf("contextTitle(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestDiscoverBoundedContexts(t *testing.T) {
	analyzer := NewBoundedContextAnalyzer()

	filePaths := []string{
		"internal/order/order.go",
		"internal/order/line_item.go",
		"internal/customer/customer.go",
		"internal/customer/address.go",
		"internal/billing/invoice.go",
	}

	contexts := analyzer.DiscoverBoundedContexts(filePaths)

	if len(contexts) != 3 {
		t.Errorf("expected 3 bounded contexts, got %d", len(contexts))
	}

	// Verify contexts are sorted by name
	expectedNames := []string{"billing", "customer", "order"}
	for i, ctx := range contexts {
		if ctx.Name != expectedNames[i] {
			t.Errorf("context[%d].Name = %q, want %q", i, ctx.Name, expectedNames[i])
		}
	}

	// Verify order context has 2 files
	for _, ctx := range contexts {
		if ctx.Name == "order" {
			if len(ctx.Files) != 2 {
				t.Errorf("order context should have 2 files, got %d", len(ctx.Files))
			}
		}
	}
}

func TestGroupTermsByContext(t *testing.T) {
	analyzer := NewBoundedContextAnalyzer()

	types := []*DiscoveredType{
		{Name: "Order", Kind: "struct", FilePath: "internal/order/order.go", LineNumber: 10},
		{Name: "LineItem", Kind: "struct", FilePath: "internal/order/order.go", LineNumber: 20},
		{Name: "Customer", Kind: "struct", FilePath: "internal/customer/customer.go", LineNumber: 5},
		{Name: "Invoice", Kind: "struct", FilePath: "internal/billing/invoice.go", LineNumber: 8},
	}

	contextTerms := analyzer.GroupTermsByContext(types)

	// Should have 3 contexts
	if len(contextTerms) != 3 {
		t.Errorf("expected 3 contexts, got %d", len(contextTerms))
	}

	// Verify order context terms
	orderTerms := contextTerms["order"]
	if len(orderTerms) != 2 {
		t.Errorf("expected 2 terms in order context, got %d", len(orderTerms))
	}

	// Verify each term has correct bounded context
	for _, term := range orderTerms {
		if term.BoundedContext != "order" {
			t.Errorf("expected term in order context to have BoundedContext='order', got %q", term.BoundedContext)
		}
	}

	// Verify customer context
	customerTerms := contextTerms["customer"]
	if len(customerTerms) != 1 || customerTerms[0].Term != "Customer" {
		t.Error("expected Customer term in customer context")
	}
}

func TestGroupTermsByContext_SameTermDifferentContexts(t *testing.T) {
	analyzer := NewBoundedContextAnalyzer()

	// Same term "Status" appears in different contexts
	types := []*DiscoveredType{
		{Name: "Status", Kind: "type", FilePath: "internal/order/status.go", LineNumber: 10},
		{Name: "Status", Kind: "type", FilePath: "internal/billing/status.go", LineNumber: 5},
	}

	contextTerms := analyzer.GroupTermsByContext(types)

	// Should have 2 contexts
	if len(contextTerms) != 2 {
		t.Errorf("expected 2 contexts, got %d", len(contextTerms))
	}

	// Both contexts should have their own Status term
	orderTerms := contextTerms["order"]
	billingTerms := contextTerms["billing"]

	if len(orderTerms) != 1 || orderTerms[0].Term != "Status" {
		t.Error("expected Status term in order context")
	}
	if len(billingTerms) != 1 || billingTerms[0].Term != "Status" {
		t.Error("expected Status term in billing context")
	}

	// Verify they are separate entries (different contexts)
	if orderTerms[0].BoundedContext != "order" {
		t.Errorf("order Status should have BoundedContext='order', got %q", orderTerms[0].BoundedContext)
	}
	if billingTerms[0].BoundedContext != "billing" {
		t.Errorf("billing Status should have BoundedContext='billing', got %q", billingTerms[0].BoundedContext)
	}
}

func TestFindCrossContextTerms(t *testing.T) {
	analyzer := NewBoundedContextAnalyzer()

	types := []*DiscoveredType{
		// Status appears in both order and billing contexts
		{Name: "Status", Kind: "type", FilePath: "internal/order/status.go", LineNumber: 10},
		{Name: "Status", Kind: "type", FilePath: "internal/billing/status.go", LineNumber: 5},
		// Order only appears in order context
		{Name: "Order", Kind: "struct", FilePath: "internal/order/order.go", LineNumber: 15},
		// Customer only appears in customer context
		{Name: "Customer", Kind: "struct", FilePath: "internal/customer/customer.go", LineNumber: 8},
	}

	contextTerms := analyzer.GroupTermsByContext(types)
	crossContextTerms := analyzer.FindCrossContextTerms(contextTerms)

	// Only Status should be cross-context
	if len(crossContextTerms) != 1 {
		t.Errorf("expected 1 cross-context term, got %d", len(crossContextTerms))
	}

	statusContexts, ok := crossContextTerms["Status"]
	if !ok {
		t.Fatal("expected Status to be a cross-context term")
	}

	if len(statusContexts) != 2 {
		t.Errorf("expected Status to appear in 2 contexts, got %d", len(statusContexts))
	}

	// Should be sorted alphabetically
	if statusContexts[0] != "billing" || statusContexts[1] != "order" {
		t.Errorf("expected contexts [billing, order], got %v", statusContexts)
	}
}

func TestGroupTermsByContext_ConfidenceCalculation(t *testing.T) {
	analyzer := NewBoundedContextAnalyzer()

	types := []*DiscoveredType{
		// Order appears in multiple files within same context - high confidence
		{Name: "Order", Kind: "struct", FilePath: "internal/order/order.go", LineNumber: 10},
		{Name: "Order", Kind: "interface", FilePath: "internal/order/repository.go", LineNumber: 5},
		// LineItem appears only once - medium confidence (it's a type)
		{Name: "LineItem", Kind: "struct", FilePath: "internal/order/order.go", LineNumber: 20},
	}

	contextTerms := analyzer.GroupTermsByContext(types)
	orderTerms := contextTerms["order"]

	// Find Order and LineItem terms
	var orderTerm, lineItemTerm *ContextualTerm
	for _, t := range orderTerms {
		if t.Term == "Order" {
			orderTerm = t
		}
		if t.Term == "LineItem" {
			lineItemTerm = t
		}
	}

	if orderTerm == nil {
		t.Fatal("expected Order term")
	}
	if orderTerm.Confidence != TermConfidenceHigh {
		t.Errorf("expected Order to have high confidence, got %s", orderTerm.Confidence)
	}
	if len(orderTerm.Files) != 2 {
		t.Errorf("expected Order to appear in 2 files, got %d", len(orderTerm.Files))
	}

	if lineItemTerm == nil {
		t.Fatal("expected LineItem term")
	}
	if lineItemTerm.Confidence != TermConfidenceMedium {
		t.Errorf("expected LineItem to have medium confidence, got %s", lineItemTerm.Confidence)
	}
}
