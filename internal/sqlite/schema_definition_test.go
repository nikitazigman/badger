package sqlite

import "testing"

func TestParseSchemaDefinitionFromFixtures(t *testing.T) {
	t.Parallel()

	t.Run("builtin_sqlite_schema_definition", func(t *testing.T) {
		t.Parallel()

		definition, err := ParseSchemaDefinitionSQL(sqliteSchemaTableSQL)
		if err != nil {
			t.Fatalf("ParseSchemaDefinitionSQL returned error: %v", err)
		}
		if len(definition.Fields) != 5 {
			t.Fatalf("len(Fields) = %d, want 5", len(definition.Fields))
		}
		assertSchemaField(t, definition.Fields[0], SchemaField{Name: "type", DeclaredType: "text"})
		assertSchemaField(t, definition.Fields[1], SchemaField{Name: "name", DeclaredType: "text"})
		assertSchemaField(t, definition.Fields[2], SchemaField{Name: "tbl_name", DeclaredType: "text"})
		assertSchemaField(t, definition.Fields[3], SchemaField{Name: "rootpage", DeclaredType: "integer"})
		assertSchemaField(t, definition.Fields[4], SchemaField{Name: "sql", DeclaredType: "text"})
	})

	t.Run("table_definition", func(t *testing.T) {
		t.Parallel()

		metadata := inspectMetadataFixture(t, "sample.db")
		applesSQL := requireSchemaSQL(t, metadata.SchemaRecords, "apples")

		definition, err := ParseSchemaDefinitionSQL(applesSQL)
		if err != nil {
			t.Fatalf("ParseSchemaDefinitionSQL returned error: %v", err)
		}
		if len(definition.Fields) != 3 {
			t.Fatalf("len(Fields) = %d, want 3", len(definition.Fields))
		}
		assertSchemaField(t, definition.Fields[0], SchemaField{Name: "id", DeclaredType: "integer"})
		assertSchemaField(t, definition.Fields[1], SchemaField{Name: "name", DeclaredType: "text"})
		assertSchemaField(t, definition.Fields[2], SchemaField{Name: "color", DeclaredType: "text"})
	})

	t.Run("index_definition", func(t *testing.T) {
		t.Parallel()

		metadata := inspectMetadataFixture(t, "companies.db")
		indexSQL := requireSchemaSQL(t, metadata.SchemaRecords, "idx_companies_country")

		definition, err := ParseSchemaDefinitionSQL(indexSQL)
		if err != nil {
			t.Fatalf("ParseSchemaDefinitionSQL returned error: %v", err)
		}
		if len(definition.Fields) != 1 {
			t.Fatalf("len(Fields) = %d, want 1", len(definition.Fields))
		}
		assertSchemaField(t, definition.Fields[0], SchemaField{Name: "country", Expression: "country"})
	})

	t.Run("sqlite_if_not_exists_table_fallback", func(t *testing.T) {
		t.Parallel()

		metadata := inspectMetadataFixture(t, "superheroes.db")
		tableSQL := requireSchemaSQL(t, metadata.SchemaRecords, "superheroes")

		definition, err := ParseSchemaDefinitionSQL(tableSQL)
		if err != nil {
			t.Fatalf("ParseSchemaDefinitionSQL returned error: %v", err)
		}
		if len(definition.Fields) != 7 {
			t.Fatalf("len(Fields) = %d, want 7", len(definition.Fields))
		}
		assertSchemaField(t, definition.Fields[0], SchemaField{Name: "id", DeclaredType: "integer"})
		assertSchemaField(t, definition.Fields[1], SchemaField{Name: "name", DeclaredType: "text"})
	})
}

func TestParseRecordWithSchema(t *testing.T) {
	t.Parallel()

	t.Run("sqlite_schema_record", func(t *testing.T) {
		t.Parallel()

		inspector, err := Open(fixturePath("sample.db"))
		if err != nil {
			t.Fatalf("Open returned error: %v", err)
		}
		defer inspector.Close()

		page, err := inspector.InspectPage(1)
		if err != nil {
			t.Fatalf("InspectPage returned error: %v", err)
		}
		if len(page.BTreePage.TableLeafCells) == 0 {
			t.Fatal("expected sqlite_schema rows on page 1")
		}

		definition, err := ParseSchemaDefinitionSQL(sqliteSchemaTableSQL)
		if err != nil {
			t.Fatalf("ParseSchemaDefinitionSQL returned error: %v", err)
		}

		record, err := ParseRecord(page.BTreePage.TableLeafCells[0].ParsedPayload, definition)
		if err != nil {
			t.Fatalf("ParseRecord returned error: %v", err)
		}
		if len(record.Row) != 5 {
			t.Fatalf("len(Row) = %d, want 5", len(record.Row))
		}
		for _, name := range []string{"type", "name", "tbl_name", "rootpage", "sql"} {
			if _, ok := record.Row[name]; !ok {
				t.Fatalf("expected field %q to exist", name)
			}
		}
	})

	t.Run("table_leaf_record", func(t *testing.T) {
		t.Parallel()

		metadata := inspectMetadataFixture(t, "sample.db")
		applesSQL := requireSchemaSQL(t, metadata.SchemaRecords, "apples")
		applesRootPage := requireSchemaRootPage(t, metadata.SchemaRecords, "apples")

		inspector, err := Open(fixturePath("sample.db"))
		if err != nil {
			t.Fatalf("Open returned error: %v", err)
		}
		defer inspector.Close()

		page, err := inspector.InspectPage(applesRootPage)
		if err != nil {
			t.Fatalf("InspectPage returned error: %v", err)
		}
		if len(page.BTreePage.TableLeafCells) == 0 {
			t.Fatal("expected at least one table leaf cell")
		}

		definition, err := ParseSchemaDefinitionSQL(applesSQL)
		if err != nil {
			t.Fatalf("ParseSchemaDefinitionSQL returned error: %v", err)
		}

		record, err := ParseRecord(page.BTreePage.TableLeafCells[0].ParsedPayload, definition)
		if err != nil {
			t.Fatalf("ParseRecord returned error: %v", err)
		}
		if len(record.Row) != 3 {
			t.Fatalf("len(Row) = %d, want 3", len(record.Row))
		}
		for _, name := range []string{"id", "name", "color"} {
			if _, ok := record.Row[name]; !ok {
				t.Fatalf("expected field %q to exist", name)
			}
		}
	})

	t.Run("index_record_has_extra_terms", func(t *testing.T) {
		t.Parallel()

		metadata := inspectMetadataFixture(t, "companies.db")
		indexSQL := requireSchemaSQL(t, metadata.SchemaRecords, "idx_companies_country")
		indexRootPage := requireSchemaRootPage(t, metadata.SchemaRecords, "idx_companies_country")

		inspector, err := Open(fixturePath("companies.db"))
		if err != nil {
			t.Fatalf("Open returned error: %v", err)
		}
		defer inspector.Close()

		page, err := inspector.InspectPage(indexRootPage)
		if err != nil {
			t.Fatalf("InspectPage returned error: %v", err)
		}
		if len(page.BTreePage.IndexInteriorCells) == 0 {
			t.Fatal("expected at least one index interior cell")
		}

		definition, err := ParseSchemaDefinitionSQL(indexSQL)
		if err != nil {
			t.Fatalf("ParseSchemaDefinitionSQL returned error: %v", err)
		}

		record, err := ParseRecord(page.BTreePage.IndexInteriorCells[0].ParsedPayload, definition)
		if err != nil {
			t.Fatalf("ParseRecord returned error: %v", err)
		}
		if len(record.Row) < 2 {
			t.Fatalf("len(Row) = %d, want at least 2", len(record.Row))
		}
		if _, ok := record.Row["country"]; !ok {
			t.Fatalf("expected field %q to exist", "country")
		}
		if _, ok := record.Row["__extra_1"]; !ok {
			t.Fatalf("expected extra field %q to exist", "__extra_1")
		}
	})
}

func inspectMetadataFixture(t *testing.T, name string) *MetadataInspection {
	t.Helper()

	inspector, err := Open(fixturePath(name))
	if err != nil {
		t.Fatalf("Open returned error: %v", err)
	}
	defer inspector.Close()

	metadata, err := inspector.InspectDatabaseMetadata()
	if err != nil {
		t.Fatalf("InspectDatabaseMetadata returned error: %v", err)
	}
	return metadata
}

func requireSchemaRecord(t *testing.T, records []GenericRecord, name string) GenericRecord {
	t.Helper()

	for _, record := range records {
		gotName, ok := record.Row["name"].(string)
		if !ok {
			t.Fatalf("name = %T, want string", record.Row["name"])
		}
		if gotName == name {
			return record
		}
	}
	t.Fatalf("schema record %q not found", name)
	return GenericRecord{}
}

func requireSchemaSQL(t *testing.T, records []GenericRecord, name string) string {
	t.Helper()

	record := requireSchemaRecord(t, records, name)
	sqlValue, ok := record.Row["sql"]
	if !ok {
		t.Fatalf("sql field missing for %q", name)
	}
	sql, ok := sqlValue.(string)
	if !ok {
		t.Fatalf("sql = %T, want string", sqlValue)
	}
	return sql
}

func requireSchemaRootPage(t *testing.T, records []GenericRecord, name string) uint32 {
	t.Helper()

	record := requireSchemaRecord(t, records, name)
	value, ok := record.Row["rootpage"].(int64)
	if !ok {
		t.Fatalf("rootpage = %T, want int64", record.Row["rootpage"])
	}
	return uint32(value)
}

func assertSchemaField(t *testing.T, got SchemaField, want SchemaField) {
	t.Helper()

	if got.Name != want.Name {
		t.Fatalf("Name = %q, want %q", got.Name, want.Name)
	}
	if got.DeclaredType != want.DeclaredType {
		t.Fatalf("DeclaredType = %q, want %q", got.DeclaredType, want.DeclaredType)
	}
	if got.Expression != want.Expression {
		t.Fatalf("Expression = %q, want %q", got.Expression, want.Expression)
	}
}
