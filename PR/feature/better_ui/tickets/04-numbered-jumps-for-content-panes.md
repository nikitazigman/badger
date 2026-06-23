# Ticket 04 - Complete numbered jumps for detail and meta panes

> Feature: **Better UI** (`feature/better_ui`)
> Source feedback: [feedback.md](../feedback.md), item 4
> Current code hotspots: `internal/tui/model.go`, `internal/tui/page_view.go`, `internal/tui/keys_test.go`, `README.md`

## Summary

Extend the numbered navigation model so users can jump directly to the middle and right
panes. The old numbered jumps only move the selection inside the left navigation pane:

- `1` -> MAIN
- `2` -> B-TREES
- `3` -> PAGES

After Ticket 01 removes MAIN, the numbers should represent the useful workflow targets in
order: b-trees, pages, detail view, and meta/info view. Storage-level information is
represented by the `sqlite_schema` system catalog b-tree and page 1, not by a separate
shortcut.

## Scope

In scope:
- Add key handling for `3` and `4`.
- `3` focuses the middle pane, currently represented by `explorerPane`.
- `4` focuses the right pane, currently represented by `inspectorPane`.
- Render visible `[3]` and `[4]` labels so the mapping is discoverable.
- Update footer hints and README docs.
- Update tests for numbered jump behavior.

Out of scope:
- Redesigning page data presentation inside the panes.
- Changing the number of panes.
- Adding mouse-only pane selection.

## Proposed mapping

Use this mapping once Ticket 01 removes MAIN:

```text
[1] B-TREES
[2] PAGES
[3] DETAIL / middle pane / explorerPane
[4] META / right pane / inspectorPane
```

If this ticket is implemented before Ticket 01, do not make `[1] MAIN` more prominent.
Prefer landing Ticket 01 first so the final numbering can be implemented directly.

Naming:
- The code currently calls the middle pane `explorerPane`.
- The user-facing ticket language calls it "detail view".
- The code currently calls the right pane `inspectorPane`.
- The user-facing ticket language calls it "meta info view".

Do not rename internal types unless the implementation becomes clearer. It is fine for UI
labels to say `DETAIL` and `META` while the internal enum remains `explorerPane` and
`inspectorPane`.

## Definition of done

- [ ] Pressing `3` focuses the middle/detail pane.
- [ ] Pressing `4` focuses the right/meta pane.
- [ ] Pressing `3` and `4` does not change the selected nav row or active filter.
- [ ] Existing pane-local controls still work after the jump:
  - in the detail pane, arrows move page structures when a page is active;
  - in the meta pane, arrows/page-up/page-down scroll inspector content.
- [ ] Visible UI copy includes `[3]` and `[4]` where users learn numbered jumps.
- [ ] Footer hints document `3` and `4`.
- [ ] README navigation docs document `3` and `4`.
- [ ] Unit tests cover the new focus jumps and verify they are selection-preserving.

## Manual test

Run:

```bash
make build
./bin/badger fixtures/companies.db
```

Verify:
- Press `1`, select a b-tree row, and ensure the selected row is unchanged after pressing
  `3` and `4`.
- Press `3`. The middle pane receives focus styling.
- Press `4`. The right pane receives focus styling.
- Select/load a page, press `3`, then use up/down. Page-structure selection moves inside
  the middle pane.
- Press `4`, then use `pgup`, `pgdown`, `home`, and arrows. The right pane scrolls without
  moving the left navigation cursor.
- Press `tab` and `shift+tab`; focus cycling still works with the new direct jumps.
