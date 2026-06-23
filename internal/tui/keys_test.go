package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/nikitazigman/badger/internal/sqlite"
)

// firstIndexOfKind returns the navItems index of the first row of the given kind, or -1.
func firstIndexOfKind(items []navItem, kind navKind) int {
	for idx, item := range items {
		if item.kind == kind {
			return idx
		}
	}
	return -1
}

func pageLoadedFromCmd(t *testing.T, cmd tea.Cmd) pageLoadedMsg {
	t.Helper()
	if cmd == nil {
		t.Fatal("cmd is nil")
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		if len(batch) == 0 {
			t.Fatal("batch command has no children")
		}
		msg = batch[0]()
	}
	loaded, ok := msg.(pageLoadedMsg)
	if !ok {
		t.Fatalf("cmd produced %T, want pageLoadedMsg", msg)
	}
	return loaded
}

func TestSectionJumpsAutoActivate(t *testing.T) {
	t.Parallel()

	m, inspector := newFixtureModel(t, "companies.db")
	m = indexAll(t, m, inspector)

	// Start from an arbitrary selection outside B-TREES.
	m.selectFirstKind(navPage)
	m.active = contentTarget{kind: navPage}

	// 1 → first B-TREES row.
	next, _ := m.Update(keyMsg("1"))
	got := next.(model)
	if want := got.indexOfFirstBTree(); got.selectedIndex != want {
		t.Fatalf("1 selected index %d, want first B-TREES row %d", got.selectedIndex, want)
	}
	if got.active.kind != got.selectedItem().kind || got.active.schemaName != got.selectedItem().schema.Name {
		t.Fatalf("1 active target = %+v, selected item = %+v", got.active, got.selectedItem())
	}

	// 2 → first PAGES row.
	next, cmd := got.Update(keyMsg("2"))
	got = next.(model)
	if want := firstIndexOfKind(got.navItems, navPage); got.selectedIndex != want {
		t.Fatalf("2 selected index %d, want first navPage row %d", got.selectedIndex, want)
	}
	if got.active.kind != navPage || got.active.pageNumber != got.selectedItem().pageNumber {
		t.Fatalf("2 active target = %+v, selected item = %+v", got.active, got.selectedItem())
	}
	if !got.loading {
		t.Fatal("2 did not start loading the selected page")
	}
	if got.loadingVisible {
		t.Fatal("2 showed loading immediately; loading indicator must be delayed")
	}
	if got.status != "" {
		t.Fatalf("2 status = %q, want no loading status before delay", got.status)
	}
	if cmd == nil {
		t.Fatal("2 on a page row returned nil cmd, want batched load/delay command")
	}
	msg := pageLoadedFromCmd(t, cmd)
	next, _ = got.Update(msg)
	loaded := next.(model)
	if loaded.loading {
		t.Fatal("page load message left loading=true")
	}
	if loaded.loadingVisible {
		t.Fatal("fast page load left loadingVisible=true")
	}
	if loaded.status != "" {
		t.Fatalf("fast page load changed footer status to %q", loaded.status)
	}
	if loaded.currentPage == nil || loaded.currentPage.PageNumber != got.active.pageNumber {
		t.Fatalf("loaded current page = %+v, want page %d", loaded.currentPage, got.active.pageNumber)
	}
	if len(loaded.pageRows) == 0 {
		t.Fatal("loaded page did not build page rows")
	}

	next, _ = loaded.Update(loadingDelayElapsedMsg{pageNumber: got.active.pageNumber})
	afterTimer := next.(model)
	if afterTimer.loadingVisible {
		t.Fatal("delay timer showed loading after the page had already loaded")
	}
	if afterTimer.footerLine() != loaded.footerLine() {
		t.Fatalf("delay timer changed footer after load to %q, want %q", afterTimer.footerLine(), loaded.footerLine())
	}
}

func TestJumpBTreesFallsBackToIndex(t *testing.T) {
	t.Parallel()

	m, inspector := newFixtureModel(t, "companies.db")
	m = indexAll(t, m, inspector)

	// Drop all tables so the only B-TREES rows are indexes.
	m.db.Tables = nil
	m.navItems = buildNavItems(m.db, nil, nil)

	next, _ := m.Update(keyMsg("1"))
	got := next.(model)
	if got.navItems[got.selectedIndex].kind != navIndex {
		t.Fatalf("1 with no tables landed on kind %v, want navIndex", got.navItems[got.selectedIndex].kind)
	}
}

func TestRemovedLetterKeysAreNoOps(t *testing.T) {
	t.Parallel()

	m, inspector := newFixtureModel(t, "companies.db")
	m = indexAll(t, m, inspector)
	m.selectFirstKind(navPage)
	idx := m.selectedIndex
	active := m.active

	for _, key := range []string{"g", "h", "p"} {
		next, _ := m.Update(keyMsg(key))
		got := next.(model)
		if got.selectedIndex != idx {
			t.Fatalf("%q moved selectedIndex from %d to %d; should be a no-op", key, idx, got.selectedIndex)
		}
		if got.active != active {
			t.Fatalf("%q changed m.active; should be a no-op", key)
		}
	}
}

func TestReservedNumberKeysAreNoOps(t *testing.T) {
	t.Parallel()

	m, inspector := newFixtureModel(t, "companies.db")
	m = indexAll(t, m, inspector)
	m.selectFirstKind(navPage)
	idx := m.selectedIndex
	active := m.active

	for _, key := range []string{"3", "4"} {
		next, _ := m.Update(keyMsg(key))
		got := next.(model)
		if got.selectedIndex != idx {
			t.Fatalf("%q moved selectedIndex from %d to %d; should be reserved", key, idx, got.selectedIndex)
		}
		if got.active != active {
			t.Fatalf("%q changed m.active; should be reserved", key)
		}
	}
}

func TestEscClearsFilterFirst(t *testing.T) {
	t.Parallel()

	m, inspector := newFixtureModel(t, "companies.db")
	m = indexAll(t, m, inspector)
	companies := objectByName(t, m.db, "companies")
	m.applyFilter(companies)
	if !m.isFiltered() {
		t.Fatal("setup: filter not active")
	}

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	got := next.(model)
	if got.isFiltered() {
		t.Fatal("esc while filtered did not clear the filter")
	}
	if got.active != m.active {
		t.Fatal("esc while filtered changed active target; it must stop after clearing")
	}
}

func TestEscUnfilteredDoesNotNavigateToRemovedOverview(t *testing.T) {
	t.Parallel()

	m, inspector := newFixtureModel(t, "companies.db")
	m = indexAll(t, m, inspector)

	// Inside an open page with explorerIndex > 0 → return to page summary.
	m.active = contentTarget{kind: navPage}
	m.focusedPane = explorerPane
	m.pageRows = []pageRowViewModel{{}, {}}
	m.explorerIndex = 1
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	got := next.(model)
	if got.explorerIndex != 0 {
		t.Fatalf("esc inside an open page left explorerIndex = %d, want 0", got.explorerIndex)
	}
	if got.active.kind != navPage {
		t.Fatalf("esc inside an open page changed active to %v, want it to stay navPage", got.active.kind)
	}

	// From elsewhere → reset page sub-selection/loading state without hidden navigation.
	m.active = contentTarget{kind: navTable, schemaName: "companies"}
	m.focusedPane = navPane
	m.loading = true
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	got = next.(model)
	if got.active.kind != navTable {
		t.Fatalf("esc from a non-page view set active to %v, want navTable", got.active.kind)
	}
	if got.loading {
		t.Fatal("esc from a non-page view left loading=true")
	}
}

func TestArrowsConfinedToSection(t *testing.T) {
	t.Parallel()

	m, inspector := newFixtureModel(t, "companies.db")
	m = indexAll(t, m, inspector)
	m.focusedPane = navPane

	// First B-TREES row: ↑ must not leave the section.
	firstBTree := m.indexOfFirstBTree()
	m.selectedIndex = firstBTree
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyUp})
	if got := next.(model).selectedIndex; got != firstBTree {
		t.Fatalf("↑ on the first B-TREES row moved to %d, want it to stay at %d", got, firstBTree)
	}

	// ↓/↑ move freely within B-TREES (incl. across tables↔indexes).
	m.selectedIndex = firstBTree
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	got := next.(model)
	if sectionForNavItem(got.navItems[got.selectedIndex]) != "B-Trees" {
		t.Fatal("↓ within B-TREES left the section")
	}
	if got.selectedIndex == firstBTree {
		t.Fatal("↓ within B-TREES did not advance")
	}
}

func TestNavMovementAutoActivatesBTreeRows(t *testing.T) {
	t.Parallel()

	m, inspector := newFixtureModel(t, "companies.db")
	m = indexAll(t, m, inspector)
	m.focusedPane = navPane
	m.selectedIndex = m.indexOfFirstBTree()

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	got := next.(model)
	if cmd != nil {
		t.Fatal("moving to a b-tree row returned a load command")
	}
	if got.active.kind != got.selectedItem().kind {
		t.Fatalf("active kind = %v, selected kind = %v", got.active.kind, got.selectedItem().kind)
	}
	if got.selectedItem().schema == nil {
		t.Fatal("setup: selected row is not a schema row")
	}
	if got.active.schemaName != got.selectedItem().schema.Name {
		t.Fatalf("active schema = %q, want selected schema %q", got.active.schemaName, got.selectedItem().schema.Name)
	}
}

func TestLoadingIndicatorAppearsAfterDelayOnly(t *testing.T) {
	t.Parallel()

	m, inspector := newFixtureModel(t, "companies.db")
	m = indexAll(t, m, inspector)

	next, cmd := m.Update(keyMsg("2"))
	got := next.(model)
	if cmd == nil {
		t.Fatal("page activation returned nil cmd")
	}
	if !got.loading {
		t.Fatal("page activation did not set loading=true")
	}
	if got.loadingVisible {
		t.Fatal("loading indicator is visible before the delay")
	}
	if strings.Contains(got.View(), "Loading page") || strings.Contains(got.View(), "Loading page details") {
		t.Fatal("view shows loading copy before the delay elapses")
	}

	next, _ = got.Update(loadingDelayElapsedMsg{pageNumber: got.active.pageNumber})
	delayed := next.(model)
	if !delayed.loadingVisible {
		t.Fatal("delay message did not reveal the loading indicator")
	}
	if delayed.status != "" {
		t.Fatalf("delay message changed footer status to %q", delayed.status)
	}
	if !strings.Contains(delayed.View(), "Loading page") {
		t.Fatal("view does not show loading copy after the delay elapses")
	}
	if strings.Contains(delayed.viewNavigation(24, 20), "Loading page") {
		t.Fatal("navigation pane shows loading copy")
	}

	next, _ = got.Update(loadingDelayElapsedMsg{pageNumber: got.active.pageNumber + 1})
	staleTimer := next.(model)
	if staleTimer.loadingVisible {
		t.Fatal("stale delay message revealed the loading indicator")
	}
}

func TestFastPageScrollKeepsPreviousPageVisibleDuringDelay(t *testing.T) {
	t.Parallel()

	m, inspector := newFixtureModel(t, "companies.db")
	m = indexAll(t, m, inspector)

	next, cmd := m.Update(keyMsg("2"))
	firstLoading := next.(model)
	next, _ = firstLoading.Update(pageLoadedFromCmd(t, cmd))
	loaded := next.(model)
	if loaded.currentPage == nil {
		t.Fatal("setup: first page did not load")
	}
	firstPage := loaded.currentPage.PageNumber

	next, cmd = loaded.Update(tea.KeyMsg{Type: tea.KeyDown})
	scrolling := next.(model)
	if cmd == nil {
		t.Fatal("moving to the next page did not start a load")
	}
	if !scrolling.loading {
		t.Fatal("moving to the next page did not set loading=true")
	}
	if scrolling.loadingVisible {
		t.Fatal("loading indicator is visible before the delay")
	}
	if scrolling.currentPage == nil || scrolling.currentPage.PageNumber != firstPage {
		t.Fatalf("currentPage = %+v, want previous page %d preserved during delay", scrolling.currentPage, firstPage)
	}

	view := scrolling.View()
	if !strings.Contains(view, "Page number: 1") {
		t.Fatalf("view did not keep previous page visible during delay; want page %d", firstPage)
	}
	if !strings.Contains(view, "STRUCTURES") {
		t.Fatal("view did not keep the previous page structure table visible during delay")
	}
	if strings.Contains(view, "Waiting for page details") || strings.Contains(view, "Loading page") {
		t.Fatal("view showed loading/empty placeholder during the delay")
	}
}

func TestStalePageLoadedMessageIgnored(t *testing.T) {
	t.Parallel()

	m, _ := newFixtureModel(t, "companies.db")
	m.active = contentTarget{kind: navPage, pageNumber: 2}
	m.loading = true
	m.status = "loading page 2"

	next, cmd := m.Update(pageLoadedMsg{page: &sqlite.PageInspection{PageNumber: 1}})
	got := next.(model)
	if cmd != nil {
		t.Fatal("stale pageLoadedMsg returned a command")
	}
	if got.currentPage != nil {
		t.Fatalf("stale pageLoadedMsg set currentPage = %+v", got.currentPage)
	}
	if !got.loading {
		t.Fatal("stale pageLoadedMsg cleared loading for the active page")
	}
	if got.status != "loading page 2" {
		t.Fatalf("stale pageLoadedMsg changed status to %q", got.status)
	}
}

func TestSectionHeadersShowJumpNumbers(t *testing.T) {
	t.Parallel()

	m, inspector := newFixtureModel(t, "companies.db")
	m = indexAll(t, m, inspector)

	view := m.View()
	for _, label := range []string{"[1] B-TREES", "[2] PAGES"} {
		if !strings.Contains(view, label) {
			t.Fatalf("navigation pane missing section header %q", label)
		}
	}
	for _, removed := range []string{"[1] MAIN", "Overview", "DB Header"} {
		if strings.Contains(view, removed) {
			t.Fatalf("navigation pane still contains removed content %q", removed)
		}
	}

	// The verbose section tokens and the removed letter hints stay out of the footer.
	for _, dropped := range []string{"1 main · 2 b-trees", "g overview", "h header"} {
		if strings.Contains(view, dropped) {
			t.Fatalf("footer still advertises removed hint %q", dropped)
		}
	}
}

func TestStartsOnVisibleBTreeRow(t *testing.T) {
	t.Parallel()

	m, _ := newFixtureModel(t, "companies.db")

	if m.selectedItem().title != "sqlite_schema" {
		t.Fatalf("selected row = %q, want sqlite_schema", m.selectedItem().title)
	}
	if m.active.kind != navTable || m.active.schemaName != "sqlite_schema" {
		t.Fatalf("active target = %+v, want sqlite_schema table", m.active)
	}

	view := m.View()
	if !strings.Contains(view, "> sqlite_schema") {
		t.Fatal("navigation does not render the selected system catalog as bare sqlite_schema")
	}
}

func TestNavRowsKeepStableWidthAcrossBTreeKinds(t *testing.T) {
	t.Parallel()

	m, _ := newFixtureModel(t, "companies.db")
	const width = 24

	var rowWidths []int
	for _, item := range m.navItems {
		if item.kind != navTable && item.kind != navIndex {
			continue
		}
		m.selectedIndex = indexOfBTreeRow(m.navItems, *item.schema)
		line := renderNavLine(selectedNavItemStyle, width, m.navMarker(m.selectedIndex), navSchemaRowText(*item.schema))
		rowWidths = append(rowWidths, lipgloss.Width(line))
	}
	if len(rowWidths) < 3 {
		t.Fatalf("expected at least sqlite_schema, table, and index rows; got widths %v", rowWidths)
	}
	for _, got := range rowWidths {
		if got != width {
			t.Fatalf("selected nav row widths = %v, want every row to be %d cells", rowWidths, width)
		}
	}

	view := m.View()
	for _, iconRow := range []string{"▦ companies", "▦ sqlite_sequence", "◈ idx_companies_country"} {
		if !strings.Contains(view, iconRow) {
			t.Fatalf("navigation missing icon row %q", iconRow)
		}
	}
}

func TestSchemaObjectMultilineSQLDoesNotChangeExplorerHeight(t *testing.T) {
	t.Parallel()

	for _, fixture := range []string{"companies.db", "sample.db"} {
		fixture := fixture
		t.Run(fixture, func(t *testing.T) {
			t.Parallel()

			m, _ := newFixtureModel(t, fixture)
			for _, table := range m.db.Tables {
				if table.IsSystem || !strings.Contains(table.SQL, "\n") {
					continue
				}
				m.selectedIndex = indexOfBTreeRow(m.navItems, table)
				next, _ := m.openSelected()
				opened := next.(model)

				const height = 12
				view := opened.viewExplorer(52, height)
				if got := strings.Count(view, "\n") + 1; got != height {
					t.Fatalf("%s/%s explorer height = %d physical rows, want %d", fixture, table.Name, got, height)
				}
			}
		})
	}
}

func TestSQLiteSchemaDoesNotShowFabricatedSQL(t *testing.T) {
	t.Parallel()

	m, _ := newFixtureModel(t, "companies.db")
	next, _ := m.openSelected()
	m = next.(model)

	view := m.View()
	if strings.Contains(view, "CREATE TABLE sqlite_schema") {
		t.Fatal("sqlite_schema view shows fabricated CREATE TABLE SQL")
	}
	if !strings.Contains(view, "No stored SQL row for sqlite_schema itself.") {
		t.Fatal("sqlite_schema view does not explain the missing stored SQL row")
	}
}

// indexOfFirstBTree mirrors selectFirstBTreeRow for assertions.
func (m model) indexOfFirstBTree() int {
	for idx, item := range m.navItems {
		if item.kind == navTable || item.kind == navIndex {
			return idx
		}
	}
	return -1
}
