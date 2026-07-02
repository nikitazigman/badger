# Page Hex View Design

> Feature: **Hex View** (`feature/hex_view`)
> Source feedback: [better_ui feedback.md](../better_ui/feedback.md), item 5
> Status: design draft

## Goal

The page view should make SQLite page bytes understandable as a physical data layer.
The current page view has the required data, but it splits understanding across a
structure table and a meta pane. Users have to mentally connect byte ranges, raw bytes,
and parsed fields.

The new design makes `[3] HEX` the primary page data view:

- `[2] PAGES` selects which database page is active.
- `[3] HEX` shows the selected page bytes as a structured hex map.
- `[4] META` explains the current page, block, or sub-block selection.

There is no separate layout view, cells view, or raw view for now. The hex view is the
page data view.

## Core Concept

`[3] HEX` shows raw bytes, but not as a plain dump. Every byte belongs to a known block
when Badger can parse it:

- database header, on page 1
- b-tree page header
- cell pointer array
- freeblocks
- unallocated regions
- cells
- drilled child ranges inside a selected block

The user navigates between parsed byte blocks, not visual hex rows and not individual
bytes. The hex viewport scrolls to keep the selected block visible.

`[4] META` never shows raw bytes. Raw bytes are already visible in `[3] HEX`. Meta shows
offset, size, type, and parsed values for the current selection.

## Pane Responsibilities

### `[2] PAGES`

`[2] PAGES` owns page selection.

When focus is in `[2]` and the user moves through pages, `[4] META` shows page-level
metadata for the selected page.

### `[3] HEX`

`[3] HEX` owns byte block selection.

When focus moves to `[3]`, Badger automatically selects the first top-level block in the
page. Usually that is the page header. On page 1, it may be the database header before
the b-tree page header.

The hex view should not show:

- page summary header
- ASCII column
- legend text
- selected-block footer

Those details either exist elsewhere or belong in `[4] META`.

### `[4] META`

`[4] META` owns parsed context.

Meta content depends on where the user is:

- focus in `[2] PAGES`: page metadata
- focus in `[3] HEX`: selected byte block or sub-block metadata
- focus in `[4] META`: same metadata, but arrows scroll the meta content

Meta is always derived from the active selection. It does not have its own independent
selection cursor.

## Hex View Shape

The hex pane is a 16-byte row view with a compact offset column. There is no ASCII column.

The hex view must not use letters inside the byte stream to identify block ownership.
Block ownership is represented by color/background styling. For example, page header,
pointer array, unallocated space, freeblocks, cells, and drilled sub-blocks each get
their own style.

Text examples in this document cannot render terminal colors. They therefore show plain
hex bytes and use `>` only as an external selection marker. The actual UI should rely on
color schemes and selected styles, not inline block letters.

Example with a top-level cell selected:

```text
┌─[3] HEX──────────────────────────────────────────────────────────────────┐
│ Offset   00 01 02 03 04 05 06 07  08 09 0A 0B 0C 0D 0E 0F               │
│ 0000   0D 00 00 00 21 00 B3 00  02 8A 02 24 01 9C 03 17                 │
│ 0010   03 88 04 08 04 81 04 F0  05 71 05 F2 06 60 06 E2                 │
│ 0020   07 49 07 CE 08 46 08 CA  09 45 09 CC 0A 44 0A BE                 │
│ 0030   0B 38 0B BA 0C 32 0C AE  0D 2D 0D A4 0E 22 0E A0                 │
│ 0040   0F 19 0F 91 00 B3 00 00  00 00 00 00 00 00 00 00                 │
│ 0050   00 00 00 00 00 00 00 00  00 00 00 00 00 00 00 00                 │
│ 0060   00 00 00 00 00 00 00 00  00 00 00 00 00 00 00 00                 │
│ 0070   00 00 00 00 00 00 00     8B 02 0D 63 6F 6D 70 61                 │
│                                                                          │
│ 0280  >8B 02 86 05 0D 63 6F 6D >70 61 6E 69 65 73 2E 2E                 │
│ 0290  >2E 00 00 00 00 00 00 00 >00 00 00 00 00 00 00 00                 │
│ 02A0  >4D 65 78 69 63 6F 00 00 >54 65 63 68 6E 6F 6C 6F                 │
│ 02B0  >67 79 00 00 00 00 00 00 >00 00 00 00 00 00 00 00                 │
└──────────────────────────────────────────────────────────────────────────┘
```

The selected block should be highlighted across every hex row it touches. If a selected
block spans multiple rows, all visible byte segments for that block should use the selected
style.

## Whole Terminal Example

When the user is focused on `[2] PAGES`, the selected page is highlighted in `[2]`, `[3]`
shows that page's hex map, and `[4]` shows page-level meta.

```text
┌─[1] B-TREES──────────────┐ ┌─[3] HEX──────────────────────────────────────────────────────────────────┐ ┌─[4] META──────────────────────┐
│  sqlite_schema           │ │ Offset   00 01 02 03 04 05 06 07  08 09 0A 0B 0C 0D 0E 0F               │ │ Page 8                        │
│> ▦ companies             │ │ 0000   0D 00 00 00 21 00 B3 00  02 8A 02 24 01 9C 03 17                 │ │ Type: leaf table              │
│  ▦ sqlite_sequence       │ │ 0010   03 88 04 08 04 81 04 F0  05 71 05 F2 06 60 06 E2                 │ │ Page size: 4096 bytes         │
│  ◈ idx_companies_country │ │ 0020   07 49 07 CE 08 46 08 CA  09 45 09 CC 0A 44 0A BE                 │ │ File offset: 28672            │
│                          │ │ 0030   0B 38 0B BA 0C 32 0C AE  0D 2D 0D A4 0E 22 0E A0                 │ │                               │
│                          │ │ 0040   0F 19 0F 91 00 B3 00 00  00 00 00 00 00 00 00 00                 │ │ STRUCTURE                     │
│                          │ │ 0050   00 00 00 00 00 00 00 00  00 00 00 00 00 00 00 00                 │ │ Cells: 33                     │
└──────────────────────────┘ │ 0060   00 00 00 00 00 00 00 00  00 00 00 00 00 00 00 00                 │ │ Cell content start: 179       │
┌─[2] PAGES────────────────┐ │ 0070   00 00 00 00 00 00 00     8B 02 0D 63 6F 6D 70 61                 │ │ First freeblock: 0            │
│  page 2                  │ │                                                                          │ │ Fragmented free bytes: 0      │
│  page 5                  │ │ 0280   8B 02 86 05 0D 63 6F 6D  70 61 6E 69 65 73 2E 2E                 │ │ Pointer array: 66 bytes       │
│  page 6                  │ │ 0290   2E 00 00 00 00 00 00 00  00 00 00 00 00 00 00 00                 │ │ Unallocated: 105 bytes        │
│  page 7                  │ │                                                                          │ │                               │
│> page 8                  │ │                                                                          │ │ BTREE                         │
│  page 9                  │ │                                                                          │ │ Object: companies             │
│  page 10                 │ │                                                                          │ │ Root page: 2                  │
└──────────────────────────┘ └──────────────────────────────────────────────────────────────────────────┘ └───────────────────────────────┘

filtered: ▦ companies (1664 pg) | ↑↓/kj move · d drill · i info · q quit
```

When the user focuses `[3] HEX`, the first block is selected automatically and `[4]`
switches to block-level meta.

```text
┌─[3] HEX──────────────────────────────────────────────────────────────────┐ ┌─[4] META──────────────────────┐
│ Offset   00 01 02 03 04 05 06 07  08 09 0A 0B 0C 0D 0E 0F               │ │ Page Header                   │
│ 0000  >0D 00 00 00 21 00 B3 00  02 8A 02 24 01 9C 03 17                 │ │ Offset: 0..7                  │
│ 0010   03 88 04 08 04 81 04 F0  05 71 05 F2 06 60 06 E2                 │ │ Size: 8 bytes                 │
│ 0020   07 49 07 CE 08 46 08 CA  09 45 09 CC 0A 44 0A BE                 │ │                               │
│ 0030   0B 38 0B BA 0C 32 0C AE  0D 2D 0D A4 0E 22 0E A0                 │ │ FIELDS                        │
│ 0040   0F 19 0F 91 00 B3 00 00  00 00 00 00 00 00 00 00                 │ │ Page kind: leaf table         │
│ 0050   00 00 00 00 00 00 00 00  00 00 00 00 00 00 00 00                 │ │ First freeblock: 0            │
│ 0060   00 00 00 00 00 00 00 00  00 00 00 00 00 00 00 00                 │ │ Cell count: 33                │
└──────────────────────────────────────────────────────────────────────────┘ │ Cell content start: 179       │
                                                                             │ Fragmented free bytes: 0      │
                                                                             └───────────────────────────────┘
```

## Navigation

Badger should keep navigation consistent with the rest of the TUI.

### Focus In `[2] PAGES`

```text
up/down/k/j    select previous/next page
3              focus HEX and select the first block for the active page
4              focus META for the selected page
```

Behavior:

- Moving page selection auto-loads and renders the selected page.
- `[4] META` shows page-level metadata while focus remains in `[2]`.
- Moving to another page resets `[3] HEX` block selection and drill state.

### Focus In `[3] HEX`

```text
up/down/k/j    select previous/next block at the current granularity
d              drill into selected block, or return to the parent granularity
i              show hex legend/info in META
4              focus META for the selected block
```

Behavior:

- Moving block selection updates `[4] META` immediately.
- Moving block selection resets meta scroll to the top.
- Selection is block-based, not row-based.
- The user never navigates by visual hex row or by individual byte.
- The selected block is highlighted across all visible byte segments it owns.
- If the selected block is outside the current viewport, the viewport scrolls to reveal it.

### Focus In `[4] META`

```text
up/down/k/j    scroll meta content
3              return focus to HEX without changing hex selection
```

Behavior:

- Meta scrolling does not change page, block, or drill selection.
- Meta does not have its own selection cursor.

## Block Selection

Top-level block selection is ordered by physical page offset.

Typical order:

1. Database Header, only on page 1
2. Page Header
3. Pointer Array
4. Freeblocks, unallocated regions, and cells sorted by `StartOffset`

This means the order follows the physical page layout, not logical row order.

When the user presses `down` from `Pointer Array`, the next selection is whichever parsed
block starts after the pointer array. That might be an unallocated region, a freeblock, or
a cell. When the user presses `up`, selection moves to the previous physical block.

The selected block may span many rendered hex rows. Every part of the selected block gets
the selected style. The row itself is not the selection target; the selected parsed byte
range is.

## Drill

Drill changes selection granularity. It does not replace the hex view and does not open a
new view.

At top level, the hex view highlights large page blocks:

```text
Page Header
Pointer Array
Unallocated
Cell 32
Cell 31
Cell 30
...
```

If the user selects `Cell 28` and presses `d`, Badger keeps the same full-page hex view,
but the selected cell is split into child ranges. Navigation now moves between child
ranges inside the cell. The user still does not navigate by visual rows or individual
bytes; `up/down/k/j` select the previous or next sub-block.

Before drill:

```text
┌─[3] HEX──────────────────────────────────────────────────────────────────┐
│ Offset   00 01 02 03 04 05 06 07  08 09 0A 0B 0C 0D 0E 0F               │
│ 0280  >8B 02 86 05 0D 63 6F 6D >70 61 6E 69 65 73 2E 2E                 │
│ 0290  >2E 00 00 00 00 00 00 00 >00 00 00 00 00 00 00 00                 │
│ 02A0  >4D 65 78 69 63 6F 00 00 >54 65 63 68 6E 6F 6C 6F                 │
│ 02B0  >67 79 00 00 00 00 00 00 >00 00 00 00 00 00 00 00                 │
└──────────────────────────────────────────────────────────────────────────┘
```

After drill:

```text
┌─[3] HEX──────────────────────────────────────────────────────────────────┐
│ Offset   00 01 02 03 04 05 06 07  08 09 0A 0B 0C 0D 0E 0F               │
│ 0280   8B 02  86 05           0D  63 6F 6D 70 61                        │
│ 0290  >6E 69 65 73 2E 2E 2E 00 >00 00 00 00 00 00 00 00                 │
│ 02A0  >4D 65 78 69 63 6F 00 00  54 65 63 68 6E 6F 6C 6F                 │
│ 02B0   67 79 00 00 00 00 00 00  00 00 00 00 00 00 00 00                 │
└──────────────────────────────────────────────────────────────────────────┘
```

In the real UI, each child range is distinguished by style. The text example cannot show
those styles, but the intended child ranges are:

- payload size
- rowid
- record header size
- serial type area
- record value area

We should refine the actual child block names and labels per block type before
implementation.

Pressing `d` again returns to the parent granularity. The selection should return to the
parent block that was drilled, not to the first block on the page.

### Drill Rules

- Drill is available only when the selected block has child ranges.
- The footer should expose the `d` hint when drill is available.
- The meta pane should not show command hints.
- Drilling resets meta scroll to the top.
- Moving to another page exits drill mode.
- Moving back to `[2] PAGES` may keep or clear drill state; the simpler first version
  should clear it when page selection changes.

## Info / Legend

The hex pane itself should not render a legend. It should remain a data surface.

Pressing `i` while focus is in `[3] HEX` should show legend/help content in `[4] META`.
When the user moves selection again, `[4] META` returns to the selected block.

Example info content:

```text
HEX INFO

Styles:
header             page/database headers
pointer-array      cell pointer array
unallocated        unused bytes
freeblock          reusable freeblock bytes
cell               b-tree cell bytes
selected           current block or sub-block

Navigation:
up/down/k/j        select block
d                  drill into block
i                  show/hide info
```

This keeps explanatory copy available without spending permanent screen space.

## Meta Views To Discuss

We need to define the exact meta view for each selection type before implementation.

Known meta views:

1. Page meta, shown when focus is in `[2] PAGES`.
2. Database header block meta, page 1 only.
3. Page header block meta.
4. Pointer array block meta.
5. Pointer entry sub-block meta, if pointer array drill is supported.
6. Freeblock block meta.
7. Unallocated block meta.
8. Table leaf cell block meta.
9. Table interior cell block meta.
10. Index leaf cell block meta.
11. Index interior cell block meta.
12. Payload-size sub-block meta.
13. RowID sub-block meta.
14. Left-child-page sub-block meta.
15. Record payload sub-block meta.
16. Record header size sub-block meta.
17. Serial type sub-block meta.
18. Record value sub-block meta.
19. Overflow pointer sub-block meta.

Core meta principles already agreed:

- Meta shows parsed information only.
- Meta includes type, offset/range, and size.
- Meta does not show raw bytes.
- Meta follows the active selection.
- Meta has scroll, but no independent selection.
- Page meta is different from block meta.
- Block and sub-block meta should be designed separately for each SQLite structure.

## Proposed Meta Renders

These renders are drafts for discussion. They intentionally do not include raw hex.

### 1. Page Meta

Shown when focus is in `[2] PAGES`.

```text
┌─[4] META──────────────────────┐
│ Page 8                        │
│ Type: leaf table              │
│ Page size: 4096 bytes         │
│ File offset: 28672            │
│                               │
│ STRUCTURE                     │
│ Cells: 33                     │
│ Cell content start: 179       │
│ First freeblock: 0            │
│ Fragmented free bytes: 0      │
│ Pointer array: 66 bytes       │
│ Freeblocks: 0                 │
│ Unallocated: 105 bytes        │
│                               │
│ BTREE                         │
│ Object: companies             │
│ Root page: 2                  │
└───────────────────────────────┘
```

### 2. Database Header Block Meta

Shown for the 100-byte SQLite database header on page 1.

```text
┌─[4] META──────────────────────┐
│ Database Header               │
│ Offset: 0..99                 │
│ Size: 100 bytes               │
│                               │
│ FIELDS                        │
│ Page size: 4096               │
│ Page count: 1664              │
│ Read version: 1               │
│ Write version: 1              │
│ Reserved bytes/page: 0        │
│ Freelist pages: 0             │
│ Schema cookie: 12             │
│ Schema format: 4              │
│ Encoding: UTF-8               │
│ SQLite version: 3.43.2        │
└───────────────────────────────┘
```

### 3. Page Header Block Meta

```text
┌─[4] META──────────────────────┐
│ Page Header                   │
│ Offset: 0..7                  │
│ Size: 8 bytes                 │
│                               │
│ FIELDS                        │
│ Page kind: leaf table         │
│ First freeblock: 0            │
│ Cell count: 33                │
│ Cell content start: 179       │
│ Fragmented free bytes: 0      │
└───────────────────────────────┘
```

For interior pages, include the right-most pointer:

```text
┌─[4] META──────────────────────┐
│ Page Header                   │
│ Offset: 0..11                 │
│ Size: 12 bytes                │
│                               │
│ FIELDS                        │
│ Page kind: interior table     │
│ First freeblock: 0            │
│ Cell count: 5                 │
│ Cell content start: 4040      │
│ Fragmented free bytes: 0      │
│ Right-most pointer: 91        │
└───────────────────────────────┘
```

### 4. Pointer Array Block Meta

```text
┌─[4] META──────────────────────┐
│ Pointer Array                 │
│ Offset: 8..73                 │
│ Size: 66 bytes                │
│                               │
│ FIELDS                        │
│ Entries: 33                   │
│ Entry size: 2 bytes           │
│ Points to: cell offsets       │
│                               │
│ POINTERS                      │
│ 00 -> offset 650              │
│ 01 -> offset 548              │
│ 02 -> offset 412              │
│ 03 -> offset 529              │
│ 04 -> offset 642              │
└───────────────────────────────┘
```

### 5. Pointer Entry Sub-Block Meta

Shown if pointer array drill splits the array into individual 2-byte entries.

```text
┌─[4] META──────────────────────┐
│ Pointer Entry 04              │
│ Parent: Pointer Array         │
│ Offset: 16..17                │
│ Size: 2 bytes                 │
│                               │
│ PARSED                        │
│ Cell offset: 642              │
│ Target block: Cell 28         │
│ Cell kind: table leaf         │
└───────────────────────────────┘
```

### 6. Freeblock Block Meta

```text
┌─[4] META──────────────────────┐
│ Freeblock                     │
│ Offset: 120..151              │
│ Size: 32 bytes                │
│                               │
│ FIELDS                        │
│ Next freeblock: 0             │
│ Block size: 32                │
│ Reusable: yes                 │
└───────────────────────────────┘
```

### 7. Unallocated Block Meta

```text
┌─[4] META──────────────────────┐
│ Unallocated                   │
│ Offset: 74..178               │
│ Size: 105 bytes               │
│                               │
│ FIELDS                        │
│ Parsed structure: none        │
│ Role: gap before cell area    │
└───────────────────────────────┘
```

### 8. Table Leaf Cell Block Meta

```text
┌─[4] META──────────────────────┐
│ Cell 28                       │
│ Type: table leaf cell         │
│ Offset: 642..790              │
│ Size: 149 bytes               │
│                               │
│ FIELDS                        │
│ RowID: 646                    │
│ Payload size: 139             │
│ Record payload: 647..790      │
│ Local payload: 139 bytes      │
│ Overflow: no                  │
└───────────────────────────────┘
```

### 9. Table Interior Cell Block Meta

```text
┌─[4] META──────────────────────┐
│ Cell 2                        │
│ Type: table interior cell     │
│ Offset: 4079..4086            │
│ Size: 8 bytes                 │
│                               │
│ FIELDS                        │
│ Left child page: 45           │
│ RowID separator: 1024         │
└───────────────────────────────┘
```

### 10. Index Leaf Cell Block Meta

```text
┌─[4] META──────────────────────┐
│ Cell 14                       │
│ Type: index leaf cell         │
│ Offset: 3010..3067            │
│ Size: 58 bytes                │
│                               │
│ FIELDS                        │
│ Payload size: 57              │
│ Record payload: 3011..3067    │
│ Local payload: 57 bytes       │
│ Overflow: no                  │
└───────────────────────────────┘
```

### 11. Index Interior Cell Block Meta

```text
┌─[4] META──────────────────────┐
│ Cell 3                        │
│ Type: index interior cell     │
│ Offset: 4020..4075            │
│ Size: 56 bytes                │
│                               │
│ FIELDS                        │
│ Left child page: 88           │
│ Payload size: 51              │
│ Record payload: 4025..4075    │
└───────────────────────────────┘
```

### 12. Payload-Size Sub-Block Meta

```text
┌─[4] META──────────────────────┐
│ Payload Size                  │
│ Parent: Cell 28               │
│ Offset: 642..643              │
│ Size: 2 bytes                 │
│                               │
│ PARSED                        │
│ Varint value: 139             │
│ Meaning: record payload bytes │
└───────────────────────────────┘
```

### 13. RowID Sub-Block Meta

```text
┌─[4] META──────────────────────┐
│ RowID                         │
│ Parent: Cell 28               │
│ Offset: 644..646              │
│ Size: 3 bytes                 │
│                               │
│ PARSED                        │
│ Varint value: 646             │
│ Meaning: table row key        │
└───────────────────────────────┘
```

### 14. Left-Child-Page Sub-Block Meta

```text
┌─[4] META──────────────────────┐
│ Left Child Page               │
│ Parent: Cell 2                │
│ Offset: 4079..4082            │
│ Size: 4 bytes                 │
│                               │
│ PARSED                        │
│ Page number: 45               │
│ Meaning: child subtree        │
└───────────────────────────────┘
```

### 15. Record Payload Sub-Block Meta

```text
┌─[4] META──────────────────────┐
│ Record Payload                │
│ Parent: Cell 28               │
│ Offset: 647..790              │
│ Size: 144 bytes               │
│                               │
│ PARSED                        │
│ Header size: 13               │
│ Serial types: 10              │
│ Values: 10                    │
│ Overflow: no                  │
└───────────────────────────────┘
```

### 16. Record Header Size Sub-Block Meta

```text
┌─[4] META──────────────────────┐
│ Record Header Size            │
│ Parent: Record Payload        │
│ Offset: 647..647              │
│ Size: 1 byte                  │
│                               │
│ PARSED                        │
│ Header size: 13               │
│ Header range: 647..659        │
│ Value area starts: 660        │
└───────────────────────────────┘
```

### 17. Serial Type Sub-Block Meta

```text
┌─[4] META──────────────────────┐
│ Serial Type 1                 │
│ Parent: Record Payload        │
│ Offset: 649..649              │
│ Size: 1 byte                  │
│                               │
│ PARSED                        │
│ Serial type: 37               │
│ Storage class: text           │
│ Value size: 12 bytes          │
│ Value block: Value 1          │
└───────────────────────────────┘
```

### 18. Record Value Sub-Block Meta

```text
┌─[4] META──────────────────────┐
│ Value 1                       │
│ Parent: Record Payload        │
│ Offset: 660..691              │
│ Size: 32 bytes                │
│                               │
│ PARSED                        │
│ Storage class: text           │
│ Serial type: 77               │
│ Value: "S. C. grupo informa"  │
└───────────────────────────────┘
```

### 19. Overflow Pointer Sub-Block Meta

```text
┌─[4] META──────────────────────┐
│ Overflow Pointer              │
│ Parent: Cell 28               │
│ Offset: 788..791              │
│ Size: 4 bytes                 │
│                               │
│ PARSED                        │
│ First overflow page: 1204     │
│ Meaning: payload continuation │
└───────────────────────────────┘
```

## Open Design Questions

- Should `d` on a drilled child always return to the parent, or should some child ranges
  have a deeper drill level?
- Should pointer array drill split into individual 2-byte pointer entries?
- Should cell drill initially split record values individually, or group all values under
  one record-values range for the first version?
- How should selected styling work when a block begins or ends in the middle of a 16-byte
  row?
- How much page-level context should remain visible in `[4] META` after the user focuses
  `[3] HEX`?
