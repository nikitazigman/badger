# Page Hex View Ticket 05 - Visual refinement and coverage gaps

> Feature: **Hex View** (`feature/hex_view`)
> Parent design: [design.md](../design.md)
> Implementation map: [implementation-map.md](../implementation-map.md)
> Depends on: [04-navigation-and-focus-polish.md](04-navigation-and-focus-polish.md)
> Current code hotspots: `internal/tui/model.go`, `internal/tui/page_view.go`

## Summary

Refine the complete HEX/META experience after the major behavior exists.

This ticket should make the UI readable, stable, and close to the design intent across
normal terminal sizes. It is also the checkpoint for deciding whether any deferred gaps
should be pulled into the first complete pass.

## What will be visible after this ticket

The selected block should be visually clear even when it starts or ends in the middle of a
row:

```text
┌─[3] HEX──────────────────────────────────────────────────────────────────┐
│ Offset   00 01 02 03 04 05 06 07  08 09 0A 0B 0C 0D 0E 0F               │
│ 0280  >8B 02 86 05 0D 63 6F 6D >70 61 6E 69 65 73 2E 2E                 │
│ 0290  >2E 00 00 00 00 00 00 00 >00 00 00 00 00 00 00 00                 │
│ 02A0  >4D 65 78 69 63 6F 00 00 >54 65 63 68 6E 6F 6C 6F                 │
│ 02B0  >67 79 00 00 00 00 00 00 >00 00 00 00 00 00 00 00                 │
└──────────────────────────────────────────────────────────────────────────┘
```

In the real TUI, block ownership should be represented by styling, not by inline letters
or permanent legend text.

## Scope

In scope:

- Refine styles for:
  - database/page headers;
  - pointer array;
  - unallocated bytes;
  - freeblocks;
  - cells;
  - drilled child ranges;
  - selected range.
- Make selected ranges readable when a block begins or ends mid-row.
- Ensure long meta values wrap or truncate cleanly.
- Ensure the HEX pane has no permanent legend/help copy.
- Ensure META has no raw bytes.
- Inspect page 1, leaf table pages, interior pages if available, and index pages.
- Decide whether pointer-array entry drill is added now or remains deferred.

Out of scope:

- `i` info/legend view unless a separate decision pulls it in.
- Large parser refactors.
- New pane layout beyond fixes needed for readability.

## Design style requirements

From the design:

- The hex pane is a 16-byte row view with compact offset column.
- There is no ASCII column.
- The hex view must not use letters inside the byte stream to identify block ownership.
- Block ownership is represented by color/background styling.
- Selected block styling applies to every visible byte segment it owns.
- The meta pane should not show command hints.
- The hex pane itself should not render a legend.

## Review checklist

- Page 1:
  - database header and page header are visually distinct;
  - page 1 database header meta remains readable.
- Leaf table page:
  - page header, pointer array, unallocated region, and cells are distinguishable.
- Index page:
  - index cells have sensible style and meta.
- Drill mode:
  - child ranges are distinguishable from parent/top-level cells.
- Narrow-ish terminal:
  - text does not overlap;
  - pane titles remain visible;
  - long meta values do not break the frame.

## Definition of done

- [ ] Block ownership styles are readable and distinguishable enough for review.
- [ ] Selected range styling is clear across one-row and multi-row blocks.
- [ ] Mid-row block starts/ends look coherent.
- [ ] META values wrap or truncate without corrupting the layout.
- [ ] HEX contains no ASCII column, permanent legend, page summary, or selected-block footer.
- [ ] META contains no raw hex or ASCII.
- [ ] A decision is recorded in this ticket or the implementation map for pointer-array drill.
- [ ] Manual review has been done with `fixtures/companies.db` and at least one other fixture.

## Manual test

Run:

```bash
make build
./bin/badger fixtures/companies.db
./bin/badger fixtures/sample.db
./bin/badger fixtures/superheroes.db
```

Verify:

- Open page 1 and confirm database-header treatment.
- Open a table leaf page and move through all top-level block kinds.
- Open an index page if present and inspect index cells.
- Drill into table and index cells.
- Resize the terminal within normal bounds and confirm the layout remains usable.

