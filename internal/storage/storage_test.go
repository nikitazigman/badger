package storage

import (
	"encoding/binary"
	"hash/fnv"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/nikitazigman/badger/internal/bbolt"
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

func TestBboltPageSummariesFreelistAndFreePageRows(t *testing.T) {
	t.Parallel()

	db, err := Open(writeSyntheticBboltFreelistDB(t))
	if err != nil {
		t.Fatalf("Open returned error: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	overview, err := db.Overview()
	if err != nil {
		t.Fatalf("Overview returned error: %v", err)
	}
	if len(overview.PageSummaries) != 5 {
		t.Fatalf("PageSummaries count = %d, want 5", len(overview.PageSummaries))
	}
	assertPageSummary(t, overview.PageSummaries, 0, PageClassMeta)
	assertPageSummary(t, overview.PageSummaries, 2, PageClassFreelist)
	assertPageSummary(t, overview.PageSummaries, 3, PageClassUnknown)
	assertPageSummary(t, overview.PageSummaries, 4, PageClassFree)

	freelist, err := db.InspectPage(PageRef{ID: 2})
	if err != nil {
		t.Fatalf("InspectPage(freelist) returned error: %v", err)
	}
	if testFieldValue(freelist.Rows, "Classification") != string(PageClassFreelist) {
		t.Fatalf("freelist classification row = %q", testFieldValue(freelist.Rows, "Classification"))
	}
	if testFieldValue(freelist.Rows, "Free page id 0") != "4" {
		t.Fatalf("freelist free page id row = %q, want 4", testFieldValue(freelist.Rows, "Free page id 0"))
	}
	block := testHexBlockByKind(freelist.HexBlocks, blockFreelistPayload)
	if block == nil {
		t.Fatal("freelist page missing freelist payload block")
	}
	if len(block.Children) != 1 {
		t.Fatalf("freelist payload children = %d, want 1", len(block.Children))
	}
	assertByteSpan(t, block.Children[0].Span, 16, 8)

	free, err := db.InspectPage(PageRef{ID: 4})
	if err != nil {
		t.Fatalf("InspectPage(free) returned error: %v", err)
	}
	if testFieldValue(free.Rows, "Classification") != string(PageClassFree) {
		t.Fatalf("free classification row = %q", testFieldValue(free.Rows, "Classification"))
	}
	if testFieldValue(free.Rows, "Freelist membership") != "yes" {
		t.Fatalf("free freelist row = %q, want yes", testFieldValue(free.Rows, "Freelist membership"))
	}
	if testFieldValue(free.Rows, "Note") == "" {
		t.Fatal("free page missing stale-bytes note")
	}
}

func TestBboltLeafPageInspectionExposesLeafRowsAndBlocks(t *testing.T) {
	t.Parallel()

	bucketPage, bucketEntry := findBboltLeafEntryInspection(t, []string{
		filepath.Join("bbolt", "single_bucket", "users.db"),
		filepath.Join("bbolt", "nested_buckets", "app.db"),
	}, "bucket")
	if testFieldValue(bucketPage.Rows, "Leaf entries") == "" {
		t.Fatal("bucket leaf page rows missing Leaf entries")
	}
	if testFieldValue(bucketPage.Rows, "Bucket entries") == "0" {
		t.Fatal("bucket leaf page rows did not count bucket entries")
	}
	assertLeafDescriptorStorageBlock(t, bucketPage)
	assertLeafEntryStorageBlocks(t, bucketEntry, true)

	ordinaryPage, ordinaryEntry := findBboltLeafEntryInspection(t, []string{
		filepath.Join("bbolt", "single_bucket", "users.db"),
		filepath.Join("bbolt", "nested_buckets", "app.db"),
		filepath.Join("bbolt", "large_values", "events.db"),
	}, "key/value")
	if testFieldValue(ordinaryPage.Rows, "Leaf entries") == "" {
		t.Fatal("ordinary leaf page rows missing Leaf entries")
	}
	assertLeafDescriptorStorageBlock(t, ordinaryPage)
	assertLeafEntryStorageBlocks(t, ordinaryEntry, false)
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

func findBboltLeafEntryInspection(t *testing.T, fixturePaths []string, entryType string) (*PageInspection, HexBlock) {
	t.Helper()

	for _, fixture := range fixturePaths {
		db, err := Open(fixturePath(fixture))
		if err != nil {
			t.Fatalf("Open(%s) returned error: %v", fixture, err)
		}

		overview, err := db.Overview()
		if err != nil {
			_ = db.Close()
			t.Fatalf("Overview(%s) returned error: %v", fixture, err)
		}
		for id := overview.FirstPageID; id < overview.FirstPageID+overview.PageCount; id++ {
			page, err := db.InspectPage(PageRef{ID: id})
			if err != nil {
				_ = db.Close()
				t.Fatalf("InspectPage(%d) in %s returned error: %v", id, fixture, err)
			}
			for _, entry := range page.HexBlocks {
				if entry.Kind != blockLeafEntry {
					continue
				}
				if testFieldValue(entry.Rows, "Type") == entryType {
					if err := db.Close(); err != nil {
						t.Fatalf("close db: %v", err)
					}
					return page, entry
				}
			}
		}
		if err := db.Close(); err != nil {
			t.Fatalf("close db: %v", err)
		}
	}

	t.Fatalf("fixtures did not contain bbolt leaf entry type %q", entryType)
	return nil, HexBlock{}
}

func assertLeafEntryStorageBlocks(t *testing.T, entry HexBlock, wantBucket bool) {
	t.Helper()

	key := testHexBlockByKind(entry.Children, blockLeafKey)
	if key == nil {
		t.Fatal("leaf entry missing key block")
	}
	if key.Span.Size == 0 {
		t.Fatal("leaf key block has zero size")
	}
	if testFieldValue(key.Rows, "Key") == "" {
		t.Fatal("leaf key block missing string key representation")
	}
	if got := testFieldValue(entry.Rows, "Key"); got == "" {
		t.Fatal("leaf entry rows missing key string representation")
	}

	value := testHexBlockByKind(entry.Children, blockLeafValue)
	if value == nil {
		t.Fatal("leaf entry missing value block")
	}
	if testFieldValue(value.Rows, "Value") == "" {
		t.Fatal("leaf value block missing raw byte count")
	}
	if wantBucket && testFieldValue(entry.Rows, "Type") != "bucket" {
		t.Fatalf("leaf entry type = %q, want bucket", testFieldValue(entry.Rows, "Type"))
	}
	if !wantBucket && testFieldValue(entry.Rows, "Type") != "key/value" {
		t.Fatalf("leaf entry type = %q, want key/value", testFieldValue(entry.Rows, "Type"))
	}
}

func assertLeafDescriptorStorageBlock(t *testing.T, page *PageInspection) {
	t.Helper()

	descriptors := testHexBlockByKind(page.HexBlocks, blockLeafDescriptors)
	if descriptors == nil {
		t.Fatal("leaf page missing descriptor list block")
	}
	if len(descriptors.Children) == 0 {
		t.Fatal("leaf descriptor list has no descriptor children")
	}
	descriptor := descriptors.Children[0]
	assertByteSpan(t, descriptor.Span, descriptor.Span.Start, 16)
	assertFieldSpan(t, descriptor.Rows, "Flags", descriptor.Span.Start, 4)
	assertFieldSpan(t, descriptor.Rows, "Position", descriptor.Span.Start+4, 4)
	assertFieldSpan(t, descriptor.Rows, "Key size", descriptor.Span.Start+8, 4)
	assertFieldSpan(t, descriptor.Rows, "Value size", descriptor.Span.Start+12, 4)
}

func assertPageSummary(t *testing.T, summaries []PageSummary, id uint64, classification PageClassification) {
	t.Helper()

	for _, summary := range summaries {
		if summary.Ref.ID != id {
			continue
		}
		if summary.Classification != classification {
			t.Fatalf("page %d classification = %q, want %q", id, summary.Classification, classification)
		}
		if summary.Label != string(classification) {
			t.Fatalf("page %d label = %q, want %q", id, summary.Label, classification)
		}
		return
	}
	t.Fatalf("page summary %d not found", id)
}

func writeSyntheticBboltFreelistDB(t *testing.T) string {
	t.Helper()

	const pageSize = 4096
	data := make([]byte, pageSize*5)
	putSyntheticBboltPageHeader(data[0:pageSize], 0, bbolt.MetaPageFlag, 0, 0)
	putSyntheticBboltMetaPayload(data[0:pageSize], pageSize, 3, 2, 5, 1)
	putSyntheticBboltPageHeader(data[2*pageSize:3*pageSize], 2, bbolt.FreelistPageFlag, 1, 0)
	binary.LittleEndian.PutUint64(data[2*pageSize+16:2*pageSize+24], 4)
	putSyntheticBboltPageHeader(data[4*pageSize:5*pageSize], 4, bbolt.LeafPageFlag, 0, 0)

	path := filepath.Join(t.TempDir(), "freelist.db")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("WriteFile synthetic bbolt db: %v", err)
	}
	return path
}

func putSyntheticBboltPageHeader(page []byte, id bbolt.PageID, flag bbolt.FlagType, count uint16, overflow uint32) {
	binary.LittleEndian.PutUint64(page[0:8], uint64(id))
	binary.LittleEndian.PutUint16(page[8:10], uint16(flag))
	binary.LittleEndian.PutUint16(page[10:12], count)
	binary.LittleEndian.PutUint32(page[12:16], overflow)
}

func putSyntheticBboltMetaPayload(page []byte, pageSize int, root bbolt.PageID, freelist bbolt.PageID, highWaterMark bbolt.PageID, txid uint64) {
	binary.LittleEndian.PutUint32(page[16:20], bbolt.Magic)
	binary.LittleEndian.PutUint32(page[20:24], bbolt.Version)
	binary.LittleEndian.PutUint32(page[24:28], uint32(pageSize))
	binary.LittleEndian.PutUint64(page[32:40], uint64(root))
	binary.LittleEndian.PutUint64(page[48:56], uint64(freelist))
	binary.LittleEndian.PutUint64(page[56:64], uint64(highWaterMark))
	binary.LittleEndian.PutUint64(page[64:72], txid)
	h := fnv.New64a()
	_, _ = h.Write(page[16:72])
	binary.LittleEndian.PutUint64(page[72:80], h.Sum64())
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
