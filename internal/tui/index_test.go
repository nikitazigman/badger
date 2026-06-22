package tui

import (
	"errors"
	"path/filepath"
	"sort"
	"testing"

	"github.com/nikitazigman/badger/internal/sqlite"
)

func fixturePath(name string) string {
	return filepath.Join("..", "..", "fixtures", name)
}

func newFixtureModel(t *testing.T, fixture string) (model, *sqlite.Inspector) {
	t.Helper()

	inspector, err := sqlite.Open(fixturePath(fixture))
	if err != nil {
		t.Fatalf("Open(%s) returned error: %v", fixture, err)
	}
	t.Cleanup(func() { _ = inspector.Close() })

	metadata, err := inspector.InspectDatabaseMetadata()
	if err != nil {
		t.Fatalf("InspectDatabaseMetadata returned error: %v", err)
	}

	m, err := newModel(inspector, metadata)
	if err != nil {
		t.Fatalf("newModel returned error: %v", err)
	}
	return m, inspector
}

// distinctSchemaRoots re-derives the expected root set straight from the view model so
// the test does not depend on collectBTreeRoots' own logic.
func distinctSchemaRoots(db databaseViewModel) []uint32 {
	seen := map[uint32]bool{}
	roots := []uint32{}
	for _, obj := range append(append([]schemaObjectViewModel{}, db.Tables...), db.Indexes...) {
		if obj.RootPage == 0 || seen[obj.RootPage] {
			continue
		}
		seen[obj.RootPage] = true
		roots = append(roots, obj.RootPage)
	}
	sort.Slice(roots, func(a, b int) bool { return roots[a] < roots[b] })
	return roots
}

func TestCollectBTreeRoots(t *testing.T) {
	t.Parallel()

	for _, fixture := range []string{"sample.db", "companies.db", "superheroes.db"} {
		fixture := fixture
		t.Run(fixture, func(t *testing.T) {
			t.Parallel()

			m, _ := newFixtureModel(t, fixture)
			got := collectBTreeRoots(m.db)

			seen := map[uint32]bool{}
			for _, r := range got {
				if r == 0 {
					t.Fatalf("collectBTreeRoots included a zero root: %v", got)
				}
				if seen[r] {
					t.Fatalf("collectBTreeRoots produced a duplicate root %d: %v", r, got)
				}
				seen[r] = true
			}

			gotSorted := append([]uint32{}, got...)
			sort.Slice(gotSorted, func(a, b int) bool { return gotSorted[a] < gotSorted[b] })
			want := distinctSchemaRoots(m.db)
			if !equalRoots(gotSorted, want) {
				t.Fatalf("collectBTreeRoots = %v, want %v", gotSorted, want)
			}
		})
	}
}

func TestInitFanOut(t *testing.T) {
	t.Parallel()

	m, _ := newFixtureModel(t, "companies.db")
	if len(m.indexRoots) != m.indexTotal {
		t.Fatalf("len(indexRoots)=%d != indexTotal=%d", len(m.indexRoots), m.indexTotal)
	}
	if m.indexPending != m.indexTotal {
		t.Fatalf("indexPending=%d != indexTotal=%d at launch", m.indexPending, m.indexTotal)
	}
	if want := len(distinctSchemaRoots(m.db)); m.indexTotal != want {
		t.Fatalf("indexTotal=%d, want %d distinct non-zero schema roots", m.indexTotal, want)
	}
	if cmd := m.Init(); cmd == nil && m.indexTotal > 0 {
		t.Fatal("Init returned nil despite having roots to index")
	}
}

func TestInitNoRoots(t *testing.T) {
	t.Parallel()

	m := model{indexRoots: nil}
	if cmd := m.Init(); cmd != nil {
		t.Fatal("Init with no roots returned a non-nil command")
	}
}

func TestIndexBTreeCmd(t *testing.T) {
	t.Parallel()

	m, inspector := newFixtureModel(t, "companies.db")
	if len(m.indexRoots) == 0 {
		t.Fatal("no roots to test")
	}
	root := m.indexRoots[0]

	msg := indexBTreeCmd(inspector, root)()
	indexed, ok := msg.(btreeIndexedMsg)
	if !ok {
		t.Fatalf("indexBTreeCmd produced %T, want btreeIndexedMsg", msg)
	}
	if indexed.err != nil {
		t.Fatalf("indexBTreeCmd(%d) err = %v, want nil", root, indexed.err)
	}
	if indexed.root != root {
		t.Fatalf("msg.root = %d, want %d", indexed.root, root)
	}

	want, err := inspector.PagesForRoot(root)
	if err != nil {
		t.Fatalf("PagesForRoot(%d) returned error: %v", root, err)
	}
	if !equalRoots(indexed.walk.Pages, want.Pages) {
		t.Fatalf("msg.walk.Pages = %v, want %v", indexed.walk.Pages, want.Pages)
	}
}

func TestUpdateReductionEndToEnd(t *testing.T) {
	t.Parallel()

	m, inspector := newFixtureModel(t, "companies.db")

	var current model = m
	for _, root := range m.indexRoots {
		msg := indexBTreeCmd(inspector, root)()
		next, _ := current.Update(msg)
		current = next.(model)
	}

	if current.indexPending != 0 {
		t.Fatalf("indexPending = %d after all messages, want 0", current.indexPending)
	}
	if len(current.indexErrors) != 0 {
		t.Fatalf("indexErrors = %v, want empty", current.indexErrors)
	}
	for _, root := range m.indexRoots {
		got, ok := current.pageIndex.Walk(root)
		if !ok {
			t.Fatalf("pageIndex missing root %d after indexing", root)
		}
		want, err := inspector.PagesForRoot(root)
		if err != nil {
			t.Fatalf("PagesForRoot(%d) returned error: %v", root, err)
		}
		if !equalRoots(got.Pages, want.Pages) {
			t.Fatalf("pageIndex.Walk(%d).Pages = %v, want %v", root, got.Pages, want.Pages)
		}
	}
}

func TestUpdateReductionHardFailure(t *testing.T) {
	t.Parallel()

	m, _ := newFixtureModel(t, "companies.db")
	pendingBefore := m.indexPending
	const badRoot uint32 = 999999

	next, _ := m.Update(btreeIndexedMsg{root: badRoot, err: errors.New("boom")})
	current := next.(model)

	if reason, ok := current.indexErrors[badRoot]; !ok || reason != "boom" {
		t.Fatalf("indexErrors[%d] = %q (ok=%v), want %q", badRoot, reason, ok, "boom")
	}
	if _, ok := current.pageIndex.Walk(badRoot); ok {
		t.Fatalf("failed root %d must not be present in pageIndex", badRoot)
	}
	if current.indexPending != pendingBefore-1 {
		t.Fatalf("indexPending = %d, want %d after one failure", current.indexPending, pendingBefore-1)
	}
}

func equalRoots(got []uint32, want []uint32) bool {
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
