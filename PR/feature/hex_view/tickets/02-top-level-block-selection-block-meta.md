# Page Hex View Ticket 02 - Top-level block selection + block META

> Feature: **Hex View** (`feature/hex_view`)
> Parent design: [design.md](../design.md)
> Implementation map: [implementation-map.md](../implementation-map.md)
> Depends on: [01-static-hex-pane-page-meta.md](01-static-hex-pane-page-meta.md)
> Current code hotspots: `internal/tui/model.go`, `internal/tui/page_view.go`, `internal/tui/keys_test.go`

## Summary

Make `[3] HEX` block-aware at the top level.

The user should navigate parsed byte blocks, not visual rows and not individual bytes.
When the user focuses `[3] HEX`, Badger selects the first parsed top-level block on the
active page and `[4] META` switches from page meta to block meta.

## What will be visible after this ticket

Top-level selected block example:

```text
┌─[3] HEX──────────────────────────────────────────────────────────────────┐
│ Offset   00 01 02 03 04 05 06 07  08 09 0A 0B 0C 0D 0E 0F               │
│ 0000  >0D 00 00 00 21 00 B3 00  02 8A 02 24 01 9C 03 17                 │
│ 0010   03 88 04 08 04 81 04 F0  05 71 05 F2 06 60 06 E2                 │
│ 0020   07 49 07 CE 08 46 08 CA  09 45 09 CC 0A 44 0A BE                 │
└──────────────────────────────────────────────────────────────────────────┘
```

Block meta example:

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

## Scope

In scope:

- Build top-level parsed blocks for the active page:
  - database header, page 1 only;
  - b-tree page header;
  - cell pointer array;
  - freeblocks;
  - unallocated regions;
  - table leaf cells;
  - table interior cells;
  - index leaf cells;
  - index interior cells.
- Sort blocks by physical page offset.
- Style bytes by block ownership.
- Highlight the selected block across every visible byte segment it owns.
- `3` focuses HEX and selects the first block if none is selected.
- Up/down/k/j in HEX moves to previous/next top-level block.
- Moving block selection resets META scroll.
- Scroll HEX to reveal the selected block.
- Add top-level block meta views for every block type above.
- `4` focuses META for the selected block.

Out of scope:

- Drill mode.
- Drill-level meta.
- Pointer-array entry drill.
- `i` info/legend view.

## Implementation notes

- Existing `buildPageRows` already builds most top-level ranges in physical order. It can be reused or replaced by a more block-specific type.
- Add explicit page UI state for selected block and hex scroll.
- Selection should be range-based:
  - row selection is not the interaction target;
  - individual byte selection is not supported.
- Selected styling overlays block ownership styling.
- Unknown bytes should render with default or muted styling.
- Page movement must reset selected block, drill state, hex scroll, and META scroll.
- `[4] META` should derive from the active page/block state, not from its own cursor.

## Required top-level meta renders from the design

Database header:

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

Interior page header:

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

Pointer array:

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

Freeblock:

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

Unallocated:

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

Table leaf cell:

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

Table interior cell:

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

Index leaf cell:

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

Index interior cell:

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

## Definition of done

- [ ] Pressing `3` focuses `[3] HEX`.
- [ ] `[3] HEX` auto-selects the first top-level block on the active page.
- [ ] Up/down/k/j in HEX move between parsed top-level blocks.
- [ ] Selection follows physical page order.
- [ ] Selected block bytes are highlighted across every visible segment.
- [ ] HEX scroll reveals selected blocks outside the viewport.
- [ ] `[4] META` switches to block-level meta when HEX is active.
- [ ] All top-level block types have parsed meta views.
- [ ] Pressing `4` focuses META without changing the selected block.
- [ ] META scrolling does not change selected page or selected block.
- [ ] Page movement resets block selection and hex scroll.
- [ ] Tests cover block ordering, HEX movement, selected block rendering, and block meta.

## Manual test

Run:

```bash
make build
./bin/badger fixtures/companies.db
```

Verify:

- Press `2`, select a page, then press `3`.
- The first parsed block is selected.
- Up/down moves through blocks, not hex rows.
- Select a cell far down the page and confirm HEX scrolls to reveal it.
- Confirm `[4] META` explains the selected block.
- Press `4`, scroll META, then press `3`; the same block remains selected.

