package sqlite

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"os"
)

// DBHeader mirrors the 100-byte SQLite database header at file offset 0.
// Field order and types map directly to https://sqlite.org/fileformat.html.
type DBHeader struct {
	HeaderString              string
	PageSize                  uint32
	WriteVersion              uint8
	ReadVersion               uint8
	ReservedBytesPerPage      uint8
	MaxEmbeddedPayloadFrac    uint8
	MinEmbeddedPayloadFrac    uint8
	LeafPayloadFrac           uint8
	FileChangeCounter         uint32
	DatabasePageCount         uint32
	FirstFreelistTrunkPage    uint32
	FreelistPageCount         uint32
	SchemaCookie              uint32
	SchemaFormat              uint32
	SuggestedCacheSize        int32
	AutoVacuumLargestRootPage uint32
	TextEncoding              uint32
	UserVersion               uint32
	IncrementalVacuum         uint32
	ApplicationID             uint32
	ReservedForExpansion      [20]byte
	VersionValidFor           uint32
	SQLiteVersionNumber       uint32
}

func parseHeader(b []byte) (*DBHeader, error) {
	if len(b) < 100 {
		return nil, fmt.Errorf("sqlite header is truncated: expected 100 bytes, got %d", len(b))
	}

	const magic = "SQLite format 3\x00"
	if !bytes.Equal(b[0:16], []byte(magic)) {
		return nil, fmt.Errorf("invalid sqlite header magic: got %q", string(b[0:16]))
	}

	pageSizeRaw := binary.BigEndian.Uint16(b[16:18])
	pageSize := uint32(pageSizeRaw)
	if pageSizeRaw == 1 {
		pageSize = 65536
	}

	header := &DBHeader{
		HeaderString:              string(b[0:16]),
		PageSize:                  pageSize,
		WriteVersion:              b[18],
		ReadVersion:               b[19],
		ReservedBytesPerPage:      b[20],
		MaxEmbeddedPayloadFrac:    b[21],
		MinEmbeddedPayloadFrac:    b[22],
		LeafPayloadFrac:           b[23],
		FileChangeCounter:         binary.BigEndian.Uint32(b[24:28]),
		DatabasePageCount:         binary.BigEndian.Uint32(b[28:32]),
		FirstFreelistTrunkPage:    binary.BigEndian.Uint32(b[32:36]),
		FreelistPageCount:         binary.BigEndian.Uint32(b[36:40]),
		SchemaCookie:              binary.BigEndian.Uint32(b[40:44]),
		SchemaFormat:              binary.BigEndian.Uint32(b[44:48]),
		SuggestedCacheSize:        int32(binary.BigEndian.Uint32(b[48:52])),
		AutoVacuumLargestRootPage: binary.BigEndian.Uint32(b[52:56]),
		TextEncoding:              binary.BigEndian.Uint32(b[56:60]),
		UserVersion:               binary.BigEndian.Uint32(b[60:64]),
		IncrementalVacuum:         binary.BigEndian.Uint32(b[64:68]),
		ApplicationID:             binary.BigEndian.Uint32(b[68:72]),
		VersionValidFor:           binary.BigEndian.Uint32(b[92:96]),
		SQLiteVersionNumber:       binary.BigEndian.Uint32(b[96:100]),
	}

	copy(header.ReservedForExpansion[:], b[72:92])

	return header, nil
}

type Inspector struct {
	file     *os.File
	dbHeader *DBHeader
}

func Open(path string) (*Inspector, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}

	buf := make([]byte, 100)
	_, err = f.ReadAt(buf, 0)
	if err != nil {
		return nil, err
	}

	header, err := parseHeader(buf)
	if err != nil {
		_ = f.Close()
		return nil, err
	}

	return &Inspector{
		file:     f,
		dbHeader: header,
	}, nil
}

func (i *Inspector) Close() error {
	return i.file.Close()
}

func (i *Inspector) readPage(number uint32) ([]byte, error) {
	if i == nil || i.file == nil {
		return nil, fmt.Errorf("inspector is not open")
	}
	if i.dbHeader == nil {
		return nil, fmt.Errorf("database header is not loaded")
	}
	if number == 0 {
		return nil, fmt.Errorf("page number must be >= 1")
	}
	if i.dbHeader.DatabasePageCount != 0 && number > i.dbHeader.DatabasePageCount {
		return nil, fmt.Errorf("page number %d out of range (page count: %d)", number, i.dbHeader.DatabasePageCount)
	}

	pageSize := i.dbHeader.PageSize
	if pageSize == 0 {
		return nil, fmt.Errorf("invalid page size: 0")
	}

	offset := uint64(number-1) * uint64(pageSize)
	buf := make([]byte, pageSize)
	n, err := i.file.ReadAt(buf, int64(offset))
	if err != nil {
		if err == io.EOF {
			return nil, fmt.Errorf("page %d is truncated: read %d of %d bytes", number, n, pageSize)
		}
		return nil, fmt.Errorf("read page %d at offset %d: %w", number, offset, err)
	}

	return buf, nil
}

type MetadataInspection struct {
	DBHeader DBHeader
}

func (i *Inspector) InspectDatabaseMetadata() (*MetadataInspection, error) {
	return &MetadataInspection{
		DBHeader: *i.dbHeader,
	}, nil
}

type PageInspection struct {
	PageNumber         uint32
	DBHeader           *DBHeader
	PageHeader         PageHeader
	CellPointers       []uint16
	TableLeafCells     []TableLeafCell
	TableInteriorCells []TableInteriorCell
	IndexLeafCells     []IndexLeafCell
	IndexInteriorCells []IndexInteriorCell
}

func (i *Inspector) InspectPage(number uint32) (*PageInspection, error) {
	page, err := i.readPage(number)
	if err != nil {
		return nil, err
	}

	inspection, err := i.parseBTreePage(page, number)
	if err != nil {
		return nil, err
	}
	if number == 1 {
		inspection.DBHeader = i.dbHeader
	}

	return inspection, nil
}

func (i *Inspector) parseBTreePage(page []byte, pageNumber uint32) (*PageInspection, error) {
	if i == nil || i.dbHeader == nil {
		return nil, fmt.Errorf("database header is not loaded")
	}

	reservedBytes := i.dbHeader.ReservedBytesPerPage
	if len(page) == 0 {
		return nil, fmt.Errorf("page %d is empty", pageNumber)
	}
	if _, err := usablePageSize(page, reservedBytes); err != nil {
		return nil, fmt.Errorf("page %d: %w", pageNumber, err)
	}

	headerOffset := 0
	if pageNumber == 1 {
		if len(page) < 100 {
			return nil, fmt.Errorf("page 1 is truncated: expected at least 100 bytes, got %d", len(page))
		}
		headerOffset = 100
	}
	if headerOffset >= len(page) {
		return nil, fmt.Errorf("page %d has invalid header offset %d", pageNumber, headerOffset)
	}

	pageHeader, err := parsePageHeader(page[headerOffset:])
	if err != nil {
		return nil, err
	}

	cellPointers := make([]uint16, 0, pageHeader.CellCount)
	headerSize := pageHeader.HeaderSize()
	for idx := range pageHeader.CellCount {
		ptrOffset := headerOffset + headerSize + int(idx)*2
		if ptrOffset+2 > len(page) {
			return nil, fmt.Errorf("page %d cell pointer %d is truncated", pageNumber, idx)
		}
		cellPointer := binary.BigEndian.Uint16(page[ptrOffset : ptrOffset+2])
		if int(cellPointer) >= len(page) {
			return nil, fmt.Errorf("page %d cell pointer %d out of range: %d", pageNumber, idx, cellPointer)
		}
		cellPointers = append(cellPointers, cellPointer)
	}

	inspection := &PageInspection{
		PageNumber:   pageNumber,
		PageHeader:   *pageHeader,
		CellPointers: cellPointers,
	}

	switch pageHeader.PageKind {
	case LeafTableBTreePage:
		cells, err := parseCells(page, cellPointers, parseTableLeafCell)
		if err != nil {
			return nil, fmt.Errorf("page %d table leaf parse failed: %w", pageNumber, err)
		}
		inspection.TableLeafCells = cells
	case InteriorTableBTreePage:
		cells, err := parseCells(page, cellPointers, parseTableInteriorCell)
		if err != nil {
			return nil, fmt.Errorf("page %d table interior parse failed: %w", pageNumber, err)
		}
		inspection.TableInteriorCells = cells
	case LeafIndexBTreePage:
		cells, err := parseCells(page, cellPointers, parseIndexLeafCell)
		if err != nil {
			return nil, fmt.Errorf("page %d index leaf parse failed: %w", pageNumber, err)
		}
		inspection.IndexLeafCells = cells
	case InteriorIndexBTreePage:
		cells, err := parseCells(page, cellPointers, parseIndexInteriorCell)
		if err != nil {
			return nil, fmt.Errorf("page %d index interior parse failed: %w", pageNumber, err)
		}
		inspection.IndexInteriorCells = cells
	}

	return inspection, nil
}

func parseCells[T any](page []byte, cellPointers []uint16, parser func([]byte) (*T, error)) ([]T, error) {
	cells := make([]T, 0, len(cellPointers))
	for idx, ptr := range cellPointers {
		cell, err := parser(page[ptr:])
		if err != nil {
			return nil, fmt.Errorf("cell %d parse failed: %w", idx, err)
		}
		cells = append(cells, *cell)
	}
	return cells, nil
}

type TableLeafCell struct {
	PayloadSize   uint64
	RowID         uint64
	HeaderVarints int
}

func parseTableLeafCell(cell []byte) (*TableLeafCell, error) {
	payloadSize, payloadVarintBytes, err := decodeVarint(cell)
	if err != nil {
		return nil, fmt.Errorf("table leaf payload size: %w", err)
	}

	rowID, rowIDVarintBytes, err := decodeVarint(cell[payloadVarintBytes:])
	if err != nil {
		return nil, fmt.Errorf("table leaf rowid: %w", err)
	}

	return &TableLeafCell{
		PayloadSize:   payloadSize,
		RowID:         rowID,
		HeaderVarints: payloadVarintBytes + rowIDVarintBytes,
	}, nil
}

type TableInteriorCell struct {
	LeftChildPage uint32
	RowID         uint64
}

func parseTableInteriorCell(cell []byte) (*TableInteriorCell, error) {
	if len(cell) < 4 {
		return nil, fmt.Errorf("table interior cell is truncated: expected at least 4 bytes, got %d", len(cell))
	}

	leftChildPage := binary.BigEndian.Uint32(cell[0:4])
	rowID, _, err := decodeVarint(cell[4:])
	if err != nil {
		return nil, fmt.Errorf("table interior rowid: %w", err)
	}

	return &TableInteriorCell{
		LeftChildPage: leftChildPage,
		RowID:         rowID,
	}, nil
}

type IndexLeafCell struct {
	PayloadSize uint64
}

func parseIndexLeafCell(cell []byte) (*IndexLeafCell, error) {
	payloadSize, _, err := decodeVarint(cell)
	if err != nil {
		return nil, fmt.Errorf("index leaf payload size: %w", err)
	}

	return &IndexLeafCell{
		PayloadSize: payloadSize,
	}, nil
}

type IndexInteriorCell struct {
	LeftChildPage uint32
	PayloadSize   uint64
}

func parseIndexInteriorCell(cell []byte) (*IndexInteriorCell, error) {
	if len(cell) < 4 {
		return nil, fmt.Errorf("index interior cell is truncated: expected at least 4 bytes, got %d", len(cell))
	}

	leftChildPage := binary.BigEndian.Uint32(cell[0:4])
	payloadSize, _, err := decodeVarint(cell[4:])
	if err != nil {
		return nil, fmt.Errorf("index interior payload size: %w", err)
	}

	return &IndexInteriorCell{
		LeftChildPage: leftChildPage,
		PayloadSize:   payloadSize,
	}, nil
}

type PageKindType int8

const (
	InteriorIndexBTreePage PageKindType = 0x02
	InteriorTableBTreePage PageKindType = 0x05
	LeafIndexBTreePage     PageKindType = 0x0a
	LeafTableBTreePage     PageKindType = 0x0d
)

type PageHeader struct {
	PageKind              PageKindType
	FirstFreeblock        uint16
	CellCount             uint16
	CellContentAreaOffset uint16
	FragmentedFreeBytes   uint8
	RightMostPointer      *uint32
}

func (h *PageHeader) HeaderSize() int {
	if h.IsInterior() {
		return 12
	}
	return 8
}

func (h *PageHeader) IsInterior() bool {
	if h.PageKind == InteriorIndexBTreePage || h.PageKind == InteriorTableBTreePage {
		return true
	}
	return false
}

func parsePageHeader(header []byte) (*PageHeader, error) {
	if len(header) < 8 {
		return nil, fmt.Errorf("page header is truncated: expected at least 8 bytes, got %d", len(header))
	}

	pageHeader := &PageHeader{}
	pageHeader.PageKind = PageKindType(header[0])
	switch pageHeader.PageKind {
	case InteriorIndexBTreePage, InteriorTableBTreePage, LeafIndexBTreePage, LeafTableBTreePage:
	default:
		return nil, fmt.Errorf("page has unsupported b-tree page kind 0x%02x", pageHeader.PageKind)
	}

	pageHeader.FirstFreeblock = binary.BigEndian.Uint16(header[1:3])
	pageHeader.CellCount = binary.BigEndian.Uint16(header[3:5])
	pageHeader.CellContentAreaOffset = binary.BigEndian.Uint16(header[5:7])
	pageHeader.FragmentedFreeBytes = header[7]

	if pageHeader.IsInterior() {
		if len(header) < 12 {
			return nil, fmt.Errorf("interior page header is truncated: expected at least 12 bytes, got %d", len(header))
		}
		value := binary.BigEndian.Uint32(header[8:12])
		pageHeader.RightMostPointer = &value
	}
	return pageHeader, nil
}

func decodeVarint(buf []byte) (uint64, int, error) {
	if len(buf) == 0 {
		return 0, 0, fmt.Errorf("varint is truncated: expected at least 1 byte, got 0")
	}

	var value uint64
	for idx := 0; idx < len(buf) && idx < 8; idx++ {
		b := buf[idx]
		value = (value << 7) | uint64(b&0x7f)
		if b&0x80 == 0 {
			return value, idx + 1, nil
		}
	}

	if len(buf) < 9 {
		return 0, 0, fmt.Errorf("varint is truncated: expected up to 9 bytes, got %d", len(buf))
	}
	value = (value << 8) | uint64(buf[8])
	return value, 9, nil
}

func localPayloadSize(pageKind PageKindType, usableSize uint16, payloadSize uint64) (uint64, error) {
	if usableSize <= 35 {
		return 0, fmt.Errorf("usable page size %d is too small", usableSize)
	}

	u := uint64(usableSize)
	m := ((u - 12) * 32 / 255) - 23
	var x uint64

	switch pageKind {
	case LeafTableBTreePage:
		x = u - 35
	case LeafIndexBTreePage, InteriorIndexBTreePage:
		x = ((u - 12) * 64 / 255) - 23
	default:
		return 0, fmt.Errorf("page kind 0x%02x does not support payload size calculation", pageKind)
	}

	if payloadSize <= x {
		return payloadSize, nil
	}
	if u <= 4 {
		return 0, fmt.Errorf("usable page size %d is invalid for overflow calculation", usableSize)
	}

	k := m + ((payloadSize - m) % (u - 4))
	if k <= x {
		return k, nil
	}
	return m, nil
}

func usablePageSize(page []byte, reservedBytes uint8) (uint16, error) {
	if int(reservedBytes) >= len(page) {
		return 0, fmt.Errorf("reserved bytes per page %d are invalid for page size %d", reservedBytes, len(page))
	}

	usable := len(page) - int(reservedBytes)
	if usable > 65535 {
		return 0, fmt.Errorf("usable page size %d exceeds uint16", usable)
	}
	return uint16(usable), nil
}
