# Ticket 04 - Complete numbered jumps for detail and meta panes

> Feature: **Better UI** (`feature/better_ui`)
> Source feedback: [feedback.md](../feedback.md), item 4
> Current code hotspots: `internal/tui/model.go`, `internal/tui/page_view.go`, `internal/tui/keys_test.go`, `README.md`

## Summary

Extend the numbered navigation model so users can jump directly to the middle and right
panes. The old numbered jumps only move the selection inside the left navigation pane:

- `1` -> MAIN
- `2` -> B-TREES
- `3` -> PAGES

After Ticket 01 removes MAIN, the numbers should represent the useful workflow targets in
order: b-trees, pages, detail view, and meta/info view. Storage-level information is
represented by the `sqlite_schema` system catalog b-tree and page 1, not by a separate
shortcut.

## Scope

In scope:
- Add key handling for `3` and `4`.
- `3` focuses the middle pane, currently represented by `explorerPane`.
- `4` focuses the right pane, currently represented by `inspectorPane`.
- Render visible `[3]` and `[4]` labels so the mapping is discoverable.
- Keep the footer focused on non-obvious controls; do not repeat `[1]` through `[4]` there.
- Update README docs.
- Update tests for numbered jump behavior.

Out of scope:
- Redesigning page data presentation inside the panes.
- Changing the number of panes.
- Adding mouse-only pane selection.

## Proposed mapping

Use this mapping once Ticket 01 removes MAIN:

```text
[1] B-TREES
[2] PAGES
[3] DETAIL / middle pane / explorerPane
[4] META / right pane / inspectorPane
```

If this ticket is implemented before Ticket 01, do not make `[1] MAIN` more prominent.
Prefer landing Ticket 01 first so the final numbering can be implemented directly.

Naming:
- The code currently calls the middle pane `explorerPane`.
- The user-facing ticket language calls it "detail view".
- The code currently calls the right pane `inspectorPane`.
- The user-facing ticket language calls it "meta info view".

Do not rename internal types unless the implementation becomes clearer. It is fine for UI
labels to say `DETAIL` and `META` while the internal enum remains `explorerPane` and
`inspectorPane`.

## Definition of done

- [ ] Pressing `3` focuses the middle/detail pane.
- [ ] Pressing `4` focuses the right/meta pane.
- [ ] Pressing `3` and `4` does not change the selected nav row or active filter.
- [ ] Existing pane-local controls still work after the jump:
  - in the detail pane, arrows move page structures when a page is active;
  - in the meta pane, arrows/page-up/page-down scroll inspector content.
- [ ] Visible UI copy includes `[3]` and `[4]` where users learn numbered jumps.
- [ ] Footer hints do not repeat the already-visible numbered panes.
- [ ] README navigation docs document `3` and `4`.
- [ ] Unit tests cover the new focus jumps and verify they are selection-preserving.

## Progress / implementation context

Status: **in progress, not closed**.

Work implemented so far:
- `internal/tui/model.go`
  - Added direct key handling:
    - `1` sets `focusedPane = navPane`, selects `[1] B-TREES`, and opens the first row.
    - `2` sets `focusedPane = navPane`, selects `[2] PAGES`, and loads the first row.
    - `3` sets `focusedPane = explorerPane`.
    - `4` sets `focusedPane = inspectorPane`.
  - Removed tab-based focus cycling; users choose views with `1` through `4`.
  - The direct focus jumps are intentionally selection-preserving:
    - they do not call `activateSelected`;
    - they do not move `selectedIndex`;
    - they do not change `active`;
    - they do not clear or switch `activeFilter`.
  - Footer hints no longer repeat the numbered jumps; the pane and section frames make
    `[1]` through `[4]` visible in the main UI.
  - The left navigation column now renders `[1] B-TREES` and `[2] PAGES` as separate
    framed sections instead of sharing one generic navigation frame.
  - The content pane headers render on the pane frames, matching `[1]` and `[2]`:
    - middle pane: `[3] DETAIL  ...`
    - right pane: `[4] META  ...`
  - Layout math was adjusted after live terminal testing:
    - pane height now subtracts border rows before passing height to `lipgloss.Height`;
    - pane widths are treated as outer widths;
    - content widths subtract border + horizontal padding;
    - footer rendering avoids Lip Gloss wrapping by manually placing/truncating the text
      before applying the padded status style.
- `internal/tui/keys_test.go`
  - Replaced the old "3/4 are reserved no-ops" test with selection-preserving focus jump
    coverage.
  - Added coverage that pane-local controls still work after numbered jumps:
    - after `3`, `down` moves `explorerIndex` for loaded page structures;
    - after `4`, `down` scrolls `inspectorScroll` without moving navigation.
  - Added a render regression for an `80x24` terminal:
    - rendered view must have exactly `m.height` physical rows;
    - first row must include the top border;
    - no rendered row may exceed `m.width`;
    - rendered view must include `[3] DETAIL` and `[4] META`.
- `README.md`
  - Updated navigation docs to describe `[3] Detail` and `[4] Meta`.

Important debugging context:
- The first attempt put `[3]` and `[4]` in footer hints only. This was discoverable but did
  not satisfy the pane-label requirement.
- The second attempt prepended title rows inside `viewExplorer` / `viewInspector`, but live
  Bubble Tea rendering still clipped the top rows. Unit-level pane rendering looked correct,
  but the running TUI showed the body shifted up.
- Live PTY capture of `./bin/badger fixtures/companies.db` showed the root cause was layout
  sizing:
  - panes were being given a height that did not account for border rows;
  - the padded footer wrapped into a second physical row;
  - the total rendered view became taller than the terminal, causing the top pane row/header
    to be pushed offscreen.
- After fixing height and footer wrapping, live PTY capture showed the top border and pane
  headers correctly.
- A later width fix changed pane width handling so the right pane border is no longer clipped
  horizontally.
- A follow-up navigation polish pass split the left column into separate lazygit-style
  framed sections and removed the generic `Navigation` title. The footer was shortened so
  it does not explain the already-visible numbered jumps.

Validation run so far:

```bash
go test ./internal/tui
go test ./...
go build -a -o bin/badger ./cmd/badger
./bin/badger fixtures/companies.db
```

The live PTY capture after the layout fixes showed:
- top border visible;
- `[3] DETAIL ...` visible in the middle pane header;
- `[4] META ...` visible in the right pane header;
- right border visible;
- footer constrained to one row.

## Remaining work / open issues

This ticket is still open. Do not treat the current implementation as final.

Known remaining issues:
- Section titles still need a proper design/rendering pass.
  - The current title/header treatment is functional but not final.
  - We need to decide how section titles inside the panes should relate to pane headers:
    for example `SUMMARY`, `DETAIL`, `ACTIONS`, `SQL`, and `STRUCTURES`.
  - The goal is a normal header hierarchy, not duplicated labels or labels embedded in body
    rows.
- The right side of the view still has the same class of problem as the original header issue.
  - The right/meta pane content can wrap awkwardly and split labels/values across lines.
  - The next pass should inspect right-pane width/content calculations and section-title
    rendering together.
  - In particular, verify that the right pane's headers, section titles, and wrapped rows do
    not appear clipped, shifted, or visually broken at common terminal widths.
- Re-run the manual test after the section-title/right-pane work is complete.

## Recommended layout guidance from lazygit

Use this as implementation guidance for the remaining layout work. Lazygit uses gocui, while
Badger currently uses Bubble Tea + Lip Gloss, so do **not** copy lazygit's coordinate expansion
mechanically. The transferable lesson is to be explicit about whether a dimension describes the
outer frame, the inner content area, or a full terminal row/column.

Rendering model:
- Treat the terminal as a zero-based grid.
  - Top-left is `(0, 0)`.
  - Bottom-right is `(width-1, height-1)`.
- Always get `width,height` from the terminal backend.
- On every full render or resize, lay out the entire UI from `(0,0)` again.
- Assign every view/pane explicit coordinates or explicit outer dimensions.

Lazygit reference points:
- `pkg/gocui/gui.go:289`
  - terminal size / render setup path.
- `pkg/gui/layout.go:18`
  - layout is recalculated on render/resize.
- `pkg/gocui/view.go:579`
  - gocui draws view content at:

```text
absoluteX = view.x0 + contentX + 1
absoluteY = view.y0 + contentY + 1
```

Important gocui-specific detail:
- In that gocui implementation, content is inset by `+1,+1`.
- Even frameless views need a one-cell coordinate expansion.
- Lazygit compensates with:

```go
frameOffset := 1
if view.Frame {
    frameOffset = 0
}

g.SetView(
    viewName,
    dimensions.X0-frameOffset,
    dimensions.Y0-frameOffset,
    dimensions.X1+frameOffset,
    dimensions.Y1+frameOffset,
    0,
)
```

- See `pkg/gui/layout.go:101`.
- Example from lazygit/gocui:
  - frameless one-line header assigned to terminal row `0` should not use `y0=0,y1=0`;
  - underlying gocui rectangle should be:

```go
SetView("header", x0-1, -1, x1+1, 1, 0)
```

  - frameless footer assigned to terminal row `height-1` should use:

```go
SetView("footer", x0-1, height-2, x1+1, height, 0)
```

Badger/Bubble Tea interpretation:
- Do not use the gocui `-1/+1` expansion directly.
- Instead, explicitly map:
  - root terminal size;
  - reserved footer/status row;
  - pane outer width/height;
  - pane border width/height;
  - pane padding;
  - pane inner content width/height.
- The current bug class came from mixing these spaces:
  - pane height was passed to Lip Gloss as if it were outer height, but Lip Gloss added border
    rows on top of it;
  - footer padding caused an extra physical row;
  - total output became taller than the terminal, so the terminal viewport clipped the top row.

Recommended root layout for Badger:

```text
row 0..height-2      framed main panes
row height-1         footer/status line
```

For framed panels:
- Treat pane titles as part of the pane/frame, not as body fallback text.
- Lazygit commonly draws panel titles on the top border and list footers on the bottom border
  rather than spending extra terminal rows on title-only views.
- In Lip Gloss, decide whether pane titles live:
  - inside the content area as row 0, with body content starting at row 2; or
  - on the border/top frame via a custom border/title renderer.
- Whichever approach is chosen, tests must assert the full rendered view:
  - has exactly `height` physical rows;
  - every row is `<= width` cells;
  - row 0 is visible and includes the top border or intended top header;
  - bottom row is visible and contains the footer/status.

Recommended render lifecycle:
1. Initialize the terminal screen backend before writing UI content.
2. Get terminal size from the backend.
3. Build a full layout from `(0,0,width,height)`.
4. Assign every pane explicit outer dimensions.
5. Derive each pane's inner content dimensions from border + padding rules.
6. Clear/redraw each pane's content area.
7. Draw frames, titles, subtitles, and footers after content.
8. Flush/show the screen.

Common cause of "top is missing":
- An off-by-one or off-by-two between outer frame dimensions and inner content dimensions.
- In gocui this often comes from content coordinates being inset by `+1,+1`.
- In Badger's current Lip Gloss rendering, the equivalent mistake was treating styled `Height`
  and `Width` as outer terminal dimensions when borders/padding add cells around them.

## Manual test

Run:

```bash
make build
./bin/badger fixtures/companies.db
```

Verify:
- Press `1`, select a b-tree row, and ensure the selected row is unchanged after pressing
  `3` and `4`.
- Press `1` from the detail/meta pane. `[1] B-TREES` receives focus styling.
- Press `2` from the detail/meta pane. `[2] PAGES` receives focus styling and loads page 1.
- Press `3`. The middle pane receives focus styling.
- Press `4`. The right pane receives focus styling.
- Select/load a page, press `3`, then use up/down. Page-structure selection moves inside
  the middle pane.
- Press `4`, then use `pgup`, `pgdown`, `home`, and arrows. The right pane scrolls without
  moving the left navigation cursor.
- Press `tab` and `shift+tab`; focus does not change.
