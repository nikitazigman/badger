# Proposal: Generic bbolt Inspector

> Branch: `feature/bbolt_inspector`
> Status: **Proposal.** No implementation yet.
> Scope: generic `go.etcd.io/bbolt` / BoltDB-compatible storage-engine inspection only.

This proposal describes adding support for inspecting bbolt database files as a second
storage engine in Badger.

The goal is not to build an etcd-specific tool. The goal is to let users open any project
database that uses bbolt as an embedded key/value store and inspect its physical layout:
meta pages, branch pages, leaf pages, buckets, key/value entries, freelist state, overflow
pages, and raw byte ranges.

---

## 1. Summary

bbolt is a single-file embedded key/value database used by many Go projects for local
metadata, indexes, queues, state stores, and service internals. It is a maintained fork of
BoltDB and is exposed through the Go module `go.etcd.io/bbolt`.

Supporting bbolt fits Badger well because the existing SQLite inspector is already built
around the same product idea:

- open a database file read-only;
- parse fixed-size pages;
- expose decoded page structures;
- walk b-trees;
- connect high-level objects to their physical pages;
- show raw bytes and decoded byte maps side by side.

bbolt is structurally simpler than SQLite in some areas. It has no SQL schema, no table
record format, and no SQLite serial types. The main complexity is bucket traversal and page
ownership rather than row decoding.

---

## 2. Product Goals

The generic bbolt inspector should let a user answer:

- Is this file a valid bbolt/BoltDB database?
- Which meta page is active?
- What page size is used?
- What pages exist in the file?
- Which pages are meta, branch, leaf, freelist, or overflow pages?
- What buckets exist, including nested buckets?
- Which page is the root of each bucket?
- Which key/value pairs are stored in a leaf page?
- Which branch page points to which child pages?
- Which pages are reachable from a bucket root?
- Which pages are free, allocated, unreachable, or suspicious?
- What raw byte range backs each decoded field?

This should be useful for all bbolt users, even when Badger cannot decode the application's
custom value payloads.

---

## 3. Non-Goals

These are intentionally out of scope for the generic engine feature:

- etcd-specific bucket semantics;
- etcd revision-key decoding;
- etcd protobuf value decoding;
- Raft WAL parsing;
- automatic project-specific codecs;
- writing, compacting, repairing, or migrating bbolt files;
- opening live databases with write locks or mutation support;
- replacing the bbolt CLI or application-level debugging tools.

Generic payload handling should stop at safe previews:

- raw bytes;
- hex;
- printable ASCII/UTF-8 preview;
- optional JSON-looking preview if the value is valid JSON;
- size and byte range.

Any richer decoding should be added later through explicit project-specific extensions.

---

## 4. Why bbolt Is a Good Fit

bbolt's physical model maps cleanly onto Badger's current architecture.

SQLite support already has:

- `Inspector.Open(path)` style file opening;
- fixed-size page reads;
- page-level parsing;
- b-tree walking;
- byte-range metadata;
- tests around headers, pages, payloads, and traversal;
- TUI concepts for pages and object-specific filtering.

bbolt needs a parallel engine package, not a rewrite:

```text
internal/sqlite/...
internal/bbolt/...
```

The generic UI can then learn that Badger supports multiple engines and choose the parser
based on file detection.

---

## 5. bbolt File Model

A bbolt file is organized as fixed-size pages. Important page concepts:

| Concept | Purpose |
| --- | --- |
| Meta pages | Store database metadata. bbolt keeps two meta pages and selects the valid/current one. |
| Branch pages | B+tree internal pages. Entries map keys to child page ids. |
| Leaf pages | B+tree leaf pages. Entries store key/value pairs or nested bucket headers. |
| Freelist pages | Track free page ids. |
| Overflow pages | Continuation pages for large logical pages spanning multiple physical pages. |
| Buckets | Logical key/value namespaces backed by b-tree roots. Buckets can be nested. |

Unlike SQLite, bbolt pages are identified by zero-based page ids in the file format. Badger
will need to present this clearly so the UI does not confuse SQLite-style page numbers with
bbolt page ids.

---

## 6. Proposed Parser Package

Add a new engine package:

```text
internal/bbolt/
  inspector.go
  meta.go
  page.go
  branch.go
  leaf.go
  bucket.go
  freelist.go
  overflow.go
  walk.go
```

Initial core types:

```go
type Inspector struct {
    path     string
    file     *os.File
    meta     Meta
    pageSize uint32
}

type Meta struct {
    Magic    uint32
    Version  uint32
    PageSize uint32
    Flags    uint32
    Root     BucketHeader
    Freelist PageID
    Pgid     PageID
    Txid     TxID
    Checksum uint64
}

type Page struct {
    ID       PageID
    Raw      []byte
    Header   PageHeader
    Branches []BranchElement
    Leaves   []LeafElement
}

type Bucket struct {
    Name       []byte
    RootPageID PageID
    Sequence   uint64
    Inline     bool
}
```

The exact field names should follow the bbolt source closely enough that maintainers can
cross-reference the upstream format without translation overhead.

---

## 7. MVP Scope

The MVP should be a page and metadata inspector.

Required:

- detect bbolt magic/version;
- read both meta pages;
- validate/select the active meta page;
- read arbitrary pages by page id;
- parse page headers;
- classify page type;
- parse branch elements;
- parse leaf elements;
- parse freelist pages;
- account for overflow pages;
- expose raw bytes for every decoded structure;
- show parse errors without crashing the TUI.

Useful MVP screens:

- database overview;
- meta page comparison;
- page list;
- selected page detail;
- selected field byte map;
- raw page bytes.

This version does not need full bucket navigation, but it should parse enough leaf structure
to make bucket traversal possible next.

Estimated effort: **3-5 focused days**.

---

## 8. Full Generic Inspector Scope

The complete generic bbolt feature should add logical navigation on top of page parsing.

Required:

- walk the root bucket;
- list top-level buckets;
- list nested buckets;
- show bucket root page ids;
- show key/value entries per bucket;
- show whether a bucket is inline or page-backed;
- filter pages by selected bucket;
- show branch-to-child relationships;
- show leaf entry key/value sizes;
- show free page ids and free spans;
- show reachable vs free vs unknown pages;
- detect cycles and invalid child pointers defensively.

Useful screens:

- `[1] MAIN`: overview, active meta, file stats;
- `[2] BUCKETS`: top-level and nested buckets;
- `[3] PAGES`: all pages or pages filtered to a bucket;
- `[4] FREELIST`: free page ids/spans and freelist page details.

Estimated effort: **1-2 weeks** for a useful generic browser.

Estimated effort for a polished Badger-quality version: **2-3 weeks**, including TUI polish,
fixtures, malformed-file tests, and page/bucket filtering.

---

## 9. Navigation Proposal

The bbolt engine should use a similar three-pane layout to the SQLite engine:

```text
Navigation | Explorer | Inspector
```

Suggested navigation sections:

| Section | Contents |
| --- | --- |
| `[1] MAIN` | Overview, active meta, meta page comparison |
| `[2] BUCKETS` | Bucket tree, including nested buckets |
| `[3] PAGES` | Physical pages, optionally filtered by bucket |
| `[4] FREELIST` | Free page ids/spans and freelist page structure |

Suggested commands:

| Key | Action |
| --- | --- |
| `1` | Jump to MAIN |
| `2` | Jump to BUCKETS |
| `3` | Jump to PAGES |
| `4` | Jump to FREELIST |
| `enter` | Open selected item |
| `f` | Filter pages to the selected bucket's reachable pages |
| `F` / `esc` | Clear active page filter |
| `tab` | Cycle panes |
| `q` | Quit |

Filtering should be bucket-scoped. Selecting a bucket and pressing `f` should show pages
reachable from that bucket root. Inline buckets may produce zero dedicated pages; that is a
valid filtered state, not an error.

---

## 10. Robustness Requirements

The parser should be strict about structural boundaries but tolerant at the workspace level.

Required behavior:

- never panic on malformed files;
- check every offset and length before slicing;
- report page truncation clearly;
- protect all walks with a visited set;
- surface skipped pages during bucket walks;
- keep already-parsed pages navigable after partial failures;
- distinguish "not a bbolt file" from "corrupt bbolt file";
- avoid loading huge values into display strings by default.

This matches the current SQLite traversal behavior where unreadable child pages degrade the
walk instead of crashing the whole view.

---

## 11. Test Strategy

Fixtures should be generated with the real bbolt library, then checked into `fixtures/`.

Suggested fixtures:

- empty database;
- one bucket with a few keys;
- nested buckets;
- many keys requiring branch pages;
- large values requiring overflow pages;
- deleted keys creating freelist entries;
- intentionally truncated file;
- intentionally corrupted page header;
- meta-page disagreement fixture.

Tests should cover:

- file detection;
- meta page parsing and active-meta selection;
- page header parsing;
- branch page parsing;
- leaf page parsing;
- freelist parsing;
- overflow accounting;
- bucket walking;
- reachable/free/unknown page classification;
- malformed-file error messages.

---

## 12. Implementation Plan

1. Add `internal/bbolt` with file detection, meta parsing, and page reads.
2. Add page header parsing and page type classification.
3. Add branch, leaf, freelist, and overflow parsing.
4. Add generated bbolt fixtures and parser tests.
5. Add bucket traversal and nested bucket listing.
6. Add page ownership/reachability model.
7. Add engine detection at app startup.
8. Add bbolt-specific TUI view models.
9. Add bucket page filtering.
10. Add malformed fixture tests and UI error states.

The first implementation should avoid sharing parser abstractions with SQLite prematurely.
Once both engines exist, common UI-facing concepts can be extracted if duplication becomes
real and painful.

---

## 13. Popularity / Usefulness

bbolt is not a general-purpose server database like Postgres or SQLite, but it is common in
Go infrastructure as an embedded database.

Current public signals:

- `go.etcd.io/bbolt` is imported by thousands of Go packages on pkg.go.dev.
- The bbolt README lists many public projects using Bolt/bbolt, including infrastructure,
  search, graph, container, and service-management tools.

This means a generic bbolt inspector would be useful beyond etcd. It would give Badger a
second strong storage-engine target while staying aligned with the project's educational
and physical-layout focus.

---

## 14. Open Questions

- Should Badger present bbolt page ids as zero-based ids everywhere, or display a
  user-facing "page N" label while preserving the raw id?
- Should inline buckets appear in the page filter with zero pages, or should they show the
  containing leaf page?
- Should value previews include best-effort JSON detection in the first version?
- Should the first release support old BoltDB files if they differ from modern bbolt in
  edge cases?
- Should engine detection be automatic only, or should users be able to force
  `--engine=bbolt` for damaged files?

