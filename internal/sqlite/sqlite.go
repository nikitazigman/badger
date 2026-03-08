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

type PageHeader struct {
	PageKind              uint8
	HeaderOffsetInPage    uint16
	HeaderSize            uint8
	FirstFreeblock        uint16
	CellCount             uint16
	CellContentAreaOffset uint16
	FragmentedFreeBytes   uint8
	RightMostPointer      *uint32
}

type PageInspection struct {
	PageNumber   uint32
	DBHeader     *DBHeader
	PageHeader   PageHeader
	CellPointers []uint16
}

func (i *Inspector) InspectPage(number uint32) (*PageInspection, error) {
	page, err := i.readPage(number)
	if err != nil {
		return nil, err
	}

	if number == 1 {
		// first 100 bytes belong to the DB Header
		page, err := parseBTreePage(page, number, 100)
		if err != nil {
			return nil, err
		}
		page.DBHeader = i.dbHeader
		return page, nil
	}

	return parseBTreePage(page, number, 0)
}

func parseBTreePage(page []byte, pageNumber uint32, headerOffset uint16) (*PageInspection, error) {
	base := int(headerOffset)
	if len(page) < base+8 {
		return nil, fmt.Errorf("page %d is too short for b-tree header at offset %d", pageNumber, headerOffset)
	}

	pageKind := page[base]
	switch pageKind {
	case 0x02, 0x05, 0x0a, 0x0d:
	default:
		return nil, fmt.Errorf("page %d has unsupported b-tree page kind 0x%02x", pageNumber, pageKind)
	}

	headerSize := uint8(8)
	if pageKind == 0x02 || pageKind == 0x05 {
		headerSize = 12
	}

	if len(page) < base+int(headerSize) {
		return nil, fmt.Errorf("page %d is too short for %d-byte b-tree header at offset %d", pageNumber, headerSize, headerOffset)
	}

	firstFreeblock := binary.BigEndian.Uint16(page[base+1 : base+3])
	cellCount := binary.BigEndian.Uint16(page[base+3 : base+5])
	cellContentAreaOffset := binary.BigEndian.Uint16(page[base+5 : base+7])
	fragmentedFreeBytes := page[base+7]

	var rightMostPointer *uint32
	if headerSize == 12 {
		value := binary.BigEndian.Uint32(page[base+8 : base+12])
		rightMostPointer = &value
	}

	cellPointerStart := uint16(base) + uint16(headerSize)
	cellPointerEnd := cellPointerStart + cellCount*2
	if len(page) < int(cellPointerEnd) {
		return nil, fmt.Errorf(
			"page %d cell pointer array out of range: need %d bytes at offset %d (len=%d)",
			pageNumber,
			int(cellCount)*2,
			cellPointerStart,
			len(page),
		)
	}

	cellPointers := make([]uint16, 0, cellCount)
	for idx := range cellCount {
		ptrOffset := cellPointerStart + idx*2
		cellPointer := binary.BigEndian.Uint16(page[ptrOffset : ptrOffset+2])
		cellPointers = append(cellPointers, cellPointer)
	}

	return &PageInspection{
		PageNumber: pageNumber,
		PageHeader: PageHeader{
			PageKind:              pageKind,
			HeaderOffsetInPage:    headerOffset,
			HeaderSize:            headerSize,
			FirstFreeblock:        firstFreeblock,
			CellCount:             cellCount,
			CellContentAreaOffset: cellContentAreaOffset,
			FragmentedFreeBytes:   fragmentedFreeBytes,
			RightMostPointer:      rightMostPointer,
		},
		CellPointers: cellPointers,
	}, nil
}
