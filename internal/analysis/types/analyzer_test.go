package types

import (
	"testing"
)

func TestAnalyzeGoFile_ExtractsStructs(t *testing.T) {
	analyzer := NewAnalyzer()

	goCode := `package domain

// DomainDoc represents domain terminology documentation
type DomainDoc struct {
	ID          string
	Title       string
	Description string
	Terms       []DomainTerm
}

// DomainTerm represents a term within a bounded context
type DomainTerm struct {
	Term       string
	Definition string
	Canonical  bool
}
`

	types := analyzer.AnalyzeGoFile("internal/domain/doc.go", goCode)

	if len(types) != 2 {
		t.Errorf("expected 2 types, got %d", len(types))
	}

	// Check first type
	if types[0].Name != "DomainDoc" {
		t.Errorf("expected first type to be DomainDoc, got %s", types[0].Name)
	}
	if types[0].Kind != "struct" {
		t.Errorf("expected kind to be struct, got %s", types[0].Kind)
	}
	if types[0].LineNumber != 4 {
		t.Errorf("expected line number 4, got %d", types[0].LineNumber)
	}
	if len(types[0].Fields) != 4 {
		t.Errorf("expected 4 fields, got %d", len(types[0].Fields))
	}

	// Check second type
	if types[1].Name != "DomainTerm" {
		t.Errorf("expected second type to be DomainTerm, got %s", types[1].Name)
	}
}

func TestAnalyzeGoFile_ExtractsInterfaces(t *testing.T) {
	analyzer := NewAnalyzer()

	goCode := `package domain

type Repository interface {
	Save(doc *DomainDoc) error
	Load(id string) (*DomainDoc, error)
}
`

	types := analyzer.AnalyzeGoFile("internal/domain/repository.go", goCode)

	// Repository should be filtered out as it's a generic term
	if len(types) != 0 {
		t.Errorf("expected 0 types (Repository filtered), got %d", len(types))
	}
}

func TestAnalyzeGoFile_FiltersGenericTerms(t *testing.T) {
	analyzer := NewAnalyzer()

	goCode := `package service

type UserService struct {
	repo Repository
}

type UserHandler struct {
	service *UserService
}

type UserManager struct {
	users map[string]*User
}

type User struct {
	ID   string
	Name string
}
`

	types := analyzer.AnalyzeGoFile("internal/service/user.go", goCode)

	// With the new filtering behavior:
	// - UserService -> extracts "User" (was filtered, domain term extracted)
	// - UserHandler -> extracts "User" (was filtered, domain term extracted)
	// - UserManager -> extracts "User" (was filtered, domain term extracted)
	// - User -> kept as-is (not filtered)
	// Total: 4 types, all named "User"
	if len(types) != 4 {
		t.Errorf("expected 4 types (User extracted from each), got %d", len(types))
		for _, typ := range types {
			t.Logf("found type: %s (wasFiltered=%v, extractedFrom=%s)", typ.Name, typ.WasFiltered, typ.ExtractedFromTerm)
		}
	}

	// All types should be named "User"
	for _, typ := range types {
		if typ.Name != "User" {
			t.Errorf("expected all types to be named User, got %s", typ.Name)
		}
	}

	// Verify extraction metadata
	extractedCount := 0
	for _, typ := range types {
		if typ.WasFiltered && typ.ExtractedFromTerm != "" {
			extractedCount++
		}
	}
	if extractedCount != 3 {
		t.Errorf("expected 3 types to have extraction metadata, got %d", extractedCount)
	}
}

func TestAnalyzeGoFile_ExtractsTypeAliases(t *testing.T) {
	analyzer := NewAnalyzer()

	goCode := `package domain

type DraftConfidence string

const (
	DraftConfidenceHigh   DraftConfidence = "high"
	DraftConfidenceMedium DraftConfidence = "medium"
	DraftConfidenceLow    DraftConfidence = "low"
)
`

	types := analyzer.AnalyzeGoFile("internal/domain/confidence.go", goCode)

	if len(types) != 1 {
		t.Errorf("expected 1 type, got %d", len(types))
	}

	if len(types) > 0 {
		if types[0].Name != "DraftConfidence" {
			t.Errorf("expected DraftConfidence, got %s", types[0].Name)
		}
		if types[0].Kind != "type" {
			t.Errorf("expected kind to be type, got %s", types[0].Kind)
		}
	}
}

func TestAnalyzeTypeScriptFile_ExtractsInterfaces(t *testing.T) {
	analyzer := NewAnalyzer()

	tsCode := `export interface UserAccount {
	id: string;
	email: string;
	profile: UserProfile;
}

interface UserProfile {
	firstName: string;
	lastName: string;
	avatar?: string;
}
`

	types := analyzer.AnalyzeTypeScriptFile("src/types/user.ts", tsCode)

	if len(types) != 2 {
		t.Errorf("expected 2 types, got %d", len(types))
	}

	if len(types) > 0 && types[0].Name != "UserAccount" {
		t.Errorf("expected UserAccount, got %s", types[0].Name)
	}
	if len(types) > 0 && types[0].Kind != "interface" {
		t.Errorf("expected kind to be interface, got %s", types[0].Kind)
	}
}

func TestAnalyzeTypeScriptFile_ExtractsClasses(t *testing.T) {
	analyzer := NewAnalyzer()

	tsCode := `export class Order {
	orderId: string;
	items: OrderItem[];
	total: number;
}

abstract class OrderItem {
	productId: string;
	quantity: number;
}
`

	types := analyzer.AnalyzeTypeScriptFile("src/models/order.ts", tsCode)

	if len(types) != 2 {
		t.Errorf("expected 2 types, got %d", len(types))
	}

	if len(types) > 0 && types[0].Name != "Order" {
		t.Errorf("expected Order, got %s", types[0].Name)
	}
	if len(types) > 1 && types[1].Name != "OrderItem" {
		t.Errorf("expected OrderItem, got %s", types[1].Name)
	}
}

func TestAnalyzeTypeScriptFile_ExtractsTypeAliases(t *testing.T) {
	analyzer := NewAnalyzer()

	tsCode := `export type OrderStatus = 'pending' | 'confirmed' | 'shipped' | 'delivered';

type PaymentMethod = 'credit' | 'debit' | 'paypal';
`

	types := analyzer.AnalyzeTypeScriptFile("src/types/status.ts", tsCode)

	if len(types) != 2 {
		t.Errorf("expected 2 types, got %d", len(types))
	}

	if len(types) > 0 && types[0].Name != "OrderStatus" {
		t.Errorf("expected OrderStatus, got %s", types[0].Name)
	}
}

func TestAnalyzeTypeScriptFile_FiltersGenericTerms(t *testing.T) {
	analyzer := NewAnalyzer()

	tsCode := `export class UserService {
	private repository: UserRepository;
}

export interface UserRepository {
	findById(id: string): Promise<User>;
}

export class UserController {
	constructor(private service: UserService) {}
}

export interface User {
	id: string;
	name: string;
}
`

	types := analyzer.AnalyzeTypeScriptFile("src/services/user.ts", tsCode)

	// With the new filtering behavior:
	// - UserService -> extracts "User" (was filtered, domain term extracted)
	// - UserRepository -> extracts "User" (was filtered, domain term extracted)
	// - UserController -> extracts "User" (was filtered, domain term extracted)
	// - User -> kept as-is (not filtered)
	// Total: 4 types, all named "User"
	if len(types) != 4 {
		t.Errorf("expected 4 types (User extracted from each), got %d", len(types))
		for _, typ := range types {
			t.Logf("found type: %s (wasFiltered=%v, extractedFrom=%s)", typ.Name, typ.WasFiltered, typ.ExtractedFromTerm)
		}
	}

	// All types should be named "User"
	for _, typ := range types {
		if typ.Name != "User" {
			t.Errorf("expected all types to be named User, got %s", typ.Name)
		}
	}

	// Verify extraction metadata
	extractedCount := 0
	for _, typ := range types {
		if typ.WasFiltered && typ.ExtractedFromTerm != "" {
			extractedCount++
		}
	}
	if extractedCount != 3 {
		t.Errorf("expected 3 types to have extraction metadata, got %d", extractedCount)
	}
}

func TestAggregateTerms_CalculatesConfidence(t *testing.T) {
	analyzer := NewAnalyzer()

	// Simulate types from multiple files
	types := []*DiscoveredType{
		{Name: "Order", Kind: "struct", FilePath: "internal/order/order.go", LineNumber: 10},
		{Name: "Order", Kind: "interface", FilePath: "internal/api/order.go", LineNumber: 5},
		{Name: "LineItem", Kind: "struct", FilePath: "internal/order/order.go", LineNumber: 20},
		{Name: "Customer", Kind: "struct", FilePath: "internal/customer/customer.go", LineNumber: 8},
	}

	termMap := analyzer.AggregateTerms(types)

	// Order appears in 2 files - should be high confidence
	orderTerm := termMap["Order"]
	if orderTerm == nil {
		t.Fatal("expected Order term to exist")
	}
	if orderTerm.Confidence != TermConfidenceHigh {
		t.Errorf("expected Order to have high confidence, got %s", orderTerm.Confidence)
	}
	if len(orderTerm.Files) != 2 {
		t.Errorf("expected Order to appear in 2 files, got %d", len(orderTerm.Files))
	}

	// LineItem appears in 1 file as a type - should be medium confidence
	lineItemTerm := termMap["LineItem"]
	if lineItemTerm == nil {
		t.Fatal("expected LineItem term to exist")
	}
	if lineItemTerm.Confidence != TermConfidenceMedium {
		t.Errorf("expected LineItem to have medium confidence, got %s", lineItemTerm.Confidence)
	}
}

func TestGetHighConfidenceTerms_SortsCorrectly(t *testing.T) {
	analyzer := NewAnalyzer()

	types := []*DiscoveredType{
		{Name: "Order", Kind: "struct", FilePath: "file1.go", LineNumber: 1},
		{Name: "Order", Kind: "struct", FilePath: "file2.go", LineNumber: 1},
		{Name: "Customer", Kind: "struct", FilePath: "file1.go", LineNumber: 10},
		{Name: "Product", Kind: "struct", FilePath: "file3.go", LineNumber: 5},
	}

	termMap := analyzer.AggregateTerms(types)
	sorted := analyzer.GetHighConfidenceTerms(termMap)

	if len(sorted) == 0 {
		t.Fatal("expected sorted terms")
	}

	// Order should be first (high confidence, appears in 2 files)
	if sorted[0].Term != "Order" {
		t.Errorf("expected Order first, got %s", sorted[0].Term)
	}
	if sorted[0].Confidence != TermConfidenceHigh {
		t.Errorf("expected high confidence first, got %s", sorted[0].Confidence)
	}
}

// TODO: Implement this test to verify field extraction tracks line numbers correctly
func TestAnalyzeGoFile_ExtractsFieldLineNumbers(t *testing.T) {
	// This test verifies that field line numbers are correctly captured
	// for use in evidence tracking.
	//
	// Implement this test with a Go struct that has multiple fields,
	// and verify each field's line number is captured correctly.
	t.Skip("Implement this test")
}
