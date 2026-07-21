package tui

import (
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/nikitazigman/badger/internal/storage"
)

// indexAll feeds every root's btreeIndexedMsg through Update so the model's pageIndex is
// fully populated, mirroring what Init's fan-out produces at launch.
func indexAll(t *testing.T, m model, db storage.Database) model {
	t.Helper()
	current := m
	for _, id := range m.indexRoots {
		next, _ := current.Update(indexBTreeCmd(db, id)())
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

func objectByPath(t *testing.T, db databaseViewModel, path string) schemaObjectViewModel {
	t.Helper()
	for _, obj := range append(append([]schemaObjectViewModel{}, db.Tables...), db.Indexes...) {
		if obj.Rows != nil && fieldValue(*obj.Rows, "Path") == path {
			return obj
		}
	}
	t.Fatalf("schema object path %q not found", path)
	return schemaObjectViewModel{}
}

func keyMsg(s string) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

func TestApplyFilterIndexed(t *testing.T) {
	t.Parallel()

	m, db := newFixtureModel(t, "companies.db")
	m = indexAll(t, m, db)
	companies := objectByName(t, m.db, "companies")

	m.applyFilter(companies)

	if !m.isFiltered() {
		t.Fatal("applyFilter on an indexed object did not set a filter")
	}
	pages, ok := m.filteredPages()
	if !ok {
		t.Fatal("filteredPages returned ok=false after applying a filter")
	}
	want, err := db.PagesForBTree(companies.ID)
	if err != nil {
		t.Fatalf("PagesForBTree(%s) returned error: %v", companies.ID, err)
	}
	if !equalPageRefs(pages, want) {
		t.Fatalf("filteredPages = %v, want PagesForBTree(id) = %v", pages, want)
	}
}

func TestApplyFilterSQLiteSchema(t *testing.T) {
	t.Parallel()

	m, db := newFixtureModel(t, "companies.db")
	m = indexAll(t, m, db)
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
	want, err := db.PagesForBTree(catalog.ID)
	if err != nil {
		t.Fatalf("PagesForBTree(%s) returned error: %v", catalog.ID, err)
	}
	if !equalPageRefs(pages, want) {
		t.Fatalf("filteredPages = %v, want sqlite_schema pages %v", pages, want)
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

func TestApplyFilterInlineBboltBucketShowsParentPage(t *testing.T) {
	t.Parallel()

	m, db := newFixtureModel(t, filepath.Join("bbolt", "nested_inline.db"))
	m = indexAll(t, m, db)
	inline := objectByPath(t, m.db, "alpha/inline_1")

	if inline.Kind != storage.BTreeInlineBucket {
		t.Fatalf("inline kind = %q, want %q", inline.Kind, storage.BTreeInlineBucket)
	}
	if inline.RootPage != 0 {
		t.Fatalf("inline root page = %d, want 0", inline.RootPage)
	}

	m.applyFilter(inline)

	if !m.isFiltered() {
		t.Fatal("applyFilter on inline bucket did not set a filter")
	}
	pages, ok := m.filteredPages()
	if !ok {
		t.Fatal("filteredPages returned ok=false after applying inline bucket filter")
	}
	want, err := db.PagesForBTree(inline.ID)
	if err != nil {
		t.Fatalf("PagesForBTree(%s) returned error: %v", inline.ID, err)
	}
	if len(want) != 1 {
		t.Fatalf("PagesForBTree(%s) returned %d pages, want parent page only", inline.ID, len(want))
	}
	if !equalPageRefs(pages, want) {
		t.Fatalf("filteredPages = %v, want inline parent page %v", pages, want)
	}
}

func TestFilterSwitchesBetweenDuplicateBboltInlineBucketNames(t *testing.T) {
	t.Parallel()

	m, db := newFixtureModel(t, filepath.Join("bbolt", "nested_inline.db"))
	m = indexAll(t, m, db)
	alpha := objectByPath(t, m.db, "alpha/inline_1")
	beta := objectByPath(t, m.db, "beta/inline_1")

	if alpha.ID == beta.ID {
		t.Fatal("setup: duplicate inline buckets have the same ID")
	}
	if alpha.Name != beta.Name || alpha.RootPage != beta.RootPage {
		t.Fatalf("setup: buckets are not ambiguous by name/root: alpha=%+v beta=%+v", alpha, beta)
	}

	m.applyFilter(alpha)
	if !m.objectIsFilterSource(alpha) {
		t.Fatal("alpha inline bucket is not the active filter source")
	}
	if m.objectIsFilterSource(beta) {
		t.Fatal("beta inline bucket was treated as the same filter source as alpha")
	}

	m.selectedIndex = indexOfBTreeRow(m.navItems, beta)
	next, _ := m.Update(keyMsg("f"))
	switched := next.(model)
	if !switched.isFiltered() {
		t.Fatal("f on duplicate-name inline bucket cleared the filter; want switch")
	}
	if switched.activeFilter.object.ID != beta.ID {
		t.Fatalf("active filter ID = %q, want %q", switched.activeFilter.object.ID, beta.ID)
	}
}

func TestApplyFilterHardFailed(t *testing.T) {
	t.Parallel()

	m, _ := newFixtureModel(t, "companies.db")
	companies := objectByName(t, m.db, "companies")
	m.indexErrors[companies.ID] = "root unreadable"

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
	if equalPageRefs(first, second) {
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

func TestReadyFilterStillApplies(t *testing.T) {
	t.Parallel()

	m, _ := newFixtureModel(t, "companies.db")
	companies := objectByName(t, m.db, "companies")
	m.pageIndex[companies.ID] = []storage.PageRef{{ID: companies.RootPage}, {ID: 9}}

	m.applyFilter(companies)
	if !m.isFiltered() {
		t.Fatal("a ready page set must apply a filter")
	}
	footer := m.filterToken()
	if strings.Contains(footer, "skipped") || strings.Contains(footer, "unreadable") {
		t.Fatalf("footer = %q, want no skipped diagnostics from storage filter API", footer)
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
	var gotPages []storage.PageRef
	for _, item := range m.navItems {
		if item.kind == navPage {
			gotPages = append(gotPages, storage.PageRef{ID: item.pageNumber})
		}
	}
	if !equalPageRefs(gotPages, want) {
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
	if uint64(count) != m.db.PageCount {
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
	index := objectByName(t, m.db, "idx_companies_country")
	m.selectedIndex = indexOfBTreeRow(m.navItems, companies)

	next, _ := m.Update(keyMsg("f"))
	filtered := next.(model)
	if !filtered.isFiltered() {
		t.Fatal("KeyMsg{f} on a B-TREES row did not apply a filter")
	}
	if !filtered.objectIsFilterSource(companies) {
		t.Fatalf("active filter source = %+v, want companies", filtered.activeFilter)
	}

	next, _ = filtered.Update(keyMsg("f"))
	cleared := next.(model)
	if cleared.isFiltered() {
		t.Fatal("KeyMsg{f} on the active source row did not clear the filter")
	}
	if cleared.selectedIndex != indexOfBTreeRow(cleared.navItems, companies) {
		t.Fatalf("clearing with f moved selectedIndex to %d, want companies row", cleared.selectedIndex)
	}

	m.selectedIndex = indexOfBTreeRow(m.navItems, companies)
	next, _ = m.Update(keyMsg("f"))
	filtered = next.(model)
	filtered.selectedIndex = indexOfBTreeRow(filtered.navItems, index)

	next, _ = filtered.Update(keyMsg("f"))
	switched := next.(model)
	if !switched.isFiltered() {
		t.Fatal("KeyMsg{f} on another B-TREES row cleared the filter; want switch")
	}
	if !switched.objectIsFilterSource(index) {
		t.Fatalf("active filter source = %+v, want idx_companies_country", switched.activeFilter)
	}
}

func TestFilterKeyNoOpOnNonBTreeRow(t *testing.T) {
	t.Parallel()

	m, inspector := newFixtureModel(t, "companies.db")
	m = indexAll(t, m, inspector)
	m.selectFirstKind(navPage) // not a B-TREES object

	next, _ := m.Update(keyMsg("f"))
	got := next.(model)
	if got.isFiltered() {
		t.Fatal("f on a page row must be a no-op")
	}

	companies := objectByName(t, got.db, "companies")
	got.applyFilter(companies)
	got.selectFirstKind(navPage) // not a B-TREES object

	next, _ = got.Update(keyMsg("f"))
	stillFiltered := next.(model)
	if !stillFiltered.objectIsFilterSource(companies) {
		t.Fatal("f on a page row changed the active filter")
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
	if !strings.Contains(view, "f clear/switch") {
		t.Fatal("View missing 'f clear/switch' in the filter footer")
	}
	m.focusedPane = explorerPane
	if strings.Contains(m.footerLine(), "f clear/switch") || strings.Contains(m.footerLine(), "f filter") {
		t.Fatalf("footer shows filter hint outside B-TREES: %q", m.footerLine())
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
