# Ticket 04 — Key bindings: section jumps & `esc`-clear (remainder)

> Feature: **Filter Pages by Table or Index** (`feature/filter_by_table`)
> Status: **📝 Drafted — depends on [Ticket 03](03-filter-state.md).**
> Context: [context.md](../context.md) · [codebase-map.md](../codebase-map.md) · [feature-notes.md](../feature-notes.md) · [design.md](../design.md)

---

## Short description

The **remainder** of the key bindings after [Ticket 03](03-filter-state.md) shipped the
filtration experience (`f` apply, `F` clear, `[`/`]` filtered paging). This ticket adds the
navigation convenience keys from `design.md` §3 that were intentionally deferred:

- `1` → jump selection to the first `MAIN` row.
- `2` → jump selection to the first `B-TREES` row.
- `3` → jump selection to the first `PAGES` row.
- `esc` → second clear-filter binding (same effect as `F`, active only while filtered).

These are pure `handleKey` additions over the nav structure Ticket 03 already builds;
existing bindings are preserved.

_Full details to be filled in when this ticket is picked up._
