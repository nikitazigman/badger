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
	Meta                  Meta
	PageKind              PageKindField
	FirstFreeblock        Uint16Field
	CellCount             Uint16Field
	CellContentAreaOffset Uint16Field
	FragmentedFreeBytes   Uint8Field
	RightMostPointer      *Uint32Field
}

func (h *PageHeader) HeaderSize() int {
	if h.IsInterior() {
		return 12
	}
	return 8
}

func (h *PageHeader) IsInterior() bool {
	if h.PageKind.Value == InteriorIndexBTreePage || h.PageKind.Value == InteriorTableBTreePage {
		return true
	}
	return false
}

func parsePageHeader(header []byte, pageNumber uint32, pageSize uint32, headerOffset int) (*PageHeader, error) {
	if len(header) < 8 {
		return nil, fmt.Errorf("page header is truncated: expected at least 8 bytes, got %d", len(header))
	}

	pageHeader := &PageHeader{}
	pageHeader.PageKind = PageKindField{
		Meta:  metaFromPage(pageNumber, pageSize, headerOffset, 1),
		Value: PageKindType(header[0]),
	}
	switch pageHeader.PageKind.Value {
	case InteriorIndexBTreePage, InteriorTableBTreePage, LeafIndexBTreePage, LeafTableBTreePage:
	default:
		return nil, fmt.Errorf("page has unsupported b-tree page kind 0x%02x", pageHeader.PageKind.Value)
	}

	pageHeader.FirstFreeblock = Uint16Field{
		Meta:  metaFromPage(pageNumber, pageSize, headerOffset+1, 2),
		Value: binary.BigEndian.Uint16(header[1:3]),
	}
	pageHeader.CellCount = Uint16Field{
		Meta:  metaFromPage(pageNumber, pageSize, headerOffset+3, 2),
		Value: binary.BigEndian.Uint16(header[3:5]),
	}
	pageHeader.CellContentAreaOffset = Uint16Field{
		Meta:  metaFromPage(pageNumber, pageSize, headerOffset+5, 2),
		Value: binary.BigEndian.Uint16(header[5:7]),
	}
	pageHeader.FragmentedFreeBytes = Uint8Field{
		Meta:  metaFromPage(pageNumber, pageSize, headerOffset+7, 1),
		Value: header[7],
	}

	if pageHeader.IsInterior() {
		if len(header) < 12 {
			return nil, fmt.Errorf("interior page header is truncated: expected at least 12 bytes, got %d", len(header))
		}
		pageHeader.RightMostPointer = &Uint32Field{
			Meta:  metaFromPage(pageNumber, pageSize, headerOffset+8, 4),
			Value: binary.BigEndian.Uint32(header[8:12]),
		}
	}
	pageHeader.Meta = metaFromPage(pageNumber, pageSize, headerOffset, pageHeader.HeaderSize())
	return pageHeader, nil
}
