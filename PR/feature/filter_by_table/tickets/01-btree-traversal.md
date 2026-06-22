# Ticket 01 — B-tree traversal: `rootpage → set of pages`

> Feature: **Filter Pages by Table or Index** (`feature/filter_by_table`)
> Status: **Ready to implement.**
> Context: [context.md](../context.md) · [codebase-map.md](../codebase-map.md) · [feature-notes.md](../feature-notes.md) · [design.md](../design.md)

---

## Summary

Add the single new data-layer capability the whole feature rests on: given a b-tree
**root page**, return the set of page numbers that make up that b-tree (interior + leaf
nodes reachable from the root). This is a pure, read-only traversal in the `sqlite`
package. It does **not** touch the TUI, model state, or any filtering UI — those land in
later tickets.

Today nothing in the codebase maps a page back to the object that owns it
(`feature-notes.md` §"What is missing", item 1). This ticket introduces that traversal as
a standalone primitive so it can be unit-tested in isolation and consumed by the
background-indexing layer next.

---

## Scope

In scope:
- A new traversal that walks one b-tree from its root and collects every page in it.
- Cycle protection against malformed/cyclic child pointers.
- Graceful degradation: a child page that fails to parse is **skipped and reported**, not
  fatal (`design.md` §4.6, `feature-notes.md` §"Robustness").
- Unit tests against the repo fixtures.

Out of scope (covered by later tickets / explicitly cut):
- Overflow-page chains — **not** walked (`design.md` §6 "Explicitly out of scope").
- Pulling a table's indexes into its result — an index is its own b-tree, walked separately.
- Any caching, goroutines, fan-out, or model wiring — that's Ticket 02.
- Any UI, key binding, or rendering change.

---

## Building blocks already in place

From `codebase-map.md` / verified in source:
- `Inspector.InspectPage(n uint32) (*PageInspection, error)` — fully parses one page.
- `PageInspection.BTreePage.PageHeader`:
  - `PageKind.Value` (`InteriorTableBTreePage` `0x05`, `InteriorIndexBTreePage` `0x02`,
    `LeafTableBTreePage` `0x0d`, `LeafIndexBTreePage` `0x0a`)
  - `IsInterior() bool`
  - `RightMostPointer *Uint32Field` — non-nil only on interior pages
- `BTreePage.TableInteriorCells[].LeftChildPage` and
  `BTreePage.IndexInteriorCells[].LeftChildPage` — both `Uint32Field` (`.Value`).

So the walk composes entirely from existing primitives; no new low-level byte parsing is
required.

---

## Proposed API

New file: `internal/sqlite/btree_walk.go`.

```go
// PageWalk is the result of walking one b-tree from its root.
// Serializable by design (plain integers) so it can later be persisted to disk.
type PageWalk struct {
    Root    uint32         // the root page the walk started from
    Pages   []uint32       // sorted, unique; includes Root; only pages actually reached
    Skipped []SkippedPage  // child pages that could not be parsed (degraded walk)
}

// SkippedPage records a child pointer that failed to parse during the walk.
type SkippedPage struct {
    Page   uint32 // the unreadable child page
    Parent uint32 // the interior page that pointed to it
    Reason string // human-readable parse error
}

// PagesForRoot walks the b-tree rooted at `root` and returns every page in it.
//
//   - Per-child parse failures are recorded in PageWalk.Skipped and the walk continues
//     (degrade, don't crash).
//   - An error is returned ONLY for a hard failure: an invalid root, or the root page
//     itself being unreadable.
//   - root == 0 (e.g. virtual tables with no b-tree) returns an empty PageWalk and no error.
func (i *Inspector) PagesForRoot(root uint32) (PageWalk, error)
```

> Note: this refines the `PagesForRoot(root) ([]uint32, error)` signature sketched in
> `design.md` §6. The richer `PageWalk` return is needed because §4.6 requires the skipped
> pages to be surfaced (count + page number), which a bare `[]uint32` can't carry.

---

## Algorithm

1. If `root == 0`, return `PageWalk{Root: 0}` (empty), no error.
2. Maintain a `visited map[uint32]bool` (cycle guard, mirrors the `parseFreeblocks`
   guard noted in `feature-notes.md`) and a work queue/stack seeded with `root`.
3. For each page number pulled from the queue:
   - Skip if already in `visited`; otherwise mark visited.
   - `InspectPage(page)`:
     - If this is the **root** and it fails → return the error (hard failure).
     - If a **non-root child** fails → append to `Skipped` (with parent + reason) and continue.
   - Add the page to the result set.
   - If `IsInterior()`:
     - enqueue every `TableInteriorCells[].LeftChildPage.Value` and
       `IndexInteriorCells[].LeftChildPage.Value`,
     - enqueue `RightMostPointer.Value` if non-nil.
   - Leaf pages are terminal.
4. Sort `Pages` ascending and dedupe (the `visited` set already guarantees uniqueness).
5. Return the `PageWalk`.

Defensive details:
- Ignore child pointers equal to `0` (don't enqueue page 0).
- The `visited` set is checked before enqueue *and* before processing, so a page referenced
  by two parents is read once.

---

## Acceptance criteria

- [ ] `PagesForRoot` exists on `*Inspector` in `internal/sqlite/btree_walk.go` with the
      signature above.
- [ ] For a single-page (leaf-only) table, returns exactly `[root]`.
- [ ] For a multi-level table (interior + leaves), returns every reachable page, sorted and
      unique, including the root and all leaves under both interior cells and the
      right-most pointer.
- [ ] An index root returns the index's own b-tree pages only (no table pages).
- [ ] A cyclic/self-referential child pointer terminates (no infinite loop) and the page is
      counted once.
- [ ] A child page that fails to parse is recorded in `Skipped` and the walk still returns
      the reachable pages; no panic, no returned error.
- [ ] A hard-failing root (unreadable / out of range) returns a non-nil error.
- [ ] `root == 0` returns an empty `PageWalk` with no error.
- [ ] No changes outside `internal/sqlite` (no TUI, no model).

---

## Testing

New tests in `internal/sqlite/btree_walk_test.go`, using the repo fixtures
(`fixtures/companies.db`, `fixtures/sample.db`, `fixtures/superheroes.db`):

- Walk each table/index root parsed from `sqlite_schema` and assert the returned page set is
  non-empty, sorted, unique, and contains the root.
- Cross-check that the union of all table/index walks is a subset of `1..PageCount` and that
  no page is claimed by two different roots (sanity check on the fixtures).
- Hand-build or reuse a fixture with a known small b-tree to assert exact page membership.
- A unit-level test for the cycle guard and the skip-on-bad-child path (a synthetic /
  truncated page input if a fixture can't produce one naturally).

---

## Notes for the next ticket

`PagesForRoot` is intentionally **pure and stateless** — no internal cache or goroutines.
Ticket 02 fans these walks out across all roots at launch (one Bubble Tea command per root
via `tea.Batch`), and the accumulated `root → PageWalk` index lives in the model. Keeping
this function lock-free is what makes that parallel fan-out safe, since
`os.File.ReadAt` is safe for concurrent use and `dbHeader` is read-only after `Open`.
