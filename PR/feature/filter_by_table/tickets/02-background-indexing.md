# Ticket 02 — Background page index (parallel walk at launch)

> Feature: **Filter Pages by Table or Index** (`feature/filter_by_table`)
> Status: **✅ Done** (`edf55e0`). Result: [results/02-background-indexing.md](../results/02-background-indexing.md).
> Depends on: [Ticket 01](01-btree-traversal.md) (`PagesForRoot` / `PageWalk`) — ✅ done (`84f69bc`).
> Context: [context.md](../context.md) · [codebase-map.md](../codebase-map.md) · [feature-notes.md](../feature-notes.md) · [design.md](../design.md)

---

## Short description

Build the full `root → pages` index eagerly at launch without blocking the UI. `Init()`
returns a `tea.Batch` of one command per b-tree root; each command runs `PagesForRoot`
(Ticket 01) in its own goroutine and returns a `btreeIndexedMsg`, which `Update` reduces
into a model-held index (plus per-root status and skip diagnostics). The structure is kept
file-serializable for later on-disk persistence (out of scope here).

---

## Summary

Ticket 01 shipped the pure primitive `(*Inspector).PagesForRoot(root) (PageWalk, error)`.
This ticket is the **bridge** between that primitive and the filter UI (Tickets 03–06): it
runs the walk for *every* b-tree root once, at startup, in parallel, and accumulates the
results into a model-held index. With the whole index built up-front, applying a filter
later (Ticket 03) is an instant map lookup rather than an async walk.

This ticket introduces **no filter state, key bindings, or PAGES rendering** — those land
in 03/05/06. Its only externally visible effect is that the model is populated with a
`root → PageWalk` index in the background after launch (and a transient indexing status).

### Strategy decisions (confirmed in discussion)

- **Eager at launch**, not lazy-on-filter. This is a deliberate departure from
  `design.md` §4.5/§6, which sketched a walk-on-apply with a `⟳ walked N/~M` progress
  state and a cancel flow (Flow E). Because the index is complete before any filter is
  applied, that per-filter walk UI and the cancel flow are **superseded** by a one-time
  launch indexing indicator. (See "Impact on the agreed design" below.)
- **Index type lives in the `sqlite` package** as a named, serializable type
  (`PageIndex`), not a bare map in the model. Matches the "file-serializable for later
  on-disk persistence" goal and keeps the data concept in the data layer.
- **Forward-only.** Build `root → pages` only. The feature only ever maps a chosen object
  to its pages (to filter the PAGES list); the reverse `page → owner` direction is never
  needed and is not built — here or anywhere in the feature.

---

## Scope

In scope:
- A serializable `PageIndex` type in `internal/sqlite` that holds `root → PageWalk`.
- `Init()` fanning `PagesForRoot` out across all unique, non-zero b-tree roots, one
  `tea.Cmd` per root, via `tea.Batch`.
- A `btreeIndexedMsg` and an `indexBTreeCmd` command (mirrors the existing `loadPageCmd`).
- `Update` reducing each message into the model: ready walks into `PageIndex`, hard
  failures into a transient per-root error map, and a pending/total counter for status.
- Unit tests for root collection, the command, and the `Update` reduction.

Out of scope (later tickets / explicitly cut):
- Any filter state, `f`/`F`/`esc` bindings, or `B-TREES`/`PAGES` rendering (Tickets 03–06).
- Any `page → owner` reverse mapping — the feature filters in one direction only
  (object → its pages); it is never built.
- On-disk persistence of the index (the type is *shaped* to allow it; no Save/Load here).
- Overflow chains / pulling a table's indexes into its set (already cut in Ticket 01).
- A worker pool / bounded concurrency — object counts are small (tens); one goroutine per
  root via `tea.Batch` is sufficient.

---

## Building blocks already in place

Verified in source:
- `(*Inspector).PagesForRoot(root uint32) (PageWalk, error)` — Ticket 01, pure & lock-free
  (`internal/sqlite/btree_walk.go`).
- `PageWalk{Root, Pages, Skipped}` / `SkippedPage{Page, Parent, Reason}` — already plain
  integers + strings, i.e. serializable.
- `databaseViewModel.Tables` / `.Indexes` (`[]schemaObjectViewModel`), each carrying
  `RootPage uint32` — the set of roots to walk (`internal/tui/view_model.go`).
- `loadPageCmd` + `pageLoadedMsg` (`internal/tui/app.go`) — the async-command pattern to
  mirror; `Update`'s message switch (`internal/tui/model.go:85`) is where the reducer goes.

**Concurrency safety — verified, not assumed.** `InspectPage` → `readPage`
(`inspector.go:44,123`) only reads `i.file` via `os.File.ReadAt` (safe for concurrent use)
and the read-only `i.dbHeader`. There is **no internal cache and no shared mutable state**
on `Inspector`, so N goroutines each calling `PagesForRoot` concurrently is safe. The
*index* itself is only ever written in `Update`, which Bubble Tea runs single-threaded, so
the map needs no lock.

---

## Proposed API

### Data layer — `internal/sqlite/page_index.go` (new)

```go
// PageIndex maps each b-tree root page to the walk that enumerated it.
// It is plain data (PageWalk is itself serializable), so the whole index can later be
// persisted to / restored from disk. Build it from a single goroutine (the TUI Update
// loop); the walks it stores are produced in parallel upstream by PagesForRoot.
type PageIndex struct {
    Walks map[uint32]PageWalk // keyed by PageWalk.Root
}

func NewPageIndex() PageIndex

// Set stores (or replaces) the walk for walk.Root.
func (idx PageIndex) Set(walk PageWalk)

// Walk returns the stored walk for root and whether it is present (indexed yet).
func (idx PageIndex) Walk(root uint32) (PageWalk, bool)

// Pages returns the page set for root, or nil if not indexed. Convenience for callers
// that only want the page numbers (Ticket 03's filtered PAGES list).
func (idx PageIndex) Pages(root uint32) []uint32
```

> `Set` takes a value receiver but mutates the underlying map (maps are reference types),
> which is intentional and matches Go idiom for map-backed wrappers.

### TUI — `internal/tui/app.go`

```go
type btreeIndexedMsg struct {
    root uint32
    walk sqlite.PageWalk
    err  error // hard failure from PagesForRoot (unreadable/invalid root)
}

func indexBTreeCmd(inspector *sqlite.Inspector, root uint32) tea.Cmd {
    return func() tea.Msg {
        walk, err := inspector.PagesForRoot(root)
        return btreeIndexedMsg{root: root, walk: walk, err: err}
    }
}
```

### TUI — `internal/tui/model.go`

New `model` fields:

```go
pageIndex    sqlite.PageIndex  // root → PageWalk (ready entries only)
indexRoots   []uint32          // unique, non-zero roots dispatched at launch
indexErrors  map[uint32]string // root → hard-failure reason (transient; NOT serialized)
indexPending int               // roots still being walked (indexTotal → 0)
indexTotal   int               // total roots dispatched
```

`indexErrors` holds the *reason string* (not an `error`) so the model's index-related state
stays serialization-friendly and consistent with `PageWalk.Skipped.Reason`.

---

## Algorithm / wiring

1. **`newModel`** computes the roots once and seeds the index state:
   - `m.indexRoots = collectBTreeRoots(db)` — union of `db.Tables` + `db.Indexes` root
     pages, **deduped** (a root belongs to exactly one object, but dedupe defensively) and
     **skipping `RootPage == 0`** (views / virtual tables have no b-tree; `PagesForRoot`
     would return empty anyway).
   - `m.pageIndex = sqlite.NewPageIndex()`, `m.indexErrors = map[uint32]string{}`.
   - `m.indexTotal = len(m.indexRoots)`, `m.indexPending = len(m.indexRoots)`.

2. **`Init()`** (currently returns `nil`) returns the fan-out:
   ```go
   func (m model) Init() tea.Cmd {
       if len(m.indexRoots) == 0 {
           return nil
       }
       cmds := make([]tea.Cmd, 0, len(m.indexRoots))
       for _, root := range m.indexRoots {
           cmds = append(cmds, indexBTreeCmd(m.inspector, root))
       }
       return tea.Batch(cmds...)
   }
   ```
   `tea.Batch` runs each command in its own goroutine — that is the parallel walk.

3. **`Update`** gains a reducer:
   ```go
   case btreeIndexedMsg:
       if m.indexPending > 0 {
           m.indexPending--
       }
       if msg.err != nil {
           m.indexErrors[msg.root] = msg.err.Error()
       } else {
           m.pageIndex.Set(msg.walk)
       }
       // Transient status only; the polished footer token is Ticket 06.
       if m.indexPending == 0 {
           m.status = indexCompleteStatus(m) // e.g. "indexed 14 b-trees (1 failed)"
       }
       return m, nil
   ```

4. **`collectBTreeRoots(db databaseViewModel) []uint32`** — small helper next to
   `buildNavItems`, dedupes via a `seen` set and skips `0`.

Status during indexing is intentionally minimal here (a one-line `indexing… N/M` /
completion string is fine); the real footer treatment lands in Ticket 06.

---

## Impact on the agreed design (`design.md`)

Worth recording because this ticket changes a previously "agreed" design:

- **§4.5 "Walk in progress" (per-filter)** → becomes a **launch-time** indexing indicator.
  Applying a filter no longer triggers a walk.
- **§6 "Lazy + cached" / "Async on filter-apply"** → replaced by eager-at-launch. The
  `map[rootpage][]uint32` cache it described is exactly what `PageIndex` is, just built
  up-front.
- **Flow E "Cancel an in-progress walk" (`esc` during walk)** → no longer applicable to
  filtering; the only walking now happens at launch. (`esc` keeps its existing meanings.)

These should be reconciled into `design.md` when Ticket 03 (filter state) is picked up.

---

## Acceptance criteria

- [x] `PageIndex` (with `Walks map[uint32]PageWalk`), `NewPageIndex`, `Set`, `Walk`, and
      `Pages` exist in `internal/sqlite/page_index.go`.
- [x] `btreeIndexedMsg` and `indexBTreeCmd` exist in `internal/tui/app.go`, mirroring the
      `loadPageCmd` / `pageLoadedMsg` pattern.
- [x] `model` holds `pageIndex`, `indexRoots`, `indexErrors`, `indexPending`, `indexTotal`,
      all initialized in `newModel`.
- [x] `Init()` returns a `tea.Batch` with exactly one command per unique non-zero root, and
      `nil` when there are no roots.
- [x] `collectBTreeRoots` returns unique roots from tables + indexes, excludes `0`, and
      contains no duplicates.
- [x] After all `btreeIndexedMsg`s are processed, `pageIndex.Walk(root)` returns a walk for
      every successfully-walked root, matching a direct `PagesForRoot(root)` call.
- [x] A hard-failing root is recorded in `indexErrors[root]` (reason string) and is **not**
      present in `pageIndex`; the walk for it does not appear, but other roots still index.
- [x] `indexPending` reaches `0` exactly when `indexTotal` messages have been processed.
- [x] The walk runs off the UI goroutine: `Init` returns commands; no `PagesForRoot` call
      happens on the `Update`/`View` path.
- [x] No filter UI, key bindings, or PAGES-list changes (kept for Tickets 03–06).
- [x] `go vet ./...` clean; existing tests still pass.

---

## Testing

New tests `internal/sqlite/page_index_test.go` and `internal/tui/index_test.go` (or
extend an existing `*_test.go` in each package):

- **`PageIndex` unit** — `NewPageIndex` is empty; `Set` then `Walk` round-trips a walk;
  `Walk` on an unindexed root returns `(_, false)`; `Pages` returns the walk's pages and
  `nil` for an unindexed root.
- **`collectBTreeRoots`** — on each fixture (`sample`/`companies`/`superheroes`): result is
  deduped, excludes `0`, and equals the set of distinct non-zero `RootPage`s across
  `Tables` + `Indexes`.
- **`indexBTreeCmd`** — invoking the returned `tea.Cmd` for a known fixture root yields a
  `btreeIndexedMsg` whose `walk` equals a direct `inspector.PagesForRoot(root)`, `err` nil.
- **`Init` fan-out** — a model built from a fixture has `len(indexRoots) == indexTotal`,
  and the count matches the distinct non-zero schema roots.
- **`Update` reduction (end-to-end, in-process)** — build a model from `companies.db`, then
  for each root feed the `btreeIndexedMsg` produced by `indexBTreeCmd` into `Update`;
  assert: every root is in `pageIndex` (matching `PagesForRoot`), `indexPending == 0`,
  `indexErrors` empty.
- **Hard-failure path** — feed a synthetic `btreeIndexedMsg{root: R, err: someErr}`; assert
  `indexErrors[R]` is set, `R` absent from `pageIndex`, `indexPending` still decremented.

(`tea.Batch` returns an opaque single `tea.Cmd`, so tests assert on `indexRoots` /
per-command behavior rather than trying to introspect the batch.)

---

## Notes for the next ticket

Ticket 03 (filter state) consumes this index directly: applying a filter to an object
becomes `pageIndex.Pages(object.RootPage)` — an instant lookup, no async walk. It should
also handle the **not-yet-indexed** edge (a filter applied before that root's
`btreeIndexedMsg` arrives): either disable `f` until `indexPending == 0`, or fall back to a
one-off `indexBTreeCmd`. `indexErrors` / `PageWalk.Skipped` carry the diagnostics that the
Ticket 06 footer (`⚠ page N unreadable`, `n skipped`) will render. The index is used in one
direction only — object root → its pages; there is no `page → owner` reverse lookup
anywhere in the feature.
