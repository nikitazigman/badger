# Page Hex View Ticket 01 - Static HEX pane + page META

> Feature: **Hex View** (`feature/hex_view`)
> Parent design: [design.md](../design.md)
> Implementation map: [implementation-map.md](../implementation-map.md)
> Current code hotspots: `internal/tui/model.go`, `internal/tui/page_view.go`, `internal/tui/keys_test.go`

## Summary

Create the first vertical slice of the new page view:

- `[2] PAGES` still selects and loads pages.
- `[3] DETAIL` becomes `[3] HEX`.
- `[3] HEX` renders the active page bytes as a compact 16-byte hex grid.
- `[4] META` shows parsed page-level metadata while focus/source is `[2] PAGES`.

This ticket intentionally does not implement block coloring, block selection, drill, or
block-level meta. The goal is to make the new page surface visible and reviewable without
changing the interaction model too much at once.

## What will be visible after this ticket

When a page is selected, the middle pane shows raw bytes in this shape:

```text
┌─[3] HEX──────────────────────────────────────────────────────────────────┐
│ Offset   00 01 02 03 04 05 06 07  08 09 0A 0B 0C 0D 0E 0F               │
│ 0000     0D 00 00 00 21 00 B3 00  02 8A 02 24 01 9C 03 17               │
│ 0010     03 88 04 08 04 81 04 F0  05 71 05 F2 06 60 06 E2               │
│ 0020     07 49 07 CE 08 46 08 CA  09 45 09 CC 0A 44 0A BE               │
└──────────────────────────────────────────────────────────────────────────┘
```

`[4] META` shows page-level metadata:

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

## Scope

In scope:

- Rename the middle page pane title from `[3] DETAIL` to `[3] HEX` when page content is active.
- Replace the old page `STRUCTURES` table with a 16-byte hex grid.
- Render only:
  - compact offset column;
  - byte columns `00` through `0F`;
  - raw page bytes.
- Remove from `[3] HEX`:
  - page summary header;
  - ASCII column;
  - legend text;
  - selected-block footer.
- Add page-level meta in `[4] META`.
- Preserve delayed page loading behavior.
- Preserve existing page selection through `[2] PAGES`.

Out of scope:

- Block coloring.
- Block selection in `[3] HEX`.
- Drill mode.
- Block-level or drill-level meta.
- `i` info/legend view.

## Implementation notes

- `viewPage` should become the static hex renderer for loaded pages.
- `detailHeaderText` should render `HEX` for page content.
- Use `currentPage.BTreePage.Raw` as the byte source.
- For page 1, the raw bytes include the SQLite database header and b-tree page bytes in one page-sized buffer.
- Add a helper like `renderHexRows(raw []byte, width int, height int) []string`.
- Keep the grid 16 bytes wide even when the pane is narrower; truncate rows with existing layout helpers if needed.
- Page meta should be produced by a new helper like `viewPageMeta(width int) string`.
- Page meta must not include raw hex or ASCII.
- If an object filter is active, use `activeFilter.object` for the `BTREE` section.
- If no filter is active, it is acceptable for the first pass to omit `Object` and `Root page` rather than infer ownership unreliably.

## Definition of done

- [ ] `[3]` page pane title says `[3] HEX`.
- [ ] Selecting a page in `[2] PAGES` renders page bytes in `[3] HEX`.
- [ ] Hex rows include an `Offset` header and byte columns `00` through `0F`.
- [ ] The old page `STRUCTURES` table is not rendered for page content.
- [ ] The page view does not render an ASCII column.
- [ ] `[4] META` shows page-level parsed metadata when focus/source is `[2] PAGES`.
- [ ] `[4] META` does not show raw hex or ASCII.
- [ ] Delayed loading still avoids flicker and preserves the previously loaded page before the loading indicator appears.
- [ ] Tests cover the new HEX title, hex header, absence of `STRUCTURES`, and page meta content.

## Manual test

Run:

```bash
make build
./bin/badger fixtures/companies.db
```

Verify:

- Press `2`.
- Move through pages with up/down.
- `[3] HEX` updates as page selection changes.
- `[4] META` updates as page selection changes.
- Page 1 still renders bytes correctly.
- No raw bytes or ASCII appear in `[4] META`.

