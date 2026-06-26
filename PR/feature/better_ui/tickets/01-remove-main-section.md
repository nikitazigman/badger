# Ticket 01 - Remove the persistent MAIN section

> Feature: **Better UI** (`feature/better_ui`)
> Source feedback: [feedback.md](../feedback.md), item 1
> Current code hotspots: `internal/tui/model.go`, `internal/tui/keys_test.go`, `README.md`

## Summary

Remove the current `[1] MAIN` navigation section from the always-visible navigation list.
The existing overview and database-header content is useful as optional context, but it
should not permanently consume navigation space. The first screen should prioritize the
things users inspect most often: b-trees, pages, and page/details content.

Current behavior:
- `buildNavItems` always prepends `Overview` and `DB Header`.
- `sectionForNavItem` groups those rows under `MAIN`.
- key `1` jumps to the first `MAIN` row.
- `esc` can reset `active` to `navOverview`.
- `viewOverview`, `viewDBHeader`, and the inspector have dedicated branches for those
  rows.

Target behavior:
- The navigation pane no longer renders a `MAIN` section or `Overview` / `DB Header` rows.
- Storage metadata remains reachable through the normal b-tree/page workflow:
  - add an explicit `sqlite_schema` system catalog row to `B-TREES`;
  - keep the navigation row short: render the row as `sqlite_schema`, without long
    inline labels;
  - explain in the detail/meta panes that it is SQLite-managed / automatically created;
  - use root page `1`;
  - make clear that page 1 also contains the database header before the b-tree payload.
- The numbered jumps are updated as part of the Better UI feature so removing `MAIN` does
  not leave stale or shifted labels behind.

## Scope

In scope:
- Remove `Overview` and `DB Header` from the normal `buildNavItems` output.
- Replace the old numbering with the feature-level target model:
  - `1` jumps to `B-TREES`.
  - `2` jumps to `PAGES`.
  - `3` and `4` are reserved for the detail and meta pane focus jumps from Ticket 04.
- Add a first-class `sqlite_schema` system catalog row to `B-TREES` instead of a separate
  info shortcut. The user can select it, inspect it, and filter pages from it like any
  other b-tree row.
- Keep and reuse the useful database-header rendering code in the detail/meta rendering
  for page 1 and the `sqlite_schema` system catalog row.
- Update footer hints, section headers, and tests that currently expect `[1] MAIN`.
- Update README navigation docs.

Out of scope:
- Redesigning the page-data representation. That is feedback item 5 and should be handled
  separately.
- Removing the underlying metadata inspection from the SQLite/data layer.
- Deleting useful overview/header renderer code if it can be reused by page 1 or
  `sqlite_schema` rendering.

## Implementation notes

Start from the navigation model:
- `buildNavItems` currently starts with `navOverview` and `navDBHeader`; stop adding those
  as always-visible rows.
- `sectionForNavItem` and `sectionLabel` should no longer expose `MAIN`.
- `selectFirstKind(navOverview)` is no longer a valid primary jump. Key `1` should select
  the first b-tree row, and key `2` should select the first page row.
- `openSelected` and `viewExplorer` need a sensible default active target when the app
  starts. Prefer landing on the first b-tree row if one exists; otherwise the first page.
- `sqlite_schema` is implicit in SQLite and is not listed as an ordinary record inside
  `sqlite_schema`, so the TUI view model should synthesize this system row explicitly:
  `Type: "table"`, `Name: "sqlite_schema"`, `TableName: "sqlite_schema"`,
  `RootPage: 1`, plus a system/auto-created flag or equivalent display metadata.
- Render the navigation row as the short name only: `sqlite_schema`. Do not add inline
  text such as `system catalog`, `automatically created`, or `root page 1` in the left
  navigation pane; those strings are too wide for small terminals.
- Explain the system-row details in the detail/meta panes instead: `System catalog`,
  `SQLite-created`, `Root page: 1`, and that filtering shows all reachable catalog b-tree
  pages. Page 1's detail/meta view should explain that bytes 0-99 are the database header
  before the b-tree page content.
- Filtering the `sqlite_schema` row should work through the existing b-tree filter model
  and scope `PAGES` to the catalog b-tree pages.
- `esc` should not reset to an invisible `Overview` row. It can clear filters first, then
  reset page sub-selection/loading state without navigating to removed content.

Keep an eye on small databases:
- A database may have no tables or indexes.
- A root-page-zero object can still exist in `B-TREES`.
- `sqlite_schema` / page 1 should still be present for any valid SQLite database.
- `PAGES` should remain available for any valid SQLite database with pages.

## Definition of done

- [x] The navigation pane does not show `[1] MAIN`, `Overview`, or `DB Header`.
- [x] The first visible navigation section is the primary inspection section, not metadata.
- [x] `B-TREES` includes a short `sqlite_schema` row; system/root/header explanations live
      in the detail/meta panes, not inline in navigation.
- [x] The app starts with a valid visible selection and no hidden `active` target.
- [x] Numbered navigation uses the new feature mapping: `1` -> `B-TREES`, `2` -> `PAGES`;
      no key jumps to removed `MAIN` content.
- [x] Selecting/filtering `sqlite_schema` works like other b-tree rows.
- [x] `esc` no longer navigates to removed overview content.
- [x] Footer key hints do not mention MAIN or removed metadata rows.
- [x] Tests that currently assert `[1] MAIN` are updated or replaced.
- [x] README navigation docs match the new layout.

## Implementation progress

Implemented in the current working tree:
- Removed `Overview` and `DB Header` from normal navigation output.
- Remapped numeric jumps so `1` selects `B-TREES`, `2` selects `PAGES`, and `3`/`4` are
  reserved no-ops for later pane-focus tickets.
- Added a synthesized `sqlite_schema` system catalog row with `RootPage: 1` and system
  display metadata. The row is selectable and filterable through the existing b-tree
  filtering path.
- Kept the `sqlite_schema` nav row short and factual. It does not display a fabricated
  `CREATE TABLE sqlite_schema(...)` statement, because SQLite does not store a
  `sqlite_schema` row for `sqlite_schema` itself.
- Added detail/meta text for `sqlite_schema` and page 1 explaining that page 1 contains
  the 100-byte database header before the b-tree payload.
- Fixed the follow-up layout issue seen when opening tables with multi-line `CREATE TABLE`
  SQL: embedded newlines are now split before pane height calculations, so Explorer
  content no longer shifts the surrounding pane layout.
- Restored table/index/root-zero glyphs in navigation after confirming the layout bug was
  caused by multi-line SQL rendering, not by glyph width.
- Updated README navigation docs.

Verification completed:
- `go test ./internal/tui`
- `make test`
- `make build`

## Manual test

Run:

```bash
make build
./bin/badger fixtures/companies.db
```

Verify:
- On launch, the navigation pane has no `[1] MAIN` section.
- `Overview` and `DB Header` are not present as navigation rows.
- The cursor starts on a visible row.
- The first `B-TREES` row is the short label `sqlite_schema`.
- Selecting `sqlite_schema` explains in the detail/meta panes that it is the SQLite-created
  system catalog with root page 1.
- `1` gets you to the b-tree list.
- `2` gets you to pages.
- Filtering `sqlite_schema` scopes `PAGES` to its b-tree pages.
- Opening `page 1` still shows the database header plus the page-1 b-tree structure.
- `esc` does not show an overview screen or move to a missing row.

Repeat with:

```bash
./bin/badger fixtures/sample.db
./bin/badger fixtures/superheroes.db
```

The navigation should remain stable and should not show empty or stale MAIN content.
