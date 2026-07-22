package storage

import (
	"encoding/hex"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/nikitazigman/badger/internal/sqlite"
)

const (
	blockDatabaseHeader    = "database_header"
	blockPageHeader        = "page_header"
	blockPointerArray      = "pointer_array"
	blockFreeblock         = "freeblock"
	blockUnallocated       = "unallocated"
	blockTableLeafCell     = "table_leaf_cell"
	blockTableInteriorCell = "table_interior_cell"
	blockIndexLeafCell     = "index_leaf_cell"
	blockIndexInteriorCell = "index_interior_cell"
	blockOverflowNextPage  = "overflow_next_page"
	blockOverflowPayload   = "overflow_payload"
	blockFreelistHeader    = "freelist_header"
	blockFreelistLeafPage  = "freelist_leaf_page"
	blockRawPage           = "raw_page"

	blockPayloadSize      = "payload_size"
	blockRowID            = "rowid"
	blockLeftChildPage    = "left_child_page"
	blockRecordPayload    = "record_payload"
	blockRecordHeaderSize = "record_header_size"
	blockSerialType       = "serial_type"
	blockRecordValue      = "record_value"
	blockOverflowPointer  = "overflow_pointer"
)

type sqliteDatabase struct {
	inspector *sqlite.Inspector
	overview  *DatabaseOverview
	btrees    map[BTreeID]BTreeItem
}

func openSQLite(path string) (Database, error) {
	inspector, err := sqlite.Open(path)
	if err != nil {
		return nil, err
	}
	return &sqliteDatabase{inspector: inspector}, nil
}

func (db *sqliteDatabase) Close() error {
	return db.inspector.Close()
}

func (db *sqliteDatabase) Engine() Engine {
	return EngineSQLite
}

func (db *sqliteDatabase) Overview() (*DatabaseOverview, error) {
	if db.overview != nil {
		return db.overview, nil
	}

	metadata, err := db.inspector.InspectDatabaseMetadata()
	if err != nil {
		return nil, err
	}

	overview := &DatabaseOverview{
		Path:              metadata.Path,
		PageSizeBytes:     uint64(metadata.DBHeader.PageSize),
		PageCount:         uint64(metadata.DBHeader.DatabasePageCount),
		FirstPageID:       1,
		DatabaseSizeBytes: uint64(metadata.DBHeader.PageSize) * uint64(metadata.DBHeader.DatabasePageCount),
		HeaderRows:        sqliteHeaderRows(metadata.DBHeader),
	}

	btrees := []BTreeItem{{
		ID:       "table:sqlite_schema",
		Kind:     BTreeTable,
		Name:     "sqlite_schema",
		RootPage: &PageRef{ID: 1},
		System:   true,
		Rows: []Field{
			{Label: "Type", Value: "table"},
			{Label: "Name", Value: "sqlite_schema"},
			{Label: "Table", Value: "sqlite_schema"},
			{Label: "SQL", Value: ""},
		},
	}}

	for _, row := range metadata.SchemaRecords {
		typ := stringValue(row["type"])
		name := stringValue(row["name"])
		tableName := stringValue(row["tbl_name"])
		rootPage := uint64Value(row["rootpage"])
		sqlText := stringValue(row["sql"])

		if name == "sqlite_schema" && typ == "table" {
			continue
		}

		kind := BTreeRootless
		switch typ {
		case "table":
			kind = BTreeTable
		case "index":
			kind = BTreeIndex
		default:
			if rootPage != 0 {
				continue
			}
		}

		var root *PageRef
		if rootPage != 0 {
			root = &PageRef{ID: rootPage}
		}

		btrees = append(btrees, BTreeItem{
			ID:       BTreeID(typ + ":" + name),
			Kind:     kind,
			Name:     name,
			RootPage: root,
			Rows: []Field{
				{Label: "Type", Value: typ},
				{Label: "Name", Value: name},
				{Label: "Table", Value: tableName},
				{Label: "SQL", Value: sqlText},
			},
		})
	}

	db.btrees = make(map[BTreeID]BTreeItem, len(btrees))
	for _, item := range btrees {
		db.btrees[item.ID] = item
	}
	overview.BTrees = btrees
	db.overview = overview
	return overview, nil
}

func (db *sqliteDatabase) InspectPage(ref PageRef) (*PageInspection, error) {
	if ref.ID == 0 || ref.ID > uint64(^uint32(0)) {
		return nil, fmt.Errorf("page number %d out of range", ref.ID)
	}
	page, err := db.inspector.InspectPage(uint32(ref.ID))
	if err != nil {
		return nil, err
	}
	return adaptSQLitePage(page), nil
}

func (db *sqliteDatabase) PagesForBTree(id BTreeID) ([]PageRef, error) {
	if db.btrees == nil {
		if _, err := db.Overview(); err != nil {
			return nil, err
		}
	}
	item, ok := db.btrees[id]
	if !ok {
		return nil, fmt.Errorf("b-tree %q not found", id)
	}
	if item.RootPage == nil {
		return []PageRef{}, nil
	}
	if item.RootPage.ID > uint64(^uint32(0)) {
		return nil, fmt.Errorf("root page %d out of range", item.RootPage.ID)
	}
	walk, err := db.inspector.PagesForRoot(uint32(item.RootPage.ID))
	if err != nil {
		return nil, err
	}
	pages := make([]PageRef, 0, len(walk.Pages))
	for _, page := range walk.Pages {
		pages = append(pages, PageRef{ID: uint64(page)})
	}
	return pages, nil
}

func sqliteHeaderRows(header sqlite.DBHeader) []Field {
	return []Field{
		{Label: "Page size", Value: strconv.FormatUint(uint64(header.PageSize), 10)},
		{Label: "Page count", Value: strconv.FormatUint(uint64(header.DatabasePageCount), 10)},
		{Label: "Read version", Value: strconv.FormatUint(uint64(header.ReadVersion), 10)},
		{Label: "Write version", Value: strconv.FormatUint(uint64(header.WriteVersion), 10)},
		{Label: "Reserved bytes/page", Value: strconv.FormatUint(uint64(header.ReservedBytesPerPage), 10)},
		{Label: "Freelist pages", Value: strconv.FormatUint(uint64(header.FreelistPageCount), 10)},
		{Label: "Schema cookie", Value: strconv.FormatUint(uint64(header.SchemaCookie), 10)},
		{Label: "Schema format", Value: strconv.FormatUint(uint64(header.SchemaFormat), 10)},
		{Label: "Encoding", Value: textEncodingLabel(header.TextEncoding)},
		{Label: "User version", Value: strconv.FormatUint(uint64(header.UserVersion), 10)},
		{Label: "Application ID", Value: strconv.FormatUint(uint64(header.ApplicationID), 10)},
		{Label: "SQLite version", Value: sqliteVersionLabel(header.SQLiteVersionNumber)},
	}
}

func adaptSQLitePage(page *sqlite.PageInspection) *PageInspection {
	switch page.Format {
	case sqlite.PageFormatOverflow:
		return adaptSQLiteOverflowPage(page)
	case sqlite.PageFormatFreelistTrunk:
		return adaptSQLiteFreelistTrunkPage(page)
	case sqlite.PageFormatFreelistLeaf:
		return adaptSQLiteFreelistLeafPage(page)
	case sqlite.PageFormatUnknown:
		return adaptSQLiteUnknownPage(page)
	}

	header := page.BTreePage.PageHeader
	pointerBytes := len(page.BTreePage.CellPointerArray.Pointers) * 2
	unallocatedBytes := 0
	for _, region := range page.BTreePage.UnallocatedRegions {
		unallocatedBytes += region.Meta.Size
	}

	rows := []Field{
		{Label: "Page", Value: strconv.FormatUint(uint64(page.PageNumber), 10)},
		{Label: "Type", Value: sqlitePageKindLabel(header.PageKind.Value)},
		{Label: "Page size", Value: fmt.Sprintf("%d bytes", len(page.BTreePage.Raw))},
		{Label: "File offset", Value: strconv.FormatUint(uint64(page.PageNumber-1)*uint64(len(page.BTreePage.Raw)), 10)},
		Blank(),
		Section("STRUCTURE"),
		{Label: "Cells", Value: strconv.FormatUint(uint64(header.CellCount.Value), 10)},
		{Label: "Cell content start", Value: strconv.FormatUint(uint64(header.CellContentAreaOffset.Value), 10)},
		{Label: "First freeblock", Value: strconv.FormatUint(uint64(header.FirstFreeblock.Value), 10)},
		{Label: "Fragmented free bytes", Value: strconv.FormatUint(uint64(header.FragmentedFreeBytes.Value), 10)},
		{Label: "Pointer array", Value: fmt.Sprintf("%d bytes", pointerBytes)},
		{Label: "Freeblocks", Value: strconv.Itoa(len(page.BTreePage.Freeblocks))},
		{Label: "Unallocated", Value: fmt.Sprintf("%d bytes", unallocatedBytes)},
	}

	return &PageInspection{
		Ref:       PageRef{ID: uint64(page.PageNumber)},
		Raw:       append([]byte(nil), page.Raw...),
		Rows:      rows,
		HexBlocks: sqlitePageBlocks(page),
	}
}

func adaptSQLiteOverflowPage(page *sqlite.PageInspection) *PageInspection {
	overflow := page.OverflowPage
	if overflow == nil {
		return adaptSQLiteUnknownPage(page)
	}

	next := strconv.FormatUint(uint64(overflow.NextPage.Value), 10)
	if overflow.NextPage.Value == 0 {
		next = "0 (end of chain)"
	}

	rows := []Field{
		{Label: "Page", Value: strconv.FormatUint(uint64(page.PageNumber), 10)},
		{Label: "Type", Value: "overflow"},
		{Label: "Page size", Value: fmt.Sprintf("%d bytes", len(page.Raw))},
		{Label: "Usable bytes", Value: strconv.FormatUint(uint64(overflow.UsablePageBytes), 10)},
		{Label: "File offset", Value: strconv.FormatUint(uint64(page.PageNumber-1)*uint64(len(page.Raw)), 10)},
		Blank(),
		Section("STRUCTURE"),
		{Label: "Next overflow page", Value: next, Span: spanPtr(overflow.NextPage.Meta)},
		{Label: "Payload", Value: fmt.Sprintf("%d bytes", overflow.Payload.Size), Span: spanPtr(overflow.Payload)},
	}
	if page.OverflowOwner != nil {
		rows = append(rows,
			Blank(),
			Section("OWNER"),
			Field{Label: "Parent page", Value: strconv.FormatUint(uint64(page.OverflowOwner.ParentPage), 10)},
			Field{Label: "Owner cell", Value: fmt.Sprintf("%s %d", page.OverflowOwner.CellKind, page.OverflowOwner.CellIndex)},
			Field{Label: "First overflow page", Value: strconv.FormatUint(uint64(page.OverflowOwner.FirstPage), 10)},
			Field{Label: "Overflow part", Value: fmt.Sprintf("%d of %d", page.OverflowOwner.PartIndex, page.OverflowOwner.PartCount)},
		)
	}

	return &PageInspection{
		Ref:       PageRef{ID: uint64(page.PageNumber)},
		Raw:       append([]byte(nil), page.Raw...),
		Rows:      rows,
		HexBlocks: sqliteOverflowPageBlocks(page),
	}
}

func adaptSQLiteFreelistTrunkPage(page *sqlite.PageInspection) *PageInspection {
	trunk := page.FreelistTrunkPage
	if trunk == nil {
		return adaptSQLiteUnknownPage(page)
	}

	next := strconv.FormatUint(uint64(trunk.NextTrunkPage.Value), 10)
	if trunk.NextTrunkPage.Value == 0 {
		next = "0 (end of chain)"
	}

	rows := []Field{
		{Label: "Page", Value: strconv.FormatUint(uint64(page.PageNumber), 10)},
		{Label: "Type", Value: "freelist trunk"},
		{Label: "Page size", Value: fmt.Sprintf("%d bytes", len(page.Raw))},
		{Label: "Usable bytes", Value: strconv.FormatUint(uint64(trunk.UsablePageBytes), 10)},
		{Label: "File offset", Value: strconv.FormatUint(uint64(page.PageNumber-1)*uint64(len(page.Raw)), 10)},
		Blank(),
		Section("STRUCTURE"),
		{Label: "Next trunk page", Value: next, Span: spanPtr(trunk.NextTrunkPage.Meta)},
		{Label: "Freelist leaf pages", Value: strconv.Itoa(len(trunk.LeafPages)), Span: spanPtr(trunk.LeafPageCount.Meta)},
	}

	return &PageInspection{
		Ref:       PageRef{ID: uint64(page.PageNumber)},
		Raw:       append([]byte(nil), page.Raw...),
		Rows:      rows,
		HexBlocks: sqliteFreelistTrunkPageBlocks(page),
	}
}

func adaptSQLiteFreelistLeafPage(page *sqlite.PageInspection) *PageInspection {
	leaf := page.FreelistLeafPage
	if leaf == nil {
		return adaptSQLiteUnknownPage(page)
	}

	rows := []Field{
		{Label: "Page", Value: strconv.FormatUint(uint64(page.PageNumber), 10)},
		{Label: "Type", Value: "freelist leaf"},
		{Label: "Page size", Value: fmt.Sprintf("%d bytes", len(page.Raw))},
		{Label: "Usable bytes", Value: strconv.FormatUint(uint64(leaf.UsablePageBytes), 10)},
		{Label: "File offset", Value: strconv.FormatUint(uint64(page.PageNumber-1)*uint64(len(page.Raw)), 10)},
		Blank(),
		Section("STRUCTURE"),
		{Label: "Reusable page", Value: "yes"},
	}

	return &PageInspection{
		Ref:       PageRef{ID: uint64(page.PageNumber)},
		Raw:       append([]byte(nil), page.Raw...),
		Rows:      rows,
		HexBlocks: sqliteFreelistLeafPageBlocks(page),
	}
}

func adaptSQLiteUnknownPage(page *sqlite.PageInspection) *PageInspection {
	rows := []Field{
		{Label: "Page", Value: strconv.FormatUint(uint64(page.PageNumber), 10)},
		{Label: "Type", Value: "unknown"},
		{Label: "Page size", Value: fmt.Sprintf("%d bytes", len(page.Raw))},
		{Label: "File offset", Value: strconv.FormatUint(uint64(page.PageNumber-1)*uint64(len(page.Raw)), 10)},
		Blank(),
		Section("STRUCTURE"),
		{Label: "Parsed structure", Value: "none"},
	}

	return &PageInspection{
		Ref:       PageRef{ID: uint64(page.PageNumber)},
		Raw:       append([]byte(nil), page.Raw...),
		Rows:      rows,
		HexBlocks: sqliteUnknownPageBlocks(page),
	}
}

func sqlitePageBlocks(page *sqlite.PageInspection) []HexBlock {
	if page == nil {
		return nil
	}

	blocks := []HexBlock{}
	if page.PageNumber == 1 && page.DBHeader != nil {
		block := HexBlock{
			ID:    "database-header",
			Kind:  blockDatabaseHeader,
			Title: "Database Header",
			Span:  spanFromMeta(sqlite.Meta{StartOffset: 0, Size: 100}),
		}
		block.Rows = databaseHeaderRows(block, page.DBHeader)
		blocks = append(blocks, block)
	}
	blocks = appendBlock(blocks, blockPageHeader, "Page Header", page.BTreePage.PageHeader.Meta, pageHeaderRows(page.BTreePage.PageHeader), nil)
	blocks = appendBlock(blocks, blockPointerArray, "Pointer Array", page.BTreePage.CellPointerArray.Meta, pointerArrayRows(page.BTreePage.CellPointerArray), nil)
	for idx, freeblock := range page.BTreePage.Freeblocks {
		blocks = appendBlock(blocks, blockFreeblock, "Freeblock", freeblock.Meta, freeblockRows(freeblock.Meta, page.BTreePage.Freeblocks), nil).withLastID(fmt.Sprintf("freeblock-%d", idx))
	}
	for idx, region := range page.BTreePage.UnallocatedRegions {
		rows := []Field{
			{Label: "Unallocated", Value: ""},
			{Label: "Offset", Value: offsetRange(region.Meta)},
			{Label: "Size", Value: fmt.Sprintf("%d bytes", region.Meta.Size)},
			Blank(),
			Section("FIELDS"),
			{Label: "Parsed structure", Value: "none"},
			{Label: "Role", Value: "gap before cell area"},
		}
		blocks = appendBlock(blocks, blockUnallocated, "Unallocated", region.Meta, rows, nil).withLastID(fmt.Sprintf("unallocated-%d", idx))
	}
	for idx, cell := range page.BTreePage.TableLeafCells {
		title := fmt.Sprintf("Cell %d", idx)
		blocks = appendBlock(blocks, blockTableLeafCell, title, cell.Meta, tableLeafCellRows(title, cell), tableLeafDrillChildren(title, cell)).withLastID(fmt.Sprintf("table-leaf-cell-%d", idx))
	}
	for idx, cell := range page.BTreePage.TableInteriorCells {
		title := fmt.Sprintf("Cell %d", idx)
		blocks = appendBlock(blocks, blockTableInteriorCell, title, cell.Meta, tableInteriorCellRows(title, cell), tableInteriorDrillChildren(title, cell)).withLastID(fmt.Sprintf("table-interior-cell-%d", idx))
	}
	for idx, cell := range page.BTreePage.IndexLeafCells {
		title := fmt.Sprintf("Cell %d", idx)
		blocks = appendBlock(blocks, blockIndexLeafCell, title, cell.Meta, indexLeafCellRows(title, cell), indexLeafDrillChildren(title, cell)).withLastID(fmt.Sprintf("index-leaf-cell-%d", idx))
	}
	for idx, cell := range page.BTreePage.IndexInteriorCells {
		title := fmt.Sprintf("Cell %d", idx)
		blocks = appendBlock(blocks, blockIndexInteriorCell, title, cell.Meta, indexInteriorCellRows(title, cell), indexInteriorDrillChildren(title, cell)).withLastID(fmt.Sprintf("index-interior-cell-%d", idx))
	}

	sort.SliceStable(blocks, func(i, j int) bool {
		if blocks[i].Span.Start == blocks[j].Span.Start {
			return blocks[i].Kind < blocks[j].Kind
		}
		return blocks[i].Span.Start < blocks[j].Span.Start
	})
	return blocks
}

func sqliteOverflowPageBlocks(page *sqlite.PageInspection) []HexBlock {
	if page == nil || page.OverflowPage == nil {
		return nil
	}
	overflow := page.OverflowPage
	blocks := []HexBlock{}
	blocks = appendBlock(blocks, blockOverflowNextPage, "Next Overflow Page", overflow.NextPage.Meta, []Field{
		{Label: "Next Overflow Page", Value: ""},
		{Label: "Offset", Value: offsetRange(overflow.NextPage.Meta)},
		{Label: "Size", Value: "4 bytes"},
		Blank(),
		Section("FIELDS"),
		{Label: "Page number", Value: overflowNextPageLabel(overflow.NextPage.Value), Span: spanPtr(overflow.NextPage.Meta)},
		{Label: "Meaning", Value: "next page in this overflow chain"},
	}, nil)
	blocks = appendBlock(blocks, blockOverflowPayload, "Overflow Payload", overflow.Payload, []Field{
		{Label: "Overflow Payload", Value: ""},
		{Label: "Offset", Value: offsetRange(overflow.Payload)},
		{Label: "Size", Value: fmt.Sprintf("%d bytes", overflow.Payload.Size)},
		Blank(),
		Section("FIELDS"),
		{Label: "Parsed structure", Value: "none"},
		{Label: "Role", Value: "record payload continuation bytes"},
	}, nil)
	return blocks
}

func sqliteFreelistTrunkPageBlocks(page *sqlite.PageInspection) []HexBlock {
	if page == nil || page.FreelistTrunkPage == nil {
		return nil
	}
	trunk := page.FreelistTrunkPage
	headerMeta := sqlite.Meta{StartOffset: 0, Size: 8}
	blocks := []HexBlock{}
	blocks = appendBlock(blocks, blockFreelistHeader, "Freelist Trunk Header", headerMeta, []Field{
		{Label: "Freelist Trunk Header", Value: ""},
		{Label: "Offset", Value: offsetRange(headerMeta)},
		{Label: "Size", Value: "8 bytes"},
		Blank(),
		Section("FIELDS"),
		{Label: "Next trunk page", Value: overflowNextPageLabel(trunk.NextTrunkPage.Value), Span: spanPtr(trunk.NextTrunkPage.Meta)},
		{Label: "Leaf page count", Value: strconv.Itoa(len(trunk.LeafPages)), Span: spanPtr(trunk.LeafPageCount.Meta)},
	}, nil)
	if trunk.Payload.Size > 0 {
		blocks = appendBlock(blocks, blockFreelistPayload, "Freelist Leaf Page Entries", trunk.Payload, freelistPayloadRows(trunk), nil)
	}
	return blocks
}

func sqliteFreelistLeafPageBlocks(page *sqlite.PageInspection) []HexBlock {
	if page == nil || page.FreelistLeafPage == nil {
		return nil
	}
	leaf := page.FreelistLeafPage
	return []HexBlock{{
		ID:    blockFreelistLeafPage,
		Kind:  blockFreelistLeafPage,
		Title: "Freelist Leaf Page",
		Span:  spanFromMeta(leaf.Payload),
		Rows: []Field{
			{Label: "Freelist Leaf Page", Value: ""},
			{Label: "Offset", Value: offsetRange(leaf.Payload)},
			{Label: "Size", Value: fmt.Sprintf("%d bytes", leaf.Payload.Size)},
			Blank(),
			Section("FIELDS"),
			{Label: "Parsed structure", Value: "none"},
			{Label: "Reusable", Value: "yes"},
		},
	}}
}

func sqliteUnknownPageBlocks(page *sqlite.PageInspection) []HexBlock {
	if page == nil || page.UnknownPage == nil {
		return nil
	}
	unknown := page.UnknownPage
	return []HexBlock{{
		ID:    blockRawPage,
		Kind:  blockRawPage,
		Title: "Raw Page",
		Span:  spanFromMeta(unknown.Payload),
		Rows: []Field{
			{Label: "Raw Page", Value: ""},
			{Label: "Offset", Value: offsetRange(unknown.Payload)},
			{Label: "Size", Value: fmt.Sprintf("%d bytes", unknown.Payload.Size)},
			Blank(),
			Section("FIELDS"),
			{Label: "Parsed structure", Value: "none"},
		},
	}}
}

func freelistPayloadRows(trunk *sqlite.FreelistTrunkPage) []Field {
	rows := []Field{
		{Label: "Freelist Leaf Page Entries", Value: ""},
		{Label: "Offset", Value: offsetRange(trunk.Payload)},
		{Label: "Size", Value: fmt.Sprintf("%d bytes", trunk.Payload.Size)},
		Blank(),
		Section("FIELDS"),
		{Label: "Entries", Value: strconv.Itoa(len(trunk.LeafPages))},
		{Label: "Entry size", Value: "4 bytes"},
	}
	if len(trunk.LeafPages) == 0 {
		return rows
	}
	rows = append(rows, Blank(), Section("PAGES"))
	for idx, leaf := range trunk.LeafPages {
		rows = append(rows, Field{
			Label: fmt.Sprintf("%02d", idx),
			Value: "page " + strconv.FormatUint(uint64(leaf.Value), 10),
			Span:  spanPtr(leaf.Meta),
		})
	}
	return rows
}

type hexBlocks []HexBlock

func (blocks hexBlocks) withLastID(id string) []HexBlock {
	if len(blocks) > 0 {
		blocks[len(blocks)-1].ID = id
	}
	return blocks
}

func appendBlock(blocks []HexBlock, kind string, title string, meta sqlite.Meta, rows []Field, children []HexBlock) hexBlocks {
	if !meta.Valid() || meta.Size <= 0 {
		return blocks
	}
	return append(blocks, HexBlock{
		ID:       kind,
		Kind:     kind,
		Title:    title,
		Span:     spanFromMeta(meta),
		Rows:     rows,
		Children: children,
	})
}

func spanFromMeta(meta sqlite.Meta) ByteSpan {
	return ByteSpan{Start: meta.StartOffset, Size: meta.Size}
}

func spanPtr(meta sqlite.Meta) *ByteSpan {
	span := spanFromMeta(meta)
	return &span
}

func databaseHeaderRows(block HexBlock, header *sqlite.DBHeader) []Field {
	rows := []Field{
		{Label: block.Title, Value: ""},
		{Label: "Offset", Value: spanRange(block.Span)},
		{Label: "Size", Value: fmt.Sprintf("%d bytes", block.Span.Size)},
		Blank(),
		Section("FIELDS"),
	}
	if header == nil {
		return append(rows, Field{Label: "", Value: "Database header is not available."})
	}
	return append(rows,
		Field{Label: "Page size", Value: strconv.FormatUint(uint64(header.PageSize), 10)},
		Field{Label: "Page count", Value: strconv.FormatUint(uint64(header.DatabasePageCount), 10)},
		Field{Label: "Read version", Value: strconv.FormatUint(uint64(header.ReadVersion), 10)},
		Field{Label: "Write version", Value: strconv.FormatUint(uint64(header.WriteVersion), 10)},
		Field{Label: "Reserved bytes/page", Value: strconv.FormatUint(uint64(header.ReservedBytesPerPage), 10)},
		Field{Label: "Freelist pages", Value: strconv.FormatUint(uint64(header.FreelistPageCount), 10)},
		Field{Label: "Schema cookie", Value: strconv.FormatUint(uint64(header.SchemaCookie), 10)},
		Field{Label: "Schema format", Value: strconv.FormatUint(uint64(header.SchemaFormat), 10)},
		Field{Label: "Encoding", Value: textEncodingLabel(header.TextEncoding)},
		Field{Label: "SQLite version", Value: sqliteVersionLabel(header.SQLiteVersionNumber)},
	)
}

func pageHeaderRows(header sqlite.PageHeader) []Field {
	rows := []Field{
		{Label: "Page Header", Value: ""},
		{Label: "Offset", Value: offsetRange(header.Meta)},
		{Label: "Size", Value: fmt.Sprintf("%d bytes", header.Meta.Size)},
		Blank(),
		Section("FIELDS"),
		{Label: "Page kind", Value: sqlitePageKindLabel(header.PageKind.Value), Span: spanPtr(header.PageKind.Meta)},
		{Label: "First freeblock", Value: strconv.FormatUint(uint64(header.FirstFreeblock.Value), 10), Span: spanPtr(header.FirstFreeblock.Meta)},
		{Label: "Cell count", Value: strconv.FormatUint(uint64(header.CellCount.Value), 10), Span: spanPtr(header.CellCount.Meta)},
		{Label: "Cell content start", Value: strconv.FormatUint(uint64(header.CellContentAreaOffset.Value), 10), Span: spanPtr(header.CellContentAreaOffset.Meta)},
		{Label: "Fragmented free bytes", Value: strconv.FormatUint(uint64(header.FragmentedFreeBytes.Value), 10), Span: spanPtr(header.FragmentedFreeBytes.Meta)},
	}
	if header.RightMostPointer != nil {
		rows = append(rows, Field{Label: "Right-most pointer", Value: strconv.FormatUint(uint64(header.RightMostPointer.Value), 10), Span: spanPtr(header.RightMostPointer.Meta)})
	}
	return rows
}

func pointerArrayRows(array sqlite.CellPointerArray) []Field {
	rows := []Field{
		{Label: "Pointer Array", Value: ""},
		{Label: "Offset", Value: offsetRange(array.Meta)},
		{Label: "Size", Value: fmt.Sprintf("%d bytes", array.Meta.Size)},
		Blank(),
		Section("FIELDS"),
		{Label: "Entries", Value: strconv.Itoa(len(array.Pointers))},
		{Label: "Entry size", Value: "2 bytes"},
		{Label: "Points to", Value: "cell offsets"},
	}
	if len(array.Pointers) == 0 {
		return rows
	}
	rows = append(rows, Blank(), Section("POINTERS"))
	for idx, pointer := range array.Pointers {
		rows = append(rows, Field{Label: fmt.Sprintf("%02d", idx), Value: fmt.Sprintf("offset %d", pointer.Value), Span: spanPtr(pointer.Meta)})
	}
	return rows
}

func freeblockRows(meta sqlite.Meta, freeblocks []sqlite.Freeblock) []Field {
	next := uint16(0)
	for _, freeblock := range freeblocks {
		if freeblock.Meta.StartOffset == meta.StartOffset {
			next = freeblock.NextFreeblock.Value
			break
		}
	}
	return []Field{
		{Label: "Freeblock", Value: ""},
		{Label: "Offset", Value: offsetRange(meta)},
		{Label: "Size", Value: fmt.Sprintf("%d bytes", meta.Size)},
		Blank(),
		Section("FIELDS"),
		{Label: "Next freeblock", Value: strconv.FormatUint(uint64(next), 10)},
		{Label: "Block size", Value: strconv.Itoa(meta.Size)},
		{Label: "Reusable", Value: "yes"},
	}
}

func tableLeafCellRows(title string, cell sqlite.TableLeafCell) []Field {
	rows := cellHeaderRows(title, "table leaf cell", cell.Meta)
	rows = append(rows,
		Section("FIELDS"),
		Field{Label: "RowID", Value: strconv.FormatUint(cell.RowID.Value, 10), Span: spanPtr(cell.RowID.Meta)},
		Field{Label: "Payload size", Value: strconv.FormatUint(cell.PayloadSize.Value, 10), Span: spanPtr(cell.PayloadSize.Meta)},
	)
	return appendRecordPayloadRows(rows, cell.ParsedPayload, true)
}

func tableInteriorCellRows(title string, cell sqlite.TableInteriorCell) []Field {
	rows := cellHeaderRows(title, "table interior cell", cell.Meta)
	return append(rows,
		Section("FIELDS"),
		Field{Label: "Left child page", Value: strconv.FormatUint(uint64(cell.LeftChildPage.Value), 10), Span: spanPtr(cell.LeftChildPage.Meta)},
		Field{Label: "RowID separator", Value: strconv.FormatUint(cell.RowID.Value, 10), Span: spanPtr(cell.RowID.Meta)},
	)
}

func indexLeafCellRows(title string, cell sqlite.IndexLeafCell) []Field {
	rows := cellHeaderRows(title, "index leaf cell", cell.Meta)
	rows = append(rows,
		Section("FIELDS"),
		Field{Label: "Payload size", Value: strconv.FormatUint(cell.PayloadSize.Value, 10), Span: spanPtr(cell.PayloadSize.Meta)},
	)
	return appendRecordPayloadRows(rows, cell.ParsedPayload, true)
}

func indexInteriorCellRows(title string, cell sqlite.IndexInteriorCell) []Field {
	rows := cellHeaderRows(title, "index interior cell", cell.Meta)
	rows = append(rows,
		Section("FIELDS"),
		Field{Label: "Left child page", Value: strconv.FormatUint(uint64(cell.LeftChildPage.Value), 10), Span: spanPtr(cell.LeftChildPage.Meta)},
		Field{Label: "Payload size", Value: strconv.FormatUint(cell.PayloadSize.Value, 10), Span: spanPtr(cell.PayloadSize.Meta)},
	)
	return appendRecordPayloadRows(rows, cell.ParsedPayload, false)
}

func cellHeaderRows(title string, cellType string, meta sqlite.Meta) []Field {
	return []Field{
		{Label: title, Value: ""},
		{Label: "Type", Value: cellType},
		{Label: "Offset", Value: offsetRange(meta)},
		{Label: "Size", Value: fmt.Sprintf("%d bytes", meta.Size)},
		Blank(),
	}
}

func appendRecordPayloadRows(rows []Field, payload *sqlite.RecordPayload, includeLocal bool) []Field {
	if payload == nil {
		return append(rows, Field{Label: "Record payload", Value: "unavailable"})
	}
	rows = append(rows, Field{Label: "Record payload", Value: offsetRange(payload.Meta), Span: spanPtr(payload.Meta)})
	if includeLocal {
		rows = append(rows, Field{Label: "Local payload", Value: fmt.Sprintf("%d bytes", payload.Meta.Size)})
	}
	rows = append(rows, Field{Label: "Overflow", Value: yesNo(payload.OverflowFirstPage != nil)})
	if payload.OverflowFirstPage != nil {
		rows = append(rows, Field{Label: "Overflow first page", Value: strconv.FormatUint(uint64(payload.OverflowFirstPage.Value), 10), Span: spanPtr(payload.OverflowFirstPage.Meta)})
	}
	return appendRecordValueRows(rows, payload)
}

func appendRecordValueRows(rows []Field, payload *sqlite.RecordPayload) []Field {
	if payload == nil || payload.OverflowFirstPage != nil {
		return rows
	}
	if len(payload.Columns) == 0 {
		return append(rows, Blank(), Section("VALUES"), Field{Label: "", Value: "No decoded values"})
	}

	rows = append(rows, Blank(), Section("VALUES"))
	for idx, column := range payload.Columns {
		rows = append(rows, Field{
			Label: fmt.Sprintf("%02d", idx),
			Value: fmt.Sprintf("%s (%s, serial %d)", recordValueLabel(column.Value), recordValueType(column), column.SerialType),
			Span:  spanPtr(column.Meta),
		})
	}
	return rows
}

func tableLeafDrillChildren(parent string, cell sqlite.TableLeafCell) []HexBlock {
	children := []HexBlock{}
	children = appendDrillChild(children, blockPayloadSize, "Payload Size", parent, cell.PayloadSize.Meta, []Field{
		{Label: "Varint value", Value: strconv.FormatUint(cell.PayloadSize.Value, 10)},
		{Label: "Meaning", Value: "record payload bytes"},
	}, nil)
	children = appendDrillChild(children, blockRowID, "RowID", parent, cell.RowID.Meta, []Field{
		{Label: "Varint value", Value: strconv.FormatUint(cell.RowID.Value, 10)},
		{Label: "Meaning", Value: "table row key"},
	}, nil)
	return appendRecordPayloadDrillChild(children, parent, cell.ParsedPayload)
}

func tableInteriorDrillChildren(parent string, cell sqlite.TableInteriorCell) []HexBlock {
	children := []HexBlock{}
	children = appendDrillChild(children, blockLeftChildPage, "Left Child Page", parent, cell.LeftChildPage.Meta, []Field{
		{Label: "Page number", Value: strconv.FormatUint(uint64(cell.LeftChildPage.Value), 10)},
		{Label: "Meaning", Value: "child subtree"},
	}, nil)
	children = appendDrillChild(children, blockRowID, "RowID", parent, cell.RowID.Meta, []Field{
		{Label: "Varint value", Value: strconv.FormatUint(cell.RowID.Value, 10)},
		{Label: "Meaning", Value: "table row separator"},
	}, nil)
	return children
}

func indexLeafDrillChildren(parent string, cell sqlite.IndexLeafCell) []HexBlock {
	children := []HexBlock{}
	children = appendDrillChild(children, blockPayloadSize, "Payload Size", parent, cell.PayloadSize.Meta, []Field{
		{Label: "Varint value", Value: strconv.FormatUint(cell.PayloadSize.Value, 10)},
		{Label: "Meaning", Value: "record payload bytes"},
	}, nil)
	return appendRecordPayloadDrillChild(children, parent, cell.ParsedPayload)
}

func indexInteriorDrillChildren(parent string, cell sqlite.IndexInteriorCell) []HexBlock {
	children := []HexBlock{}
	children = appendDrillChild(children, blockLeftChildPage, "Left Child Page", parent, cell.LeftChildPage.Meta, []Field{
		{Label: "Page number", Value: strconv.FormatUint(uint64(cell.LeftChildPage.Value), 10)},
		{Label: "Meaning", Value: "child subtree"},
	}, nil)
	children = appendDrillChild(children, blockPayloadSize, "Payload Size", parent, cell.PayloadSize.Meta, []Field{
		{Label: "Varint value", Value: strconv.FormatUint(cell.PayloadSize.Value, 10)},
		{Label: "Meaning", Value: "record payload bytes"},
	}, nil)
	return appendRecordPayloadDrillChild(children, parent, cell.ParsedPayload)
}

func appendRecordPayloadDrillChild(children []HexBlock, parent string, payload *sqlite.RecordPayload) []HexBlock {
	if payload == nil {
		return children
	}

	headerSize := strconv.FormatUint(payload.HeaderSize.Value, 10)
	if payload.HeaderSize.Meta.Size == 0 {
		headerSize = "unavailable"
	}
	payloadRows := []Field{
		{Label: "Header size", Value: headerSize},
		{Label: "Serial types", Value: strconv.Itoa(len(payload.SerialTypes))},
		{Label: "Values", Value: strconv.Itoa(len(payload.Columns))},
		{Label: "Overflow", Value: yesNo(payload.OverflowFirstPage != nil)},
	}

	children = appendDrillChild(children, blockRecordPayload, "Record Payload", parent, payload.Meta, payloadRows, recordPayloadDrillChildren(payload))

	if payload.OverflowFirstPage != nil {
		children = appendDrillChild(children, blockOverflowPointer, "Overflow Pointer", parent, payload.OverflowFirstPage.Meta, []Field{
			{Label: "First overflow page", Value: strconv.FormatUint(uint64(payload.OverflowFirstPage.Value), 10)},
			{Label: "Meaning", Value: "payload continuation"},
		}, nil)
	}
	return children
}

func recordPayloadDrillChildren(payload *sqlite.RecordPayload) []HexBlock {
	children := []HexBlock{}
	if payload.HeaderSize.Meta.Size > 0 {
		headerEnd := payload.Meta.StartOffset + int(payload.HeaderSize.Value) - 1
		children = appendDrillChild(children, blockRecordHeaderSize, "Record Header Size", "Record Payload", payload.HeaderSize.Meta, []Field{
			{Label: "Header size", Value: strconv.FormatUint(payload.HeaderSize.Value, 10)},
			{Label: "Header range", Value: fmt.Sprintf("%d..%d", payload.Meta.StartOffset, headerEnd)},
			{Label: "Value area starts", Value: strconv.Itoa(headerEnd + 1)},
		}, nil)
	}

	for idx, serialType := range payload.SerialTypes {
		valueSize := 0
		if idx < len(payload.Columns) {
			valueSize = payload.Columns[idx].Meta.Size
		}
		children = appendDrillChild(children, blockSerialType, fmt.Sprintf("Serial Type %d", idx+1), "Record Payload", serialType.Meta, []Field{
			{Label: "Serial type", Value: strconv.FormatUint(serialType.Value, 10)},
			{Label: "Storage class", Value: storageClassLabel(serialType.Value)},
			{Label: "Value size", Value: byteCount(valueSize)},
			{Label: "Value block", Value: fmt.Sprintf("Value %d", idx+1)},
		}, nil)
	}

	for idx, column := range payload.Columns {
		children = appendDrillChild(children, blockRecordValue, fmt.Sprintf("Value %d", idx+1), "Record Payload", column.Meta, []Field{
			{Label: "Storage class", Value: storageClassLabel(column.SerialType)},
			{Label: "Serial type", Value: strconv.FormatUint(column.SerialType, 10)},
			{Label: "Value", Value: recordValueLabel(column.Value)},
		}, nil)
	}
	return children
}

func appendDrillChild(children []HexBlock, kind string, title string, parent string, meta sqlite.Meta, parsed []Field, nested []HexBlock) []HexBlock {
	if !meta.Valid() || meta.Size <= 0 {
		return children
	}
	rows := []Field{
		{Label: title, Value: ""},
		{Label: "Parent", Value: parent},
		{Label: "Offset", Value: offsetRange(meta)},
		{Label: "Size", Value: byteCount(meta.Size)},
		Blank(),
		Section("PARSED"),
	}
	if len(parsed) == 0 {
		rows = append(rows, Field{Label: "Parsed structure", Value: "none"})
	} else {
		rows = append(rows, parsed...)
	}
	return append(children, HexBlock{
		ID:       fmt.Sprintf("%s-%d", kind, len(children)),
		Kind:     kind,
		Title:    title,
		Span:     spanFromMeta(meta),
		Rows:     rows,
		Children: nested,
	})
}

func sqlitePageKindLabel(kind sqlite.PageKindType) string {
	switch kind {
	case sqlite.InteriorIndexBTreePage:
		return "interior index"
	case sqlite.InteriorTableBTreePage:
		return "interior table"
	case sqlite.LeafIndexBTreePage:
		return "leaf index"
	case sqlite.LeafTableBTreePage:
		return "leaf table"
	default:
		return fmt.Sprintf("0x%02x", kind)
	}
}

func overflowNextPageLabel(value uint32) string {
	if value == 0 {
		return "0 (end of chain)"
	}
	return strconv.FormatUint(uint64(value), 10)
}

func textEncodingLabel(value uint32) string {
	switch value {
	case 1:
		return "UTF-8"
	case 2:
		return "UTF-16le"
	case 3:
		return "UTF-16be"
	default:
		return fmt.Sprintf("unknown (%d)", value)
	}
}

func sqliteVersionLabel(value uint32) string {
	major := value / 1000000
	minor := (value / 1000) % 1000
	patch := value % 1000
	return fmt.Sprintf("%d.%d.%d", major, minor, patch)
}

func stringValue(value any) string {
	if value == nil {
		return ""
	}
	if s, ok := value.(string); ok {
		return s
	}
	return fmt.Sprint(value)
}

func uint64Value(value any) uint64 {
	switch v := value.(type) {
	case uint32:
		return uint64(v)
	case uint64:
		return v
	case int64:
		return uint64(v)
	case int:
		return uint64(v)
	case float64:
		return uint64(v)
	default:
		return 0
	}
}

func offsetRange(meta sqlite.Meta) string {
	if meta.Size <= 0 {
		return fmt.Sprintf("%d..%d", meta.StartOffset, meta.StartOffset)
	}
	return fmt.Sprintf("%d..%d", meta.StartOffset, meta.EndOffset()-1)
}

func spanRange(span ByteSpan) string {
	if span.Size <= 0 {
		return fmt.Sprintf("%d..%d", span.Start, span.Start)
	}
	return fmt.Sprintf("%d..%d", span.Start, span.End()-1)
}

func recordValueType(column sqlite.RecordColumn) string {
	switch value := column.Value.(type) {
	case nil:
		return "null"
	case int64, int, uint64, uint32:
		return "int"
	case float64:
		return "float"
	case string:
		return fmt.Sprintf("text, %d bytes", column.Meta.Size)
	case []byte:
		return fmt.Sprintf("blob, %d bytes", len(value))
	default:
		return fmt.Sprintf("%T", value)
	}
}

func recordValueLabel(value any) string {
	switch value := value.(type) {
	case nil:
		return "NULL"
	case string:
		return truncateLine(fmt.Sprintf("%q", value), 80)
	case []byte:
		if len(value) == 0 {
			return "x''"
		}
		encoded := strings.ToUpper(hex.EncodeToString(value))
		if len(encoded) > 64 {
			encoded = encoded[:64] + "..."
		}
		return "x'" + encoded + "'"
	default:
		return fmt.Sprint(value)
	}
}

func truncateLine(value string, maxRunes int) string {
	runes := []rune(value)
	if len(runes) <= maxRunes {
		return value
	}
	if maxRunes <= 3 {
		return string(runes[:maxRunes])
	}
	return string(runes[:maxRunes-3]) + "..."
}

func storageClassLabel(serialType uint64) string {
	switch serialType {
	case 0:
		return "null"
	case 1, 2, 3, 4, 5, 6:
		return "int"
	case 7:
		return "float"
	case 8:
		return "const-0"
	case 9:
		return "const-1"
	default:
		if serialType%2 == 1 {
			return "text"
		}
		return "blob"
	}
}

func byteCount(size int) string {
	if size == 1 {
		return "1 byte"
	}
	return fmt.Sprintf("%d bytes", size)
}

func yesNo(value bool) string {
	if value {
		return "yes"
	}
	return "no"
}
