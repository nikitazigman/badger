package sqlite

import (
	"encoding/binary"
	"fmt"
)

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
