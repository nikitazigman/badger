package sqlite

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"sort"
	"testing"
)

// writeSyntheticDB writes a minimal SQLite file with a valid 100-byte header and the
// given raw page contents, then opens an Inspector over it. Pages are written first
// and the header stamped last, so page 1 content beyond offset 100 is preserved.
func writeSyntheticDB(t *testing.T, pageSize uint32, pageCount uint32, pages map[uint32][]byte) *Inspector {
	t.Helper()

	buf := make([]byte, int(pageSize)*int(pageCount))
	for num, data := range pages {
		offset := int(num-1) * int(pageSize)
		copy(buf[offset:offset+len(data)], data)
	}

	copy(buf[0:16], []byte("SQLite format 3\x00"))
	binary.BigEndian.PutUint16(buf[16:18], uint16(pageSize))
	buf[18] = 1 // write version
	buf[19] = 1 // read version
	buf[21] = 64 // max embedded payload fraction
	buf[22] = 32 // min embedded payload fraction
	buf[23] = 32 // leaf payload fraction
	binary.BigEndian.PutUint32(buf[28:32], pageCount)
	binary.BigEndian.PutUint32(buf[44:48], 1) // schema format
	binary.BigEndian.PutUint32(buf[56:60], 1) // text encoding (utf-8)

	path := filepath.Join(t.TempDir(), "synthetic.db")
	if err := os.WriteFile(path, buf, 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	inspector, err := Open(path)
	if err != nil {
		t.Fatalf("Open synthetic db returned error: %v", err)
	}
	t.Cleanup(func() { _ = inspector.Close() })
	return inspector
}

// interiorTablePage builds an interior table b-tree page (kind 0x05) with no cells
// and the given right-most pointer, with its header at headerOffset (100 for page 1).
func interiorTablePage(pageSize uint32, headerOffset int, rightMost uint32) []byte {
	p := make([]byte, pageSize)
	p[headerOffset] = byte(InteriorTableBTreePage)
	binary.BigEndian.PutUint16(p[headerOffset+5:headerOffset+7], uint16(pageSize)) // cell content area offset
	binary.BigEndian.PutUint32(p[headerOffset+8:headerOffset+12], rightMost)
	return p
}

// newCyclicInspector backs page 2 with an interior page whose right-most pointer
// references itself, so an unguarded walk would loop forever.
func newCyclicInspector(t *testing.T) *Inspector {
	t.Helper()
	return writeSyntheticDB(t, 4096, 2, map[uint32][]byte{
		2: interiorTablePage(4096, 0, 2),
	})
}

// newBadChildInspector backs page 1 with an interior page whose right-most pointer
// references an out-of-range child page that cannot be read.
func newBadChildInspector(t *testing.T) *Inspector {
	t.Helper()
	return writeSyntheticDB(t, 4096, 1, map[uint32][]byte{
		1: interiorTablePage(4096, 100, 999),
	})
}

// schemaRoot pairs a schema object's name with the root page of its b-tree.
type schemaRoot struct {
	objType string
	name    string
	root    uint32
}

// schemaRoots reads sqlite_schema and returns the (type, name, rootpage) of every
// object whose rootpage is non-zero (i.e. backed by a real b-tree).
func schemaRoots(t *testing.T, inspector *Inspector) []schemaRoot {
	t.Helper()

	metadata, err := inspector.InspectDatabaseMetadata()
	if err != nil {
		t.Fatalf("InspectDatabaseMetadata returned error: %v", err)
	}

	var roots []schemaRoot
	for _, record := range metadata.SchemaRecords {
		root, ok := record["rootpage"].(int64)
		if !ok || root == 0 {
			continue
		}
		objType, _ := record["type"].(string)
		name, _ := record["name"].(string)
		roots = append(roots, schemaRoot{objType: objType, name: name, root: uint32(root)})
	}
	return roots
}

func isSortedUnique(pages []uint32) bool {
	for idx := 1; idx < len(pages); idx++ {
		if pages[idx] <= pages[idx-1] {
			return false
		}
	}
	return true
}

func contains(pages []uint32, target uint32) bool {
	for _, p := range pages {
		if p == target {
			return true
		}
	}
	return false
}

func TestPagesForRootRootZero(t *testing.T) {
	t.Parallel()

	inspector, err := Open(fixturePath("sample.db"))
	if err != nil {
		t.Fatalf("Open returned error: %v", err)
	}
	defer inspector.Close()

	walk, err := inspector.PagesForRoot(0)
	if err != nil {
		t.Fatalf("PagesForRoot(0) returned error: %v", err)
	}
	if walk.Root != 0 {
		t.Fatalf("walk.Root = %d, want 0", walk.Root)
	}
	if len(walk.Pages) != 0 {
		t.Fatalf("len(walk.Pages) = %d, want 0", len(walk.Pages))
	}
	if len(walk.Skipped) != 0 {
		t.Fatalf("len(walk.Skipped) = %d, want 0", len(walk.Skipped))
	}
}

func TestPagesForRootHardFailingRoot(t *testing.T) {
	t.Parallel()

	inspector, err := Open(fixturePath("sample.db"))
	if err != nil {
		t.Fatalf("Open returned error: %v", err)
	}
	defer inspector.Close()

	outOfRange := inspector.dbHeader.DatabasePageCount + 1000
	if _, err := inspector.PagesForRoot(outOfRange); err == nil {
		t.Fatalf("PagesForRoot(%d) = nil error, want a hard failure", outOfRange)
	}
}

func TestPagesForRootSinglePageTable(t *testing.T) {
	t.Parallel()

	// sample.db is a tiny fixture; its tables fit on a single leaf page, so each
	// walk returns exactly [root].
	inspector, err := Open(fixturePath("sample.db"))
	if err != nil {
		t.Fatalf("Open returned error: %v", err)
	}
	defer inspector.Close()

	roots := schemaRoots(t, inspector)
	if len(roots) == 0 {
		t.Fatal("no schema roots found in sample.db")
	}

	for _, sr := range roots {
		walk, err := inspector.PagesForRoot(sr.root)
		if err != nil {
			t.Fatalf("PagesForRoot(%d) for %q returned error: %v", sr.root, sr.name, err)
		}
		if len(walk.Pages) != 1 || walk.Pages[0] != sr.root {
			t.Fatalf("PagesForRoot(%d) for %q = %v, want [%d]", sr.root, sr.name, walk.Pages, sr.root)
		}
	}
}

func TestPagesForRootAcrossFixtures(t *testing.T) {
	t.Parallel()

	fixtures := []string{"sample.db", "companies.db", "superheroes.db"}
	for _, fixture := range fixtures {
		t.Run(fixture, func(t *testing.T) {
			t.Parallel()

			inspector, err := Open(fixturePath(fixture))
			if err != nil {
				t.Fatalf("Open returned error: %v", err)
			}
			defer inspector.Close()

			pageCount := inspector.dbHeader.DatabasePageCount
			roots := schemaRoots(t, inspector)
			if len(roots) == 0 {
				t.Fatal("no schema roots found")
			}

			owner := map[uint32]uint32{} // page -> root that claimed it
			for _, sr := range roots {
				walk, err := inspector.PagesForRoot(sr.root)
				if err != nil {
					t.Fatalf("PagesForRoot(%d) for %q returned error: %v", sr.root, sr.name, err)
				}

				if len(walk.Pages) == 0 {
					t.Fatalf("PagesForRoot(%d) for %q returned no pages", sr.root, sr.name)
				}
				if !isSortedUnique(walk.Pages) {
					t.Fatalf("PagesForRoot(%d) for %q pages not sorted/unique: %v", sr.root, sr.name, walk.Pages)
				}
				if !contains(walk.Pages, sr.root) {
					t.Fatalf("PagesForRoot(%d) for %q pages %v missing root", sr.root, sr.name, walk.Pages)
				}
				if len(walk.Skipped) != 0 {
					t.Fatalf("PagesForRoot(%d) for %q unexpectedly skipped %v", sr.root, sr.name, walk.Skipped)
				}

				for _, p := range walk.Pages {
					if p < 1 || (pageCount != 0 && p > pageCount) {
						t.Fatalf("page %d for %q out of 1..%d range", p, sr.name, pageCount)
					}
					if prev, claimed := owner[p]; claimed {
						t.Fatalf("page %d claimed by both root %d and root %d", p, prev, sr.root)
					}
					owner[p] = sr.root
				}
			}
		})
	}
}

// TestPagesForRootMultiLevel asserts exact page membership for a b-tree that spans
// more than one level (interior root + leaf children), exercising both the interior
// cells and the right-most pointer.
func TestPagesForRootMultiLevel(t *testing.T) {
	t.Parallel()

	inspector, err := Open(fixturePath("superheroes.db"))
	if err != nil {
		t.Fatalf("Open returned error: %v", err)
	}
	defer inspector.Close()

	roots := schemaRoots(t, inspector)

	// Find a root whose page is interior, then independently re-derive its expected
	// page set from the page structure and compare.
	foundInterior := false
	for _, sr := range roots {
		inspection, err := inspector.InspectPage(sr.root)
		if err != nil {
			t.Fatalf("InspectPage(%d) returned error: %v", sr.root, err)
		}
		if !inspection.BTreePage.PageHeader.IsInterior() {
			continue
		}
		foundInterior = true

		expected := map[uint32]bool{sr.root: true}
		for _, cell := range inspection.BTreePage.TableInteriorCells {
			expected[cell.LeftChildPage.Value] = true
		}
		for _, cell := range inspection.BTreePage.IndexInteriorCells {
			expected[cell.LeftChildPage.Value] = true
		}
		if ptr := inspection.BTreePage.PageHeader.RightMostPointer; ptr != nil {
			expected[ptr.Value] = true
		}

		walk, err := inspector.PagesForRoot(sr.root)
		if err != nil {
			t.Fatalf("PagesForRoot(%d) returned error: %v", sr.root, err)
		}

		// Every direct child of the interior root must appear in the walk, plus the
		// root itself. (Children here are leaves in these fixtures, so this is exact.)
		want := make([]uint32, 0, len(expected))
		for p := range expected {
			want = append(want, p)
		}
		sort.Slice(want, func(a, b int) bool { return want[a] < want[b] })

		if len(walk.Pages) != len(want) {
			t.Fatalf("PagesForRoot(%d) = %v, want %v", sr.root, walk.Pages, want)
		}
		for idx := range want {
			if walk.Pages[idx] != want[idx] {
				t.Fatalf("PagesForRoot(%d) = %v, want %v", sr.root, walk.Pages, want)
			}
		}
	}

	if !foundInterior {
		t.Skip("no multi-level b-tree present in superheroes.db")
	}
}

func TestPagesForRootCycleGuard(t *testing.T) {
	t.Parallel()

	// Page 2 is an interior page whose right-most pointer references itself. Without
	// the visited guard this would recurse forever; with it, the walk terminates and
	// page 2 is counted exactly once.
	inspector := newCyclicInspector(t)

	walk, err := inspector.PagesForRoot(2)
	if err != nil {
		t.Fatalf("PagesForRoot returned error: %v", err)
	}
	if count := countOccurrences(walk.Pages, 2); count != 1 {
		t.Fatalf("page 2 counted %d times, want 1", count)
	}
}

func TestPagesForRootSkipsBadChild(t *testing.T) {
	t.Parallel()

	// An interior root pointing at a child page that is out of range must be
	// recorded in Skipped and not abort the walk.
	inspector := newBadChildInspector(t)

	walk, err := inspector.PagesForRoot(1)
	if err != nil {
		t.Fatalf("PagesForRoot returned error: %v", err)
	}
	if !contains(walk.Pages, 1) {
		t.Fatalf("walk.Pages %v missing root page 1", walk.Pages)
	}
	if len(walk.Skipped) != 1 {
		t.Fatalf("len(walk.Skipped) = %d, want 1 (%v)", len(walk.Skipped), walk.Skipped)
	}
	if walk.Skipped[0].Parent != 1 {
		t.Fatalf("Skipped[0].Parent = %d, want 1", walk.Skipped[0].Parent)
	}
}

func countOccurrences(pages []uint32, target uint32) int {
	count := 0
	for _, p := range pages {
		if p == target {
			count++
		}
	}
	return count
}
