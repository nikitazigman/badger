package sqlite

import (
	"encoding/binary"
	"fmt"
)

type CellPointerArray struct {
	Meta     Meta
	Pointers []Uint16Field
}

func parseCells[T any](page []byte, pointers []Uint16Field, parser func([]byte, uint16) (*T, error)) ([]T, error) {
	cells := make([]T, 0, len(pointers))
	for idx, ptr := range pointers {
		cell, err := parser(page[ptr.Value:], ptr.Value)
		if err != nil {
			return nil, fmt.Errorf("cell %d parse failed: %w", idx, err)
		}
		cells = append(cells, *cell)
	}
	return cells, nil
}

type TableLeafCell struct {
	Meta          Meta
	PayloadSize   VarintField
	RowID         VarintField
	ParsedPayload *RecordPayload
}

func parseTableLeafCell(cell []byte, usablePageBytes uint16, pageNumber uint32, pageSize uint32, cellOffset uint16) (*TableLeafCell, error) {
	payloadSize, payloadVarintBytes, err := decodeVarint(cell)
	if err != nil {
		return nil, fmt.Errorf("table leaf payload size: %w", err)
	}

	rowID, rowIDVarintBytes, err := decodeVarint(cell[payloadVarintBytes:])
	if err != nil {
		return nil, fmt.Errorf("table leaf rowid: %w", err)
	}

	payloadInfo, err := decodeCellLocalPayload(cell, LeafTableBTreePage, payloadSize, usablePageBytes)
	if err != nil {
		return nil, fmt.Errorf("payload extraction: %w", err)
	}

	parsedPayload, err := parseCellRecordPayload(payloadInfo, pageNumber, pageSize, int(cellOffset))
	if err != nil {
		return nil, fmt.Errorf("table leaf parsed payload: %w", err)
	}

	cellSize := int(payloadInfo.OverflowOffset + 4)
	if payloadInfo.OverflowFirstPage == nil {
		cellSize = payloadInfo.PayloadOffset + len(payloadInfo.LocalPayload)
	}
	return &TableLeafCell{
		Meta: metaFromPage(pageNumber, pageSize, int(cellOffset), cellSize),
		PayloadSize: VarintField{
			Meta:  metaFromPage(pageNumber, pageSize, int(cellOffset), payloadVarintBytes),
			Value: payloadSize,
		},
		RowID: VarintField{
			Meta:  metaFromPage(pageNumber, pageSize, int(cellOffset)+payloadVarintBytes, rowIDVarintBytes),
			Value: rowID,
		},
		ParsedPayload: parsedPayload,
	}, nil
}

type TableInteriorCell struct {
	Meta          Meta
	LeftChildPage Uint32Field
	RowID         VarintField
}

func parseTableInteriorCell(cell []byte, pageNumber uint32, pageSize uint32, cellOffset uint16) (*TableInteriorCell, error) {
	if len(cell) < 4 {
		return nil, fmt.Errorf("table interior cell is truncated: expected at least 4 bytes, got %d", len(cell))
	}

	leftChildPage := binary.BigEndian.Uint32(cell[0:4])
	rowID, rowIDBytes, err := decodeVarint(cell[4:])
	if err != nil {
		return nil, fmt.Errorf("table interior rowid: %w", err)
	}

	return &TableInteriorCell{
		Meta: metaFromPage(pageNumber, pageSize, int(cellOffset), 4+rowIDBytes),
		LeftChildPage: Uint32Field{
			Meta:  metaFromPage(pageNumber, pageSize, int(cellOffset), 4),
			Value: leftChildPage,
		},
		RowID: VarintField{
			Meta:  metaFromPage(pageNumber, pageSize, int(cellOffset)+4, rowIDBytes),
			Value: rowID,
		},
	}, nil
}

type IndexLeafCell struct {
	Meta          Meta
	PayloadSize   VarintField
	ParsedPayload *RecordPayload
}

func parseIndexLeafCell(cell []byte, usablePageBytes uint16, pageNumber uint32, pageSize uint32, cellOffset uint16) (*IndexLeafCell, error) {
	payloadSize, payloadVarintBytes, err := decodeVarint(cell)
	if err != nil {
		return nil, fmt.Errorf("index leaf payload size: %w", err)
	}

	payloadInfo, err := decodeCellLocalPayload(cell, LeafIndexBTreePage, payloadSize, usablePageBytes)
	if err != nil {
		return nil, fmt.Errorf("payload extraction: %w", err)
	}

	parsedPayload, err := parseCellRecordPayload(payloadInfo, pageNumber, pageSize, int(cellOffset))
	if err != nil {
		return nil, fmt.Errorf("index leaf parsed payload: %w", err)
	}
	cellSize := int(payloadInfo.OverflowOffset + 4)
	if payloadInfo.OverflowFirstPage == nil {
		cellSize = payloadInfo.PayloadOffset + len(payloadInfo.LocalPayload)
	}
	return &IndexLeafCell{
		Meta: metaFromPage(pageNumber, pageSize, int(cellOffset), cellSize),
		PayloadSize: VarintField{
			Meta:  metaFromPage(pageNumber, pageSize, int(cellOffset), payloadVarintBytes),
			Value: payloadSize,
		},
		ParsedPayload: parsedPayload,
	}, nil
}

type IndexInteriorCell struct {
	Meta          Meta
	LeftChildPage Uint32Field
	PayloadSize   VarintField
	ParsedPayload *RecordPayload
}

func parseIndexInteriorCell(cell []byte, usablePageBytes uint16, pageNumber uint32, pageSize uint32, cellOffset uint16) (*IndexInteriorCell, error) {
	if len(cell) < 4 {
		return nil, fmt.Errorf("index interior cell is truncated: expected at least 4 bytes, got %d", len(cell))
	}

	leftChildPage := binary.BigEndian.Uint32(cell[0:4])
	payloadSize, payloadVarintBytes, err := decodeVarint(cell[4:])
	if err != nil {
		return nil, fmt.Errorf("index interior payload size: %w", err)
	}

	payloadInfo, err := decodeCellLocalPayload(cell, InteriorIndexBTreePage, payloadSize, usablePageBytes)
	if err != nil {
		return nil, fmt.Errorf("payload extraction: %w", err)
	}

	parsedPayload, err := parseCellRecordPayload(payloadInfo, pageNumber, pageSize, int(cellOffset))
	if err != nil {
		return nil, fmt.Errorf("index interior parsed payload: %w", err)
	}
	cellSize := int(payloadInfo.OverflowOffset + 4)
	if payloadInfo.OverflowFirstPage == nil {
		cellSize = payloadInfo.PayloadOffset + len(payloadInfo.LocalPayload)
	}
	return &IndexInteriorCell{
		Meta: metaFromPage(pageNumber, pageSize, int(cellOffset), cellSize),
		LeftChildPage: Uint32Field{
			Meta:  metaFromPage(pageNumber, pageSize, int(cellOffset), 4),
			Value: leftChildPage,
		},
		PayloadSize: VarintField{
			Meta:  metaFromPage(pageNumber, pageSize, int(cellOffset)+4, payloadVarintBytes),
			Value: payloadSize,
		},
		ParsedPayload: parsedPayload,
	}, nil
}

func parseCellRecordPayload(payloadInfo *payloadDecode, pageNumber uint32, pageSize uint32, cellOffset int) (*RecordPayload, error) {
	if payloadInfo.OverflowFirstPage != nil {
		return &RecordPayload{
			Meta: metaFromPage(pageNumber, pageSize, cellOffset+payloadInfo.PayloadOffset, len(payloadInfo.LocalPayload)),
			OverflowFirstPage: &Uint32Field{
				Meta:  metaFromPage(pageNumber, pageSize, cellOffset+payloadInfo.OverflowOffset, 4),
				Value: *payloadInfo.OverflowFirstPage,
			},
		}, nil
	}

	parsedPayload, err := parseRecordPayload(payloadInfo.LocalPayload, pageNumber, pageSize, cellOffset+payloadInfo.PayloadOffset)
	if err != nil {
		return nil, fmt.Errorf("record decode: %w", err)
	}
	return parsedPayload, nil
}
