package tui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	xansi "github.com/charmbracelet/x/ansi"
	"github.com/nikitazigman/badger/internal/storage"
)

const loadingIndicatorDelay = 500 * time.Millisecond

type pane int

const (
	navPane pane = iota
	explorerPane
	inspectorPane
)

type navKind int

const (
	navOverview navKind = iota
	navDBHeader
	navTable
	navIndex
	navPage
)

type navItem struct {
	kind       navKind
	title      string
	subtitle   string
	pageNumber uint64
	schema     *schemaObjectViewModel
}

type loadingDelayElapsedMsg struct {
	pageNumber uint64
}

func showLoadingAfterDelayCmd(pageNumber uint64) tea.Cmd {
	return tea.Tick(loadingIndicatorDelay, func(time.Time) tea.Msg {
		return loadingDelayElapsedMsg{pageNumber: pageNumber}
	})
}

type contentTarget struct {
	kind       navKind
	pageNumber uint64
	schemaName string
	schemaID   storage.BTreeID
}

type pageMetaSource int

const (
	pageMetaFromPages pageMetaSource = iota
	pageMetaFromHex
)

// filterSource identifies the single object PAGES is scoped to. It stores the object
// only; the page set is derived from pageIndex on demand.
type filterSource struct {
	object schemaObjectViewModel // Type → icon, Name, RootPage (0 for virtual tables / views)
}

type model struct {
	database        storage.Database
	db              databaseViewModel
	navItems        []navItem
	selectedIndex   int
	inspectorScroll int
	active          contentTarget
	currentPage     *storage.PageInspection
	focusedPane     pane
	selectedBlock   int
	blockSelected   bool
	drill           drillState
	metaSource      pageMetaSource
	hexScroll       int
	width           int
	height          int
	status          string
	loading         bool
	loadingVisible  bool
	err             error
	pageIndex       map[storage.BTreeID][]storage.PageRef // b-tree id -> ready pages
	indexRoots      []storage.BTreeID                     // unique, root-backed ids dispatched at launch
	indexErrors     map[storage.BTreeID]string            // b-tree id -> hard-failure reason
	indexPending    int
	indexTotal      int
	activeFilter    *filterSource // nil = unfiltered
	pendingG        bool
	numericPrefix   string
}

func newModel(database storage.Database, overview *storage.DatabaseOverview) (model, error) {
	db, err := newDatabaseViewModel(overview)
	if err != nil {
		return model{}, err
	}

	indexRoots := collectBTreeRoots(db)
	navItems := buildNavItems(db, nil, nil)

	return model{
		database:      database,
		db:            db,
		navItems:      navItems,
		active:        initialContentTarget(navItems),
		focusedPane:   navPane,
		width:         120,
		height:        34,
		status:        "",
		selectedIndex: 0,
		pageIndex:     map[storage.BTreeID][]storage.PageRef{},
		indexRoots:    indexRoots,
		indexErrors:   map[storage.BTreeID]string{},
		indexPending:  len(indexRoots),
		indexTotal:    len(indexRoots),
	}, nil
}

// applyFilter scopes PAGES to obj's b-tree. A rootless object is a valid filter with an
// empty page set; an indexed object filters to its walked pages. If the object hard-failed
// or has not been walked yet, the filter is NOT applied and a status explains why.
func (m *model) applyFilter(obj schemaObjectViewModel) {
	switch {
	case obj.RootPage == 0 && obj.Kind != storage.BTreeInlineBucket:
		m.setFilter(obj)
	case m.walkPresent(obj.ID):
		m.setFilter(obj)
	case m.hasIndexError(obj.ID):
		m.status = "⚠ can't filter " + objectIcon(obj) + " " + obj.Name + ": " + m.indexErrors[obj.ID]
	default:
		m.status = "still indexing " + objectIcon(obj) + " " + obj.Name + "… try again in a moment"
	}
}

// setFilter stores the filter, rebuilds the nav list so PAGES re-scopes, and keeps the
// cursor on the source row (design §4.2).
func (m *model) setFilter(obj schemaObjectViewModel) {
	m.activeFilter = &filterSource{object: obj}
	pages, _ := m.filteredPages()
	m.navItems = buildNavItems(m.db, m.activeFilter, pages)
	m.selectedIndex = indexOfBTreeRow(m.navItems, obj)
	m.status = fmt.Sprintf("filtered to %s %s", objectIcon(obj), obj.Name)
}

// clearFilter drops the active filter, rebuilds the full 1..PageCount PAGES list, and
// keeps the cursor on the same B-TREES row.
func (m *model) clearFilter() {
	if m.activeFilter == nil {
		return
	}
	src := m.activeFilter.object
	m.activeFilter = nil
	m.navItems = buildNavItems(m.db, nil, nil)
	m.selectedIndex = indexOfBTreeRow(m.navItems, src)
	m.status = "filter cleared"
}

func (m model) isFiltered() bool { return m.activeFilter != nil }

// filteredPages returns (pages, true) when a filter is active (an empty slice for a
// virtual table), else (nil, false). The bool means "filter active", NOT "has pages".
func (m model) filteredPages() ([]storage.PageRef, bool) {
	if m.activeFilter == nil {
		return nil, false
	}
	root := m.activeFilter.object.RootPage
	if root == 0 && m.activeFilter.object.Kind != storage.BTreeInlineBucket {
		return []storage.PageRef{}, true
	}
	return append([]storage.PageRef(nil), m.pageIndex[m.activeFilter.object.ID]...), true
}

func (m model) walkPresent(id storage.BTreeID) bool {
	_, ok := m.pageIndex[id]
	return ok
}

func (m model) hasIndexError(id storage.BTreeID) bool {
	_, ok := m.indexErrors[id]
	return ok
}

// objectIsFilterSource reports whether obj is the object the active filter is scoped to.
// Stable storage IDs disambiguate nested/inline buckets; name/root is the fallback for
// synthetic rootless objects that may not have an ID.
// pagesSummaryLine is the summary pane's Pages row for obj: the filtered count when obj is
// the active filter source, "— (unfiltered)" otherwise (design §4.1 / §4.2).
func (m model) pagesSummaryLine(obj schemaObjectViewModel) string {
	if m.objectIsFilterSource(obj) {
		pages, _ := m.filteredPages()
		return fmt.Sprintf("Pages:     %d (filtered)", len(pages))
	}
	return "Pages:     — (unfiltered)"
}

func (m model) objectIsFilterSource(obj schemaObjectViewModel) bool {
	if m.activeFilter == nil {
		return false
	}
	active := m.activeFilter.object
	if active.ID != "" && obj.ID != "" {
		return active.ID == obj.ID
	}
	return active.Name == obj.Name && active.RootPage == obj.RootPage
}

func (m model) Init() tea.Cmd {
	if len(m.indexRoots) == 0 {
		return nil
	}
	cmds := make([]tea.Cmd, 0, len(m.indexRoots))
	for _, id := range m.indexRoots {
		cmds = append(cmds, indexBTreeCmd(m.database, id))
	}
	return tea.Batch(cmds...)
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil
	case tea.KeyMsg:
		return m.handleKey(msg)
	case pageLoadedMsg:
		if msg.page == nil || m.active.kind != navPage || msg.page.Ref.ID != m.active.pageNumber {
			return m, nil
		}
		m.currentPage = msg.page
		m.validateBlockSelection()
		m.validateDrillSelection()
		if m.focusedPane == explorerPane {
			m.selectFirstHexBlockIfNeeded()
		}
		m.inspectorScroll = 0
		m.loading = false
		m.loadingVisible = false
		return m, nil
	case loadingDelayElapsedMsg:
		if !m.loading || m.active.kind != navPage || msg.pageNumber != m.active.pageNumber {
			return m, nil
		}
		m.loadingVisible = true
		return m, nil
	case btreeIndexedMsg:
		if m.indexPending > 0 {
			m.indexPending--
		}
		if msg.err != nil {
			m.indexErrors[msg.id] = msg.err.Error()
		} else {
			m.pageIndex[msg.id] = append([]storage.PageRef(nil), msg.pages...)
		}
		// Transient status only; the polished footer token is Ticket 06.
		if m.indexPending == 0 {
			m.status = indexCompleteStatus(m)
		}
		return m, nil
	case errMsg:
		m.loading = false
		m.loadingVisible = false
		m.err = msg.err
		m.status = msg.err.Error()
		return m, nil
	}

	return m, nil
}

func (m model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	if m.pendingG {
		m.pendingG = false
		switch key {
		case "g":
			return m.jumpActiveListBoundary(true)
		case "e":
			return m.jumpActiveListBoundary(false)
		}
	}

	if isDigitKey(key) {
		if m.canUseNumericPrefix() {
			m.numericPrefix += key
		}
		return m, nil
	}

	if m.numericPrefix != "" && !keyAcceptsNumericPrefix(key) {
		switch key {
		case "ctrl+c", "q", "U", "I", "O", "P", "esc":
			m.clearNumericPrefix()
		case "d", "u":
			m.rejectNumericPrefix(key)
			return m, nil
		case "g":
			m.rejectNumericPrefix("gg")
			return m, nil
		default:
			m.clearNumericPrefix()
			return m, nil
		}
	}

	switch key {
	case "ctrl+c", "q":
		return m, tea.Quit
	case "U":
		m.clearNumericPrefix()
		m.focusedPane = navPane
		if m.selectBTreeRowForPaneReturn() {
			return m.activateSelected()
		}
		return m, nil
	case "I":
		m.clearNumericPrefix()
		m.focusedPane = navPane
		if m.selectPageRowForPaneReturn() {
			return m.activateSelected()
		}
		return m, nil
	case "O":
		m.clearNumericPrefix()
		m.focusedPane = explorerPane
		if m.active.kind == navPage {
			m.metaSource = pageMetaFromHex
			m.selectFirstHexBlockIfNeeded()
			m.revealSelectedHexBlock()
		}
		return m, nil
	case "P":
		m.clearNumericPrefix()
		if m.active.kind == navPage {
			switch m.focusedPane {
			case navPane:
				m.metaSource = pageMetaFromPages
			case explorerPane:
				m.metaSource = pageMetaFromHex
			}
		}
		m.focusedPane = inspectorPane
		return m, nil
	case "f":
		item := m.selectedItem()
		if (item.kind == navTable || item.kind == navIndex) && item.schema != nil {
			if m.objectIsFilterSource(*item.schema) {
				m.clearFilter()
				next, cmd := m.activateSelected()
				activated := next.(model)
				activated.status = "filter cleared"
				return activated, cmd
			}
			m.applyFilter(*item.schema)
			if m.objectIsFilterSource(*item.schema) {
				return m.activateSelected()
			}
		}
		return m, nil
	case "d":
		if m.focusedPane == navPane {
			if m.selectedIndexHasBTree() || m.selectedIndexHasKind(navPage) {
				if m.moveSelectionWithinSection(10) {
					return m.activateSelected()
				}
			}
		} else if m.active.kind == navPage {
			m.drillIn()
		}
		return m, nil
	case "u":
		if m.focusedPane == navPane && (m.selectedIndexHasBTree() || m.selectedIndexHasKind(navPage)) {
			if m.moveSelectionWithinSection(-10) {
				return m.activateSelected()
			}
		}
		return m, nil
	case "b":
		if m.active.kind == navPage {
			m.drillBack()
		}
		return m, nil
	case "enter":
		if m.focusedPane == navPane {
			return m.activateSelected()
		}
		return m, nil
	case "up", "k":
		if m.focusedPane == navPane {
			if m.selectedIndexHasBTree() || m.selectedIndexHasKind(navPage) {
				count := m.consumeNumericPrefix(1)
				if count > 0 && m.moveSelectionWithinSection(-count) {
					return m.activateSelected()
				}
				return m, nil
			}
			if m.moveSelection(-1) {
				return m.activateSelected()
			}
		} else if m.focusedPane == explorerPane && m.active.kind == navPage && m.drill.active {
			if !m.scrollSelectedHexRegion(-1, 1) && !m.moveDrillChild(-1) {
				m.scrollHex(-1, 1)
			}
		} else if m.focusedPane == explorerPane && m.active.kind == navPage {
			if !m.scrollSelectedHexRegion(-1, 1) && !m.moveHexBlock(-1) {
				m.scrollHex(-1, 1)
			}
		} else if m.focusedPane == inspectorPane {
			m.scrollInspector(-1, 1)
		}
		return m, nil
	case "down", "j":
		if m.focusedPane == navPane {
			if m.selectedIndexHasBTree() || m.selectedIndexHasKind(navPage) {
				count := m.consumeNumericPrefix(1)
				if count > 0 && m.moveSelectionWithinSection(count) {
					return m.activateSelected()
				}
				return m, nil
			}
			if m.moveSelection(1) {
				return m.activateSelected()
			}
		} else if m.focusedPane == explorerPane && m.active.kind == navPage && m.drill.active {
			if !m.scrollSelectedHexRegion(1, 1) && !m.moveDrillChild(1) {
				m.scrollHex(1, 1)
			}
		} else if m.focusedPane == explorerPane && m.active.kind == navPage {
			if !m.scrollSelectedHexRegion(1, 1) && !m.moveHexBlock(1) {
				m.scrollHex(1, 1)
			}
		} else if m.focusedPane == inspectorPane {
			m.scrollInspector(1, 1)
		}
		return m, nil
	case "g":
		m.pendingG = true
		return m, nil
	case "pgup":
		if m.focusedPane == explorerPane && m.active.kind == navPage {
			m.scrollHex(-1, 8)
		} else if m.focusedPane == inspectorPane {
			m.scrollInspector(-1, 8)
		}
		return m, nil
	case "pgdown":
		if m.focusedPane == explorerPane && m.active.kind == navPage {
			m.scrollHex(1, 8)
		} else if m.focusedPane == inspectorPane {
			m.scrollInspector(1, 8)
		}
		return m, nil
	case "home":
		if m.focusedPane == inspectorPane {
			m.inspectorScroll = 0
		}
		return m, nil
	case "esc":
		m.clearNumericPrefix()
		if m.isFiltered() {
			m.clearFilter()
			return m, nil
		}
		m.loading = false
		m.loadingVisible = false
		m.inspectorScroll = 0
		m.resetHexSelection()
		m.status = "reset page selection"
		return m, nil
	}

	return m, nil
}

func (m model) jumpActiveListBoundary(first bool) (tea.Model, tea.Cmd) {
	if m.focusedPane != navPane {
		return m, nil
	}

	var match func(navItem) bool
	emptyStatus := ""
	switch {
	case m.selectedIndexHasBTree():
		match = isBTreeNavItem
		emptyStatus = "no b-trees in current b-tree list"
	case m.selectedIndexHasKind(navPage):
		match = func(item navItem) bool { return item.kind == navPage }
		emptyStatus = "no pages in current page list"
	case m.active.kind == navTable || m.active.kind == navIndex:
		match = isBTreeNavItem
		emptyStatus = "no b-trees in current b-tree list"
	case m.active.kind == navPage:
		match = func(item navItem) bool { return item.kind == navPage }
		emptyStatus = "no pages in current page list"
	case isBTreeNavItem(m.selectedItem()):
		match = isBTreeNavItem
		emptyStatus = "no b-trees in current b-tree list"
	case m.selectedItem().kind == navPage:
		match = func(item navItem) bool { return item.kind == navPage }
		emptyStatus = "no pages in current page list"
	default:
		return m, nil
	}

	idx, ok := m.boundaryIndexMatching(match, first)
	if !ok {
		m.status = emptyStatus
		return m, nil
	}
	m.selectNavIndex(idx)
	return m.activateSelected()
}

func (m model) boundaryIndexMatching(match func(navItem) bool, first bool) (int, bool) {
	found := false
	result := 0
	for idx, item := range m.navItems {
		if !match(item) {
			continue
		}
		if first {
			return idx, true
		}
		found = true
		result = idx
	}
	return result, found
}

func isDigitKey(key string) bool {
	return len(key) == 1 && key[0] >= '0' && key[0] <= '9'
}

func keyAcceptsNumericPrefix(key string) bool {
	return key == "j" || key == "k"
}

func (m model) canUseNumericPrefix() bool {
	return m.focusedPane == navPane && (m.selectedIndexHasBTree() || m.selectedIndexHasKind(navPage))
}

func (m *model) clearNumericPrefix() {
	m.numericPrefix = ""
}

func (m *model) rejectNumericPrefix(key string) {
	m.clearNumericPrefix()
	m.status = fmt.Sprintf("count not supported for %s", key)
}

func (m *model) consumeNumericPrefix(defaultCount int) int {
	if m.numericPrefix == "" {
		return defaultCount
	}
	count := 0
	limit := max(1, len(m.navItems))
	for _, r := range m.numericPrefix {
		count = count*10 + int(r-'0')
		if count > limit {
			count = limit
			break
		}
	}
	m.clearNumericPrefix()
	return count
}

func (m *model) moveSelection(delta int) bool {
	next := m.selectedIndex + delta
	if next < 0 || next >= len(m.navItems) {
		return false
	}
	// Arrows stay within the current section; 1/2 cross sections.
	if sectionForNavItem(m.navItems[next]) != sectionForNavItem(m.navItems[m.selectedIndex]) {
		return false
	}
	m.selectedIndex = next
	m.inspectorScroll = 0
	return true
}

func (m *model) moveSelectionWithinSection(delta int) bool {
	if m.selectedIndex < 0 || m.selectedIndex >= len(m.navItems) {
		return false
	}
	section := sectionForNavItem(m.navItems[m.selectedIndex])
	first, last, ok := m.sectionBounds(section)
	if !ok {
		return false
	}
	next := clamp(m.selectedIndex+delta, first, last)
	if next == m.selectedIndex {
		return false
	}
	m.selectedIndex = next
	m.inspectorScroll = 0
	return true
}

func (m model) sectionBounds(section string) (int, int, bool) {
	first := 0
	last := 0
	found := false
	for idx, item := range m.navItems {
		if sectionForNavItem(item) != section {
			continue
		}
		if !found {
			first = idx
		}
		last = idx
		found = true
	}
	return first, last, found
}

func (m *model) selectFirstKind(kind navKind) bool {
	for idx, item := range m.navItems {
		if item.kind == kind {
			if m.selectedIndex == idx {
				return false
			}
			m.selectedIndex = idx
			m.inspectorScroll = 0
			return true
		}
	}
	return false
}

func (m *model) selectPageRowForPaneReturn() bool {
	if m.selectedIndexHasKind(navPage) {
		return true
	}
	if m.active.kind == navPage {
		if idx, ok := m.indexOfPageRow(m.active.pageNumber); ok {
			m.selectNavIndex(idx)
			return true
		}
		if idx, ok := m.nearestPageRow(m.active.pageNumber); ok {
			m.selectNavIndex(idx)
			return true
		}
	}
	if idx, ok := m.firstIndexMatching(func(item navItem) bool { return item.kind == navPage }); ok {
		m.selectNavIndex(idx)
		return true
	}
	m.status = "no pages in current page list"
	return false
}

func (m *model) selectBTreeRowForPaneReturn() bool {
	if m.selectedIndexHasBTree() {
		return true
	}
	if idx, ok := m.indexOfActiveBTreeRow(); ok {
		m.selectNavIndex(idx)
		return true
	}
	if idx, ok := m.firstIndexMatching(func(item navItem) bool { return isBTreeNavItem(item) }); ok {
		m.selectNavIndex(idx)
		return true
	}
	m.status = "no b-trees in current b-tree list"
	return false
}

func (m model) selectedIndexHasKind(kind navKind) bool {
	return m.selectedIndex >= 0 && m.selectedIndex < len(m.navItems) && m.navItems[m.selectedIndex].kind == kind
}

func (m model) selectedIndexHasBTree() bool {
	return m.selectedIndex >= 0 && m.selectedIndex < len(m.navItems) && isBTreeNavItem(m.navItems[m.selectedIndex])
}

func (m *model) selectNavIndex(idx int) {
	if m.selectedIndex == idx {
		return
	}
	m.selectedIndex = idx
	m.inspectorScroll = 0
}

func (m model) firstIndexMatching(match func(navItem) bool) (int, bool) {
	for idx, item := range m.navItems {
		if match(item) {
			return idx, true
		}
	}
	return 0, false
}

func (m model) indexOfPageRow(pageNumber uint64) (int, bool) {
	for idx, item := range m.navItems {
		if item.kind == navPage && item.pageNumber == pageNumber {
			return idx, true
		}
	}
	return 0, false
}

func (m model) nearestPageRow(pageNumber uint64) (int, bool) {
	bestIndex := 0
	var bestDistance uint64
	found := false
	for idx, item := range m.navItems {
		if item.kind != navPage {
			continue
		}
		distance := pageDistance(item.pageNumber, pageNumber)
		if !found || distance < bestDistance || (distance == bestDistance && item.pageNumber < m.navItems[bestIndex].pageNumber) {
			bestIndex = idx
			bestDistance = distance
			found = true
		}
	}
	return bestIndex, found
}

func pageDistance(a uint64, b uint64) uint64 {
	if a > b {
		return a - b
	}
	return b - a
}

func (m model) indexOfActiveBTreeRow() (int, bool) {
	if m.active.kind == navPage && m.activeFilter != nil {
		return indexOfBTreeRowOK(m.navItems, m.activeFilter.object)
	}
	if m.active.kind != navTable && m.active.kind != navIndex {
		return 0, false
	}
	for idx, item := range m.navItems {
		if item.kind != m.active.kind || item.schema == nil {
			continue
		}
		if m.active.schemaID != "" && item.schema.ID == m.active.schemaID {
			return idx, true
		}
		if m.active.schemaID == "" && item.schema.Name == m.active.schemaName {
			return idx, true
		}
	}
	return 0, false
}

// selectFirstBTreeRow jumps to the first row of the merged B-TREES section: the first
// table, or the first index when the database has no tables.
func (m *model) selectFirstBTreeRow() bool {
	for idx, item := range m.navItems {
		if isBTreeNavItem(item) {
			if m.selectedIndex == idx {
				return false
			}
			m.selectedIndex = idx
			m.inspectorScroll = 0
			return true
		}
	}
	return false
}

func (m *model) scrollInspector(direction int, amount int) {
	if amount < 1 {
		amount = 1
	}
	m.inspectorScroll += direction * amount
	if m.inspectorScroll < 0 {
		m.inspectorScroll = 0
	}
}

func (m *model) scrollHex(direction int, amount int) {
	if amount < 1 {
		amount = 1
	}
	if m.currentPage == nil {
		return
	}
	dataRows := m.hexDataRows()
	rawRows := (len(m.currentPage.Raw) + 15) / 16
	maxScroll := max(0, rawRows-dataRows)
	m.hexScroll = clamp(m.hexScroll+direction*amount, 0, maxScroll)
}

func (m *model) scrollSelectedHexRegion(direction int, amount int) bool {
	if amount < 1 {
		amount = 1
	}
	if m.currentPage == nil || len(m.currentPage.Raw) == 0 {
		return false
	}
	blocks := buildPageBlocks(m.currentPage)
	meta, ok := m.selectedHexMeta(blocks)
	if !ok || meta.Size <= 0 {
		return false
	}

	dataRows := m.hexDataRows()
	if dataRows <= 0 {
		return false
	}
	startRow := meta.Start / 16
	endRow := max(startRow, (meta.End()-1)/16)
	selectedRows := endRow - startRow + 1
	if selectedRows <= dataRows || selectedRows <= dataRows*4 {
		return false
	}

	rawRows := (len(m.currentPage.Raw) + 15) / 16
	maxScroll := max(0, rawRows-dataRows)
	current := revealHexMetaScroll(clamp(m.hexScroll, 0, maxScroll), meta, dataRows)
	minWithin := startRow
	maxWithin := min(maxScroll, endRow-dataRows+1)
	switch {
	case direction > 0 && current < maxWithin:
		m.hexScroll = min(current+amount, maxWithin)
		return true
	case direction < 0 && current > minWithin:
		m.hexScroll = max(current-amount, minWithin)
		return true
	default:
		return false
	}
}

func (m *model) resetHexSelection() {
	m.selectedBlock = 0
	m.blockSelected = false
	m.drill = drillState{}
	m.hexScroll = 0
}

func (m *model) selectFirstHexBlockIfNeeded() {
	if m.blockSelected {
		m.validateBlockSelection()
		return
	}
	if len(m.currentPageBlocks()) == 0 {
		return
	}
	m.selectedBlock = 0
	m.blockSelected = true
}

func (m *model) validateBlockSelection() {
	if !m.blockSelected {
		return
	}
	blocks := m.currentPageBlocks()
	if len(blocks) == 0 {
		m.resetHexSelection()
		return
	}
	if m.selectedBlock < 0 {
		m.selectedBlock = 0
	}
	if m.selectedBlock >= len(blocks) {
		m.selectedBlock = len(blocks) - 1
	}
}

func (m *model) validateDrillSelection() {
	if !m.drill.active {
		return
	}
	blocks := m.currentPageBlocks()
	if m.drill.parentBlock < 0 || m.drill.parentBlock >= len(blocks) {
		m.drill = drillState{}
		return
	}
	children := buildDrillChildren(blocks[m.drill.parentBlock], m.currentPage)
	if len(children) == 0 {
		m.drill = drillState{}
		return
	}
	if len(m.drill.stack) == 0 {
		m.drill.stack = []drillFrame{{title: blocks[m.drill.parentBlock].title(), children: children}}
	}
	m.selectedBlock = m.drill.parentBlock
	m.blockSelected = true
	m.cloneDrillStack()
	for idx := range m.drill.stack {
		if len(m.drill.stack[idx].children) == 0 {
			m.drill.stack = m.drill.stack[:idx]
			break
		}
		m.drill.stack[idx].selectedChild = clamp(m.drill.stack[idx].selectedChild, 0, len(m.drill.stack[idx].children)-1)
	}
	if len(m.drill.stack) == 0 {
		m.drill = drillState{}
	}
}

func (m *model) moveHexBlock(delta int) bool {
	blocks := m.currentPageBlocks()
	if len(blocks) == 0 {
		m.resetHexSelection()
		return true
	}
	if !m.blockSelected {
		m.selectedBlock = 0
		m.blockSelected = true
		m.inspectorScroll = 0
		m.revealSelectedHexBlock()
		return true
	}
	next := clamp(m.selectedBlock+delta, 0, len(blocks)-1)
	if next == m.selectedBlock {
		return false
	}
	m.selectedBlock = next
	m.inspectorScroll = 0
	m.revealSelectedHexBlock()
	return true
}

func (m *model) drillIn() {
	if !m.drill.active {
		m.enterDrill()
		return
	}
	child, ok := m.selectedDrillChild()
	if ok && len(child.children) > 0 {
		m.cloneDrillStack()
		m.drill.stack = append(m.drill.stack, drillFrame{
			title:    child.title,
			children: child.children,
		})
		m.inspectorScroll = 0
		m.revealSelectedDrillChild()
		return
	}
}

func (m *model) enterDrill() {
	block, ok := m.selectedPageBlock()
	if !ok {
		return
	}
	children := buildDrillChildren(block, m.currentPage)
	if len(children) == 0 {
		return
	}
	m.drill = drillState{
		active:      true,
		parentBlock: m.selectedBlock,
		stack: []drillFrame{{
			title:    block.title(),
			children: children,
		}},
	}
	m.blockSelected = true
	m.inspectorScroll = 0
	m.revealSelectedDrillChild()
}

func (m *model) drillBack() {
	if !m.drill.active {
		return
	}
	if len(m.drill.stack) > 1 {
		m.cloneDrillStack()
		m.drill.stack = m.drill.stack[:len(m.drill.stack)-1]
		m.inspectorScroll = 0
		m.revealSelectedDrillChild()
		return
	}

	parent := m.drill.parentBlock
	m.drill = drillState{}
	blocks := m.currentPageBlocks()
	if len(blocks) == 0 {
		m.resetHexSelection()
		return
	}
	m.selectedBlock = clamp(parent, 0, len(blocks)-1)
	m.blockSelected = true
	m.inspectorScroll = 0
	m.revealSelectedHexBlock()
}

func (m *model) moveDrillChild(delta int) bool {
	children := m.currentDrillChildren()
	if len(children) == 0 {
		m.drill = drillState{}
		return true
	}
	m.cloneDrillStack()
	frame := &m.drill.stack[len(m.drill.stack)-1]
	next := clamp(frame.selectedChild+delta, 0, len(children)-1)
	if next == frame.selectedChild {
		return false
	}
	frame.selectedChild = next
	m.inspectorScroll = 0
	m.revealSelectedDrillChild()
	return true
}

func (m *model) cloneDrillStack() {
	if len(m.drill.stack) == 0 {
		return
	}
	m.drill.stack = append([]drillFrame(nil), m.drill.stack...)
}

func (m *model) revealSelectedHexBlock() {
	blocks := m.currentPageBlocks()
	if !m.blockSelected || m.selectedBlock < 0 || m.selectedBlock >= len(blocks) {
		return
	}
	m.hexScroll = revealHexMetaScroll(m.hexScroll, blocks[m.selectedBlock].meta, m.hexDataRows())
}

func (m *model) revealSelectedDrillChild() {
	child, ok := m.selectedDrillChild()
	if !ok {
		return
	}
	m.hexScroll = revealHexMetaScroll(m.hexScroll, child.meta, m.hexDataRows())
}

func (m model) hexDataRows() int {
	return max(1, m.height-4-1)
}

func (m model) activateSelected() (tea.Model, tea.Cmd) {
	item := m.selectedItem()
	m.active = contentTarget{
		kind: item.kind,
	}
	m.err = nil
	m.loading = false
	m.loadingVisible = false
	m.resetHexSelection()

	switch item.kind {
	case navOverview:
		m.status = "overview"
		m.currentPage = nil
		m.inspectorScroll = 0
		return m, nil
	case navDBHeader:
		m.status = "database header"
		m.currentPage = nil
		m.inspectorScroll = 0
		return m, nil
	case navTable, navIndex:
		if item.schema != nil {
			m.active.schemaName = item.schema.Name
			m.active.schemaID = item.schema.ID
			m.status = fmt.Sprintf("opened %s %s", item.schema.Type, item.schema.Name)
		}
		m.currentPage = nil
		m.inspectorScroll = 0
		return m, nil
	case navPage:
		m.active.pageNumber = item.pageNumber
		m.inspectorScroll = 0
		m.metaSource = pageMetaFromPages
		m.loading = true
		m.loadingVisible = false
		m.status = ""
		return m, tea.Batch(
			loadPageCmd(m.database, item.pageNumber),
			showLoadingAfterDelayCmd(item.pageNumber),
		)
	default:
		return m, nil
	}
}

func (m model) openSelected() (tea.Model, tea.Cmd) {
	return m.activateSelected()
}

func (m model) View() string {
	if m.width < 60 || m.height < 12 {
		return "terminal too small for badger"
	}

	rootMarginX := 1
	rootMarginTop := 1
	availableWidth := max(0, m.width-rootMarginX*2)
	bodyHeight := max(0, m.height-rootMarginTop-1)

	navWidth := clamp(availableWidth/4, 24, 34)
	inspectorWidth := clamp(availableWidth/4, 28, 38)
	explorerWidth := availableWidth - navWidth - inspectorWidth
	if m.active.kind == navPage {
		navWidth, explorerWidth, inspectorWidth = pagePaneWidths(availableWidth, navWidth, explorerWidth, inspectorWidth)
	}
	paneContentHeight := max(0, bodyHeight-2)

	nav := m.viewNavigationColumn(navWidth, bodyHeight)
	explorerRows := strings.Split(m.viewExplorer(paneInnerWidth(explorerWidth), paneContentHeight), "\n")
	explorer := renderTitledFrameWithScrollbar(
		explorerWidth,
		bodyHeight,
		detailFrameTitle(m.detailHeaderText()),
		m.focusedPane == explorerPane,
		explorerRows,
		m.hexScrollbarTrack(paneContentHeight),
	)
	inspector := renderTitledFrame(
		inspectorWidth,
		bodyHeight,
		metaFrameTitle(m.metaHeaderText()),
		m.focusedPane == inspectorPane,
		strings.Split(m.viewInspector(paneInnerWidth(inspectorWidth), paneContentHeight), "\n"),
	)

	body := lipgloss.JoinHorizontal(lipgloss.Top, nav, explorer, inspector)
	statusWidth := max(0, availableWidth-2)
	statusText := lipgloss.PlaceHorizontal(statusWidth, lipgloss.Left, truncateCells(m.footerLine(), statusWidth))
	status := statusStyle.Render(statusText)

	lines := make([]string, 0, m.height)
	for idx := 0; idx < rootMarginTop; idx++ {
		lines = append(lines, strings.Repeat(" ", m.width))
	}
	for _, line := range strings.Split(body, "\n") {
		lines = append(lines, insetLine(line, rootMarginX, availableWidth))
	}
	lines = append(lines, insetLine(status, rootMarginX, availableWidth))
	return strings.Join(lines, "\n")
}

func pagePaneWidths(availableWidth int, navWidth int, explorerWidth int, inspectorWidth int) (int, int, int) {
	const (
		minPageNavWidth       = 22
		minPageInspectorWidth = 24
		minFullHexFrameWidth  = 61
	)

	deficit := minFullHexFrameWidth - explorerWidth
	if deficit <= 0 {
		return navWidth, explorerWidth, inspectorWidth
	}

	shrinkInspector := min(deficit, max(0, inspectorWidth-minPageInspectorWidth))
	inspectorWidth -= shrinkInspector
	explorerWidth += shrinkInspector
	deficit -= shrinkInspector

	shrinkNav := min(deficit, max(0, navWidth-minPageNavWidth))
	navWidth -= shrinkNav
	explorerWidth += shrinkNav

	if explorerWidth > availableWidth {
		explorerWidth = availableWidth
		navWidth = 0
		inspectorWidth = 0
	}
	return navWidth, explorerWidth, inspectorWidth
}

// footerLine builds the always-on footer: the key hints, with a leading context segment.
// While filtered the context is the filter token (and the filter-aware key set); otherwise
// it is the latest transient status, if any.
func (m model) footerLine() string {
	keys := m.footerKeyHints()
	if m.status != "" && strings.HasPrefix(m.status, "no ") {
		return m.status + "  |  " + keys
	}
	if m.isFiltered() {
		return m.filterToken() + "  |  " + keys
	}
	if m.status != "" {
		return m.status + "  |  " + keys
	}
	return keys
}

func (m model) footerKeyHints() string {
	hints := []string{"↑↓ move"}
	if m.canDrillIn() {
		hints = append(hints, "d drill")
	}
	if m.canDrillBack() {
		hints = append(hints, "b back")
	}
	if m.canFilterFromFooter() {
		if m.isFiltered() {
			hints = append(hints, "f clear/switch")
		} else {
			hints = append(hints, "f filter")
		}
	}
	hints = append(hints, "q quit")
	return strings.Join(hints, " · ")
}

func (m model) canDrillIn() bool {
	if m.focusedPane == navPane {
		return false
	}
	if m.active.kind != navPage {
		return false
	}
	if !m.drill.active {
		block, ok := m.selectedPageBlock()
		if !ok {
			return false
		}
		return len(buildDrillChildren(block, m.currentPage)) > 0
	}
	child, ok := m.selectedDrillChild()
	return ok && len(child.children) > 0
}

func (m model) canDrillBack() bool {
	return m.active.kind == navPage && m.drill.active
}

func (m model) canFilterFromFooter() bool {
	if m.focusedPane != navPane {
		return false
	}
	item := m.selectedItem()
	return (item.kind == navTable || item.kind == navIndex) && item.schema != nil
}

func insetLine(line string, left int, width int) string {
	if left <= 0 {
		return lipgloss.PlaceHorizontal(width, lipgloss.Left, truncateCells(line, width))
	}
	return strings.Repeat(" ", left) + lipgloss.PlaceHorizontal(width, lipgloss.Left, truncateCells(line, width))
}

// filterToken renders the active-filter indicator: the source icon + name and page count.
// Hard-failure statuses are only shown while unfiltered, where the transient status
// segment surfaces them.
func (m model) filterToken() string {
	obj := m.activeFilter.object
	pages, _ := m.filteredPages()

	var b strings.Builder
	fmt.Fprintf(&b, "⦿ filtered: %s %s (%d pg", objectIcon(obj), obj.Name, len(pages))
	b.WriteString(")")
	return b.String()
}

func (m model) selectedItem() navItem {
	if len(m.navItems) == 0 {
		return navItem{kind: navPage, title: "page 1", pageNumber: 1}
	}
	if m.selectedIndex < 0 || m.selectedIndex >= len(m.navItems) {
		return m.navItems[0]
	}
	return m.navItems[m.selectedIndex]
}

func (m model) viewNavigationColumn(width int, height int) string {
	btreeOuterHeight := clamp(m.navSectionSize("B-Trees")+2, 5, max(5, height/2))
	pagesOuterHeight := height - btreeOuterHeight
	if pagesOuterHeight < 4 {
		pagesOuterHeight = 4
		btreeOuterHeight = max(4, height-pagesOuterHeight)
	}

	btrees := m.viewNavigationSection(width, btreeOuterHeight, "B-Trees")
	pages := m.viewNavigationSection(width, pagesOuterHeight, "Pages")
	return lipgloss.JoinVertical(lipgloss.Left, btrees, pages)
}

func (m model) viewNavigationSection(width int, outerHeight int, section string) string {
	contentHeight := max(0, outerHeight-2)
	contentWidth := max(0, width-4)
	active := m.focusedPane == navPane && sectionForNavItem(m.selectedItem()) == section

	rows := make([]string, 0, contentHeight)
	for _, row := range m.visibleNavSection(section, contentHeight) {
		lineStyle := navItemStyle
		if row.index == m.selectedIndex {
			lineStyle = selectedNavItemStyle
		}
		rows = append(rows, renderNavLine(lineStyle, contentWidth, m.navMarker(row.index), row.text))
	}

	return renderTitledFrame(width, outerHeight, sectionLabel(section), active, rows)
}

func (m model) navSectionSize(section string) int {
	count := 0
	for _, item := range m.navItems {
		if sectionForNavItem(item) == section {
			count++
		}
	}
	return count
}

func (m model) visibleNavSection(section string, height int) []visibleNavRow {
	rows := make([]visibleNavRow, 0, len(m.navItems))
	for idx, item := range m.navItems {
		if sectionForNavItem(item) != section {
			continue
		}
		text := item.title
		if (item.kind == navTable || item.kind == navIndex) && item.schema != nil {
			text = navSchemaRowText(*item.schema)
		} else if item.subtitle != "" {
			text += "  " + mutedInline(item.subtitle)
		}
		rows = append(rows, visibleNavRow{index: idx, section: section, text: text})
	}
	if height <= 0 || len(rows) <= height {
		return rows
	}

	selectedLine := 0
	for idx, row := range rows {
		if row.index == m.selectedIndex {
			selectedLine = idx
			break
		}
	}
	start := selectedLine - height/2
	if start < 0 {
		start = 0
	}
	end := start + height
	if end > len(rows) {
		end = len(rows)
		start = max(0, end-height)
	}
	return rows[start:end]
}

func renderTitledFrame(width int, height int, title string, active bool, rows []string) string {
	return renderTitledFrameWithScrollbar(width, height, title, active, rows, nil)
}

func renderTitledFrameWithScrollbar(width int, height int, title string, active bool, rows []string, rightTrack []string) string {
	if width <= 0 || height <= 0 {
		return ""
	}
	if width < 4 || height < 2 {
		return strings.Repeat(" ", width)
	}

	color := lipgloss.Color("240")
	titleStyle := navSectionTitleStyle
	if active {
		color = lipgloss.Color("33")
		titleStyle = activeNavSectionTitleStyle
	}
	borderStyle := lipgloss.NewStyle().Foreground(color)

	innerWidth := max(0, width-2)
	leading := 0
	if innerWidth > 1 {
		leading = 1
	}
	title = truncateCells(title, max(0, innerWidth-leading))
	topFill := strings.Repeat("─", max(0, innerWidth-leading-lipgloss.Width(title)))
	lines := []string{
		borderStyle.Render("┌"+strings.Repeat("─", leading)) + titleStyle.Render(title) + borderStyle.Render(topFill+"┐"),
	}

	contentWidth := max(0, width-4)
	contentHeight := height - 2
	for idx := 0; idx < contentHeight; idx++ {
		line := ""
		if idx < len(rows) {
			line = truncateCells(rows[idx], contentWidth)
		}
		line = lipgloss.PlaceHorizontal(contentWidth, lipgloss.Left, line)
		rightBorder := borderStyle.Render("│")
		if idx < len(rightTrack) {
			rightBorder = rightTrack[idx]
		}
		lines = append(lines, borderStyle.Render("│")+" "+line+" "+rightBorder)
	}
	lines = append(lines, borderStyle.Render("└"+strings.Repeat("─", innerWidth)+"┘"))
	return strings.Join(lines, "\n")
}

func renderNavLine(style lipgloss.Style, width int, marker string, text string) string {
	markerWidth := lipgloss.Width(marker)
	textWidth := max(0, width-markerWidth)
	line := marker + truncateCells(text, textWidth)
	return style.Render(lipgloss.PlaceHorizontal(width, lipgloss.Left, line))
}

// navMarker returns the two-cell row prefix: ▶ for the active filter source (it wins, so
// the cursor and source merge into a single ▶ when they coincide), > for the cursor, two
// spaces otherwise. Never two markers on a row (design §2).
func (m model) navMarker(idx int) string {
	if idx < 0 || idx >= len(m.navItems) {
		return "  "
	}
	item := m.navItems[idx]
	if item.schema != nil && m.objectIsFilterSource(*item.schema) {
		return "▶ "
	}
	if idx == m.selectedIndex {
		return "> "
	}
	return "  "
}

type visibleNavRow struct {
	index   int
	section string
	text    string
}

func (m model) visibleNavItems(height int) []visibleNavRow {
	type navRow struct {
		index   int
		section string
		text    string
	}

	rows := make([]navRow, 0, len(m.navItems))
	lastSection := ""
	for idx, item := range m.navItems {
		section := sectionForNavItem(item)
		if section != lastSection {
			lastSection = section
		}
		text := item.title
		if (item.kind == navTable || item.kind == navIndex) && item.schema != nil {
			text = navSchemaRowText(*item.schema)
		} else if item.subtitle != "" {
			text += "  " + mutedInline(item.subtitle)
		}
		rows = append(rows, navRow{index: idx, section: section, text: text})
	}

	selectedLine := clamp(m.selectedIndex, 0, len(rows)-1)
	start := selectedLine - height/2
	if start < 0 {
		start = 0
	}
	end := start + height
	if end > len(rows) {
		end = len(rows)
		start = max(0, end-height)
	}

	out := make([]visibleNavRow, 0, end-start)
	currentSection := ""
	for _, row := range rows[start:end] {
		section := ""
		if row.section != currentSection {
			section = row.section
			currentSection = row.section
		}
		out = append(out, visibleNavRow{
			index:   row.index,
			section: section,
			text:    row.text,
		})
	}
	return out
}

func (m model) viewExplorer(width int, height int) string {
	var lines []string
	switch m.active.kind {
	case navOverview:
		lines = m.viewOverview(width)
	case navDBHeader:
		lines = m.viewDBHeader(width)
	case navTable, navIndex:
		lines = m.viewSchemaObject(width)
	case navPage:
		lines = m.viewPage(width, height)
	default:
		lines = []string{"No content"}
	}

	return fitVertical(lines, height)
}

func (m model) detailHeaderText() string {
	switch m.active.kind {
	case navOverview:
		return "Overview"
	case navDBHeader:
		return "Database Header"
	case navTable, navIndex:
		if item := m.selectedItem(); item.schema != nil {
			return item.schema.Name
		}
		return "Schema Object"
	case navPage:
		return "HEX"
	default:
		return "No content"
	}
}

func (m model) metaHeaderText() string {
	if m.active.kind == navPage {
		if child, ok := m.selectedDrillChild(); ok && m.pageMetaUsesHexSelection() {
			return child.title
		}
		if block, ok := m.selectedPageBlock(); ok && m.pageMetaUsesHexSelection() {
			return block.title()
		}
		return fmt.Sprintf("page %d", m.inspectorPageNumber())
	}
	return m.selectedItem().title
}

func (m model) pageMetaUsesHexSelection() bool {
	if m.active.kind != navPage {
		return false
	}
	if m.focusedPane == explorerPane {
		return true
	}
	return m.focusedPane == inspectorPane && m.metaSource == pageMetaFromHex
}

func (m model) viewOverview(width int) []string {
	lines := []string{
		fmt.Sprintf("File: %s", m.db.Path),
		fmt.Sprintf("Page size: %s", formatBytes(uint64(m.db.PageSize))),
		fmt.Sprintf("Page count: %d", m.db.PageCount),
		fmt.Sprintf("DB size: %s", formatBytes(uint64(m.db.DatabaseSizeBytes))),
		fmt.Sprintf("Encoding: %s", m.db.EncodingLabel),
		fmt.Sprintf("Freelist pages: %d", m.db.FreelistPageCount),
		fmt.Sprintf("Tables: %d", len(m.db.Tables)),
		fmt.Sprintf("Indexes: %d", len(m.db.Indexes)),
		"",
		sectionStyle.Render("TABLES"),
	}

	for _, table := range m.db.Tables {
		lines = append(lines, fmt.Sprintf("- %s  root page %d", table.Name, table.RootPage))
	}
	if len(m.db.Tables) == 0 {
		lines = append(lines, mutedStyle.Render("No tables"))
	}

	lines = append(lines, "", sectionStyle.Render("INDEXES"))
	for _, index := range m.db.Indexes {
		lines = append(lines, fmt.Sprintf("- %s  root page %d", index.Name, index.RootPage))
	}
	if len(m.db.Indexes) == 0 {
		lines = append(lines, mutedStyle.Render("No indexes"))
	}

	lines = append(lines, "", mutedStyle.Render("Move through navigation to inspect items."))
	return wrapLines(lines, width)
}

func (m model) viewDBHeader(width int) []string {
	header := m.db.HeaderRows
	lines := []string{}
	for _, row := range header {
		lines = append(lines, fmt.Sprintf("%-24s %s", row.Label+":", row.Value))
	}
	return wrapLines(lines, width)
}

func (m model) viewSchemaObject(width int) []string {
	item := m.selectedItem()
	obj := item.schema
	if obj == nil {
		return []string{"No schema object selected."}
	}
	if obj.Kind == storage.BTreeBucket || obj.Kind == storage.BTreeInlineBucket {
		if obj.Rows == nil {
			return []string{"No storage object metadata."}
		}
		return wrapLines(fieldLines(*obj.Rows), width)
	}

	rootLine := fmt.Sprintf("Root page: %d", obj.RootPage)
	if obj.RootPage == 0 {
		rootLine = "Root page: — (no b-tree)"
	}

	lines := []string{
		fmt.Sprintf("Type: %s", obj.Type),
		fmt.Sprintf("Name: %s", obj.Name),
		fmt.Sprintf("Table: %s", obj.TableName),
		rootLine,
	}
	if obj.IsSystem {
		lines = append(lines,
			"System catalog",
			"SQLite-created",
			"Filtering shows all reachable catalog b-tree pages.",
			"Page 1 stores the 100-byte database header before the b-tree payload.",
			"",
			sectionStyle.Render("SQL"),
			"No stored SQL row for sqlite_schema itself.",
		)
		return wrapLines(lines, width)
	}
	lines = append(lines, "", sectionStyle.Render("SQL"), obj.SQL)
	return wrapLines(lines, width)
}

func (m model) viewPage(width int, height int) []string {
	pageNumber := m.inspectorPageNumber()
	preservingLoadedPage := m.loading && !m.loadingVisible && m.currentPage != nil

	if m.loading {
		if m.loadingVisible {
			return wrapLines([]string{"Loading page bytes..."}, width)
		}
		if !preservingLoadedPage {
			return wrapLines([]string{"Waiting for page bytes..."}, width)
		}
	}

	if m.currentPage == nil || m.currentPage.Ref.ID != pageNumber {
		return wrapLines([]string{"Waiting for page bytes..."}, width)
	}

	return m.renderHexRows(width, height)
}

func (m model) viewInspector(width int, height int) string {
	item := m.selectedItem()
	if m.active.kind == navPage {
		if content := m.viewPageMeta(); content != "" {
			return m.renderInspectorViewport(strings.Split(content, "\n"), width, height)
		}
	}

	lines := []string{}

	switch item.kind {
	case navOverview:
		lines = append(lines,
			"",
			sectionStyle.Render("SUMMARY"),
			fmt.Sprintf("File path: %s", m.db.Path),
			fmt.Sprintf("Page size: %d", m.db.PageSize),
			fmt.Sprintf("Page count: %d", m.db.PageCount),
			fmt.Sprintf("DB size: %d bytes", m.db.DatabaseSizeBytes),
			fmt.Sprintf("Encoding: %s", m.db.EncodingLabel),
			"",
			sectionStyle.Render("ACTIONS"),
			"- open DB header",
			"- open a schema object",
			"- open a page",
		)
	case navDBHeader:
		lines = append(lines,
			"",
			sectionStyle.Render("DETAIL"),
			"100-byte SQLite database header",
			"Schema cookie: "+fieldValue(m.db.HeaderRows, "Schema cookie"),
			"Schema format: "+fieldValue(m.db.HeaderRows, "Schema format"),
			fmt.Sprintf("SQLite version: %s", m.db.SQLiteVersionLabel),
		)
	case navTable, navIndex:
		if item.schema != nil {
			obj := *item.schema
			rootLine := fmt.Sprintf("Root:      page %d", obj.RootPage)
			if obj.RootPage == 0 {
				rootLine = "Root:      —"
			}
			lines = append(lines,
				"",
				sectionStyle.Render("SUMMARY"),
				fmt.Sprintf("Type:      %s %s", objectIcon(obj), obj.Type),
				rootLine,
				m.pagesSummaryLine(obj),
				fmt.Sprintf("Table:     %s", obj.TableName),
			)
			if obj.IsSystem {
				lines = append(lines,
					"Catalog:   System catalog",
					"Managed:   SQLite-created",
					"Page 1:    database header, then catalog b-tree payload",
				)
			}
			lines = append(lines, "", sectionStyle.Render("ACTIONS"))
			if m.objectIsFilterSource(obj) {
				lines = append(lines, "- f        clear filter")
			} else {
				lines = append(lines, "- f        filter pages to this b-tree")
			}
		}
	case navPage:
		lines = append(lines,
			"",
			sectionStyle.Render("DETAIL"),
			fmt.Sprintf("Page number: %d", item.pageNumber),
			fmt.Sprintf("File offset: %d", (item.pageNumber-1)*m.db.PageSize),
		)
		if item.pageNumber == 1 {
			lines = append(lines, "Bytes 0-99: SQLite database header before b-tree content")
		}
		if m.currentPage != nil && m.currentPage.Ref.ID == item.pageNumber {
			lines = append(lines, pageSummaryLines(m.currentPage)...)
		}
		lines = append(lines,
			"",
			sectionStyle.Render("ACTIONS"),
			"- move in navigation to load pages",
		)
	}

	if m.err != nil {
		lines = append(lines, "", errorStyle.Render(m.err.Error()))
	}

	return m.renderInspectorViewport(lines, width, height)
}

func (m model) viewPageMeta() string {
	pageNumber := m.inspectorPageNumber()
	lines := []string{}

	if m.currentPage == nil || m.currentPage.Ref.ID != pageNumber {
		if m.loadingVisible {
			lines = append(lines, "Page metadata is loading.")
		} else {
			lines = append(lines, "Waiting for page metadata.")
		}
		return strings.Join(lines, "\n")
	}

	page := m.currentPage
	if child, ok := m.selectedDrillChild(); ok && m.pageMetaUsesHexSelection() {
		return strings.Join(drillChildMetaLines(child), "\n")
	}
	if block, ok := m.selectedPageBlock(); ok && m.pageMetaUsesHexSelection() {
		return strings.Join(blockMetaLines(block, page), "\n")
	}

	lines = append(lines, pageRows(page.Rows)...)

	if m.activeFilter != nil {
		lines = append(lines,
			"",
			sectionStyle.Render("BTREE"),
			fmt.Sprintf("Object: %s", m.activeFilter.object.Name),
			fmt.Sprintf("Root page: %d", m.activeFilter.object.RootPage),
		)
	}

	return strings.Join(lines, "\n")
}

func pageRows(rows []storage.Field) []string {
	lines := make([]string, 0, len(rows))
	for _, line := range fieldLines(rows) {
		if line == "Page: "+fieldValue(rows, "Page") {
			lines = append(lines, "Page "+fieldValue(rows, "Page"))
			continue
		}
		lines = append(lines, line)
	}
	return lines
}

func pageSummaryLines(page *storage.PageInspection) []string {
	if page == nil {
		return nil
	}
	lines := []string{}
	if value := fieldValue(page.Rows, "Type"); value != "" {
		lines = append(lines, "Page kind: "+value)
	}
	if block := firstHexBlockByKind(page.HexBlocks, pageBlockPageHeader); block != nil {
		lines = append(lines, fmt.Sprintf("Header bytes: %d..%d", block.Span.Start, block.Span.End()))
	}
	if value := fieldValue(page.Rows, "Pointer array"); value != "" {
		lines = append(lines, "Pointer array: "+value)
	}
	return lines
}

func firstHexBlockByKind(blocks []storage.HexBlock, kind string) *storage.HexBlock {
	for idx := range blocks {
		if blocks[idx].Kind == kind {
			return &blocks[idx]
		}
	}
	return nil
}

func (m model) inspectorPageNumber() uint64 {
	item := m.selectedItem()
	pageNumber := item.pageNumber
	if m.loading && !m.loadingVisible && m.currentPage != nil {
		pageNumber = m.currentPage.Ref.ID
	}
	return pageNumber
}

func (m model) currentPageBlocks() []pageBlock {
	pageNumber := m.inspectorPageNumber()
	if m.currentPage == nil || m.currentPage.Ref.ID != pageNumber {
		return nil
	}
	return buildPageBlocks(m.currentPage)
}

func (m model) selectedPageBlock() (pageBlock, bool) {
	blocks := m.currentPageBlocks()
	if !m.blockSelected || m.selectedBlock < 0 || m.selectedBlock >= len(blocks) {
		return pageBlock{}, false
	}
	return blocks[m.selectedBlock], true
}

func (m model) currentDrillChildren() []drillChild {
	if !m.drill.active || m.currentPage == nil || len(m.drill.stack) == 0 {
		return nil
	}
	blocks := m.currentPageBlocks()
	if m.drill.parentBlock < 0 || m.drill.parentBlock >= len(blocks) {
		return nil
	}
	return m.drill.stack[len(m.drill.stack)-1].children
}

func (m model) selectedDrillChild() (drillChild, bool) {
	children := m.currentDrillChildren()
	if len(m.drill.stack) == 0 {
		return drillChild{}, false
	}
	selected := m.drill.stack[len(m.drill.stack)-1].selectedChild
	if selected < 0 || selected >= len(children) {
		return drillChild{}, false
	}
	return children[selected], true
}

func paneInnerWidth(outerWidth int) int {
	return max(0, outerWidth-4)
}

func (m model) renderInspectorViewport(lines []string, width int, height int) string {
	contentWidth := inspectorContentWidth(width)

	wrapped := wrapInspectorLines(lines, contentWidth)
	maxScroll := max(0, len(wrapped)-height)
	scroll := clamp(m.inspectorScroll, 0, maxScroll)

	visible := wrapped
	if len(visible) > height {
		visible = visible[scroll:]
	}
	if len(visible) > height {
		visible = visible[:height]
	}
	if len(visible) < height {
		padding := make([]string, height-len(visible))
		visible = append(visible, padding...)
	}

	if contentWidth == width || maxScroll == 0 {
		return strings.Join(visible, "\n")
	}

	return renderScrollbar(visible, contentWidth, height, scroll, maxScroll)
}

func inspectorContentWidth(width int) int {
	contentWidth := width - 2
	if contentWidth < 8 {
		return width
	}
	return contentWidth
}

func renderScrollbar(lines []string, contentWidth int, height int, scroll int, maxScroll int) string {
	if height <= 0 {
		return ""
	}

	track := scrollbarTrack(height, scroll, maxScroll)

	out := make([]string, 0, height)
	for idx := 0; idx < height; idx++ {
		padded := lipgloss.NewStyle().Width(contentWidth).Render(lines[idx])
		out = append(out, padded+" "+track[idx])
	}
	return strings.Join(out, "\n")
}

func scrollbarTrack(height int, scroll int, maxScroll int) []string {
	track := make([]string, height)
	for idx := range track {
		track[idx] = scrollbarTrackStyle.Render("│")
	}

	thumbStart := 0
	thumbSize := height
	if maxScroll > 0 {
		thumbSize = max(1, height*height/(height+maxScroll))
		if thumbSize > height {
			thumbSize = height
		}
		thumbStart = (scroll * (height - thumbSize)) / maxScroll
	}

	for idx := 0; idx < thumbSize && thumbStart+idx < height; idx++ {
		track[thumbStart+idx] = scrollbarThumbStyle.Render("█")
	}
	return track
}

func (m model) renderHexRows(width int, height int) []string {
	if height <= 0 {
		return nil
	}
	if m.currentPage == nil {
		return []string{"No page bytes."}
	}
	raw := m.currentPage.Raw
	if len(raw) == 0 {
		return []string{"No page bytes."}
	}

	lines := []string{hexOffsetStyle.Render(truncateCells("Offset   00 01 02 03 04 05 06 07  08 09 0A 0B 0C 0D 0E 0F", width))}
	blocks := buildPageBlocks(m.currentPage)
	selectedMeta, hasSelectedMeta := m.selectedHexMeta(blocks)
	drillChildren := m.currentDrillChildren()
	dataRows := max(0, height-1)
	scroll, _, ok := m.hexViewportScroll(dataRows)
	if !ok {
		scroll = 0
	}
	for offset := scroll * 16; offset < len(raw) && len(lines) < height; offset += 16 {
		end := offset + 16
		if end > len(raw) {
			end = len(raw)
		}
		lines = append(lines, truncateCells(formatHexRowWithSelection(offset, raw[offset:end], blocks, selectedMeta, hasSelectedMeta, drillChildren), width))
	}
	return lines
}

func (m model) hexViewportScroll(dataRows int) (int, int, bool) {
	if m.currentPage == nil || len(m.currentPage.Raw) == 0 {
		return 0, 0, false
	}
	rawRows := (len(m.currentPage.Raw) + 15) / 16
	maxScroll := max(0, rawRows-dataRows)
	scroll := clamp(m.hexScroll, 0, maxScroll)
	blocks := buildPageBlocks(m.currentPage)
	if selectedMeta, ok := m.selectedHexMeta(blocks); ok {
		scroll = revealHexMetaScroll(scroll, selectedMeta, dataRows)
	}
	return scroll, maxScroll, true
}

func (m model) hexScrollbarTrack(height int) []string {
	if m.active.kind != navPage || height <= 0 {
		return nil
	}
	pageNumber := m.inspectorPageNumber()
	if m.currentPage == nil || m.currentPage.Ref.ID != pageNumber || len(m.currentPage.Raw) == 0 {
		return nil
	}
	dataRows := max(0, height-1)
	scroll, maxScroll, ok := m.hexViewportScroll(dataRows)
	if !ok {
		return nil
	}
	return scrollbarTrack(height, scroll, maxScroll)
}

func (m model) selectedHexMeta(blocks []pageBlock) (storage.ByteSpan, bool) {
	if child, ok := m.selectedDrillChild(); ok {
		return child.meta, true
	}
	if m.blockSelected && m.selectedBlock >= 0 && m.selectedBlock < len(blocks) {
		return blocks[m.selectedBlock].meta, true
	}
	return storage.ByteSpan{}, false
}

func formatHexRow(offset int, chunk []byte, blocks []pageBlock, selected int) string {
	selectedMeta := storage.ByteSpan{}
	hasSelectedMeta := false
	if selected >= 0 && selected < len(blocks) {
		selectedMeta = blocks[selected].meta
		hasSelectedMeta = true
	}
	return formatHexRowWithSelection(offset, chunk, blocks, selectedMeta, hasSelectedMeta, nil)
}

func formatHexRowWithSelection(offset int, chunk []byte, blocks []pageBlock, selectedMeta storage.ByteSpan, hasSelectedMeta bool, drillChildren []drillChild) string {
	var b strings.Builder
	b.WriteString(hexOffsetStyle.Render(fmt.Sprintf("%04X     ", offset)))
	for idx := 0; idx < 16; idx++ {
		if idx > 0 {
			if idx == 8 {
				b.WriteString("  ")
			} else {
				b.WriteByte(' ')
			}
		}
		if idx < len(chunk) {
			byteOffset := offset + idx
			token := fmt.Sprintf("%02X", chunk[idx])
			b.WriteString(styleHexByte(token, byteOffset, blocks, selectedMeta, hasSelectedMeta, drillChildren))
		} else {
			b.WriteString("  ")
		}
	}
	return b.String()
}

func styleHexByte(token string, offset int, blocks []pageBlock, selectedMeta storage.ByteSpan, hasSelectedMeta bool, drillChildren []drillChild) string {
	if hasSelectedMeta && offset >= selectedMeta.Start && offset < selectedMeta.End() {
		return selectedHexByteStyle.Render(token)
	}
	for _, child := range drillChildren {
		if offset >= child.meta.Start && offset < child.meta.End() {
			return drillChildStyle(child.kind).Render(token)
		}
	}
	for _, block := range blocks {
		if offset < block.meta.Start || offset >= block.meta.End() {
			continue
		}
		return blockStyle(block.kind).Render(token)
	}
	return unknownHexByteStyle.Render(token)
}

func detailFrameTitle(title string) string {
	if title == "HEX" {
		return "[O] HEX"
	}
	return "[O] DETAIL  " + title
}

func metaFrameTitle(title string) string {
	return "[P] META  " + title
}

var (
	sectionStyle                 = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("110"))
	navSectionTitleStyle         = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("110"))
	activeNavSectionTitleStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("117"))
	statusStyle                  = lipgloss.NewStyle().Foreground(lipgloss.Color("252")).Background(lipgloss.Color("236")).Padding(0, 1)
	navItemStyle                 = lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	selectedNavItemStyle         = lipgloss.NewStyle().Foreground(lipgloss.Color("230")).Background(lipgloss.Color("24"))
	mutedStyle                   = lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
	hexOffsetStyle               = lipgloss.NewStyle().Foreground(lipgloss.Color("242"))
	unknownHexByteStyle          = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	dbHeaderHexByteStyle         = lipgloss.NewStyle().Foreground(lipgloss.Color("111"))
	pageHeaderHexByteStyle       = lipgloss.NewStyle().Foreground(lipgloss.Color("117"))
	freelistHexByteStyle         = lipgloss.NewStyle().Foreground(lipgloss.Color("174"))
	pointerArrayHexByteStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("179"))
	freeblockHexByteStyle        = lipgloss.NewStyle().Foreground(lipgloss.Color("209"))
	unallocatedHexByteStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
	cellHexByteStyle             = lipgloss.NewStyle().Foreground(lipgloss.Color("151"))
	selectedHexByteStyle         = lipgloss.NewStyle().Foreground(lipgloss.Color("230")).Background(lipgloss.Color("25")).Bold(true)
	payloadSizeHexByteStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
	rowIDHexByteStyle            = lipgloss.NewStyle().Foreground(lipgloss.Color("45"))
	leftChildPageHexByteStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("117"))
	recordPayloadHexByteStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("120"))
	recordHeaderSizeHexByteStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("229"))
	serialTypeHexByteStyle       = lipgloss.NewStyle().Foreground(lipgloss.Color("183"))
	recordValueHexByteStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("111"))
	overflowPointerHexByteStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("209"))
	leafDescriptorHexByteStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("179"))
	leafKeyHexByteStyle          = lipgloss.NewStyle().Foreground(lipgloss.Color("45"))
	leafValueHexByteStyle        = lipgloss.NewStyle().Foreground(lipgloss.Color("111"))
	errorStyle                   = lipgloss.NewStyle().Foreground(lipgloss.Color("203"))
	scrollbarTrackStyle          = lipgloss.NewStyle().Foreground(lipgloss.Color("238"))
	scrollbarThumbStyle          = lipgloss.NewStyle().Foreground(lipgloss.Color("117"))
)

func drillChildStyle(kind string) lipgloss.Style {
	switch kind {
	case drillChildPayloadSize:
		return payloadSizeHexByteStyle
	case drillChildRowID:
		return rowIDHexByteStyle
	case drillChildLeftChildPage:
		return leftChildPageHexByteStyle
	case drillChildRecordPayload:
		return recordPayloadHexByteStyle
	case drillChildRecordHeaderSize:
		return recordHeaderSizeHexByteStyle
	case drillChildSerialType:
		return serialTypeHexByteStyle
	case drillChildRecordValue:
		return recordValueHexByteStyle
	case drillChildOverflowPointer:
		return overflowPointerHexByteStyle
	case drillChildBranchDescriptor:
		return leafDescriptorHexByteStyle
	case drillChildBranchEntry:
		return cellHexByteStyle
	case drillChildLeafDescriptor:
		return leafDescriptorHexByteStyle
	case drillChildLeafKey:
		return leafKeyHexByteStyle
	case drillChildLeafValue:
		return leafValueHexByteStyle
	case drillChildLeafEntry:
		return cellHexByteStyle
	case drillChildDescriptorFlags, drillChildDescriptorPos, drillChildDescriptorKeySz, drillChildDescriptorValSz, drillChildDescriptorChild:
		return leafDescriptorHexByteStyle
	case drillChildBucketRootPage, drillChildBucketSequence:
		return leafValueHexByteStyle
	default:
		return cellHexByteStyle
	}
}

func blockStyle(kind string) lipgloss.Style {
	switch kind {
	case pageBlockDatabaseHeader:
		return dbHeaderHexByteStyle
	case pageBlockPageHeader:
		return pageHeaderHexByteStyle
	case pageBlockMetaPayload:
		return dbHeaderHexByteStyle
	case pageBlockFreelistPayload:
		return freelistHexByteStyle
	case pageBlockBranchDescriptors, pageBlockBranchDescriptor:
		return leafDescriptorHexByteStyle
	case pageBlockBranchEntry:
		return cellHexByteStyle
	case pageBlockLeafDescriptors, pageBlockLeafDescriptor:
		return leafDescriptorHexByteStyle
	case pageBlockLeafKey:
		return leafKeyHexByteStyle
	case pageBlockLeafValue:
		return leafValueHexByteStyle
	case pageBlockLeafEntry:
		return cellHexByteStyle
	case pageBlockPointerArray:
		return pointerArrayHexByteStyle
	case pageBlockFreeblock:
		return freeblockHexByteStyle
	case pageBlockUnallocated:
		return unallocatedHexByteStyle
	case pageBlockTableLeafCell, pageBlockTableInteriorCell, pageBlockIndexLeafCell, pageBlockIndexInteriorCell:
		return cellHexByteStyle
	default:
		return unknownHexByteStyle
	}
}

// buildNavItems builds the flat nav list. Tables and indexes share one B-TREES section.
// The PAGES section lists filteredPages when filter != nil (an empty list for a virtual
// table) and the full 1..PageCount otherwise. The root page is intentionally not shown on
// B-TREES rows (it lives in the detail / summary panes — design §2).
func buildNavItems(db databaseViewModel, filter *filterSource, filteredPages []storage.PageRef) []navItem {
	items := []navItem{}

	for _, table := range db.Tables {
		table := table
		items = append(items, navItem{
			kind:   navTable,
			title:  table.Name,
			schema: &table,
		})
	}

	for _, index := range db.Indexes {
		index := index
		items = append(items, navItem{
			kind:   navIndex,
			title:  index.Name,
			schema: &index,
		})
	}

	if filter != nil {
		for _, page := range filteredPages {
			items = append(items, navItem{
				kind:       navPage,
				title:      fmt.Sprintf("page %d", page.ID),
				pageNumber: page.ID,
			})
		}
	} else {
		for pageNumber := db.FirstPageID; pageNumber < db.FirstPageID+db.PageCount; pageNumber++ {
			items = append(items, navItem{
				kind:       navPage,
				title:      fmt.Sprintf("page %d", pageNumber),
				pageNumber: pageNumber,
			})
		}
	}

	return items
}

func initialContentTarget(items []navItem) contentTarget {
	if len(items) == 0 {
		return contentTarget{kind: navPage, pageNumber: 1}
	}
	item := items[0]
	target := contentTarget{kind: item.kind, pageNumber: item.pageNumber}
	if item.schema != nil {
		target.schemaName = item.schema.Name
		target.schemaID = item.schema.ID
	}
	return target
}

func navSchemaRowText(obj schemaObjectViewModel) string {
	if obj.IsSystem {
		return obj.Name
	}
	return objectIcon(obj) + " " + obj.Name
}

// objectIcon maps a schema object to its glyph: ◈ index, ⊞ virtual table / view (no
// b-tree, RootPage == 0), ▦ ordinary table.
func objectIcon(obj schemaObjectViewModel) string {
	switch {
	case obj.Type == "index":
		return "◈"
	case obj.Kind == storage.BTreeInlineBucket:
		return "⊞"
	case obj.Kind == storage.BTreeBucket:
		return "▦"
	case obj.RootPage == 0:
		return "⊞"
	default:
		return "▦"
	}
}

// indexOfBTreeRow returns the nav index of the B-TREES row for obj, or 0 if absent.
func indexOfBTreeRow(items []navItem, obj schemaObjectViewModel) int {
	idx, ok := indexOfBTreeRowOK(items, obj)
	if !ok {
		return 0
	}
	return idx
}

func indexOfBTreeRowOK(items []navItem, obj schemaObjectViewModel) (int, bool) {
	for idx, item := range items {
		if !isBTreeNavItem(item) {
			continue
		}
		if item.schema == nil {
			continue
		}
		if obj.ID != "" && item.schema.ID == obj.ID {
			return idx, true
		}
		if obj.ID == "" && item.schema.Name == obj.Name && item.schema.RootPage == obj.RootPage {
			return idx, true
		}
	}
	return 0, false
}

func isBTreeNavItem(item navItem) bool {
	return item.kind == navTable || item.kind == navIndex
}

// collectBTreeRoots returns unique storage object IDs that can produce a page set. A
// physical root belongs to exactly one object, but this dedupes defensively; rootless
// objects are skipped while inline buckets are kept so storage can return the parent page.
func collectBTreeRoots(db databaseViewModel) []storage.BTreeID {
	seen := make(map[storage.BTreeID]bool)
	roots := make([]storage.BTreeID, 0, len(db.Tables)+len(db.Indexes))

	collect := func(objects []schemaObjectViewModel) {
		for _, obj := range objects {
			if obj.ID == "" || seen[obj.ID] {
				continue
			}
			if obj.RootPage == 0 && obj.Kind != storage.BTreeInlineBucket {
				continue
			}
			seen[obj.ID] = true
			roots = append(roots, obj.ID)
		}
	}

	collect(db.Tables)
	collect(db.Indexes)
	return roots
}

// indexCompleteStatus is the transient one-line status shown once every root has been
// walked. The polished footer treatment lands in Ticket 06.
func indexCompleteStatus(m model) string {
	indexed := len(m.indexRoots) - len(m.indexErrors)
	if len(m.indexErrors) == 0 {
		return fmt.Sprintf("indexed %d b-trees", indexed)
	}
	return fmt.Sprintf("indexed %d b-trees (%d failed)", indexed, len(m.indexErrors))
}

// sectionLabel renders a section header with its pane-jump key prefix. Sections without a
// jump key render bare.
func sectionLabel(section string) string {
	key := map[string]string{"B-Trees": "U", "Pages": "I"}[section]
	if key == "" {
		return strings.ToUpper(section)
	}
	return "[" + key + "] " + strings.ToUpper(section)
}

func sectionForNavItem(item navItem) string {
	switch item.kind {
	case navTable, navIndex:
		return "B-Trees"
	case navPage:
		return "Pages"
	default:
		return "Other"
	}
}

func fitVertical(lines []string, height int) string {
	if len(lines) > height {
		lines = lines[:height]
	}
	if len(lines) < height {
		padding := make([]string, height-len(lines))
		lines = append(lines, padding...)
	}
	return strings.Join(lines, "\n")
}

func wrapLines(lines []string, width int) []string {
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		out = appendRenderedLines(out, line, width)
	}
	return out
}

func wrapInspectorLines(lines []string, width int) []string {
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		for _, segment := range strings.Split(line, "\n") {
			out = appendInspectorLine(out, segment, width)
		}
	}
	return out
}

func appendRenderedLines(out []string, line string, width int) []string {
	for _, segment := range strings.Split(line, "\n") {
		if strings.TrimSpace(segment) == "" {
			out = append(out, "")
			continue
		}
		rendered := lipgloss.NewStyle().Width(width).Render(segment)
		out = append(out, strings.Split(rendered, "\n")...)
	}
	return out
}

func appendInspectorLine(out []string, line string, width int) []string {
	if strings.TrimSpace(line) == "" {
		return append(out, "")
	}
	if strings.Contains(line, "\x1b[") {
		rendered := lipgloss.NewStyle().Width(width).Render(line)
		return append(out, strings.Split(rendered, "\n")...)
	}

	runes := []rune(line)
	for len(runes) > width && width > 0 {
		out = append(out, string(runes[:width]))
		runes = runes[width:]
	}
	if len(runes) == 0 {
		return append(out, "")
	}
	return append(out, string(runes))
}

func mutedInline(text string) string {
	return mutedStyle.Render(text)
}

func clamp(value int, minValue int, maxValue int) int {
	if value < minValue {
		return minValue
	}
	if value > maxValue {
		return maxValue
	}
	return value
}

func max(a int, b int) int {
	if a > b {
		return a
	}
	return b
}

func min(a int, b int) int {
	if a < b {
		return a
	}
	return b
}

func truncateLine(text string, width int) string {
	if width <= 0 {
		return ""
	}
	runes := []rune(text)
	if len(runes) <= width {
		return text
	}
	if width <= 1 {
		return string(runes[:width])
	}
	return string(runes[:width-1]) + "…"
}

func truncateCells(text string, width int) string {
	if width <= 0 {
		return ""
	}
	if lipgloss.Width(text) <= width {
		return text
	}
	if width <= 1 {
		return xansi.Truncate(text, width, "")
	}
	return xansi.Truncate(text, width, "…")
}
