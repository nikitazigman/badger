package sqlite

import (
	"bytes"
	"encoding/binary"
	"fmt"
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
