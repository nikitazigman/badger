# Feature Notes: technical realities of filtering pages by table/index

These are the concrete facts that should anchor the design discussion. No decisions made.

## How SQLite associates pages with a table/index

- Every table and index is a **b-tree** identified by its `rootpage` (from
  `sqlite_schema`, already parsed into `schemaObjectViewModel.RootPage`).
- The set of pages "belonging to" an object = all nodes of that b-tree:
  - Start at `rootpage`.
  - If the page is **interior** (`PageKind` 0x05 table / 0x02 index): visit every
    `LeftChildPage` from its interior cells **and** the page header's `RightMostPointer`.
  - If **leaf** (0x0d table / 0x0a index): it's a terminal node.
  - Recurse until all reachable pages are collected.
- **Overflow pages**: a leaf cell whose payload is large spills into an overflow page
  chain. The parser already surfaces the first overflow page as
  `RecordPayload.OverflowFirstPage`, but the *rest* of the chain is not currently walked,
  and overflow pages are **not** b-tree pages (they parse differently). Decision needed on
  whether "related pages" includes overflow pages.
- **Tables vs their indexes**: an index is a separate b-tree with its own root page. A
  table's own b-tree does **not** include its index pages. If a user filters by a table
  and expects to also see its indexes, that's an explicit product choice (the owning table
  is available via `schemaObjectViewModel.TableName` on each index).

## What the codebase already gives us (building blocks)
- `inspector.InspectPage(n)` returns a fully parsed `BTreePage` for any page, including:
  - `PageHeader.PageKind` + `PageHeader.RightMostPointer`
  - `TableInteriorCells[].LeftChildPage`, `IndexInteriorCells[].LeftChildPage`
- So a traversal can be composed from existing primitives — no new low-level parsing
  needed for the b-tree walk itself (overflow-chain walking would be new).

## What is missing (the actual new work)
1. **A traversal that maps rootpage → set of pages.** Does not exist anywhere today.
   - Candidate home: a new `Inspector` method (data layer) returning page numbers for a
     root, OR a TUI-side helper. Design docs favor keeping parsing in the parser layer.
2. **A page-ownership concept in the model.** `model`/`databaseViewModel` have no notion
   of "which object owns which page" or "current filter".
3. **UI to choose an object and apply/clear a filter**, and to reflect the filtered set in
   navigation (`buildNavItems` currently hard-loops `1..PageCount`).

## Cost / performance considerations
- A b-tree walk reads every page in the tree. For large DBs that is many `ReadAt` calls.
- `InspectPage` does full structural parsing per page; if traversal only needs page kind +
  child pointers, a lighter read might be preferable — open question.
- Results are stable for a read-only DB, so caching the rootpage→pages map per object is
  viable.

## Robustness
- The design docs require pages to **stay navigable on partial parse**. A b-tree walk must
  tolerate a child page that fails to parse (skip + surface, don't crash the workspace).
- Cycle protection: `parseFreeblocks` already guards against cycles; a page-traversal
  walk would need its own visited-set guard against malformed child pointers.

## Reference material in-repo
- `SQLitePageFormat.md` — authoritative on-disk format (b-tree pages, interior/leaf cells,
  overflow pages, freeblocks). Use for exact byte semantics.
- `.agent/project-scope.md` / `.agent/tui-design.md` — product + UX intent, and the
  explicit MVP exclusion of this feature (see `context.md` §3).
