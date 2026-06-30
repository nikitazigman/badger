package sqlite

import "testing"

func TestInspectDatabaseMetadataIncludesSchemaRecords(t *testing.T) {
	t.Parallel()

	t.Run("sample_db", func(t *testing.T) {
		t.Parallel()

		inspector, err := Open(fixturePath("sample.db"))
		if err != nil {
			t.Fatalf("Open returned error: %v", err)
		}
		defer inspector.Close()

		metadata, err := inspector.InspectDatabaseMetadata()
		if err != nil {
			t.Fatalf("InspectDatabaseMetadata returned error: %v", err)
		}

		if metadata.Path != fixturePath("sample.db") {
			t.Fatalf("Path = %q, want %q", metadata.Path, fixturePath("sample.db"))
		}
		if len(metadata.SchemaRecords) != 3 {
			t.Fatalf("len(SchemaRecords) = %d, want 3", len(metadata.SchemaRecords))
		}

		assertSchemaRecord(t, metadata.SchemaRecords[0], "table", "apples", "apples")
		assertSchemaRecord(t, metadata.SchemaRecords[1], "table", "sqlite_sequence", "sqlite_sequence")
		assertSchemaRecord(t, metadata.SchemaRecords[2], "table", "oranges", "oranges")
	})

	t.Run("companies_db", func(t *testing.T) {
		t.Parallel()

		inspector, err := Open(fixturePath("companies.db"))
		if err != nil {
			t.Fatalf("Open returned error: %v", err)
		}
		defer inspector.Close()

		metadata, err := inspector.InspectDatabaseMetadata()
		if err != nil {
			t.Fatalf("InspectDatabaseMetadata returned error: %v", err)
		}

		if len(metadata.SchemaRecords) != 3 {
			t.Fatalf("len(SchemaRecords) = %d, want 3", len(metadata.SchemaRecords))
		}

		assertSchemaRecord(t, metadata.SchemaRecords[0], "table", "companies", "companies")
		assertSchemaRecord(t, metadata.SchemaRecords[1], "table", "sqlite_sequence", "sqlite_sequence")
		assertSchemaRecord(t, metadata.SchemaRecords[2], "index", "idx_companies_country", "companies")
	})

	// multipage_schema.db has so many tables that the sqlite_schema b-tree is
	// deeper than one page: page 1 is an interior node and the schema rows live on
	// its leaf children. This guards against regressing to the old "page 1 must be
	// a leaf" assumption.
	t.Run("multipage_schema_db", func(t *testing.T) {
		t.Parallel()

		inspector, err := Open(fixturePath("multipage_schema.db"))
		if err != nil {
			t.Fatalf("Open returned error: %v", err)
		}
		defer inspector.Close()

		page, err := inspector.InspectPage(1)
		if err != nil {
			t.Fatalf("InspectPage(1) returned error: %v", err)
		}
		if !page.BTreePage.PageHeader.IsInterior() {
			t.Fatalf("fixture invariant broken: page 1 is not an interior page (kind 0x%02x)", page.BTreePage.PageHeader.PageKind.Value)
		}

		metadata, err := inspector.InspectDatabaseMetadata()
		if err != nil {
			t.Fatalf("InspectDatabaseMetadata returned error: %v", err)
		}

		if len(metadata.SchemaRecords) != 120 {
			t.Fatalf("len(SchemaRecords) = %d, want 120", len(metadata.SchemaRecords))
		}
	})
}

func assertSchemaRecord(t *testing.T, got Row, wantType string, wantName string, wantTableName string) {
	t.Helper()

	recordType, ok := got["type"].(string)
	if !ok {
		t.Fatalf("type = %T, want string", got["type"])
	}
	if recordType != wantType {
		t.Fatalf("type = %q, want %q", recordType, wantType)
	}

	name, ok := got["name"].(string)
	if !ok {
		t.Fatalf("name = %T, want string", got["name"])
	}
	if name != wantName {
		t.Fatalf("name = %q, want %q", name, wantName)
	}

	tableName, ok := got["tbl_name"].(string)
	if !ok {
		t.Fatalf("tbl_name = %T, want string", got["tbl_name"])
	}
	if tableName != wantTableName {
		t.Fatalf("tbl_name = %q, want %q", tableName, wantTableName)
	}

	sqlValue, exists := got["sql"]
	if !exists {
		t.Fatal(`sql field missing`)
	}
	sql, ok := sqlValue.(string)
	if !ok {
		t.Fatalf("sql = %T, want string", sqlValue)
	}
	if sql == "" {
		t.Fatal("sql = empty string, want non-empty SQL text")
	}
}
