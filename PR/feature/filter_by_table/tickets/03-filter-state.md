# Ticket 03 — Filter end-to-end: state, merged B-TREES nav, `f`/`F`, and filter UI

> Feature: **Filter Pages by Table or Index** (`feature/filter_by_table`)
> Status: **📝 Drafted — ready for implementation.**
> Depends on: [Ticket 02](02-background-indexing.md) (`PageIndex`, `indexErrors`) — ✅ done (`edf55e0`).
> Absorbs: the merged-B-TREES nav + filter-aware PAGES, the `f`/`F` apply/clear keys, and
> all filter rendering (markers, footer token, `Pages: n (filtered)`). Only the section-jump
> keys are split out into [Ticket 04](04-key-bindings.md).
> Context: [context.md](../context.md) · [codebase-map.md](../codebase-map.md) · [feature-notes.md](../feature-notes.md) · [design.md](../design.md)

---

## Why this is one ticket

The original 03→06 split delivered nothing visible until the last ticket. This ticket
instead delivers the **full filtration experience end-to-end** in one slice so it can be
exercised in the running app: select a table/index in a merged `B-TREES` section, press `f`
to scope the `PAGES` list to that b-tree, see the `▶` source marker and the footer token,
and press `F` to clear.

Deliberately **deferred** to a follow-up ([Ticket 04](04-key-bindings.md)):
- `1` / `2` / `3` section-jump key bindings (`design.md` §3).
- `esc` as a second clear binding (`F` is the only clear key here).

Everything else from `design.md` §2 / §4 needed to *see and drive* a filter is in scope.

---

## Decisions confirmed in discussion

- **No pending-filter state.** Indexing is eager and completes in milliseconds (Ticket 02),
  so an un-indexed root is a rare race. Pressing `f` on a not-yet-indexed object reports a
  "still indexing… try again" status and does **not** apply; the user re-presses `f`. The
  `btreeIndexedMsg` reducer from Ticket 02 is **untouched**.
- **Virtual tables / views filter to an empty set.** `RootPage == 0` has no b-tree; `f`
  applies a valid filter with **0 pages** (`design.md` §4.7), shown with the `⊞` icon — not
  rejected.
- **Derive, don't snapshot.** `activeFilter` stores the source object only; its page set and
  skip diagnostics are read from `pageIndex` on demand (DB is read-only, index is immutable
  after build → no staleness, no duplicated slice).
- **`[`/`]` becomes filter-aware** (consistency requirement). With a filtered `PAGES` list,
  prev/next on an open page must step the filtered set, not `1..PageCount`.

---

## Scope

In scope:
1. **State** — `filterSource` + `activeFilter *filterSource`; `applyFilter`, `clearFilter`,
   `isFiltered`, `filteredPages`.
2. **Nav** — merge the separate `Tables` / `Indexes` sections into one `B-TREES` section;
   make `buildNavItems` filter-aware (iterate `filteredPages()` when filtered, else
   `1..PageCount`); rebuild nav on apply/clear.
3. **Keys** — `f` → apply filter to the selected `B-TREES` object; `F` → clear; `[`/`]` →
   step the filtered page set when filtered.
4. **Render** — `▦`/`◈`/`⊞` icons in `B-TREES`; `>` cursor vs `▶` filter-source marker
   (merged to a single `▶` when they coincide); footer token
   `⦿ filtered: <icon> <name> (<n> pg)` with `· n skipped` / `⚠ page N unreadable` when
   degraded and `F clear`; `Pages: n (filtered)` / `Pages: — (unfiltered)` in detail +
   summary; the `still indexing …` / `can't filter …: <reason>` statuses for the failure
   branches.

Out of scope:
- `1`/`2`/`3` section jumps and `esc`-clear (follow-up [Ticket 04](04-key-bindings.md)).
- Overflow chains / pulling a table's indexes into its set (cut in Tickets 01/02).
- On-disk persistence of the active filter across restarts (`design.md` §7: no).
- The reverse `page → owner` mapping (never built — Ticket 02).

---

## Building blocks already in place

- `pageIndex sqlite.PageIndex` — `Walk(root) (PageWalk, bool)`, `Pages(root) []uint32`
  (nil when unindexed); `PageWalk.Skipped []SkippedPage{Page, Parent, Reason}` — Ticket 02.
- `indexErrors map[uint32]string`, `indexPending int` on `model` — Ticket 02.
- `schemaObjectViewModel{ Type, Name, TableName string; RootPage uint32; SQL string }`;
  `databaseViewModel.Tables` / `.Indexes` (virtual tables/views carry `RootPage == 0`) —
  `internal/tui/view_model.go`.
- `buildNavItems(db)`, `navItem`, `navKind` (`navOverview|navDBHeader|navTable|navIndex|navPage`),
  `selectedIndex`, `viewNavigation`, `handleKey` (incl. existing `[`/`]` paging),
  `viewSchemaObject` / `viewPage` (detail titles), `viewInspector` (summary), and the
  footer/status line in `View` — `internal/tui/model.go` (per `codebase-map.md`).

---

## 1. State (`internal/tui/model.go`)

```go
// filterSource identifies the single object PAGES is scoped to. Stores the object only;
// page set + skip diagnostics are derived from pageIndex.
type filterSource struct {
    object schemaObjectViewModel // Type → icon, Name, RootPage (0 for virtual/views)
}

// model addition
activeFilter *filterSource // nil = unfiltered

func (m *model) applyFilter(obj schemaObjectViewModel) {
    switch {
    case obj.RootPage == 0: // virtual table / view: no b-tree, valid empty filter
        m.setFilter(obj)
    case m.pageIndex != nil && walkPresent(m.pageIndex, obj.RootPage):
        m.setFilter(obj)
    case hasIndexError(m, obj.RootPage):
        m.status = "can't filter " + obj.Name + ": " + m.indexErrors[obj.RootPage]
    default:
        m.status = "still indexing " + obj.Name + "… try again in a moment"
    }
}

// setFilter stores the filter, rebuilds the nav list (PAGES re-scopes), and keeps the
// cursor on the source row (design.md §4.2).
func (m *model) setFilter(obj schemaObjectViewModel) {
    m.activeFilter = &filterSource{object: obj}
    m.navItems = buildNavItems(m.db, m.activeFilter) // filter-aware
    m.selectedIndex = indexOfBTreeRow(m.navItems, obj)
}

func (m *model) clearFilter() {
    if m.activeFilter == nil {
        return
    }
    src := m.activeFilter.object
    m.activeFilter = nil
    m.navItems = buildNavItems(m.db, nil)
    m.selectedIndex = indexOfBTreeRow(m.navItems, src) // stay on the same B-TREES row
}

func (m model) isFiltered() bool { return m.activeFilter != nil }

// filteredPages returns (pages, true) when filtered (empty for a virtual table), else
// (nil, false). bool means "filter active", NOT "has pages".
func (m model) filteredPages() ([]uint32, bool) {
    if m.activeFilter == nil {
        return nil, false
    }
    root := m.activeFilter.object.RootPage
    if root == 0 {
        return []uint32{}, true
    }
    return m.pageIndex.Pages(root), true
}
```

`Pages: n (filtered)` count and `n skipped` / `⚠ page N` come from
`m.pageIndex.Walk(activeFilter.object.RootPage)` (`PageWalk.Pages` length + `.Skipped`).

---

## 2. Navigation (`buildNavItems` + `viewNavigation`)

- **Merge sections.** Replace the separate `TABLES` / `INDEXES` headers with one `B-TREES`
  header listing tables then indexes (or schema order — confirm), each row carrying its
  `schemaObjectViewModel`. Keep `navTable` / `navIndex` kinds; the section header is what
  merges.
- **Icons.** `navIndex` → `◈`; `navTable` with `RootPage == 0` → `⊞` (virtual); other
  `navTable` → `▦`. Centralize in an `objectIcon(obj)` helper (also used by detail/footer).
- **Filter-aware PAGES.** `buildNavItems(db, activeFilter)` builds the `PAGES` rows from
  `filteredPages()` when a filter is active (empty list for a virtual table) and from
  `1..PageCount` otherwise.
- **Rebuild trigger.** Only `applyFilter` / `clearFilter` change the filter, and both rebuild
  `navItems` + fix `selectedIndex` (above). No rebuild on plain navigation.

---

## 3. Key bindings (`handleKey`)

- **`f`** — when the selected nav row is a `B-TREES` object (`navTable`/`navIndex`), call
  `applyFilter(selectedObject)`. No-op (or ignored) on `MAIN`/`PAGES` rows.
- **`F`** — `clearFilter()`. No-op when unfiltered.
- **`[` / `]`** — when filtered and a page is open, step to the prev/next page **within
  `filteredPages()`** (clamp at ends); unchanged (`1..PageCount`) when unfiltered.
- Existing bindings (`tab`, arrows, `enter`, `g`, `h`, `q`, …) preserved. `1`/`2`/`3` and
  `esc`-clear are **not** added here.

---

## 4. Rendering

- **Markers (`viewNavigation`).** Cursor row → `>`; the `activeFilter` source row → `▶`;
  when the cursor is on the source row, render a single `▶` (no `> ▶`). One marker per row,
  everywhere.
- **Footer (`View`).** When filtered:
  `⦿ filtered: <icon> <name> (<n> pg) | F clear | … | q quit`, with
  `(<n> pg · <k> skipped)` and a trailing `⚠ page <N> unreadable` when
  `PageWalk.Skipped` is non-empty (`design.md` §4.6). When unfiltered, the existing footer.
  The `still indexing …` / `can't filter …` statuses surface via the normal `m.status` line.
- **Detail + summary panes.** `viewSchemaObject` title and `viewPage` title echo the icon;
  the summary shows `Pages: <n> (filtered)` when filtered (with the source object) and
  `Pages: — (unfiltered)` otherwise; a virtual table shows `Root: —` and `Pages: 0 (filtered)`.

---

## Acceptance criteria

**State**
- [ ] `filterSource`, `activeFilter`, `applyFilter`, `clearFilter`, `isFiltered`,
      `filteredPages` exist with the behavior above.
- [ ] Indexed object → `activeFilter` set, `filteredPages()` equals `pageIndex.Pages(root)`.
- [ ] Virtual table (`RootPage == 0`) → `activeFilter` set, `filteredPages()` returns
      `([]uint32{}, true)`.
- [ ] Hard-failed root → unfiltered, `can't filter …` status.
- [ ] Not-yet-indexed root → unfiltered, `still indexing …` status.
- [ ] Applying a second filter replaces the first; `clearFilter` returns to `(nil, false)`.
- [ ] No pending-filter state; `btreeIndexedMsg` reducer unchanged.

**Nav**
- [ ] One `B-TREES` section lists tables + indexes with `▦`/`◈`/`⊞` icons; no separate
      `TABLES`/`INDEXES` headers remain.
- [ ] When filtered, `PAGES` lists exactly `filteredPages()` (empty for a virtual table);
      when unfiltered, the full `1..PageCount`.
- [ ] Applying keeps the cursor on the source row; clearing keeps it on that same row.

**Keys**
- [ ] `f` on a `B-TREES` row applies the filter; `f` elsewhere is a no-op.
- [ ] `F` clears the filter; no-op when unfiltered.
- [ ] `[`/`]` step the filtered set when filtered and `1..PageCount` when not; clamp at ends.
- [ ] `1`/`2`/`3` and `esc`-clear are **not** wired (left for the follow-up).

**Render**
- [ ] Cursor `>` vs source `▶`, merged to one `▶` when coincident; never two markers on a row.
- [ ] Footer shows `⦿ filtered: <icon> <name> (<n> pg)` (+ `· k skipped` / `⚠ page N`) and
      `F clear` when filtered; normal footer otherwise.
- [ ] Summary shows `Pages: n (filtered)` / `Pages: — (unfiltered)`; virtual table shows
      `Root: —`, `Pages: 0 (filtered)`.

**General**
- [ ] `go vet ./...` clean; existing tests pass.

---

## Testing

State + nav + keys are unit-testable in `internal/tui` (extend `index_test.go` /
`newFixtureModel`); rendering is asserted on `View()` output substrings.

- **Filter state** (as the original Ticket 03): ready / virtual / hard-failed / not-indexed /
  switch / clear, plus a degraded (`Skipped` non-empty) root still applying.
- **Nav rebuild** — after `applyFilter(companies)`, the `PAGES` rows equal
  `pageIndex.Pages(root)` and the cursor sits on the `companies` row; after `clearFilter`,
  `PAGES` is `1..PageCount` again on the same row.
- **`f`/`F` via `Update`** — feed a `KeyMsg{f}` with a `B-TREES` row selected → filtered;
  `KeyMsg{F}` → unfiltered. `f` on a `MAIN`/`PAGES` row → no change.
- **`[`/`]` filtered paging** — with a filter active and a page open, `]` advances within
  `filteredPages()` and clamps at the last filtered page (does not jump to `+1`).
- **Render** — `View()` contains `⦿ filtered: ▦ companies (… pg)` and `F clear` when
  filtered; a virtual-table filter shows `⊞`, `(0 pg)`, and `Pages: 0 (filtered)`; the source
  row shows `▶` and a single marker when the cursor is on it.

---

## Notes for the follow-up ticket ([Ticket 04](04-key-bindings.md))

What remains after this ticket is the **section-jump navigation**: `1` → first `MAIN` row,
`2` → first `B-TREES` row, `3` → first `PAGES` row (`design.md` §3), plus optionally `esc`
as a second clear binding. Those are pure `handleKey` additions over the structure this
ticket already builds.
