# Ticket 05 - Keep navigation section headers fixed

> Feature: **Better UI** (`feature/better_ui`)
> Source feedback: follow-up navigation feedback
> Current code hotspots: `internal/tui/model.go`, `internal/tui/keys_test.go`, `README.md`

## Summary

Make every navigation section header an unscrollable element with a stable position and
height budget. Section headers such as `[1] B-TREES` and `[2] PAGES` should always stay in
place while rows inside each section scroll within that section's own viewport.

Current behavior:
- `viewNavigation` renders section headers as part of the same visible row slice as the
  section's items.
- `visibleNavItems` centers the selected row inside one shared navigation scroll window.
- When the user jumps to `PAGES` and presses Down repeatedly, the `[1] B-TREES` section
  header can disappear from the navigation pane.

Target behavior:
- Each section header is rendered outside that section's scrollable item list.
- Each section has a static size proportional to the terminal height.
- Scrolling inside `PAGES` never moves or hides `[1] B-TREES`.
- Scrolling inside `B-TREES` never moves or hides `[2] PAGES`.

## Scope

In scope:
- Replace the single shared navigation viewport with per-section viewports.
- Keep every section header visible whenever the section exists.
- Compute section row budgets from the current terminal height, with sensible minimums for
  small terminals.
- Scroll only the rows inside the active section's allocated viewport.
- Preserve the existing keyboard model:
  - arrow keys stay within the current section;
  - `1` jumps to the first b-tree row;
  - `2` jumps to the first page row;
  - section jumps auto-activate the selected row after Ticket 02.
- Add tests for long `PAGES` lists that reproduce the reported case.
- Update README navigation docs if they describe the scrolling behavior.

Out of scope:
- Changing which sections exist.
- Changing the numbered jump mapping from Tickets 01 and 04.
- Redesigning page data representation.
- Adding mouse-driven scrolling.

## Implementation notes

The current problem is structural: `visibleNavItems` returns a single centered slice of
rows, and `viewNavigation` injects headers into that slice. That makes headers behave like
scroll content.

Prefer a rendering model closer to:

```text
Navigation

[1] B-TREES
<fixed-height b-tree row viewport>

[2] PAGES
<fixed-height page row viewport>
```

Possible implementation shape:
- Group `m.navItems` by `sectionForNavItem`.
- Build a deterministic ordered section list: `B-Trees`, then `Pages`, then future
  sections if any.
- Calculate each section's body height from the available navigation height after the
  title, blank lines, and headers are reserved.
- Allocate at least one visible row to every non-empty section when the terminal is tall
  enough.
- Distribute remaining row slots proportionally. A simple equal split is acceptable for
  the first pass; if one section is empty or tiny, give the unused slots to sections with
  more rows.
- For each section, derive a section-local selected index and scroll window. The selected
  row should remain visible inside that section's viewport without changing the header's
  screen position.
- Keep the `fitVertical` truncation as a final guard, not as the primary layout mechanism.

Pay attention to small terminal behavior:
- The existing `terminal too small for badger` cutoff remains valid.
- If there is not enough room to show every header plus at least one row, prefer showing
  headers first and truncate row lists predictably.
- Do not let a long `PAGES` list consume the b-tree section's reserved height.

## Definition of done

- [ ] `[1] B-TREES` remains visible after jumping to `PAGES` and pressing Down many times.
- [ ] `[2] PAGES` remains visible after jumping to `B-TREES` and pressing Down many times.
- [ ] Navigation section headers are not part of the section row scroll windows.
- [ ] Each non-empty section receives a stable viewport height derived from terminal
      height.
- [ ] The selected row remains visible within its own section viewport.
- [ ] Arrow-key confinement within sections still works.
- [ ] Numbered jumps still select the first row of the target section.
- [ ] Unit tests cover the sticky-header behavior for long page lists and constrained
      terminal heights.

## Manual test

Run:

```bash
make build
./bin/badger fixtures/companies.db
```

Verify:
- `[1] B-TREES` and `[2] PAGES` are visible in the navigation pane on launch.
- Press `2` to jump to pages.
- Press Down repeatedly until the selected page row is far into the page list.
- `[1] B-TREES` stays visible in the same place and keeps a stable section height.
- `[2] PAGES` stays visible in the same place while only page rows scroll.
- Press `1` to jump back to b-trees.
- `[2] PAGES` remains visible while b-tree rows scroll inside their own section.

Repeat after resizing the terminal taller and shorter. The section heights should adjust
proportionally to the terminal size, but headers should not become scrollable rows.
