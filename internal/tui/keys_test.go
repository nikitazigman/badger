package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
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

func TestSectionJumpsSelectOnly(t *testing.T) {
	t.Parallel()

	m, inspector := newFixtureModel(t, "companies.db")
	m = indexAll(t, m, inspector)

	// Start from an arbitrary selection outside B-TREES.
	m.selectFirstKind(navPage)
	m.active = contentTarget{kind: navPage}
	before := m.active

	// 1 → first B-TREES row.
	next, _ := m.Update(keyMsg("1"))
	got := next.(model)
	if want := got.indexOfFirstBTree(); got.selectedIndex != want {
		t.Fatalf("1 selected index %d, want first B-TREES row %d", got.selectedIndex, want)
	}
	if got.active != before {
		t.Fatalf("1 changed m.active to %+v; jumps must be select-only", got.active)
	}

	// 2 → first PAGES row.
	next, _ = got.Update(keyMsg("2"))
	got = next.(model)
	if want := firstIndexOfKind(got.navItems, navPage); got.selectedIndex != want {
		t.Fatalf("2 selected index %d, want first navPage row %d", got.selectedIndex, want)
	}
	if got.active != before {
		t.Fatalf("2 changed m.active to %+v; jumps must be select-only", got.active)
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
