# Feature Context: Filter Pages by Table or Index

> Branch: `feature/filter_by_table`
> Status: **Context gathering only.** No implementation, no design decisions yet.

This document captures everything needed to start a discussion about implementing
"filter pages by a specific table or index". It is split across:

- `context.md` (this file) — project overview + feature framing
- `codebase-map.md` — architecture, packages, key types, data flow
- `feature-notes.md` — the hard technical realities specific to this feature

---

## 1. What Badger is

Badger is a **read-only terminal UI (TUI) for exploring SQLite database files at the
byte and page level**. It is a *learning/investigation* tool, not a SQL client. Users
open a `.db` file and drill from high-level metadata down to raw bytes:

```
overview → page → page structure (header / pointer array / cell / freeblock) → parsed fields → decoded values → raw bytes
```

- Language: **Go** (`go 1.26.1`, see `go.mod`).
- TUI framework: **Bubble Tea** + **Lipgloss** (`github.com/charmbracelet/bubbletea`, `.../lipgloss`).
- Entry point: `cmd/badger/main.go` → `tui.Run(path, os.Stdout)`.
- Invocation: `badger <file.db>` (single positional arg).
- Status: **pre-alpha**, originated from the CodeCrafters "Build your own SQLite" course.

### Repo layout (relevant parts)
```
cmd/badger/main.go        CLI entry
internal/tui/             Bubble Tea TUI (model, views, view models)
internal/sqlite/          SQLite file parser / inspector (the data layer)
fixtures/*.db             sample databases (sample, companies, superheroes)
.agent/project-scope.md   product scope draft
.agent/tui-design.md      TUI design spec (wireframes, state model, milestones)
SQLitePageFormat.md       full SQLite on-disk format reference
docs/screenshots/         UI screenshots
```

---

## 2. The feature request

> Users should be able to **select a table or index** and have the UI show **only the
> pages related to that table or index**.

In other words: scope the page list (and possibly navigation) down to the set of pages
that belong to a chosen schema object's b-tree.

### Why this is non-trivial (read `feature-notes.md` for detail)
- The current navigation lists **every page from 1..PageCount** flatly. There is **no
  page → table/index ownership mapping** anywhere in the codebase today.
- Determining which pages belong to a table/index requires **walking the b-tree** from
  that object's `rootpage`, following interior-cell child pointers + the right-most
  pointer, and also following **overflow page** chains. None of this traversal exists yet.
- This was **explicitly listed as out-of-scope for the MVP** in both scope docs. So this
  feature is the first time page-ownership is being introduced. That makes it a notable
  scope expansion worth discussing before designing.

---

## 3. Explicit prior decisions about this feature (from `.agent/` docs)

Both `.agent/project-scope.md` and `.agent/tui-design.md` deliberately **excluded** this
feature from the MVP:

- `project-scope.md` "MVP Scope Boundaries": *"filtering pages by table or index"* listed
  as excluded; also *"page-to-table ownership mapping as a general feature"* excluded.
- `project-scope.md` "Confirmed Decisions" #11: *"Page-to-table mapping, page filters by
  table, and raw hex mode are out of scope for the MVP."*
- `project-scope.md` #12: *"Root/schema parsing is still required for the overview screen,
  but page ownership should not be shown as a general feature."*
- `tui-design.md` "The MVP should not: ... map arbitrary pages back to tables / filter
  pages by table or index".

**Implication for discussion:** implementing this feature is the MVP boundary being
crossed intentionally. Worth confirming the product intent and how deep the ownership
mapping should go.

---

## 4. What already exists that this feature can build on

- Schema objects (tables + indexes) are already parsed from `sqlite_schema` (page 1) and
  available as `databaseViewModel.Tables` / `.Indexes`, each carrying `Name`, `TableName`,
  `RootPage`, `Type`, `SQL`. (`internal/tui/view_model.go`)
- Navigation already groups items into sections: Main / Tables / Indexes / Pages.
- Pages are loaded lazily on demand via `inspector.InspectPage(n)`; a single page's full
  b-tree structure (cells, interior child pointers, right-most pointer) is already parsed.
- Interior cells expose `LeftChildPage` and the page header exposes `RightMostPointer` —
  these are exactly the pointers needed to traverse a b-tree to enumerate its pages.

See `codebase-map.md` for exact types and where they live.

---

## 5. Open questions to resolve before designing (not answered here)

1. **Scope of "related pages"**: just the b-tree pages of the object? Include overflow
   pages? For a *table*, include the pages of its *indexes* too, or keep them separate?
2. **UX model**: a filter that narrows the existing flat Pages list? A new "object → pages"
   drill-down view? A toggle? How does the user clear the filter?
3. **Where the traversal lives**: new method on `sqlite.Inspector` (data layer) vs. derived
   in the TUI view-model layer. (Design docs push parsing concerns into the parser layer.)
4. **Performance / laziness**: walking a b-tree means reading many pages. Eager on select,
   or lazy/cached?
5. **Robustness**: partial-parse / corrupted pages — how should a broken b-tree walk degrade?

These are for the upcoming discussion, intentionally left open.
