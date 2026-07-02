package tui

import (
	"encoding/hex"
	"fmt"
	"sort"
	"strings"

	"github.com/nikitazigman/badger/internal/sqlite"
)

type pageBlockKind int

const (
	pageBlockDatabaseHeader pageBlockKind = iota
	pageBlockPageHeader
	pageBlockPointerArray
	pageBlockFreeblock
	pageBlockUnallocated
	pageBlockTableLeafCell
	pageBlockTableInteriorCell
	pageBlockIndexLeafCell
	pageBlockIndexInteriorCell
)

type pageBlock struct {
	kind      pageBlockKind
	meta      sqlite.Meta
	cellIndex int
}

func buildPageBlocks(page *sqlite.PageInspection) []pageBlock {
	if page == nil {
		return nil
	}

	blocks := []pageBlock{}
	if page.PageNumber == 1 && page.DBHeader != nil {
		blocks = append(blocks, pageBlock{kind: pageBlockDatabaseHeader, meta: sqlite.Meta{StartOffset: 0, Size: 100}})
	}
	blocks = appendBlock(blocks, pageBlockPageHeader, page.BTreePage.PageHeader.Meta, -1)
	blocks = appendBlock(blocks, pageBlockPointerArray, page.BTreePage.CellPointerArray.Meta, -1)
	for _, freeblock := range page.BTreePage.Freeblocks {
		blocks = appendBlock(blocks, pageBlockFreeblock, freeblock.Meta, -1)
	}
	for _, region := range page.BTreePage.UnallocatedRegions {
		blocks = appendBlock(blocks, pageBlockUnallocated, region.Meta, -1)
	}
	for idx, cell := range page.BTreePage.TableLeafCells {
		blocks = appendBlock(blocks, pageBlockTableLeafCell, cell.Meta, idx)
	}
	for idx, cell := range page.BTreePage.TableInteriorCells {
		blocks = appendBlock(blocks, pageBlockTableInteriorCell, cell.Meta, idx)
	}
	for idx, cell := range page.BTreePage.IndexLeafCells {
		blocks = appendBlock(blocks, pageBlockIndexLeafCell, cell.Meta, idx)
	}
	for idx, cell := range page.BTreePage.IndexInteriorCells {
		blocks = appendBlock(blocks, pageBlockIndexInteriorCell, cell.Meta, idx)
	}

	sort.SliceStable(blocks, func(i, j int) bool {
		if blocks[i].meta.StartOffset == blocks[j].meta.StartOffset {
			return blocks[i].kind < blocks[j].kind
		}
		return blocks[i].meta.StartOffset < blocks[j].meta.StartOffset
	})
	return blocks
}

func appendBlock(blocks []pageBlock, kind pageBlockKind, meta sqlite.Meta, cellIndex int) []pageBlock {
	if !meta.Valid() || meta.Size <= 0 {
		return blocks
	}
	return append(blocks, pageBlock{kind: kind, meta: meta, cellIndex: cellIndex})
}

func (b pageBlock) title() string {
	switch b.kind {
	case pageBlockDatabaseHeader:
		return "Database Header"
	case pageBlockPageHeader:
		return "Page Header"
	case pageBlockPointerArray:
		return "Pointer Array"
	case pageBlockFreeblock:
		return "Freeblock"
	case pageBlockUnallocated:
		return "Unallocated"
	case pageBlockTableLeafCell, pageBlockTableInteriorCell, pageBlockIndexLeafCell, pageBlockIndexInteriorCell:
		return fmt.Sprintf("Cell %d", b.cellIndex)
	default:
		return "Block"
	}
}

func revealHexBlockScroll(scroll int, block pageBlock, dataRows int) int {
	if dataRows <= 0 {
		return scroll
	}
	startRow := block.meta.StartOffset / 16
	endRow := max(startRow, (block.meta.EndOffset()-1)/16)
	if startRow < scroll {
		return startRow
	}
	if endRow >= scroll+dataRows {
		if endRow-startRow+1 >= dataRows {
			return startRow
		}
		return endRow - dataRows + 1
	}
	return scroll
}

func blockMetaLines(block pageBlock, page *sqlite.PageInspection) []string {
	if page == nil {
		return []string{"Waiting for page metadata."}
	}

	switch block.kind {
	case pageBlockDatabaseHeader:
		return databaseHeaderMetaLines(block, page.DBHeader)
	case pageBlockPageHeader:
		return pageHeaderMetaLines(block, page.BTreePage.PageHeader)
	case pageBlockPointerArray:
		return pointerArrayMetaLines(block, page.BTreePage.CellPointerArray)
	case pageBlockFreeblock:
		return freeblockMetaLines(block, page.BTreePage.Freeblocks)
	case pageBlockUnallocated:
		return []string{
			block.title(),
			"Offset: " + offsetRange(block.meta),
			fmt.Sprintf("Size: %d bytes", block.meta.Size),
			"",
			sectionStyle.Render("FIELDS"),
			"Parsed structure: none",
			"Role: gap before cell area",
		}
	case pageBlockTableLeafCell:
		if block.cellIndex >= 0 && block.cellIndex < len(page.BTreePage.TableLeafCells) {
			return tableLeafCellMetaLines(block, page.BTreePage.TableLeafCells[block.cellIndex])
		}
	case pageBlockTableInteriorCell:
		if block.cellIndex >= 0 && block.cellIndex < len(page.BTreePage.TableInteriorCells) {
			return tableInteriorCellMetaLines(block, page.BTreePage.TableInteriorCells[block.cellIndex])
		}
	case pageBlockIndexLeafCell:
		if block.cellIndex >= 0 && block.cellIndex < len(page.BTreePage.IndexLeafCells) {
			return indexLeafCellMetaLines(block, page.BTreePage.IndexLeafCells[block.cellIndex])
		}
	case pageBlockIndexInteriorCell:
		if block.cellIndex >= 0 && block.cellIndex < len(page.BTreePage.IndexInteriorCells) {
			return indexInteriorCellMetaLines(block, page.BTreePage.IndexInteriorCells[block.cellIndex])
		}
	}

	return []string{
		block.title(),
		"Offset: " + offsetRange(block.meta),
		fmt.Sprintf("Size: %d bytes", block.meta.Size),
	}
}

func databaseHeaderMetaLines(block pageBlock, header *sqlite.DBHeader) []string {
	lines := []string{
		block.title(),
		"Offset: " + offsetRange(block.meta),
		fmt.Sprintf("Size: %d bytes", block.meta.Size),
		"",
		sectionStyle.Render("FIELDS"),
	}
	if header == nil {
		return append(lines, "Database header is not available.")
	}
	return append(lines,
		fmt.Sprintf("Page size: %d", header.PageSize),
		fmt.Sprintf("Page count: %d", header.DatabasePageCount),
		fmt.Sprintf("Read version: %d", header.ReadVersion),
		fmt.Sprintf("Write version: %d", header.WriteVersion),
		fmt.Sprintf("Reserved bytes/page: %d", header.ReservedBytesPerPage),
		fmt.Sprintf("Freelist pages: %d", header.FreelistPageCount),
		fmt.Sprintf("Schema cookie: %d", header.SchemaCookie),
		fmt.Sprintf("Schema format: %d", header.SchemaFormat),
		fmt.Sprintf("Encoding: %s", textEncodingLabel(header.TextEncoding)),
		fmt.Sprintf("SQLite version: %s", sqliteVersionLabel(header.SQLiteVersionNumber)),
	)
}

func pageHeaderMetaLines(block pageBlock, header sqlite.PageHeader) []string {
	lines := []string{
		block.title(),
		"Offset: " + offsetRange(block.meta),
		fmt.Sprintf("Size: %d bytes", block.meta.Size),
		"",
		sectionStyle.Render("FIELDS"),
		fmt.Sprintf("Page kind: %s", pageKindLabel(header.PageKind.Value)),
		fmt.Sprintf("First freeblock: %d", header.FirstFreeblock.Value),
		fmt.Sprintf("Cell count: %d", header.CellCount.Value),
		fmt.Sprintf("Cell content start: %d", header.CellContentAreaOffset.Value),
		fmt.Sprintf("Fragmented free bytes: %d", header.FragmentedFreeBytes.Value),
	}
	if header.RightMostPointer != nil {
		lines = append(lines, fmt.Sprintf("Right-most pointer: %d", header.RightMostPointer.Value))
	}
	return lines
}

func pointerArrayMetaLines(block pageBlock, array sqlite.CellPointerArray) []string {
	lines := []string{
		block.title(),
		"Offset: " + offsetRange(block.meta),
		fmt.Sprintf("Size: %d bytes", block.meta.Size),
		"",
		sectionStyle.Render("FIELDS"),
		fmt.Sprintf("Entries: %d", len(array.Pointers)),
		"Entry size: 2 bytes",
		"Points to: cell offsets",
	}
	if len(array.Pointers) == 0 {
		return lines
	}
	lines = append(lines, "", sectionStyle.Render("POINTERS"))
	for idx, pointer := range array.Pointers {
		lines = append(lines, fmt.Sprintf("%02d -> offset %d", idx, pointer.Value))
	}
	return lines
}

func freeblockMetaLines(block pageBlock, freeblocks []sqlite.Freeblock) []string {
	next := uint16(0)
	for _, freeblock := range freeblocks {
		if freeblock.Meta.StartOffset == block.meta.StartOffset {
			next = freeblock.NextFreeblock.Value
			break
		}
	}
	return []string{
		block.title(),
		"Offset: " + offsetRange(block.meta),
		fmt.Sprintf("Size: %d bytes", block.meta.Size),
		"",
		sectionStyle.Render("FIELDS"),
		fmt.Sprintf("Next freeblock: %d", next),
		fmt.Sprintf("Block size: %d", block.meta.Size),
		"Reusable: yes",
	}
}

func tableLeafCellMetaLines(block pageBlock, cell sqlite.TableLeafCell) []string {
	lines := cellHeaderLines(block, "table leaf cell")
	lines = append(lines,
		sectionStyle.Render("FIELDS"),
		fmt.Sprintf("RowID: %d", cell.RowID.Value),
		fmt.Sprintf("Payload size: %d", cell.PayloadSize.Value),
	)
	lines = appendRecordPayloadLines(lines, cell.ParsedPayload, true)
	return lines
}

func tableInteriorCellMetaLines(block pageBlock, cell sqlite.TableInteriorCell) []string {
	lines := cellHeaderLines(block, "table interior cell")
	return append(lines,
		sectionStyle.Render("FIELDS"),
		fmt.Sprintf("Left child page: %d", cell.LeftChildPage.Value),
		fmt.Sprintf("RowID separator: %d", cell.RowID.Value),
	)
}

func indexLeafCellMetaLines(block pageBlock, cell sqlite.IndexLeafCell) []string {
	lines := cellHeaderLines(block, "index leaf cell")
	lines = append(lines,
		sectionStyle.Render("FIELDS"),
		fmt.Sprintf("Payload size: %d", cell.PayloadSize.Value),
	)
	lines = appendRecordPayloadLines(lines, cell.ParsedPayload, true)
	return lines
}

func indexInteriorCellMetaLines(block pageBlock, cell sqlite.IndexInteriorCell) []string {
	lines := cellHeaderLines(block, "index interior cell")
	lines = append(lines,
		sectionStyle.Render("FIELDS"),
		fmt.Sprintf("Left child page: %d", cell.LeftChildPage.Value),
		fmt.Sprintf("Payload size: %d", cell.PayloadSize.Value),
	)
	lines = appendRecordPayloadLines(lines, cell.ParsedPayload, false)
	return lines
}

func cellHeaderLines(block pageBlock, cellType string) []string {
	return []string{
		block.title(),
		"Type: " + cellType,
		"Offset: " + offsetRange(block.meta),
		fmt.Sprintf("Size: %d bytes", block.meta.Size),
		"",
	}
}

func appendRecordPayloadLines(lines []string, payload *sqlite.RecordPayload, includeLocal bool) []string {
	if payload == nil {
		return append(lines, "Record payload: unavailable")
	}
	lines = append(lines, "Record payload: "+offsetRange(payload.Meta))
	if includeLocal {
		lines = append(lines, fmt.Sprintf("Local payload: %d bytes", payload.Meta.Size))
	}
	lines = append(lines, "Overflow: "+yesNo(payload.OverflowFirstPage != nil))
	if payload.OverflowFirstPage != nil {
		lines = append(lines, fmt.Sprintf("Overflow first page: %d", payload.OverflowFirstPage.Value))
	}
	lines = appendRecordValueLines(lines, payload)
	return lines
}

func appendRecordValueLines(lines []string, payload *sqlite.RecordPayload) []string {
	if payload == nil || payload.OverflowFirstPage != nil {
		return lines
	}
	if len(payload.Columns) == 0 {
		return append(lines, "", sectionStyle.Render("VALUES"), "No decoded values")
	}

	lines = append(lines, "", sectionStyle.Render("VALUES"))
	for idx, column := range payload.Columns {
		lines = append(lines, fmt.Sprintf("%02d: %s (%s, serial %d)", idx, recordValueLabel(column.Value), recordValueType(column), column.SerialType))
	}
	return lines
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

func offsetRange(meta sqlite.Meta) string {
	if meta.Size <= 0 {
		return fmt.Sprintf("%d..%d", meta.StartOffset, meta.StartOffset)
	}
	return fmt.Sprintf("%d..%d", meta.StartOffset, meta.EndOffset()-1)
}

func yesNo(value bool) string {
	if value {
		return "yes"
	}
	return "no"
}
