# Badger Project Scope Draft

## Working Title
Badger is a terminal-based learning and investigation tool for understanding how SQLite stores data on disk.

## Product Goal
The product should help a user open a SQLite database file and inspect how SQLite represents the database at the file, page, cell, and record level.

The tool is not just a parser dump. It should be an interactive teaching interface that connects:

- raw bytes
- SQLite page structures
- parsed field meanings
- table and schema context
- human-readable record values

The core value is to let a user move from "these are the bytes on disk" to "this is what they mean in SQLite."

## Product Positioning
Badger is primarily a learning tool. The main audience is people who want to use their own SQLite database files to understand how SQLite stores data internally.

That means the product should favor:

- explanatory labels and parsed meaning
- smooth drill-down from overview to bytes
- high visibility of metadata and structure context
- readable defaults over dense expert-only output

## Primary Use Cases
- A learner wants to understand the SQLite file format by exploring a real database interactively.
- A developer wants to inspect a specific page and see exactly how headers, cell pointers, and cells are laid out.
- A user wants to inspect a record payload and understand the decoded tuple values.
- A user wants to compare the raw on-disk representation with the parsed interpretation.

## Product Principles
- Byte-accurate: every visual block should correspond to real byte ranges in the file.
- Interactive: users should be able to drill down from high-level database info into page internals.
- Educational: labels, parsed views, and metadata should make structures understandable, but the MVP should stay compact rather than explanation-heavy.
- Traceable: every parsed structure should link back to offsets, sizes, and raw bytes.
- Safe: the first version should be read-only.

## Suggested MVP Definition
The first usable version should support:

- Opening a SQLite file from the CLI.
- Showing database-level metadata:
  - page size
  - page count
  - schema version
  - encoding
  - freelist info
  - number of tables and indexes
- Parsing the schema/root structures needed to populate the initial overview screen.
- Listing schema objects:
  - tables
  - indexes
  - root pages
- Opening any page by page number.
- Rendering page structure as ordered byte blocks, such as:
  - page header
  - cell pointer array
  - unallocated space
  - cell content area
  - freeblocks
- Selecting a block and seeing:
  - start offset
  - end offset
  - size
  - raw bytes
  - parsed fields
  - compact semantic labels
- Selecting a cell pointer and jumping to the referenced cell.
- Selecting a cell and seeing:
  - cell type
  - payload structure
  - rowid if present
  - overflow references if present
- Selecting a record payload and seeing:
  - record header
  - serial types
  - decoded values rendered in the correct logical type
  - raw byte slices
- Navigating pages sequentially.
- Keeping persistent metadata visible in a side panel while the user explores.
- Supporting mouse interaction in addition to keyboard navigation.

## Intended Full Product Scope
Your intended product target goes beyond the MVP and should eventually include all major structures relevant to understanding on-disk layout, including:

- database header
- b-tree pages
- cells
- records
- indexes
- overflow pages
- freeblocks
- fragmented and unallocated page space

The practical implementation plan should still sequence these capabilities, but the end-state product definition should include all of them.

## MVP Scope Boundaries
The MVP should intentionally exclude:

- page-to-table ownership mapping as a general feature
- filtering pages by table or index
- mapping decoded record tuples back to table columns
- SQL parsing to infer column meaning and affinity
- a dedicated raw hex exploration mode

The MVP should instead focus on showing parsed page blocks and allowing drill-down into those blocks.

## Non-Goals For MVP
- Editing or rewriting database files.
- WAL and rollback journal inspection.
- Full SQL query support inside the TUI.
- Visualization of every SQLite edge case on day one.
- Support for corrupted databases beyond best-effort parsing.

## Proposed User Flow
### 1. Open Database
User runs:

```bash
badger tui path/to/file.db
```

### 2. Land On Database Overview
The initial screen should immediately answer:

- What file is this?
- What are the main database settings?
- How many pages does it have?
- How many tables and indexes are present?
- What are the main schema objects?

This overview should be the default first screen every time a database is opened.

### 3. Choose Exploration Path
From the overview, the user should be able to:

- open a schema object
- open a page by page number
- open the page explorer
- inspect database header fields

### 4. Inspect Page Layout
When a page is opened, the user should see a structural layout of that page, broken into meaningful regions. Example:

```text
[Page Header][Cell Pointer Array][Unallocated Space][Cell 0][Cell 1][Cell 2]
```

The selected region should synchronize with a details pane.

### 5. Drill Into Parsed Structures
When a region is selected, the user should see both:

- raw representation
- parsed interpretation

Example drill-down path:

- page
- cell pointer
- cell
- payload
- record header
- serial type
- decoded value

### 6. Continue Navigation
The user should be able to move:

- back to page list
- to previous or next page
- to parent context like table or schema object
- to overflow pages if relevant

## Proposed Screen Model
The preferred direction is a single multi-pane screen rather than a stack of separate full-screen views.

The product can still switch focus between modes, but the mental model should be one workspace with synchronized panes.

### Main Workspace Panes
#### 1. Left Pane: Navigation
Purpose: hold the high-level navigation context.

Suggested contents:

- database overview sections
- tables and indexes
- page list

#### 2. Center Pane: Primary Explorer
Purpose: show the main currently selected entity.

Typical contents:

- overview summary
- selected page layout
- selected schema object summary

#### 3. Right Pane: Inspector
Purpose: always show metadata and parsed details for the current selection.

Typical contents:

- offsets
- sizes
- raw bytes
- parsed fields
- compact semantic labels

#### 4. Optional Bottom Pane: Hex / Status / Help
Purpose: show supporting low-level detail without displacing the main explorer.

Typical contents:

- keyboard hints
- current path or breadcrumb

This pane does not need to ship in the MVP if it slows down delivery of the core parsed-block workflow.

## Rendering Direction
Because this is a TUI, the rendering should optimize for clarity rather than full graphical fidelity.

Suggested rendering approach:

- left panel: navigation or object list
- center panel: current page structure or selected entity
- right panel: parsed details and explanations
- optional bottom panel: status / key hints

The right-side inspector should remain visible across the main workflow so metadata is always available as the user moves through the database.

For the page structure, the chosen direction is a hybrid page-layout canvas:

- blocks are shown in byte order
- blocks flow left to right and top to bottom
- each block keeps a minimum readable width
- cells are shown individually in the MVP
- selection drives the inspector

## Core Interactions
- Move selection with keyboard.
- Select entities with mouse.
- Open selected entity.
- Expand into nested parsed structures.
- Jump from pointer to target.
- Toggle between raw bytes and parsed interpretation.
- Toggle between page-relative offsets and file-relative offsets.
- Move to previous or next page.
- Return to previous context.

## Data Model The UI Needs
To support the UI well, the underlying parser layer should expose:

- database header model
- schema object model
- page summary model
- page layout segments with byte ranges
- parsed page header model
- parsed cell pointer model
- parsed cell model
- parsed payload model
- parsed record model

## Open Questions To Resolve
### Confirmed Decisions
1. The product is mainly a learning tool.
2. The default landing screen is the database overview.
3. The UI should use a single multi-pane workspace.
4. Navigation is page-first.
5. The page rendering direction should be a hybrid view.
6. The most important first-release journeys are:
   - open a DB and understand its layout
   - open a page and see how bytes map to cells
   - inspect a record payload and understand its values
7. The inspector should stay compact in the MVP.
8. The page explorer should show cells individually in byte order.
9. Wide terminals are the primary target.
10. Mouse support is required.
11. Page-to-table mapping, page filters by table, and raw hex mode are out of scope for the MVP.
12. Root/schema parsing is still required for the overview screen, but page ownership should not be shown as a general feature.
13. Full SQL text should be visible in schema object views.

### Remaining Product Questions
14. Is read-only inspection enough for the foreseeable roadmap, or do you already expect editing features later?
15. Do you want command-line shortcuts like `badger tui file.db --page 5` in the first milestone?

### Remaining Rendering Questions
16. No major rendering questions remain for the MVP.

### Remaining Relationship Questions
17. No open relationship questions remain for the MVP. Root/schema parsing is required only to populate the overview.

### Remaining Record Questions
18. Index payloads should be presented as index tuples, with clear labeling that they point into the table b-tree rather than being mapped to table columns.

### Remaining UI Questions
19. Mouse support should include click, scroll, and hover interactions.

### Remaining Teaching Questions
20. Do you want a later mode that explicitly links parsed output back to the SQLite file format spec?

### Remaining Scope Control Questions
21. The first demo is successful if a user can open a SQLite file, move between pages, understand the page format and storage order, and inspect cell headers and record tuples clearly.

## MVP Success Criteria
- User opens a SQLite file and immediately sees database-level metadata.
- User can move through pages interactively.
- User can inspect a page and understand its structural regions.
- User can select a cell and inspect its header and payload.
- User can inspect record tuples and understand the stored values.
- User can distinguish table pages from index pages at the page-structure level, even without full page-to-table ownership mapping.

## Recommended MVP Statement
Build a read-only Bubble Tea TUI that opens a SQLite database into a multi-pane workspace, starts on a database overview, keeps metadata visible in a persistent inspector pane, lets the user browse pages directly, renders each page as a continuous byte-ordered layout of structural blocks, and supports drill-down from page blocks into parsed SQLite structures and decoded record values with raw byte slices.

## Recommended Next Step
Answer the open questions above, then turn this document into:

- product definition
- view/state model
- keyboard interaction spec
- milestone plan
