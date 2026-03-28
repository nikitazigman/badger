# Badger TUI Design

## Purpose
This document translates the product scope into a concrete TUI design for the MVP.

It focuses on:

- workspace layout
- pane responsibilities
- state model
- navigation model
- keyboard and mouse interactions
- page rendering strategy
- inspector behavior
- MVP implementation boundaries

## Design Goals
- Let the user understand a database quickly from the first screen.
- Keep the UI compact and information-dense without becoming cryptic.
- Make page structure navigation feel direct and low-friction.
- Keep metadata visible while drilling into lower-level structures.
- Use one stable multi-pane workspace instead of several disconnected screens.

## MVP Summary
The MVP TUI should:

- open a SQLite file into a single workspace
- land on a database overview
- let the user move from overview to page inspection
- render page regions as a selectable ordered list in byte order
- let the user select page structures directly from that list
- show full cell information in the inspector when a cell is selected
- show raw bytes first inside the inspector for the current selection
- support both keyboard and mouse interactions

The MVP should not:

- provide a dedicated raw hex exploration mode
- map arbitrary pages back to tables
- filter pages by table or index
- map record values back to schema columns

## Workspace Layout
The interface should use three main panes.

```text
+--------------------+--------------------------------------+---------------------------+
| Navigation         | Explorer                             | Inspector                 |
|                    |                                      |                           |
| overview           | database summary                     | selected item details     |
| tables             | or                                   | offsets                   |
| indexes            | page structure                       | sizes                     |
| pages              | or                                   | parsed fields             |
|                    | selected object summary              | raw byte slices           |
|                    |                                      |                           |
+--------------------+--------------------------------------+---------------------------+
| Status / Hints / Breadcrumbs                                                       |
+------------------------------------------------------------------------------------+
```

## Pane Responsibilities
### Navigation Pane
The left pane is the primary navigation surface.

It should contain:

- overview entry
- database header entry
- tables section
- indexes section
- pages section

The pages section should allow quick movement by page number. For large databases it should be scrollable and virtualized if needed later.

### Explorer Pane
The center pane is the main content area.

It should render one of these content modes:

- database overview
- database header detail
- schema object summary
- page explorer

This pane is the main place where users understand structure and move deeper.

### Inspector Pane
The right pane is persistent and always tied to the current selection.

It should show:

- selection type
- page number if applicable
- page-relative offset
- file-relative offset when applicable
- byte length
- raw byte slice preview
- parsed field/value list
- compact semantic labels

It should not try to teach aggressively in the MVP. Labels should be short and direct.

### Status Bar
The bottom bar should show:

- current focus pane
- current path or breadcrumb
- main key hints
- hover or selection hint text

The status bar can stay lightweight in the MVP.

## Focus Model
The workspace should maintain a single active focus target.

There are two related concepts:

- focused pane
- selected item within that pane

Focus should move between panes with keyboard shortcuts and mouse clicks.

Suggested focus order:

1. navigation
2. explorer
3. inspector

The inspector normally reflects selection from another pane, but can still receive focus for scrolling.

## Primary Modes
The app should use one workspace with a small number of central content modes.

### Mode: Overview
Explorer shows top-level database summary.

Suggested overview sections:

- file path
- page size
- page count
- database size
- encoding
- freelist info
- schema object counts
- top tables
- top indexes

The user should be able to select summary items that jump to a more specific target, such as the database header or a page.

### Mode: Schema Object
Explorer shows a summary of a selected table or index.

For MVP, this should show:

- object type
- object name
- root page
- full SQL definition if available

This mode is mostly a bridge into page exploration rather than a full schema browser.

The full SQL definition should be fully visible in a scrollable region rather than truncated.

### Mode: Page Explorer
Explorer shows the currently selected page and its structural regions.

This is the main MVP mode after overview.

## Page Explorer Design
### Goals
- Make page layout understandable at a glance.
- Preserve byte-order truth.
- Render page structures as a readable ordered list.
- Avoid duplicating the same information in both a visual canvas and a table.
- Allow the inspector to tell the full story of the selected structure.

### Page Header Strip
At the top of the explorer pane, show a compact page summary line.

Suggested fields:

- page number
- page kind
- page size
- cell count
- right-most pointer if interior
- freeblock offset
- cell content area start
- fragmented free bytes

Example:

```text
Page 7 | table leaf | size 4096 | cells 14 | cell area 3620 | freeblock 0 | frag 0
```

### Structural List View
The main page display should show the page as a scrollable ordered list of page structures.

Suggested columns:

- kind
- range
- size
- notes

Example:

```text
Kind            Range          Size    Notes
Header          0..7           8b      leaf hdr
Pointer Array   8..15          8b      4 offsets
Cell 3          3810..3884     75b     tbl leaf
Cell 2          3885..3949     65b     tbl leaf
Cell 1          3950..4019     70b     tbl leaf
Cell 0          4020..4095     76b     tbl leaf
```

Behavior:

- each row corresponds to a real byte range
- rows are sorted in page byte order
- selection is shown by color and highlight rather than extra symbols in the final UI
- the list is scrollable for large pages
- the inspector updates from the selected row

### List Rules
The page list should follow these rules:

- always show the page header as its own row
- always show the full cell pointer array as its own row
- show freeblocks as separate rows when present
- show unallocated regions as separate rows when present
- show cells individually in byte order
- do not group cell ranges in the MVP

### Selection Model
The explorer should support direct selection of page structures, but deep parsing should render in the inspector rather than replacing the explorer layout.

Expected selection flow:

1. page
2. page structure row
3. inspector sub-selection such as pointer entry or value bytes

The selected explorer row should drive the inspector contents.

## Inspector Design
### Inspector Sections
The inspector should render sections in a consistent order:

1. identity
2. raw bytes
3. byte map
4. parsed fields
5. decoded values or entries

### Identity Section
Show:

- selected type
- page number
- parent item
- subtype if applicable

Examples:

- `Page Header`
- `Cell Pointer 3`
- `Table Leaf Cell 5`
- `Record Header`

### Offsets Section
Show inside the identity section:

- page-relative start
- page-relative end
- file-relative start
- file-relative end
- total byte size

### Raw Bytes Section
The MVP should not implement a full raw-view mode, but the inspector should still show raw bytes for the current selection first.

Suggested representation:

- colored or highlighted hex byte slice
- truncation when large
- optional ASCII preview when useful

### Byte Map Section
The inspector should explain how parts of the raw bytes map to parsed meanings.

Examples:

- payload size bytes
- rowid bytes
- record header size bytes
- serial type bytes
- record body bytes
- pointer entry bytes

### Parsed Fields Section
This is the structured interpretation of the selected bytes.

Examples:

- page type
- first freeblock offset
- number of cells
- cell content area start
- rowid
- payload size
- overflow page number
- record header size
- serial types
- decoded value

### Decoded Values Or Entries Section
This section should show the highest-level human-readable interpretation for the current selection.

Examples:

- decoded tuple values for a table leaf cell
- pointer targets for a pointer array
- field value for a database header entry

## Record and Payload Rendering
### Table Leaf Payload
When a table leaf cell is selected, the inspector should show the full parsed story for that cell in one place.

Show:

- payload size
- rowid if present
- record header size
- serial types
- tuple values
- raw slices per value

Values should be rendered by logical type where possible:

- integers as numbers
- text as strings
- blobs as hex
- null explicitly as null

### Index Payload
For index pages, the payload should be clearly labeled as an index tuple.

The MVP should explain it minimally:

- this is an index tuple
- it participates in index ordering
- it references table content through SQLite b-tree relationships

The UI does not need to map it to table columns yet.

## Navigation Model
### Keyboard
Suggested baseline keymap:

- `tab`: move focus to next pane
- `shift+tab`: move focus to previous pane
- `up` / `down`: move selection
- `left` / `right`: optional future use for inspector sub-selection
- `enter`: open selected navigation item
- `esc`: move up one level
- `[` / `]`: previous page / next page
- `g`: jump to overview
- `p`: focus pages section
- `h`: focus database header
- `q`: quit

This keymap can be refined later, but the MVP should keep it small.

### Mouse
Mouse support should include:

- click to focus a pane
- click to select an item
- click on actionable navigation items to open
- scroll within lists and inspector
- hover to update lightweight status hints only

Hover should not be required for core use. It should not drive the inspector in the MVP.

## State Model
The runtime state should be explicit and predictable.

### App State
Suggested top-level fields:

- loaded file path
- parsed database metadata
- schema object list
- page summaries
- active workspace mode
- focused pane
- current page number
- navigation selection state
- explorer selection state
- inspector scroll state
- transient status message

### Selection State
The selected entity should be represented as a typed path.

Example conceptual shape:

```text
Page(7) -> Cell(3)
```

This avoids ambiguous inspector rendering.

### Derived View Models
The UI layer should derive render-friendly models instead of rendering parser structs directly.

Suggested derived models:

- overview summary model
- navigation tree model
- page row model
- structural row model
- inspector section model

This keeps parsing concerns separate from rendering concerns.

## Data Contract Between Parser And TUI
The TUI will need stable structures from the parser layer.

Minimum required contracts:

- database metadata summary
- schema object summary
- page summary
- parsed page layout with byte ranges
- parsed cell pointer list
- parsed cells
- parsed payload summary
- parsed record summary

Each parsed structure should expose:

- stable type
- byte range
- raw bytes or byte slice reference
- parsed fields
- child items

## Error Handling
The TUI should degrade cleanly when a structure cannot be fully parsed.

Behavior:

- keep the page navigable
- show parse failure in the inspector
- still show raw byte range when possible
- avoid crashing the workspace

## Layout Constraints
The MVP targets wide terminals.

Assumptions:

- three-pane layout is the default
- no alternative compact mobile-like layout is needed
- resizing should still work, but narrow layouts are not a design priority

## Visual Style Direction
The interface should feel technical and deliberate rather than decorative.

Style guidance:

- strong pane separation
- consistent highlighting for current focus and current selection
- restrained color usage by structure type
- compact tables and labels
- minimal prose

Good candidates for color coding:

- page headers
- pointer arrays
- cells
- byte-map regions inside the inspector
- free space
- index vs table pages

## Main Views
The following wireframes describe the main MVP screens.

### 1. Database Overview
```text
+----------------------+----------------------------------------------------+---------------------------+
| Navigation           | Overview                                           | Inspector                 |
|                      |                                                    |                           |
| > Overview           | File: fixtures/sample.db                           | Selected: Overview        |
|   DB Header          | Page size: 4096                                    |                           |
|                      | Page count: 18                                     | Summary                   |
| Tables               | DB size: 73728 bytes                               | - file path               |
|   users              | Encoding: UTF-8                                    | - page size               |
|   posts              | Freelist pages: 0                                  | - page count              |
|   comments           | Tables: 3                                          | - db size                 |
|                      | Indexes: 2                                         | - encoding                |
| Indexes              |                                                    |                           |
|   idx_users_email    | Tables                                             | Actions                   |
|   idx_posts_user_id  | - users           root page 2                      | - open DB header          |
|                      | - posts           root page 5                      | - open page list          |
| Pages                | - comments        root page 8                      | - open schema object      |
|   page 1             |                                                    |                           |
|   page 2             | Indexes                                            |                           |
|   page 3             | - idx_users_email    root page 11                  |                           |
|   ...                | - idx_posts_user_id root page 14                   |                           |
+----------------------+----------------------------------------------------+---------------------------+
```

### 2. Database Header View
```text
+----------------------+----------------------------------------------------+---------------------------+
| Navigation           | Database Header                                    | Inspector                 |
|                      |                                                    |                           |
|   Overview           | Field              Range        Size    Value      | Selected: Page Size       |
| > DB Header          | Header String      0..15        16b     SQLite...  |                           |
|                      | Page Size          16..17       2b      4096       | Raw Bytes                 |
| Tables               | Write Version      18..18       1b      1          | 10 00                     |
|   users              | Read Version       19..19       1b      1          |                           |
|   posts              | Reserved Space     20..20       1b      0          | Byte Map                  |
|                      | Max Payload Frac   21..21       1b      64         | [10 00] page size         |
| Indexes              | Min Payload Frac   22..22       1b      32         |                           |
|   idx_users_email    | Leaf Payload Frac  23..23       1b      32         | Parsed                    |
|                      | Change Counter     24..27       4b      ...        | - field: page size        |
| Pages                | DB Size            28..31       4b      18 pages   | - value: 4096             |
|   page 1             | Freelist Trunk     32..35       4b      0          |                           |
|   page 2             | Freelist Count     36..39       4b      0          | Meaning                   |
|   page 3             | Schema Cookie      40..43       4b      ...        | Size of each DB page      |
+----------------------+----------------------------------------------------+---------------------------+
```

### 3. Schema Object View
```text
+----------------------+----------------------------------------------------+---------------------------+
| Navigation           | Table: users                                       | Inspector                 |
|                      |                                                    |                           |
|   Overview           | Summary                                            | Selected: users           |
|   DB Header          | Type: table                                        |                           |
| Tables               | Name: users                                        | Raw Bytes                 |
| > users              | Root page: 2                                       | n/a                       |
|   posts              |                                                    |                           |
|   comments           | SQL                                                | Parsed                    |
|                      | CREATE TABLE users (                               | - type: table             |
| Indexes              |   id INTEGER PRIMARY KEY,                          | - name: users             |
|   idx_users_email    |   email TEXT NOT NULL,                             | - root page: 2            |
|                      |   name TEXT                                        |                           |
| Pages                | );                                                 | Actions                   |
|   page 1             |                                                    | - open root page          |
|   page 2             |                                                    |                           |
|   page 3             |                                                    | Meaning                   |
|                      |                                                    | Schema definition for     |
|                      |                                                    | this table                |
+----------------------+----------------------------------------------------+---------------------------+
```

### 4. Page View With Header Selected
```text
+----------------------+----------------------------------------------------+---------------------------+
| Navigation           | Page 2 | table leaf | cells 4 | free 0 | frag 0  | Inspector                 |
|                      |                                                    |                           |
| Pages                | Kind            Range          Size    Notes       | Selected: Header          |
|   page 1             | > Header         0..7           8b      leaf hdr   |                           |
| > page 2             |   Pointer Array  8..15          8b      4 offsets  | Raw Bytes                 |
|   page 3             |   Cell 3         3810..3884     75b     tbl leaf   | 0d 00 00 04 0e e2 00 00   |
|                      |   Cell 2         3885..3949     65b     tbl leaf   |                           |
|                      |   Cell 1         3950..4019     70b     tbl leaf   | Byte Map                  |
|                      |   Cell 0         4020..4095     76b     tbl leaf   | [0d] page type            |
|                      |                                                    | [00 00] first freeblock   |
|                      |                                                    | [04] number of cells      |
|                      |                                                    | [0e e2] cell area start   |
|                      |                                                    | [00] fragmented bytes     |
|                      |                                                    |                           |
|                      |                                                    | Parsed                    |
|                      |                                                    | - page type: table leaf   |
|                      |                                                    | - first freeblock: 0      |
|                      |                                                    | - cells: 4                |
|                      |                                                    | - cell area start: 3810   |
+----------------------+----------------------------------------------------+---------------------------+
```

### 5. Page View With Pointer Array Selected
```text
+----------------------+----------------------------------------------------+---------------------------+
| Navigation           | Page 2 | table leaf | cells 4 | free 0 | frag 0  | Inspector                 |
|                      |                                                    |                           |
| Pages                | Kind            Range          Size    Notes       | Selected: Pointer Array   |
|   page 1             |   Header         0..7           8b      leaf hdr   |                           |
| > page 2             | > Pointer Array  8..15          8b      4 offsets  | Raw Bytes                 |
|   page 3             |   Cell 3         3810..3884     75b     tbl leaf   | 0f b4 0f 6e 0f 2d 0e e2   |
|                      |   Cell 2         3885..3949     65b     tbl leaf   |                           |
|                      |   Cell 1         3950..4019     70b     tbl leaf   | Byte Map                  |
|                      |   Cell 0         4020..4095     76b     tbl leaf   | [0f b4] ptr 0 -> 4020     |
|                      |                                                    | [0f 6e] ptr 1 -> 3950     |
|                      |                                                    | [0f 2d] ptr 2 -> 3885     |
|                      |                                                    | [0e e2] ptr 3 -> 3810     |
|                      |                                                    |                           |
|                      |                                                    | Parsed Entries            |
|                      |                                                    | - ptr 0 -> Cell 0         |
|                      |                                                    | - ptr 1 -> Cell 1         |
|                      |                                                    | - ptr 2 -> Cell 2         |
|                      |                                                    | - ptr 3 -> Cell 3         |
+----------------------+----------------------------------------------------+---------------------------+
```

### 6. Page View With Table Leaf Cell Selected
```text
+----------------------+----------------------------------------------------+---------------------------+
| Navigation           | Page 2 | table leaf | cells 4 | free 0 | frag 0  | Inspector                 |
|                      |                                                    |                           |
| Pages                | Kind            Range          Size    Notes       | Selected: Cell 1          |
|   page 1             |   Header         0..7           8b      leaf hdr   |                           |
| > page 2             |   Pointer Array  8..15          8b      4 offsets  | Raw Bytes                 |
|   page 3             |   Cell 3         3810..3884     75b     tbl leaf   | 2d 03 04 00 23 15 61 6c...|
|                      |   Cell 2         3885..3949     65b     tbl leaf   |                           |
|                      | > Cell 1         3950..4019     70b     tbl leaf   | Byte Map                  |
|                      |   Cell 0         4020..4095     76b     tbl leaf   | [2d] payload size         |
|                      |                                                    | [03] rowid                |
|                      |                                                    | [04] record hdr size      |
|                      |                                                    | [00 23 15] serial types   |
|                      |                                                    | [61 6c 69 ...] values     |
|                      |                                                    |                           |
|                      |                                                    | Cell Header               |
|                      |                                                    | - payload size: 45        |
|                      |                                                    | - rowid: 3                |
|                      |                                                    | - first overflow: none    |
|                      |                                                    |                           |
|                      |                                                    | Record                    |
|                      |                                                    | - header size: 4          |
|                      |                                                    | - serial types: 0,23,21   |
|                      |                                                    | - values:                 |
|                      |                                                    |   1. NULL                 |
|                      |                                                    |   2. "alice@example.com"  |
|                      |                                                    |   3. "Alice"              |
+----------------------+----------------------------------------------------+---------------------------+
```

### 7. Page View With Index Leaf Cell Selected
```text
+----------------------+----------------------------------------------------+---------------------------+
| Navigation           | Page 11 | index leaf | cells 3 | free 0 | frag 0 | Inspector                 |
|                      |                                                    |                           |
| Pages                | Kind            Range          Size    Notes       | Selected: Cell 0          |
|   page 10            |   Header         0..7           8b      index hdr  |                           |
| > page 11            |   Pointer Array  8..13          6b      3 offsets  | Raw Bytes                 |
|   page 12            | > Cell 0         4040..4095     56b     idx leaf   | 20 17 09 61 6c 69 63...   |
|                      |   Cell 1         3980..4039     60b     idx leaf   |                           |
|                      |   Cell 2         3915..3979     65b     idx leaf   | Byte Map                  |
|                      |                                                    | [20] payload size         |
|                      |                                                    | [17 09] serial types      |
|                      |                                                    | [61 6c 69 ...] values     |
|                      |                                                    |                           |
|                      |                                                    | Meaning                   |
|                      |                                                    | An index leaf cell stores |
|                      |                                                    | an index tuple            |
|                      |                                                    |                           |
|                      |                                                    | Index Tuple               |
|                      |                                                    | - value 1: "alice@..."    |
|                      |                                                    | - value 2: row reference  |
+----------------------+----------------------------------------------------+---------------------------+
```

### 8. Page View With Freeblock Selected
```text
+----------------------+----------------------------------------------------+---------------------------+
| Navigation           | Page 6 | table leaf | cells 3 | free 1 | frag 2   | Inspector                 |
|                      |                                                    |                           |
| Pages                | Kind            Range          Size    Notes       | Selected: Freeblock 0     |
|   page 5             |   Header         0..7           8b      leaf hdr   |                           |
| > page 6             |   Pointer Array  8..13          6b      3 offsets  | Raw Bytes                 |
|   page 7             | > Freeblock 0    120..143       24b     freeblock  | 00 00 00 18 ...           |
|                      |   Unallocated    144..201       58b     unused     |                           |
|                      |   Cell 2         3890..3955     66b     tbl leaf   | Byte Map                  |
|                      |   Cell 1         3956..4020     65b     tbl leaf   | [00 00] next freeblock    |
|                      |   Cell 0         4021..4095     75b     tbl leaf   | [00 18] size              |
|                      |                                                    |                           |
|                      |                                                    | Parsed                    |
|                      |                                                    | - next freeblock: 0       |
|                      |                                                    | - size: 24                |
+----------------------+----------------------------------------------------+---------------------------+
```

### 9. Partial Parse View
```text
+----------------------+----------------------------------------------------+---------------------------+
| Navigation           | Page 9 | unknown / partial parse                      | Inspector                 |
|                      |                                                    |                           |
| Pages                | Kind            Range          Size    Notes       | Selected: Unknown Block   |
|   page 8             |   Header         0..11          12b     partial    |                           |
| > page 9             |   Pointer Array  12..21         10b     partial    | Raw Bytes                 |
|   page 10            | > Unknown Block  3500..3600     100b    parse err  | ab 04 88 ...              |
|                      |   Cell 0         4020..4095     76b     partial    |                           |
|                      |                                                    | Parse Status              |
|                      |                                                    | - partial parse           |
|                      |                                                    | - unexpected varint       |
|                      |                                                    |                           |
|                      |                                                    | Meaning                   |
|                      |                                                    | The page stays navigable  |
+----------------------+----------------------------------------------------+---------------------------+
```

## Transitions
The MVP should keep transitions simple and predictable.

### Global Rules
- The navigation pane changes the explorer mode.
- The explorer selection changes the inspector contents.
- The inspector should not replace the explorer view in the MVP.
- Selecting a cell should immediately show both cell header data and record data in the inspector.

### Transition: App Open -> Overview
1. User runs `badger tui file.db`.
2. App loads database metadata and schema summaries.
3. Explorer opens in `Overview`.
4. Inspector shows overview metadata.

### Transition: Overview -> Database Header
1. User selects `DB Header` in the navigation pane or overview actions.
2. Explorer switches to the database header field list.
3. Inspector shows the selected header field.

### Transition: Overview -> Schema Object
1. User selects a table or index in the navigation pane or overview summary.
2. Explorer switches to the schema object summary.
3. Inspector shows summary metadata for that object.

### Transition: Overview Or Schema Object -> Page
1. User selects a page from the navigation pane, or opens a root page from a schema object.
2. Explorer switches to `Page Explorer`.
3. Explorer shows the ordered page structure list for that page.
4. Inspector shows the selected row from that page, defaulting to the first structural row.

### Transition: Page Row -> Inspector Update
1. User moves up or down in the page structure list, or clicks a row.
2. Explorer remains on the same page and only changes the selected row.
3. Inspector refreshes to show raw bytes, byte map, parsed fields, and decoded values for that row.

### Transition: Header Row Selected
1. User selects `Header`.
2. Inspector shows page-header bytes and decoded page-header fields.

### Transition: Pointer Array Row Selected
1. User selects `Pointer Array`.
2. Inspector shows pointer-array bytes and decoded pointer entries.
3. The explorer remains unchanged.

### Transition: Cell Row Selected
1. User selects a `Cell N` row.
2. Inspector shows:
   - raw bytes
   - byte map
   - cell header fields
   - record fields
   - decoded values
3. No extra page or inspector subview is required for the MVP.

### Transition: Page -> Previous Or Next Page
1. User presses `[` or `]`, or chooses a nearby page in navigation.
2. Explorer switches to the new page.
3. Inspector resets to the default selected row for that page.

### Transition: Any View -> Back
1. User presses `Esc`.
2. If focus is in inspector scroll state, it stops scrolling first.
3. Otherwise the app returns to the previous higher-level explorer mode when applicable.
4. On page views, `Esc` should return to the previously visited overview or schema context if that context opened the page.

## MVP Milestones
### Milestone 1: Workspace Shell
- Bubble Tea app boots
- three-pane layout renders
- file loads
- overview renders
- navigation and focus work

### Milestone 2: Page Explorer
- page list works
- page explorer renders a scrollable ordered structure list
- inspector shows raw bytes, byte map, and parsed row metadata

### Milestone 3: Cell and Payload Drill-Down
- cell pointers are selectable
- cells are selectable
- selecting a cell shows full cell header and record details
- record tuples render with typed values and raw slices

### Milestone 4: Mouse and Polish
- mouse click support
- scroll support
- hover hints
- status bar polish
- parse error presentation

## Recommended Next Step
Use this design doc to define:

- Bubble Tea model hierarchy
- view model interfaces
- keymap constants
- first pass of the page explorer renderer
