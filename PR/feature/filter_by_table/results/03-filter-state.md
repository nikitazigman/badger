# Result — Ticket 03: Filter end-to-end (state, merged B-TREES nav, `f`/`F`, filter UI)

> Feature: **Filter Pages by Table or Index** (`feature/filter_by_table`)
> Ticket: [tickets/03-filter-state.md](../tickets/03-filter-state.md) · Status: **✅ Done**
> Commit: `93c761b` — *Implement ticket 3*
> Depends on: [Ticket 02](02-background-indexing.md) (`PageIndex`, `indexErrors`) — ✅ done (`edf55e0`)

---

## Summary

Delivered the **full filtration experience end-to-end** in one slice, so it is exercisable
in the running app: select a table/index in the merged `B-TREES` section, press `f` to
scope `PAGES` to that b-tree, see the `▶` source marker and the footer token, and press `F`
to clear. This is the first ticket where Ticket 02's launch-time `PageIndex` becomes
user-visible — applying a filter is an instant `pageIndex.Pages(root)` lookup, never a walk.

The filter follows the ticket's **derive-don't-snapshot** rule: `activeFilter` stores only
the source object; its page set and skip diagnostics are read from `pageIndex` on demand
(the DB is read-only and the index is immutable after build, so there is no staleness and
no duplicated slice). There is **no pending-filter state** — pressing `f` on a not-yet-walked
root reports a retry status and does not apply; the `btreeIndexedMsg` reducer is untouched.

All work landed in `internal/tui` (state + nav + keys + render); the `sqlite` data layer
was not touched.

---

## What was delivered

| File | Δ | Purpose |
| --- | --- | --- |
| `internal/tui/model.go` | +324/−38 | filter state, filter-aware nav + paging, `f`/`F` keys, markers/footer/summary render |
| `internal/tui/filter_test.go` | +318 (new) | 12 unit tests: state, nav rebuild, keys via `Update`, filtered paging, render |

### State (model.go)

```go
type filterSource struct {
    object schemaObjectViewModel // Type → icon, Name, RootPage (0 for virtual / views)
}

// model addition
activeFilter *filterSource // nil = unfiltered

func (m *model) applyFilter(obj schemaObjectViewModel) // virtual / indexed → setFilter; else status
func (m *model) setFilter(obj schemaObjectViewModel)   // store, rebuild nav, cursor on source
func (m *model) clearFilter()                          // drop filter, rebuild, cursor on same row
func (m model)  isFiltered() bool
func (m model)  filteredPages() ([]uint32, bool)       // ([]uint32{}, true) for a virtual table
```

Supporting helpers: `walkPresent(root)`, `hasIndexError(root)`, `objectIsFilterSource(obj)`,
`indexOfBTreeRow(items, obj)`, and `objectIcon(obj)` (centralizes `▦`/`◈`/`⊞`).

`applyFilter` decision table (matches `design.md` §4.5 / §4.7):

| Object state | Outcome |
| --- | --- |
| `RootPage == 0` (virtual / view) | filter applied, **0 pages** (`⊞`) — not rejected |
| walk present in `pageIndex` | filter applied to `pageIndex.Pages(root)` |
| `indexErrors[root]` set | unfiltered, `⚠ can't filter <icon> <name>: <reason>` |
| otherwise (not yet walked) | unfiltered, `still indexing <icon> <name>… try again in a moment` |

### Nav (model.go)

- **Merged section.** `sectionForNavItem` now maps both `navTable` and `navIndex` to one
  `B-TREES` section; the separate `Tables` / `Indexes` headers are gone. The `root N`
  subtitle was dropped from B-TREES rows (it lives in the detail / summary panes —
  `design.md` §2); each row instead renders its `objectIcon`.
- **Filter-aware PAGES.** `buildNavItems(db, filter, filteredPages)` lists the filtered set
  when `filter != nil` (empty for a virtual table) and the full `1..PageCount` otherwise.
  Only `applyFilter` / `clearFilter` rebuild `navItems`; both fix `selectedIndex` via
  `indexOfBTreeRow` so the cursor stays on the source row.

### Keys (handleKey)

- **`f`** — on a `navTable` / `navIndex` row, calls `applyFilter(*item.schema)`; a no-op
  on `MAIN` / `PAGES` rows.
- **`F`** — `clearFilter()`; a no-op when unfiltered.
- **`[` / `]`** — refactored into `openRelativePage` → `openPageNumber` + a pure
  `stepWithin(pages, current, delta)` helper. When filtered it steps the **filtered** set
  and **clamps at the ends** (a no-op past either end — never jumps to `current±1` outside
  the filter); unchanged `1..PageCount` behaviour when unfiltered.
- `1` / `2` / `3` and `esc`-clear are **not** wired (deferred to Ticket 04).

### Render (model.go)

- **Markers.** `navMarker(idx)` returns `▶` for the active-filter source (it wins, so the
  cursor and source merge into a single `▶` when they coincide), `>` for the cursor, two
  spaces otherwise — never two markers on a row.
- **Footer.** `filterToken()` renders `⦿ filtered: <icon> <name> (<n> pg)` with a degraded
  tail (`· k skipped` + `⚠ page <N> unreadable`) when `PageWalk.Skipped` is non-empty
  (`design.md` §4.6).
- **Detail + summary.** `viewSchemaObject` title echoes the icon (`▦ TABLE companies`) and
  shows `Root page: — (no b-tree)` for a virtual table; the summary pane shows
  `Pages: <n> (filtered)` for the active source and `Pages: — (unfiltered)` otherwise,
  with `Root: —` for a virtual table. `viewPage` titles echo the filter icon when filtered.

---

## Beyond the ticket (UX follow-up)

After review the footer was reworked into an **always-on key-hint bar** (it previously
only showed `m.status`, which transient messages overwrote, and swapped to the filter token
only when filtered). The footer is now `context  |  keys`:

- **Idle / after an action** → latest transient status (or nothing) + `navKeys`.
- **Filtered** → `filterToken()` + a shorter filter-aware key set (`filterKeys`) so the
  token and hints fit on one line.

```
LAUNCH:    indexed 3 b-trees  |  tab focus · ↑↓ move · enter open · f filter · g overview · h header · [ ] page · q quit
STATUS:    opened page 5      |  tab focus · ↑↓ move · enter open · f filter · …
FILTERED:  ⦿ filtered: ▦ companies (1664 pg)  |  F clear · tab focus · enter open · [ ] page · q quit
```

Two other small additions not spelled out in the ticket: `applyFilter` / `clearFilter` set
a brief confirmation status (`filtered to <icon> <name>` / `filter cleared`), and the launch
`status` is now empty (the key bar is always present, so the old key-hint seed was redundant).

---

## Acceptance criteria

All boxes in the ticket are checked. Highlights:

- [x] State: `filterSource` / `activeFilter` / `applyFilter` / `clearFilter` / `isFiltered`
      / `filteredPages` with the indexed / virtual / hard-failed / not-indexed / switch /
      clear behaviour; no pending-filter state; `btreeIndexedMsg` reducer unchanged.
- [x] Nav: one `B-TREES` section with `▦`/`◈`/`⊞` icons; filtered `PAGES` equals
      `filteredPages()`; cursor stays on the source row across apply / clear.
- [x] Keys: `f` applies (no-op off B-TREES), `F` clears (no-op when unfiltered), `[`/`]`
      step + clamp the filtered set; `1`/`2`/`3` and `esc`-clear left for Ticket 04.
- [x] Render: single `▶`/`>` marker; footer token + `F clear` when filtered; summary
      `Pages: n (filtered)` / `— (unfiltered)`; virtual table `⊞`, `0 pg`, `Root: —`.
- [x] `go build ./...`, `go vet ./...` clean; full `go test ./...` green.

---

## Tests

12 new test functions in `internal/tui/filter_test.go` (all passing); the existing Ticket 02
suite and the `sqlite` package are untouched and still green. Helpers added: `indexAll`
(feeds every root's `btreeIndexedMsg` through `Update` to populate `pageIndex`),
`objectByName`, and `keyMsg`.

| Test | Covers |
| --- | --- |
| `TestApplyFilterIndexed` | indexed object → filtered; `filteredPages()` equals `PagesForRoot(root).Pages` |
| `TestApplyFilterVirtualTable` | injected `RootPage == 0` object → `([]uint32{}, true)`, not rejected |
| `TestApplyFilterHardFailed` | `indexErrors[root]` set → unfiltered, `can't filter` status |
| `TestApplyFilterNotYetIndexed` | fresh model (no walk yet) → unfiltered, `still indexing` status |
| `TestSwitchAndClearFilter` | second `applyFilter` replaces the first; `clearFilter` → `(nil, false)` |
| `TestDegradedFilterStillApplies` | synthetic `Skipped` walk still applies; footer shows `1 skipped` + `⚠ page 774 unreadable` |
| `TestNavRebuildOnApplyAndClear` | filtered `PAGES` rows equal `filteredPages()`, cursor on `companies`; clear → full `1..PageCount`, same row |
| `TestFilterKeysViaUpdate` | `KeyMsg{f}` on a B-TREES row → filtered; `KeyMsg{F}` → unfiltered |
| `TestFilterKeyNoOpOnNonBTreeRow` | `f` on a `MAIN` row → no change |
| `TestFilteredPagingClampsAtEnds` | `]` advances within `filteredPages()` and clamps at the last filtered page (no jump to `+1`) |
| `TestFilterRenderFooterAndMarkers` | `View()` has `⦿ filtered: ▦ companies` + `F clear`; single `▶` marker on the source row |
| `TestFilterRenderVirtualTable` | `View()` has `⦿ filtered: ⊞ fts_docs (0 pg)` and `Pages: 0 (filtered)` |

No fixture ships a virtual table, so the two virtual-table tests inject a synthetic
`{Type: "table", Name: "fts_docs", RootPage: 0}` object into the view model.

---

## Notes for the follow-up ticket ([Ticket 04](../tickets/04-key-bindings.md))

What remains is the **section-jump navigation** built on this ticket's structure: `1` →
first `MAIN` row, `2` → first `B-TREES` row, `3` → first `PAGES` row (`design.md` §3), plus
optionally `esc` as a second clear binding. These are pure `handleKey` additions —
`selectFirstKind(kind)` already exists and does exactly the jump those keys need. If the
always-on footer's `navKeys` / `filterKeys` strings gain `1 main · 2 b-trees · 3 pages`,
update them alongside the bindings so the hint bar stays accurate.
