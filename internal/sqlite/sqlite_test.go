package sqlite

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const fixturePageSize = 4096

func fixturePath(name string) string {
	return filepath.Join("..", "..", "fixtures", name)
}

func TestParseHeaderFromSampleFixture(t *testing.T) {
	t.Parallel()

	buf := readFixtureBytes(t, "sample.db", 0, 100)
	got, err := parseHeader(buf)
	if err != nil {
		t.Fatalf("parseHeader returned error: %v", err)
	}

	if got.HeaderString != "SQLite format 3\x00" {
		t.Fatalf("HeaderString = %q, want %q", got.HeaderString, "SQLite format 3\x00")
	}
	if got.PageSize != 4096 {
		t.Fatalf("PageSize = %d, want 4096", got.PageSize)
	}
	if got.WriteVersion != 1 || got.ReadVersion != 1 {
		t.Fatalf("write/read version = %d/%d, want 1/1", got.WriteVersion, got.ReadVersion)
	}
	if got.DatabasePageCount != 4 {
		t.Fatalf("DatabasePageCount = %d, want 4", got.DatabasePageCount)
	}
	if got.SchemaCookie != 2 {
		t.Fatalf("SchemaCookie = %d, want 2", got.SchemaCookie)
	}
	if got.SchemaFormat != 4 {
		t.Fatalf("SchemaFormat = %d, want 4", got.SchemaFormat)
	}
	if got.TextEncoding != 1 {
		t.Fatalf("TextEncoding = %d, want 1", got.TextEncoding)
	}
	if got.VersionValidFor != 5 {
		t.Fatalf("VersionValidFor = %d, want 5", got.VersionValidFor)
	}
	if got.SQLiteVersionNumber != 3034000 {
		t.Fatalf("SQLiteVersionNumber = %d, want 3034000", got.SQLiteVersionNumber)
	}
}

func TestParseHeaderPageSizeSpecialCase65536(t *testing.T) {
	t.Parallel()

	buf := make([]byte, 100)
	copy(buf[:16], []byte("SQLite format 3\x00"))
	binary.BigEndian.PutUint16(buf[16:18], 1)

	got, err := parseHeader(buf)
	if err != nil {
		t.Fatalf("parseHeader returned error: %v", err)
	}
	if got.PageSize != 65536 {
		t.Fatalf("PageSize = %d, want 65536", got.PageSize)
	}
}

func TestParseHeaderErrors(t *testing.T) {
	t.Parallel()

	t.Run("truncated", func(t *testing.T) {
		t.Parallel()

		_, err := parseHeader(make([]byte, 99))
		if err == nil {
			t.Fatal("expected error for truncated header, got nil")
		}
		if !strings.Contains(err.Error(), "sqlite header is truncated") {
			t.Fatalf("error = %q, want substring %q", err.Error(), "sqlite header is truncated")
		}
	})

	t.Run("invalid_magic", func(t *testing.T) {
		t.Parallel()

		buf := make([]byte, 100)
		copy(buf[:16], []byte("Not SQLite magic"))
		binary.BigEndian.PutUint16(buf[16:18], 4096)

		_, err := parseHeader(buf)
		if err == nil {
			t.Fatal("expected error for invalid magic, got nil")
		}
		if !strings.Contains(err.Error(), "invalid sqlite header magic") {
			t.Fatalf("error = %q, want substring %q", err.Error(), "invalid sqlite header magic")
		}
	})
}

func TestParseBTreePageLeafPageFromSampleFixture(t *testing.T) {
	t.Parallel()

	page := readFixturePage(t, "sample.db", 1)
	got, err := parseBTreePage(page[100:], 1)
	if err != nil {
		t.Fatalf("parseBTreePage returned error: %v", err)
	}

	if got.PageHeader.PageKind != 0x0d {
		t.Fatalf("PageKind = 0x%02x, want 0x0d", got.PageHeader.PageKind)
	}
	if got.PageHeader.HeaderSize() != 8 {
		t.Fatalf("HeaderSize = %d, want 8", got.PageHeader.HeaderSize())
	}
	if got.PageHeader.FirstFreeblock != 0 {
		t.Fatalf("FirstFreeblock = %d, want 0", got.PageHeader.FirstFreeblock)
	}
	if got.PageHeader.CellCount != 3 {
		t.Fatalf("CellCount = %d, want 3", got.PageHeader.CellCount)
	}
	if got.PageHeader.CellContentAreaOffset != 3779 {
		t.Fatalf("CellContentAreaOffset = %d, want 3779", got.PageHeader.CellContentAreaOffset)
	}
	if got.PageHeader.FragmentedFreeBytes != 0 {
		t.Fatalf("FragmentedFreeBytes = %d, want 0", got.PageHeader.FragmentedFreeBytes)
	}
	if got.PageHeader.RightMostPointer != nil {
		t.Fatalf("RightMostPointer = %v, want nil for leaf page", *got.PageHeader.RightMostPointer)
	}
	wantPointers := []uint16{3983, 3901, 3779}
	if !equalUint16Slice(got.CellPointers, wantPointers) {
		t.Fatalf("CellPointers = %v, want %v", got.CellPointers, wantPointers)
	}
}

func TestParseBTreePageInteriorKindsFromFixtures(t *testing.T) {
	t.Parallel()

	t.Run("table_interior_companies_page_2", func(t *testing.T) {
		t.Parallel()

		page := readFixturePage(t, "companies.db", 2)
		got, err := parseBTreePage(page, 2)
		if err != nil {
			t.Fatalf("parseBTreePage returned error: %v", err)
		}

		if got.PageHeader.PageKind != 0x05 {
			t.Fatalf("PageKind = 0x%02x, want 0x05", got.PageHeader.PageKind)
		}
		if got.PageHeader.HeaderSize() != 12 {
			t.Fatalf("HeaderSize = %d, want 12", got.PageHeader.HeaderSize())
		}
		if got.PageHeader.CellCount != 4 {
			t.Fatalf("CellCount = %d, want 4", got.PageHeader.CellCount)
		}
		if got.PageHeader.CellContentAreaOffset != 4065 {
			t.Fatalf("CellContentAreaOffset = %d, want 4065", got.PageHeader.CellContentAreaOffset)
		}
		if got.PageHeader.RightMostPointer == nil || *got.PageHeader.RightMostPointer != 1644 {
			t.Fatalf("RightMostPointer = %v, want 1644", got.PageHeader.RightMostPointer)
		}
		wantPointers := []uint16{4089, 4081, 4073, 4065}
		if !equalUint16Slice(got.CellPointers, wantPointers) {
			t.Fatalf("CellPointers = %v, want %v", got.CellPointers, wantPointers)
		}
	})

	t.Run("index_interior_companies_page_4", func(t *testing.T) {
		t.Parallel()

		page := readFixturePage(t, "companies.db", 4)
		got, err := parseBTreePage(page, 4)
		if err != nil {
			t.Fatalf("parseBTreePage returned error: %v", err)
		}

		if got.PageHeader.PageKind != 0x02 {
			t.Fatalf("PageKind = 0x%02x, want 0x02", got.PageHeader.PageKind)
		}
		if got.PageHeader.HeaderSize() != 12 {
			t.Fatalf("HeaderSize = %d, want 12", got.PageHeader.HeaderSize())
		}
		if got.PageHeader.CellCount != 1 {
			t.Fatalf("CellCount = %d, want 1", got.PageHeader.CellCount)
		}
		if got.PageHeader.CellContentAreaOffset != 4078 {
			t.Fatalf("CellContentAreaOffset = %d, want 4078", got.PageHeader.CellContentAreaOffset)
		}
		if got.PageHeader.RightMostPointer == nil || *got.PageHeader.RightMostPointer != 1850 {
			t.Fatalf("RightMostPointer = %v, want 1850", got.PageHeader.RightMostPointer)
		}
		wantPointers := []uint16{4078}
		if !equalUint16Slice(got.CellPointers, wantPointers) {
			t.Fatalf("CellPointers = %v, want %v", got.CellPointers, wantPointers)
		}
	})
}

func TestParseBTreePageIndexLeafFromFixture(t *testing.T) {
	t.Parallel()

	page := readFixturePage(t, "superheroes.db", 7)
	got, err := parseBTreePage(page, 7)
	if err != nil {
		t.Fatalf("parseBTreePage returned error: %v", err)
	}

	if got.PageHeader.PageKind != 0x0a {
		t.Fatalf("PageKind = 0x%02x, want 0x0a", got.PageHeader.PageKind)
	}
	if got.PageHeader.HeaderSize() != 8 {
		t.Fatalf("HeaderSize = %d, want 8", got.PageHeader.HeaderSize())
	}
	if got.PageHeader.CellCount != 448 {
		t.Fatalf("CellCount = %d, want 448", got.PageHeader.CellCount)
	}
	if got.PageHeader.RightMostPointer != nil {
		t.Fatal("RightMostPointer must be nil for leaf page")
	}
	if len(got.CellPointers) != 448 {
		t.Fatalf("len(CellPointers) = %d, want 448", len(got.CellPointers))
	}
	wantPrefix := []uint16{4090, 4084, 4078, 4072, 4066}
	if !equalUint16Slice(got.CellPointers[:5], wantPrefix) {
		t.Fatalf("CellPointers prefix = %v, want %v", got.CellPointers[:5], wantPrefix)
	}
}

func TestParseBTreePageErrors(t *testing.T) {
	t.Parallel()

	t.Run("unsupported_page_kind", func(t *testing.T) {
		t.Parallel()

		page := make([]byte, 8)
		page[0] = 0x01
		_, err := parseBTreePage(page, 9)
		if err == nil {
			t.Fatal("expected error for unsupported page kind, got nil")
		}
		if !strings.Contains(err.Error(), "unsupported b-tree page kind") {
			t.Fatalf("error = %q, want substring %q", err.Error(), "unsupported b-tree page kind")
		}
	})
}

func readFixturePage(t *testing.T, fixture string, pageNumber uint32) []byte {
	t.Helper()

	buf := readFixtureBytes(t, fixture, int((pageNumber-1)*fixturePageSize), fixturePageSize)
	return buf
}

func readFixtureBytes(t *testing.T, fixture string, offset int, size int) []byte {
	t.Helper()

	path := fixturePath(fixture)
	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("open fixture %s: %v", path, err)
	}
	defer func() {
		_ = file.Close()
	}()

	buf := make([]byte, size)
	n, err := file.ReadAt(buf, int64(offset))
	if err != nil {
		t.Fatalf("read %d bytes at offset %d from %s: %v", size, offset, path, err)
	}
	if n != size {
		t.Fatalf("read %d bytes at offset %d from %s, want %d", n, offset, path, size)
	}

	return buf
}

func equalUint16Slice(got []uint16, want []uint16) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
