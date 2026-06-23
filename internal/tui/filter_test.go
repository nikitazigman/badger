package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/nikitazigman/badger/internal/sqlite"
)

// indexAll feeds every root's btreeIndexedMsg through Update so the model's pageIndex is
// fully populated, mirroring what Init's fan-out produces at launch.
func indexAll(t *testing.T, m model, inspector *sqlite.Inspector) model {
	t.Helper()
	current := m
	for _, root := range m.indexRoots {
		next, _ := current.Update(indexBTreeCmd(inspector, root)())
		current = next.(model)
	}
	return current
}

func objectByName(t *testing.T, db databaseViewModel, name string) schemaObjectViewModel {
	t.Helper()
	for _, obj := range append(append([]schemaObjectViewModel{}, db.Tables...), db.Indexes...) {
		if obj.Name == name {
			return obj
		}
	}
	t.Fatalf("schema object %q not found", name)
	return schemaObjectViewModel{}
}

func keyMsg(s string) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

func TestApplyFilterIndexed(t *testing.T) {
	t.Parallel()

	m, inspector := newFixtureModel(t, "companies.db")
	m = indexAll(t, m, inspector)
	companies := objectByName(t, m.db, "companies")

	m.applyFilter(companies)

	if !m.isFiltered() {
		t.Fatal("applyFilter on an indexed object did not set a filter")
	}
	pages, ok := m.filteredPages()
	if !ok {
		t.Fatal("filteredPages returned ok=false after applying a filter")
	}
	walk, err := inspector.PagesForRoot(companies.RootPage)
	if err != nil {
		t.Fatalf("PagesForRoot(%d) returned error: %v", companies.RootPage, err)
	}
	if !equalRoots(pages, walk.Pages) {
		t.Fatalf("filteredPages = %v, want PagesForRoot(root).Pages = %v", pages, walk.Pages)
	}
}

func TestApplyFilterSQLiteSchema(t *testing.T) {
	t.Parallel()

	m, inspector := newFixtureModel(t, "companies.db")
	m = indexAll(t, m, inspector)
	catalog := objectByName(t, m.db, "sqlite_schema")

	if !catalog.IsSystem {
		t.Fatal("sqlite_schema row is not marked as a system catalog")
	}
	if catalog.RootPage != 1 {
		t.Fatalf("sqlite_schema root page = %d, want 1", catalog.RootPage)
	}

	m.applyFilter(catalog)

	if !m.isFiltered() {
		t.Fatal("applyFilter on sqlite_schema did not set a filter")
	}
	pages, ok := m.filteredPages()
	if !ok {
		t.Fatal("filteredPages returned ok=false after applying sqlite_schema filter")
	}
	walk, err := inspector.PagesForRoot(1)
	if err != nil {
		t.Fatalf("PagesForRoot(1) returned error: %v", err)
	}
	if !equalRoots(pages, walk.Pages) {
		t.Fatalf("filteredPages = %v, want sqlite_schema walk pages %v", pages, walk.Pages)
	}
}

func TestApplyFilterVirtualTable(t *testing.T) {
	t.Parallel()

	m, inspector := newFixtureModel(t, "companies.db")
	m = indexAll(t, m, inspector)

	// No fixture ships a virtual table, so inject one (RootPage == 0) into the view model.
	virtual := schemaObjectViewModel{Type: "table", Name: "fts_docs", RootPage: 0}
	m.db.Tables = append(m.db.Tables, virtual)
	m.navItems = buildNavItems(m.db, nil, nil)

	m.applyFilter(virtual)

	if !m.isFiltered() {
		t.Fatal("a virtual table must apply as a valid (empty) filter, not be rejected")
	}
	pages, ok := m.filteredPages()
	if !ok || pages == nil || len(pages) != 0 {
		t.Fatalf("filteredPages = (%v, %v), want ([]uint32{}, true)", pages, ok)
	}
}

func TestApplyFilterHardFailed(t *testing.T) {
	t.Parallel()

	m, _ := newFixtureModel(t, "companies.db")
	companies := objectByName(t, m.db, "companies")
	m.indexErrors[companies.RootPage] = "root unreadable"

	m.applyFilter(companies)

	if m.isFiltered() {
		t.Fatal("a hard-failed root must not apply a filter")
	}
	if !strings.Contains(m.status, "can't filter") {
		t.Fatalf("status = %q, want a can't-filter message", m.status)
	}
}

func TestApplyFilterNotYetIndexed(t *testing.T) {
	t.Parallel()

	// Fresh model: nothing has been walked yet, no recorded errors.
	m, _ := newFixtureModel(t, "companies.db")
	companies := objectByName(t, m.db, "companies")

	m.applyFilter(companies)

	if m.isFiltered() {
		t.Fatal("a not-yet-indexed root must not apply a filter")
	}
	if !strings.Contains(m.status, "still indexing") {
		t.Fatalf("status = %q, want a still-indexing message", m.status)
	}
}

func TestSwitchAndClearFilter(t *testing.T) {
	t.Parallel()

	m, inspector := newFixtureModel(t, "companies.db")
	m = indexAll(t, m, inspector)
	companies := objectByName(t, m.db, "companies")
	index := objectByName(t, m.db, "idx_companies_country")

	m.applyFilter(companies)
	first, _ := m.filteredPages()

	m.applyFilter(index) // a second filter replaces the first
	second, ok := m.filteredPages()
	if !ok {
		t.Fatal("filter not active after switching")
	}
	if equalRoots(first, second) {
		t.Fatal("switching the filter did not change the page set")
	}
	if m.activeFilter.object.Name != "idx_companies_country" {
		t.Fatalf("active filter object = %q, want idx_companies_country", m.activeFilter.object.Name)
	}

	m.clearFilter()
	if pages, ok := m.filteredPages(); ok || pages != nil {
		t.Fatalf("after clearFilter filteredPages = (%v, %v), want (nil, false)", pages, ok)
	}
}

func TestDegradedFilterStillApplies(t *testing.T) {
	t.Parallel()

	m, _ := newFixtureModel(t, "companies.db")
	companies := objectByName(t, m.db, "companies")
	// Synthesize a degraded walk: a couple of readable pages plus one skipped child.
	m.pageIndex.Set(sqlite.PageWalk{
		Root:    companies.RootPage,
		Pages:   []uint32{companies.RootPage, 9},
		Skipped: []sqlite.SkippedPage{{Page: 774, Parent: companies.RootPage, Reason: "short read"}},
	})

	m.applyFilter(companies)
	if !m.isFiltered() {
		t.Fatal("a degraded walk must still apply a filter")
	}
	footer := m.filterToken()
	if !strings.Contains(footer, "1 skipped") || !strings.Contains(footer, "⚠ page 774 unreadable") {
		t.Fatalf("footer = %q, want skipped + unreadable diagnostics", footer)
	}
}

func TestNavRebuildOnApplyAndClear(t *testing.T) {
	t.Parallel()

	m, inspector := newFixtureModel(t, "companies.db")
	m = indexAll(t, m, inspector)
	companies := objectByName(t, m.db, "companies")

	m.applyFilter(companies)

	// PAGES rows must equal exactly the filtered page set.
	want, _ := m.filteredPages()
	var gotPages []uint32
	for _, item := range m.navItems {
		if item.kind == navPage {
			gotPages = append(gotPages, item.pageNumber)
		}
	}
	if !equalRoots(gotPages, want) {
		t.Fatalf("filtered PAGES rows = %v, want %v", gotPages, want)
	}
	// Cursor sits on the companies row.
	if m.selectedItem().schema == nil || m.selectedItem().schema.Name != "companies" {
		t.Fatalf("cursor not on companies row after applyFilter; on %q", m.selectedItem().title)
	}
	srcIndex := m.selectedIndex

	m.clearFilter()

	var count int
	for _, item := range m.navItems {
		if item.kind == navPage {
			count++
		}
	}
	if uint32(count) != m.db.PageCount {
		t.Fatalf("after clearFilter PAGES count = %d, want full %d", count, m.db.PageCount)
	}
	if m.selectedIndex != srcIndex {
		t.Fatalf("clearFilter moved cursor: selectedIndex = %d, want %d", m.selectedIndex, srcIndex)
	}
}

func TestFilterKeysViaUpdate(t *testing.T) {
	t.Parallel()

	m, inspector := newFixtureModel(t, "companies.db")
	m = indexAll(t, m, inspector)
	companies := objectByName(t, m.db, "companies")
	m.selectedIndex = indexOfBTreeRow(m.navItems, companies)

	next, _ := m.Update(keyMsg("f"))
	filtered := next.(model)
	if !filtered.isFiltered() {
		t.Fatal("KeyMsg{f} on a B-TREES row did not apply a filter")
	}

	next, _ = filtered.Update(keyMsg("F"))
	cleared := next.(model)
	if cleared.isFiltered() {
		t.Fatal("KeyMsg{F} did not clear the filter")
	}
}

func TestFilterKeyNoOpOnNonBTreeRow(t *testing.T) {
	t.Parallel()

	m, inspector := newFixtureModel(t, "companies.db")
	m = indexAll(t, m, inspector)
	m.selectFirstKind(navPage) // not a B-TREES object

	next, _ := m.Update(keyMsg("f"))
	if next.(model).isFiltered() {
		t.Fatal("f on a page row must be a no-op")
	}
}

func TestFilterRenderFooterAndMarkers(t *testing.T) {
	t.Parallel()

	m, inspector := newFixtureModel(t, "companies.db")
	m = indexAll(t, m, inspector)
	companies := objectByName(t, m.db, "companies")
	m.applyFilter(companies) // cursor lands on the companies row

	view := m.View()
	if !strings.Contains(view, "⦿ filtered: ▦ companies") {
		t.Fatalf("View missing filter footer token; got footer build %q", m.filterToken())
	}
	if !strings.Contains(view, "F clear") {
		t.Fatal("View missing 'F clear' in the filter footer")
	}
	// The cursor is on the source row → a single ▶ marker, never '> ▶'.
	if strings.Contains(view, "> ▶") || strings.Contains(view, "▶ >") {
		t.Fatal("source row shows a double marker; want a single ▶")
	}
	if !strings.Contains(view, "▶ ▦ companies") {
		t.Fatal("source row missing the solid ▶ marker")
	}
}

func TestFilterRenderVirtualTable(t *testing.T) {
	t.Parallel()

	m, inspector := newFixtureModel(t, "companies.db")
	m = indexAll(t, m, inspector)
	virtual := schemaObjectViewModel{Type: "table", Name: "fts_docs", RootPage: 0}
	m.db.Tables = append(m.db.Tables, virtual)
	m.navItems = buildNavItems(m.db, nil, nil)

	m.applyFilter(virtual)

	view := m.View()
	if !strings.Contains(view, "⦿ filtered: ⊞ fts_docs (0 pg)") {
		t.Fatalf("View missing virtual-table footer; footer = %q", m.filterToken())
	}
	if !strings.Contains(view, "Pages:     0 (filtered)") {
		t.Fatal("summary missing 'Pages: 0 (filtered)' for a virtual table")
	}
}
