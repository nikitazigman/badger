# Codebase Map (relevant to filter-by-table)

Two packages matter: `internal/sqlite` (parsing/data) and `internal/tui` (rendering/state).

```
cmd/badger/main.go
  └─ tui.Run(path, out)                         internal/tui/app.go
       ├─ sqlite.Open(path) -> *Inspector       internal/sqlite/inspector.go
       ├─ inspector.InspectDatabaseMetadata()   -> *MetadataInspection (page 1 schema)
       └─ newModel(inspector, metadata)         internal/tui/model.go (Bubble Tea model)
```

---

## internal/sqlite — the data layer

### `Inspector` (`inspector.go`)
- `Open(path) (*Inspector, error)` — opens file, parses the 100-byte DB header.
- `Close()`.
- `readPage(number uint32) ([]byte, error)` — bounds-checked raw page read.
- `InspectDatabaseMetadata() (*MetadataInspection, error)` — reads page 1, parses
  `sqlite_schema` rows into `SchemaRecords []Row`.
- `InspectPage(number uint32) (*PageInspection, error)` — parses one page's full b-tree
  structure. **Lazy, per-page.** This is the only page-reading entry the TUI uses.

```go
type MetadataInspection struct {
    Path          string
    DBHeader      DBHeader
    SchemaRecords []Row      // each row: type, name, tbl_name, rootpage, sql, ...
}

type PageInspection struct {
    PageNumber uint32
    DBHeader   *DBHeader     // non-nil only for page 1
    BTreePage  BTreePage
}
```

### `BTreePage` (`btree_page.go`) — the parsed structure of ONE page
```go
type BTreePage struct {
    PageNumber         uint32
    Raw                []byte
    UsablePageBytes    uint16
    PageHeader         PageHeader
    CellPointerArray   CellPointerArray
    TableLeafCells     []TableLeafCell
    TableInteriorCells []TableInteriorCell   // each has LeftChildPage (Uint32Field)
    IndexLeafCells     []IndexLeafCell
    IndexInteriorCells []IndexInteriorCell   // each has LeftChildPage (Uint32Field)
    Freeblocks         []Freeblock
    UnallocatedRegions []UnallocatedRegion
}
```
- `parseBTreePage(...)` dispatches on `PageHeader.PageKind`.

### `PageHeader` (`page_header.go`)
```go
type PageHeader struct {
    Meta                  Meta
    PageKind              PageKindField   // 0x02 interior idx, 0x05 interior tbl, 0x0a leaf idx, 0x0d leaf tbl
    FirstFreeblock        Uint16Field
    CellCount             Uint16Field
    CellContentAreaOffset Uint16Field
    FragmentedFreeBytes   Uint8Field
    RightMostPointer      *Uint32Field    // non-nil ONLY for interior pages
}
func (h *PageHeader) IsInterior() bool
```
Page-kind constants: `InteriorIndexBTreePage`, `InteriorTableBTreePage`,
`LeafIndexBTreePage`, `LeafTableBTreePage`.

### Cells (`cells.go`) — the b-tree traversal primitives
- `TableInteriorCell.LeftChildPage Uint32Field` → child page number.
- `IndexInteriorCell.LeftChildPage Uint32Field` → child page number.
- Leaf cells carry `ParsedPayload *RecordPayload`; payload may have
  `OverflowFirstPage *Uint32Field` (start of an overflow page chain).

### `Meta` (`meta.go`)
- `{StartOffset, Size}` plus helpers: `EndOffset()`, `FileStartOffset(page,pageSize)`,
  `FileEndOffset(...)`. Every parsed field carries a `Meta` so the UI can show byte ranges.

### Other files
- `header.go` (`DBHeader`, `parseHeader`), `record.go` / `record_projection.go` /
  `payload.go` (record + payload decoding), `schema_definition.go` (CREATE TABLE/INDEX
  SQL parse), `utils.go` (varints, usable page size), `meta.go`. Plus `*_test.go`.

**Key gap:** there is **no** function that, given a root page, returns the set of pages in
that b-tree. Traversal would compose `InspectPage` + interior `LeftChildPage` +
`RightMostPointer` (+ overflow chains). This is the core new capability the feature needs.

---

## internal/tui — the rendering/state layer

### `model` (`model.go`) — the Bubble Tea model
Key fields:
```go
type model struct {
    inspector     *sqlite.Inspector
    db            databaseViewModel
    navItems      []navItem        // flat list: overview, header, each table, each index, each page
    selectedIndex int              // selected nav item
    explorerIndex int              // selected row within page explorer
    active        contentTarget    // what the explorer is currently showing
    currentPage   *sqlite.PageInspection
    pageRows      []pageRowViewModel
    focusedPane   pane             // navPane | explorerPane | inspectorPane
    width, height int
    status        string
    loading       bool
    err           error
}
```

- `navKind`: `navOverview | navDBHeader | navTable | navIndex | navPage`.
- `buildNavItems(db)` — builds the flat nav list. **This is where the Pages section is
  generated as a simple `for pageNumber := 1..PageCount` loop** — the natural place a
  filter would change behavior.
- `Update` / `handleKey` — keyboard handling (`tab`, arrows, `enter`, `g/h/p`, `[`/`]`,
  `esc`, `q`). Mouse motion enabled in `app.go` but click handling is minimal.
- Views: `viewNavigation`, `viewExplorer` → (`viewOverview` | `viewDBHeader` |
  `viewSchemaObject` | `viewPage`), `viewInspector` / `viewPageInspector`.
- Page loading is async via `loadPageCmd` (`app.go`) → emits `pageLoadedMsg`.

### View models (`view_model.go`)
```go
type schemaObjectViewModel struct { Type, Name, TableName string; RootPage uint32; SQL string }
type databaseViewModel struct {
    Path; PageSize; PageCount; DatabaseSizeBytes; FreelistPageCount;
    EncodingLabel; SQLiteVersionLabel; DBHeader; HeaderRows;
    Tables  []schemaObjectViewModel    // type == "table"
    Indexes []schemaObjectViewModel    // type == "index"
}
```
- `newDatabaseViewModel(metadata)` splits `SchemaRecords` into Tables / Indexes by `type`.
- Note `schemaObjectViewModel.TableName` (the index's owning table) is already captured —
  useful if a table filter should also pull in its indexes.

### Page rows (`page_view.go`)
- `buildPageRows(page *PageInspection) []pageRowViewModel` — turns one page's parsed
  structures into selectable, byte-ordered explorer rows (header, pointer array, cells,
  freeblocks, unallocated). Drives the inspector. Sorted by `SortStart`.

---

## Data flow summary
1. `tui.Run` opens the file, parses metadata (page 1 schema), builds the model.
2. Nav list = overview + header + tables + indexes + **all pages 1..N**.
3. Selecting a page → async `InspectPage` → `buildPageRows` → explorer + inspector.
4. There is currently **no** relationship drawn between a selected table/index and the
   page list — that link is exactly what this feature introduces.
