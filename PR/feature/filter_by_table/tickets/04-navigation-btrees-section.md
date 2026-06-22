# Ticket 04 — Navigation: merged B-TREES section & filter-aware PAGES

> Feature: **Filter Pages by Table or Index** (`feature/filter_by_table`)
> Status: **Drafted — not ready for implementation.**
> Context: [context.md](../context.md) · [codebase-map.md](../codebase-map.md) · [feature-notes.md](../feature-notes.md) · [design.md](../design.md)

---

## Short description

Merge the separate `Tables` and `Indexes` nav sections into one `B-TREES` section with
`▦` / `◈` icons, and make `buildNavItems` filter-aware so the `PAGES` section iterates the
filtered page set when a filter is active and the full `1..PageCount` range otherwise
(`design.md` §2 / §7).

_Details to be filled in when this ticket is picked up._
