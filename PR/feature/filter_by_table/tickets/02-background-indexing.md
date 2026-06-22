# Ticket 02 — Background page index (parallel walk at launch)

> Feature: **Filter Pages by Table or Index** (`feature/filter_by_table`)
> Status: **Drafted — not ready for implementation.**
> Context: [context.md](../context.md) · [codebase-map.md](../codebase-map.md) · [feature-notes.md](../feature-notes.md) · [design.md](../design.md)

---

## Short description

Build the full `root → pages` index eagerly at launch without blocking the UI. `Init()`
returns a `tea.Batch` of one command per b-tree root; each command runs `PagesForRoot`
(Ticket 01) in its own goroutine and returns a `btreeIndexedMsg`, which `Update` reduces
into a model-held index (plus per-root status and skip diagnostics). The structure is kept
file-serializable for later on-disk persistence (out of scope here).

_Details to be filled in when this ticket is picked up._
