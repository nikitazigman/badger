# Result — Ticket 01: B-tree traversal (`rootpage → set of pages`)

> Feature: **Filter Pages by Table or Index** (`feature/filter_by_table`)
> Ticket: [tickets/01-btree-traversal.md](../tickets/01-btree-traversal.md) · Status: **✅ Done**
> Commit: `84f69bc` — *Add btree walker*

---

## Summary

Implemented the data-layer primitive the whole feature rests on: given a b-tree
**root page**, walk the tree and return every page number that makes up that b-tree
(interior + leaf nodes reachable from the root). The walk is a pure, read-only,
stateless operation in the `sqlite` package — it touches no TUI, model state, or
filtering UI (those land in later tickets).

This introduces the first page → owner mapping capability in the codebase
(`feature-notes.md` §"What is missing", item 1), built entirely from existing
primitives (`InspectPage`, interior-cell `LeftChildPage`, `RightMostPointer`) so no
new low-level byte parsing was needed.

---

## What was delivered

| File | Lines | Purpose |
| --- | --- | --- |
| `internal/sqlite/btree_walk.go` | 91 | `PageWalk`, `SkippedPage`, `PagesForRoot`, recursive `walkBTree` |
| `internal/sqlite/btree_walk_test.go` | 356 | 7 test functions + synthetic-DB fixture helpers |

### API (as shipped)

```go
type PageWalk struct {
    Root    uint32        // the root page the walk started from
    Pages   []uint32      // sorted, unique; includes Root; only pages actually reached
    Skipped []SkippedPage // child pages that could not be parsed (degraded walk)
}

type SkippedPage struct {
    Page   uint32 // the unreadable child page
    Parent uint32 // the interior page that pointed to it
    Reason string // human-readable parse error
}

func (i *Inspector) PagesForRoot(root uint32) (PageWalk, error)
```

This matches the ticket's proposed API exactly, including the richer `PageWalk`
return (over the bare `[]uint32` sketched in `design.md` §6) so that skipped pages
can be surfaced per `design.md` §4.6.

---

## Algorithm (as implemented)

The ticket sketched an iterative queue; after review the implementation uses
**recursive depth-first descent**, which is materially simpler and equally correct
(visit order is irrelevant — results are sorted at the end).

1. `root == 0` → return an empty `PageWalk`, no error.
2. Inspect the **root** once in `PagesForRoot`; a failure here is the **hard error**.
3. Seed a `visited` set with the root, then recurse via `walkBTree`:
   - Record the current page.
   - If it's a leaf → terminal, return.
   - If interior → collect child pointers (`TableInteriorCells[].LeftChildPage`,
     `IndexInteriorCells[].LeftChildPage`, and `RightMostPointer` if non-nil) and
     recurse into each unvisited, non-zero child.
   - A **child** that fails to parse is appended to `Skipped` (page + parent +
     reason) and the walk continues — degrade, don't crash.
4. Sort `Pages` ascending (uniqueness already guaranteed by `visited`).

**Design notes**
- *Root vs child error split* falls out naturally: the root is inspected in
  `PagesForRoot` (hard failure) and children inside `walkBTree` (soft skip).
- *Cycle / double-count guard*: `visited[child]` is marked **before** inspecting the
  child, so self-referential or shared pointers are descended exactly once, and a bad
  child is recorded only once even if two parents point at it.
- *Recursion depth* = tree height. Real SQLite b-trees are very shallow (huge
  interior fan-out → ~3–5 levels even for large tables), so native stack depth is a
  non-issue for well-formed files. This was a deliberate trade for simpler code over
  the iterative-stack approach; documented here as a known assumption.
- *Out of scope, intentionally*: overflow-page chains are **not** followed, and a
  table's indexes are not pulled in (an index is its own b-tree, walked separately).

---

## Acceptance criteria

- [x] `PagesForRoot` exists on `*Inspector` in `internal/sqlite/btree_walk.go` with the specified signature.
- [x] Single-page (leaf-only) table returns exactly `[root]`.
- [x] Multi-level table returns every reachable page, sorted and unique, including root, interior, and all leaves under both interior cells and the right-most pointer.
- [x] An index root returns the index's own b-tree pages only (no table pages).
- [x] A cyclic/self-referential child pointer terminates and the page is counted once.
- [x] A child page that fails to parse is recorded in `Skipped`; the walk still returns reachable pages — no panic, no returned error.
- [x] A hard-failing root (unreadable / out of range) returns a non-nil error.
- [x] `root == 0` returns an empty `PageWalk` with no error.
- [x] No changes outside `internal/sqlite` (no TUI, no model).

---

## Tests

7 test functions, all passing; `go vet ./internal/sqlite/` clean.

| Test | Covers |
| --- | --- |
| `TestPagesForRootRootZero` | `root == 0` → empty walk, no error |
| `TestPagesForRootHardFailingRoot` | out-of-range root → non-nil error |
| `TestPagesForRootSinglePageTable` | leaf-only tables (`sample.db`) → `[root]` |
| `TestPagesForRootAcrossFixtures` | sweep of `sample`/`companies`/`superheroes`: non-empty, sorted, unique, root-inclusive, within `1..PageCount`, no page claimed by two roots |
| `TestPagesForRootMultiLevel` | exact page membership for an interior root (`superheroes.db`) |
| `TestPagesForRootCycleGuard` | self-referential pointer terminates, counted once (synthetic DB) |
| `TestPagesForRootSkipsBadChild` | out-of-range child → recorded in `Skipped`, walk continues (synthetic DB) |

**Coverage verification.** A depth probe over the fixtures confirmed
`companies.db` contains **depth-3** b-trees (interior → interior → leaves) for both
the `companies` table and the `idx_companies_country` index, and `superheroes.db` is
depth-2. So `TestPagesForRootAcrossFixtures` genuinely exercises multi-level
recursion, not just a single hop.

The cycle-guard and bad-child cases use hand-built in-memory SQLite files
(`writeSyntheticDB` + `interiorTablePage` helpers) since the repo fixtures can't
naturally produce a cyclic or unreadable child pointer.

---

## Notes for the next ticket

`PagesForRoot` is intentionally **pure, stateless, and lock-free** — no internal
cache or goroutines. Ticket 02 can safely fan these walks out across all roots at
launch (one Bubble Tea command per root via `tea.Batch`) and accumulate the
`root → PageWalk` index in the model, since `os.File.ReadAt` is safe for concurrent
use and `dbHeader` is read-only after `Open`.
