package sqlite

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

func TestInspectPageParsedPayloadFromFixtures(t *testing.T) {
	t.Parallel()

	t.Run("table_leaf_sample_page_1", func(t *testing.T) {
		t.Parallel()

		inspector, err := Open(fixturePath("sample.db"))
		if err != nil {
			t.Fatalf("Open returned error: %v", err)
		}
		defer inspector.Close()

		inspection, err := inspector.InspectPage(1)
		if err != nil {
			t.Fatalf("InspectPage returned error: %v", err)
		}
		if inspection.BTreePage.PageHeader.PageKind.Value != LeafTableBTreePage {
			t.Fatalf("PageKind = 0x%02x, want 0x0d", inspection.BTreePage.PageHeader.PageKind.Value)
		}
		if len(inspection.BTreePage.TableLeafCells) == 0 {
			t.Fatal("expected at least one table leaf cell")
		}
		for idx, cell := range inspection.BTreePage.TableLeafCells {
			if cell.ParsedPayload == nil {
				t.Fatalf("cell %d ParsedPayload = nil, want non-nil", idx)
			}
			if cell.ParsedPayload.OverflowFirstPage != nil {
				t.Fatalf("cell %d OverflowFirstPage != nil, want nil for sample fixture", idx)
			}
			if len(cell.ParsedPayload.SerialTypes) == 0 {
				t.Fatalf("cell %d SerialTypes is empty", idx)
			}
			if len(cell.ParsedPayload.Columns) != len(cell.ParsedPayload.SerialTypes) {
				t.Fatalf("cell %d len(Columns) = %d, want %d", idx, len(cell.ParsedPayload.Columns), len(cell.ParsedPayload.SerialTypes))
			}
			if cell.ParsedPayload.Meta.Size == 0 {
				t.Fatalf("cell %d parsed payload meta size = 0", idx)
			}
		}
	})

	t.Run("index_pages_have_parsed_payload", func(t *testing.T) {
		t.Parallel()

		inspector, err := Open(fixturePath("companies.db"))
		if err != nil {
			t.Fatalf("Open returned error: %v", err)
		}
		defer inspector.Close()

		interiorIndexPage, err := inspector.InspectPage(4)
		if err != nil {
			t.Fatalf("InspectPage(4) returned error: %v", err)
		}
		if interiorIndexPage.BTreePage.PageHeader.PageKind.Value != InteriorIndexBTreePage {
			t.Fatalf("page 4 kind = 0x%02x, want 0x02", interiorIndexPage.BTreePage.PageHeader.PageKind.Value)
		}
		if len(interiorIndexPage.BTreePage.IndexInteriorCells) == 0 {
			t.Fatal("expected at least one index interior cell on page 4")
		}
		if interiorIndexPage.BTreePage.IndexInteriorCells[0].ParsedPayload == nil {
			t.Fatal("index interior cell ParsedPayload = nil, want non-nil")
		}
	})

	t.Run("index_leaf_page_has_parsed_payload", func(t *testing.T) {
		t.Parallel()

		inspector, err := Open(fixturePath("superheroes.db"))
		if err != nil {
			t.Fatalf("Open returned error: %v", err)
		}
		defer inspector.Close()

		inspection, err := inspector.InspectPage(7)
		if err != nil {
			t.Fatalf("InspectPage(7) returned error: %v", err)
		}
		if inspection.BTreePage.PageHeader.PageKind.Value != LeafIndexBTreePage {
			t.Fatalf("page 7 kind = 0x%02x, want 0x0a", inspection.BTreePage.PageHeader.PageKind.Value)
		}
		if len(inspection.BTreePage.IndexLeafCells) == 0 {
			t.Fatal("expected at least one index leaf cell")
		}
		first := inspection.BTreePage.IndexLeafCells[0]
		if first.ParsedPayload == nil {
			t.Fatal("index leaf cell ParsedPayload = nil, want non-nil")
		}
		if len(first.ParsedPayload.Columns) != len(first.ParsedPayload.SerialTypes) {
			t.Fatalf("len(Columns) = %d, want %d", len(first.ParsedPayload.Columns), len(first.ParsedPayload.SerialTypes))
		}
		if len(inspection.BTreePage.CellPointerArray.Pointers) == 0 {
			t.Fatal("CellPointers is empty")
		}
		if inspection.BTreePage.CellPointerArray.Meta.Size == 0 {
			t.Fatal("CellPointerArray.Meta.Size = 0")
		}
	})
}

func TestInspectPageMarksOverflowCell(t *testing.T) {
	t.Parallel()

	dbPath := filepath.Join(t.TempDir(), "overflow.db")
	if err := os.WriteFile(dbPath, buildSinglePageOverflowDB(), 0644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	inspector, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open returned error: %v", err)
	}
	defer inspector.Close()

	inspection, err := inspector.InspectPage(1)
	if err != nil {
		t.Fatalf("InspectPage returned error: %v", err)
	}

	if len(inspection.BTreePage.TableLeafCells) != 1 {
		t.Fatalf("len(TableLeafCells) = %d, want 1", len(inspection.BTreePage.TableLeafCells))
	}
	parsed := inspection.BTreePage.TableLeafCells[0].ParsedPayload
	if parsed == nil {
		t.Fatal("ParsedPayload = nil, want non-nil")
	}
	if parsed.OverflowFirstPage == nil {
		t.Fatal("OverflowFirstPage = nil, want non-nil")
	}
	if parsed.OverflowFirstPage.Value != 2 {
		t.Fatalf("OverflowFirstPage = %d, want 2", parsed.OverflowFirstPage.Value)
	}
	if len(parsed.Columns) != 0 {
		t.Fatalf("len(Columns) = %d, want 0 for overflow-only marker payload", len(parsed.Columns))
	}
}

func TestInspectDatabaseMetadata(t *testing.T) {
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
	if metadata.DBHeader.HeaderString != "SQLite format 3\x00" {
		t.Fatalf("HeaderString = %q, want %q", metadata.DBHeader.HeaderString, "SQLite format 3\x00")
	}
	if metadata.DBHeader.PageSize != 4096 {
		t.Fatalf("PageSize = %d, want 4096", metadata.DBHeader.PageSize)
	}
}

func buildSinglePageOverflowDB() []byte {
	const pageSize = 4096
	buf := make([]byte, pageSize)

	copy(buf[0:16], []byte("SQLite format 3\x00"))
	binary.BigEndian.PutUint16(buf[16:18], uint16(pageSize))
	buf[18] = 1
	buf[19] = 1
	buf[20] = 0
	buf[21] = 64
	buf[22] = 32
	buf[23] = 32
	binary.BigEndian.PutUint32(buf[28:32], 1)
	binary.BigEndian.PutUint32(buf[44:48], 4)
	binary.BigEndian.PutUint32(buf[56:60], 1)
	binary.BigEndian.PutUint32(buf[92:96], 1)
	binary.BigEndian.PutUint32(buf[96:100], 3034000)

	headerOffset := 100
	buf[headerOffset] = byte(LeafTableBTreePage)
	binary.BigEndian.PutUint16(buf[headerOffset+3:headerOffset+5], 1)
	binary.BigEndian.PutUint16(buf[headerOffset+5:headerOffset+7], 3500)
	binary.BigEndian.PutUint16(buf[headerOffset+8:headerOffset+10], 3500)

	cellOffset := 3500
	cell := make([]byte, 0, 500)
	cell = append(cell, encodeVarint(4062)...)
	cell = append(cell, encodeVarint(1)...)
	cell = append(cell, make([]byte, 489)...)
	cell = append(cell, 0x00, 0x00, 0x00, 0x02)
	copy(buf[cellOffset:cellOffset+len(cell)], cell)

	return buf
}
