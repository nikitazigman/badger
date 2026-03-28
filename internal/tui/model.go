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

type model struct {
	inspector     *sqlite.Inspector
	db            databaseViewModel
	navItems      []navItem
	selectedIndex int
	explorerIndex int
	active        contentTarget
	currentPage   *sqlite.PageInspection
	pageRows      []pageRowViewModel
	focusedPane   pane
	width         int
	height        int
	status        string
	loading       bool
	err           error
}

func newModel(inspector *sqlite.Inspector, metadata *sqlite.MetadataInspection) (model, error) {
	db, err := newDatabaseViewModel(metadata)
	if err != nil {
		return model{}, err
	}

	return model{
		inspector:     inspector,
		db:            db,
		navItems:      buildNavItems(db),
		active:        contentTarget{kind: navOverview},
		focusedPane:   navPane,
		width:         120,
		height:        34,
		status:        "tab focus | up/down move | enter open | g overview | h header | p pages | [ ] page | q quit",
		selectedIndex: 0,
	}, nil
}

func (m model) Init() tea.Cmd {
	return nil
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
		m.loading = false
		m.status = fmt.Sprintf("opened page %d", msg.page.PageNumber)
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
		}
		return m, nil
	case "down", "j":
		if m.focusedPane == navPane {
			m.moveSelection(1)
		} else if m.focusedPane == explorerPane && m.active.kind == navPage {
			m.moveExplorerSelection(1)
		}
		return m, nil
	case "esc":
		if m.active.kind == navPage && m.focusedPane == explorerPane && m.explorerIndex > 0 {
			m.explorerIndex = 0
			m.status = "returned to page summary"
			return m, nil
		}
		m.active = contentTarget{kind: navOverview}
		m.currentPage = nil
		m.pageRows = nil
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
		return m, nil
	case navDBHeader:
		m.status = "database header"
		m.currentPage = nil
		m.pageRows = nil
		return m, nil
	case navTable, navIndex:
		if item.schema != nil {
			m.active.schemaName = item.schema.Name
			m.status = fmt.Sprintf("opened %s %s", item.schema.Type, item.schema.Name)
		}
		m.currentPage = nil
		m.pageRows = nil
		return m, nil
	case navPage:
		m.active.pageNumber = item.pageNumber
		m.explorerIndex = 0
		m.pageRows = nil
		m.loading = true
		m.status = fmt.Sprintf("loading page %d", item.pageNumber)
		return m, loadPageCmd(m.inspector, item.pageNumber)
	default:
		return m, nil
	}
}

func (m model) openRelativePage(delta int) (tea.Model, tea.Cmd) {
	if m.active.kind != navPage || m.db.PageCount == 0 {
		return m, nil
	}

	page := int(m.active.pageNumber) + delta
	if page < 1 || page > int(m.db.PageCount) {
		return m, nil
	}

	for idx, item := range m.navItems {
		if item.kind == navPage && item.pageNumber == uint32(page) {
			m.selectedIndex = idx
			return m.openSelected()
		}
	}

	return m, nil
}

func (m model) View() string {
	if m.width < 60 || m.height < 12 {
		return "terminal too small for badger tui"
	}

	navWidth := clamp(m.width/4, 24, 34)
	inspectorWidth := clamp(m.width/4, 28, 38)
	explorerWidth := m.width - navWidth - inspectorWidth - 2
	bodyHeight := m.height - 1

	nav := paneStyle(m.focusedPane == navPane, navWidth, bodyHeight).Render(m.viewNavigation(navWidth-2, bodyHeight-2))
	explorer := paneStyle(m.focusedPane == explorerPane, explorerWidth, bodyHeight).Render(m.viewExplorer(explorerWidth-2, bodyHeight-2))
	inspector := paneStyle(m.focusedPane == inspectorPane, inspectorWidth, bodyHeight).Render(m.viewInspector(inspectorWidth-2, bodyHeight-2))

	body := lipgloss.JoinHorizontal(lipgloss.Top, nav, explorer, inspector)
	status := statusStyle.Width(m.width).Render(truncateLine(m.status, m.width))
	return lipgloss.JoinVertical(lipgloss.Left, body, status)
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
		prefix := "  "
		if row.index == m.selectedIndex {
			lineStyle = selectedNavItemStyle
			prefix = "> "
		}
		rows = append(rows, lineStyle.Width(width).Render(prefix+row.text))
	}

	if selected.kind == navPage && m.loading {
		rows = append(rows, "", mutedStyle.Render("Loading page..."))
	}

	return fitVertical(rows, height)
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
		if item.subtitle != "" {
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

	lines := []string{
		titleStyle.Render("Schema Object"),
		"",
		fmt.Sprintf("Type: %s", obj.Type),
		fmt.Sprintf("Name: %s", obj.Name),
		fmt.Sprintf("Table: %s", obj.TableName),
		fmt.Sprintf("Root page: %d", obj.RootPage),
		"",
		sectionStyle.Render("SQL"),
		obj.SQL,
	}
	return wrapLines(lines, width)
}

func (m model) viewPage(width int) []string {
	item := m.selectedItem()
	lines := []string{
		titleStyle.Render("Page"),
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
			return fitVertical(wrapLines(strings.Split(content, "\n"), width), height)
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
			lines = append(lines,
				"",
				sectionStyle.Render("DETAIL"),
				fmt.Sprintf("Type: %s", item.schema.Type),
				fmt.Sprintf("Root page: %d", item.schema.RootPage),
				fmt.Sprintf("Table name: %s", item.schema.TableName),
				"",
				sectionStyle.Render("ACTIONS"),
				"- enter to open summary",
				fmt.Sprintf("- open page %d", item.schema.RootPage),
			)
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

	return fitVertical(wrapLines(lines, width), height)
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
)

func buildNavItems(db databaseViewModel) []navItem {
	items := []navItem{
		{kind: navOverview, title: "Overview"},
		{kind: navDBHeader, title: "DB Header"},
	}

	for _, table := range db.Tables {
		table := table
		items = append(items, navItem{
			kind:     navTable,
			title:    table.Name,
			subtitle: fmt.Sprintf("root %d", table.RootPage),
			schema:   &table,
		})
	}

	for _, index := range db.Indexes {
		index := index
		items = append(items, navItem{
			kind:     navIndex,
			title:    index.Name,
			subtitle: fmt.Sprintf("root %d", index.RootPage),
			schema:   &index,
		})
	}

	for pageNumber := uint32(1); pageNumber <= db.PageCount; pageNumber++ {
		items = append(items, navItem{
			kind:       navPage,
			title:      fmt.Sprintf("page %d", pageNumber),
			pageNumber: pageNumber,
		})
	}

	return items
}

func sectionForNavItem(item navItem) string {
	switch item.kind {
	case navOverview, navDBHeader:
		return "Main"
	case navTable:
		return "Tables"
	case navIndex:
		return "Indexes"
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
