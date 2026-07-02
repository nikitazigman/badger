# Page Hex View Implementation Map

> Feature: `feature/hex_view`
> Source design: [design.md](design.md)
> Scope note: implement the new HEX/META flow and skip the `i` info view for the first pass.

## Objective

Replace the current page detail table with a structured 16-byte hex map:

- `[2] PAGES` selects and loads the active database page.
- `[3] HEX` renders the active page bytes and owns parsed block selection.
- `[4] META` renders parsed context for either the selected page or selected hex block.

The first implementation should preserve the existing left navigation and page loading behavior, but change the page-specific middle and right panes to the new design.

## Current Code Map

### `internal/tui/model.go`

Owns the Bubble Tea model state, input handling, and most rendering.

Current relevant state:

- `focusedPane`: `navPane`, `explorerPane`, `inspectorPane`.
- `active`: active content target, including `navPage`.
- `currentPage`: loaded `sqlite.PageInspection`.
- `pageRows`: current page structure rows from `buildPageRows`.
- `explorerIndex`: currently selected page structure row.
- `inspectorScroll`: scroll offset for `[4] META`.
- `loading`, `loadingVisible`: delayed page loading display.

Current page behavior:

- `2` focuses `navPane`, selects first `navPage`, and activates it.
- Moving within `navPane` auto-activates selected pages.
- `3` focuses `explorerPane`.
- `4` focuses `inspectorPane`.
- Up/down in `explorerPane` moves `explorerIndex` through `pageRows`.
- Up/down in `inspectorPane` scrolls `inspectorScroll`.
- `viewPage` renders a page summary plus a `STRUCTURES` table.
- `viewPageInspector` renders raw bytes, ASCII, byte map, parsed fields, and decoded lines for the selected row.

Needed changes:

- Reinterpret `explorerPane` as `[3] HEX` for `navPage`.
- Keep `inspectorPane` as `[4] META`.
- Add page-specific state for selected hex block, drill mode, and hex scroll.
- Make page-level meta depend on focus/source, not on an independent inspector selection.

### `internal/tui/page_view.go`

Builds `pageRowViewModel` values from `sqlite.PageInspection`.

Current useful fields:

- `Type`: structure kind.
- `Title`: display name.
- `Meta`: page-local byte range.
- `ParsedFields`: parsed key/value data.
- `DecodedLines`: decoded payload text.
- `SortStart`: physical ordering offset.

Current output is already sorted by physical page offset and includes:

- database header on page 1
- b-tree page header
- pointer array
- freeblocks
- unallocated regions
- table leaf cells
- table interior cells
- index leaf cells
- index interior cells

Needed changes:

- Either rename `pageRowViewModel` to a block-oriented type or layer a new `pageHexBlock` type over it.
- Remove raw byte and ASCII concepts from meta rendering.
- Add child block builders for drill mode.
- Add richer typed fields where current generic `ParsedFields` are not enough for the proposed meta renders.

### `internal/sqlite`

Already exposes the parsed structures needed for the first version:

- `PageInspection`, `BTreePage`, `DBHeader`
- `PageHeader`
- `CellPointerArray`
- `Freeblock`
- `UnallocatedRegion`
- `TableLeafCell`
- `TableInteriorCell`
- `IndexLeafCell`
- `IndexInteriorCell`
- `RecordPayload`
- `Meta`

The TUI should not need parser changes for the initial implementation unless tests reveal missing data for a specific meta field.

## Target State Model

Add page-specific UI state to `model`.

Suggested fields:

```go
hexBlocks        []pageHexBlock
hexBlockIndex    int
hexDrillParent   int
hexDrillBlocks   []pageHexBlock
hexDrilled       bool
hexScrollLine    int
metaSource       pageMetaSource
```

Alternative: keep `pageRows` as top-level blocks and add only child/drill state. If using this path, rename in a later cleanup only after behavior is stable.

Suggested supporting types:

```go
type pageMetaSource int

const (
	pageMetaFromPages pageMetaSource = iota
	pageMetaFromHex
)

type pageHexBlock struct {
	Type       pageHexBlockType
	Title      string
	Parent     string
	Meta       sqlite.Meta
	Style      hexBlockStyle
	Fields     []labelValue
	Decoded    []string
	CellIndex  int
	CanDrill   bool
	Children   []pageHexBlock
	SortStart  int
}
```

The exact type shape can be smaller if the implementation reuses existing `pageRowViewModel` fields.

## Target Navigation

### Focus in `[2] PAGES`

Required behavior:

- Up/down/k/j select previous or next page within the PAGES section.
- Moving page selection auto-loads and renders the selected page.
- `[4] META` shows page-level metadata while focus remains in `[2]`.
- `3` focuses `[3] HEX` and selects the first top-level block for the loaded page.
- `4` focuses `[4] META` for the selected page.
- Moving to another page resets hex selection, drill state, hex scroll, and meta scroll.

Implementation tasks:

- Update `handleKey("3")` to call a helper like `focusHexPane()`.
- Update `activateSelected()` for `navPage` to reset hex state.
- Update `pageLoadedMsg` handling to build top-level blocks and initialize indexes.
- Track `metaSource = pageMetaFromPages` when focus enters or remains in PAGES.

### Focus in `[3] HEX`

Required behavior:

- Up/down/k/j select previous or next block at the current granularity.
- Selection is block-based, not row-based.
- Moving block selection resets meta scroll to top.
- Moving block selection updates `[4] META`.
- `d` drills into selected block if child ranges exist, or returns to parent granularity if already drilled.
- `4` focuses `[4] META` without changing hex selection.
- If selected block is outside the visible hex viewport, scroll to reveal it.

Implementation tasks:

- Replace `moveExplorerSelection` with `moveHexSelection`, or route page-specific explorer movement to a new helper.
- Add `toggleHexDrill()`.
- Add `selectedHexBlock()` and `activeHexBlocks()` helpers.
- Add `ensureSelectedHexBlockVisible(contentHeight int)`.
- Reset `metaSource = pageMetaFromHex` when focus enters or moves in HEX.
- Do not implement `i` for the first pass.

### Focus in `[4] META`

Required behavior:

- Up/down/k/j scroll meta content.
- `3` returns focus to HEX without changing the selected block.
- Meta has no independent selection cursor.

Implementation tasks:

- Keep existing `scrollInspector`.
- Ensure `3` preserves `hexBlockIndex` and drill state.
- Ensure meta content derives from `metaSource` and current focus context.

## Hex Rendering Map

### Layout

Render page bytes in 16-byte rows:

```text
Offset   00 01 02 03 04 05 06 07  08 09 0A 0B 0C 0D 0E 0F
0000     XX XX XX XX XX XX XX XX  XX XX XX XX XX XX XX XX
```

No ASCII column, legend, page summary header, or selected-block footer inside `[3] HEX`.

### Byte Ownership

Build a style map for each page byte:

- database header
- page header
- pointer array
- freeblock
- unallocated
- table leaf cell
- table interior cell
- index leaf cell
- index interior cell
- drilled child block
- selected block

Top-level ownership comes from the physically sorted blocks. Drill ownership overlays child blocks only within the drilled parent range.

### Selection Highlighting

Selection applies to byte ranges, not visual rows:

- If the selected block spans multiple rows, every visible byte in the range uses selected styling.
- If a selected range starts or ends mid-row, only bytes in the range are selected.
- Non-selected known ranges use their block style.
- Unknown/unparsed bytes use default muted styling.

### Scrolling

The hex viewport scrolls by rendered row number.

Implementation tasks:

- Compute row count from `len(page.BTreePage.Raw)`.
- Compute selected start and end rows from `Meta.StartOffset / 16` and `(Meta.EndOffset()-1) / 16`.
- Keep selected start row visible when moving to a block outside the viewport.
- Preserve current viewport when moving within visible range.

## Meta Rendering Map

Meta must never show raw hex or ASCII.

### Page Meta

Shown when focus/source is `[2] PAGES`.

Fields:

- `Page N`
- `Type`
- `Page size`
- `File offset`
- `STRUCTURE`
- `Cells`
- `Cell content start`
- `First freeblock`
- `Fragmented free bytes`
- `Pointer array`
- `Freeblocks`
- `Unallocated`
- `BTREE`
- `Object`
- `Root page`

Object/root lookup can use the active filter when present. Without a filter, either omit object context or infer from `pageIndex` if simple and reliable.

### Database Header Block Meta

Shown for page 1 bytes `0..99`.

Fields:

- range and size
- page size
- page count
- read/write versions
- reserved bytes per page
- freelist pages
- schema cookie
- schema format
- encoding
- SQLite version

### Page Header Block Meta

Fields:

- page kind
- first freeblock
- cell count
- cell content start
- fragmented free bytes
- right-most pointer for interior pages

### Pointer Array Block Meta

Fields:

- entries
- entry size
- points to cell offsets
- pointer list, truncated naturally by meta scroll

Optional first-pass drill:

- Pointer entry sub-blocks are useful but can be a separate task if cell drill is implemented first.

### Freeblock Block Meta

Fields:

- next freeblock
- block size
- reusable yes/no

### Unallocated Block Meta

Fields:

- parsed structure: none
- role: gap before cell area

### Cell Block Meta

Separate by cell kind:

- table leaf cell
- table interior cell
- index leaf cell
- index interior cell

Fields should include available parsed values:

- rowid or rowid separator
- left child page
- payload size
- record payload range
- local payload size
- overflow yes/no

### Drill Sub-Block Meta

Initial useful child ranges:

- payload size
- rowid
- left child page
- record payload
- record header size
- serial type
- record value
- overflow pointer

Each child meta should include:

- title
- parent title
- offset/range
- size
- parsed value/meaning where available

## Drill Implementation Map

### First-Pass Drill Targets

Implement drill for cells first:

- table leaf cell:
  - payload size
  - rowid
  - record payload
  - record header size
  - serial types
  - record values
  - overflow pointer if present
- table interior cell:
  - left child page
  - rowid
- index leaf cell:
  - payload size
  - record payload
  - record header size
  - serial types
  - record values
  - overflow pointer if present
- index interior cell:
  - left child page
  - payload size
  - record payload
  - record header size
  - serial types
  - record values
  - overflow pointer if present

Defer pointer-array drill if time is tight; it is explicitly an open design question.

### Toggle Behavior

- Top level plus selected block with children plus `d`: enter drill mode.
- Drilled plus `d`: return to top level and reselect the parent block.
- Top level plus selected block without children plus `d`: no-op.
- Page change: exit drill mode.

## Styling Tasks

Current styles live at the bottom of `internal/tui/model.go`.

Add a small set of hex byte styles:

- header
- pointer array
- unallocated
- freeblock
- cell
- drilled child
- selected
- unknown/default

Keep the palette readable in low-color terminals. Avoid relying only on foreground hue; selected styling should use a background.

## Test Plan

Add focused tests in `internal/tui/keys_test.go` or a new `internal/tui/page_hex_test.go`.

Required behavioral tests:

- `3` from a loaded page focuses HEX and selects the first block.
- Up/down in HEX changes selected block, not page navigation.
- Moving HEX selection resets `inspectorScroll`.
- `4` from PAGES shows page meta, not block meta.
- `4` from HEX preserves selected block meta.
- Up/down in META scrolls only meta content.
- Page movement in PAGES resets hex selection and drill state.
- `d` enters drill for a cell with children.
- `d` again exits drill and reselects the parent block.
- `d` on a block without children is a no-op.

Required rendering tests:

- `[3] HEX` title appears instead of `[3] DETAIL`.
- Page view contains `Offset` and 16 hex columns.
- Page view does not contain `STRUCTURES`, raw ASCII, or selected-block footer.
- Page meta does not contain raw hex.
- Block meta contains title, offset/range, size, and parsed fields.

Useful lower-level tests:

- Top-level block builder sorts by physical offset.
- Page 1 includes database header before page header.
- Selected range across multiple rows marks only bytes in the selected range.
- Hex viewport scroll reveals a selected block outside the visible rows.

## Implementation Order

1. Add/adjust page block data model.
2. Build top-level hex blocks from `sqlite.PageInspection`.
3. Add block selection, page reset, and focus-source state.
4. Replace page detail table rendering with 16-byte HEX rendering.
5. Replace page inspector raw output with page/block META renderers.
6. Add cell drill child builders and `d` navigation.
7. Add tests for navigation and rendering.
8. Run `go test ./...`.
9. Do a manual TUI smoke check on `fixtures/companies.db` if practical.

## Reviewable Milestones

Each milestone should leave the TUI in a manually testable state so design and behavior can be reviewed before the next layer is added.

### 1. Static HEX Pane + Page META

Task:

- Replace the page structure table in `[3]` with a 16-byte hex grid for the active page.
- Rename `[3] DETAIL` to `[3] HEX`.
- Render the compact offset column and byte columns only.
- Replace page meta in `[4]` with parsed page-level metadata.
- Do not implement block highlighting, block navigation, or drill yet.

Definition of done:

- Selecting pages in `[2] PAGES` loads page bytes into `[3] HEX`.
- `[3] HEX` shows `Offset` and columns `00` through `0F`.
- No ASCII column is rendered.
- The old `STRUCTURES` table is gone from the page view.
- `[4] META` shows page number, page type, page size, file offset, cells, pointer array size, freeblocks, unallocated bytes, and b-tree context where available.
- `[4] META` does not show raw hex or ASCII.
- Existing page loading behavior still works.

Manual review:

- Open `fixtures/companies.db`.
- Press `2`, move through pages, and confirm the hex bytes change with the page.
- Confirm `[3]` looks like a byte map rather than a summary/table view.
- Confirm `[4] META` explains the selected page while focus remains in `[2] PAGES`.

### 2. Top-Level Block Coloring, Selection + Block META

Task:

- Build top-level parsed blocks from the current page inspection.
- Style bytes by block ownership: database header, page header, pointer array, freeblock, unallocated, and cells.
- Add block-based selection in `[3] HEX` using up/down/k/j.
- Auto-select the first top-level block when focusing `[3]`.
- Scroll the hex viewport to reveal the selected block.
- Add block-specific meta views for every top-level block type.

Definition of done:

- Pressing `3` focuses HEX and selects the first parsed block.
- Up/down in HEX moves between parsed byte blocks, not visual rows.
- The selected block is highlighted across every visible byte segment it owns.
- `[4] META` changes from page meta to selected block meta when focus is in HEX.
- Block meta includes title, offset/range, size, and parsed fields.
- Block meta covers database header, page header, pointer array, freeblock, unallocated region, and all cell kinds.
- Pressing `4` focuses META for the selected block, and scrolling META does not change selection.
- Moving between pages resets block selection and scroll.

Manual review:

- Press `2`, select a page, then press `3`.
- Move through blocks and confirm selection jumps from header to pointer array to physical page regions/cells.
- Confirm selected cells far down the page scroll into view.
- Confirm `[4] META` explains whichever block is selected.
- Press `4`, scroll meta, then press `3` and confirm the same block is still selected.

### 3. Drill + Drill META

Task:

- Add one-level drill state for selected blocks with children.
- Implement cell child ranges first: payload size, rowid/left child, record payload, record header size, serial types, values, overflow pointer where present.
- Keep the full-page hex grid visible while drilling.
- Use child-range styling and selection inside the drilled parent.
- Add child-specific meta views for every implemented drill child type.

Definition of done:

- Pressing `d` on a drillable cell enters child selection mode.
- Up/down moves between child ranges inside that cell.
- Pressing `d` again returns to top-level selection and reselects the parent cell.
- Pressing `d` on a non-drillable block is a no-op.
- `[4] META` explains the selected child range while drilled.
- Drill meta includes title, parent title, offset/range, size, parsed value, and meaning where available.
- Page changes exit drill mode.

Manual review:

- Select a cell in HEX.
- Press `d` and confirm the same page remains visible but the selection granularity changes to parts of the cell.
- Move through payload size, rowid/left child, record header, serial type, and value ranges and confirm META changes for each.
- Press `d` again and confirm the parent cell is selected again.

### 4. Navigation And Focus Polish

Task:

- Make focus transitions exactly match the design.
- Ensure `[2] PAGES`, `[3] HEX`, and `[4] META` each have the correct local controls.
- Reset page-specific state consistently on page changes.
- Keep meta source explicit: page meta from PAGES, block/drill meta from HEX.

Definition of done:

- `2` focuses PAGES and page movement updates page meta.
- `3` focuses HEX and selects the first block if none is selected.
- `4` focuses META for the current page/block/drill source.
- Up/down in META scrolls only META content.
- `3` from META returns to HEX without changing selection.
- Moving to another page exits drill mode, resets block selection, resets hex scroll, and resets meta scroll.

Manual review:

- Move between all three panes using `2`, `3`, and `4`.
- Confirm each pane's arrow keys affect only that pane's responsibility.
- Confirm page changes reset HEX/drill state cleanly.

### 5. Visual Refinement And Coverage Gaps

Task:

- Refine color/style choices for block ownership and selected bytes.
- Fill in any missing meta fields that are already available from `internal/sqlite`.
- Decide whether pointer-array drill should be added now or remain deferred.
- Tighten behavior around narrow panes and long meta values.

Definition of done:

- The hex map is readable across normal terminal sizes.
- Selection is visually clear when a block starts or ends mid-row.
- Long meta values wrap or truncate cleanly.
- No permanent legend/help text appears in HEX.
- No raw bytes appear in META.

Manual review:

- Inspect page 1, a leaf table page, and an index page if available.
- Confirm each block style is distinguishable enough.
- Resize the terminal within reasonable bounds and confirm the layout remains usable.

### 6. Polish, Tests, And Footer

Task:

- Tighten footer hints for the implemented feature set.
- Add navigation and rendering tests.
- Fix any visual regressions discovered during manual review.
- Run the full test suite.

Definition of done:

- Footer does not advertise skipped features such as `i info`.
- Tests cover page loading, HEX focus, block movement, drill toggle, page meta, block meta, and META scrolling.
- `go test ./...` passes.
- The TUI is manually smoke-tested against at least `fixtures/companies.db`.

Manual review:

- Exercise the complete flow: select page, inspect HEX, move blocks, drill, inspect META, scroll META, change page.
- Confirm the UI matches the agreed design closely enough for the first complete pass.

## Deferred

- `i` info/legend view.
- Pointer-array entry drill, unless it falls out cheaply after the cell drill model exists.
- Deeper drill levels beyond one parent/child toggle.
- Inferring page ownership across all b-trees when no filter is active.
- Parser changes for fields not already exposed by `internal/sqlite`.
