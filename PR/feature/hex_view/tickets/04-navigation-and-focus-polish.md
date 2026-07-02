# Page Hex View Ticket 04 - Navigation and focus polish

> Feature: **Hex View** (`feature/hex_view`)
> Parent design: [design.md](../design.md)
> Implementation map: [implementation-map.md](../implementation-map.md)
> Depends on: [03-drill-and-drill-meta.md](03-drill-and-drill-meta.md)
> Current code hotspots: `internal/tui/model.go`, `internal/tui/keys_test.go`

## Summary

Make the three-pane page workflow match the proposed design exactly:

- `[2] PAGES` owns page selection.
- `[3] HEX` owns block or drill-child selection.
- `[4] META` owns only meta scrolling.

This ticket is about interaction consistency after the visual and meta pieces exist.

## What will be visible after this ticket

When focus is in `[2] PAGES`, page meta is active:

```text
┌─[1] B-TREES──────────────┐ ┌─[3] HEX──────────────────────────────────────────────────────────────────┐ ┌─[4] META──────────────────────┐
│  sqlite_schema           │ │ Offset   00 01 02 03 04 05 06 07  08 09 0A 0B 0C 0D 0E 0F               │ │ Page 8                        │
│> ▦ companies             │ │ 0000   0D 00 00 00 21 00 B3 00  02 8A 02 24 01 9C 03 17                 │ │ Type: leaf table              │
│  ▦ sqlite_sequence       │ │ 0010   03 88 04 08 04 81 04 F0  05 71 05 F2 06 60 06 E2                 │ │ Page size: 4096 bytes         │
│  ◈ idx_companies_country │ │ 0020   07 49 07 CE 08 46 08 CA  09 45 09 CC 0A 44 0A BE                 │ │ File offset: 28672            │
└──────────────────────────┘ │ 0030   0B 38 0B BA 0C 32 0C AE  0D 2D 0D A4 0E 22 0E A0                 │ │ STRUCTURE                     │
┌─[2] PAGES────────────────┐ │ 0040   0F 19 0F 91 00 B3 00 00  00 00 00 00 00 00 00 00                 │ │ Cells: 33                     │
│  page 2                  │ └──────────────────────────────────────────────────────────────────────────┘ │ Pointer array: 66 bytes       │
│  page 5                  │                                                                              └───────────────────────────────┘
│> page 8                  │
└──────────────────────────┘
```

When focus is in `[3] HEX`, block meta is active:

```text
┌─[3] HEX──────────────────────────────────────────────────────────────────┐ ┌─[4] META──────────────────────┐
│ Offset   00 01 02 03 04 05 06 07  08 09 0A 0B 0C 0D 0E 0F               │ │ Page Header                   │
│ 0000  >0D 00 00 00 21 00 B3 00  02 8A 02 24 01 9C 03 17                 │ │ Offset: 0..7                  │
│ 0010   03 88 04 08 04 81 04 F0  05 71 05 F2 06 60 06 E2                 │ │ Size: 8 bytes                 │
│ 0020   07 49 07 CE 08 46 08 CA  09 45 09 CC 0A 44 0A BE                 │ │                               │
│ 0030   0B 38 0B BA 0C 32 0C AE  0D 2D 0D A4 0E 22 0E A0                 │ │ FIELDS                        │
└──────────────────────────────────────────────────────────────────────────┘ │ Page kind: leaf table         │
                                                                             └───────────────────────────────┘
```

## Scope

In scope:

- Make numbered focus jumps source-aware:
  - `2` focuses PAGES and uses page meta.
  - `3` focuses HEX and uses block/drill meta.
  - `4` focuses META for the current page/block/drill source.
- Ensure arrows in each pane affect only that pane's responsibility:
  - PAGES: page selection and page load.
  - HEX: block/drill selection.
  - META: meta scroll.
- Ensure `3` from META returns to HEX without changing selection.
- Ensure page changes reset:
  - top-level block selection;
  - drill state;
  - hex scroll;
  - meta scroll.
- Ensure moving block/drill selection resets meta scroll.

Out of scope:

- New visual styles.
- New meta fields.
- Pointer-array drill.
- `i` info/legend view.

## Key behavior from the design

Focus in `[2] PAGES`:

```text
up/down/k/j    select previous/next page
3              focus HEX and select the first block for the active page
4              focus META for the selected page
```

Focus in `[3] HEX`:

```text
up/down/k/j    select previous/next block at the current granularity
d              drill into selected block, or return to the parent granularity
4              focus META for the selected block
```

Focus in `[4] META`:

```text
up/down/k/j    scroll meta content
3              return focus to HEX without changing hex selection
```

## Definition of done

- [ ] `2` focuses PAGES and page movement updates page meta.
- [ ] `3` focuses HEX and selects the first block if none is selected.
- [ ] `4` focuses META without changing the current page/block/drill selection.
- [ ] Up/down/k/j in PAGES select pages only within the PAGES section.
- [ ] Up/down/k/j in HEX move block or child selection only.
- [ ] Up/down/k/j in META scroll meta only.
- [ ] `3` from META returns to HEX without changing selection.
- [ ] Page changes reset HEX selection, drill state, HEX scroll, and META scroll.
- [ ] Moving HEX selection resets META scroll.
- [ ] Tests cover all focus transitions and local controls.

## Manual test

Run:

```bash
make build
./bin/badger fixtures/companies.db
```

Verify:

- Press `2`, move pages, and confirm page meta updates.
- Press `3`, move blocks, and confirm block meta updates.
- Press `4`, scroll meta, and confirm page/block selection does not change.
- Press `3` from META and confirm the same HEX selection remains active.
- Drill into a cell, go to META, return to HEX, and confirm drill selection remains active.
- Change pages and confirm drill/selection state resets.

