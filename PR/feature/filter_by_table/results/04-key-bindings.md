# Result — Ticket 04: Section-jump keys (`1`/`2`/`3`), section-confined arrows, `esc`-clear

> Feature: **Filter Pages by Table or Index** (`feature/filter_by_table`)
> Ticket: [tickets/04-key-bindings.md](../tickets/04-key-bindings.md) · Status: **✅ Done**
> Commit: `53f0986` — *Implement the ticket*
> Depends on: [Ticket 03](03-filter-state.md) (merged `B-TREES` nav, `f`/`F`, `filteredPages`) — ✅ done (`93c761b`)

---

## Summary

Wired the deferred **lazygit-style section jumps** and finished the key map on top of
Ticket 03's nav structure. `1` / `2` / `3` jump the cursor to the first row of `MAIN` /
`B-TREES` / `PAGES` (select-only — they move the cursor, `enter` opens), and `↑` / `↓` are
now **confined to the current section** so the numbered jumps are the only way to cross
between sections. `esc` gained a clear-filter meaning that runs before its existing
page-summary / overview behaviour.

Two decisions taken **during implementation review** shaped the final result beyond the
original draft:

1. The jump numbers were put **on the section headers** (`[1] MAIN` / `[2] B-TREES` /
   `[3] PAGES`) instead of being spelled out in the footer — this is where `design.md` §2/§4
   already showed them, and it keeps the footer short.
2. The `[` / `]` prev/next-page binding shipped in Ticket 03 was **removed**: a filtered set
   is now paged by jumping to `PAGES` (`3`) and using the arrows. The now-unreachable
   `openRelativePage` → `openPageNumber` → `stepWithin` chain and its test were deleted.

All work stayed in `internal/tui`; no `sqlite` data-layer or filter-state changes.

---

## What was delivered

| File | Δ | Purpose |
| --- | --- | --- |
| `internal/tui/model.go` | +41/−76 | `1`/`2`/`3` + `esc`-clear keys, removed `g`/`h`/`p` + `[`/`]`, section-confined `moveSelection`, `selectFirstBTreeRow`, `sectionLabel` headers, trimmed footer strings, deleted dead paging chain |
| `internal/tui/keys_test.go` | +220 (new) | 7 unit tests for the jumps, arrows, `esc`, removed keys, and header labels |
| `internal/tui/filter_test.go` | −30 | removed `TestFilteredPagingClampsAtEnds` (exercised the deleted `[`/`]` paging) |

Net `model.go` shrank (−35 lines): the removed `[`/`]` paging chain outweighs the added
jump/label code.

### Keys (`handleKey`)

- **`1` / `2` / `3`** — select-only section jumps. `1` → `selectFirstKind(navOverview)`,
  `3` → `selectFirstKind(navPage)`, `2` → new `selectFirstBTreeRow()` (first `navTable`, or
  first `navIndex` when the DB has no tables). None call `openSelected` and none touch
  `focusedPane`; a jump into an empty section is a safe no-op (`selectedIndex` unchanged).
- **`g` / `h` / `p`** — removed (now no-ops). They were redundant with the numbered scheme.
- **`[` / `]`** — removed (now no-ops); see _Filtered paging_ below.
- **`esc`** — gains a leading `if m.isFiltered() { m.clearFilter(); return }` guard, then
  falls through to its unchanged behaviour: inside an open page with `explorerIndex > 0`
  return to the page summary, else reset to Overview.

### Arrows (`moveSelection`)

`moveSelection` no longer clamps across the whole flat list; it returns early when the next
row is in a different section (compared via `sectionForNavItem`). So `↑` / `↓` stop at the
first / last row of the current section and roam freely **within** it (including across the
tables↔indexes boundary inside `B-TREES`); `1` / `2` / `3` are the only way to cross.

### Headers & footer

- **Section headers** render via a new `sectionLabel(section)` helper (next to
  `sectionForNavItem`), called from `viewNavigation` where the header was previously
  `strings.ToUpper(row.section)`. It prefixes the jump number: `[1] MAIN` / `[2] B-TREES` /
  `[3] PAGES`; sections without a jump key render bare.
- **Footer hint strings** dropped the `g overview · h header` hints, the `[ ] page` hint, and
  the verbose `1 main · 2 b-trees · 3 pages` tokens (now redundant with the headers):

```go
navKeys    = "tab focus · ↑↓ move · enter open · f filter · q quit"
filterKeys = "F clear · tab focus · enter open · q quit"
```

### Filtered paging removed

`[` / `]` stepped the page set (clamped to the filtered set when filtered). It was withdrawn
in favour of "jump to `PAGES`, use arrows", which the section-confined arrows make clean.
The dead chain `openRelativePage` / `openPageNumber` / `stepWithin` was deleted; `filteredPages()`
(still used to build the `PAGES` rows and render the summary) is untouched. This reverses the
`[`/`]` part of Ticket 03 — flagged in the ticket's _Decisions_ section.

---

## Doc reconciliation (`design.md`)

- **§2** section table already showed `[1] MAIN` / `[2] B-TREES` / `[3] PAGES`; the
  implementation now matches it.
- **§3** key table: dropped the `g` / `h` rows (already done in the draft) and the
  `[` / `]` row; `esc` row reads "Clear the filter (when filtered); else return to page
  summary / overview"; added notes that the jumps are select-only + advertised on the
  headers, that arrows are section-confined, and that there is no prev/next-page binding.
- **§4** wireframe footers: stripped the `1 main · 2 b-trees · 3 pages` and `[ ] page`
  tokens to match the shipped `navKeys` / `filterKeys`.

---

## Acceptance criteria

All boxes in the ticket are checked. Highlights:

- [x] `1` / `2` / `3` select the first `MAIN` / `B-TREES` / `PAGES` row without opening it;
      `2` falls back to the first index when there are no tables; empty-section jumps are no-ops.
- [x] `g` / `h` / `p` and `[` / `]` removed (now no-ops).
- [x] `esc` clears an active filter and stops; unfiltered it keeps the page-summary / overview
      behaviour.
- [x] `↑` / `↓` confined to the current section; crossing sections is only via `1` / `2` / `3`.
- [x] Section headers render `[1] MAIN` / `[2] B-TREES` / `[3] PAGES`; footer no longer
      mentions `g` / `h` / `p`, `[` / `]`, or the verbose section tokens.
- [x] `design.md` §3 / §4 match the shipped bindings.
- [x] `go build ./...`, `go vet ./...` clean; full `go test ./...` green.

---

## Tests

7 new test functions in `internal/tui/keys_test.go` (all passing), reusing the Ticket 03
helpers `indexAll`, `objectByName`, and `keyMsg`. Special keys (`esc`, `↑`, `↓`) are sent as
`tea.KeyMsg{Type: tea.KeyEsc / KeyUp / KeyDown}`.

| Test | Covers |
| --- | --- |
| `TestSectionJumpsSelectOnly` | `1`/`2`/`3` land on the first MAIN / B-TREES / PAGES row; `m.active` unchanged (no open) |
| `TestJumpBTreesFallsBackToIndex` | with tables dropped, `2` lands on the first `navIndex` row |
| `TestRemovedLetterKeysAreNoOps` | `g` / `h` / `p` leave `selectedIndex` and `m.active` unchanged |
| `TestEscClearsFilterFirst` | `esc` while filtered clears the filter and does not reset to overview |
| `TestEscUnfilteredKeepsExistingBehaviour` | `esc` returns to page summary inside an open page; resets to Overview elsewhere |
| `TestArrowsConfinedToSection` | `↓` on last MAIN / `↑` on first B-TREES row stay put; `↓` within B-TREES advances inside the section |
| `TestSectionHeadersShowJumpNumbers` | `View()` shows `[1] MAIN` / `[2] B-TREES` / `[3] PAGES`; footer drops the verbose tokens and `g`/`h` hints |

The Ticket 03 test `TestFilteredPagingClampsAtEnds` was removed with the `[`/`]` binding it
exercised. The remaining Ticket 03 filter suite and the `sqlite` package are untouched and
still green.

---

## Notes / follow-ups

- Removing `[` / `]` reverses a piece of shipped Ticket 03 behaviour. It is intentional and
  recorded in the ticket; if prev/next-page is wanted back later, `filteredPages()` still
  provides the ordered set the old `stepWithin` walked.
- The feature (Tickets 01–04) is complete: background b-tree indexing, merged `B-TREES` nav,
  `f`/`F` filtering, and the full lazygit-style key map.
