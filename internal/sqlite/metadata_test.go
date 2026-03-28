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
}

func assertSchemaRecord(t *testing.T, got GenericRecord, wantType string, wantName string, wantTableName string) {
	t.Helper()

	recordType, ok := got.Row["type"].(string)
	if !ok {
		t.Fatalf("type = %T, want string", got.Row["type"])
	}
	if recordType != wantType {
		t.Fatalf("type = %q, want %q", recordType, wantType)
	}

	name, ok := got.Row["name"].(string)
	if !ok {
		t.Fatalf("name = %T, want string", got.Row["name"])
	}
	if name != wantName {
		t.Fatalf("name = %q, want %q", name, wantName)
	}

	tableName, ok := got.Row["tbl_name"].(string)
	if !ok {
		t.Fatalf("tbl_name = %T, want string", got.Row["tbl_name"])
	}
	if tableName != wantTableName {
		t.Fatalf("tbl_name = %q, want %q", tableName, wantTableName)
	}

	sqlValue, exists := got.Row["sql"]
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
