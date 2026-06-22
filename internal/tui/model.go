package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/nikitazigman/badger/internal/sqlite"
)

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
	pageNumber uint32
	schema     *schemaObjectViewModel
}

type contentTarget struct {
	kind       navKind
	pageNumber uint32
	schemaName string
}

// filterSource identifies the single object PAGES is scoped to. It stores the object
// only; the page set and skip diagnostics are derived from pageIndex on demand (the DB is
// read-only and the index is immutable once built, so there is nothing to snapshot).
type filterSource struct {
	object schemaObjectViewModel // Type → icon, Name, RootPage (0 for virtual tables / views)
}

type model struct {
	inspector       *sqlite.Inspector
	db              databaseViewModel
	navItems        []navItem
	selectedIndex   int
	explorerIndex   int
	inspectorScroll int
	active          contentTarget
	currentPage     *sqlite.PageInspection
	pageRows        []pageRowViewModel
	focusedPane     pane
	width           int
	height          int
	status          string
	loading         bool
	err             error
	pageIndex       sqlite.PageIndex  // root → PageWalk (ready entries only)
	indexRoots      []uint32          // unique, non-zero roots dispatched at launch
	indexErrors     map[uint32]string // root → hard-failure reason (transient; NOT serialized)
	indexPending    int               // roots still being walked (indexTotal → 0)
	indexTotal      int               // total roots dispatched
	activeFilter    *filterSource     // nil = unfiltered
}

func newModel(inspector *sqlite.Inspector, metadata *sqlite.MetadataInspection) (model, error) {
	db, err := newDatabaseViewModel(metadata)
	if err != nil {
		return model{}, err
	}

	indexRoots := collectBTreeRoots(db)

	return model{
		inspector:     inspector,
		db:            db,
		navItems:      buildNavItems(db, nil, nil),
		active:        contentTarget{kind: navOverview},
		focusedPane:   navPane,
		width:         120,
		height:        34,
		status:        "",
		selectedIndex: 0,
		pageIndex:     sqlite.NewPageIndex(),
		indexRoots:    indexRoots,
		indexErrors:   map[uint32]string{},
		indexPending:  len(indexRoots),
		indexTotal:    len(indexRoots),
	}, nil
}

// applyFilter scopes PAGES to obj's b-tree. A virtual table / view (RootPage == 0) is a
// valid filter with an empty page set; an indexed object filters to its walked pages. If
// the object hard-failed or has not been walked yet, the filter is NOT applied and a
// status explains why (the user re-presses f once indexing finishes — see design §4.5).
func (m *model) applyFilter(obj schemaObjectViewModel) {
	switch {
	case obj.RootPage == 0: // virtual table / view: no b-tree, valid empty filter
		m.setFilter(obj)
	case m.walkPresent(obj.RootPage):
		m.setFilter(obj)
	case m.hasIndexError(obj.RootPage):
		m.status = "⚠ can't filter " + objectIcon(obj) + " " + obj.Name + ": " + m.indexErrors[obj.RootPage]
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
func (m model) filteredPages() ([]uint32, bool) {
	if m.activeFilter == nil {
		return nil, false
	}
	root := m.activeFilter.object.RootPage
	if root == 0 {
		return []uint32{}, true
	}
	return m.pageIndex.Pages(root), true
}

func (m model) walkPresent(root uint32) bool {
	_, ok := m.pageIndex.Walk(root)
	return ok
}

func (m model) hasIndexError(root uint32) bool {
	_, ok := m.indexErrors[root]
	return ok
}

// objectIsFilterSource reports whether obj is the object the active filter is scoped to.
// Name disambiguates virtual tables / views, which all share RootPage == 0.
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
	return m.activeFilter != nil &&
		m.activeFilter.object.Name == obj.Name &&
		m.activeFilter.object.RootPage == obj.RootPage
}

func (m model) Init() tea.Cmd {
	if len(m.indexRoots) == 0 {
		return nil
	}
	cmds := make([]tea.Cmd, 0, len(m.indexRoots))
	for _, root := range m.indexRoots {
		cmds = append(cmds, indexBTreeCmd(m.inspector, root))
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
		m.currentPage = msg.page
		m.pageRows = buildPageRows(msg.page)
		m.explorerIndex = 0
		m.inspectorScroll = 0
		m.loading = false
		m.status = fmt.Sprintf("opened page %d", msg.page.PageNumber)
		return m, nil
	case btreeIndexedMsg:
		if m.indexPending > 0 {
			m.indexPending--
		}
		if msg.err != nil {
			m.indexErrors[msg.root] = msg.err.Error()
		} else {
			m.pageIndex.Set(msg.walk)
		}
		// Transient status only; the polished footer token is Ticket 06.
		if m.indexPending == 0 {
			m.status = indexCompleteStatus(m)
		}
		return m, nil
	case errMsg:
		m.loading = false
		m.err = msg.err
		m.status = msg.err.Error()
		return m, nil
	}

	return m, nil
}

func (m model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c", "q":
		return m, tea.Quit
	case "tab":
		m.focusedPane = (m.focusedPane + 1) % 3
		return m, nil
	case "shift+tab":
		m.focusedPane = (m.focusedPane + 2) % 3
		return m, nil
	case "g":
		m.selectFirstKind(navOverview)
		return m.openSelected()
	case "h":
		m.selectFirstKind(navDBHeader)
		return m.openSelected()
	case "p":
		m.selectFirstKind(navPage)
		return m, nil
	case "f":
		item := m.selectedItem()
		if (item.kind == navTable || item.kind == navIndex) && item.schema != nil {
			m.applyFilter(*item.schema)
		}
		return m, nil
	case "F":
		m.clearFilter()
		return m, nil
	case "[":
		return m.openRelativePage(-1)
	case "]":
		return m.openRelativePage(1)
	case "enter":
		return m.openSelected()
	case "up", "k":
		if m.focusedPane == navPane {
			m.moveSelection(-1)
		} else if m.focusedPane == explorerPane && m.active.kind == navPage {
			m.moveExplorerSelection(-1)
		} else if m.focusedPane == inspectorPane {
			m.scrollInspector(-1, 1)
		}
		return m, nil
	case "down", "j":
		if m.focusedPane == navPane {
			m.moveSelection(1)
		} else if m.focusedPane == explorerPane && m.active.kind == navPage {
			m.moveExplorerSelection(1)
		} else if m.focusedPane == inspectorPane {
			m.scrollInspector(1, 1)
		}
		return m, nil
	case "pgup":
		if m.focusedPane == inspectorPane {
			m.scrollInspector(-1, 8)
		}
		return m, nil
	case "pgdown":
		if m.focusedPane == inspectorPane {
			m.scrollInspector(1, 8)
		}
		return m, nil
	case "home":
		if m.focusedPane == inspectorPane {
			m.inspectorScroll = 0
		}
		return m, nil
	case "esc":
		if m.active.kind == navPage && m.focusedPane == explorerPane && m.explorerIndex > 0 {
			m.explorerIndex = 0
			m.inspectorScroll = 0
			m.status = "returned to page summary"
			return m, nil
		}
		m.active = contentTarget{kind: navOverview}
		m.currentPage = nil
		m.pageRows = nil
		m.inspectorScroll = 0
		m.status = "returned to overview"
		return m, nil
	}

	return m, nil
}

func (m *model) moveSelection(delta int) {
	next := m.selectedIndex + delta
	if next < 0 {
		next = 0
	}
	if next >= len(m.navItems) {
		next = len(m.navItems) - 1
	}
	m.selectedIndex = next
	m.inspectorScroll = 0
}

func (m *model) selectFirstKind(kind navKind) {
	for idx, item := range m.navItems {
		if item.kind == kind {
			m.selectedIndex = idx
			return
		}
	}
}

func (m *model) moveExplorerSelection(delta int) {
	if len(m.pageRows) == 0 {
		return
	}
	next := m.explorerIndex + delta
	if next < 0 {
		next = 0
	}
	if next >= len(m.pageRows) {
		next = len(m.pageRows) - 1
	}
	m.explorerIndex = next
	m.inspectorScroll = 0
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

func (m model) openSelected() (tea.Model, tea.Cmd) {
	item := m.selectedItem()
	m.active = contentTarget{
		kind: item.kind,
	}
	m.err = nil

	switch item.kind {
	case navOverview:
		m.status = "overview"
		m.currentPage = nil
		m.pageRows = nil
		m.inspectorScroll = 0
		return m, nil
	case navDBHeader:
		m.status = "database header"
		m.currentPage = nil
		m.pageRows = nil
		m.inspectorScroll = 0
		return m, nil
	case navTable, navIndex:
		if item.schema != nil {
			m.active.schemaName = item.schema.Name
			m.status = fmt.Sprintf("opened %s %s", item.schema.Type, item.schema.Name)
		}
		m.currentPage = nil
		m.pageRows = nil
		m.inspectorScroll = 0
		return m, nil
	case navPage:
		m.active.pageNumber = item.pageNumber
		m.explorerIndex = 0
		m.pageRows = nil
		m.inspectorScroll = 0
		m.loading = true
		m.status = fmt.Sprintf("loading page %d", item.pageNumber)
		return m, loadPageCmd(m.inspector, item.pageNumber)
	default:
		return m, nil
	}
}

func (m model) openRelativePage(delta int) (tea.Model, tea.Cmd) {
	if m.active.kind != navPage {
		return m, nil
	}

	// When filtered, prev/next steps the filtered page set; clamping at either end is a
	// no-op rather than stepping into a page outside the filter (design §3 / ticket 03).
	if pages, ok := m.filteredPages(); ok {
		target, found := stepWithin(pages, m.active.pageNumber, delta)
		if !found {
			return m, nil
		}
		return m.openPageNumber(target)
	}

	if m.db.PageCount == 0 {
		return m, nil
	}
	page := int(m.active.pageNumber) + delta
	if page < 1 || page > int(m.db.PageCount) {
		return m, nil
	}
	return m.openPageNumber(uint32(page))
}

// openPageNumber selects and opens the nav row for the given page, if present.
func (m model) openPageNumber(page uint32) (tea.Model, tea.Cmd) {
	for idx, item := range m.navItems {
		if item.kind == navPage && item.pageNumber == page {
			m.selectedIndex = idx
			return m.openSelected()
		}
	}
	return m, nil
}

// stepWithin returns the page delta positions away from current inside the sorted set
// pages, and whether such a page exists (false when current is absent or the step falls
// off either end — the caller treats that as a no-op).
func stepWithin(pages []uint32, current uint32, delta int) (uint32, bool) {
	pos := -1
	for i, p := range pages {
		if p == current {
			pos = i
			break
		}
	}
	if pos == -1 {
		return 0, false
	}
	next := pos + delta
	if next < 0 || next >= len(pages) {
		return 0, false
	}
	return pages[next], true
}

func (m model) View() string {
	if m.width < 60 || m.height < 12 {
		return "terminal too small for badger"
	}

	navWidth := clamp(m.width/4, 24, 34)
	inspectorWidth := clamp(m.width/4, 28, 38)
	explorerWidth := m.width - navWidth - inspectorWidth - 2
	bodyHeight := m.height - 1

	nav := paneStyle(m.focusedPane == navPane, navWidth, bodyHeight).Render(m.viewNavigation(navWidth-2, bodyHeight-2))
	explorer := paneStyle(m.focusedPane == explorerPane, explorerWidth, bodyHeight).Render(m.viewExplorer(explorerWidth-2, bodyHeight-2))
	inspector := paneStyle(m.focusedPane == inspectorPane, inspectorWidth, bodyHeight).Render(m.viewInspector(inspectorWidth-2, bodyHeight-2))

	body := lipgloss.JoinHorizontal(lipgloss.Top, nav, explorer, inspector)
	status := statusStyle.Width(m.width).Render(truncateLine(m.footerLine(), m.width))
	return lipgloss.JoinVertical(lipgloss.Left, body, status)
}

// Persistent key-hint bars. The hints are always visible in the footer; the contextual
// segment (a transient status, or the filter token while filtered) is prepended to them.
const (
	navKeys    = "tab focus · ↑↓ move · enter open · f filter · g overview · h header · [ ] page · q quit"
	filterKeys = "F clear · tab focus · enter open · [ ] page · q quit"
)

// footerLine builds the always-on footer: the key hints, with a leading context segment.
// While filtered the context is the filter token (and the filter-aware key set); otherwise
// it is the latest transient status, if any.
func (m model) footerLine() string {
	if m.isFiltered() {
		return m.filterToken() + "  |  " + filterKeys
	}
	if m.status != "" {
		return m.status + "  |  " + navKeys
	}
	return navKeys
}

// filterToken renders the active-filter indicator: the source icon + name, page count, and
// a degraded-walk tail (· k skipped + ⚠ page N unreadable) when some pages could not be
// read (design §4.6 / §4.7). The retry / hard-failure statuses are NOT shown here — they
// only occur while unfiltered, where the transient status segment surfaces them.
func (m model) filterToken() string {
	obj := m.activeFilter.object
	pages, _ := m.filteredPages()

	var b strings.Builder
	fmt.Fprintf(&b, "⦿ filtered: %s %s (%d pg", objectIcon(obj), obj.Name, len(pages))
	walk, ok := m.pageIndex.Walk(obj.RootPage)
	if ok && len(walk.Skipped) > 0 {
		fmt.Fprintf(&b, " · %d skipped", len(walk.Skipped))
	}
	b.WriteString(")")
	if ok && len(walk.Skipped) > 0 {
		fmt.Fprintf(&b, " | ⚠ page %d unreadable", walk.Skipped[0].Page)
	}
	return b.String()
}

func (m model) selectedItem() navItem {
	if len(m.navItems) == 0 {
		return navItem{kind: navOverview, title: "Overview"}
	}
	if m.selectedIndex < 0 || m.selectedIndex >= len(m.navItems) {
		return m.navItems[0]
	}
	return m.navItems[m.selectedIndex]
}

func (m model) selectedPageRow() *pageRowViewModel {
	if len(m.pageRows) == 0 {
		return nil
	}
	if m.explorerIndex < 0 || m.explorerIndex >= len(m.pageRows) {
		return &m.pageRows[0]
	}
	return &m.pageRows[m.explorerIndex]
}

func (m model) viewNavigation(width int, height int) string {
	rows := make([]string, 0, len(m.navItems)+8)
	selected := m.selectedItem()

	rows = append(rows, titleStyle.Render("Navigation"))
	rows = append(rows, "")

	visible := m.visibleNavItems(height - 2)
	lastSection := ""
	for _, row := range visible {
		if row.section != "" && row.section != lastSection {
			if len(rows) > 2 {
				rows = append(rows, "")
			}
			rows = append(rows, sectionStyle.Render(strings.ToUpper(row.section)))
			lastSection = row.section
		}

		lineStyle := navItemStyle
		if row.index == m.selectedIndex {
			lineStyle = selectedNavItemStyle
		}
		rows = append(rows, lineStyle.Width(width).Render(m.navMarker(row.index)+row.text))
	}

	if selected.kind == navPage && m.loading {
		rows = append(rows, "", mutedStyle.Render("Loading page..."))
	}

	return fitVertical(rows, height)
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
			text = objectIcon(*item.schema) + " " + item.title
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
		lines = m.viewPage(width)
	default:
		lines = []string{"No content"}
	}

	return fitVertical(lines, height)
}

func (m model) viewOverview(width int) []string {
	lines := []string{
		titleStyle.Render("Overview"),
		"",
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

	lines = append(lines, "", mutedStyle.Render("Press enter to open the selected item from navigation."))
	return wrapLines(lines, width)
}

func (m model) viewDBHeader(width int) []string {
	header := m.db.HeaderRows
	lines := []string{
		titleStyle.Render("Database Header"),
		"",
	}
	for _, row := range header {
		lines = append(lines, fmt.Sprintf("%-24s %s", row.Label+":", row.Value))
	}
	return wrapLines(lines, width)
}

func (m model) viewSchemaObject(width int) []string {
	item := m.selectedItem()
	obj := item.schema
	if obj == nil {
		return []string{titleStyle.Render("Schema Object"), "", "No schema object selected."}
	}

	rootLine := fmt.Sprintf("Root page: %d", obj.RootPage)
	if obj.RootPage == 0 {
		rootLine = "Root page: — (no b-tree)"
	}

	lines := []string{
		titleStyle.Render(objectIcon(*obj) + "  " + strings.ToUpper(obj.Type) + "  " + obj.Name),
		"",
		fmt.Sprintf("Type: %s", obj.Type),
		fmt.Sprintf("Name: %s", obj.Name),
		fmt.Sprintf("Table: %s", obj.TableName),
		rootLine,
		"",
		sectionStyle.Render("SQL"),
		obj.SQL,
	}
	return wrapLines(lines, width)
}

func (m model) viewPage(width int) []string {
	item := m.selectedItem()
	pageTitle := "Page"
	if m.isFiltered() {
		pageTitle = objectIcon(m.activeFilter.object) + " Page"
	}
	lines := []string{
		titleStyle.Render(pageTitle),
		"",
		fmt.Sprintf("Page number: %d", item.pageNumber),
	}

	if m.loading {
		lines = append(lines, "", "Loading page details...")
		return wrapLines(lines, width)
	}

	if m.currentPage == nil || m.currentPage.PageNumber != item.pageNumber {
		lines = append(lines, "", "Press enter to load this page.")
		return wrapLines(lines, width)
	}

	page := m.currentPage
	lines = append(lines,
		fmt.Sprintf("Page %d | %s | size %d | cells %d | cell area %d | freeblock %d | frag %d",
			page.PageNumber,
			pageKindLabel(page.BTreePage.PageHeader.PageKind.Value),
			len(page.BTreePage.Raw),
			page.BTreePage.PageHeader.CellCount.Value,
			page.BTreePage.PageHeader.CellContentAreaOffset.Value,
			page.BTreePage.PageHeader.FirstFreeblock.Value,
			page.BTreePage.PageHeader.FragmentedFreeBytes.Value,
		),
		"",
		sectionStyle.Render("STRUCTURES"),
		fmt.Sprintf("%-18s %-14s %-7s %s", "Kind", "Range", "Size", "Notes"),
	)

	for _, row := range m.visiblePageRows(width, 12) {
		lineStyle := navItemStyle
		prefix := "  "
		if row.index == m.explorerIndex {
			lineStyle = selectedNavItemStyle
			prefix = "> "
		}
		text := fmt.Sprintf("%-18s %-14s %-7s %s", row.Title, row.RangeLabel, row.SizeLabel, row.Notes)
		lines = append(lines, lineStyle.Width(width).Render(prefix+truncateLine(text, width-2)))
	}

	lines = append(lines, "", mutedStyle.Render("Focus explorer to move through page structures."))

	return wrapLines(lines, width)
}

func (m model) viewInspector(width int, height int) string {
	item := m.selectedItem()
	if m.active.kind == navPage {
		if content := m.viewPageInspector(width); content != "" {
			return m.renderInspectorViewport(strings.Split(content, "\n"), width, height)
		}
	}

	lines := []string{
		titleStyle.Render("Inspector"),
		"",
		fmt.Sprintf("Selected: %s", item.title),
	}

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
			fmt.Sprintf("Schema cookie: %d", m.db.DBHeader.SchemaCookie),
			fmt.Sprintf("Schema format: %d", m.db.DBHeader.SchemaFormat),
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
				"",
				sectionStyle.Render("ACTIONS"),
			)
			if m.objectIsFilterSource(obj) {
				lines = append(lines, "- F        clear filter", "- enter    open object")
			} else {
				lines = append(lines, "- f        filter pages to this b-tree", "- enter    open object")
			}
		}
	case navPage:
		lines = append(lines,
			"",
			sectionStyle.Render("DETAIL"),
			fmt.Sprintf("Page number: %d", item.pageNumber),
			fmt.Sprintf("File offset: %d", (item.pageNumber-1)*m.db.PageSize),
		)
		if m.currentPage != nil && m.currentPage.PageNumber == item.pageNumber {
			header := m.currentPage.BTreePage.PageHeader
			lines = append(lines,
				fmt.Sprintf("Page kind: %s", pageKindLabel(header.PageKind.Value)),
				fmt.Sprintf("Header bytes: %d..%d", header.Meta.StartOffset, header.Meta.EndOffset()),
				fmt.Sprintf("Cell pointers: %d", len(m.currentPage.BTreePage.CellPointerArray.Pointers)),
			)
		}
		lines = append(lines,
			"",
			sectionStyle.Render("ACTIONS"),
			"- enter to load page",
			"- [ previous page",
			"- ] next page",
		)
	}

	if m.err != nil {
		lines = append(lines, "", errorStyle.Render(m.err.Error()))
	}

	return m.renderInspectorViewport(lines, width, height)
}

func (m model) viewPageInspector(width int) string {
	item := m.selectedItem()
	lines := []string{
		titleStyle.Render("Inspector"),
		"",
		fmt.Sprintf("Selected: page %d", item.pageNumber),
	}

	if m.currentPage == nil || m.currentPage.PageNumber != item.pageNumber {
		lines = append(lines, "", "Load the page to inspect its structures.")
		return strings.Join(lines, "\n")
	}

	row := m.selectedPageRow()
	if row == nil {
		lines = append(lines, "", "No page structure selected.")
		return strings.Join(lines, "\n")
	}

	pageSize := m.pageSizeForCurrentPage()
	fileStart := row.Meta.FileStartOffset(item.pageNumber, pageSize)
	fileEndExclusive := row.Meta.FileEndOffset(item.pageNumber, pageSize)
	fileEnd := fileEndExclusive
	if row.Meta.Size > 0 {
		fileEnd = fileEndExclusive - 1
	}

	lines = append(lines,
		fmt.Sprintf("Type: %s", row.Title),
		fmt.Sprintf("Page range: %s", row.RangeLabel),
		fmt.Sprintf("File range: %d..%d", fileStart, fileEnd),
		fmt.Sprintf("Byte size: %d", row.Meta.Size),
		"",
		sectionStyle.Render("RAW BYTES"),
		row.RawHex,
	)
	if row.RawASCII != "" {
		lines = append(lines, "ASCII: "+row.RawASCII)
	}

	lines = append(lines, "", sectionStyle.Render("BYTE MAP"))
	lines = append(lines, row.ByteMap...)

	lines = append(lines, "", sectionStyle.Render("PARSED FIELDS"))
	for _, field := range row.ParsedFields {
		lines = append(lines, fmt.Sprintf("%s: %s", field.Label, field.Value))
	}

	lines = append(lines, "", sectionStyle.Render("DECODED"))
	lines = append(lines, row.DecodedLines...)

	return strings.Join(lines, "\n")
}

func paneStyle(focused bool, width int, height int) lipgloss.Style {
	border := lipgloss.NormalBorder()
	color := lipgloss.Color("240")
	if focused {
		color = lipgloss.Color("33")
	}
	return lipgloss.NewStyle().
		Border(border).
		BorderForeground(color).
		Padding(0, 1).
		Width(width).
		Height(height)
}

func (m model) renderInspectorViewport(lines []string, width int, height int) string {
	contentWidth := width - 2
	if contentWidth < 8 {
		contentWidth = width
	}

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

func renderScrollbar(lines []string, contentWidth int, height int, scroll int, maxScroll int) string {
	if height <= 0 {
		return ""
	}

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

	out := make([]string, 0, height)
	for idx := 0; idx < height; idx++ {
		padded := lipgloss.NewStyle().Width(contentWidth).Render(lines[idx])
		out = append(out, padded+" "+track[idx])
	}
	return strings.Join(out, "\n")
}

type visiblePageRow struct {
	index int
	pageRowViewModel
}

func (m model) visiblePageRows(width int, limit int) []visiblePageRow {
	if len(m.pageRows) == 0 {
		return nil
	}

	if limit <= 0 || limit > len(m.pageRows) {
		limit = len(m.pageRows)
	}
	start := m.explorerIndex - limit/2
	if start < 0 {
		start = 0
	}
	end := start + limit
	if end > len(m.pageRows) {
		end = len(m.pageRows)
		start = max(0, end-limit)
	}

	rows := make([]visiblePageRow, 0, end-start)
	for idx := start; idx < end; idx++ {
		rows = append(rows, visiblePageRow{
			index:            idx,
			pageRowViewModel: m.pageRows[idx],
		})
	}
	_ = width
	return rows
}

func (m model) pageSizeForCurrentPage() uint32 {
	if m.currentPage != nil && m.currentPage.DBHeader != nil && m.currentPage.DBHeader.PageSize > 0 {
		return m.currentPage.DBHeader.PageSize
	}
	if m.db.PageSize > 0 {
		return m.db.PageSize
	}
	return 1
}

var (
	titleStyle           = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("230"))
	sectionStyle         = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("110"))
	statusStyle          = lipgloss.NewStyle().Foreground(lipgloss.Color("252")).Background(lipgloss.Color("236")).Padding(0, 1)
	navItemStyle         = lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	selectedNavItemStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("230")).Background(lipgloss.Color("24"))
	mutedStyle           = lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
	errorStyle           = lipgloss.NewStyle().Foreground(lipgloss.Color("203"))
	scrollbarTrackStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("238"))
	scrollbarThumbStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("117"))
)

// buildNavItems builds the flat nav list. Tables and indexes share one B-TREES section.
// The PAGES section lists filteredPages when filter != nil (an empty list for a virtual
// table) and the full 1..PageCount otherwise. The root page is intentionally not shown on
// B-TREES rows (it lives in the detail / summary panes — design §2).
func buildNavItems(db databaseViewModel, filter *filterSource, filteredPages []uint32) []navItem {
	items := []navItem{
		{kind: navOverview, title: "Overview"},
		{kind: navDBHeader, title: "DB Header"},
	}

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
		for _, pageNumber := range filteredPages {
			items = append(items, navItem{
				kind:       navPage,
				title:      fmt.Sprintf("page %d", pageNumber),
				pageNumber: pageNumber,
			})
		}
	} else {
		for pageNumber := uint32(1); pageNumber <= db.PageCount; pageNumber++ {
			items = append(items, navItem{
				kind:       navPage,
				title:      fmt.Sprintf("page %d", pageNumber),
				pageNumber: pageNumber,
			})
		}
	}

	return items
}

// objectIcon maps a schema object to its B-TREES glyph: ◈ index, ⊞ virtual table / view
// (no b-tree, RootPage == 0), ▦ ordinary table. Echoed in nav rows, detail/page titles,
// and the footer filter token. Glyphs are placeholders pending terminal testing (design §7).
func objectIcon(obj schemaObjectViewModel) string {
	switch {
	case obj.Type == "index":
		return "◈"
	case obj.RootPage == 0:
		return "⊞"
	default:
		return "▦"
	}
}

// indexOfBTreeRow returns the nav index of the B-TREES row for obj, or 0 if absent.
func indexOfBTreeRow(items []navItem, obj schemaObjectViewModel) int {
	for idx, item := range items {
		if (item.kind == navTable || item.kind == navIndex) && item.schema != nil &&
			item.schema.Name == obj.Name && item.schema.RootPage == obj.RootPage {
			return idx
		}
	}
	return 0
}

// collectBTreeRoots returns the unique, non-zero b-tree root pages across the database's
// tables and indexes. A root belongs to exactly one object, but it dedupes defensively;
// RootPage == 0 (views / virtual tables, which have no b-tree) is skipped.
func collectBTreeRoots(db databaseViewModel) []uint32 {
	seen := make(map[uint32]bool)
	roots := make([]uint32, 0, len(db.Tables)+len(db.Indexes))

	collect := func(objects []schemaObjectViewModel) {
		for _, obj := range objects {
			if obj.RootPage == 0 || seen[obj.RootPage] {
				continue
			}
			seen[obj.RootPage] = true
			roots = append(roots, obj.RootPage)
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

func sectionForNavItem(item navItem) string {
	switch item.kind {
	case navOverview, navDBHeader:
		return "Main"
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
		if strings.TrimSpace(line) == "" {
			out = append(out, "")
			continue
		}
		out = append(out, lipgloss.NewStyle().Width(width).Render(line))
	}
	return out
}

func wrapInspectorLines(lines []string, width int) []string {
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			out = append(out, "")
			continue
		}
		if strings.Contains(line, "\x1b[") {
			out = append(out, lipgloss.NewStyle().Width(width).Render(line))
			continue
		}

		runes := []rune(line)
		for len(runes) > width && width > 0 {
			out = append(out, string(runes[:width]))
			runes = runes[width:]
		}
		if len(runes) == 0 {
			out = append(out, "")
			continue
		}
		out = append(out, string(runes))
	}
	return out
}

func mutedInline(text string) string {
	return mutedStyle.Render(text)
}

func pageKindLabel(kind sqlite.PageKindType) string {
	switch kind {
	case sqlite.InteriorIndexBTreePage:
		return "interior index"
	case sqlite.InteriorTableBTreePage:
		return "interior table"
	case sqlite.LeafIndexBTreePage:
		return "leaf index"
	case sqlite.LeafTableBTreePage:
		return "leaf table"
	default:
		return fmt.Sprintf("0x%02x", kind)
	}
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
