# Page Hex View Ticket 03 - Drill + drill META

> Feature: **Hex View** (`feature/hex_view`)
> Parent design: [design.md](../design.md)
> Implementation map: [implementation-map.md](../implementation-map.md)
> Depends on: [02-top-level-block-selection-block-meta.md](02-top-level-block-selection-block-meta.md)
> Current code hotspots: `internal/tui/model.go`, `internal/tui/page_view.go`, `internal/sqlite/cells.go`, `internal/sqlite/payload.go`

## Summary

Add one-level drill mode for selected blocks with child ranges.

The full-page hex view remains visible. Drill changes only selection granularity: the user
moves through child ranges inside the selected parent block, and `[4] META` explains the
selected child range.

## What will be visible after this ticket

Before drill:

```text
┌─[3] HEX──────────────────────────────────────────────────────────────────┐
│ 0280  >8B 02 86 05 0D 63 6F 6D >70 61 6E 69 65 73 2E 2E                 │
│ 0290  >2E 00 00 00 00 00 00 00 >00 00 00 00 00 00 00 00                 │
│ 02A0  >4D 65 78 69 63 6F 00 00 >54 65 63 68 6E 6F 6C 6F                 │
│ 02B0  >67 79 00 00 00 00 00 00 >00 00 00 00 00 00 00 00                 │
└──────────────────────────────────────────────────────────────────────────┘
```

After drill:

```text
┌─[3] HEX──────────────────────────────────────────────────────────────────┐
│ 0280   8B 02  86 05           0D  63 6F 6D 70 61                        │
│ 0290  >6E 69 65 73 2E 2E 2E 00 >00 00 00 00 00 00 00 00                 │
│ 02A0  >4D 65 78 69 63 6F 00 00  54 65 63 68 6E 6F 6C 6F                 │
│ 02B0   67 79 00 00 00 00 00 00  00 00 00 00 00 00 00 00                 │
└──────────────────────────────────────────────────────────────────────────┘
```

## Scope

In scope:

- Add one-level drill state.
- `d` on a drillable top-level block enters drill mode.
- `d` while drilled exits drill mode and reselects the parent block.
- Up/down/k/j while drilled move through child ranges.
- Page changes exit drill mode.
- Add drill child meta views.
- Implement cell drill first:
  - table leaf cell;
  - table interior cell;
  - index leaf cell;
  - index interior cell.

Out of scope:

- Multi-level drill beyond one parent/child toggle.
- Pointer-array entry drill, unless explicitly pulled forward.
- `i` info/legend view.

## Drill child ranges

Implement children where the parser exposes the data:

- payload size
- rowid
- left child page
- record payload
- record header size
- serial type
- record value
- overflow pointer

Every child block should include:

- title;
- parent title;
- `sqlite.Meta`;
- parsed fields;
- meaning text where useful.

## Required drill meta renders from the design

Payload size:

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

RowID:

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

Left child page:

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

Record payload:

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

Record header size:

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

Serial type:

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

Record value:

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

Overflow pointer:

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

## Definition of done

- [ ] Pressing `d` on a drillable cell enters drill mode.
- [ ] Pressing `d` while drilled exits drill mode and reselects the parent cell.
- [ ] Pressing `d` on a non-drillable block is a no-op.
- [ ] Up/down/k/j in drill mode move through child ranges.
- [ ] Drill selection is highlighted in the full-page HEX view.
- [ ] `[4] META` explains the selected child range.
- [ ] Drill meta includes title, parent, offset/range, size, parsed value, and meaning where available.
- [ ] Page changes exit drill mode.
- [ ] Tests cover entering drill, moving child selection, leaving drill, and page-change reset.

## Manual test

Run:

```bash
make build
./bin/badger fixtures/companies.db
```

Verify:

- Select a page with table cells.
- Press `3`.
- Move to a cell.
- Press `d`.
- Move through payload size, rowid, record payload, serial type, and value child ranges.
- Confirm `[4] META` changes for each child range.
- Press `d` again and confirm the parent cell is selected.
- Move to another page and confirm drill mode is cleared.

