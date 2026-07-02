# Page Hex View Ticket 04 - Remaining polish, docs, and release checks

> Feature: **Hex View** (`feature/hex_view`)
> Parent design: [design.md](../design.md)
> Implementation map: [implementation-map.md](../implementation-map.md)
> Depends on: [03-drill-and-drill-meta.md](03-drill-and-drill-meta.md)
> Supersedes: old tickets 04, 05, and 06
> Current code hotspots: `internal/tui/model.go`, `internal/tui/keys_test.go`, `README.md`, `PR/feature/hex_view/implementation-map.md`

## Summary

Most Hex View behavior is already implemented and covered by tests. This ticket tracks the
remaining work needed to close the feature cleanly: stale docs, a few focused coverage
gaps, explicit deferred-scope decisions, and manual visual/release smoke checks.

## Already implemented

- `[2] PAGES` owns page selection and page loading.
- `[3] HEX` renders the 16-byte page grid and owns block/drill selection.
- `[4] META` renders page, block, or drill-child metadata and owns only meta scrolling.
- Top-level page blocks are physically ordered and styled.
- Cell drill supports nested `Record Payload` drill:
  - `d` drills into the selected drillable block/child;
  - `b` backs out one layer or exits drill at the top layer.
- Footer hints are contextual:
  - `d drill` only appears when drill-in is available;
  - `b back` only appears while drilled;
  - `f filter` / `f clear/switch` only appear in `[1] B-TREES` when applicable.
- Automated tests cover the main HEX/META rendering, drill behavior, contextual footer
  hints, page-change reset, and block/drill selection rendering.

## Remaining scope

### 1. Update stale documentation

Update `README.md` so it describes the current Hex View behavior instead of the old page
structure table.

Current stale README claims include:

- `[3]` shows page structures as rows.
- `[4] META` includes raw bytes, ASCII previews, byte maps, and decoded fields.
- Table/index cell payloads include raw hex, ASCII preview, and byte maps.

Replace with current behavior:

- `[3] HEX` shows the 16-byte page grid for loaded pages.
- `[4] META` shows parsed page/block/drill metadata without raw hex or ASCII.
- Page blocks include database header, page header, pointer array, freeblocks,
  unallocated regions, and table/index cells.
- `d` drills into drillable byte ranges; `b` backs out.
- Footer hints are contextual.

Also update `PR/feature/hex_view/implementation-map.md` where it still describes:

- `d` as both drill-in and drill-out;
- pointer-array drill as an unresolved question without a final first-pass decision;
- the old page-row/raw-byte model as current implementation context.

### 2. Record deferred pointer-array drill decision

Pointer-array entry drill remains deferred for this feature pass.

Record this explicitly in the implementation map:

```text
Pointer-array entry drill is deferred. The first complete Hex View release drills cells
and record payload internals only.
```

Do not implement pointer-array entry drill in this ticket unless a separate decision
pulls it forward.

### 3. Remove or implement stale page META action copy

Page META still advertises:

```text
- [ previous page
- ] next page
```

but `[` and `]` do not appear to have key handlers.

Choose one:

- remove those action lines; or
- implement and test `[` / `]` page navigation.

Conservative choice: remove the action lines, because page movement is already owned by
`[2] PAGES`.

### 4. Add focused tests for remaining navigation polish

Add direct tests for these cases:

- Pressing `3` from `[4] META` returns to `[3] HEX` without changing the selected block.
- Pressing `3` from `[4] META` while drilled returns to `[3] HEX` without changing the
  selected drill layer or selected drill child.
- Pressing `4` from `[2] PAGES` shows page metadata even after prior HEX/drill activity
  has occurred.

These behaviors likely already work; the missing part is explicit coverage.

### 5. Manual visual review

Run the app and visually inspect:

```bash
make build
./bin/badger fixtures/companies.db
./bin/badger fixtures/sample.db
./bin/badger fixtures/superheroes.db
```

Review at normal and narrow-ish terminal sizes:

- Page 1:
  - database header and page header are visually distinct;
  - database-header META remains readable.
- Table leaf pages:
  - page header, pointer array, unallocated region, and cells are distinguishable;
  - selected ranges remain clear when they start/end mid-row.
- Index pages:
  - index cells have sensible style and META.
- Drill mode:
  - payload size, rowid, record payload, record header size, serial types, values, and
    overflow pointers are visually distinguishable;
  - selected drill range remains obvious.
- Layout:
  - pane titles remain visible;
  - long META values wrap/truncate without corrupting frames;
  - HEX has no ASCII column, permanent legend, page summary, or selected-block footer;
  - META has no raw hex or ASCII.

### 6. Release checks

Run:

```bash
go test ./... -count=1
make build
```

If `make build` needs Go cache access outside the workspace, rerun with the normal
approved build path and record that the binary builds successfully.

## Definition of done

- [ ] README describes current HEX/META behavior and no longer documents the old page
      structure table/raw-byte META as the primary page view.
- [ ] Implementation map reflects current `d` / `b` drill behavior.
- [ ] Implementation map explicitly records pointer-array entry drill as deferred.
- [ ] Stale `[` / `]` page action copy is removed or implemented with tests.
- [ ] Tests cover META→HEX selection preservation for both block and drill selections.
- [ ] Tests cover `[4]` from `[2] PAGES` showing page metadata after prior HEX/drill use.
- [ ] Manual visual review is done on `companies.db` and at least one additional fixture.
- [ ] `go test ./... -count=1` passes.
- [ ] `make build` passes.
