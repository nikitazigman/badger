package sqlite

import (
	"encoding/binary"
	"fmt"
)

type BTreePage struct {
	PageHeader         PageHeader
	CellPointers       []uint16
	TableLeafCells     []TableLeafCell
	TableInteriorCells []TableInteriorCell
	IndexLeafCells     []IndexLeafCell
	IndexInteriorCells []IndexInteriorCell
}

func parseBTreePage(page []byte, pageNumber uint32, reservedBytesPerPage uint8) (*BTreePage, error) {
	if len(page) == 0 {
		return nil, fmt.Errorf("page %d is empty", pageNumber)
	}
	usablePageBytes, err := usablePageSize(page, reservedBytesPerPage)
	if err != nil {
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

	inspection := &BTreePage{
		PageHeader:   *pageHeader,
		CellPointers: cellPointers,
	}

	switch pageHeader.PageKind {
	case LeafTableBTreePage:
		cells, err := parseCells(page, cellPointers, func(cell []byte) (*TableLeafCell, error) {
			return parseTableLeafCell(cell, usablePageBytes)
		})
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
		cells, err := parseCells(page, cellPointers, func(cell []byte) (*IndexLeafCell, error) {
			return parseIndexLeafCell(cell, usablePageBytes)
		})
		if err != nil {
			return nil, fmt.Errorf("page %d index leaf parse failed: %w", pageNumber, err)
		}
		inspection.IndexLeafCells = cells
	case InteriorIndexBTreePage:
		cells, err := parseCells(page, cellPointers, func(cell []byte) (*IndexInteriorCell, error) {
			return parseIndexInteriorCell(cell, usablePageBytes)
		})
		if err != nil {
			return nil, fmt.Errorf("page %d index interior parse failed: %w", pageNumber, err)
		}
		inspection.IndexInteriorCells = cells
	}

	return inspection, nil
}
