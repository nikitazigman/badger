package storage

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

func fixturePath(name string) string {
	return filepath.Join("..", "..", "fixtures", name)
}

func TestOpenSQLiteOverview(t *testing.T) {
	t.Parallel()

	db, err := Open(fixturePath("companies.db"))
	if err != nil {
		t.Fatalf("Open returned error: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	if got := db.Engine(); got != EngineSQLite {
		t.Fatalf("Engine = %q, want %q", got, EngineSQLite)
	}

	overview, err := db.Overview()
	if err != nil {
		t.Fatalf("Overview returned error: %v", err)
	}
	if overview.FirstPageID != 1 {
		t.Fatalf("FirstPageID = %d, want 1", overview.FirstPageID)
	}
	if overview.PageSizeBytes == 0 || overview.PageCount == 0 || overview.DatabaseSizeBytes == 0 {
		t.Fatalf("overview has invalid size fields: %+v", overview)
	}
	if len(overview.HeaderRows) == 0 {
		t.Fatal("Overview returned no header rows")
	}
	if btreeByName(overview.BTrees, "sqlite_schema") == nil {
		t.Fatal("Overview missing sqlite_schema b-tree item")
	}
	if btreeByName(overview.BTrees, "companies") == nil {
		t.Fatal("Overview missing companies b-tree item")
	}
}

func TestSQLiteInspectPageAndPagesForBTree(t *testing.T) {
	t.Parallel()

	db, err := Open(fixturePath("companies.db"))
	if err != nil {
		t.Fatalf("Open returned error: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	overview, err := db.Overview()
	if err != nil {
		t.Fatalf("Overview returned error: %v", err)
	}
	companies := btreeByName(overview.BTrees, "companies")
	if companies == nil {
		t.Fatal("companies b-tree item not found")
	}

	pages, err := db.PagesForBTree(companies.ID)
	if err != nil {
		t.Fatalf("PagesForBTree returned error: %v", err)
	}
	if len(pages) == 0 {
		t.Fatal("PagesForBTree returned no pages")
	}
	if companies.RootPage == nil {
		t.Fatal("companies b-tree item has nil root page")
	}
	if pages[0].ID != companies.RootPage.ID {
		t.Fatalf("first page = %d, want root page %d", pages[0].ID, companies.RootPage.ID)
	}

	page, err := db.InspectPage(pages[0])
	if err != nil {
		t.Fatalf("InspectPage returned error: %v", err)
	}
	if page.Ref != pages[0] {
		t.Fatalf("page ref = %+v, want %+v", page.Ref, pages[0])
	}
	if len(page.Raw) != int(overview.PageSizeBytes) {
		t.Fatalf("raw page bytes = %d, want %d", len(page.Raw), overview.PageSizeBytes)
	}
	if len(page.Rows) == 0 || len(page.HexBlocks) == 0 {
		t.Fatalf("page inspection missing rows or hex blocks: rows=%d blocks=%d", len(page.Rows), len(page.HexBlocks))
	}
}

func TestOpenBboltOverview(t *testing.T) {
	t.Parallel()

	db, err := Open(fixturePath(filepath.Join("bbolt", "single_bucket", "users.db")))
	if err != nil {
		t.Fatalf("Open returned error: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	if got := db.Engine(); got != EngineBbolt {
		t.Fatalf("Engine = %q, want %q", got, EngineBbolt)
	}

	overview, err := db.Overview()
	if err != nil {
		t.Fatalf("Overview returned error: %v", err)
	}
	if overview.FirstPageID != 0 {
		t.Fatalf("FirstPageID = %d, want 0", overview.FirstPageID)
	}
	if overview.PageSizeBytes == 0 || overview.PageCount == 0 || overview.DatabaseSizeBytes == 0 {
		t.Fatalf("overview has invalid size fields: %+v", overview)
	}
	highWaterMark, err := strconv.ParseUint(testFieldValue(overview.HeaderRows, "High water mark"), 10, 64)
	if err != nil {
		t.Fatalf("High water mark header row is invalid: %v", err)
	}
	if overview.PageCount != highWaterMark {
		t.Fatalf("PageCount = %d, want high water mark %d", overview.PageCount, highWaterMark)
	}
	if testFieldValue(overview.HeaderRows, "Transaction ID") == "" {
		t.Fatal("Overview missing Transaction ID header row")
	}

	root := btreeByName(overview.BTrees, "root")
	if root == nil {
		t.Fatal("Overview missing root bucket b-tree item")
	}
	if root.Kind != BTreeBucket {
		t.Fatalf("root kind = %q, want %q", root.Kind, BTreeBucket)
	}
	if root.RootPage == nil {
		t.Fatal("root bucket has nil root page")
	}
}

func TestOpenBboltNestedBucketsFixture(t *testing.T) {
	t.Parallel()

	db, err := Open(fixturePath(filepath.Join("bbolt", "nested_buckets", "app.db")))
	if err != nil {
		t.Fatalf("Open returned error: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	if got := db.Engine(); got != EngineBbolt {
		t.Fatalf("Engine = %q, want %q", got, EngineBbolt)
	}
	overview, err := db.Overview()
	if err != nil {
		t.Fatalf("Overview returned error: %v", err)
	}
	if overview.FirstPageID != 0 || overview.PageCount == 0 {
		t.Fatalf("unexpected bbolt page range: first=%d count=%d", overview.FirstPageID, overview.PageCount)
	}
}

func TestOpenBboltDetectsByMetaNotExtension(t *testing.T) {
	t.Parallel()

	source := fixturePath(filepath.Join("bbolt", "single_bucket", "users.db"))
	data, err := os.ReadFile(source)
	if err != nil {
		t.Fatalf("ReadFile fixture: %v", err)
	}
	path := filepath.Join(t.TempDir(), "users.notdb")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("WriteFile copied fixture: %v", err)
	}

	db, err := Open(path)
	if err != nil {
		t.Fatalf("Open returned error: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	if got := db.Engine(); got != EngineBbolt {
		t.Fatalf("Engine = %q, want %q", got, EngineBbolt)
	}
}

func TestBboltInspectPageAndPagesForBTree(t *testing.T) {
	t.Parallel()

	db, err := Open(fixturePath(filepath.Join("bbolt", "single_bucket", "users.db")))
	if err != nil {
		t.Fatalf("Open returned error: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	overview, err := db.Overview()
	if err != nil {
		t.Fatalf("Overview returned error: %v", err)
	}
	root := btreeByName(overview.BTrees, "root")
	if root == nil {
		t.Fatal("root bucket b-tree item not found")
	}

	pages, err := db.PagesForBTree(root.ID)
	if err != nil {
		t.Fatalf("PagesForBTree returned error: %v", err)
	}
	if len(pages) != 1 {
		t.Fatalf("PagesForBTree returned %d pages, want 1", len(pages))
	}
	if root.RootPage == nil || pages[0].ID != root.RootPage.ID {
		t.Fatalf("PagesForBTree first page = %+v, want root %+v", pages[0], root.RootPage)
	}

	page, err := db.InspectPage(PageRef{ID: 0})
	if err != nil {
		t.Fatalf("InspectPage(0) returned error: %v", err)
	}
	if page.Ref.ID != 0 {
		t.Fatalf("page ref = %+v, want page 0", page.Ref)
	}
	if len(page.Raw) != int(overview.PageSizeBytes) {
		t.Fatalf("raw page bytes = %d, want %d", len(page.Raw), overview.PageSizeBytes)
	}
	if len(page.Rows) == 0 || len(page.HexBlocks) == 0 {
		t.Fatalf("page inspection missing rows or hex blocks: rows=%d blocks=%d", len(page.Rows), len(page.HexBlocks))
	}

	if _, err := db.InspectPage(PageRef{ID: overview.PageCount}); err == nil {
		t.Fatal("InspectPage(PageCount) returned nil error")
	}
}

func TestBboltMetaPageInspectionExposesHeaderAndMetaBlocks(t *testing.T) {
	t.Parallel()

	db, err := Open(fixturePath(filepath.Join("bbolt", "single_bucket", "users.db")))
	if err != nil {
		t.Fatalf("Open returned error: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	page, err := db.InspectPage(PageRef{ID: 0})
	if err != nil {
		t.Fatalf("InspectPage(0) returned error: %v", err)
	}

	if testFieldValue(page.Rows, "Magic") == "" {
		t.Fatal("page rows missing Magic")
	}
	if testFieldValue(page.Rows, "Checksum") == "" {
		t.Fatal("page rows missing Checksum")
	}

	header := testHexBlockByKind(page.HexBlocks, blockPageHeader)
	if header == nil {
		t.Fatal("page missing page header hex block")
	}
	assertByteSpan(t, header.Span, 0, 16)
	assertFieldSpan(t, header.Rows, "Flags", 8, 2)
	assertFieldSpan(t, header.Rows, "Overflow", 12, 4)

	meta := testHexBlockByKind(page.HexBlocks, blockMetaPayload)
	if meta == nil {
		t.Fatal("page missing meta payload hex block")
	}
	assertByteSpan(t, meta.Span, 16, 64)
	assertFieldSpan(t, meta.Rows, "Magic", 16, 4)
	assertFieldSpan(t, meta.Rows, "Page size", 24, 4)
	assertFieldSpan(t, meta.Rows, "Root page", 32, 8)
	assertFieldSpan(t, meta.Rows, "Sequence", 40, 8)
	assertFieldSpan(t, meta.Rows, "Freelist page", 48, 8)
	assertFieldSpan(t, meta.Rows, "High water mark", 56, 8)
	assertFieldSpan(t, meta.Rows, "Transaction ID", 64, 8)
	assertFieldSpan(t, meta.Rows, "Checksum", 72, 8)
}

func TestOpenUnsupportedEngine(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "not-a-db")
	if err := os.WriteFile(path, []byte("not sqlite"), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	if _, err := Open(path); err == nil {
		t.Fatal("Open returned nil error for unsupported database")
	}
}

func btreeByName(items []BTreeItem, name string) *BTreeItem {
	for idx := range items {
		if items[idx].Name == name {
			return &items[idx]
		}
	}
	return nil
}

func testFieldValue(rows []Field, label string) string {
	for _, row := range rows {
		if row.Label == label {
			return row.Value
		}
	}
	return ""
}

func testHexBlockByKind(blocks []HexBlock, kind string) *HexBlock {
	for idx := range blocks {
		if blocks[idx].Kind == kind {
			return &blocks[idx]
		}
	}
	return nil
}

func assertByteSpan(t *testing.T, got ByteSpan, start int, size int) {
	t.Helper()

	want := ByteSpan{Start: start, Size: size}
	if got != want {
		t.Fatalf("span = %+v, want %+v", got, want)
	}
}

func assertFieldSpan(t *testing.T, rows []Field, label string, start int, size int) {
	t.Helper()

	for _, row := range rows {
		if row.Label != label {
			continue
		}
		if row.Span == nil {
			t.Fatalf("%q has nil span", label)
		}
		assertByteSpan(t, *row.Span, start, size)
		return
	}
	t.Fatalf("field %q not found", label)
}
