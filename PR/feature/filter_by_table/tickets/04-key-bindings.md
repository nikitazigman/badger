# Ticket 04 — Section-jump keys (`1`/`2`/`3`) & `esc`-clear (remainder)

> Feature: **Filter Pages by Table or Index** (`feature/filter_by_table`)
> Status: **✅ Done — see [results/04-key-bindings.md](../results/04-key-bindings.md).** Depends on [Ticket 03](03-filter-state.md) (✅ done, `93c761b`).
> Context: [context.md](../context.md) · [codebase-map.md](../codebase-map.md) · [feature-notes.md](../feature-notes.md) · [design.md](../design.md)

---

## Short description

The **remainder** of the key bindings after [Ticket 03](03-filter-state.md) shipped the
filtration experience (`f` apply, `F` clear). This ticket adds the lazygit-style numbered
section jumps from `design.md` §3 that were intentionally deferred, plus `esc` as a second
clear-filter binding:

- `1` → jump selection to the first `MAIN` row (Overview).
- `2` → jump selection to the first `B-TREES` row.
- `3` → jump selection to the first `PAGES` row.
- `esc` → clear the active filter (same effect as `F`) before its existing behaviour.

The numbered jumps also become the **only** way to cross sections: `↑`/`↓` are confined to
the current section (MAIN / B-TREES / PAGES) and `1`/`2`/`3` move between them. This keeps
arrow navigation predictable inside long lists (e.g. thousands of pages) and makes the
section jumps load-bearing rather than convenience-only.

The section jumps are made **discoverable** by numbering the nav-pane section headers
themselves — `[1] MAIN` / `[2] B-TREES` / `[3] PAGES` — so the footer no longer has to spell
them out. Two changes landed during implementation review (see _Decisions_ below): the jump
numbers moved onto the headers, and the `[`/`]` prev/next-page binding shipped in Ticket 03
was **removed** (paging a filtered set is now "jump to `PAGES`, use arrows").

These are `handleKey` additions plus a small change to `moveSelection` and `viewNavigation`
over the nav structure Ticket 03 already builds. No state, parsing, or filter-rendering
changes beyond the footer hint strings and section-header labels.

---

## Decisions confirmed in discussion

These points are where `design.md` §3 collided with behaviour Ticket 03 already shipped, or
where implementation review changed direction; all were resolved in discussion and
**supersede the design table** where they differ.

- **`1`/`2`/`3` are select-only and *replace* `g`/`h`/`p`.** The numbered jumps move the nav
  cursor **without opening** the item (matching today's `p`); `enter` opens. The pre-existing
  `g` (overview), `h` (header) and `p` (first page) bindings are **removed** in favour of the
  numbered scheme — they were redundant (`g`≈`1`, `p`≈`3`) and the design moved to numbered
  jumps. _This diverges from `design.md` §3, which still lists `g`/`h`; update §3 and the
  wireframe footers as part of this ticket (see §4 below)._
- **`esc` clears the filter first, then falls through.** When a filter is active, `esc`
  clears it and stops (`design.md` §3: "active only while filtered"). When unfiltered, `esc`
  keeps its existing meaning untouched: return to the page summary inside an open page
  (`explorerIndex → 0`), otherwise reset the view to Overview. This is the simplest mental
  model and never strands the user in a filtered state.
- **Arrows are section-confined; `1`/`2`/`3` cross sections.** `↑`/`↓` stop at the first /
  last row of the current section instead of spilling into the adjacent one. Crossing
  between MAIN, B-TREES and PAGES is done exclusively with the numbered jumps. This is a
  deliberate change to today's free-roaming arrow behaviour, agreed in discussion.
- **Jump numbers live on the section headers, not the footer.** The nav-pane section
  headers render as `[1] MAIN` / `[2] B-TREES` / `[3] PAGES` (via a `sectionLabel` helper),
  which is where the design wireframes (§2, §4) already showed them. With the numbers inline
  on the headers, the verbose `1 main · 2 b-trees · 3 pages` footer tokens are redundant and
  were dropped — keeping the footer short. _Decided during implementation review._
- **`[`/`]` prev/next-page is removed.** Ticket 03's `[`/`]` filtered-paging binding is
  withdrawn: a user pages through a filtered set by jumping to `PAGES` (`3`) and using the
  section-confined arrows. The now-unreachable `openRelativePage` → `openPageNumber` →
  `stepWithin` chain and its test (`TestFilteredPagingClampsAtEnds`) were deleted with it.
  `filteredPages()` (still used by rendering) is untouched. _Decided during implementation
  review; this reverses part of Ticket 03._

---

## Scope

In scope:
1. **Keys (`handleKey`)** — add `1`/`2`/`3` select-only section jumps; remove `g`/`h`/`p`;
   remove `[`/`]`; make `esc` clear an active filter before its existing branches.
2. **Section-confined arrows** — make `moveSelection` (model.go:320) clamp `↑`/`↓` to the
   current section's bounds instead of the whole list.
3. **B-TREES jump helper** — `2` must land on the first `B-TREES` row, which is the first
   `navTable`, or the first `navIndex` when the database has no tables.
4. **Numbered section headers** — render headers as `[1] MAIN` / `[2] B-TREES` / `[3] PAGES`
   via a `sectionLabel` helper in `viewNavigation`.
5. **Footer hint strings** — update `navKeys` / `filterKeys` (model.go:487): drop the removed
   `g`/`h`/`p` and `[`/`]` hints; the section numbers live on the headers, not the footer.
6. **Dead-code removal** — delete the now-unreachable `openRelativePage` / `openPageNumber` /
   `stepWithin` chain and its test, freed up by removing `[`/`]`.
7. **Doc reconciliation** — update `design.md` §3 (and the wireframe footers in §4) so the
   key table matches what ships: numbered jumps in place of `g`/`h`, `esc` clears filter,
   arrows section-confined, `[`/`]` row removed.

Out of scope:
- Any change to filter state, nav rebuild, or rendering from Ticket 03 (`applyFilter`,
  `clearFilter`, `buildNavItems`, markers, footer token, `filteredPages` — all reused as-is).
- Opening behaviour for the numbered jumps (they are select-only by decision above).

---

## Building blocks already in place (Ticket 03 / earlier)

- `handleKey` (model.go:235) with the current `g`/`h`/`p`/`f`/`F`/`esc` cases — the surface
  this ticket edits.
- `selectFirstKind(kind navKind)` (model.go:332) — already does exactly the select-only jump
  `1`/`3` need (`navOverview` / `navPage`); reused directly.
- `navKind`: `navOverview | navDBHeader | navTable | navIndex | navPage` (model.go:23).
  "First MAIN row" = `navOverview`; "first PAGES row" = `navPage`.
- `sectionForNavItem(item)` (model.go:1180) returns `Main` / `B-Trees` / `Pages` (both
  `navTable` and `navIndex` map to `B-Trees`) — reused to detect section boundaries in
  `moveSelection`.
- `moveSelection(delta)` (model.go:320) — currently clamps to `[0, len-1]` across the whole
  flat list; this ticket makes it section-aware.
- `clearFilter()` / `isFiltered()` (Ticket 03) — `esc` reuses `clearFilter()`.
- `navKeys` / `filterKeys` always-on footer hint strings (model.go:487) — Ticket 03's note
  flagged these for update alongside the bindings.
- No test references `g`/`h`/`p` (confirmed by grep), so their removal breaks nothing.

---

## 1. Keys (`handleKey`, `internal/tui/model.go`)

Replace the `g` / `h` / `p` cases with `1` / `2` / `3`, delete the `[` / `]` cases, and
extend `esc`:

```go
case "1":
    m.selectFirstKind(navOverview) // first MAIN row; select-only
    return m, nil
case "2":
    m.selectFirstBTreeRow()        // first navTable, else first navIndex
    return m, nil
case "3":
    m.selectFirstKind(navPage)     // first PAGES row; select-only (was `p`)
    return m, nil
// `[` / `]` cases removed; f / F / enter / arrows unchanged
case "esc":
    if m.isFiltered() {            // NEW: clear filter first, then stop
        m.clearFilter()
        return m, nil
    }
    // existing behaviour, unchanged:
    if m.active.kind == navPage && m.focusedPane == explorerPane && m.explorerIndex > 0 {
        m.explorerIndex = 0
        m.inspectorScroll = 0
        m.status = "returned to page summary"
        return m, nil
    }
    m.active = contentTarget{kind: navOverview}
    m.currentPage = nil
    m.pageRows = nil
    m.inspectorScroll = 0
    m.status = "returned to overview"
    return m, nil
```

New helper (next to `selectFirstKind`):

```go
// selectFirstBTreeRow jumps to the first row of the merged B-TREES section: the first
// table, or the first index when the database has no tables.
func (m *model) selectFirstBTreeRow() {
    for idx, item := range m.navItems {
        if item.kind == navTable || item.kind == navIndex {
            m.selectedIndex = idx
            return
        }
    }
}
```

Notes:
- **Select-only, no focus change** — consistent with today's `p`. The jumps move
  `selectedIndex`; they do not call `openSelected()` and do not force `focusedPane` to nav
  (`design.md` §3 scopes them to "anywhere in nav"). `enter` opens the landed row.
- **No-op safety** — `selectFirstKind` / `selectFirstBTreeRow` leave `selectedIndex`
  unchanged when no matching row exists (e.g. `2` in a schema-less DB). Acceptable.

## 2. Section-confined arrows (`moveSelection`, model.go:320)

Confine `↑`/`↓` to the section the cursor is already in; `1`/`2`/`3` are the only way out:

```go
func (m *model) moveSelection(delta int) {
    next := m.selectedIndex + delta
    if next < 0 || next >= len(m.navItems) {
        return
    }
    // Arrows stay within the current section; 1/2/3 cross sections.
    if sectionForNavItem(m.navItems[next]) != sectionForNavItem(m.navItems[m.selectedIndex]) {
        return
    }
    m.selectedIndex = next
    m.inspectorScroll = 0
}
```

Notes:
- A jump (`1`/`2`/`3`) followed by `↑`/`↓` then roams freely **within** the landed section.
- The change is local to `moveSelection`; `selectFirstKind` / `selectFirstBTreeRow` set
  `selectedIndex` directly and are unaffected. No test currently asserts cross-section arrow
  movement (confirmed by grep), so nothing regresses.

## 3. Footer hint strings (model.go:487) & section-header labels

Rather than spelling the section jumps out in the footer, the nav-pane **section headers**
advertise them inline — `[1] MAIN` / `[2] B-TREES` / `[3] PAGES` — via a `sectionLabel`
helper used by `viewNavigation`. The footer therefore drops both the verbose
`1 main · 2 b-trees · 3 pages` tokens and the `[ ] page` tokens (the `[`/`]` prev/next-page
binding is removed — see §1):

```go
navKeys    = "tab focus · ↑↓ move · enter open · f filter · q quit"
filterKeys = "F clear · tab focus · enter open · q quit"
```

Drop the old `g overview · h header` hints too. The strings stay one line wide (the filter
token already shares the filtered line — see Ticket 03's footer rework).

The header label lives in a small helper next to `sectionForNavItem`, called from
`viewNavigation` where headers were previously rendered with `strings.ToUpper(row.section)`:

```go
// sectionLabel renders a section header with its jump-key number prefix (e.g. "[1] MAIN").
// Sections without a jump key render bare.
func sectionLabel(section string) string {
    num := map[string]string{"Main": "1", "B-Trees": "2", "Pages": "3"}[section]
    if num == "" {
        return strings.ToUpper(section)
    }
    return "[" + num + "] " + strings.ToUpper(section)
}
```

## 4. Doc reconciliation (`design.md`)

- §3 key table: replace the `g` / `h` rows with `1` / `2` / `3` (select-only), and change the
  `esc` row to "Clear the filter (when filtered); else return to page summary / overview".
  Remove the trailing sentence claiming `g`/`h` are preserved.
- §4 wireframe footers: strip the `1 main · 2 b-trees · 3 pages` and `[ ] page` tokens to
  match the new hint strings (the numbers now live on the `[1] MAIN` / `[2] B-TREES` /
  `[3] PAGES` section headers, and `[`/`]` is removed).
- §3: add a note that `↑`/`↓` are confined to the current section and that `1`/`2`/`3` are
  the only way to move between sections; drop the `[`/`]` prev/next-page row.

---

## Acceptance criteria

**Keys**
- [x] `1` selects the first `MAIN` row (Overview) without opening it; focus unchanged.
- [x] `2` selects the first `B-TREES` row (first table, or first index when there are no
      tables) without opening it.
- [x] `3` selects the first `PAGES` row without opening it (replaces `p`).
- [x] `g` / `h` / `p` are removed and are now no-ops.
- [x] `[` / `]` are removed and are now no-ops.
- [x] `esc` while filtered clears the filter and does nothing else.
- [x] `esc` while unfiltered keeps today's behaviour: page-summary return inside an open
      page, else reset to Overview.
- [x] The numbered jumps are no-ops (no panic, `selectedIndex` unchanged) when the target
      section is empty.

**Arrows**
- [x] `↓` on the last row of a section stays put (does not enter the next section); `↑` on
      the first row of a section stays put.
- [x] `↑`/`↓` move freely between rows **within** a section (incl. across tables↔indexes
      inside B-TREES).
- [x] Crossing sections is possible only via `1`/`2`/`3`.

**Headers & footer**
- [x] Nav-pane section headers render as `[1] MAIN` / `[2] B-TREES` / `[3] PAGES`.
- [x] `navKeys` / `filterKeys` no longer mention `g` / `h` / `p`, `[` / `]`, or the verbose
      `1 main · 2 b-trees · 3 pages` tokens.

**Docs**
- [x] `design.md` §3 (and the §4 footers) match the shipped bindings.

**General**
- [x] `go build ./...` and `go vet ./...` clean; full `go test ./...` green.

---

## Testing

Pure `handleKey` / render behaviour, unit-tested in `internal/tui/keys_test.go` via `Update`
+ `keyMsg` (helpers reused from `filter_test.go`). Shipped cases:

- **`TestSectionJumpsSelectOnly`** — from an arbitrary selection, `1` lands on the Overview
  row, `2` on the first B-TREES row, `3` on the first page row; `m.active` is unchanged each
  time (proves select-only, no open).
- **`TestJumpBTreesFallsBackToIndex`** — with a view model that has indexes but no tables, `2`
  lands on the first `navIndex` row.
- **`TestRemovedLetterKeysAreNoOps`** — `g` / `h` / `p` leave `selectedIndex` and `m.active`
  unchanged.
- **`TestEscClearsFilterFirst`** — with a filter active, `esc` clears it (`isFiltered()`
  false afterwards) and does not reset `m.active` to overview.
- **`TestEscUnfilteredKeepsExistingBehaviour`** — unfiltered, `esc` inside an open page with
  `explorerIndex > 0` returns to the page summary; from elsewhere it resets to Overview.
- **`TestArrowsConfinedToSection`** — on the last MAIN row, `↓` does not advance into
  B-TREES; on the first B-TREES row, `↑` does not return to MAIN; `↓` within B-TREES stays in
  the section and advances.
- **`TestSectionHeadersShowJumpNumbers`** — `View()` contains `[1] MAIN` / `[2] B-TREES` /
  `[3] PAGES`, and the footer no longer carries the verbose section tokens or `g`/`h` hints.

The Ticket 03 test `TestFilteredPagingClampsAtEnds` was **removed** alongside the `[`/`]`
binding it exercised.
