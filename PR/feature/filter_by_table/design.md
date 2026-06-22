# Design: Filter Pages by Table or Index

> Branch: `feature/filter_by_table`
> Status: **Design agreed.** No implementation yet.
> Companion docs: `context.md`, `codebase-map.md`, `feature-notes.md`.

This document specifies the UX and the supporting data-layer capability for filtering
the page list down to the pages belonging to a single table or index b-tree.

---

## 1. Summary

Badger lists every page `1..PageCount` in a flat `PAGES` section. This feature lets the
user pick one schema object (a table **or** an index) and scope the `PAGES` list to **only
the pages of that object's b-tree**.

The filter is a **persistent mode**: once applied it stays active across all navigation
until explicitly cleared. Only one b-tree can be filtered at a time.

### Confirmed product decisions
- **Scope = b-tree only.** The pages of the selected object's own b-tree (interior + leaf,
  reachable from its root page). Overflow pages are **not** included. A table's indexes are
  **not** pulled in — an index is filtered on its own.
- **One filter at a time.** Tables and indexes share a single `B-TREES` navigation section,
  so the user selects exactly one object as the filter source.
- **Persistent mode.** Filter state lives in the footer status bar and survives navigation
  until cleared.

---

## 2. Navigation model

The left navigation pane has three numbered sections (lazygit-style jump targets):

| Section       | Contents                                                        |
|---------------|-----------------------------------------------------------------|
| `[1] MAIN`    | Overview, DB Header                                             |
| `[2] B-TREES` | All tables and indexes, merged. `▦` = table, `◈` = index.       |
| `[3] PAGES`   | Page list. Full `1..PageCount` when unfiltered; the selected b-tree's pages when filtered. |

Notes:
- The `B-TREES` section shows **icon + name only**. The root page is intentionally *not*
  shown here (it was visual noise); it appears in the middle detail pane (`root page N`)
  and the right summary (`Root: page N`).
- The icon (`▦` / `◈`) also echoes into the middle detail pane title, the page title when a
  page is open, and the footer filter token.

### Row markers
- **`>` (hollow)** — the cursor / current selection.
- **`▶` (solid)** — the active filter source row.
- When the cursor sits on the filter source, the two merge into a single solid `▶`
  (no double markers anywhere).

---

## 3. Key bindings

| Key        | Context                          | Action                                              |
|------------|----------------------------------|-----------------------------------------------------|
| `1`        | anywhere in nav                  | Jump selection to first item of `MAIN`              |
| `2`        | anywhere in nav                  | Jump selection to first item of `B-TREES`           |
| `3`        | anywhere in nav                  | Jump selection to first item of `PAGES`             |
| `f`        | a table/index row selected       | Apply filter: scope `PAGES` to that b-tree          |
| `F`        | filter active                    | Clear the filter                                    |
| `esc`      | filter active                    | Clear the filter (same as `F`)                      |
| `enter`    | any nav row                      | Open the selected object/page in the explorer       |
| `tab`      | anywhere                         | Cycle pane focus (nav / explorer / inspector)       |
| `↑ / ↓`    | nav                              | Move selection                                      |
| `[` / `]`  | a page open                      | Previous / next page (within the active filter set) |
| `g`        | anywhere                         | Go to Overview                                      |
| `h`        | anywhere                         | Go to DB Header                                     |
| `q`        | anywhere                         | Quit                                                |

Existing bindings are preserved. New bindings: `1` `2` `3` (section jumps), `f` (apply
filter), `F` (clear filter). `esc` gains a clear-filter meaning while a filter is active.

Interaction detail: while a filter is active, `[` / `]` page navigation steps through the
**filtered** page set, not `1..PageCount`.

---

## 4. Screens / states

Layout is the existing three-pane shell: **Navigation | Explorer (detail) | Summary**,
with a footer status bar.

### 4.1 Filter OFF — table selected

```
┌─ Navigation ───────────┬─ companies ──────────────────────┬─ Selected: companies ──────┐
│ [1] MAIN                │ ▦ TABLE  companies                │ SUMMARY                     │
│   Overview              │ root page 2                       │ Type:     table             │
│   DB Header             │ Columns: 7                        │ Root:     page 2            │
│                         │ CREATE TABLE companies (          │ Pages:    — (unfiltered)    │
│ [2] B-TREES             │   id INTEGER PRIMARY KEY,         │                             │
│ > ▦ companies           │   name TEXT, country TEXT, ...    │ ACTIONS                     │
│   ▦ sqlite_sequence     │ )                                 │ - press f to filter pages   │
│   ◈ idx_companies_…     │                                   │   to this b-tree            │
│                         │ ┌─────────────────────────────┐  │ - enter  open object        │
│ [3] PAGES               │ │ Press  f  to filter pages   │  │                             │
│   page 1                │ │ to the companies b-tree     │  │                             │
│   page 2 … page 1910    │ └─────────────────────────────┘  │                             │
└─────────────────────────┴───────────────────────────────────┴─────────────────────────────┘
 1 main · 2 b-trees · 3 pages | tab focus | enter open | f filter b-tree | g overview | q quit
```

### 4.2 Filter ON by table — cursor on source row (solid `▶`)

```
┌─ Navigation ───────────┬─ companies ──────────────────────┬─ Selected: companies ──────┐
│ [1] MAIN                │ ▦ TABLE  companies                │ SUMMARY                     │
│   Overview              │ root page 2                       │ Type:     table             │
│   DB Header             │ Columns: 7                        │ Root:     page 2            │
│                         │ CREATE TABLE companies (          │ Pages:    1842 (filtered)   │
│ [2] B-TREES             │   id INTEGER PRIMARY KEY,         │                             │
│ ▶ ▦ companies           │   name TEXT, ...                  │ ACTIONS                     │
│   ▦ sqlite_sequence     │ )                                 │ - F / esc  clear filter     │
│   ◈ idx_companies_…     │                                   │ - enter    open object      │
│                         │                                   │                             │
│ [3] PAGES               │                                   │                             │
│   page 2                │                                   │                             │
│   page 9 … (1840 more)  │                                   │                             │
└─────────────────────────┴───────────────────────────────────┴─────────────────────────────┘
 ⦿ filtered: ▦ companies (1842 pg) | F clear | 1 main · 2 b-trees · 3 pages | q quit
```

### 4.3 Filter ON by table — after `3` jumps to PAGES

Cursor `>` moves to a page row; the source row keeps the solid `▶`.

```
┌─ Navigation ───────────┬─ page 9 ─────────────────────────┬─ Selected: page 9 ─────────┐
│ [1] MAIN                │ ▦ PAGE 9  leaf table b-tree       │ SUMMARY                     │
│   Overview              │ part of: companies                │ Page kind: leaf table       │
│   DB Header             │                                   │ Cells:     31               │
│                         │ Page Header        offset 0..8    │ Right ptr: —                │
│ [2] B-TREES             │ Cell Pointers      offset 8..70   │                             │
│ ▶ ▦ companies           │ Leaf Cell #0   rowid 1            │ ACTIONS                     │
│   ▦ sqlite_sequence     │ Leaf Cell #1   rowid 2            │ - F / esc  clear filter     │
│   ◈ idx_companies_…     │ ...                               │ - enter    open page        │
│                         │                                   │                             │
│ [3] PAGES               │                                   │                             │
│   page 2                │                                   │                             │
│ > page 9                │                                   │                             │
│   page 17 … (1839 more) │                                   │                             │
└─────────────────────────┴───────────────────────────────────┴─────────────────────────────┘
 ⦿ filtered: ▦ companies (1842 pg) | F clear | 1 main · 2 b-trees · 3 pages | [ ] page | q quit
```

### 4.4 Filter ON by index — cursor on source row (solid `▶`)

```
┌─ Navigation ───────────┬─ idx_companies_country ──────────┬─ Selected: idx_companies_… ┐
│ [1] MAIN                │ ◈ INDEX  idx_companies_country    │ SUMMARY                     │
│   Overview              │ root page 4                       │ Type:     index             │
│   DB Header             │ on table: companies               │ Root:     page 4            │
│                         │ CREATE INDEX idx_companies_country│ Pages:    68 (filtered)     │
│ [2] B-TREES             │   ON companies(country)           │                             │
│   ▦ companies           │                                   │ ACTIONS                     │
│   ▦ sqlite_sequence     │                                   │ - F / esc  clear filter     │
│ ▶ ◈ idx_companies_…     │                                   │ - enter    open object      │
│                         │                                   │                             │
│ [3] PAGES               │                                   │                             │
│   page 4                │                                   │                             │
│   page 12 … (66 more)   │                                   │                             │
└─────────────────────────┴───────────────────────────────────┴─────────────────────────────┘
 ⦿ filtered: ◈ idx_companies_country (68 pg) | F clear | 1 main · 2 b-trees · 3 pages | q quit
```

### 4.5 Walk in progress (async, large b-tree)

Applying a filter reads many pages, so the walk is async. A transient state shows progress
before `PAGES` repopulates:

```
│ [3] PAGES               │   ⟳ filtering companies… walked 612 / ~1900 pages            │
```

Footer during the walk:

```
 ⟳ filtering ▦ companies… | esc cancel | 1 main · 2 b-trees · 3 pages | q quit
```

### 4.6 Degraded walk (unreadable child page)

If a child page fails to parse, the walk skips it and surfaces the fact instead of crashing
the workspace. The `PAGES` list still populates with what was reachable.

```
 ⦿ filtered: ▦ companies (1841 pg · 1 skipped) | ⚠ page 774 unreadable | F clear | q quit
```

---

## 5. User flows

### Flow A — Apply a filter
1. User opens nav (or presses `2` to jump to `B-TREES`).
2. User moves to a table or index row.
3. User presses `f`.
4. The b-tree walk runs async (4.5). On completion:
   - `PAGES` is scoped to the walked page set.
   - The source row shows the solid `▶`.
   - The footer shows `⦿ filtered: <icon> <name> (<n> pg) | F clear`.
   - The middle/summary panes show `Pages: <n> (filtered)`.

### Flow B — Browse filtered pages
1. With a filter active, user presses `3` to jump to `PAGES` (or arrows down to it).
2. Cursor `>` lands on the first filtered page; the source row keeps `▶`.
3. `enter` opens a page; `[` / `]` step through the filtered set only.

### Flow C — Switch the filter to a different object
1. User presses `2` (or navigates) to `B-TREES`.
2. User selects a different table/index and presses `f`.
3. The previous filter is replaced (single-filter rule); a new walk runs; the solid `▶`
   moves to the new source row.

### Flow D — Clear the filter
1. From anywhere, user presses `F` (or `esc` while a filter is active).
2. `PAGES` returns to the full `1..PageCount` list.
3. The footer filter token disappears; the `▶` marker reverts to a plain cursor.

### Flow E — Cancel an in-progress walk
1. During the async walk (4.5), user presses `esc`.
2. The walk is abandoned; the prior page list (filtered or full) is restored.

---

## 6. Supporting data-layer capability

The only genuinely new capability is **rootpage → set-of-pages traversal**. It composes
from primitives that already exist (`InspectPage`, interior `LeftChildPage`,
`PageHeader.RightMostPointer`).

- **Home:** a new method on `sqlite.Inspector` (data layer), e.g.
  `PagesForRoot(root uint32) ([]uint32, error)` returning sorted page numbers. Keeping the
  walk in the parser layer matches the project's design-doc guidance.
- **Walk:** BFS/DFS from `root`. At interior pages, follow every interior cell's
  `LeftChildPage` plus the header's `RightMostPointer`. Leaf pages are terminal.
- **Cycle safety:** a `visited` set guards against malformed/cyclic child pointers
  (mirrors the existing `parseFreeblocks` cycle guard).
- **Degrade, don't crash:** a child that fails to parse is skipped and reported (count +
  page number), consistent with the "stay navigable on partial parse" requirement (4.6).
- **Lazy + cached:** walk on first filter-apply; memoize per root page
  (`map[rootpage][]uint32`). The DB is read-only, so cached results are stable.
- **Async:** invoked through the existing async command pattern (like `loadPageCmd`) so the
  UI stays responsive on large trees, emitting a completion message that repopulates `PAGES`.

### Explicitly out of scope (per product decisions)
- Overflow-page chains (no overflow walking).
- Pulling a table's indexes into its filter (indexes are filtered independently).

`PagesForRoot` is the natural extension point if either is wanted later (e.g. an
`includeOverflow` flag or a sibling method).

---

## 7. Model state (conceptual)

The TUI model gains a single filter concept (one active filter at most):

- whether a filter is active,
- the source object (icon/type + name + root page),
- the cached filtered page numbers,
- any skipped-page diagnostics from the walk.

`buildNavItems` becomes filter-aware: the `PAGES` section iterates the filtered page set
when a filter is active, otherwise the full `1..PageCount` range. The `MAIN` and `B-TREES`
sections are unaffected, so the user can always pick a different object to re-filter.

---

## 8. Open / deferred items
- Exact glyphs (`▦` / `◈`) are placeholders; confirm they render well in target terminals.
  Fallback: `[T]` / `[I]`.
- Progress granularity for the async walk (4.5) — exact vs approximate page count.
- Whether to persist the last-used filter across app restarts (currently: no).
