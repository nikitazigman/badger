package tui

import (
	"errors"
	"path/filepath"
	"sort"
	"testing"

	"github.com/nikitazigman/badger/internal/storage"
)

func fixturePath(name string) string {
	return filepath.Join("..", "..", "fixtures", name)
}

func newFixtureModel(t *testing.T, fixture string) (model, storage.Database) {
	t.Helper()

	db, err := storage.Open(fixturePath(fixture))
	if err != nil {
		t.Fatalf("Open(%s) returned error: %v", fixture, err)
	}
	t.Cleanup(func() { _ = db.Close() })

	overview, err := db.Overview()
	if err != nil {
		t.Fatalf("Overview returned error: %v", err)
	}

	m, err := newModel(db, overview)
	if err != nil {
		t.Fatalf("newModel returned error: %v", err)
	}
	return m, db
}

// distinctSchemaIDs re-derives the expected b-tree id set straight from the view model so
// the test does not depend on collectBTreeRoots' own logic.
func distinctSchemaIDs(db databaseViewModel) []storage.BTreeID {
	seen := map[storage.BTreeID]bool{}
	ids := []storage.BTreeID{}
	for _, obj := range append(append([]schemaObjectViewModel{}, db.Tables...), db.Indexes...) {
		if obj.RootPage == 0 || obj.ID == "" || seen[obj.ID] {
			continue
		}
		seen[obj.ID] = true
		ids = append(ids, obj.ID)
	}
	sort.Slice(ids, func(a, b int) bool { return ids[a] < ids[b] })
	return ids
}

func TestCollectBTreeRoots(t *testing.T) {
	t.Parallel()

	for _, fixture := range []string{"sample.db", "companies.db", "superheroes.db"} {
		fixture := fixture
		t.Run(fixture, func(t *testing.T) {
			t.Parallel()

			m, _ := newFixtureModel(t, fixture)
			got := collectBTreeRoots(m.db)

			seen := map[storage.BTreeID]bool{}
			for _, id := range got {
				if id == "" {
					t.Fatalf("collectBTreeRoots included a zero root: %v", got)
				}
				if seen[id] {
					t.Fatalf("collectBTreeRoots produced a duplicate id %s: %v", id, got)
				}
				seen[id] = true
			}

			gotSorted := append([]storage.BTreeID{}, got...)
			sort.Slice(gotSorted, func(a, b int) bool { return gotSorted[a] < gotSorted[b] })
			want := distinctSchemaIDs(m.db)
			if !equalBTreeIDs(gotSorted, want) {
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
	if want := len(distinctSchemaIDs(m.db)); m.indexTotal != want {
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

	m, db := newFixtureModel(t, "companies.db")
	if len(m.indexRoots) == 0 {
		t.Fatal("no roots to test")
	}
	id := m.indexRoots[0]

	msg := indexBTreeCmd(db, id)()
	indexed, ok := msg.(btreeIndexedMsg)
	if !ok {
		t.Fatalf("indexBTreeCmd produced %T, want btreeIndexedMsg", msg)
	}
	if indexed.err != nil {
		t.Fatalf("indexBTreeCmd(%s) err = %v, want nil", id, indexed.err)
	}
	if indexed.id != id {
		t.Fatalf("msg.id = %s, want %s", indexed.id, id)
	}

	want, err := db.PagesForBTree(id)
	if err != nil {
		t.Fatalf("PagesForBTree(%s) returned error: %v", id, err)
	}
	if !equalPageRefs(indexed.pages, want) {
		t.Fatalf("msg.pages = %v, want %v", indexed.pages, want)
	}
}

func TestUpdateReductionEndToEnd(t *testing.T) {
	t.Parallel()

	m, db := newFixtureModel(t, "companies.db")

	var current model = m
	for _, id := range m.indexRoots {
		msg := indexBTreeCmd(db, id)()
		next, _ := current.Update(msg)
		current = next.(model)
	}

	if current.indexPending != 0 {
		t.Fatalf("indexPending = %d after all messages, want 0", current.indexPending)
	}
	if len(current.indexErrors) != 0 {
		t.Fatalf("indexErrors = %v, want empty", current.indexErrors)
	}
	for _, id := range m.indexRoots {
		got, ok := current.pageIndex[id]
		if !ok {
			t.Fatalf("pageIndex missing id %s after indexing", id)
		}
		want, err := db.PagesForBTree(id)
		if err != nil {
			t.Fatalf("PagesForBTree(%s) returned error: %v", id, err)
		}
		if !equalPageRefs(got, want) {
			t.Fatalf("pageIndex[%s] = %v, want %v", id, got, want)
		}
	}
}

func TestUpdateReductionHardFailure(t *testing.T) {
	t.Parallel()

	m, _ := newFixtureModel(t, "companies.db")
	pendingBefore := m.indexPending
	const badID storage.BTreeID = "table:missing"

	next, _ := m.Update(btreeIndexedMsg{id: badID, err: errors.New("boom")})
	current := next.(model)

	if reason, ok := current.indexErrors[badID]; !ok || reason != "boom" {
		t.Fatalf("indexErrors[%s] = %q (ok=%v), want %q", badID, reason, ok, "boom")
	}
	if _, ok := current.pageIndex[badID]; ok {
		t.Fatalf("failed id %s must not be present in pageIndex", badID)
	}
	if current.indexPending != pendingBefore-1 {
		t.Fatalf("indexPending = %d, want %d after one failure", current.indexPending, pendingBefore-1)
	}
}

func equalBTreeIDs(got []storage.BTreeID, want []storage.BTreeID) bool {
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

func equalPageRefs(got []storage.PageRef, want []storage.PageRef) bool {
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
