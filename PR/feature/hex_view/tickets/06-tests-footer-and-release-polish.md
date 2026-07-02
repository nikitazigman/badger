# Page Hex View Ticket 06 - Tests, footer, and release polish

> Feature: **Hex View** (`feature/hex_view`)
> Parent design: [design.md](../design.md)
> Implementation map: [implementation-map.md](../implementation-map.md)
> Depends on: [05-visual-refinement-and-coverage-gaps.md](05-visual-refinement-and-coverage-gaps.md)
> Current code hotspots: `internal/tui/model.go`, `internal/tui/keys_test.go`, `README.md`

## Summary

Close the feature with tests, footer hints, and final manual smoke coverage.

At this point the page HEX/META workflow should be complete enough to use end to end. This
ticket makes sure the implemented behavior is documented, discoverable, and guarded by
tests.

## What will be visible after this ticket

Footer hints should match implemented behavior and avoid skipped features:

```text
filtered: ▦ companies (1664 pg) | ↑↓/kj move · 3 hex · 4 meta · d drill · q quit
```

Do not advertise `i info` while the info view remains deferred.

## Scope

In scope:

- Finalize footer hints for:
  - normal page navigation;
  - filtered navigation;
  - HEX focus;
  - META focus;
  - drill availability if implemented contextually.
- Add automated tests for the full flow.
- Update README or feature docs if they currently describe the old page table.
- Run the full test suite.
- Build the binary.
- Manually smoke-test fixtures.

Out of scope:

- New feature behavior beyond fixing bugs found during tests.
- `i` info/legend view.
- Pointer-array drill unless it was pulled into Ticket 05.

## Required test coverage

Navigation/state:

- `3` from a loaded page focuses HEX and selects the first block.
- Up/down in HEX changes selected block, not page navigation.
- Moving HEX selection resets `inspectorScroll`.
- `4` from PAGES shows page meta.
- `4` from HEX preserves selected block meta.
- Up/down in META scrolls only meta content.
- Page movement in PAGES resets hex selection and drill state.
- `d` enters drill for a cell with children.
- `d` again exits drill and reselects the parent block.
- `d` on a block without children is a no-op.

Rendering:

- `[3] HEX` title appears instead of `[3] DETAIL` for page content.
- Page view contains `Offset` and 16 hex columns.
- Page view does not contain `STRUCTURES`, raw ASCII, or selected-block footer.
- Page meta does not contain raw hex.
- Block meta contains title, offset/range, size, and parsed fields.
- Drill meta contains parent, offset/range, size, and parsed fields.

Lower-level helpers:

- Top-level block builder sorts by physical offset.
- Page 1 includes database header before page header.
- Selected range across multiple rows marks only bytes in the selected range.
- Hex viewport scroll reveals a selected block outside the visible rows.

## Definition of done

- [ ] Footer hints match the implemented feature set.
- [ ] Footer does not mention deferred `i info`.
- [ ] Tests cover navigation, HEX rendering, page meta, block meta, drill meta, and reset behavior.
- [ ] README or relevant docs no longer describe the old page structure table as the primary page view.
- [ ] `go test ./...` passes.
- [ ] `make build` passes.
- [ ] Manual smoke test passes on `fixtures/companies.db`.
- [ ] Manual smoke test passes on at least one additional fixture.

## Manual test

Run:

```bash
go test ./...
make build
./bin/badger fixtures/companies.db
```

Verify the complete flow:

- Press `2`, choose a page.
- Confirm page meta.
- Press `3`, move through blocks.
- Confirm block meta.
- Drill into a cell.
- Confirm drill meta.
- Press `4`, scroll meta.
- Press `3`, return to same HEX selection.
- Change page and confirm HEX/drill state resets.
- Confirm footer hints match the current focus and do not mention unimplemented commands.

