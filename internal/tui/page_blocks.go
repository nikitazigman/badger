package tui

import (
	"fmt"
	"strings"

	"github.com/nikitazigman/badger/internal/storage"
)

const (
	pageBlockDatabaseHeader    = "database_header"
	pageBlockPageHeader        = "page_header"
	pageBlockPointerArray      = "pointer_array"
	pageBlockFreeblock         = "freeblock"
	pageBlockUnallocated       = "unallocated"
	pageBlockTableLeafCell     = "table_leaf_cell"
	pageBlockTableInteriorCell = "table_interior_cell"
	pageBlockIndexLeafCell     = "index_leaf_cell"
	pageBlockIndexInteriorCell = "index_interior_cell"
)

type pageBlock struct {
	kind      string
	meta      storage.ByteSpan
	titleText string
	rows      []storage.Field
	children  []drillChild
}

func buildPageBlocks(page *storage.PageInspection) []pageBlock {
	if page == nil {
		return nil
	}
	blocks := make([]pageBlock, 0, len(page.HexBlocks))
	for _, block := range page.HexBlocks {
		blocks = append(blocks, pageBlockFromStorage(block))
	}
	return blocks
}

func pageBlockFromStorage(block storage.HexBlock) pageBlock {
	children := make([]drillChild, 0, len(block.Children))
	for _, child := range block.Children {
		children = append(children, drillChildFromStorage(child))
	}
	return pageBlock{
		kind:      block.Kind,
		meta:      block.Span,
		titleText: block.Title,
		rows:      block.Rows,
		children:  children,
	}
}

func (b pageBlock) title() string {
	if b.titleText != "" {
		return b.titleText
	}
	return "Block"
}

func revealHexBlockScroll(scroll int, block pageBlock, dataRows int) int {
	return revealHexMetaScroll(scroll, block.meta, dataRows)
}

func revealHexMetaScroll(scroll int, meta storage.ByteSpan, dataRows int) int {
	if dataRows <= 0 {
		return scroll
	}
	startRow := meta.Start / 16
	endRow := max(startRow, (meta.End()-1)/16)
	if startRow < scroll {
		return startRow
	}
	if endRow >= scroll+dataRows {
		if endRow-startRow+1 >= dataRows {
			return startRow
		}
		return endRow - dataRows + 1
	}
	return scroll
}

func blockMetaLines(block pageBlock, page *storage.PageInspection) []string {
	if page == nil {
		return []string{"Waiting for page metadata."}
	}
	if len(block.rows) == 0 {
		return []string{
			block.title(),
			"Offset: " + spanRange(block.meta),
			fmt.Sprintf("Size: %d bytes", block.meta.Size),
		}
	}
	return fieldLines(block.rows)
}

func fieldLines(rows []storage.Field) []string {
	lines := make([]string, 0, len(rows))
	for _, row := range rows {
		switch {
		case row.Label == "" && row.Value == "":
			lines = append(lines, "")
		case row.Label == "$section":
			lines = append(lines, sectionStyle.Render(row.Value))
		case row.Label == "":
			lines = append(lines, row.Value)
		case row.Value == "":
			lines = append(lines, row.Label)
		case strings.HasPrefix(row.Value, "offset ") && len(row.Label) == 2:
			lines = append(lines, fmt.Sprintf("%s -> %s", row.Label, row.Value))
		default:
			lines = append(lines, row.Label+": "+row.Value)
		}
	}
	return lines
}

func spanRange(meta storage.ByteSpan) string {
	if meta.Size <= 0 {
		return fmt.Sprintf("%d..%d", meta.Start, meta.Start)
	}
	return fmt.Sprintf("%d..%d", meta.Start, meta.End()-1)
}

func yesNo(value bool) string {
	if value {
		return "yes"
	}
	return "no"
}
