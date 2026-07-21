package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/nikitazigman/badger/internal/storage"
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

func syntheticBTreeListModel(count int) model {
	items := make([]navItem, 0, count+1)
	for idx := 0; idx < count; idx++ {
		obj := schemaObjectViewModel{
			ID:       storage.BTreeID(fmt.Sprintf("table:%02d", idx)),
			Kind:     storage.BTreeTable,
			Type:     "table",
			Name:     fmt.Sprintf("table_%02d", idx),
			RootPage: uint64(idx + 1),
		}
		items = append(items, navItem{
			kind:   navTable,
			title:  obj.Name,
			schema: &obj,
		})
	}
	items = append(items, navItem{kind: navPage, title: "page 1", pageNumber: 1})
	return model{
		navItems:      items,
		selectedIndex: 0,
		active:        contentTarget{kind: navTable, schemaName: "table_00", schemaID: "table:00"},
		focusedPane:   navPane,
	}
}

func TestPaneJumpsAutoActivate(t *testing.T) {
	t.Parallel()

	m, inspector := newFixtureModel(t, "companies.db")
	m = indexAll(t, m, inspector)

	// Start from an arbitrary selection outside B-TREES.
	m.selectFirstKind(navPage)
	m.active = contentTarget{kind: navPage}
	m.focusedPane = inspectorPane

	// U -> first B-TREES row.
	next, _ := m.Update(keyMsg("U"))
	got := next.(model)
	if got.focusedPane != navPane {
		t.Fatalf("U focused pane = %v, want navPane", got.focusedPane)
	}
	if want := got.indexOfFirstBTree(); got.selectedIndex != want {
		t.Fatalf("U selected index %d, want first B-TREES row %d", got.selectedIndex, want)
	}
	if got.active.kind != got.selectedItem().kind || got.active.schemaName != got.selectedItem().schema.Name {
		t.Fatalf("U active target = %+v, selected item = %+v", got.active, got.selectedItem())
	}

	// I -> first PAGES row.
	got.focusedPane = explorerPane
	next, cmd := got.Update(keyMsg("I"))
	got = next.(model)
	if got.focusedPane != navPane {
		t.Fatalf("I focused pane = %v, want navPane", got.focusedPane)
	}
	if want := firstIndexOfKind(got.navItems, navPage); got.selectedIndex != want {
		t.Fatalf("I selected index %d, want first navPage row %d", got.selectedIndex, want)
	}
	if got.active.kind != navPage || got.active.pageNumber != got.selectedItem().pageNumber {
		t.Fatalf("I active target = %+v, selected item = %+v", got.active, got.selectedItem())
	}
	if !got.loading {
		t.Fatal("I did not start loading the selected page")
	}
	if got.loadingVisible {
		t.Fatal("I showed loading immediately; loading indicator must be delayed")
	}
	if got.status != "" {
		t.Fatalf("I status = %q, want no loading status before delay", got.status)
	}
	if cmd == nil {
		t.Fatal("I on a page row returned nil cmd, want batched load/delay command")
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
	if loaded.currentPage == nil || loaded.currentPage.Ref.ID != got.active.pageNumber {
		t.Fatalf("loaded current page = %+v, want page %d", loaded.currentPage, got.active.pageNumber)
	}
	if len(loaded.currentPage.Raw) == 0 {
		t.Fatal("loaded page has no raw bytes")
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

func TestPagePaneReturnPreservesSelectedPage(t *testing.T) {
	t.Parallel()

	m, inspector := newFixtureModel(t, "companies.db")
	m = indexAll(t, m, inspector)

	pageIndex, ok := m.indexOfPageRow(3)
	if !ok {
		t.Fatal("setup: page 3 row not found")
	}
	m.selectedIndex = pageIndex
	next, cmd := m.activateSelected()
	loading := next.(model)
	next, _ = loading.Update(pageLoadedFromCmd(t, cmd))
	loaded := next.(model)

	for _, key := range []string{"O", "P"} {
		next, _ = loaded.Update(keyMsg(key))
		away := next.(model)
		next, cmd = away.Update(keyMsg("I"))
		got := next.(model)
		if cmd == nil {
			t.Fatalf("I after %s returned nil cmd, want page load", key)
		}
		if got.focusedPane != navPane {
			t.Fatalf("I after %s focused pane = %v, want navPane", key, got.focusedPane)
		}
		if got.selectedIndex != pageIndex {
			t.Fatalf("I after %s selected index %d, want original page index %d", key, got.selectedIndex, pageIndex)
		}
		if got.active.kind != navPage || got.active.pageNumber != 3 {
			t.Fatalf("I after %s active = %+v, want page 3", key, got.active)
		}
	}
}

func TestPagePaneReturnReselectsActivePageFromOtherSelection(t *testing.T) {
	t.Parallel()

	m, inspector := newFixtureModel(t, "companies.db")
	m = indexAll(t, m, inspector)

	pageIndex, ok := m.indexOfPageRow(4)
	if !ok {
		t.Fatal("setup: page 4 row not found")
	}
	m.selectedIndex = pageIndex
	next, cmd := m.activateSelected()
	activePage := next.(model)
	next, _ = activePage.Update(pageLoadedFromCmd(t, cmd))
	loaded := next.(model)

	loaded.selectedIndex = loaded.indexOfFirstBTree()
	loaded.focusedPane = inspectorPane
	next, cmd = loaded.Update(keyMsg("I"))
	got := next.(model)
	if cmd == nil {
		t.Fatal("I did not load the reselected active page")
	}
	if got.selectedIndex != pageIndex {
		t.Fatalf("I selected index %d, want active page index %d", got.selectedIndex, pageIndex)
	}
	if got.active.kind != navPage || got.active.pageNumber != 4 {
		t.Fatalf("I active = %+v, want page 4", got.active)
	}
}

func TestBTreePaneReturnPreservesSelectedBTree(t *testing.T) {
	t.Parallel()

	m, inspector := newFixtureModel(t, "companies.db")
	m = indexAll(t, m, inspector)
	obj := objectByName(t, m.db, "idx_companies_country")
	btreeIndex := indexOfBTreeRow(m.navItems, obj)

	m.selectedIndex = btreeIndex
	next, cmd := m.activateSelected()
	if cmd != nil {
		t.Fatal("setup: b-tree activation returned a command")
	}
	opened := next.(model)

	for _, key := range []string{"O", "P"} {
		next, _ = opened.Update(keyMsg(key))
		away := next.(model)
		next, cmd = away.Update(keyMsg("U"))
		got := next.(model)
		if cmd != nil {
			t.Fatalf("U after %s returned cmd %v, want nil", key, cmd)
		}
		if got.focusedPane != navPane {
			t.Fatalf("U after %s focused pane = %v, want navPane", key, got.focusedPane)
		}
		if got.selectedIndex != btreeIndex {
			t.Fatalf("U after %s selected index %d, want original b-tree index %d", key, got.selectedIndex, btreeIndex)
		}
		if got.active.kind != navIndex || got.active.schemaID != obj.ID {
			t.Fatalf("U after %s active = %+v, want b-tree %s", key, got.active, obj.ID)
		}
	}
}

func TestBTreePaneReturnReselectsActiveBTreeFromPageSelection(t *testing.T) {
	t.Parallel()

	m, inspector := newFixtureModel(t, "companies.db")
	m = indexAll(t, m, inspector)
	obj := objectByName(t, m.db, "companies")
	btreeIndex := indexOfBTreeRow(m.navItems, obj)

	m.selectedIndex = btreeIndex
	next, cmd := m.activateSelected()
	if cmd != nil {
		t.Fatal("setup: b-tree activation returned a command")
	}
	opened := next.(model)
	if !opened.selectFirstKind(navPage) {
		t.Fatal("setup: no page rows")
	}
	opened.focusedPane = inspectorPane

	next, cmd = opened.Update(keyMsg("U"))
	got := next.(model)
	if cmd != nil {
		t.Fatalf("U returned cmd %v, want nil", cmd)
	}
	if got.selectedIndex != btreeIndex {
		t.Fatalf("U selected index %d, want active b-tree index %d", got.selectedIndex, btreeIndex)
	}
	if got.active.kind != navTable || got.active.schemaID != obj.ID {
		t.Fatalf("U active = %+v, want b-tree %s", got.active, obj.ID)
	}
}

func TestBTreePaneReturnReselectsFilterSourceForActivePage(t *testing.T) {
	t.Parallel()

	m, inspector := newFixtureModel(t, "companies.db")
	m = indexAll(t, m, inspector)
	companies := objectByName(t, m.db, "companies")
	m.applyFilter(companies)

	pageIndex := firstIndexOfKind(m.navItems, navPage)
	if pageIndex < 0 {
		t.Fatal("setup: filtered page rows not found")
	}
	m.selectedIndex = pageIndex
	next, cmd := m.activateSelected()
	loading := next.(model)
	next, _ = loading.Update(pageLoadedFromCmd(t, cmd))
	loaded := next.(model)
	loaded.focusedPane = inspectorPane

	next, cmd = loaded.Update(keyMsg("U"))
	got := next.(model)
	if cmd != nil {
		t.Fatalf("U returned cmd %v, want nil", cmd)
	}
	if want := indexOfBTreeRow(got.navItems, companies); got.selectedIndex != want {
		t.Fatalf("U selected index %d, want filter source index %d", got.selectedIndex, want)
	}
	if got.active.kind != navTable || got.active.schemaID != companies.ID {
		t.Fatalf("U active = %+v, want filter source %s", got.active, companies.ID)
	}
}

func TestPaneReturnFallsBackAndHandlesEmptyLists(t *testing.T) {
	t.Parallel()

	m, inspector := newFixtureModel(t, "companies.db")
	m = indexAll(t, m, inspector)
	companies := objectByName(t, m.db, "companies")
	m.applyFilter(companies)
	m.navItems = buildNavItems(m.db, m.activeFilter, []storage.PageRef{{ID: 2}, {ID: 7}})
	m.active = contentTarget{kind: navPage, pageNumber: 5}
	m.selectedIndex = indexOfBTreeRow(m.navItems, companies)

	next, cmd := m.Update(keyMsg("I"))
	got := next.(model)
	if cmd == nil {
		t.Fatal("I did not load the nearest fallback page")
	}
	if got.selectedItem().kind != navPage || got.selectedItem().pageNumber != 7 {
		t.Fatalf("I fallback selected %+v, want page 7", got.selectedItem())
	}

	emptyPages := got
	emptyPages.navItems = buildNavItems(emptyPages.db, emptyPages.activeFilter, nil)
	emptyPages.active = contentTarget{kind: navPage, pageNumber: 5}
	emptyPages.selectedIndex = indexOfBTreeRow(emptyPages.navItems, companies)
	next, cmd = emptyPages.Update(keyMsg("I"))
	noPages := next.(model)
	if cmd != nil {
		t.Fatalf("I on empty pages returned cmd %v, want nil", cmd)
	}
	if noPages.selectedIndex != emptyPages.selectedIndex {
		t.Fatalf("I on empty pages moved selection from %d to %d", emptyPages.selectedIndex, noPages.selectedIndex)
	}
	if !strings.Contains(noPages.footerLine(), "no pages in current page list") {
		t.Fatalf("I on empty pages footer = %q", noPages.footerLine())
	}

	emptyBTrees := model{
		navItems:      []navItem{{kind: navPage, title: "page 1", pageNumber: 1}},
		selectedIndex: 0,
		active:        contentTarget{kind: navTable, schemaName: "missing", schemaID: "table:missing"},
		focusedPane:   inspectorPane,
	}
	next, cmd = emptyBTrees.Update(keyMsg("U"))
	noBTrees := next.(model)
	if cmd != nil {
		t.Fatalf("U on empty b-trees returned cmd %v, want nil", cmd)
	}
	if noBTrees.selectedIndex != emptyBTrees.selectedIndex {
		t.Fatalf("U on empty b-trees moved selection from %d to %d", emptyBTrees.selectedIndex, noBTrees.selectedIndex)
	}
	if !strings.Contains(noBTrees.footerLine(), "no b-trees in current b-tree list") {
		t.Fatalf("U on empty b-trees footer = %q", noBTrees.footerLine())
	}
}

func TestJumpBTreesFallsBackToIndex(t *testing.T) {
	t.Parallel()

	m, inspector := newFixtureModel(t, "companies.db")
	m = indexAll(t, m, inspector)

	// Drop all tables so the only B-TREES rows are indexes.
	m.db.Tables = nil
	m.navItems = buildNavItems(m.db, nil, nil)

	next, _ := m.Update(keyMsg("U"))
	got := next.(model)
	if got.navItems[got.selectedIndex].kind != navIndex {
		t.Fatalf("U with no tables landed on kind %v, want navIndex", got.navItems[got.selectedIndex].kind)
	}
}

func TestPaneJumpsActivateWhenSelectionAlreadyOnTarget(t *testing.T) {
	t.Parallel()

	m, inspector := newFixtureModel(t, "companies.db")
	m = indexAll(t, m, inspector)

	m.selectedIndex = m.indexOfFirstBTree()
	m.active = contentTarget{kind: navPage, pageNumber: 1}
	m.focusedPane = inspectorPane
	next, cmd := m.Update(keyMsg("U"))
	got := next.(model)
	if cmd != nil {
		t.Fatalf("U returned cmd %v, want nil for b-tree activation", cmd)
	}
	if got.focusedPane != navPane {
		t.Fatalf("U focused pane = %v, want navPane", got.focusedPane)
	}
	if got.active.kind != got.selectedItem().kind || got.active.schemaName != got.selectedItem().schema.Name {
		t.Fatalf("U did not activate already-selected b-tree; active=%+v selected=%+v", got.active, got.selectedItem())
	}

	m = got
	if !m.selectFirstKind(navPage) {
		t.Fatal("setup: no page rows")
	}
	m.active = contentTarget{kind: navTable, schemaName: "sqlite_schema"}
	m.focusedPane = explorerPane
	next, cmd = m.Update(keyMsg("I"))
	got = next.(model)
	if cmd == nil {
		t.Fatal("I did not return page load command for already-selected page")
	}
	if got.focusedPane != navPane {
		t.Fatalf("I focused pane = %v, want navPane", got.focusedPane)
	}
	if got.active.kind != navPage || got.active.pageNumber != got.selectedItem().pageNumber {
		t.Fatalf("I did not activate already-selected page; active=%+v selected=%+v", got.active, got.selectedItem())
	}
}

func TestBoundaryJumpsActivateBTreeRows(t *testing.T) {
	t.Parallel()

	m, inspector := newFixtureModel(t, "companies.db")
	m = indexAll(t, m, inspector)
	m.focusedPane = navPane

	lastBTree, ok := m.boundaryIndexMatching(isBTreeNavItem, false)
	if !ok {
		t.Fatal("setup: no b-tree rows")
	}
	m.selectedIndex = m.indexOfFirstBTree()

	next, cmd := m.Update(keyMsg("g"))
	pending := next.(model)
	if cmd != nil {
		t.Fatalf("g returned cmd %v, want nil", cmd)
	}
	if !pending.pendingG {
		t.Fatal("g did not enter pending-g state")
	}

	next, cmd = pending.Update(keyMsg("e"))
	got := next.(model)
	if cmd != nil {
		t.Fatalf("ge returned cmd %v, want nil for b-tree activation", cmd)
	}
	if got.pendingG {
		t.Fatal("ge left pending-g state set")
	}
	if got.selectedIndex != lastBTree {
		t.Fatalf("ge selected index %d, want last b-tree index %d", got.selectedIndex, lastBTree)
	}
	if got.active.kind != got.selectedItem().kind || got.active.schemaID != got.selectedItem().schema.ID {
		t.Fatalf("ge active = %+v, selected item = %+v", got.active, got.selectedItem())
	}

	next, cmd = got.Update(keyMsg("g"))
	pending = next.(model)
	next, cmd = pending.Update(keyMsg("g"))
	got = next.(model)
	if cmd != nil {
		t.Fatalf("gg returned cmd %v, want nil for b-tree activation", cmd)
	}
	if want := got.indexOfFirstBTree(); got.selectedIndex != want {
		t.Fatalf("gg selected index %d, want first b-tree index %d", got.selectedIndex, want)
	}
	if got.active.kind != got.selectedItem().kind || got.active.schemaID != got.selectedItem().schema.ID {
		t.Fatalf("gg active = %+v, selected item = %+v", got.active, got.selectedItem())
	}
}

func TestBoundaryJumpsWorkAfterBTreePaneJump(t *testing.T) {
	t.Parallel()

	m := syntheticBTreeListModel(12)
	pageIndex := len(m.navItems) - 1
	m.selectedIndex = pageIndex
	m.active = contentTarget{kind: navTable, schemaName: "table_03", schemaID: "table:03"}
	m.focusedPane = inspectorPane

	next, cmd := m.Update(keyMsg("U"))
	btrees := next.(model)
	if cmd != nil {
		t.Fatalf("U returned cmd %v, want nil", cmd)
	}
	if btrees.focusedPane != navPane {
		t.Fatalf("U focused pane = %v, want navPane", btrees.focusedPane)
	}
	if btrees.selectedIndex != 3 {
		t.Fatalf("U selected index %d, want active b-tree index 3", btrees.selectedIndex)
	}

	next, _ = btrees.Update(keyMsg("g"))
	pending := next.(model)
	next, cmd = pending.Update(keyMsg("e"))
	got := next.(model)
	if cmd != nil {
		t.Fatalf("ge returned cmd %v, want nil for b-tree activation", cmd)
	}
	if got.selectedIndex != 11 {
		t.Fatalf("ge selected index %d, want last b-tree index 11", got.selectedIndex)
	}
	if got.active.kind != navTable || got.active.schemaID != "table:11" {
		t.Fatalf("ge active = %+v, want table:11", got.active)
	}

	next, _ = got.Update(keyMsg("g"))
	pending = next.(model)
	next, cmd = pending.Update(keyMsg("g"))
	got = next.(model)
	if cmd != nil {
		t.Fatalf("gg returned cmd %v, want nil for b-tree activation", cmd)
	}
	if got.selectedIndex != 0 {
		t.Fatalf("gg selected index %d, want first b-tree index 0", got.selectedIndex)
	}
	if got.active.kind != navTable || got.active.schemaID != "table:00" {
		t.Fatalf("gg active = %+v, want table:00", got.active)
	}
}

func TestBoundaryJumpsActivateUnfilteredPageRows(t *testing.T) {
	t.Parallel()

	m, inspector := newFixtureModel(t, "companies.db")
	m = indexAll(t, m, inspector)
	m.focusedPane = navPane
	if !m.selectFirstKind(navPage) {
		t.Fatal("setup: no page rows")
	}
	firstPage := m.selectedIndex
	lastPage, ok := m.boundaryIndexMatching(func(item navItem) bool { return item.kind == navPage }, false)
	if !ok {
		t.Fatal("setup: no page rows")
	}

	next, _ := m.Update(keyMsg("g"))
	pending := next.(model)
	next, cmd := pending.Update(keyMsg("e"))
	got := next.(model)
	if cmd == nil {
		t.Fatal("ge did not return page load command")
	}
	if got.selectedIndex != lastPage {
		t.Fatalf("ge selected index %d, want last page index %d", got.selectedIndex, lastPage)
	}
	if got.active.kind != navPage || got.active.pageNumber != got.selectedItem().pageNumber {
		t.Fatalf("ge active = %+v, selected item = %+v", got.active, got.selectedItem())
	}

	next, _ = got.Update(keyMsg("g"))
	pending = next.(model)
	next, cmd = pending.Update(keyMsg("g"))
	got = next.(model)
	if cmd == nil {
		t.Fatal("gg did not return page load command")
	}
	if got.selectedIndex != firstPage {
		t.Fatalf("gg selected index %d, want first page index %d", got.selectedIndex, firstPage)
	}
	if got.active.kind != navPage || got.active.pageNumber != got.selectedItem().pageNumber {
		t.Fatalf("gg active = %+v, selected item = %+v", got.active, got.selectedItem())
	}
}

func TestBoundaryJumpsRespectFilteredPages(t *testing.T) {
	t.Parallel()

	m, inspector := newFixtureModel(t, "companies.db")
	m = indexAll(t, m, inspector)
	companies := objectByName(t, m.db, "companies")
	m.applyFilter(companies)
	m.focusedPane = navPane
	if !m.selectFirstKind(navPage) {
		t.Fatal("setup: no filtered page rows")
	}
	firstPage := m.selectedItem().pageNumber
	lastPageIndex, ok := m.boundaryIndexMatching(func(item navItem) bool { return item.kind == navPage }, false)
	if !ok {
		t.Fatal("setup: no filtered page rows")
	}
	lastPage := m.navItems[lastPageIndex].pageNumber
	if firstPage == lastPage {
		t.Fatal("setup: filtered page list has only one page")
	}

	next, _ := m.Update(keyMsg("g"))
	pending := next.(model)
	next, cmd := pending.Update(keyMsg("e"))
	got := next.(model)
	if cmd == nil {
		t.Fatal("ge did not return page load command")
	}
	if got.active.kind != navPage || got.active.pageNumber != lastPage {
		t.Fatalf("ge active = %+v, want filtered last page %d", got.active, lastPage)
	}

	next, _ = got.Update(keyMsg("g"))
	pending = next.(model)
	next, cmd = pending.Update(keyMsg("g"))
	got = next.(model)
	if cmd == nil {
		t.Fatal("gg did not return page load command")
	}
	if got.active.kind != navPage || got.active.pageNumber != firstPage {
		t.Fatalf("gg active = %+v, want filtered first page %d", got.active, firstPage)
	}
}

func TestPendingGStateClearsOnUnrelatedKeysEscAndPaneJumps(t *testing.T) {
	t.Parallel()

	for _, key := range []string{"h", "esc", "U", "I", "O", "P"} {
		key := key
		t.Run(key, func(t *testing.T) {
			t.Parallel()

			m, inspector := newFixtureModel(t, "companies.db")
			m = indexAll(t, m, inspector)
			m.focusedPane = navPane
			if !m.selectFirstKind(navPage) {
				t.Fatal("setup: no page rows")
			}

			next, _ := m.Update(keyMsg("g"))
			pending := next.(model)
			if !pending.pendingG {
				t.Fatal("setup: pending-g state was not set")
			}

			next, _ = pending.Update(keyMsg(key))
			got := next.(model)
			if got.pendingG {
				t.Fatalf("%q did not clear pending-g state", key)
			}
		})
	}
}

func TestBoundaryJumpsHandleEmptyVisiblePageList(t *testing.T) {
	t.Parallel()

	m := model{
		focusedPane:   navPane,
		navItems:      nil,
		selectedIndex: 0,
	}

	next, cmd := m.Update(keyMsg("g"))
	pending := next.(model)
	if cmd != nil {
		t.Fatalf("g returned cmd %v, want nil", cmd)
	}
	next, cmd = pending.Update(keyMsg("e"))
	got := next.(model)
	if cmd != nil {
		t.Fatalf("ge on empty list returned cmd %v, want nil", cmd)
	}
	if !strings.Contains(got.footerLine(), "no pages in current page list") {
		t.Fatalf("ge on empty page list footer = %q", got.footerLine())
	}
}

func TestBoundaryJumpsHandleEmptyVisibleBTreeList(t *testing.T) {
	t.Parallel()

	m := model{
		focusedPane:   navPane,
		navItems:      []navItem{{kind: navPage, title: "page 1", pageNumber: 1}},
		selectedIndex: -1,
		active:        contentTarget{kind: navTable, schemaName: "missing", schemaID: "table:missing"},
	}

	next, cmd := m.Update(keyMsg("g"))
	pending := next.(model)
	if cmd != nil {
		t.Fatalf("g returned cmd %v, want nil", cmd)
	}
	next, cmd = pending.Update(keyMsg("g"))
	got := next.(model)
	if cmd != nil {
		t.Fatalf("gg on empty b-tree list returned cmd %v, want nil", cmd)
	}
	if got.selectedIndex != m.selectedIndex {
		t.Fatalf("gg on empty b-tree list moved selection from %d to %d", m.selectedIndex, got.selectedIndex)
	}
	if !strings.Contains(got.footerLine(), "no b-trees in current b-tree list") {
		t.Fatalf("gg on empty b-tree list footer = %q", got.footerLine())
	}
}

func TestBTreeListUDHotkeysActivateVisibleRows(t *testing.T) {
	t.Parallel()

	m := syntheticBTreeListModel(15)
	firstIndex := m.selectedIndex

	next, cmd := m.Update(keyMsg("d"))
	down := next.(model)
	if cmd != nil {
		t.Fatalf("d on b-tree list returned cmd %v, want nil", cmd)
	}
	if down.selectedIndex != firstIndex+10 {
		t.Fatalf("d selected index %d, want %d", down.selectedIndex, firstIndex+10)
	}
	if down.active.kind != navTable || down.active.schemaID != "table:10" {
		t.Fatalf("d active = %+v, want table:10", down.active)
	}

	next, cmd = down.Update(keyMsg("u"))
	up := next.(model)
	if cmd != nil {
		t.Fatalf("u on b-tree list returned cmd %v, want nil", cmd)
	}
	if up.selectedIndex != firstIndex {
		t.Fatalf("u selected index %d, want %d", up.selectedIndex, firstIndex)
	}
	if up.active.kind != navTable || up.active.schemaID != "table:00" {
		t.Fatalf("u active = %+v, want table:00", up.active)
	}
}

func TestBTreeListUDHotkeysClampAtBoundaries(t *testing.T) {
	t.Parallel()

	m := syntheticBTreeListModel(15)
	m.selectedIndex = 13

	next, cmd := m.Update(keyMsg("d"))
	got := next.(model)
	if cmd != nil {
		t.Fatalf("d near last b-tree returned cmd %v, want nil", cmd)
	}
	if got.selectedIndex != 14 {
		t.Fatalf("d near last b-tree selected index %d, want clamped last index 14", got.selectedIndex)
	}
	if got.active.kind != navTable || got.active.schemaID != "table:14" {
		t.Fatalf("d active = %+v, want table:14", got.active)
	}
}

func TestPageListUDHotkeysActivateVisibleRows(t *testing.T) {
	t.Parallel()

	m, inspector := newFixtureModel(t, "companies.db")
	m = indexAll(t, m, inspector)
	m.focusedPane = navPane
	if !m.selectFirstKind(navPage) {
		t.Fatal("setup: no page rows")
	}
	firstIndex := m.selectedIndex
	firstPage := m.selectedItem().pageNumber
	lastPage, ok := m.boundaryIndexMatching(func(item navItem) bool { return item.kind == navPage }, false)
	if !ok {
		t.Fatal("setup: no page rows")
	}
	if lastPage < firstIndex+10 {
		t.Fatalf("setup: page list has too few rows for a 10-row jump: first=%d last=%d", firstIndex, lastPage)
	}

	next, cmd := m.Update(keyMsg("d"))
	down := next.(model)
	if cmd == nil {
		t.Fatal("d on page list did not return page load command")
	}
	if down.selectedIndex != firstIndex+10 {
		t.Fatalf("d selected index %d, want %d", down.selectedIndex, firstIndex+10)
	}
	if down.active.kind != navPage || down.active.pageNumber != down.selectedItem().pageNumber {
		t.Fatalf("d active = %+v, selected item = %+v", down.active, down.selectedItem())
	}
	if down.drill.active {
		t.Fatal("d on page list entered drill mode")
	}

	next, cmd = down.Update(keyMsg("u"))
	up := next.(model)
	if cmd == nil {
		t.Fatal("u on page list did not return page load command")
	}
	if up.selectedIndex != firstIndex {
		t.Fatalf("u selected index %d, want %d", up.selectedIndex, firstIndex)
	}
	if up.active.kind != navPage || up.active.pageNumber != firstPage {
		t.Fatalf("u active = %+v, want page %d", up.active, firstPage)
	}

	next, cmd = up.Update(keyMsg("u"))
	stillFirst := next.(model)
	if cmd != nil {
		t.Fatalf("u at first page returned cmd %v, want nil", cmd)
	}
	if stillFirst.selectedIndex != firstIndex || stillFirst.active.pageNumber != firstPage {
		t.Fatalf("u at first page changed selection/active to index %d page %d", stillFirst.selectedIndex, stillFirst.active.pageNumber)
	}
}

func TestPageListUDHotkeysClampAtBoundaries(t *testing.T) {
	t.Parallel()

	m, inspector := newFixtureModel(t, "companies.db")
	m = indexAll(t, m, inspector)
	m.focusedPane = navPane
	if !m.selectFirstKind(navPage) {
		t.Fatal("setup: no page rows")
	}
	lastPage, ok := m.boundaryIndexMatching(func(item navItem) bool { return item.kind == navPage }, false)
	if !ok {
		t.Fatal("setup: no page rows")
	}
	m.selectedIndex = lastPage - 1

	next, cmd := m.Update(keyMsg("d"))
	got := next.(model)
	if cmd == nil {
		t.Fatal("d near last page did not return page load command")
	}
	if got.selectedIndex != lastPage {
		t.Fatalf("d near last page selected index %d, want clamped last page index %d", got.selectedIndex, lastPage)
	}
	if got.active.kind != navPage || got.active.pageNumber != got.selectedItem().pageNumber {
		t.Fatalf("d active = %+v, selected item = %+v", got.active, got.selectedItem())
	}
}

func TestPageListUDHotkeysRespectFilteredPages(t *testing.T) {
	t.Parallel()

	m, inspector := newFixtureModel(t, "companies.db")
	m = indexAll(t, m, inspector)
	companies := objectByName(t, m.db, "companies")
	m.applyFilter(companies)
	m.focusedPane = navPane
	if !m.selectFirstKind(navPage) {
		t.Fatal("setup: no filtered page rows")
	}
	firstFilteredPage := m.selectedItem().pageNumber
	lastPage, ok := m.boundaryIndexMatching(func(item navItem) bool { return item.kind == navPage }, false)
	if !ok {
		t.Fatal("setup: no filtered page rows")
	}
	expectedDownIndex := min(m.selectedIndex+10, lastPage)

	next, cmd := m.Update(keyMsg("d"))
	down := next.(model)
	if cmd == nil {
		t.Fatal("d on filtered page list did not return page load command")
	}
	if down.selectedIndex != expectedDownIndex {
		t.Fatalf("d selected filtered index %d, want %d", down.selectedIndex, expectedDownIndex)
	}
	if down.active.kind != navPage || down.active.pageNumber != down.selectedItem().pageNumber {
		t.Fatalf("d active = %+v, selected item = %+v", down.active, down.selectedItem())
	}
	if down.active.pageNumber == firstFilteredPage {
		t.Fatalf("d stayed on first filtered page %d", firstFilteredPage)
	}

	next, cmd = down.Update(keyMsg("u"))
	up := next.(model)
	if cmd == nil {
		t.Fatal("u on filtered page list did not return page load command")
	}
	if up.active.kind != navPage || up.active.pageNumber != firstFilteredPage {
		t.Fatalf("u active = %+v, want first filtered page %d", up.active, firstFilteredPage)
	}
}

func TestDigitPaneKeysAreNoOps(t *testing.T) {
	t.Parallel()

	m, inspector := newFixtureModel(t, "companies.db")
	m = indexAll(t, m, inspector)
	if !m.selectFirstKind(navPage) {
		t.Fatal("setup: no page rows")
	}
	m.focusedPane = inspectorPane
	idx := m.selectedIndex
	active := m.active

	for _, key := range []string{"1", "2", "3", "4"} {
		next, cmd := m.Update(keyMsg(key))
		got := next.(model)
		if cmd != nil {
			t.Fatalf("%q returned cmd %v, want nil", key, cmd)
		}
		if got.focusedPane != inspectorPane {
			t.Fatalf("%q changed focusedPane from inspectorPane to %v", key, got.focusedPane)
		}
		if got.selectedIndex != idx {
			t.Fatalf("%q moved selectedIndex from %d to %d", key, idx, got.selectedIndex)
		}
		if got.active != active {
			t.Fatalf("%q changed active from %+v to %+v", key, active, got.active)
		}
	}
}

func TestRemovedLetterKeysAreNoOps(t *testing.T) {
	t.Parallel()

	m, inspector := newFixtureModel(t, "companies.db")
	m = indexAll(t, m, inspector)
	m.selectFirstKind(navPage)
	idx := m.selectedIndex
	active := m.active

	for _, key := range []string{"h", "p"} {
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

func TestTabKeysAreNoOps(t *testing.T) {
	t.Parallel()

	m, inspector := newFixtureModel(t, "companies.db")
	m = indexAll(t, m, inspector)
	m.focusedPane = explorerPane
	idx := m.selectedIndex
	active := m.active

	for _, key := range []tea.KeyMsg{
		{Type: tea.KeyTab},
		{Type: tea.KeyShiftTab},
	} {
		next, cmd := m.Update(key)
		got := next.(model)
		if cmd != nil {
			t.Fatalf("%q returned cmd %v, want nil", key, cmd)
		}
		if got.focusedPane != explorerPane {
			t.Fatalf("%q changed focusedPane from explorerPane to %v", key, got.focusedPane)
		}
		if got.selectedIndex != idx {
			t.Fatalf("%q moved selectedIndex from %d to %d", key, idx, got.selectedIndex)
		}
		if got.active != active {
			t.Fatalf("%q changed active from %+v to %+v", key, active, got.active)
		}
	}
}

func TestContentPaneJumpsPreserveNavigationState(t *testing.T) {
	t.Parallel()

	m, inspector := newFixtureModel(t, "companies.db")
	m = indexAll(t, m, inspector)
	companies := objectByName(t, m.db, "companies")
	m.applyFilter(companies)
	if m.activeFilter == nil {
		t.Fatal("setup: filter not active")
	}
	if !m.selectFirstKind(navPage) {
		t.Fatal("setup: no page rows after applying filter")
	}

	idx := m.selectedIndex
	active := m.active
	filterSource := m.activeFilter.object

	for _, tc := range []struct {
		key  string
		pane pane
	}{
		{key: "O", pane: explorerPane},
		{key: "P", pane: inspectorPane},
	} {
		m.focusedPane = navPane
		next, cmd := m.Update(keyMsg(tc.key))
		got := next.(model)
		if cmd != nil {
			t.Fatalf("%q returned cmd %v, want nil", tc.key, cmd)
		}
		if got.focusedPane != tc.pane {
			t.Fatalf("%q focused pane = %v, want %v", tc.key, got.focusedPane, tc.pane)
		}
		if got.selectedIndex != idx {
			t.Fatalf("%q moved selectedIndex from %d to %d", tc.key, idx, got.selectedIndex)
		}
		if got.active != active {
			t.Fatalf("%q changed m.active from %+v to %+v", tc.key, active, got.active)
		}
		if got.activeFilter == nil || got.activeFilter.object != filterSource {
			t.Fatalf("%q changed active filter from %+v to %+v", tc.key, filterSource, got.activeFilter)
		}
	}
}

func TestPaneLocalControlsWorkAfterPaneJumps(t *testing.T) {
	t.Parallel()

	m, inspector := newFixtureModel(t, "companies.db")
	m = indexAll(t, m, inspector)

	next, cmd := m.Update(keyMsg("I"))
	loading := next.(model)
	next, _ = loading.Update(pageLoadedFromCmd(t, cmd))
	loaded := next.(model)

	next, cmd = loaded.Update(keyMsg("O"))
	detail := next.(model)
	if cmd != nil {
		t.Fatal("O returned a command")
	}
	selectedIndex := detail.selectedIndex
	next, cmd = detail.Update(tea.KeyMsg{Type: tea.KeyDown})
	detailMoved := next.(model)
	if cmd != nil {
		t.Fatal("detail down returned a command")
	}
	if detailMoved.focusedPane != explorerPane {
		t.Fatalf("detail focus = %v, want explorerPane", detailMoved.focusedPane)
	}
	if detailMoved.inspectorScroll != detail.inspectorScroll {
		t.Fatalf("detail down changed inspectorScroll from %d to %d", detail.inspectorScroll, detailMoved.inspectorScroll)
	}
	if detailMoved.selectedIndex != selectedIndex {
		t.Fatalf("detail down moved nav selection from %d to %d", selectedIndex, detailMoved.selectedIndex)
	}

	next, cmd = detailMoved.Update(keyMsg("P"))
	meta := next.(model)
	if cmd != nil {
		t.Fatal("P returned a command")
	}
	next, cmd = meta.Update(tea.KeyMsg{Type: tea.KeyDown})
	metaScrolled := next.(model)
	if cmd != nil {
		t.Fatal("meta down returned a command")
	}
	if metaScrolled.focusedPane != inspectorPane {
		t.Fatalf("meta focus = %v, want inspectorPane", metaScrolled.focusedPane)
	}
	if metaScrolled.inspectorScroll != 1 {
		t.Fatalf("meta down set inspectorScroll = %d, want 1", metaScrolled.inspectorScroll)
	}
	if metaScrolled.selectedIndex != selectedIndex {
		t.Fatalf("meta down moved nav selection from %d to %d", selectedIndex, metaScrolled.selectedIndex)
	}
}

func TestEnterOnlyActivatesNavigationPane(t *testing.T) {
	t.Parallel()

	m, inspector := newFixtureModel(t, "companies.db")
	m = indexAll(t, m, inspector)

	next, cmd := m.Update(keyMsg("I"))
	loading := next.(model)
	next, _ = loading.Update(pageLoadedFromCmd(t, cmd))
	loaded := next.(model)

	loaded.focusedPane = explorerPane
	loaded.inspectorScroll = 3
	active := loaded.active
	selectedIndex := loaded.selectedIndex
	next, cmd = loaded.Update(tea.KeyMsg{Type: tea.KeyEnter})
	got := next.(model)
	if cmd != nil {
		t.Fatalf("enter in detail returned cmd %v, want nil", cmd)
	}
	if got.inspectorScroll != 3 {
		t.Fatalf("enter in detail changed inspectorScroll to %d, want 3", got.inspectorScroll)
	}
	if got.active != active || got.selectedIndex != selectedIndex {
		t.Fatalf("enter in detail changed active/selection: active=%+v selected=%d", got.active, got.selectedIndex)
	}

	got.focusedPane = inspectorPane
	next, cmd = got.Update(tea.KeyMsg{Type: tea.KeyEnter})
	got = next.(model)
	if cmd != nil {
		t.Fatalf("enter in meta returned cmd %v, want nil", cmd)
	}

	got.focusedPane = navPane
	next, cmd = got.Update(tea.KeyMsg{Type: tea.KeyEnter})
	got = next.(model)
	if cmd == nil {
		t.Fatal("enter in navigation did not activate selected page")
	}
	if got.inspectorScroll != 0 {
		t.Fatalf("enter in navigation left inspectorScroll = %d, want reset to 0", got.inspectorScroll)
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

	// Inside an open page, Esc resets transient page state without hidden navigation.
	m.active = contentTarget{kind: navPage}
	m.focusedPane = explorerPane
	m.loading = true
	m.inspectorScroll = 3
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	got := next.(model)
	if got.loading {
		t.Fatal("esc inside an open page left loading=true")
	}
	if got.inspectorScroll != 0 {
		t.Fatalf("esc inside an open page left inspectorScroll = %d, want 0", got.inspectorScroll)
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

	next, cmd := m.Update(keyMsg("I"))
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
	if strings.Contains(delayed.viewNavigationColumn(24, 20), "Loading page") {
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

	next, cmd := m.Update(keyMsg("I"))
	firstLoading := next.(model)
	next, _ = firstLoading.Update(pageLoadedFromCmd(t, cmd))
	loaded := next.(model)
	if loaded.currentPage == nil {
		t.Fatal("setup: first page did not load")
	}
	firstPage := loaded.currentPage.Ref.ID

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
	if scrolling.currentPage == nil || scrolling.currentPage.Ref.ID != firstPage {
		t.Fatalf("currentPage = %+v, want previous page %d preserved during delay", scrolling.currentPage, firstPage)
	}

	view := scrolling.View()
	if !strings.Contains(view, "[O] HEX") {
		t.Fatalf("view did not render the page pane as HEX during delay:\n%s", view)
	}
	if !strings.Contains(view, "Offset   00 01 02 03 04 05 06 07") ||
		!strings.Contains(view, "0000") ||
		!strings.Contains(view, "53 51 4C 69 74 65 20 66  6F 72 6D 61 74 20 33") {
		t.Fatalf("view did not keep previous page hex bytes visible during delay; want page %d:\n%s", firstPage, view)
	}
	if strings.Contains(view, "STRUCTURES") {
		t.Fatal("view still shows the previous page structure table during delay")
	}
	if strings.Contains(view, "Waiting for page details") || strings.Contains(view, "Loading page") {
		t.Fatal("view showed loading/empty placeholder during the delay")
	}
}

func TestPageHexPaneAndMeta(t *testing.T) {
	t.Parallel()

	m, inspector := newFixtureModel(t, "companies.db")
	m = indexAll(t, m, inspector)

	next, cmd := m.Update(keyMsg("I"))
	loading := next.(model)
	next, _ = loading.Update(pageLoadedFromCmd(t, cmd))
	loaded := next.(model)

	view := loaded.View()
	pagePane := loaded.viewExplorer(80, 8)
	meta := loaded.viewInspector(60, 20)
	for _, want := range []string{
		"[O] HEX",
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("page view missing %q:\n%s", want, view)
		}
	}
	for _, want := range []string{
		"Offset   00 01 02 03 04 05 06 07  08 09 0A 0B 0C 0D 0E 0F",
		"0000",
		"53 51 4C 69 74 65 20 66  6F 72 6D 61 74 20 33 00",
	} {
		if !strings.Contains(pagePane, want) {
			t.Fatalf("page hex pane missing %q:\n%s", want, pagePane)
		}
	}
	for _, want := range []string{
		"[P] META",
		"Page 1",
		"Type: leaf table",
		"Page size: 4096 bytes",
		"File offset: 0",
		"STRUCTURE",
		"Cells:",
		"Pointer array:",
	} {
		if !strings.Contains(view+"\n"+meta, want) {
			t.Fatalf("page meta missing %q:\n%s", want, meta)
		}
	}
	for _, dropped := range []string{"STRUCTURES", "RAW BYTES", "ASCII:"} {
		if strings.Contains(view, dropped) {
			t.Fatalf("page view still contains removed page detail %q:\n%s", dropped, view)
		}
	}
}

func TestPageViewKeepsFullHexRowsAtDefaultWidth(t *testing.T) {
	t.Parallel()

	m, inspector := newFixtureModel(t, "companies.db")
	m = indexAll(t, m, inspector)
	m.width = 120
	m.height = 34

	next, cmd := m.Update(keyMsg("I"))
	loading := next.(model)
	next, _ = loading.Update(pageLoadedFromCmd(t, cmd))
	loaded := next.(model)

	view := loaded.View()
	for _, want := range []string{
		"Offset   00 01 02 03 04 05 06 07  08 09 0A 0B 0C 0D 0E 0F",
		"53 51 4C 69 74 65 20 66  6F 72 6D 61 74 20 33 00",
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("120-column page view clipped full hex row %q:\n%s", want, view)
		}
	}
}

func TestFilteredPageMetaShowsBTreeSource(t *testing.T) {
	t.Parallel()

	m, inspector := newFixtureModel(t, "companies.db")
	m = indexAll(t, m, inspector)
	companies := objectByName(t, m.db, "companies")
	m.applyFilter(companies)

	next, cmd := m.Update(keyMsg("I"))
	loading := next.(model)
	next, _ = loading.Update(pageLoadedFromCmd(t, cmd))
	loaded := next.(model)

	meta := loaded.viewInspector(36, 20)
	for _, want := range []string{"BTREE", "Object: companies", "Root page: 2"} {
		if !strings.Contains(meta, want) {
			t.Fatalf("filtered page meta missing %q:\n%s", want, meta)
		}
	}
	if strings.Contains(meta, "ASCII:") || strings.Contains(meta, "RAW BYTES") {
		t.Fatalf("filtered page meta contains raw byte detail:\n%s", meta)
	}
}

func TestPageBlocksArePhysicalOrder(t *testing.T) {
	t.Parallel()

	m, inspector := newFixtureModel(t, "companies.db")
	m = indexAll(t, m, inspector)

	next, cmd := m.Update(keyMsg("I"))
	loading := next.(model)
	next, _ = loading.Update(pageLoadedFromCmd(t, cmd))
	loaded := next.(model)

	blocks := buildPageBlocks(loaded.currentPage)
	if len(blocks) < 4 {
		t.Fatalf("loaded page has %d blocks, want at least database header, page header, pointer array, and data", len(blocks))
	}
	if blocks[0].kind != pageBlockDatabaseHeader || blocks[0].meta.Start != 0 || blocks[0].meta.Size != 100 {
		t.Fatalf("first block = %+v, want 100-byte database header at offset 0", blocks[0])
	}
	if blocks[1].kind != pageBlockPageHeader || blocks[1].meta.Start != 100 {
		t.Fatalf("second block = %+v, want page header at offset 100", blocks[1])
	}
	for idx := 1; idx < len(blocks); idx++ {
		if blocks[idx].meta.Start < blocks[idx-1].meta.Start {
			t.Fatalf("blocks are not sorted physically at %d: prev=%+v current=%+v", idx, blocks[idx-1], blocks[idx])
		}
	}
}

func TestHexFocusAndMovementSelectTopLevelBlocks(t *testing.T) {
	t.Parallel()

	m, inspector := newFixtureModel(t, "companies.db")
	m = indexAll(t, m, inspector)

	next, cmd := m.Update(keyMsg("I"))
	loading := next.(model)
	next, _ = loading.Update(pageLoadedFromCmd(t, cmd))
	loaded := next.(model)

	next, cmd = loaded.Update(keyMsg("O"))
	hex := next.(model)
	if cmd != nil {
		t.Fatal("O returned a command")
	}
	if hex.focusedPane != explorerPane {
		t.Fatalf("O focused pane = %v, want explorerPane", hex.focusedPane)
	}
	if !hex.blockSelected || hex.selectedBlock != 0 {
		t.Fatalf("O selected block = (%v, %d), want first block", hex.blockSelected, hex.selectedBlock)
	}
	if meta := hex.viewInspector(48, 12); !strings.Contains(meta, "Database Header") || !strings.Contains(meta, "Page size:") {
		t.Fatalf("hex focus did not switch META to first block:\n%s", meta)
	}

	hex.inspectorScroll = 3
	next, cmd = hex.Update(tea.KeyMsg{Type: tea.KeyDown})
	moved := next.(model)
	if cmd != nil {
		t.Fatal("hex down returned a command")
	}
	if moved.selectedBlock != 1 {
		t.Fatalf("hex down selected block %d, want 1", moved.selectedBlock)
	}
	if moved.inspectorScroll != 0 {
		t.Fatalf("hex movement left inspectorScroll = %d, want reset", moved.inspectorScroll)
	}
	if meta := moved.viewInspector(48, 12); !strings.Contains(meta, "Page Header") || !strings.Contains(meta, "Page kind:") {
		t.Fatalf("hex movement did not switch META to page header:\n%s", meta)
	}

	next, cmd = moved.Update(keyMsg("P"))
	metaFocused := next.(model)
	if cmd != nil {
		t.Fatal("P returned a command")
	}
	if metaFocused.focusedPane != inspectorPane || metaFocused.selectedBlock != moved.selectedBlock {
		t.Fatalf("P changed focus/selection to pane=%v block=%d", metaFocused.focusedPane, metaFocused.selectedBlock)
	}
	next, _ = metaFocused.Update(tea.KeyMsg{Type: tea.KeyDown})
	scrolled := next.(model)
	if scrolled.selectedBlock != metaFocused.selectedBlock {
		t.Fatalf("META scroll changed selected block from %d to %d", metaFocused.selectedBlock, scrolled.selectedBlock)
	}
	if scrolled.inspectorScroll != 1 {
		t.Fatalf("META scroll = %d, want 1", scrolled.inspectorScroll)
	}
}

func TestMetaToHexPreservesBlockSelection(t *testing.T) {
	t.Parallel()

	m, inspector := newFixtureModel(t, "companies.db")
	m = indexAll(t, m, inspector)

	next, cmd := m.Update(keyMsg("I"))
	loading := next.(model)
	next, _ = loading.Update(pageLoadedFromCmd(t, cmd))
	loaded := next.(model)
	next, _ = loaded.Update(keyMsg("O"))
	hex := next.(model)
	next, _ = hex.Update(tea.KeyMsg{Type: tea.KeyDown})
	hex = next.(model)
	selectedBlock := hex.selectedBlock

	next, cmd = hex.Update(keyMsg("P"))
	meta := next.(model)
	if cmd != nil {
		t.Fatal("P returned a command")
	}
	if meta.focusedPane != inspectorPane {
		t.Fatalf("P focused pane = %v, want inspectorPane", meta.focusedPane)
	}

	next, cmd = meta.Update(keyMsg("O"))
	back := next.(model)
	if cmd != nil {
		t.Fatal("O from META returned a command")
	}
	if back.focusedPane != explorerPane {
		t.Fatalf("O from META focused pane = %v, want explorerPane", back.focusedPane)
	}
	if !back.blockSelected || back.selectedBlock != selectedBlock {
		t.Fatalf("O from META selected block = (%v, %d), want (%v, %d)", back.blockSelected, back.selectedBlock, true, selectedBlock)
	}
}

func TestMetaToHexPreservesDrillSelection(t *testing.T) {
	t.Parallel()

	m, inspector := newFixtureModel(t, "companies.db")
	m = indexAll(t, m, inspector)

	next, cmd := m.Update(keyMsg("I"))
	loading := next.(model)
	next, _ = loading.Update(pageLoadedFromCmd(t, cmd))
	loaded := next.(model)
	next, _ = loaded.Update(keyMsg("O"))
	hex := next.(model)

	parent := selectFirstDrillableBlock(t, &hex)
	next, _ = hex.Update(keyMsg("d"))
	drilled := next.(model)
	next, _ = drilled.Update(tea.KeyMsg{Type: tea.KeyDown})
	drilled = next.(model)
	stackDepth := len(drilled.drill.stack)
	selectedChild := drilled.drill.stack[stackDepth-1].selectedChild

	next, cmd = drilled.Update(keyMsg("P"))
	meta := next.(model)
	if cmd != nil {
		t.Fatal("P returned a command")
	}
	next, cmd = meta.Update(keyMsg("O"))
	back := next.(model)
	if cmd != nil {
		t.Fatal("O from META returned a command")
	}
	if back.focusedPane != explorerPane {
		t.Fatalf("O from META focused pane = %v, want explorerPane", back.focusedPane)
	}
	if !back.drill.active || back.drill.parentBlock != parent {
		t.Fatalf("O from META changed drill parent/state to %+v, want parent %d active", back.drill, parent)
	}
	if len(back.drill.stack) != stackDepth {
		t.Fatalf("O from META changed drill depth to %d, want %d", len(back.drill.stack), stackDepth)
	}
	if got := back.drill.stack[len(back.drill.stack)-1].selectedChild; got != selectedChild {
		t.Fatalf("O from META changed selected drill child to %d, want %d", got, selectedChild)
	}
	if back.selectedBlock != parent {
		t.Fatalf("O from META selected block %d, want parent %d", back.selectedBlock, parent)
	}
}

func TestPagesToMetaShowsPageMetadataAfterHexDrillActivity(t *testing.T) {
	t.Parallel()

	m, inspector := newFixtureModel(t, "companies.db")
	m = indexAll(t, m, inspector)

	next, cmd := m.Update(keyMsg("I"))
	loading := next.(model)
	next, _ = loading.Update(pageLoadedFromCmd(t, cmd))
	loaded := next.(model)
	next, _ = loaded.Update(keyMsg("O"))
	hex := next.(model)
	selectFirstDrillableBlock(t, &hex)
	next, _ = hex.Update(keyMsg("d"))
	drilled := next.(model)

	drilled.focusedPane = navPane
	next, cmd = drilled.Update(keyMsg("P"))
	meta := next.(model)
	if cmd != nil {
		t.Fatal("P from PAGES returned a command")
	}
	if meta.focusedPane != inspectorPane {
		t.Fatalf("P from PAGES focused pane = %v, want inspectorPane", meta.focusedPane)
	}
	content := meta.viewInspector(52, 14)
	for _, want := range []string{"Page 1", "STRUCTURE", "Cells:", "Pointer array:"} {
		if !strings.Contains(content, want) {
			t.Fatalf("page META missing %q after drill activity:\n%s", want, content)
		}
	}
	if strings.Contains(content, "Parent: Cell") || strings.Contains(content, "Payload Size") {
		t.Fatalf("P from PAGES showed drill metadata instead of page metadata:\n%s", content)
	}
}

func TestCellBlockMetaShowsParsedValues(t *testing.T) {
	t.Parallel()

	m, inspector := newFixtureModel(t, "companies.db")
	m = indexAll(t, m, inspector)

	next, cmd := m.Update(keyMsg("I"))
	loading := next.(model)
	next, _ = loading.Update(pageLoadedFromCmd(t, cmd))
	loaded := next.(model)

	var cellBlock pageBlock
	found := false
	for _, block := range buildPageBlocks(loaded.currentPage) {
		if block.kind == pageBlockTableLeafCell || block.kind == pageBlockIndexLeafCell || block.kind == pageBlockIndexInteriorCell {
			cellBlock = block
			found = true
			break
		}
	}
	if !found {
		t.Fatal("fixture page has no payload-carrying cell block")
	}

	meta := strings.Join(blockMetaLines(cellBlock, loaded.currentPage), "\n")
	for _, want := range []string{"VALUES", "00:", "serial"} {
		if !strings.Contains(meta, want) {
			t.Fatalf("cell block meta missing parsed value token %q:\n%s", want, meta)
		}
	}
	if !strings.Contains(meta, "\"") && !strings.Contains(meta, "NULL") {
		t.Fatalf("cell block meta does not show decoded scalar values:\n%s", meta)
	}
}

func TestCellDrillToggleMovementAndMeta(t *testing.T) {
	t.Parallel()

	m, inspector := newFixtureModel(t, "companies.db")
	m = indexAll(t, m, inspector)

	next, cmd := m.Update(keyMsg("I"))
	loading := next.(model)
	next, _ = loading.Update(pageLoadedFromCmd(t, cmd))
	loaded := next.(model)
	next, _ = loaded.Update(keyMsg("O"))
	hex := next.(model)

	parent := selectFirstDrillableBlock(t, &hex)
	children := buildDrillChildren(hex.currentPageBlocks()[parent], hex.currentPage)
	for _, want := range []string{"Payload Size", "Record Payload"} {
		if !hasDrillChildTitle(children, want) {
			t.Fatalf("drill children missing %q: %+v", want, children)
		}
	}
	payloadIdx := drillChildIndex(children, "Record Payload")
	if payloadIdx < 0 {
		t.Fatal("drill children missing Record Payload")
	}
	for _, want := range []string{"Record Header Size", "Serial Type 1", "Value 1"} {
		if !hasDrillChildTitle(children[payloadIdx].children, want) {
			t.Fatalf("record payload children missing %q: %+v", want, children[payloadIdx].children)
		}
	}

	next, cmd = hex.Update(keyMsg("d"))
	drilled := next.(model)
	if cmd != nil {
		t.Fatal("d returned a command")
	}
	if !drilled.drill.active {
		t.Fatal("d on a drillable cell did not enter drill mode")
	}
	if drilled.drill.parentBlock != parent || drilled.selectedBlock != parent {
		t.Fatalf("drill parent/selected block = %d/%d, want %d", drilled.drill.parentBlock, drilled.selectedBlock, parent)
	}
	if len(drilled.drill.stack) != 1 {
		t.Fatalf("drill stack depth = %d, want 1", len(drilled.drill.stack))
	}

	first, ok := drilled.selectedDrillChild()
	if !ok {
		t.Fatal("drill mode has no selected child")
	}
	meta := drilled.viewInspector(52, 14)
	for _, want := range []string{first.title, "Parent: Cell", "Offset:", "Size:", "PARSED"} {
		if !strings.Contains(meta, want) {
			t.Fatalf("drill meta missing %q:\n%s", want, meta)
		}
	}

	second := children[1]
	rowOffset := (second.meta.Start / 16) * 16
	rowEnd := rowOffset + 16
	if rowEnd > len(drilled.currentPage.Raw) {
		rowEnd = len(drilled.currentPage.Raw)
	}
	rendered := formatHexRowWithSelection(rowOffset, drilled.currentPage.Raw[rowOffset:rowEnd], drilled.currentPageBlocks(), first.meta, true, drilled.currentDrillChildren())
	wantSiblingByte := drillChildStyle(second.kind).Render(fmt.Sprintf("%02X", drilled.currentPage.Raw[second.meta.Start]))
	if !strings.Contains(rendered, wantSiblingByte) {
		t.Fatalf("unselected drill sibling byte did not use drill child style:\n%s", rendered)
	}

	next, cmd = drilled.Update(tea.KeyMsg{Type: tea.KeyDown})
	moved := next.(model)
	if cmd != nil {
		t.Fatal("drill down returned a command")
	}
	if moved.drill.stack[len(moved.drill.stack)-1].selectedChild != drilled.drill.stack[len(drilled.drill.stack)-1].selectedChild+1 {
		t.Fatalf("drill down selected child %d, want %d", moved.drill.stack[len(moved.drill.stack)-1].selectedChild, drilled.drill.stack[len(drilled.drill.stack)-1].selectedChild+1)
	}
	selectedAfterMove, ok := moved.selectedDrillChild()
	if !ok {
		t.Fatal("drill down left no selected child")
	}
	if selectedAfterMove.title == first.title && selectedAfterMove.meta == first.meta {
		t.Fatalf("drill down did not change selected child: %+v", selectedAfterMove)
	}
	if meta := moved.viewInspector(52, 14); !strings.Contains(meta, selectedAfterMove.title) {
		t.Fatalf("drill movement did not update META to %q:\n%s", selectedAfterMove.title, meta)
	}

	for moved.drill.stack[len(moved.drill.stack)-1].selectedChild < payloadIdx {
		next, _ = moved.Update(tea.KeyMsg{Type: tea.KeyDown})
		moved = next.(model)
	}
	selectedPayload, ok := moved.selectedDrillChild()
	if !ok || selectedPayload.title != "Record Payload" {
		t.Fatalf("selected child = %+v, want Record Payload", selectedPayload)
	}

	next, cmd = moved.Update(keyMsg("d"))
	nested := next.(model)
	if cmd != nil {
		t.Fatal("d on Record Payload returned a command")
	}
	if !nested.drill.active || len(nested.drill.stack) != 2 {
		t.Fatalf("d on Record Payload did not enter nested drill; state=%+v", nested.drill)
	}
	nestedChild, ok := nested.selectedDrillChild()
	if !ok || nestedChild.title != "Record Header Size" {
		t.Fatalf("nested selected child = %+v, want Record Header Size", nestedChild)
	}
	if meta := nested.viewInspector(52, 14); !strings.Contains(meta, "Parent: Record Payload") {
		t.Fatalf("nested drill meta missing payload parent:\n%s", meta)
	}

	rowOffset = (nestedChild.meta.Start / 16) * 16
	rowEnd = rowOffset + 16
	if rowEnd > len(nested.currentPage.Raw) {
		rowEnd = len(nested.currentPage.Raw)
	}
	rendered = formatHexRowWithSelection(rowOffset, nested.currentPage.Raw[rowOffset:rowEnd], nested.currentPageBlocks(), nestedChild.meta, true, nested.currentDrillChildren())
	wantByte := selectedHexByteStyle.Render(fmt.Sprintf("%02X", nested.currentPage.Raw[nestedChild.meta.Start]))
	if !strings.Contains(rendered, wantByte) {
		t.Fatalf("selected nested drill child byte did not use selected style:\n%s", rendered)
	}

	next, cmd = nested.Update(keyMsg("d"))
	stillNested := next.(model)
	if cmd != nil {
		t.Fatal("d on nested leaf returned a command")
	}
	if !stillNested.drill.active || len(stillNested.drill.stack) != 2 {
		t.Fatalf("d on nested leaf changed drill state; state=%+v", stillNested.drill)
	}

	next, cmd = stillNested.Update(keyMsg("b"))
	backToCell := next.(model)
	if cmd != nil {
		t.Fatal("b in nested drill returned a command")
	}
	if !backToCell.drill.active || len(backToCell.drill.stack) != 1 {
		t.Fatalf("b in nested drill did not return to parent drill; state=%+v", backToCell.drill)
	}

	backToCell.drill.stack[0].selectedChild = 0
	next, cmd = backToCell.Update(keyMsg("d"))
	leafNoop := next.(model)
	if cmd != nil {
		t.Fatal("d on non-nested drill child returned a command")
	}
	if !leafNoop.drill.active || len(leafNoop.drill.stack) != 1 {
		t.Fatalf("d on non-nested leaf changed drill state; state=%+v", leafNoop.drill)
	}

	next, cmd = leafNoop.Update(keyMsg("b"))
	exited := next.(model)
	if cmd != nil {
		t.Fatal("b at top drill layer returned a command")
	}
	if exited.drill.active {
		t.Fatal("b at top drill layer did not exit drill mode")
	}
	if exited.selectedBlock != parent {
		t.Fatalf("exiting drill selected block %d, want parent %d", exited.selectedBlock, parent)
	}
}

func TestFooterDrillHintsAreContextual(t *testing.T) {
	t.Parallel()

	m, inspector := newFixtureModel(t, "companies.db")
	m = indexAll(t, m, inspector)

	if strings.Contains(m.footerLine(), "d drill") || strings.Contains(m.footerLine(), "b back") {
		t.Fatalf("footer shows drill hints before a drillable page selection: %q", m.footerLine())
	}

	next, cmd := m.Update(keyMsg("I"))
	loading := next.(model)
	next, _ = loading.Update(pageLoadedFromCmd(t, cmd))
	loaded := next.(model)
	next, _ = loaded.Update(keyMsg("O"))
	hex := next.(model)

	hex.selectedBlock = 0
	hex.blockSelected = true
	if strings.Contains(hex.footerLine(), "d drill") || strings.Contains(hex.footerLine(), "b back") {
		t.Fatalf("footer shows drill hints on non-drillable block: %q", hex.footerLine())
	}

	selectFirstDrillableBlock(t, &hex)
	if !strings.Contains(hex.footerLine(), "d drill") {
		t.Fatalf("footer missing drill hint on drillable cell: %q", hex.footerLine())
	}
	if strings.Contains(hex.footerLine(), "b back") {
		t.Fatalf("footer shows back before entering drill: %q", hex.footerLine())
	}

	next, _ = hex.Update(keyMsg("d"))
	drilled := next.(model)
	if strings.Contains(drilled.footerLine(), "d drill") {
		t.Fatalf("footer shows drill hint on leaf drill child: %q", drilled.footerLine())
	}
	if !strings.Contains(drilled.footerLine(), "b back") {
		t.Fatalf("footer missing back hint while drilled: %q", drilled.footerLine())
	}

	payloadIdx := drillChildIndex(drilled.currentDrillChildren(), "Record Payload")
	for drilled.drill.stack[len(drilled.drill.stack)-1].selectedChild < payloadIdx {
		next, _ = drilled.Update(tea.KeyMsg{Type: tea.KeyDown})
		drilled = next.(model)
	}
	if !strings.Contains(drilled.footerLine(), "d drill") || !strings.Contains(drilled.footerLine(), "b back") {
		t.Fatalf("footer should show both hints on nested drillable child: %q", drilled.footerLine())
	}

	next, _ = drilled.Update(keyMsg("d"))
	nested := next.(model)
	if !strings.Contains(nested.footerLine(), "b back") {
		t.Fatalf("footer missing back hint in nested drill: %q", nested.footerLine())
	}
	if strings.Contains(nested.footerLine(), "d drill") {
		t.Fatalf("footer shows drill hint on nested leaf: %q", nested.footerLine())
	}
}

func TestFooterFilterHintIsContextualToBTrees(t *testing.T) {
	t.Parallel()

	m, inspector := newFixtureModel(t, "companies.db")
	m = indexAll(t, m, inspector)

	next, _ := m.Update(keyMsg("U"))
	btrees := next.(model)
	if !strings.Contains(btrees.footerLine(), "f filter") {
		t.Fatalf("footer missing filter hint in B-TREES: %q", btrees.footerLine())
	}

	next, cmd := btrees.Update(keyMsg("I"))
	pages := next.(model)
	if cmd == nil {
		t.Fatal("I did not activate a page")
	}
	if strings.Contains(pages.footerLine(), "f filter") || strings.Contains(pages.footerLine(), "f clear/switch") {
		t.Fatalf("footer shows filter hint outside B-TREES: %q", pages.footerLine())
	}

	next, _ = pages.Update(pageLoadedFromCmd(t, cmd))
	loaded := next.(model)
	next, _ = loaded.Update(keyMsg("O"))
	hex := next.(model)
	if strings.Contains(hex.footerLine(), "f filter") || strings.Contains(hex.footerLine(), "f clear/switch") {
		t.Fatalf("footer shows filter hint in HEX view: %q", hex.footerLine())
	}
}

func TestDrillNoOpAndPageChangeReset(t *testing.T) {
	t.Parallel()

	m, inspector := newFixtureModel(t, "companies.db")
	m = indexAll(t, m, inspector)

	next, cmd := m.Update(keyMsg("I"))
	loading := next.(model)
	next, _ = loading.Update(pageLoadedFromCmd(t, cmd))
	loaded := next.(model)
	next, _ = loaded.Update(keyMsg("O"))
	hex := next.(model)

	hex.selectedBlock = 0
	hex.blockSelected = true
	next, cmd = hex.Update(keyMsg("d"))
	got := next.(model)
	if cmd != nil {
		t.Fatal("d on non-drillable block returned a command")
	}
	if got.drill.active {
		t.Fatal("d on non-drillable block entered drill mode")
	}
	if got.selectedBlock != 0 || !got.blockSelected {
		t.Fatalf("d on non-drillable block changed selection to (%v, %d)", got.blockSelected, got.selectedBlock)
	}

	parent := selectFirstDrillableBlock(t, &got)
	next, _ = got.Update(keyMsg("d"))
	drilled := next.(model)
	if !drilled.drill.active || drilled.drill.parentBlock != parent {
		t.Fatal("setup: failed to enter drill mode")
	}

	drilled.focusedPane = navPane
	next, cmd = drilled.Update(tea.KeyMsg{Type: tea.KeyDown})
	movingPage := next.(model)
	if cmd == nil {
		t.Fatal("moving to next page did not return a load command")
	}
	if movingPage.drill.active {
		t.Fatal("page movement left drill mode active")
	}
	if movingPage.blockSelected {
		t.Fatal("page movement left a selected hex block")
	}
}

func TestBboltDescriptorFieldDrillNavigationAndHighlight(t *testing.T) {
	t.Parallel()

	raw := make([]byte, 32)
	for idx := range raw {
		raw[idx] = byte(idx)
	}
	page := &storage.PageInspection{
		Ref: storage.PageRef{ID: 1},
		Raw: raw,
		HexBlocks: []storage.HexBlock{{
			Kind:  pageBlockLeafDescriptors,
			Title: "Leaf Descriptors",
			Span:  storage.ByteSpan{Start: 0, Size: 16},
			Children: []storage.HexBlock{{
				Kind:  drillChildLeafDescriptor,
				Title: "Leaf Entry 0 Descriptor",
				Span:  storage.ByteSpan{Start: 0, Size: 16},
				Children: []storage.HexBlock{
					{Kind: drillChildDescriptorFlags, Title: "Flags", Span: storage.ByteSpan{Start: 0, Size: 4}},
					{Kind: drillChildDescriptorPos, Title: "Position", Span: storage.ByteSpan{Start: 4, Size: 4}},
					{Kind: drillChildDescriptorKeySz, Title: "Key size", Span: storage.ByteSpan{Start: 8, Size: 4}},
					{Kind: drillChildDescriptorValSz, Title: "Value size", Span: storage.ByteSpan{Start: 12, Size: 4}},
				},
			}},
		}},
	}
	m := model{currentPage: page, selectedBlock: 0, blockSelected: true, width: 80, height: 24}

	m.drillIn()
	descriptor, ok := m.selectedDrillChild()
	if !ok || descriptor.kind != drillChildLeafDescriptor {
		t.Fatalf("selected descriptor child = %+v, want leaf descriptor", descriptor)
	}
	m.drillIn()
	field, ok := m.selectedDrillChild()
	if !ok || field.kind != drillChildDescriptorFlags {
		t.Fatalf("selected descriptor field = %+v, want flags", field)
	}
	if !m.moveDrillChild(1) {
		t.Fatal("moving between descriptor fields returned false")
	}
	field, ok = m.selectedDrillChild()
	if !ok || field.kind != drillChildDescriptorPos || field.meta != (storage.ByteSpan{Start: 4, Size: 4}) {
		t.Fatalf("selected descriptor field = %+v, want position bytes 4..7", field)
	}

	rendered := formatHexRowWithSelection(0, raw[:16], m.currentPageBlocks(), field.meta, true, m.currentDrillChildren())
	if want := selectedHexByteStyle.Render("04"); !strings.Contains(rendered, want) {
		t.Fatalf("selected descriptor field byte did not use selected style:\n%s", rendered)
	}
}

func TestBboltBucketValueFieldDrillNavigationAndHighlight(t *testing.T) {
	t.Parallel()

	raw := make([]byte, 64)
	for idx := range raw {
		raw[idx] = byte(idx)
	}
	page := &storage.PageInspection{
		Ref: storage.PageRef{ID: 1},
		Raw: raw,
		HexBlocks: []storage.HexBlock{{
			Kind:  pageBlockLeafEntry,
			Title: "Leaf Entry 0",
			Span:  storage.ByteSpan{Start: 0, Size: 32},
			Children: []storage.HexBlock{
				{Kind: drillChildLeafKey, Title: "Leaf Entry 0 Key", Span: storage.ByteSpan{Start: 0, Size: 4}},
				{
					Kind:  drillChildLeafValue,
					Title: "Leaf Entry 0 Value",
					Span:  storage.ByteSpan{Start: 16, Size: 32},
					Children: []storage.HexBlock{
						{Kind: drillChildBucketRootPage, Title: "Root page", Span: storage.ByteSpan{Start: 16, Size: 8}},
						{Kind: drillChildBucketSequence, Title: "Sequence", Span: storage.ByteSpan{Start: 24, Size: 8}},
						{Kind: pageBlockPageHeader, Title: "Inline Page Header", Span: storage.ByteSpan{Start: 32, Size: 16}},
					},
				},
			},
		}},
	}
	m := model{currentPage: page, selectedBlock: 0, blockSelected: true, width: 80, height: 24}

	m.drillIn()
	if !m.moveDrillChild(1) {
		t.Fatal("moving from leaf key to value returned false")
	}
	value, ok := m.selectedDrillChild()
	if !ok || value.kind != drillChildLeafValue {
		t.Fatalf("selected leaf child = %+v, want value", value)
	}
	m.drillIn()
	root, ok := m.selectedDrillChild()
	if !ok || root.kind != drillChildBucketRootPage || root.meta != (storage.ByteSpan{Start: 16, Size: 8}) {
		t.Fatalf("selected bucket field = %+v, want root page bytes 16..23", root)
	}
	if m.currentDrillChildren()[0].kind != drillChildBucketRootPage || m.currentDrillChildren()[1].kind != drillChildBucketSequence || m.currentDrillChildren()[2].kind != pageBlockPageHeader {
		t.Fatalf("bucket value children order = %+v, want root, sequence, inline page header", m.currentDrillChildren())
	}
	if !m.moveDrillChild(1) {
		t.Fatal("moving between bucket value fields returned false")
	}
	sequence, ok := m.selectedDrillChild()
	if !ok || sequence.kind != drillChildBucketSequence {
		t.Fatalf("selected bucket field = %+v, want sequence", sequence)
	}

	rendered := formatHexRowWithSelection(16, raw[16:32], m.currentPageBlocks(), sequence.meta, true, m.currentDrillChildren())
	if want := selectedHexByteStyle.Render("18"); !strings.Contains(rendered, want) {
		t.Fatalf("selected bucket field byte did not use selected style:\n%s", rendered)
	}
}

func TestDrillSubtypeStylesAreContrasting(t *testing.T) {
	t.Parallel()

	rendered := map[string]string{
		"payload size": fmt.Sprint(drillChildStyle(drillChildPayloadSize).GetForeground()),
		"rowid":        fmt.Sprint(drillChildStyle(drillChildRowID).GetForeground()),
		"payload":      fmt.Sprint(drillChildStyle(drillChildRecordPayload).GetForeground()),
	}

	for leftName, left := range rendered {
		for rightName, right := range rendered {
			if leftName >= rightName {
				continue
			}
			if left == right {
				t.Fatalf("%s and %s render with the same style %q", leftName, rightName, left)
			}
		}
	}
}

func TestBboltLeafStylesAreContrasting(t *testing.T) {
	t.Parallel()

	topLevel := map[string]string{
		"descriptors": fmt.Sprint(blockStyle(pageBlockLeafDescriptors).GetForeground()),
		"entry":       fmt.Sprint(blockStyle(pageBlockLeafEntry).GetForeground()),
	}
	for leftName, left := range topLevel {
		for rightName, right := range topLevel {
			if leftName >= rightName {
				continue
			}
			if left == right {
				t.Fatalf("%s and %s render with the same top-level style %q", leftName, rightName, left)
			}
		}
	}

	drill := map[string]string{
		"descriptor": fmt.Sprint(drillChildStyle(drillChildLeafDescriptor).GetForeground()),
		"key":        fmt.Sprint(drillChildStyle(drillChildLeafKey).GetForeground()),
		"value":      fmt.Sprint(drillChildStyle(drillChildLeafValue).GetForeground()),
	}
	for leftName, left := range drill {
		for rightName, right := range drill {
			if leftName >= rightName {
				continue
			}
			if left == right {
				t.Fatalf("%s and %s render with the same drill style %q", leftName, rightName, left)
			}
		}
	}
}

func TestBboltBranchStylesAreNotUnknown(t *testing.T) {
	t.Parallel()

	unknown := fmt.Sprint(unknownHexByteStyle.GetForeground())
	topLevel := map[string]string{
		"descriptors": fmt.Sprint(blockStyle(pageBlockBranchDescriptors).GetForeground()),
		"descriptor":  fmt.Sprint(blockStyle(pageBlockBranchDescriptor).GetForeground()),
		"entry":       fmt.Sprint(blockStyle(pageBlockBranchEntry).GetForeground()),
	}
	for name, color := range topLevel {
		if color == unknown {
			t.Fatalf("branch %s block uses unknown hex style %q", name, color)
		}
		if color == "" {
			t.Fatalf("branch %s block has no foreground style", name)
		}
	}

	drill := map[string]string{
		"descriptor": fmt.Sprint(drillChildStyle(drillChildBranchDescriptor).GetForeground()),
		"entry":      fmt.Sprint(drillChildStyle(drillChildBranchEntry).GetForeground()),
	}
	for name, color := range drill {
		if color == unknown {
			t.Fatalf("branch %s drill child uses unknown hex style %q", name, color)
		}
		if color == "" {
			t.Fatalf("branch %s drill child has no foreground style", name)
		}
	}
}

func TestMetaPayloadBlockStyleIsNotUnknown(t *testing.T) {
	t.Parallel()

	got := fmt.Sprint(blockStyle(pageBlockMetaPayload).GetForeground())
	if got == fmt.Sprint(unknownHexByteStyle.GetForeground()) {
		t.Fatalf("meta payload block uses unknown hex style %q", got)
	}
	if got == "" {
		t.Fatal("meta payload block has no foreground style")
	}
}

func TestHexSelectionRenderingAndScrollReveal(t *testing.T) {
	t.Parallel()

	m, inspector := newFixtureModel(t, "companies.db")
	m = indexAll(t, m, inspector)
	m.height = 8

	next, cmd := m.Update(keyMsg("I"))
	loading := next.(model)
	next, _ = loading.Update(pageLoadedFromCmd(t, cmd))
	loaded := next.(model)

	next, _ = loaded.Update(keyMsg("O"))
	hex := next.(model)
	blocks := hex.currentPageBlocks()
	if len(blocks) < 5 {
		t.Fatalf("loaded page has %d blocks, want enough blocks to test scrolling", len(blocks))
	}

	hex.selectedBlock = len(blocks) - 1
	hex.blockSelected = true
	hex.revealSelectedHexBlock()
	last := blocks[len(blocks)-1]
	if hex.selectedBlock != len(blocks)-1 {
		t.Fatalf("selected block %d, want last block %d", hex.selectedBlock, len(blocks)-1)
	}
	if last.meta.Start >= 16 && hex.hexScroll == 0 {
		t.Fatal("selecting a later block did not advance hexScroll")
	}

	view := hex.viewExplorer(80, 4)
	rowOffset := (last.meta.Start / 16) * 16
	if !strings.Contains(view, fmt.Sprintf("%04X", rowOffset)) {
		t.Fatalf("hex viewport did not reveal selected block starting at %d:\n%s", last.meta.Start, view)
	}

	rowEnd := rowOffset + 16
	if rowEnd > len(hex.currentPage.Raw) {
		rowEnd = len(hex.currentPage.Raw)
	}
	rendered := formatHexRow(rowOffset, hex.currentPage.Raw[rowOffset:rowEnd], blocks, hex.selectedBlock)
	wantByte := selectedHexByteStyle.Render(fmt.Sprintf("%02X", hex.currentPage.Raw[last.meta.Start]))
	if !strings.Contains(rendered, wantByte) {
		t.Fatalf("selected block byte did not use selected style:\n%s", rendered)
	}
}

func TestHexRevealAllowsScrollingWithinLargeSelectedBlock(t *testing.T) {
	t.Parallel()

	meta := storage.ByteSpan{Start: 0, Size: 4096}
	if got := revealHexMetaScroll(5, meta, 4); got != 5 {
		t.Fatalf("scroll inside selected block = %d, want 5", got)
	}
	if got := revealHexMetaScroll(300, meta, 4); got != 252 {
		t.Fatalf("scroll past selected block = %d, want 252", got)
	}

	later := storage.ByteSpan{Start: 160, Size: 4096}
	if got := revealHexMetaScroll(0, later, 4); got != 10 {
		t.Fatalf("scroll before selected block = %d, want selected start row 10", got)
	}
}

func TestHexPaneScrollsWhenSingleLargeBlockCannotMoveSelection(t *testing.T) {
	t.Parallel()

	raw := make([]byte, 4096)
	m := model{
		active:        contentTarget{kind: navPage, pageNumber: 7},
		currentPage:   &storage.PageInspection{Ref: storage.PageRef{ID: 7}, Raw: raw, HexBlocks: []storage.HexBlock{{Kind: "bbolt_overflow_extent", Title: "Overflow Extent", Span: storage.ByteSpan{Start: 0, Size: len(raw)}}}},
		navItems:      []navItem{{kind: navPage, title: "page 7", pageNumber: 7}},
		focusedPane:   explorerPane,
		selectedBlock: 0,
		blockSelected: true,
		height:        8,
	}

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	scrolled := next.(model)
	if cmd != nil {
		t.Fatal("hex down returned a command")
	}
	if scrolled.selectedBlock != 0 || !scrolled.blockSelected {
		t.Fatalf("hex down changed selected block to (%v, %d)", scrolled.blockSelected, scrolled.selectedBlock)
	}
	if scrolled.hexScroll != 1 {
		t.Fatalf("hex down scroll = %d, want 1", scrolled.hexScroll)
	}

	next, _ = scrolled.Update(tea.KeyMsg{Type: tea.KeyPgDown})
	paged := next.(model)
	if paged.hexScroll != 9 {
		t.Fatalf("hex pgdown scroll = %d, want 9", paged.hexScroll)
	}
	track := strings.Join(paged.hexScrollbarTrack(6), "\n")
	if !strings.Contains(track, "│") || !strings.Contains(track, "█") {
		t.Fatalf("hex viewport did not expose scrollbar track:\n%s", track)
	}
}

func TestHexPaneScrollsWithinLargeSelectedBlockBeforeMovingSelection(t *testing.T) {
	t.Parallel()

	raw := make([]byte, 8192)
	m := model{
		active: contentTarget{kind: navPage, pageNumber: 7},
		currentPage: &storage.PageInspection{
			Ref: storage.PageRef{ID: 7},
			Raw: raw,
			HexBlocks: []storage.HexBlock{
				{Kind: "leaf_value", Title: "Leaf Value", Span: storage.ByteSpan{Start: 0, Size: 4096}},
				{Kind: "leaf_entry", Title: "Leaf Entry 1", Span: storage.ByteSpan{Start: 4096, Size: 16}},
			},
		},
		navItems:      []navItem{{kind: navPage, title: "page 7", pageNumber: 7}},
		focusedPane:   explorerPane,
		selectedBlock: 0,
		blockSelected: true,
		height:        8,
	}

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	scrolled := next.(model)
	if cmd != nil {
		t.Fatal("hex down returned a command")
	}
	if scrolled.selectedBlock != 0 {
		t.Fatalf("hex down selected block %d, want to stay on large block", scrolled.selectedBlock)
	}
	if scrolled.hexScroll != 1 {
		t.Fatalf("hex down scroll = %d, want 1", scrolled.hexScroll)
	}

	scrolled.hexScroll = 253
	next, _ = scrolled.Update(tea.KeyMsg{Type: tea.KeyDown})
	moved := next.(model)
	if moved.selectedBlock != 1 {
		t.Fatalf("hex down at selected block end selected block %d, want 1", moved.selectedBlock)
	}
}

func TestHexPaneScrollsWithinLargeSelectedDrillChildBeforeMovingSelection(t *testing.T) {
	t.Parallel()

	raw := make([]byte, 8192)
	m := model{
		active: contentTarget{kind: navPage, pageNumber: 7},
		currentPage: &storage.PageInspection{
			Ref: storage.PageRef{ID: 7},
			Raw: raw,
			HexBlocks: []storage.HexBlock{{
				Kind:  "leaf_entry",
				Title: "Leaf Entry",
				Span:  storage.ByteSpan{Start: 0, Size: 4112},
				Children: []storage.HexBlock{
					{Kind: "leaf_value", Title: "Leaf Value", Span: storage.ByteSpan{Start: 0, Size: 4096}},
					{Kind: "leaf_key", Title: "Leaf Key", Span: storage.ByteSpan{Start: 4096, Size: 16}},
				},
			}},
		},
		navItems:      []navItem{{kind: navPage, title: "page 7", pageNumber: 7}},
		focusedPane:   explorerPane,
		selectedBlock: 0,
		blockSelected: true,
		drill: drillState{
			active:      true,
			parentBlock: 0,
			stack: []drillFrame{{
				title: "Leaf Entry",
				children: []drillChild{
					{kind: "leaf_value", title: "Leaf Value", meta: storage.ByteSpan{Start: 0, Size: 4096}},
					{kind: "leaf_key", title: "Leaf Key", meta: storage.ByteSpan{Start: 4096, Size: 16}},
				},
			}},
		},
		height: 8,
	}

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	scrolled := next.(model)
	if cmd != nil {
		t.Fatal("hex down returned a command")
	}
	if selected := scrolled.drill.stack[0].selectedChild; selected != 0 {
		t.Fatalf("hex down selected drill child %d, want to stay on large child", selected)
	}
	if scrolled.hexScroll != 1 {
		t.Fatalf("hex down scroll = %d, want 1", scrolled.hexScroll)
	}

	scrolled.hexScroll = 253
	next, _ = scrolled.Update(tea.KeyMsg{Type: tea.KeyDown})
	moved := next.(model)
	if selected := moved.drill.stack[0].selectedChild; selected != 1 {
		t.Fatalf("hex down at selected child end selected child %d, want 1", selected)
	}
}

func selectFirstDrillableBlock(t *testing.T, m *model) int {
	t.Helper()
	for idx, block := range m.currentPageBlocks() {
		if len(buildDrillChildren(block, m.currentPage)) == 0 {
			continue
		}
		m.selectedBlock = idx
		m.blockSelected = true
		m.drill = drillState{}
		m.revealSelectedHexBlock()
		return idx
	}
	t.Fatal("fixture page has no drillable cell block")
	return -1
}

func hasDrillChildTitle(children []drillChild, title string) bool {
	return drillChildIndex(children, title) >= 0
}

func drillChildIndex(children []drillChild, title string) int {
	for idx, child := range children {
		if child.title == title {
			return idx
		}
	}
	return -1
}

func TestPageMovementResetsHexSelection(t *testing.T) {
	t.Parallel()

	m, inspector := newFixtureModel(t, "companies.db")
	m = indexAll(t, m, inspector)

	next, cmd := m.Update(keyMsg("I"))
	loading := next.(model)
	next, _ = loading.Update(pageLoadedFromCmd(t, cmd))
	loaded := next.(model)
	next, _ = loaded.Update(keyMsg("O"))
	hex := next.(model)

	hex.focusedPane = navPane
	hex.hexScroll = 4
	next, cmd = hex.Update(tea.KeyMsg{Type: tea.KeyDown})
	movingPage := next.(model)
	if cmd == nil {
		t.Fatal("moving to next page did not return a load command")
	}
	if movingPage.blockSelected {
		t.Fatal("page movement left a selected hex block")
	}
	if movingPage.hexScroll != 0 {
		t.Fatalf("page movement left hexScroll = %d, want 0", movingPage.hexScroll)
	}
}

func TestStalePageLoadedMessageIgnored(t *testing.T) {
	t.Parallel()

	m, _ := newFixtureModel(t, "companies.db")
	m.active = contentTarget{kind: navPage, pageNumber: 2}
	m.loading = true
	m.status = "loading page 2"

	next, cmd := m.Update(pageLoadedMsg{page: &storage.PageInspection{Ref: storage.PageRef{ID: 1}}})
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

func TestVisibleJumpLabels(t *testing.T) {
	t.Parallel()

	m, inspector := newFixtureModel(t, "companies.db")
	m = indexAll(t, m, inspector)

	view := m.View()
	for _, label := range []string{"[U] B-TREES", "[I] PAGES", "[O] DETAIL", "[P] META"} {
		if !strings.Contains(view, label) {
			t.Fatalf("view missing jump label %q", label)
		}
	}
	for _, removed := range []string{"[1] B-TREES", "[2] PAGES", "[3] DETAIL", "[4] META", "[U] MAIN", "Overview", "DB Header"} {
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
	if strings.Contains(m.footerLine(), "d drill") || strings.Contains(m.footerLine(), "b back") {
		t.Fatalf("footer shows drill hints without a drillable selection: %q", m.footerLine())
	}
	for _, label := range []string{"[U] b-trees", "[I] pages", "[O] detail", "[P] meta"} {
		if strings.Contains(m.footerLine(), label) {
			t.Fatalf("footer still explains obvious jump hint %q: %q", label, m.footerLine())
		}
	}
}

func TestNavigationSectionsRenderAsSeparateFrames(t *testing.T) {
	t.Parallel()

	m, inspector := newFixtureModel(t, "companies.db")
	m = indexAll(t, m, inspector)

	nav := m.viewNavigationColumn(28, 22)
	if strings.Contains(nav, "Navigation") {
		t.Fatalf("navigation column still renders generic title:\n%s", nav)
	}
	if strings.Count(nav, "┌") != 2 {
		t.Fatalf("navigation column should render two framed sections:\n%s", nav)
	}
	for _, label := range []string{"[U] B-TREES", "[I] PAGES"} {
		if !strings.Contains(nav, label) {
			t.Fatalf("navigation column missing section frame title %q:\n%s", label, nav)
		}
	}
}

func TestContentPaneTitlesShowJumpKeys(t *testing.T) {
	t.Parallel()

	m, inspector := newFixtureModel(t, "companies.db")
	m = indexAll(t, m, inspector)

	m.width = 80
	m.height = 24
	fullView := m.View()
	fullViewLines := strings.Split(fullView, "\n")
	if got := len(fullViewLines); got != m.height {
		t.Fatalf("80x24 view has %d rows, want %d:\n%s", got, m.height, fullView)
	}
	if strings.TrimSpace(fullViewLines[0]) != "" {
		t.Fatalf("80x24 view first row = %q, want top inset", fullViewLines[0])
	}
	if !strings.Contains(fullViewLines[1], "┌") {
		t.Fatalf("80x24 view second row = %q, want top border", fullViewLines[1])
	}
	for idx, line := range fullViewLines {
		if got := lipgloss.Width(line); got > m.width {
			t.Fatalf("80x24 view row %d width = %d, want <= %d: %q", idx, got, m.width, line)
		}
	}
	for _, label := range []string{"[O] DETAIL", "[P] META", "sqlite_schema"} {
		if !strings.Contains(fullView, label) {
			t.Fatalf("80x24 view missing %q:\n%s", label, fullView)
		}
	}
}

func TestTruncateCellsPreservesANSISequences(t *testing.T) {
	t.Parallel()

	row := formatHexRowWithSelection(
		0,
		[]byte{0x53, 0x51, 0x4c, 0x69, 0x74, 0x65, 0x20, 0x66, 0x6f, 0x72, 0x6d, 0x61, 0x74, 0x20, 0x33, 0x00},
		[]pageBlock{{kind: pageBlockDatabaseHeader, meta: storage.ByteSpan{Start: 0, Size: 100}}},
		storage.ByteSpan{Start: 0, Size: 1},
		true,
		nil,
	)

	for width := 1; width < lipgloss.Width(row); width++ {
		got := truncateCells(row, width)
		if hasIncompleteCSI(got) {
			t.Fatalf("truncateCells left incomplete CSI at width %d: %q", width, got)
		}
		if gotWidth := lipgloss.Width(got); gotWidth > width {
			t.Fatalf("truncateCells width %d produced %d cells: %q", width, gotWidth, got)
		}
	}
}

func hasIncompleteCSI(s string) bool {
	inCSI := false
	for idx := 0; idx < len(s); idx++ {
		if !inCSI {
			if s[idx] == '\x1b' && idx+1 < len(s) && s[idx+1] == '[' {
				inCSI = true
				idx++
			}
			continue
		}
		if s[idx] >= 0x40 && s[idx] <= 0x7e {
			inCSI = false
		}
	}
	return inCSI
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
