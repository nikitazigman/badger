package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
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

	// Start from an arbitrary, non-overview selection.
	m.selectFirstKind(navPage)
	m.active = contentTarget{kind: navPage}
	before := m.active

	// 1 → first MAIN row (Overview), select-only.
	next, _ := m.Update(keyMsg("1"))
	got := next.(model)
	if want := firstIndexOfKind(got.navItems, navOverview); got.selectedIndex != want {
		t.Fatalf("1 selected index %d, want first navOverview row %d", got.selectedIndex, want)
	}
	if got.active != before {
		t.Fatalf("1 changed m.active to %+v; jumps must be select-only", got.active)
	}

	// 2 → first B-TREES row.
	next, _ = got.Update(keyMsg("2"))
	got = next.(model)
	if want := got.indexOfFirstBTree(); got.selectedIndex != want {
		t.Fatalf("2 selected index %d, want first B-TREES row %d", got.selectedIndex, want)
	}
	if got.active != before {
		t.Fatalf("2 changed m.active to %+v; jumps must be select-only", got.active)
	}

	// 3 → first PAGES row.
	next, _ = got.Update(keyMsg("3"))
	got = next.(model)
	if want := firstIndexOfKind(got.navItems, navPage); got.selectedIndex != want {
		t.Fatalf("3 selected index %d, want first navPage row %d", got.selectedIndex, want)
	}
	if got.active != before {
		t.Fatalf("3 changed m.active to %+v; jumps must be select-only", got.active)
	}
}

func TestJumpBTreesFallsBackToIndex(t *testing.T) {
	t.Parallel()

	m, inspector := newFixtureModel(t, "companies.db")
	m = indexAll(t, m, inspector)

	// Drop all tables so the only B-TREES rows are indexes.
	m.db.Tables = nil
	m.navItems = buildNavItems(m.db, nil, nil)

	next, _ := m.Update(keyMsg("2"))
	got := next.(model)
	if got.navItems[got.selectedIndex].kind != navIndex {
		t.Fatalf("2 with no tables landed on kind %v, want navIndex", got.navItems[got.selectedIndex].kind)
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
	if got.active.kind == navOverview && m.active.kind != navOverview {
		t.Fatal("esc while filtered fell through to the overview reset; it must stop after clearing")
	}
}

func TestEscUnfilteredKeepsExistingBehaviour(t *testing.T) {
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

	// From elsewhere → reset to Overview.
	m.active = contentTarget{kind: navDBHeader}
	m.focusedPane = navPane
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	got = next.(model)
	if got.active.kind != navOverview {
		t.Fatalf("esc from a non-page view set active to %v, want navOverview", got.active.kind)
	}
}

func TestArrowsConfinedToSection(t *testing.T) {
	t.Parallel()

	m, inspector := newFixtureModel(t, "companies.db")
	m = indexAll(t, m, inspector)
	m.focusedPane = navPane

	// Last MAIN row: ↓ must not advance into B-TREES.
	lastMain := -1
	for idx, item := range m.navItems {
		if sectionForNavItem(item) == "Main" {
			lastMain = idx
		}
	}
	m.selectedIndex = lastMain
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	if got := next.(model).selectedIndex; got != lastMain {
		t.Fatalf("↓ on the last MAIN row moved to %d, want it to stay at %d", got, lastMain)
	}

	// First B-TREES row: ↑ must not return to MAIN.
	firstBTree := m.indexOfFirstBTree()
	m.selectedIndex = firstBTree
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyUp})
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
	for _, label := range []string{"[1] MAIN", "[2] B-TREES", "[3] PAGES"} {
		if !strings.Contains(view, label) {
			t.Fatalf("navigation pane missing section header %q", label)
		}
	}

	// The verbose section tokens and the removed letter hints stay out of the footer.
	for _, dropped := range []string{"1 main · 2 b-trees", "g overview", "h header"} {
		if strings.Contains(view, dropped) {
			t.Fatalf("footer still advertises removed hint %q", dropped)
		}
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
