package tui

import (
	"encoding/hex"
	"fmt"
	"sort"
	"strings"

	"github.com/nikitazigman/badger/internal/sqlite"
)

type pageRowType int

const (
	pageRowDatabaseHeader pageRowType = iota
	pageRowPageHeader
	pageRowPointerArray
	pageRowFreeblock
	pageRowUnallocated
	pageRowTableLeafCell
	pageRowTableInteriorCell
	pageRowIndexLeafCell
	pageRowIndexInteriorCell
)

type pageRowViewModel struct {
	Type         pageRowType
	Title        string
	RangeLabel   string
	SizeLabel    string
	Notes        string
	Meta         sqlite.Meta
	RawHex       string
	RawASCII     string
	ByteMap      []string
	ParsedFields []labelValue
	DecodedLines []string
	SortStart    int
}

func buildPageRows(page *sqlite.PageInspection) []pageRowViewModel {
	if page == nil {
		return nil
	}

	rows := make([]pageRowViewModel, 0, 8+len(page.BTreePage.Freeblocks)+len(page.BTreePage.UnallocatedRegions))
	btree := page.BTreePage

	if page.PageNumber == 1 && page.DBHeader != nil {
		meta := sqlite.Meta{StartOffset: 0, Size: 100}
		rows = append(rows, pageRowViewModel{
			Type:       pageRowDatabaseHeader,
			Title:      "Database Header",
			RangeLabel: formatMetaRange(meta),
			SizeLabel:  formatByteCount(meta.Size),
			Notes:      "100-byte file header",
			Meta:       meta,
			RawHex:     formatRawHex(btree.BytesFor(meta), 32),
			RawASCII:   formatASCII(btree.BytesFor(meta), 32),
			ByteMap: []string{
				"0..15 magic string",
				"16..17 page size",
				"18 read/write format",
				"28..31 database page count",
				"56..59 text encoding",
				"96..99 sqlite version",
			},
			ParsedFields: buildHeaderRows(*page.DBHeader),
			DecodedLines: []string{
				fmt.Sprintf("Encoding: %s", textEncodingLabel(page.DBHeader.TextEncoding)),
				fmt.Sprintf("SQLite version: %s", sqliteVersionLabel(page.DBHeader.SQLiteVersionNumber)),
			},
			SortStart: meta.StartOffset,
		})
	}

	rows = append(rows, buildPageHeaderRow(page))
	rows = append(rows, buildPointerArrayRow(page))

	for _, freeblock := range btree.Freeblocks {
		rows = append(rows, buildFreeblockRow(page, freeblock))
	}
	for _, region := range btree.UnallocatedRegions {
		rows = append(rows, buildUnallocatedRow(page, region))
	}
	for idx, cell := range btree.TableLeafCells {
		rows = append(rows, buildTableLeafCellRow(page, idx, cell))
	}
	for idx, cell := range btree.TableInteriorCells {
		rows = append(rows, buildTableInteriorCellRow(page, idx, cell))
	}
	for idx, cell := range btree.IndexLeafCells {
		rows = append(rows, buildIndexLeafCellRow(page, idx, cell))
	}
	for idx, cell := range btree.IndexInteriorCells {
		rows = append(rows, buildIndexInteriorCellRow(page, idx, cell))
	}

	sort.Slice(rows, func(i, j int) bool {
		if rows[i].SortStart == rows[j].SortStart {
			return rows[i].Meta.Size < rows[j].Meta.Size
		}
		return rows[i].SortStart < rows[j].SortStart
	})

	return rows
}

func buildPageHeaderRow(page *sqlite.PageInspection) pageRowViewModel {
	header := page.BTreePage.PageHeader
	raw := page.BTreePage.BytesFor(header.Meta)
	byteMap := []string{
		fmt.Sprintf("%s page kind", formatMetaRange(header.PageKind.Meta)),
		fmt.Sprintf("%s first freeblock", formatMetaRange(header.FirstFreeblock.Meta)),
		fmt.Sprintf("%s cell count", formatMetaRange(header.CellCount.Meta)),
		fmt.Sprintf("%s cell content area start", formatMetaRange(header.CellContentAreaOffset.Meta)),
		fmt.Sprintf("%s fragmented free bytes", formatMetaRange(header.FragmentedFreeBytes.Meta)),
	}
	fields := []labelValue{
		{Label: "Page kind", Value: pageKindLabel(header.PageKind.Value)},
		{Label: "Cell count", Value: fmt.Sprintf("%d", header.CellCount.Value)},
		{Label: "Cell area start", Value: fmt.Sprintf("%d", header.CellContentAreaOffset.Value)},
		{Label: "First freeblock", Value: fmt.Sprintf("%d", header.FirstFreeblock.Value)},
		{Label: "Fragmented free bytes", Value: fmt.Sprintf("%d", header.FragmentedFreeBytes.Value)},
	}
	if header.RightMostPointer != nil {
		byteMap = append(byteMap, fmt.Sprintf("%s right-most pointer", formatMetaRange(header.RightMostPointer.Meta)))
		fields = append(fields, labelValue{Label: "Right-most pointer", Value: fmt.Sprintf("%d", header.RightMostPointer.Value)})
	}

	return pageRowViewModel{
		Type:         pageRowPageHeader,
		Title:        "Page Header",
		RangeLabel:   formatMetaRange(header.Meta),
		SizeLabel:    formatByteCount(header.Meta.Size),
		Notes:        pageKindLabel(header.PageKind.Value),
		Meta:         header.Meta,
		RawHex:       formatRawHex(raw, 32),
		RawASCII:     formatASCII(raw, 32),
		ByteMap:      byteMap,
		ParsedFields: fields,
		DecodedLines: []string{"Core b-tree page header fields."},
		SortStart:    header.Meta.StartOffset,
	}
}

func buildPointerArrayRow(page *sqlite.PageInspection) pageRowViewModel {
	array := page.BTreePage.CellPointerArray
	raw := page.BTreePage.BytesFor(array.Meta)
	byteMap := make([]string, 0, len(array.Pointers))
	decoded := make([]string, 0, len(array.Pointers))
	for idx, ptr := range array.Pointers {
		byteMap = append(byteMap, fmt.Sprintf("%s pointer %d", formatMetaRange(ptr.Meta), idx))
		decoded = append(decoded, fmt.Sprintf("Pointer %d -> offset %d", idx, ptr.Value))
	}

	return pageRowViewModel{
		Type:       pageRowPointerArray,
		Title:      "Pointer Array",
		RangeLabel: formatMetaRange(array.Meta),
		SizeLabel:  formatByteCount(array.Meta.Size),
		Notes:      fmt.Sprintf("%d offsets", len(array.Pointers)),
		Meta:       array.Meta,
		RawHex:     formatRawHex(raw, 32),
		RawASCII:   formatASCII(raw, 32),
		ByteMap:    byteMap,
		ParsedFields: []labelValue{
			{Label: "Entries", Value: fmt.Sprintf("%d", len(array.Pointers))},
		},
		DecodedLines: decoded,
		SortStart:    array.Meta.StartOffset,
	}
}

func buildFreeblockRow(page *sqlite.PageInspection, block sqlite.Freeblock) pageRowViewModel {
	raw := page.BTreePage.BytesFor(block.Meta)
	return pageRowViewModel{
		Type:       pageRowFreeblock,
		Title:      "Freeblock",
		RangeLabel: formatMetaRange(block.Meta),
		SizeLabel:  formatByteCount(block.Meta.Size),
		Notes:      fmt.Sprintf("next %d", block.NextFreeblock.Value),
		Meta:       block.Meta,
		RawHex:     formatRawHex(raw, 32),
		RawASCII:   formatASCII(raw, 32),
		ByteMap: []string{
			fmt.Sprintf("%s next freeblock", formatMetaRange(block.NextFreeblock.Meta)),
			formatMetaRange(sqlite.Meta{StartOffset: block.Meta.StartOffset + 2, Size: 2}) + " block size",
		},
		ParsedFields: []labelValue{
			{Label: "Next freeblock", Value: fmt.Sprintf("%d", block.NextFreeblock.Value)},
			{Label: "Size", Value: fmt.Sprintf("%d", block.Meta.Size)},
		},
		DecodedLines: []string{"Reusable space inside the page."},
		SortStart:    block.Meta.StartOffset,
	}
}

func buildUnallocatedRow(page *sqlite.PageInspection, region sqlite.UnallocatedRegion) pageRowViewModel {
	raw := page.BTreePage.BytesFor(region.Meta)
	return pageRowViewModel{
		Type:       pageRowUnallocated,
		Title:      "Unallocated",
		RangeLabel: formatMetaRange(region.Meta),
		SizeLabel:  formatByteCount(region.Meta.Size),
		Notes:      "unused bytes",
		Meta:       region.Meta,
		RawHex:     formatRawHex(raw, 32),
		RawASCII:   formatASCII(raw, 32),
		ByteMap:    []string{"No parsed structure in this range."},
		ParsedFields: []labelValue{
			{Label: "Size", Value: fmt.Sprintf("%d", region.Meta.Size)},
		},
		DecodedLines: []string{"Gap between active structures."},
		SortStart:    region.Meta.StartOffset,
	}
}

func buildTableLeafCellRow(page *sqlite.PageInspection, idx int, cell sqlite.TableLeafCell) pageRowViewModel {
	raw := page.BTreePage.BytesFor(cell.Meta)
	fields := []labelValue{
		{Label: "Cell kind", Value: "table leaf"},
		{Label: "RowID", Value: fmt.Sprintf("%d", cell.RowID.Value)},
		{Label: "Payload size", Value: fmt.Sprintf("%d", cell.PayloadSize.Value)},
	}
	byteMap := []string{
		fmt.Sprintf("%s payload size", formatMetaRange(cell.PayloadSize.Meta)),
		fmt.Sprintf("%s rowid", formatMetaRange(cell.RowID.Meta)),
	}
	decoded := []string{fmt.Sprintf("Table row with rowid %d.", cell.RowID.Value)}
	if cell.ParsedPayload != nil {
		byteMap = append(byteMap, payloadByteMap(cell.ParsedPayload)...)
		fields = append(fields, payloadParsedFields(cell.ParsedPayload)...)
		decoded = append(decoded, payloadDecodedLines(cell.ParsedPayload)...)
	}

	return pageRowViewModel{
		Type:         pageRowTableLeafCell,
		Title:        fmt.Sprintf("Cell %d", idx),
		RangeLabel:   formatMetaRange(cell.Meta),
		SizeLabel:    formatByteCount(cell.Meta.Size),
		Notes:        "table leaf",
		Meta:         cell.Meta,
		RawHex:       formatRawHex(raw, 32),
		RawASCII:     formatASCII(raw, 32),
		ByteMap:      byteMap,
		ParsedFields: fields,
		DecodedLines: decoded,
		SortStart:    cell.Meta.StartOffset,
	}
}

func buildTableInteriorCellRow(page *sqlite.PageInspection, idx int, cell sqlite.TableInteriorCell) pageRowViewModel {
	raw := page.BTreePage.BytesFor(cell.Meta)
	return pageRowViewModel{
		Type:       pageRowTableInteriorCell,
		Title:      fmt.Sprintf("Cell %d", idx),
		RangeLabel: formatMetaRange(cell.Meta),
		SizeLabel:  formatByteCount(cell.Meta.Size),
		Notes:      "table interior",
		Meta:       cell.Meta,
		RawHex:     formatRawHex(raw, 32),
		RawASCII:   formatASCII(raw, 32),
		ByteMap: []string{
			fmt.Sprintf("%s left child page", formatMetaRange(cell.LeftChildPage.Meta)),
			fmt.Sprintf("%s rowid", formatMetaRange(cell.RowID.Meta)),
		},
		ParsedFields: []labelValue{
			{Label: "Cell kind", Value: "table interior"},
			{Label: "Left child page", Value: fmt.Sprintf("%d", cell.LeftChildPage.Value)},
			{Label: "RowID", Value: fmt.Sprintf("%d", cell.RowID.Value)},
		},
		DecodedLines: []string{"Interior table pointer entry."},
		SortStart:    cell.Meta.StartOffset,
	}
}

func buildIndexLeafCellRow(page *sqlite.PageInspection, idx int, cell sqlite.IndexLeafCell) pageRowViewModel {
	raw := page.BTreePage.BytesFor(cell.Meta)
	fields := []labelValue{
		{Label: "Cell kind", Value: "index leaf"},
		{Label: "Payload size", Value: fmt.Sprintf("%d", cell.PayloadSize.Value)},
	}
	byteMap := []string{
		fmt.Sprintf("%s payload size", formatMetaRange(cell.PayloadSize.Meta)),
	}
	decoded := []string{"Index tuple used for index ordering."}
	if cell.ParsedPayload != nil {
		byteMap = append(byteMap, payloadByteMap(cell.ParsedPayload)...)
		fields = append(fields, payloadParsedFields(cell.ParsedPayload)...)
		decoded = append(decoded, payloadDecodedLines(cell.ParsedPayload)...)
	}

	return pageRowViewModel{
		Type:         pageRowIndexLeafCell,
		Title:        fmt.Sprintf("Cell %d", idx),
		RangeLabel:   formatMetaRange(cell.Meta),
		SizeLabel:    formatByteCount(cell.Meta.Size),
		Notes:        "index leaf",
		Meta:         cell.Meta,
		RawHex:       formatRawHex(raw, 32),
		RawASCII:     formatASCII(raw, 32),
		ByteMap:      byteMap,
		ParsedFields: fields,
		DecodedLines: decoded,
		SortStart:    cell.Meta.StartOffset,
	}
}

func buildIndexInteriorCellRow(page *sqlite.PageInspection, idx int, cell sqlite.IndexInteriorCell) pageRowViewModel {
	raw := page.BTreePage.BytesFor(cell.Meta)
	fields := []labelValue{
		{Label: "Cell kind", Value: "index interior"},
		{Label: "Left child page", Value: fmt.Sprintf("%d", cell.LeftChildPage.Value)},
		{Label: "Payload size", Value: fmt.Sprintf("%d", cell.PayloadSize.Value)},
	}
	byteMap := []string{
		fmt.Sprintf("%s left child page", formatMetaRange(cell.LeftChildPage.Meta)),
		fmt.Sprintf("%s payload size", formatMetaRange(cell.PayloadSize.Meta)),
	}
	decoded := []string{"Interior index tuple used for b-tree routing."}
	if cell.ParsedPayload != nil {
		byteMap = append(byteMap, payloadByteMap(cell.ParsedPayload)...)
		fields = append(fields, payloadParsedFields(cell.ParsedPayload)...)
		decoded = append(decoded, payloadDecodedLines(cell.ParsedPayload)...)
	}

	return pageRowViewModel{
		Type:         pageRowIndexInteriorCell,
		Title:        fmt.Sprintf("Cell %d", idx),
		RangeLabel:   formatMetaRange(cell.Meta),
		SizeLabel:    formatByteCount(cell.Meta.Size),
		Notes:        "index interior",
		Meta:         cell.Meta,
		RawHex:       formatRawHex(raw, 32),
		RawASCII:     formatASCII(raw, 32),
		ByteMap:      byteMap,
		ParsedFields: fields,
		DecodedLines: decoded,
		SortStart:    cell.Meta.StartOffset,
	}
}

func payloadByteMap(payload *sqlite.RecordPayload) []string {
	if payload == nil {
		return nil
	}
	if payload.OverflowFirstPage != nil {
		return []string{
			fmt.Sprintf("%s local payload", formatMetaRange(payload.Meta)),
			fmt.Sprintf("%s overflow pointer", formatMetaRange(payload.OverflowFirstPage.Meta)),
		}
	}

	lines := []string{
		fmt.Sprintf("%s record payload", formatMetaRange(payload.Meta)),
		fmt.Sprintf("%s record header size", formatMetaRange(payload.HeaderSize.Meta)),
	}
	for idx, serialType := range payload.SerialTypes {
		lines = append(lines, fmt.Sprintf("%s serial type %d", formatMetaRange(serialType.Meta), idx))
	}
	for idx, column := range payload.Columns {
		lines = append(lines, fmt.Sprintf("%s value %d", formatMetaRange(column.Meta), idx))
	}
	return lines
}

func payloadParsedFields(payload *sqlite.RecordPayload) []labelValue {
	if payload == nil {
		return nil
	}
	fields := []labelValue{
		{Label: "Payload bytes", Value: fmt.Sprintf("%d", payload.Meta.Size)},
	}
	if payload.OverflowFirstPage != nil {
		fields = append(fields, labelValue{Label: "Overflow page", Value: fmt.Sprintf("%d", payload.OverflowFirstPage.Value)})
		return fields
	}

	fields = append(fields,
		labelValue{Label: "Record header size", Value: fmt.Sprintf("%d", payload.HeaderSize.Value)},
		labelValue{Label: "Serial type count", Value: fmt.Sprintf("%d", len(payload.SerialTypes))},
	)
	for idx, serialType := range payload.SerialTypes {
		fields = append(fields, labelValue{
			Label: fmt.Sprintf("Serial %d", idx),
			Value: fmt.Sprintf("%d (%s)", serialType.Value, storageClassLabel(serialType.Value)),
		})
	}
	return fields
}

func payloadDecodedLines(payload *sqlite.RecordPayload) []string {
	if payload == nil {
		return nil
	}
	if payload.OverflowFirstPage != nil {
		return []string{
			"Payload spills to overflow page.",
			fmt.Sprintf("First overflow page: %d", payload.OverflowFirstPage.Value),
		}
	}

	lines := make([]string, 0, len(payload.Columns))
	for idx, column := range payload.Columns {
		lines = append(lines, fmt.Sprintf("Value %d: %s", idx, formatRecordValue(column.Value)))
	}
	return lines
}

func formatMetaRange(meta sqlite.Meta) string {
	if meta.Size <= 0 {
		return fmt.Sprintf("%d..%d", meta.StartOffset, meta.StartOffset)
	}
	return fmt.Sprintf("%d..%d", meta.StartOffset, meta.EndOffset()-1)
}

func formatByteCount(size int) string {
	return fmt.Sprintf("%db", size)
}

func formatRawHex(raw []byte, maxBytes int) string {
	if len(raw) == 0 {
		return "<empty>"
	}
	if len(raw) > maxBytes {
		return strings.ToUpper(hex.EncodeToString(raw[:maxBytes])) + "..."
	}
	return strings.ToUpper(hex.EncodeToString(raw))
}

func formatASCII(raw []byte, maxBytes int) string {
	if len(raw) == 0 {
		return ""
	}
	if len(raw) > maxBytes {
		raw = raw[:maxBytes]
	}
	out := make([]byte, len(raw))
	for idx, b := range raw {
		if b >= 32 && b <= 126 {
			out[idx] = b
			continue
		}
		out[idx] = '.'
	}
	return string(out)
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

func formatRecordValue(value any) string {
	switch v := value.(type) {
	case nil:
		return "null"
	case string:
		return fmt.Sprintf("%q", v)
	case []byte:
		if len(v) == 0 {
			return "0x"
		}
		return "0x" + strings.ToUpper(hex.EncodeToString(v))
	default:
		return fmt.Sprint(v)
	}
}
