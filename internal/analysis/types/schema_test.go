package types

import (
	"testing"
)

func TestAnalyzeSQLFile_ExtractsTableNames(t *testing.T) {
	analyzer := NewSchemaAnalyzer()

	sqlContent := `
CREATE TABLE users (
    id INTEGER PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    email VARCHAR(255)
);

CREATE TABLE orders (
    id INTEGER PRIMARY KEY,
    user_id INTEGER,
    total DECIMAL(10, 2)
);
`

	entities := analyzer.AnalyzeSQLFile("migrations/001_create_tables.sql", sqlContent)

	if len(entities) != 2 {
		t.Fatalf("expected 2 entities, got %d", len(entities))
	}

	// Check table names are converted to PascalCase
	names := make(map[string]bool)
	for _, e := range entities {
		names[e.Name] = true
	}

	if !names["Users"] {
		t.Error("expected to find 'Users' entity")
	}
	if !names["Orders"] {
		t.Error("expected to find 'Orders' entity")
	}
}

func TestAnalyzeSQLFile_ExtractsColumns(t *testing.T) {
	analyzer := NewSchemaAnalyzer()

	sqlContent := `
CREATE TABLE orders (
    id INTEGER PRIMARY KEY,
    customer_name VARCHAR(255) NOT NULL,
    order_date DATE,
    total_amount DECIMAL(10, 2)
);
`

	entities := analyzer.AnalyzeSQLFile("migrations/001_orders.sql", sqlContent)

	if len(entities) != 1 {
		t.Fatalf("expected 1 entity, got %d", len(entities))
	}

	order := entities[0]
	if len(order.Columns) < 3 {
		t.Fatalf("expected at least 3 columns, got %d", len(order.Columns))
	}

	// Check column name conversion
	colNames := make(map[string]bool)
	for _, col := range order.Columns {
		colNames[col.Name] = true
	}

	if !colNames["CustomerName"] {
		t.Error("expected 'CustomerName' column (converted from customer_name)")
	}
	if !colNames["OrderDate"] {
		t.Error("expected 'OrderDate' column (converted from order_date)")
	}
}

func TestAnalyzeSQLFile_ExtractsPrimaryKey(t *testing.T) {
	analyzer := NewSchemaAnalyzer()

	sqlContent := `
CREATE TABLE users (
    id INTEGER PRIMARY KEY,
    name VARCHAR(255)
);
`

	entities := analyzer.AnalyzeSQLFile("schema.sql", sqlContent)

	if len(entities) != 1 {
		t.Fatalf("expected 1 entity, got %d", len(entities))
	}

	var idColumn *SchemaColumn
	for i := range entities[0].Columns {
		if entities[0].Columns[i].RawName == "id" {
			idColumn = &entities[0].Columns[i]
			break
		}
	}

	if idColumn == nil {
		t.Fatal("expected to find id column")
	}

	if !idColumn.IsPrimary {
		t.Error("expected id column to be marked as primary key")
	}
}

func TestAnalyzeSQLFile_DetectsForeignKeysInline(t *testing.T) {
	analyzer := NewSchemaAnalyzer()

	sqlContent := `
CREATE TABLE orders (
    id INTEGER PRIMARY KEY,
    customer_id INTEGER REFERENCES customers(id),
    status VARCHAR(50)
);

CREATE TABLE customers (
    id INTEGER PRIMARY KEY,
    name VARCHAR(255)
);
`

	entities := analyzer.AnalyzeSQLFile("migrations/001_orders.sql", sqlContent)

	// Find orders entity
	var orders *SchemaEntity
	for _, e := range entities {
		if e.Name == "Orders" {
			orders = e
			break
		}
	}

	if orders == nil {
		t.Fatal("expected to find Orders entity")
	}

	if len(orders.ForeignKeys) != 1 {
		t.Fatalf("expected 1 foreign key, got %d", len(orders.ForeignKeys))
	}

	fk := orders.ForeignKeys[0]
	if fk.ColumnName != "CustomerId" {
		t.Errorf("expected foreign key column 'CustomerId', got '%s'", fk.ColumnName)
	}
	if fk.TargetTable != "Customers" {
		t.Errorf("expected target table 'Customers', got '%s'", fk.TargetTable)
	}
}

func TestAnalyzeSQLFile_DetectsForeignKeysTableLevel(t *testing.T) {
	analyzer := NewSchemaAnalyzer()

	sqlContent := `
CREATE TABLE order_items (
    id INTEGER PRIMARY KEY,
    order_id INTEGER,
    product_id INTEGER,
    quantity INTEGER,
    FOREIGN KEY (order_id) REFERENCES orders(id),
    FOREIGN KEY (product_id) REFERENCES products(id)
);

CREATE TABLE orders (
    id INTEGER PRIMARY KEY
);

CREATE TABLE products (
    id INTEGER PRIMARY KEY
);
`

	entities := analyzer.AnalyzeSQLFile("migrations/001_items.sql", sqlContent)

	// Find order_items entity
	var orderItems *SchemaEntity
	for _, e := range entities {
		if e.Name == "OrderItems" {
			orderItems = e
			break
		}
	}

	if orderItems == nil {
		t.Fatal("expected to find OrderItems entity")
	}

	if len(orderItems.ForeignKeys) != 2 {
		t.Fatalf("expected 2 foreign keys, got %d", len(orderItems.ForeignKeys))
	}

	// Check both foreign keys exist
	fkTargets := make(map[string]bool)
	for _, fk := range orderItems.ForeignKeys {
		fkTargets[fk.TargetTable] = true
	}

	if !fkTargets["Orders"] {
		t.Error("expected foreign key to Orders")
	}
	if !fkTargets["Products"] {
		t.Error("expected foreign key to Products")
	}
}

func TestAnalyzeSQLFile_DetectsAlterTableForeignKey(t *testing.T) {
	analyzer := NewSchemaAnalyzer()

	sqlContent := `
CREATE TABLE orders (
    id INTEGER PRIMARY KEY,
    customer_id INTEGER
);

CREATE TABLE customers (
    id INTEGER PRIMARY KEY,
    name VARCHAR(255)
);

ALTER TABLE orders ADD FOREIGN KEY (customer_id) REFERENCES customers(id);
`

	entities := analyzer.AnalyzeSQLFile("migrations/001_orders.sql", sqlContent)

	// Find orders entity
	var orders *SchemaEntity
	for _, e := range entities {
		if e.Name == "Orders" {
			orders = e
			break
		}
	}

	if orders == nil {
		t.Fatal("expected to find Orders entity")
	}

	if len(orders.ForeignKeys) != 1 {
		t.Fatalf("expected 1 foreign key, got %d", len(orders.ForeignKeys))
	}

	if orders.ForeignKeys[0].TargetTable != "Customers" {
		t.Errorf("expected foreign key to Customers, got %s", orders.ForeignKeys[0].TargetTable)
	}
}

func TestAnalyzeSQLFile_HandlesIfNotExists(t *testing.T) {
	analyzer := NewSchemaAnalyzer()

	sqlContent := `
CREATE TABLE IF NOT EXISTS users (
    id INTEGER PRIMARY KEY,
    name VARCHAR(255)
);
`

	entities := analyzer.AnalyzeSQLFile("schema.sql", sqlContent)

	if len(entities) != 1 {
		t.Fatalf("expected 1 entity, got %d", len(entities))
	}

	if entities[0].Name != "Users" {
		t.Errorf("expected entity name 'Users', got '%s'", entities[0].Name)
	}
}

func TestAnalyzeSQLFile_HandlesQuotedIdentifiers(t *testing.T) {
	analyzer := NewSchemaAnalyzer()

	sqlContent := `
CREATE TABLE "user_accounts" (
    "id" INTEGER PRIMARY KEY,
    "full_name" VARCHAR(255)
);

CREATE TABLE ` + "`order_items`" + ` (
    ` + "`id`" + ` INTEGER PRIMARY KEY,
    ` + "`item_name`" + ` VARCHAR(255)
);
`

	entities := analyzer.AnalyzeSQLFile("schema.sql", sqlContent)

	if len(entities) != 2 {
		t.Fatalf("expected 2 entities, got %d", len(entities))
	}

	names := make(map[string]bool)
	for _, e := range entities {
		names[e.Name] = true
	}

	if !names["UserAccounts"] {
		t.Error("expected to find 'UserAccounts' entity")
	}
	if !names["OrderItems"] {
		t.Error("expected to find 'OrderItems' entity")
	}
}

func TestAnalyzePrismaFile_ExtractsModels(t *testing.T) {
	analyzer := NewSchemaAnalyzer()

	prismaContent := `
model User {
  id    Int    @id @default(autoincrement())
  email String @unique
  name  String?
  posts Post[]
}

model Post {
  id       Int    @id @default(autoincrement())
  title    String
  content  String?
  author   User   @relation(fields: [authorId], references: [id])
  authorId Int
}
`

	entities := analyzer.AnalyzePrismaFile("prisma/schema.prisma", prismaContent)

	if len(entities) != 2 {
		t.Fatalf("expected 2 entities, got %d", len(entities))
	}

	names := make(map[string]bool)
	for _, e := range entities {
		names[e.Name] = true
	}

	if !names["User"] {
		t.Error("expected to find 'User' entity")
	}
	if !names["Post"] {
		t.Error("expected to find 'Post' entity")
	}
}

func TestAnalyzePrismaFile_ExtractsFields(t *testing.T) {
	analyzer := NewSchemaAnalyzer()

	prismaContent := `
model Order {
  id          Int      @id @default(autoincrement())
  orderNumber String   @unique
  totalAmount Decimal
  status      String
  createdAt   DateTime @default(now())
}
`

	entities := analyzer.AnalyzePrismaFile("schema.prisma", prismaContent)

	if len(entities) != 1 {
		t.Fatalf("expected 1 entity, got %d", len(entities))
	}

	order := entities[0]
	fieldNames := make(map[string]bool)
	for _, col := range order.Columns {
		fieldNames[col.Name] = true
	}

	if !fieldNames["OrderNumber"] {
		t.Error("expected 'OrderNumber' field")
	}
	if !fieldNames["TotalAmount"] {
		t.Error("expected 'TotalAmount' field")
	}
	if !fieldNames["Status"] {
		t.Error("expected 'Status' field")
	}
}

func TestAnalyzePrismaFile_DetectsExplicitRelations(t *testing.T) {
	analyzer := NewSchemaAnalyzer()

	prismaContent := `
model Post {
  id       Int    @id
  title    String
  author   User   @relation(fields: [authorId], references: [id])
  authorId Int
}

model User {
  id    Int    @id
  name  String
  posts Post[]
}
`

	entities := analyzer.AnalyzePrismaFile("schema.prisma", prismaContent)

	// Find Post entity
	var post *SchemaEntity
	for _, e := range entities {
		if e.Name == "Post" {
			post = e
			break
		}
	}

	if post == nil {
		t.Fatal("expected to find Post entity")
	}

	if len(post.ForeignKeys) != 1 {
		t.Fatalf("expected 1 foreign key, got %d", len(post.ForeignKeys))
	}

	fk := post.ForeignKeys[0]
	if fk.TargetTable != "User" {
		t.Errorf("expected foreign key to User, got %s", fk.TargetTable)
	}
	if fk.ColumnName != "AuthorId" {
		t.Errorf("expected column AuthorId, got %s", fk.ColumnName)
	}
}

func TestAnalyzePrismaFile_DetectsImplicitRelations(t *testing.T) {
	analyzer := NewSchemaAnalyzer()

	prismaContent := `
model Comment {
  id       Int    @id
  content  String
  post     Post
  postId   Int
}

model Post {
  id       Int       @id
  title    String
  comments Comment[]
}
`

	entities := analyzer.AnalyzePrismaFile("schema.prisma", prismaContent)

	// Find Comment entity
	var comment *SchemaEntity
	for _, e := range entities {
		if e.Name == "Comment" {
			comment = e
			break
		}
	}

	if comment == nil {
		t.Fatal("expected to find Comment entity")
	}

	// Should detect implicit relation from Post field
	if len(comment.ForeignKeys) < 1 {
		t.Fatal("expected at least 1 foreign key for implicit relation")
	}

	var foundPostFK bool
	for _, fk := range comment.ForeignKeys {
		if fk.TargetTable == "Post" {
			foundPostFK = true
			break
		}
	}

	if !foundPostFK {
		t.Error("expected foreign key to Post")
	}
}

func TestToPascalCase(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"user", "User"},
		{"user_account", "UserAccount"},
		{"order_line_item", "OrderLineItem"},
		{"USER", "USER"},           // All caps preserved for camelCase path
		{"user-account", "UserAccount"},
		{"", ""},
		{"orderNumber", "OrderNumber"},   // camelCase preserved
		{"totalAmount", "TotalAmount"},   // camelCase preserved
		{"authorId", "AuthorId"},         // camelCase preserved
	}

	for _, test := range tests {
		result := toPascalCase(test.input)
		if result != test.expected {
			t.Errorf("toPascalCase(%q) = %q, expected %q", test.input, result, test.expected)
		}
	}
}

func TestToDiscoveredTypes_ConvertsSchemaEntities(t *testing.T) {
	analyzer := NewSchemaAnalyzer()

	entities := []*SchemaEntity{
		{
			Name:       "Orders",
			RawName:    "orders",
			Source:     SchemaSourceSQL,
			FilePath:   "migrations/001.sql",
			LineNumber: 10,
			Columns: []SchemaColumn{
				{Name: "Id", RawName: "id", Type: "INTEGER", LineNumber: 11, IsPrimary: true},
				{Name: "CustomerName", RawName: "customer_name", Type: "VARCHAR(255)", LineNumber: 12},
			},
		},
	}

	types := analyzer.ToDiscoveredTypes(entities)

	if len(types) != 1 {
		t.Fatalf("expected 1 type, got %d", len(types))
	}

	orderType := types[0]
	if orderType.Name != "Orders" {
		t.Errorf("expected name 'Orders', got '%s'", orderType.Name)
	}
	if orderType.Kind != "schema" {
		t.Errorf("expected kind 'schema', got '%s'", orderType.Kind)
	}
	if orderType.Language != "sql" {
		t.Errorf("expected language 'sql', got '%s'", orderType.Language)
	}
	if len(orderType.Fields) != 2 {
		t.Errorf("expected 2 fields, got %d", len(orderType.Fields))
	}
}

func TestToDiscoveredRelationships_ConvertsForeignKeys(t *testing.T) {
	analyzer := NewSchemaAnalyzer()

	entities := []*SchemaEntity{
		{
			Name:     "Orders",
			FilePath: "migrations/001.sql",
			ForeignKeys: []SchemaForeignKey{
				{ColumnName: "CustomerId", TargetTable: "Customers", LineNumber: 15},
			},
		},
		{
			Name:     "Customers",
			FilePath: "migrations/001.sql",
		},
	}

	relationships := analyzer.ToDiscoveredRelationships(entities)

	if len(relationships) != 1 {
		t.Fatalf("expected 1 relationship, got %d", len(relationships))
	}

	rel := relationships[0]
	if rel.SourceType != "Orders" {
		t.Errorf("expected source 'Orders', got '%s'", rel.SourceType)
	}
	if rel.TargetType != "Customers" {
		t.Errorf("expected target 'Customers', got '%s'", rel.TargetType)
	}
	if rel.Type != RelationshipReferences {
		t.Errorf("expected relationship type 'references', got '%s'", rel.Type)
	}
}

func TestToDiscoveredRelationships_FiltersUnknownEntities(t *testing.T) {
	analyzer := NewSchemaAnalyzer()

	entities := []*SchemaEntity{
		{
			Name:     "Orders",
			FilePath: "migrations/001.sql",
			ForeignKeys: []SchemaForeignKey{
				{ColumnName: "CustomerId", TargetTable: "Customers", LineNumber: 15},
				{ColumnName: "RegionId", TargetTable: "Regions", LineNumber: 16}, // Regions not in entities
			},
		},
		{
			Name:     "Customers",
			FilePath: "migrations/001.sql",
		},
	}

	relationships := analyzer.ToDiscoveredRelationships(entities)

	// Should only include relationship to Customers, not Regions
	if len(relationships) != 1 {
		t.Fatalf("expected 1 relationship (filtered unknown), got %d", len(relationships))
	}

	if relationships[0].TargetType != "Customers" {
		t.Error("expected only relationship to known entity Customers")
	}
}

func TestMergeSchemaAndTypeTerms_BoostsConfidenceForDualSource(t *testing.T) {
	// Simulate type-discovered terms
	typeTerms := map[string]*TermOccurrence{
		"Order": {
			Term:       "Order",
			Files:      []string{"internal/order/order.go"},
			Lines:      []string{"internal/order/order.go:10"},
			Confidence: TermConfidenceMedium, // Single file
			Types:      []*DiscoveredType{{Name: "Order"}},
		},
		"Customer": {
			Term:       "Customer",
			Files:      []string{"internal/customer/customer.go"},
			Lines:      []string{"internal/customer/customer.go:5"},
			Confidence: TermConfidenceMedium,
			Types:      []*DiscoveredType{{Name: "Customer"}},
		},
	}

	schemaEntities := []*SchemaEntity{
		{
			Name:       "Order",
			FilePath:   "migrations/001.sql",
			LineNumber: 20,
		},
		{
			Name:       "Product", // New entity only in schema
			FilePath:   "migrations/001.sql",
			LineNumber: 30,
		},
	}

	merged := MergeSchemaAndTypeTerms(typeTerms, schemaEntities)

	// Order should be boosted to high confidence (found in both)
	if merged["Order"].Confidence != TermConfidenceHigh {
		t.Errorf("expected Order confidence HIGH (dual source), got %s", merged["Order"].Confidence)
	}

	// Order should have both files in evidence
	if len(merged["Order"].Files) != 2 {
		t.Errorf("expected Order to have 2 files, got %d", len(merged["Order"].Files))
	}

	// Customer should remain medium (only in code)
	if merged["Customer"].Confidence != TermConfidenceMedium {
		t.Errorf("expected Customer confidence MEDIUM (single source), got %s", merged["Customer"].Confidence)
	}

	// Product should be added at medium confidence (schema only)
	if merged["Product"] == nil {
		t.Fatal("expected Product term from schema")
	}
	if merged["Product"].Confidence != TermConfidenceMedium {
		t.Errorf("expected Product confidence MEDIUM (schema only), got %s", merged["Product"].Confidence)
	}
}

func TestMergeSchemaAndTypeTerms_AddsColumnsAsLowConfidence(t *testing.T) {
	typeTerms := map[string]*TermOccurrence{}

	schemaEntities := []*SchemaEntity{
		{
			Name:       "Order",
			FilePath:   "migrations/001.sql",
			LineNumber: 10,
			Columns: []SchemaColumn{
				{Name: "CustomerName", LineNumber: 12},
				{Name: "TotalAmount", LineNumber: 13},
			},
		},
	}

	merged := MergeSchemaAndTypeTerms(typeTerms, schemaEntities)

	// Columns should be added at low confidence
	if merged["CustomerName"] == nil {
		t.Fatal("expected CustomerName term from column")
	}
	if merged["CustomerName"].Confidence != TermConfidenceLow {
		t.Errorf("expected column confidence LOW, got %s", merged["CustomerName"].Confidence)
	}
}

func TestMergeSchemaAndTypeTerms_BoostsColumnConfidenceWhenInCode(t *testing.T) {
	typeTerms := map[string]*TermOccurrence{
		"CustomerName": {
			Term:       "CustomerName",
			Files:      []string{"internal/order/order.go"},
			Lines:      []string{"internal/order/order.go:15"},
			Confidence: TermConfidenceLow, // Field in one file
			Types:      nil,
		},
	}

	schemaEntities := []*SchemaEntity{
		{
			Name:       "Order",
			FilePath:   "migrations/001.sql",
			LineNumber: 10,
			Columns: []SchemaColumn{
				{Name: "CustomerName", LineNumber: 12},
			},
		},
	}

	merged := MergeSchemaAndTypeTerms(typeTerms, schemaEntities)

	// CustomerName should be boosted from LOW to MEDIUM (in both code and schema)
	if merged["CustomerName"].Confidence != TermConfidenceMedium {
		t.Errorf("expected CustomerName confidence MEDIUM (boosted), got %s", merged["CustomerName"].Confidence)
	}
}

func TestIsSchemaFile(t *testing.T) {
	tests := []struct {
		path     string
		expected bool
	}{
		{"schema.sql", true},
		{"db/schema.sql", true},
		{"schema.prisma", true},
		{"prisma/schema.prisma", true},
		{"migrations/001_create_users.sql", true},
		{"db/migrations/20230101_init.sql", true},
		{"internal/order/order.go", false},
		{"src/types/order.ts", false},
		{"README.md", false},
	}

	for _, test := range tests {
		result := IsSchemaFile(test.path)
		if result != test.expected {
			t.Errorf("IsSchemaFile(%q) = %v, expected %v", test.path, result, test.expected)
		}
	}
}

func TestAnalyzeSQLFile_HandlesMultipleColumnTypes(t *testing.T) {
	analyzer := NewSchemaAnalyzer()

	sqlContent := `
CREATE TABLE products (
    id SERIAL PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    price DECIMAL(10, 2),
    quantity INTEGER DEFAULT 0,
    description TEXT,
    is_active BOOLEAN DEFAULT true,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    uuid UUID
);
`

	entities := analyzer.AnalyzeSQLFile("schema.sql", sqlContent)

	if len(entities) != 1 {
		t.Fatalf("expected 1 entity, got %d", len(entities))
	}

	colTypes := make(map[string]string)
	for _, col := range entities[0].Columns {
		colTypes[col.RawName] = col.Type
	}

	// Note: the regex captures the type in the case it appears in the SQL
	// Case is normalized to uppercase in the regex
	expectedTypes := map[string]string{
		"id":          "SERIAL",
		"name":        "VARCHAR(255)",
		"price":       "DECIMAL(10, 2)",
		"quantity":    "INTEGER",
		"description": "TEXT",
		"is_active":   "BOOLEAN",
		"created_at":  "TIMESTAMP",
		"uuid":        "UUID",
	}

	for colName, expectedType := range expectedTypes {
		if colTypes[colName] != expectedType {
			t.Errorf("column %s: expected type %s, got %s", colName, expectedType, colTypes[colName])
		}
	}
}

func TestAnalyzeSQLFile_SkipsConstraintLines(t *testing.T) {
	analyzer := NewSchemaAnalyzer()

	sqlContent := `
CREATE TABLE orders (
    id INTEGER PRIMARY KEY,
    customer_id INTEGER,
    UNIQUE (id),
    CHECK (id > 0),
    CONSTRAINT fk_customer FOREIGN KEY (customer_id) REFERENCES customers(id)
);

CREATE TABLE customers (
    id INTEGER PRIMARY KEY
);
`

	entities := analyzer.AnalyzeSQLFile("schema.sql", sqlContent)

	// Find orders entity
	var orders *SchemaEntity
	for _, e := range entities {
		if e.Name == "Orders" {
			orders = e
			break
		}
	}

	if orders == nil {
		t.Fatal("expected to find Orders entity")
	}

	// Should only have id and customer_id columns, not UNIQUE, CHECK, or CONSTRAINT as columns
	for _, col := range orders.Columns {
		if col.Name == "UNIQUE" || col.Name == "CHECK" || col.Name == "CONSTRAINT" {
			t.Errorf("constraint keyword %q should not be extracted as column", col.Name)
		}
	}
}
