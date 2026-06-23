# Ticket 02 - Load selected b-tree and page content automatically

> Feature: **Better UI** (`feature/better_ui`)
> Source feedback: [feedback.md](../feedback.md), item 2
> Current code hotspots: `internal/tui/model.go`, `internal/tui/page_view.go`, `internal/tui/keys_test.go`

## Summary

Remove the extra `Enter` step for inspecting b-tree objects and pages. When the user moves
the navigation cursor onto a b-tree or page row, the relevant content should open
automatically.

Current behavior:
- Arrow keys only move `selectedIndex`.
- `Enter` calls `openSelected`.
- Pages show `Press enter to load this page.` until opened.
- B-tree object details show only after `Enter`.

Target behavior:
- Navigating to a b-tree row immediately renders that table/index detail.
- Navigating to a page row immediately starts loading and then renders that page.
- The user should be able to inspect the database by moving the cursor, without repeatedly
  pressing `Enter`.

## Scope

In scope:
- Call the same content-opening behavior currently behind `Enter` whenever nav selection
  changes.
- Auto-load pages selected by:
  - arrow movement inside the navigation pane,
  - numbered section jumps,
  - filter application/clear if the cursor lands on a page row.
- Prevent stale page content from being shown while a newly selected page is loading.
- Keep page loads asynchronous through `loadPageCmd`.
- Ignore stale `pageLoadedMsg` responses if the user has already moved to another page.
- Update footer/action copy that says `enter open` or `Press enter to load this page`.

Out of scope:
- Changing the page-structure explorer behavior inside an already loaded page.
- Redesigning raw page visualization.
- Adding caching beyond the current `currentPage` behavior unless needed to prevent
  visible flicker or stale data.

## Implementation notes

The simplest shape is to split `openSelected` into reusable logic that can be called after
selection movement:
- `moveSelection` can return whether the selection changed.
- `handleKey` can call something like `activateSelected()` after a successful movement or
  numbered jump.
- For page rows, `activateSelected` should set `active.kind = navPage`, set
  `active.pageNumber`, reset `pageRows`, mark `loading = true`, and return `loadPageCmd`.
- For table/index rows, it should set `active.kind`, `active.schemaName`, clear page
  state, and render details immediately.

Guard async page loading:
- `pageLoadedMsg` currently does not include the requested page number except through the
  returned `PageInspection`.
- Before applying it, confirm `m.active.kind == navPage` and
  `msg.page.PageNumber == m.active.pageNumber`.
- If it is stale, ignore it instead of replacing the current page.

Decide what `Enter` does after this change:
- It can become a harmless no-op for nav rows.
- Or it can remain as an alias for activation, as long as the UI no longer requires it.

## Definition of done

- [ ] Moving onto a table row automatically shows that table's schema/details.
- [ ] Moving onto an index row automatically shows that index's schema/details.
- [ ] Moving onto a page row automatically starts page loading.
- [ ] Page rows no longer show `Press enter to load this page` as the normal path.
- [ ] Stale page loads cannot replace the content for a newer selected page.
- [ ] Footer/action hints no longer imply `Enter` is required to open nav content.
- [ ] Unit tests cover auto-activation for movement and stale page-load protection.

## Manual test

Run:

```bash
make build
./bin/badger fixtures/companies.db
```

Verify:
- Move to `[2] B-TREES` and arrow through several rows. The middle/detail content updates
  as the cursor moves; `Enter` is not needed.
- Move to `[3] PAGES`. The first selected page begins loading immediately.
- Arrow down through pages quickly. The displayed page number must match the selected page
  row after loading settles.
- The old `Press enter to load this page` message does not appear as the main workflow.
- Pressing `Enter` does not break the selected view.

Also test while filtered:
- Move to a b-tree row and apply a filter with `f`.
- Navigate through the filtered page list.
- Each selected page loads automatically and the page count/list remain scoped to the
  active filter.
