package sqlite

import (
	"encoding/binary"
	"fmt"
	"sort"
)

type BTreePage struct {
	PageNumber         uint32
	Raw                []byte
	UsablePageBytes    uint16
	PageHeader         PageHeader
	CellPointerArray   CellPointerArray
	TableLeafCells     []TableLeafCell
	TableInteriorCells []TableInteriorCell
	IndexLeafCells     []IndexLeafCell
	IndexInteriorCells []IndexInteriorCell
	Freeblocks         []Freeblock
	UnallocatedRegions []UnallocatedRegion
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

	pageHeader, err := parsePageHeader(page[headerOffset:], pageNumber, uint32(len(page)), headerOffset)
	if err != nil {
		return nil, err
	}

	cellPointers := make([]Uint16Field, 0, pageHeader.CellCount.Value)
	headerSize := pageHeader.HeaderSize()
	for idx := range pageHeader.CellCount.Value {
		ptrOffset := headerOffset + headerSize + int(idx)*2
		if ptrOffset+2 > len(page) {
			return nil, fmt.Errorf("page %d cell pointer %d is truncated", pageNumber, idx)
		}
		cellPointer := binary.BigEndian.Uint16(page[ptrOffset : ptrOffset+2])
		if int(cellPointer) >= len(page) {
			return nil, fmt.Errorf("page %d cell pointer %d out of range: %d", pageNumber, idx, cellPointer)
		}
		cellPointers = append(cellPointers, Uint16Field{
			Meta:  metaFromPage(pageNumber, uint32(len(page)), ptrOffset, 2),
			Value: cellPointer,
		})
	}

	inspection := &BTreePage{
		PageNumber:      pageNumber,
		Raw:             append([]byte(nil), page...),
		UsablePageBytes: usablePageBytes,
		PageHeader:      *pageHeader,
		CellPointerArray: CellPointerArray{
			Meta:     metaFromPage(pageNumber, uint32(len(page)), headerOffset+headerSize, len(cellPointers)*2),
			Pointers: cellPointers,
		},
	}
	freeblocks, err := parseFreeblocks(page, pageNumber, uint32(len(page)), pageHeader.FirstFreeblock.Value)
	if err != nil {
		return nil, fmt.Errorf("page %d freeblock parse failed: %w", pageNumber, err)
	}
	inspection.Freeblocks = freeblocks
	inspection.UnallocatedRegions = parseUnallocatedRegions(pageNumber, uint32(len(page)), headerOffset+headerSize+len(cellPointers)*2, int(pageHeader.CellContentAreaOffset.Value), freeblocks)

	switch pageHeader.PageKind.Value {
	case LeafTableBTreePage:
		cells, err := parseCells(page, cellPointers, func(cell []byte, offset uint16) (*TableLeafCell, error) {
			return parseTableLeafCell(cell, usablePageBytes, pageNumber, uint32(len(page)), offset)
		})
		if err != nil {
			return nil, fmt.Errorf("page %d table leaf parse failed: %w", pageNumber, err)
		}
		inspection.TableLeafCells = cells
	case InteriorTableBTreePage:
		cells, err := parseCells(page, cellPointers, func(cell []byte, offset uint16) (*TableInteriorCell, error) {
			return parseTableInteriorCell(cell, pageNumber, uint32(len(page)), offset)
		})
		if err != nil {
			return nil, fmt.Errorf("page %d table interior parse failed: %w", pageNumber, err)
		}
		inspection.TableInteriorCells = cells
	case LeafIndexBTreePage:
		cells, err := parseCells(page, cellPointers, func(cell []byte, offset uint16) (*IndexLeafCell, error) {
			return parseIndexLeafCell(cell, usablePageBytes, pageNumber, uint32(len(page)), offset)
		})
		if err != nil {
			return nil, fmt.Errorf("page %d index leaf parse failed: %w", pageNumber, err)
		}
		inspection.IndexLeafCells = cells
	case InteriorIndexBTreePage:
		cells, err := parseCells(page, cellPointers, func(cell []byte, offset uint16) (*IndexInteriorCell, error) {
			return parseIndexInteriorCell(cell, usablePageBytes, pageNumber, uint32(len(page)), offset)
		})
		if err != nil {
			return nil, fmt.Errorf("page %d index interior parse failed: %w", pageNumber, err)
		}
		inspection.IndexInteriorCells = cells
	}

	return inspection, nil
}

type Freeblock struct {
	Meta          Meta
	NextFreeblock Uint16Field
}

type UnallocatedRegion struct {
	Meta Meta
}

func parseFreeblocks(page []byte, pageNumber uint32, pageSize uint32, first uint16) ([]Freeblock, error) {
	if first == 0 {
		return nil, nil
	}
	seen := map[uint16]struct{}{}
	var blocks []Freeblock
	current := first
	for current != 0 {
		if _, exists := seen[current]; exists {
			return nil, fmt.Errorf("freeblock chain contains cycle at offset %d", current)
		}
		seen[current] = struct{}{}
		if int(current)+4 > len(page) {
			return nil, fmt.Errorf("freeblock at offset %d is truncated", current)
		}
		next := binary.BigEndian.Uint16(page[current : current+2])
		size := int(binary.BigEndian.Uint16(page[current+2 : current+4]))
		if size < 4 {
			return nil, fmt.Errorf("freeblock at offset %d has invalid size %d", current, size)
		}
		if int(current)+size > len(page) {
			return nil, fmt.Errorf("freeblock at offset %d exceeds page bounds", current)
		}
		blocks = append(blocks, Freeblock{
			Meta: metaFromPage(pageNumber, pageSize, int(current), size),
			NextFreeblock: Uint16Field{
				Meta:  metaFromPage(pageNumber, pageSize, int(current), 2),
				Value: next,
			},
		})
		current = next
	}
	return blocks, nil
}

func parseUnallocatedRegions(pageNumber uint32, pageSize uint32, start int, end int, freeblocks []Freeblock) []UnallocatedRegion {
	if end <= start {
		return nil
	}
	sort.Slice(freeblocks, func(i, j int) bool {
		return freeblocks[i].Meta.StartOffset < freeblocks[j].Meta.StartOffset
	})
	cursor := start
	var regions []UnallocatedRegion
	for _, block := range freeblocks {
		if block.Meta.EndOffset() <= start || block.Meta.StartOffset >= end {
			continue
		}
		if cursor < block.Meta.StartOffset {
			regions = append(regions, UnallocatedRegion{Meta: metaFromPage(pageNumber, pageSize, cursor, block.Meta.StartOffset-cursor)})
		}
		if block.Meta.EndOffset() > cursor {
			cursor = block.Meta.EndOffset()
		}
	}
	if cursor < end {
		regions = append(regions, UnallocatedRegion{Meta: metaFromPage(pageNumber, pageSize, cursor, end-cursor)})
	}
	return regions
}

func (p *BTreePage) BytesFor(meta Meta) []byte {
	if p == nil || !meta.Valid() || meta.StartOffset < 0 || meta.EndOffset() > len(p.Raw) {
		return nil
	}
	return append([]byte(nil), p.Raw[meta.StartOffset:meta.EndOffset()]...)
}
