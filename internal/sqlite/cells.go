package sqlite

import (
	"encoding/binary"
	"fmt"
)

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
	ParsedPayload *RecordPayload
}

func parseTableLeafCell(cell []byte, usablePageBytes uint16) (*TableLeafCell, error) {
	payloadSize, payloadVarintBytes, err := decodeVarint(cell)
	if err != nil {
		return nil, fmt.Errorf("table leaf payload size: %w", err)
	}

	rowID, _, err := decodeVarint(cell[payloadVarintBytes:])
	if err != nil {
		return nil, fmt.Errorf("table leaf rowid: %w", err)
	}

	parsedPayload, err := parseCellRecordPayload(cell, LeafTableBTreePage, payloadSize, usablePageBytes)
	if err != nil {
		return nil, fmt.Errorf("table leaf parsed payload: %w", err)
	}

	return &TableLeafCell{
		PayloadSize:   payloadSize,
		RowID:         rowID,
		ParsedPayload: parsedPayload,
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
	PayloadSize   uint64
	ParsedPayload *RecordPayload
}

func parseIndexLeafCell(cell []byte, usablePageBytes uint16) (*IndexLeafCell, error) {
	payloadSize, _, err := decodeVarint(cell)
	if err != nil {
		return nil, fmt.Errorf("index leaf payload size: %w", err)
	}

	parsedPayload, err := parseCellRecordPayload(cell, LeafIndexBTreePage, payloadSize, usablePageBytes)
	if err != nil {
		return nil, fmt.Errorf("index leaf parsed payload: %w", err)
	}

	return &IndexLeafCell{
		PayloadSize:   payloadSize,
		ParsedPayload: parsedPayload,
	}, nil
}

type IndexInteriorCell struct {
	LeftChildPage uint32
	PayloadSize   uint64
	ParsedPayload *RecordPayload
}

func parseIndexInteriorCell(cell []byte, usablePageBytes uint16) (*IndexInteriorCell, error) {
	if len(cell) < 4 {
		return nil, fmt.Errorf("index interior cell is truncated: expected at least 4 bytes, got %d", len(cell))
	}

	leftChildPage := binary.BigEndian.Uint32(cell[0:4])
	payloadSize, _, err := decodeVarint(cell[4:])
	if err != nil {
		return nil, fmt.Errorf("index interior payload size: %w", err)
	}

	parsedPayload, err := parseCellRecordPayload(cell, InteriorIndexBTreePage, payloadSize, usablePageBytes)
	if err != nil {
		return nil, fmt.Errorf("index interior parsed payload: %w", err)
	}

	return &IndexInteriorCell{
		LeftChildPage: leftChildPage,
		PayloadSize:   payloadSize,
		ParsedPayload: parsedPayload,
	}, nil
}

func parseCellRecordPayload(cell []byte, pageKind PageKindType, payloadSize uint64, usablePageBytes uint16) (*RecordPayload, error) {
	localPayload, overflowFirstPage, err := cellLocalPayload(cell, pageKind, payloadSize, usablePageBytes)
	if err != nil {
		return nil, fmt.Errorf("payload extraction: %w", err)
	}

	if overflowFirstPage != nil {
		return &RecordPayload{
			UsesOverflow:      true,
			OverflowFirstPage: overflowFirstPage,
		}, nil
	}

	parsedPayload, err := parseRecordPayload(localPayload)
	if err != nil {
		return nil, fmt.Errorf("record decode: %w", err)
	}
	return parsedPayload, nil
}
