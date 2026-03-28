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
	got, err := parseBTreePageForTest(page, 1)
	if err != nil {
		t.Fatalf("parseBTreePage returned error: %v", err)
	}

	if got.PageHeader.PageKind.Value != 0x0d {
		t.Fatalf("PageKind = 0x%02x, want 0x0d", got.PageHeader.PageKind.Value)
	}
	if got.PageHeader.HeaderSize() != 8 {
		t.Fatalf("HeaderSize = %d, want 8", got.PageHeader.HeaderSize())
	}
	if got.PageHeader.FirstFreeblock.Value != 0 {
		t.Fatalf("FirstFreeblock = %d, want 0", got.PageHeader.FirstFreeblock.Value)
	}
	if got.PageHeader.CellCount.Value != 3 {
		t.Fatalf("CellCount = %d, want 3", got.PageHeader.CellCount.Value)
	}
	if got.PageHeader.CellContentAreaOffset.Value != 3779 {
		t.Fatalf("CellContentAreaOffset = %d, want 3779", got.PageHeader.CellContentAreaOffset.Value)
	}
	if got.PageHeader.FragmentedFreeBytes.Value != 0 {
		t.Fatalf("FragmentedFreeBytes = %d, want 0", got.PageHeader.FragmentedFreeBytes.Value)
	}
	if got.PageHeader.RightMostPointer != nil {
		t.Fatalf("RightMostPointer = %v, want nil for leaf page", got.PageHeader.RightMostPointer.Value)
	}
	wantPointers := []uint16{3983, 3901, 3779}
	if !equalUint16Slice(cellPointerValues(got.CellPointerArray.Pointers), wantPointers) {
		t.Fatalf("CellPointers = %v, want %v", got.CellPointerArray.Pointers, wantPointers)
	}
	if got.PageHeader.Meta.StartOffset != 100 || got.PageHeader.Meta.EndOffset() != 108 {
		t.Fatalf("PageHeader.Meta = %+v, want 100..108", got.PageHeader.Meta)
	}
	if got.CellPointerArray.Meta.StartOffset != 108 || got.CellPointerArray.Meta.EndOffset() != 114 {
		t.Fatalf("CellPointerArray.Meta = %+v, want 108..114", got.CellPointerArray.Meta)
	}
	if len(got.TableLeafCells) != int(got.PageHeader.CellCount.Value) {
		t.Fatalf("len(TableLeafCells) = %d, want %d", len(got.TableLeafCells), got.PageHeader.CellCount.Value)
	}
	if got.TableLeafCells[0].RowID.Value != 1 {
		t.Fatalf("first RowID = %d, want 1", got.TableLeafCells[0].RowID.Value)
	}
	if got.TableLeafCells[2].RowID.Value != 3 {
		t.Fatalf("last RowID = %d, want 3", got.TableLeafCells[2].RowID.Value)
	}
	if len(got.TableInteriorCells) != 0 || len(got.IndexLeafCells) != 0 || len(got.IndexInteriorCells) != 0 {
		t.Fatal("only TableLeafCells should be populated for leaf table pages")
	}
}

func TestParseBTreePageInteriorKindsFromFixtures(t *testing.T) {
	t.Parallel()

	t.Run("table_interior_companies_page_2", func(t *testing.T) {
		t.Parallel()

		page := readFixturePage(t, "companies.db", 2)
		got, err := parseBTreePageForTest(page, 2)
		if err != nil {
			t.Fatalf("parseBTreePage returned error: %v", err)
		}

		if got.PageHeader.PageKind.Value != 0x05 {
			t.Fatalf("PageKind = 0x%02x, want 0x05", got.PageHeader.PageKind.Value)
		}
		if got.PageHeader.HeaderSize() != 12 {
			t.Fatalf("HeaderSize = %d, want 12", got.PageHeader.HeaderSize())
		}
		if got.PageHeader.CellCount.Value != 4 {
			t.Fatalf("CellCount = %d, want 4", got.PageHeader.CellCount.Value)
		}
		if got.PageHeader.CellContentAreaOffset.Value != 4065 {
			t.Fatalf("CellContentAreaOffset = %d, want 4065", got.PageHeader.CellContentAreaOffset.Value)
		}
		if got.PageHeader.RightMostPointer == nil || got.PageHeader.RightMostPointer.Value != 1644 {
			t.Fatalf("RightMostPointer = %v, want 1644", got.PageHeader.RightMostPointer)
		}
		wantPointers := []uint16{4089, 4081, 4073, 4065}
		if !equalUint16Slice(cellPointerValues(got.CellPointerArray.Pointers), wantPointers) {
			t.Fatalf("CellPointers = %v, want %v", got.CellPointerArray.Pointers, wantPointers)
		}
		if len(got.TableInteriorCells) != int(got.PageHeader.CellCount.Value) {
			t.Fatalf("len(TableInteriorCells) = %d, want %d", len(got.TableInteriorCells), got.PageHeader.CellCount.Value)
		}
		if got.TableInteriorCells[0].RowID.Value >= got.TableInteriorCells[len(got.TableInteriorCells)-1].RowID.Value {
			t.Fatalf("expected increasing rowids, got first=%d last=%d", got.TableInteriorCells[0].RowID.Value, got.TableInteriorCells[len(got.TableInteriorCells)-1].RowID.Value)
		}
		if len(got.TableLeafCells) != 0 || len(got.IndexLeafCells) != 0 || len(got.IndexInteriorCells) != 0 {
			t.Fatal("only TableInteriorCells should be populated for interior table pages")
		}
	})

	t.Run("index_interior_companies_page_4", func(t *testing.T) {
		t.Parallel()

		page := readFixturePage(t, "companies.db", 4)
		got, err := parseBTreePageForTest(page, 4)
		if err != nil {
			t.Fatalf("parseBTreePage returned error: %v", err)
		}

		if got.PageHeader.PageKind.Value != 0x02 {
			t.Fatalf("PageKind = 0x%02x, want 0x02", got.PageHeader.PageKind.Value)
		}
		if got.PageHeader.HeaderSize() != 12 {
			t.Fatalf("HeaderSize = %d, want 12", got.PageHeader.HeaderSize())
		}
		if got.PageHeader.CellCount.Value != 1 {
			t.Fatalf("CellCount = %d, want 1", got.PageHeader.CellCount.Value)
		}
		if got.PageHeader.CellContentAreaOffset.Value != 4078 {
			t.Fatalf("CellContentAreaOffset = %d, want 4078", got.PageHeader.CellContentAreaOffset.Value)
		}
		if got.PageHeader.RightMostPointer == nil || got.PageHeader.RightMostPointer.Value != 1850 {
			t.Fatalf("RightMostPointer = %v, want 1850", got.PageHeader.RightMostPointer)
		}
		wantPointers := []uint16{4078}
		if !equalUint16Slice(cellPointerValues(got.CellPointerArray.Pointers), wantPointers) {
			t.Fatalf("CellPointers = %v, want %v", got.CellPointerArray.Pointers, wantPointers)
		}
		if len(got.IndexInteriorCells) != int(got.PageHeader.CellCount.Value) {
			t.Fatalf("len(IndexInteriorCells) = %d, want %d", len(got.IndexInteriorCells), got.PageHeader.CellCount.Value)
		}
		if got.IndexInteriorCells[0].LeftChildPage.Value == 0 {
			t.Fatal("expected non-zero LeftChildPage")
		}
		if got.IndexInteriorCells[0].PayloadSize.Value == 0 {
			t.Fatal("expected non-zero PayloadSize")
		}
		if len(got.TableLeafCells) != 0 || len(got.TableInteriorCells) != 0 || len(got.IndexLeafCells) != 0 {
			t.Fatal("only IndexInteriorCells should be populated for interior index pages")
		}
	})
}

func TestParseBTreePageIndexLeafFromFixture(t *testing.T) {
	t.Parallel()

	page := readFixturePage(t, "superheroes.db", 7)
	got, err := parseBTreePageForTest(page, 7)
	if err != nil {
		t.Fatalf("parseBTreePage returned error: %v", err)
	}

	if got.PageHeader.PageKind.Value != 0x0a {
		t.Fatalf("PageKind = 0x%02x, want 0x0a", got.PageHeader.PageKind.Value)
	}
	if got.PageHeader.HeaderSize() != 8 {
		t.Fatalf("HeaderSize = %d, want 8", got.PageHeader.HeaderSize())
	}
	if got.PageHeader.CellCount.Value != 448 {
		t.Fatalf("CellCount = %d, want 448", got.PageHeader.CellCount.Value)
	}
	if got.PageHeader.RightMostPointer != nil {
		t.Fatal("RightMostPointer must be nil for leaf page")
	}
	if len(got.CellPointerArray.Pointers) != 448 {
		t.Fatalf("len(CellPointers) = %d, want 448", len(got.CellPointerArray.Pointers))
	}
	wantPrefix := []uint16{4090, 4084, 4078, 4072, 4066}
	if !equalUint16Slice(cellPointerValues(got.CellPointerArray.Pointers[:5]), wantPrefix) {
		t.Fatalf("CellPointers prefix = %v, want %v", got.CellPointerArray.Pointers[:5], wantPrefix)
	}
	if len(got.IndexLeafCells) != int(got.PageHeader.CellCount.Value) {
		t.Fatalf("len(IndexLeafCells) = %d, want %d", len(got.IndexLeafCells), got.PageHeader.CellCount.Value)
	}
	if got.IndexLeafCells[0].PayloadSize.Value == 0 || got.IndexLeafCells[len(got.IndexLeafCells)-1].PayloadSize.Value == 0 {
		t.Fatal("expected non-zero payload sizes for first and last index leaf cells")
	}
	if len(got.TableLeafCells) != 0 || len(got.TableInteriorCells) != 0 || len(got.IndexInteriorCells) != 0 {
		t.Fatal("only IndexLeafCells should be populated for leaf index pages")
	}
}

func TestParseBTreePageErrors(t *testing.T) {
	t.Parallel()

	t.Run("unsupported_page_kind", func(t *testing.T) {
		t.Parallel()

		page := make([]byte, 8)
		page[0] = 0x01
		_, err := parseBTreePageForTest(page, 9)
		if err == nil {
			t.Fatal("expected error for unsupported page kind, got nil")
		}
		if !strings.Contains(err.Error(), "unsupported b-tree page kind") {
			t.Fatalf("error = %q, want substring %q", err.Error(), "unsupported b-tree page kind")
		}
	})
}

func TestBTreePageBytesFor(t *testing.T) {
	t.Parallel()

	page := readFixturePage(t, "sample.db", 1)
	got, err := parseBTreePageForTest(page, 1)
	if err != nil {
		t.Fatalf("parseBTreePage returned error: %v", err)
	}

	headerBytes := got.BytesFor(got.PageHeader.Meta)
	if len(headerBytes) != got.PageHeader.Meta.Size {
		t.Fatalf("len(BytesFor(PageHeader.Meta)) = %d, want %d", len(headerBytes), got.PageHeader.Meta.Size)
	}
	if headerBytes[0] != byte(LeafTableBTreePage) {
		t.Fatalf("header first byte = 0x%02x, want 0x%02x", headerBytes[0], byte(LeafTableBTreePage))
	}
	pointerBytes := got.BytesFor(got.CellPointerArray.Meta)
	if len(pointerBytes) != got.CellPointerArray.Meta.Size {
		t.Fatalf("len(BytesFor(CellPointerArray.Meta)) = %d, want %d", len(pointerBytes), got.CellPointerArray.Meta.Size)
	}
}

func TestDecodeVarint(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   []byte
		want    uint64
		wantN   int
		wantErr string
	}{
		{
			name:  "one_byte",
			input: []byte{0x7f},
			want:  127,
			wantN: 1,
		},
		{
			name:  "multi_byte",
			input: []byte{0x81, 0x00},
			want:  128,
			wantN: 2,
		},
		{
			name:  "nine_byte",
			input: []byte{0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x01},
			want:  1,
			wantN: 9,
		},
		{
			name:    "truncated",
			input:   []byte{0x81},
			wantErr: "varint is truncated",
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, n, err := decodeVarint(tc.input)
			if tc.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tc.wantErr)
				}
				if !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("error = %q, want substring %q", err.Error(), tc.wantErr)
				}
				return
			}

			if err != nil {
				t.Fatalf("decodeVarint returned error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("decodeVarint value = %d, want %d", got, tc.want)
			}
			if n != tc.wantN {
				t.Fatalf("decodeVarint bytes = %d, want %d", n, tc.wantN)
			}
		})
	}
}

func TestCellParsersFromFixtures(t *testing.T) {
	t.Parallel()

	t.Run("table_leaf_0x0d", func(t *testing.T) {
		t.Parallel()

		page := readFixturePage(t, "sample.db", 1)
		cell, err := parseTableLeafCell(page[3983:], fixturePageSize, 1, fixturePageSize, 3983)
		if err != nil {
			t.Fatalf("parseTableLeafCell returned error: %v", err)
		}
		if cell.RowID.Value != 1 {
			t.Fatalf("RowID = %d, want 1", cell.RowID.Value)
		}
		if cell.PayloadSize.Value == 0 {
			t.Fatal("expected non-zero payload size")
		}
		if cell.Meta.StartOffset != 3983 {
			t.Fatalf("cell meta start = %d, want 3983", cell.Meta.StartOffset)
		}
	})

	t.Run("table_interior_0x05", func(t *testing.T) {
		t.Parallel()

		page := readFixturePage(t, "companies.db", 2)
		cell, err := parseTableInteriorCell(page[4089:], 2, fixturePageSize, 4089)
		if err != nil {
			t.Fatalf("parseTableInteriorCell returned error: %v", err)
		}
		if cell.LeftChildPage.Value == 0 || cell.RowID.Value == 0 {
			t.Fatalf("unexpected table interior cell: %+v", *cell)
		}
	})

	t.Run("index_leaf_0x0a", func(t *testing.T) {
		t.Parallel()

		page := readFixturePage(t, "superheroes.db", 7)
		cell, err := parseIndexLeafCell(page[4090:], fixturePageSize, 7, fixturePageSize, 4090)
		if err != nil {
			t.Fatalf("parseIndexLeafCell returned error: %v", err)
		}
		if cell.PayloadSize.Value == 0 {
			t.Fatal("expected non-zero payload size")
		}
	})

	t.Run("index_interior_0x02", func(t *testing.T) {
		t.Parallel()

		page := readFixturePage(t, "companies.db", 4)
		cell, err := parseIndexInteriorCell(page[4078:], fixturePageSize, 4, fixturePageSize, 4078)
		if err != nil {
			t.Fatalf("parseIndexInteriorCell returned error: %v", err)
		}
		if cell.LeftChildPage.Value == 0 || cell.PayloadSize.Value == 0 {
			t.Fatalf("unexpected index interior cell: %+v", *cell)
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

func cellPointerValues(got []Uint16Field) []uint16 {
	values := make([]uint16, 0, len(got))
	for _, ptr := range got {
		values = append(values, ptr.Value)
	}
	return values
}

func parseBTreePageForTest(page []byte, pageNumber uint32) (*BTreePage, error) {
	return parseBTreePage(page, pageNumber, 0)
}
