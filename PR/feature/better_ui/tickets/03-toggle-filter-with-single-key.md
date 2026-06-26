# Ticket 03 - Replace `f`/`F` with single-key filter toggle

> Feature: **Better UI** (`feature/better_ui`)
> Source feedback: [feedback.md](../feedback.md), item 3
> Current code hotspots: `internal/tui/model.go`, `internal/tui/filter_test.go`, `README.md`

## Summary

Use one key for selecting and deselecting a b-tree page filter. The current split between
lowercase `f` to apply and uppercase `F` to clear is unnecessary.

Current behavior:
- On a table/index row, `f` applies a filter.
- `F` clears the active filter from anywhere.
- `esc` also clears the active filter before doing anything else.
- The active source row is marked with `▶`.

Target behavior:
- Pressing `f` on an unfiltered table/index row applies that row as the filter.
- Pressing `f` again on the active source row clears the filter.
- Pressing `f` on a different table/index row switches the filter to that object.
- Uppercase `F` is removed from the advertised workflow and should not be needed.

## Scope

In scope:
- Update `handleKey` so `f` toggles the filter when the selected row is the active filter
  source.
- Keep `f` as "switch filter" when another table/index row is selected.
- Keep `f` as a no-op on non-b-tree rows unless Ticket 04 explicitly maps it differently.
- Remove `F clear` from footer hints, inspector action copy, tests, and README docs.
- Decide whether uppercase `F` should become a strict no-op or a temporary backward
  compatible alias. The desired UI should not advertise it either way.

Out of scope:
- Changing how the page index is built.
- Changing what pages belong to a table/index filter.
- Changing `esc` unless a separate design decision removes its clear-filter behavior.

## Implementation notes

The existing helpers are close to what is needed:
- `m.isFiltered()`
- `m.objectIsFilterSource(obj)`
- `m.applyFilter(obj)`
- `m.clearFilter()`

The `f` branch can follow this decision tree:

```text
if selected row is not table/index:
    no-op
else if selected object is the active filter source:
    clearFilter()
else:
    applyFilter(selected object)
```

After applying or clearing, preserve the current selection behavior:
- Applying a filter should keep the cursor on the source row.
- Clearing from the source row should keep the cursor on that same b-tree row.
- Switching filters should move the source marker to the new object.

Footer copy should become something like:
- unfiltered: `tab focus · arrows move · f filter · q quit`
- filtered: `filtered: ... | f clear/switch · tab focus · q quit`

Use exact wording that fits the final key model after Ticket 02 removes the need for
`enter open`.

## Definition of done

- [ ] `f` applies a filter from an unfiltered b-tree row.
- [ ] `f` clears the filter when pressed on the active source row.
- [ ] `f` switches the active filter when pressed on another b-tree row.
- [ ] `f` remains a no-op on page and meta/detail rows.
- [ ] `F` is no longer advertised in the footer, inspector actions, README, or tests.
- [ ] The active source marker remains correct after apply, clear, and switch.
- [ ] Tests cover apply, clear-by-toggle, switch-by-toggle, and non-b-tree no-op behavior.

## Manual test

Run:

```bash
make build
./bin/badger fixtures/companies.db
```

Verify:
- Jump to b-trees and select `companies`.
- Press `f`. The footer shows a filtered state, `PAGES` is scoped to `companies`, and the
  source row has the `▶` marker.
- Press `f` again while still on `companies`. The filter clears and the full page list
  returns.
- Press `f` on `companies`, move to `idx_companies_country`, then press `f`. The filter
  switches to the index and the marker moves to that row.
- Move to a page row and press `f`. The current filter state does not change.
- The footer and inspector action text do not mention uppercase `F`.
