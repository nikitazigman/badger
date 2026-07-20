package storage

import (
	"os"
	"path/filepath"
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
