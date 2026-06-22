# Result — Ticket 02: Background page index (parallel walk at launch)

> Feature: **Filter Pages by Table or Index** (`feature/filter_by_table`)
> Ticket: [tickets/02-background-indexing.md](../tickets/02-background-indexing.md) · Status: **✅ Done**
> Commit: `edf55e0` — *Implement the second ticket, and update the design*
> Depends on: [Ticket 01](../results/01-btree-traversal.md) (`PagesForRoot` / `PageWalk`)

---

## Summary

Built the **bridge** between Ticket 01's pure walk primitive and the filter UI of the
later tickets: every b-tree root is walked **once, at launch, in parallel, off the UI
goroutine**, and the results accumulate into a model-held `root → PageWalk` index. With
the whole index built up-front, Ticket 03's filter-apply becomes an instant map lookup
(`pageIndex.Pages(root)`) instead of an async walk.

The index type (`PageIndex`) lives in the `sqlite` data layer as a named, serializable
wrapper — not a bare map in the model — keeping the data concept in the data layer and
leaving the door open for the (out-of-scope) on-disk persistence. Concurrency is via one
Bubble Tea command per root through `tea.Batch`; no worker pool, since object counts are
small (tens).

This ticket adds **no filter state, key bindings, or PAGES rendering** (Tickets 03–06).
Its only user-visible effect is a transient one-line footer status once indexing
completes (`indexed N b-trees`); see "Visibility" below.

---

## What was delivered

| File | Δ | Purpose |
| --- | --- | --- |
| `internal/sqlite/page_index.go` | +38 | `PageIndex` type + `NewPageIndex`, `Set`, `Walk`, `Pages` |
| `internal/sqlite/page_index_test.go` | +75 | 3 unit tests for the index wrapper |
| `internal/tui/app.go` | +13 | `btreeIndexedMsg`, `indexBTreeCmd` (mirrors `loadPageCmd`/`pageLoadedMsg`) |
| `internal/tui/model.go` | +67/−12 | 5 model fields, `newModel` seeding, `Init()` fan-out, `Update` reducer, `collectBTreeRoots` + `indexCompleteStatus` helpers |
| `internal/tui/index_test.go` | +206 | 6 TUI tests (new test file for the package) |

### API (as shipped)

```go
// internal/sqlite/page_index.go
type PageIndex struct {
    Walks map[uint32]PageWalk // keyed by PageWalk.Root
}

func NewPageIndex() PageIndex
func (idx PageIndex) Set(walk PageWalk)            // value receiver, mutates backing map
func (idx PageIndex) Walk(root uint32) (PageWalk, bool)
func (idx PageIndex) Pages(root uint32) []uint32   // nil when unindexed

// internal/tui/app.go
type btreeIndexedMsg struct {
    root uint32
    walk sqlite.PageWalk
    err  error // hard failure from PagesForRoot (unreadable/invalid root)
}
func indexBTreeCmd(inspector *sqlite.Inspector, root uint32) tea.Cmd
```

This matches the ticket's proposed API exactly.

---

## Wiring (as implemented)

1. **`newModel`** computes the roots once via `collectBTreeRoots(db)` and seeds the index
   state: `pageIndex = NewPageIndex()`, `indexErrors = {}`, and
   `indexTotal = indexPending = len(indexRoots)`.
2. **`Init()`** returns `nil` when there are no roots; otherwise a `tea.Batch` of one
   `indexBTreeCmd` per root. `tea.Batch` runs each in its own goroutine — that *is* the
   parallel walk.
3. **`Update`** gains a `btreeIndexedMsg` reducer: decrement `indexPending` (guarded `> 0`),
   route a hard failure into `indexErrors[root]` (reason **string**, for serialization
   friendliness) or a success into `pageIndex.Set(walk)`, and set a transient completion
   status when `indexPending` hits `0`.
4. **`collectBTreeRoots(db) []uint32`** — unions `db.Tables` + `db.Indexes` root pages,
   dedupes via a `seen` set, and skips `RootPage == 0` (views / virtual tables have no
   b-tree).
5. **`indexCompleteStatus(m)`** — `indexed N b-trees`, or `indexed N b-trees (M failed)`
   when `indexErrors` is non-empty.

**Design notes**
- *Reason as `string`, not `error`* — keeps the model's index-related state
  serialization-friendly and consistent with `PageWalk.Skipped.Reason`.
- *No lock on the map* — the index is only ever written in `Update`, which Bubble Tea runs
  single-threaded; the parallelism is upstream in `PagesForRoot` (verified lock-free in
  Ticket 01).
- *`collectBTreeRoots` dedupes defensively* — a root belongs to exactly one object, but the
  `seen` set guards against malformed schemas without changing the happy path.

---

## Acceptance criteria

- [x] `PageIndex`, `NewPageIndex`, `Set`, `Walk`, `Pages` in `internal/sqlite/page_index.go`.
- [x] `btreeIndexedMsg` and `indexBTreeCmd` in `internal/tui/app.go`, mirroring `loadPageCmd`.
- [x] `model` holds `pageIndex`, `indexRoots`, `indexErrors`, `indexPending`, `indexTotal`, all seeded in `newModel`.
- [x] `Init()` returns a `tea.Batch` of one command per unique non-zero root, `nil` when there are none.
- [x] `collectBTreeRoots` returns unique roots from tables + indexes, excludes `0`, no duplicates.
- [x] After all messages, `pageIndex.Walk(root)` matches a direct `PagesForRoot(root)` for every walked root.
- [x] A hard-failing root lands in `indexErrors[root]` and is absent from `pageIndex`; other roots still index.
- [x] `indexPending` reaches `0` exactly when `indexTotal` messages are processed.
- [x] Walk runs off the UI goroutine; no `PagesForRoot` on the `Update`/`View` path.
- [x] No filter UI, key bindings, or PAGES-list changes.
- [x] `go vet ./...` clean; existing tests still pass.

---

## Tests

9 new test functions, all passing; `go build ./...` and `go vet ./...` clean; full
`go test ./...` green.

| Test | Covers |
| --- | --- |
| `TestPageIndexEmpty` | fresh index: initialized map, `Walk`/`Pages` miss → `(_, false)` / `nil` |
| `TestPageIndexSetWalkRoundTrip` | `Set` then `Walk`/`Pages` round-trip; miss on an unindexed root |
| `TestPageIndexSetReplaces` | `Set` on an existing root replaces, not duplicates |
| `TestCollectBTreeRoots` | sweep of `sample`/`companies`/`superheroes`: deduped, no `0`, equals distinct non-zero schema roots (re-derived independently of `collectBTreeRoots`) |
| `TestInitFanOut` | `len(indexRoots) == indexTotal == indexPending`, count matches distinct schema roots, `Init` non-nil when roots exist |
| `TestInitNoRoots` | `Init` on a root-less model returns `nil` |
| `TestIndexBTreeCmd` | invoking the command yields a `btreeIndexedMsg` whose `walk` equals a direct `PagesForRoot`, `err` nil |
| `TestUpdateReductionEndToEnd` | feed every root's message through `Update` (`companies.db`): every root in `pageIndex` matching `PagesForRoot`, `indexPending == 0`, `indexErrors` empty |
| `TestUpdateReductionHardFailure` | synthetic `btreeIndexedMsg{err}`: `indexErrors[root]` set, root absent from `pageIndex`, `indexPending` still decremented |

`internal/tui/index_test.go` is the first test file in the `tui` package; it adds a
`fixturePath` / `newFixtureModel` helper that opens a fixture, inspects metadata, and
builds a `model` the same way `Run` does.

---

## Visibility (answering "how do I see it work?")

This ticket is **background plumbing** — there is no panel reflecting the index contents
yet. The one user-visible signal is the **footer status line**, which `Update` sets to
`indexed N b-trees` once `indexPending` reaches `0`. Two caveats:

- For the repo fixtures the parallel walks finish in **milliseconds**, so there is
  effectively no visible "indexing…" phase — the completed string just appears at launch.
- The status is **transient**: the first navigation keypress overwrites it.

The reliable way to observe the populated index today is `TestUpdateReductionEndToEnd`.
The index becomes genuinely visible in **Ticket 03**, when the filtered PAGES list starts
consuming `pageIndex.Pages(root)`.

---

## Impact on the agreed design (`design.md`)

Per the ticket, the design was reconciled in the same commit (`edf55e0` also touches
`design.md`):

- **§4.5 "Walk in progress" (per-filter)** → a **launch-time** indexing indicator;
  applying a filter no longer triggers a walk.
- **§6 "Lazy + cached" / "Async on filter-apply"** → replaced by eager-at-launch; the
  `map[rootpage][]uint32` cache it described *is* `PageIndex`, built up-front.
- **Flow E "Cancel an in-progress walk"** → no longer applies to filtering; the only
  walking happens at launch.

---

## Notes for the next ticket

Ticket 03 (filter state) consumes this index directly: applying a filter to an object
becomes `pageIndex.Pages(object.RootPage)` — an instant lookup, no async walk. It must
handle the **not-yet-indexed** edge (a filter applied before that root's `btreeIndexedMsg`
arrives): either disable `f` until `indexPending == 0`, or fall back to a one-off
`indexBTreeCmd`. `indexErrors` / `PageWalk.Skipped` carry the diagnostics that the Ticket
06 footer (`⚠ page N unreadable`, `n skipped`) will render. The index is used in one
direction only — object root → its pages; there is no `page → owner` reverse lookup
anywhere in the feature.
